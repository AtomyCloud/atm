"""Verify local migration packages without executing foreign-platform binaries."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform as host_platform
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def node_manifest(data):
    manifest = json.loads(data)
    assert manifest["schema"] == manifest["min_core_api"] == 2
    assert manifest["id"] == "node" and manifest["version"] == "0.0.1"
    assert manifest["executor"] == "native" and manifest["protocol"] == "jsonl-v1"
    assert manifest["full_setup"] is True
    assert manifest["platforms"] == ["linux"]
    assert manifest["operations"] == ["menu", "status", "install", "update", "use", "remove", "uninstall", "integrate"]
    assert manifest["executables"] == {
        "linux/amd64": "bin/linux-amd64/atm-node",
        "linux/arm64": "bin/linux-arm64/atm-node",
    }
    return manifest


def isolated_environment(root):
    env = {key: value for key, value in os.environ.items() if not key.startswith("ATM_")}
    env.update(HOME=str(root / "user"), XDG_CONFIG_HOME=str(root / "config"),
               XDG_DATA_HOME=str(root / "data"), XDG_CACHE_HOME=str(root / "cache"),
               ATM_CONFIG_DIR=str(root / "config"), ATM_STATE_DIR=str(root / "state"),
               ATM_APPS_DIR=str(root / "apps"), ATM_LANG="en-us", PATH="")
    return env


def snapshot(root):
    return {str(path.relative_to(root)): (path.stat().st_mode,
            hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None)
            for path in root.rglob("*")}


def verify_source(executable):
    """Isolated source manifest and dry-run checks; never run the Node backend."""
    source = Path(__file__).resolve().parents[1]
    manifest = node_manifest((source / "plugins/node/plugin.json").read_bytes())
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(host_platform.machine())
    assert sys.platform.startswith("linux") and arch, "source checks require Linux amd64/arm64"
    relative = Path("plugins/node") / manifest["executables"]["linux/" + arch]
    with tempfile.TemporaryDirectory(prefix="atm-node-source-") as temp:
        root = Path(temp)
        runtime = root / "runtime"
        node = runtime / "plugins/node"
        node.mkdir(parents=True)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/node/plugin.json", node / "plugin.json")
        # A legacy entrypoint is present but must never be selected as fallback.
        (node / "plugin.sh").write_text("exit 99\n")
        env = isolated_environment(root)
        command = [str(executable.resolve()), "--home", str(runtime), "--json"]

        def run(*args, check=True):
            return subprocess.run(command + list(args), env=env, text=True,
                                  capture_output=True, check=check, timeout=20)

        before = snapshot(root)
        discovered, = json.loads(run("plugins", "list").stdout)
        assert discovered["executor"] == "native" and not discovered["available"]
        assert run("--dry-run", "install", "node", check=False).returncode != 0
        assert run("status", "node", check=False).returncode != 0
        assert snapshot(root) == before, "missing-binary paths modified disk"
        binary = runtime / relative
        binary.parent.mkdir(parents=True)
        shutil.copyfile(source / relative, binary)
        binary.chmod(0o755)
        before = snapshot(root)
        discovered, = json.loads(run("plugins", "list").stdout)
        assert discovered["available"] and discovered["executor"] == "native"
        for operation in manifest["operations"]:
            args = ["26.0.0"] if operation in ("use", "remove") else []
            plan = json.loads(run("--dry-run", "plugin", "node", operation, *args).stdout)
            assert plan["executor"] == "native" and plan["protocol"] == "jsonl-v1"
            assert plan["operation"] == operation and plan["dry_run"] is True
            assert plan["command"] == [str(binary)]
        assert snapshot(root) == before, "dry-run modified disk"
    print("PASS source: native metadata, missing-binary refusal, no legacy fallback, eight no-write dry-run routes")


def verify(directory, selected=()):
    targets = (
        ("linux-amd64", ".tar.gz", 62),
        ("linux-arm64", ".tar.gz", 183),
        ("windows-amd64", ".zip", 0x8664),
        ("darwin-amd64", ".tar.gz", 0x01000007),
        ("darwin-arm64", ".tar.gz", 0x0100000C),
    )
    if set(selected) - {target[0] for target in targets}:
        raise ValueError("unsupported package target")
    for platform, extension, machine in targets:
        if selected and platform not in selected:
            continue
        root = "atm-0.0.2-" + platform
        path = directory / (root + extension)
        expected = (directory / (path.name + ".sha256")).read_text().split()[0]
        assert hashlib.sha256(path.read_bytes()).hexdigest() == expected
        if extension == ".zip":
            with zipfile.ZipFile(path) as archive:
                assert len(archive.namelist()) == len(set(archive.namelist()))
                entries = {name: archive.read(name) for name in archive.namelist()}
                modes = {info.filename: info.external_attr >> 16 for info in archive.infolist()}
        else:
            with tarfile.open(path) as archive:
                assert all(member.isfile() for member in archive.getmembers())
                assert len(archive.getmembers()) == len({member.name for member in archive.getmembers()})
                entries = {member.name: archive.extractfile(member).read() for member in archive.getmembers()}
                modes = {member.name: member.mode for member in archive.getmembers()}
        for name in entries:
            parts = PurePosixPath(name).parts
            assert parts[0] == root and ".." not in parts
            assert not any(p in ("internal-docs", ".git", ".config", ".codex", ".agents", "__pycache__", "tests") for p in parts)
            assert not name.endswith(".go")
            assert not name.startswith(root + "/plugins/node/native/")
        manifest = json.loads(entries[root + "/release.json"])
        assert manifest["version"] == "0.0.2" and manifest["migration"] is True
        assert manifest["os"] + "-" + manifest["arch"] == platform
        assert set(entries) == {root + "/" + name for name in manifest["files"]} | {root + "/release.json"}
        for name, digest in manifest["files"].items():
            assert hashlib.sha256(entries[root + "/" + name]).hexdigest() == digest
        node = node_manifest(entries[root + "/plugins/node/plugin.json"])
        node_files = {name for name in entries if name.startswith(root + "/plugins/node/bin/")}
        if platform.startswith("linux"):
            entry = root + "/plugins/node/" + node["executables"][platform.replace("-", "/")]
            assert node_files == {entry}, "missing or foreign Node executable"
            native = entries[entry]
            assert native[:6] == b"\x7fELF\x02\x01"
            assert struct.unpack_from("<H", native, 18)[0] == machine
            assert modes[entry] & 0o111, "Node executable permission missing"
        else:
            assert not node_files, "unsupported Node backend must remain unavailable"
        exe = entries[root + ("/atm.exe" if platform.startswith("windows") else "/atm")]
        if platform.startswith("windows"):
            assert exe[:2] == b"MZ"
            pe = struct.unpack_from("<I", exe, 0x3C)[0]
            assert exe[pe:pe + 4] == b"PE\x00\x00"
            assert struct.unpack_from("<H", exe, pe + 4)[0] == machine
        elif platform.startswith("darwin"):
            assert exe[:4] == b"\xcf\xfa\xed\xfe"
            assert struct.unpack_from("<I", exe, 4)[0] == machine
            assert struct.unpack_from("<I", exe, 12)[0] == 2
        else:
            assert exe[:4] == b"\x7fELF"
            assert struct.unpack_from("<H", exe, 18)[0] == machine
        if platform == "linux-amd64" and sys.platform.startswith("linux") and host_platform.machine() == "x86_64":
            with tempfile.TemporaryDirectory() as temp:
                with tarfile.open(path) as archive:
                    archive.extractall(temp, filter="data")
                executable = Path(temp) / root / "atm"
                env = isolated_environment(Path(temp))
                result = subprocess.run([str(executable), "--version"], env=env, text=True, capture_output=True, check=True, timeout=20)
                assert "atm 0.0.2" in result.stdout
                result = subprocess.run([str(executable), "--json", "plugins", "list"], env=env, text=True, capture_output=True, check=True, timeout=20)
                plugins = json.loads(result.stdout)
                assert len(plugins) == 16
                node, = [item for item in plugins if item["id"] == "node"]
                assert node["available"] and node["executor"] == "native"
        print(f"PASS {platform}: archive checksum, payload hashes, runtime-only paths and executable architecture")


if __name__ == "__main__":
    if sys.argv[1] == "--source":
        verify_source(Path(sys.argv[2]))
    else:
        verify(Path(sys.argv[1]), sys.argv[2:])

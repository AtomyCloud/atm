"""Verify local migration packages without executing foreign-platform binaries."""
import ast
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform as host_platform
import re
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile


TARGET_MACHINES = {
    "linux/amd64": 62, "linux/arm64": 183, "windows/amd64": 0x8664,
    "darwin/amd64": 0x01000007, "darwin/arm64": 0x0100000C,
}
ROUTE_ARGS = {
    "menu": [], "status": [], "install": [], "update": [],
    "use": ["1.2.3"], "remove": ["1.2.3"], "uninstall": [], "integrate": [],
}


def native_manifest(data, plugin_id):
    manifest = json.loads(data)
    assert manifest["schema"] == manifest["min_core_api"] == 2
    assert manifest["id"] == plugin_id and manifest["version"] == "0.0.1"
    assert re.fullmatch(r"[a-z][a-z0-9_]{0,63}", plugin_id)
    assert manifest["executor"] == "native" and manifest["protocol"] == "jsonl-v1"
    assert manifest["operations"] and len(set(manifest["operations"])) == len(manifest["operations"])
    assert set(manifest["operations"]) <= ROUTE_ARGS.keys(), "add per-operation dry-run arguments"
    assert manifest["executables"]
    assert len(set(manifest["executables"].values())) == len(manifest["executables"])
    assert {target.split("/")[0] for target in manifest["executables"]} == set(manifest["platforms"])
    for target, relative in manifest["executables"].items():
        path = PurePosixPath(relative)
        assert target in TARGET_MACHINES
        assert not path.is_absolute() and ".." not in path.parts and "\\" not in relative
        assert path.as_posix() == relative and path.parts[0] == "bin" and len(path.parts) > 1
        if target.startswith("windows/"):
            assert relative.lower().endswith(".exe")
    return manifest


def node_manifest(data):
    """Compatibility helper for callers of the original Node checker."""
    return native_manifest(data, "node")


def source_manifests(source):
    manifests = {}
    for path in sorted((source / "plugins").glob("*/plugin.json")):
        data = json.loads(path.read_bytes())
        if data.get("schema") == 2 or data.get("executor") == "native":
            manifest = native_manifest(path.read_bytes(), path.parent.name)
            assert (path.parent / "native").is_dir(), f"missing native source: {path.parent}"
            manifests[manifest["id"]] = manifest
    return manifests


def check_architecture(data, target):
    machine = TARGET_MACHINES[target]
    if target.startswith("windows/"):
        assert data[:2] == b"MZ"
        pe = struct.unpack_from("<I", data, 0x3C)[0]
        assert data[pe:pe + 4] == b"PE\x00\x00"
        assert struct.unpack_from("<H", data, pe + 4)[0] == machine
    elif target.startswith("darwin/"):
        assert data[:4] == b"\xcf\xfa\xed\xfe"
        assert struct.unpack_from("<I", data, 4)[0] == machine
        assert struct.unpack_from("<I", data, 12)[0] == 2
    else:
        assert data[:6] == b"\x7fELF\x02\x01"
        assert struct.unpack_from("<H", data, 18)[0] == machine


def host_target():
    arch = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(host_platform.machine())
    system = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}.get(host_platform.system())
    return f"{system}/{arch}"


def isolated_environment(root):
    env = {key: value for key, value in os.environ.items() if not key.startswith("ATM_")}
    env.update(HOME=str(root / "user"), XDG_CONFIG_HOME=str(root / "config"),
               XDG_DATA_HOME=str(root / "data"), XDG_CACHE_HOME=str(root / "cache"),
               ATM_CONFIG_DIR=str(root / "config"), ATM_STATE_DIR=str(root / "state"),
               ATM_APPS_DIR=str(root / "apps"), ATM_LANG="en-us", PATH="",
               ATM_NODE_SYSTEM_BINARY=str(root / "absent-system-node"),
               JAVA_HOME=str(root / "absent-java"), ANDROID_HOME=str(root / "absent-sdk"),
               ANDROID_SDK_ROOT=str(root / "absent-sdk"), ANDROID_USER_HOME=str(root / "android-user"))
    return env


def snapshot(root):
    entries = {}
    for path in root.rglob("*"):
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode):
            identity = os.readlink(path)
        elif stat.S_ISREG(mode):
            identity = hashlib.sha256(path.read_bytes()).hexdigest()
        else:
            identity = None
        entries[str(path.relative_to(root))] = (mode, identity)
    return entries


def seed_user_state(root):
    """Detect changes to existing user state as well as unexpected new files."""
    user = root / "user"
    user.mkdir()
    for name in (".bashrc", ".zshrc", ".profile"):
        path = user / name
        path.write_text("# Existing user configuration\n", encoding="utf-8")
        path.chmod(0o600)
    saved = root / "apps" / "unrelated-application" / "data"
    saved.mkdir(parents=True)
    (saved / "settings.json").write_text('{"preserve": true}\n', encoding="utf-8")
    (user / "unavailable-volume").symlink_to(root / "absent-volume", target_is_directory=True)


def verify_read_only_trace(trace, executable, backend):
    assert "connect(" not in trace, "read-only backend attempted a network connection"
    assert "execveat(" not in trace, "unexpected descriptor-based execution"
    executions = []
    for line in trace.splitlines():
        if "execve(" not in line:
            continue
        match = re.search(r'execve\(("(?:[^"\\]|\\.)*")', line)
        assert match, f"unrecognized execution trace: {line}"
        # strace uses C octal escapes for non-ASCII filesystem bytes, not JSON.
        executions.append(os.fsdecode(ast.literal_eval("b" + match[1])))
    assert executions == [str(executable), str(backend)], executions


def read_only_runner(command, env, backend):
    """Opt-in Linux syscall gate; trace output lives outside the observed runtime."""
    trace_enabled = os.environ.get("ATM_TEST_NATIVE_TRACE") == "1"
    tracer = shutil.which("strace") if trace_enabled else None
    if trace_enabled and not tracer:
        raise RuntimeError("ATM_TEST_NATIVE_TRACE=1 requires strace")

    def run(*args):
        with tempfile.TemporaryDirectory(prefix="atm-native-trace-") as directory:
            trace = Path(directory) / "calls"
            invocation = command + list(args)
            if tracer:
                invocation = [tracer, "-f", "-qq", "-s", "4096", "-e",
                              "trace=execve,execveat,connect", "-o", str(trace), *invocation]
            result = subprocess.run(invocation, env=env, text=True, capture_output=True,
                                    check=True, timeout=20)
            if tracer:
                verify_read_only_trace(trace.read_text(), command[0], backend)
            return result
    return run


def verify_read_only_backend(run, manifest, root):
    """Run only locally compiled menu/status, never downloaded applications."""
    before = snapshot(root)
    for operation in ("menu", "status"):
        if operation not in manifest["operations"]:
            continue
        result = run("--yes", "plugin", manifest["id"], operation)
        events = [json.loads(line) for line in result.stdout.splitlines()]
        assert events and events[0]["type"] == "started"
        terminal, = [event for event in events if event["type"] == "result"]
        assert terminal["status"] == "success", terminal
        if operation == "menu":
            assert terminal.get("choices"), "root menu returned no choices"
            assert all(choice["operation"] in manifest["operations"] for choice in terminal["choices"])
        assert snapshot(root) == before, f"{manifest['id']} {operation} modified isolated runtime/home"


def verify_source(executable):
    """Isolated dry-run routes and read-only compiled menu/status checks."""
    source = Path(__file__).resolve().parents[1]
    for manifest in source_manifests(source).values():
        verify_native_source(executable, source, manifest)


def verify_native_source(executable, source, manifest):
    plugin_id = manifest["id"]
    target = host_target()
    assert target in TARGET_MACHINES, "unsupported test host"
    with tempfile.TemporaryDirectory(prefix="atm-native-source-") as temp:
        root = Path(temp)
        runtime = root / "runtime"
        plugin = runtime / "plugins" / plugin_id
        plugin.mkdir(parents=True)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins" / plugin_id / "plugin.json", plugin / "plugin.json")
        # A legacy entrypoint is present but must never be selected as fallback.
        marker = root / "legacy-executed"
        (plugin / "plugin.sh").write_text(f'#!/bin/sh\nprintf fallback > "{marker}"\nexit 99\n')
        (plugin / "plugin.sh").chmod(0o755)
        env = isolated_environment(root)
        seed_user_state(root)
        command = [str(executable.resolve()), "--home", str(runtime), "--json"]

        def run(*args, check=True):
            return subprocess.run(command + list(args), env=env, text=True,
                                  capture_output=True, check=check, timeout=20)

        before = snapshot(root)
        discovered, = json.loads(run("plugins", "list").stdout)
        assert discovered["executor"] == "native" and not discovered["available"]
        for operation in manifest["operations"]:
            assert run("--dry-run", "plugin", plugin_id, operation,
                       *ROUTE_ARGS[operation], check=False).returncode != 0
        if "status" in manifest["operations"]:
            assert run("status", plugin_id, check=False).returncode != 0
        assert snapshot(root) == before, "missing-binary paths modified disk"
        if target not in manifest["executables"]:
            print(f"PASS source {plugin_id}: unavailable on {target}, no fallback/writes")
            return
        relative = Path("plugins") / plugin_id / manifest["executables"][target]
        binary = runtime / relative
        binary.parent.mkdir(parents=True)
        shutil.copyfile(source / relative, binary)
        check_architecture(binary.read_bytes(), target)
        digest = (source / (str(relative) + ".sha256")).read_text().split()[0]
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == digest
        binary.chmod(0o755)
        before = snapshot(root)
        discovered, = json.loads(run("plugins", "list").stdout)
        assert discovered["available"] and discovered["executor"] == "native"
        for operation in manifest["operations"]:
            plan = json.loads(run("--dry-run", "plugin", plugin_id, operation, *ROUTE_ARGS[operation]).stdout)
            assert plan["executor"] == "native" and plan["protocol"] == "jsonl-v1"
            assert plan["operation"] == operation and plan["dry_run"] is True
            assert plan["command"] == [str(binary)]
        assert snapshot(root) == before, "dry-run modified disk"
        verify_read_only_backend(read_only_runner(command, env, binary), manifest, root)
    print(f"PASS source {plugin_id}: native metadata, missing-binary refusal, no fallback, {len(manifest['operations'])} no-write routes")


def verify(directory, selected=()):
    source = Path(__file__).resolve().parents[1]
    natives = source_manifests(source)
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
            assert not any(p.startswith(".") for p in parts)
        manifest = json.loads(entries[root + "/release.json"])
        assert manifest["version"] == "0.0.2" and manifest["migration"] is True
        assert manifest["os"] + "-" + manifest["arch"] == platform
        assert set(entries) == {root + "/" + name for name in manifest["files"]} | {root + "/release.json"}
        for name, digest in manifest["files"].items():
            assert hashlib.sha256(entries[root + "/" + name]).hexdigest() == digest
        target = platform.replace("-", "/")
        packaged = {}
        for name, data in entries.items():
            parts = PurePosixPath(name).parts
            if len(parts) == 4 and parts[1] == "plugins" and parts[3] == "plugin.json":
                declared = json.loads(data)
                if declared.get("schema") == 2 or declared.get("executor") == "native":
                    packaged[parts[2]] = native_manifest(data, parts[2])
        assert packaged == natives, "native manifests differ from source"
        for plugin_id, native in packaged.items():
            prefix = root + "/plugins/" + plugin_id + "/"
            assert not any(name.startswith(prefix + "native/") for name in entries)
            binaries = {name for name in entries if name.startswith(prefix + "bin/")}
            relative = native["executables"].get(target)
            expected = {prefix + relative} if relative else set()
            assert binaries == expected, f"{plugin_id}: missing, stale or foreign native executable"
            if relative:
                check_architecture(entries[prefix + relative], target)
                assert modes[prefix + relative] & 0o111, "native executable permission missing"
        exe = entries[root + ("/atm.exe" if platform.startswith("windows") else "/atm")]
        check_architecture(exe, target)
        if target == host_target() and extension == ".tar.gz":
            with tempfile.TemporaryDirectory() as temp:
                with tarfile.open(path) as archive:
                    archive.extractall(temp, filter="data")
                executable = Path(temp) / root / "atm"
                env = isolated_environment(Path(temp))
                seed_user_state(Path(temp))
                result = subprocess.run([str(executable), "--version"], env=env, text=True, capture_output=True, check=True, timeout=20)
                assert "atm 0.0.2" in result.stdout
                result = subprocess.run([str(executable), "--json", "plugins", "list"], env=env, text=True, capture_output=True, check=True, timeout=20)
                plugins = json.loads(result.stdout)
                for plugin_id, native in natives.items():
                    item, = [item for item in plugins if item["id"] == plugin_id]
                    assert item["executor"] == "native"
                    assert item["available"] == (target in native["executables"])
                    if not item["available"]:
                        continue

                    backend = Path(temp) / root / "plugins" / plugin_id / native["executables"][target]
                    run = read_only_runner([str(executable), "--json"], env, backend)
                    verify_read_only_backend(run, native, Path(temp))
        print(f"PASS {platform}: archive checksum, payload hashes, runtime-only paths and executable architecture")


if __name__ == "__main__":
    if sys.argv[1] == "--source":
        verify_source(Path(sys.argv[2]))
    else:
        verify(Path(sys.argv[1]), sys.argv[2:])

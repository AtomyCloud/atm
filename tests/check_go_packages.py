"""Verify local migration packages without executing foreign-platform binaries."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile


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
                entries = {name: archive.read(name) for name in archive.namelist()}
        else:
            with tarfile.open(path) as archive:
                assert all(member.isfile() for member in archive.getmembers())
                entries = {member.name: archive.extractfile(member).read() for member in archive.getmembers()}
        for name in entries:
            parts = PurePosixPath(name).parts
            assert parts[0] == root and ".." not in parts
            assert not any(p in ("internal-docs", ".git", ".config", ".codex", "__pycache__") for p in parts)
        manifest = json.loads(entries[root + "/release.json"])
        assert manifest["version"] == "0.0.2" and manifest["migration"] is True
        assert manifest["os"] + "-" + manifest["arch"] == platform
        assert set(entries) == {root + "/" + name for name in manifest["files"]} | {root + "/release.json"}
        for name, digest in manifest["files"].items():
            assert hashlib.sha256(entries[root + "/" + name]).hexdigest() == digest
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
        if platform == "linux-amd64" and sys.platform.startswith("linux"):
            with tempfile.TemporaryDirectory() as temp:
                with tarfile.open(path) as archive:
                    archive.extractall(temp, filter="data")
                executable = Path(temp) / root / "atm"
                result = subprocess.run([str(executable), "--version"], text=True, capture_output=True, check=True)
                assert "atm 0.0.2" in result.stdout
                result = subprocess.run([str(executable), "--json", "plugins", "list"], text=True, capture_output=True, check=True)
                assert len(json.loads(result.stdout)) == 16
        print(f"PASS {platform}: archive checksum, payload hashes, runtime-only paths and executable architecture")


if __name__ == "__main__":
    verify(Path(sys.argv[1]), sys.argv[2:])

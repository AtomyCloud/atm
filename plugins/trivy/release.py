#!/usr/bin/env python3
"""Resolve official Trivy assets and verify/extract only the binary."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import sys
import tarfile
import tempfile


VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+\Z")
BASE = "https://github.com/aquasecurity/trivy/releases/download/"


def resolve(release, platform):
    version = str(release.get("tag_name", "")).removeprefix("v")
    if not VERSION.fullmatch(version) or release.get("draft") is not False or release.get("prerelease") is not False:
        raise ValueError("Expected an official stable Trivy release")
    if platform == "docker":
        return version
    if platform not in ("Linux-64bit", "Linux-32bit", "Linux-ARM64", "Linux-ARM", "Linux-PPC64LE", "Linux-s390x"):
        raise ValueError("Unsupported Trivy platform")
    names = [f"trivy_{version}_{platform}.tar.gz", f"trivy_{version}_checksums.txt"]
    entries = release.get("assets")
    if not isinstance(entries, list) or any(not isinstance(asset, dict) for asset in entries):
        raise ValueError("Invalid Trivy release asset list")
    urls = []
    for name in names:
        assets = [asset for asset in entries if asset.get("name") == name]
        expected = BASE + "v" + version + "/" + name
        if len(assets) != 1 or assets[0].get("browser_download_url") != expected:
            raise ValueError("Official release asset missing or URL differs from the expected source: " + name)
        urls.append(expected)
    return "\t".join((version, *urls))


def install(archive, checksums, destination):
    archive, checksums, destination = Path(archive), Path(checksums), Path(destination)
    if os.path.lexists(destination):
        raise ValueError("Existing Trivy version will not be overwritten")
    expected = []
    for line in checksums.read_text(encoding="utf-8").splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[1].lstrip("*") == archive.name:
            if not re.fullmatch(r"[a-fA-F0-9]{64}", fields[0]):
                raise ValueError("Invalid SHA-256 checksum")
            expected.append(fields[0].lower())
    if len(expected) != 1:
        raise ValueError("Missing or duplicate checksum for this Trivy archive")
    digest = hashlib.sha256()
    with archive.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    if digest.hexdigest() != expected[0]:
        raise ValueError("Trivy archive SHA-256 mismatch")
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "r:gz") as package:
        binaries = []
        for member in package:
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts or not path.parts:
                raise ValueError("Unsafe Trivy archive path")
            if not (member.isfile() or member.isdir()):
                raise ValueError("Archive links and special files are not supported")
            if str(path) == "trivy":
                if not member.isfile() or not 0 < member.size <= 256 * 1024 * 1024:
                    raise ValueError("Invalid Trivy binary")
                binaries.append(member)
        if len(binaries) != 1:
            raise ValueError("Expected exactly one Trivy binary at the archive root")
        with tempfile.TemporaryDirectory(prefix=".atm-trivy-", dir=destination.parent) as scratch:
            stage = Path(scratch)
            (stage / "bin").mkdir()
            with package.extractfile(binaries[0]) as source, (stage / "bin/trivy").open("xb") as output:
                shutil.copyfileobj(source, output)
            (stage / "bin/trivy").chmod(0o755)
            (stage / ".atm-trivy-managed").write_text(expected[0] + "\n", encoding="ascii")
            if os.path.lexists(destination):
                raise ValueError("Existing Trivy version will not be overwritten")
            stage.rename(destination)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest="action", required=True)
    resolver = actions.add_parser("resolve")
    resolver.add_argument("platform")
    installer = actions.add_parser("install")
    installer.add_argument("archive")
    installer.add_argument("checksums")
    installer.add_argument("destination")
    args = parser.parse_args()
    if args.action == "resolve":
        data = json.load(sys.stdin)
        if not isinstance(data, dict):
            raise ValueError("Invalid GitHub release response")
        print(resolve(data, args.platform))
    else:
        install(args.archive, args.checksums, args.destination)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, EOFError, tarfile.TarError) as error:
        raise SystemExit(str(error))

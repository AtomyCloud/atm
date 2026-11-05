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

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "lib"))
from atm_plugin_i18n import translator

tr = translator(__file__)


VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+\Z")
BASE = "https://github.com/aquasecurity/trivy/releases/download/"


def resolve(release, platform):
    version = str(release.get("tag_name", "")).removeprefix("v")
    if not VERSION.fullmatch(version) or release.get("draft") is not False or release.get("prerelease") is not False:
        raise ValueError(tr("ATM_PLUGIN_TRIVY_EXPECTED_STABLE"))
    if platform == "docker":
        return version
    if platform not in ("Linux-64bit", "Linux-32bit", "Linux-ARM64", "Linux-ARM", "Linux-PPC64LE", "Linux-s390x"):
        raise ValueError(tr("ATM_PLUGIN_TRIVY_UNSUPPORTED_PLATFORM"))
    names = [f"trivy_{version}_{platform}.tar.gz", f"trivy_{version}_checksums.txt"]
    entries = release.get("assets")
    if not isinstance(entries, list) or any(not isinstance(asset, dict) for asset in entries):
        raise ValueError(tr("ATM_PLUGIN_TRIVY_INVALID_ASSETS"))
    urls = []
    for name in names:
        assets = [asset for asset in entries if asset.get("name") == name]
        expected = BASE + "v" + version + "/" + name
        if len(assets) != 1 or assets[0].get("browser_download_url") != expected:
            raise ValueError(tr("ATM_PLUGIN_TRIVY_ASSET_SOURCE_MISMATCH", name=name))
        urls.append(expected)
    return "\t".join((version, *urls))


def install(archive, checksums, destination):
    archive, checksums, destination = Path(archive), Path(checksums), Path(destination)
    if os.path.lexists(destination):
        raise ValueError(tr("ATM_PLUGIN_TRIVY_NO_OVERWRITE"))
    expected = []
    for line in checksums.read_text(encoding="utf-8").splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[1].lstrip("*") == archive.name:
            if not re.fullmatch(r"[a-fA-F0-9]{64}", fields[0]):
                raise ValueError(tr("ATM_PLUGIN_TRIVY_INVALID_CHECKSUM"))
            expected.append(fields[0].lower())
    if len(expected) != 1:
        raise ValueError(tr("ATM_PLUGIN_TRIVY_CHECKSUM_COUNT"))
    digest = hashlib.sha256()
    with archive.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    if digest.hexdigest() != expected[0]:
        raise ValueError(tr("ATM_PLUGIN_TRIVY_CHECKSUM_MISMATCH"))
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "r:gz") as package:
        binaries = []
        for member in package:
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts or not path.parts:
                raise ValueError(tr("ATM_PLUGIN_TRIVY_UNSAFE_PATH"))
            if not (member.isfile() or member.isdir()):
                raise ValueError(tr("ATM_PLUGIN_TRIVY_UNSUPPORTED_MEMBERS"))
            if str(path) == "trivy":
                if not member.isfile() or not 0 < member.size <= 256 * 1024 * 1024:
                    raise ValueError(tr("ATM_PLUGIN_TRIVY_INVALID_BINARY"))
                binaries.append(member)
        if len(binaries) != 1:
            raise ValueError(tr("ATM_PLUGIN_TRIVY_BINARY_COUNT"))
        with tempfile.TemporaryDirectory(prefix=".atm-trivy-", dir=destination.parent) as scratch:
            stage = Path(scratch)
            (stage / "bin").mkdir()
            with package.extractfile(binaries[0]) as source, (stage / "bin/trivy").open("xb") as output:
                shutil.copyfileobj(source, output)
            (stage / "bin/trivy").chmod(0o755)
            (stage / ".atm-trivy-managed").write_text(expected[0] + "\n", encoding="ascii")
            if os.path.lexists(destination):
                raise ValueError(tr("ATM_PLUGIN_TRIVY_NO_OVERWRITE"))
            stage.rename(destination)


def main():
    parser = argparse.ArgumentParser(description=tr("ATM_PLUGIN_TRIVY_RELEASE_HELP"))
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
            raise ValueError(tr("ATM_PLUGIN_TRIVY_INVALID_RESPONSE"))
        print(resolve(data, args.platform))
    else:
        install(args.archive, args.checksums, args.destination)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, EOFError, tarfile.TarError) as error:
        raise SystemExit(str(error))

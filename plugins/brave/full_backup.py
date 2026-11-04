#!/usr/bin/env python3
"""Read-only collection of Brave user data; no installation or restore actions."""
import argparse
import datetime
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tarfile
import tempfile


class BackupError(Exception):
    pass


def detect_sources(home, standard=None):
    candidates = [
        ("standard", standard or home / ".config/BraveSoftware/Brave-Browser"),
        ("flatpak", home / ".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser"),
        ("snap", home / "snap/brave/current/.config/BraveSoftware/Brave-Browser"),
    ]
    sources = []
    seen = set()
    for kind, location in candidates:
        try:
            source = location.resolve(strict=True)
        except FileNotFoundError:
            continue
        if not stat.S_ISDIR(source.stat().st_mode):
            raise BackupError(f"Not a profile directory: {location}")
        if source not in seen:
            sources.append((kind, source))
            seen.add(source)
    if not sources:
        raise BackupError("No Brave data directories found (standard, Flatpak or Snap).")
    return sources


def require_closed():
    # Anchor argv matching: a backup destination named "brave" is not a browser.
    patterns = [
        ("-x", "brave|brave-browser|brave-browser-s|brave-browser-b|brave-browser-n"),
        ("-f", r"^([^[:space:]]*/)?(brave|brave-browser|brave-browser-stable|brave-browser-beta|brave-browser-nightly)([[:space:]]|$)"),
    ]
    for flag, pattern in patterns:
        result = subprocess.run(["pgrep", "-u", str(os.getuid()), flag, pattern],
                                stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, check=False)
        if result.returncode == 0:
            raise BackupError("Close Brave Browser before creating a full backup.")
        if result.returncode != 1:
            raise BackupError("Unable to check running Brave processes; backup cancelled.")


def backup(home, destination, standard=None, dry_run=False):
    sources = detect_sources(home, standard)
    destination = destination.resolve()
    for kind, source in sources:
        if destination == source or source in destination.parents:
            raise BackupError(f"Backup destination cannot be inside a Brave profile: {source}")
        print(f"Source [{kind}]: {source}", flush=True)
    print(f"Backup directory: {destination}", flush=True)
    if dry_run:
        print("DRY-RUN: create brave-<date>_<time>-<id>.tar.gz; no files written.")
        return None

    require_closed()
    timestamp = datetime.datetime.now().astimezone()
    destination.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=f".brave-{timestamp:%Y-%m-%d_%H-%M-%S}-",
                                suffix=".tar.gz.part", dir=destination)
    temporary = Path(name)
    final = destination / temporary.name[1:-5]
    try:
        with os.fdopen(fd, "wb") as handle:
            with tarfile.open(fileobj=handle, mode="w:gz", compresslevel=6, dereference=False) as archive:
                records = []
                for kind, source in sources:
                    member = f"{kind}/Brave-Browser"
                    archive.add(source, arcname=member)
                    records.append({"installation": kind, "source": str(source), "archive_path": member})
                manifest = json.dumps({"schema": 1, "app": "brave", "created_at": timestamp.isoformat(),
                                       "sources": records}, indent=2).encode("utf-8")
                info = tarfile.TarInfo("backup.json")
                info.size = len(manifest)
                info.mode = 0o600
                archive.addfile(info, io.BytesIO(manifest))
            handle.flush()
            os.fsync(handle.fileno())
        require_closed()
        # Hard-link publication is atomic and fails rather than replacing any target.
        os.link(temporary, final)
    finally:
        temporary.unlink(missing_ok=True)
    print(f"Archive: {final}", flush=True)
    return final


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--home", required=True, type=Path)
    parser.add_argument("--backups", required=True, type=Path)
    parser.add_argument("--standard-profile", type=Path)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    try:
        backup(args.home, args.backups, args.standard_profile, args.dry_run)
    except (BackupError, OSError, RuntimeError, tarfile.TarError) as exc:
        print(f"Brave full backup: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

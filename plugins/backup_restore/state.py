#!/usr/bin/env python3
"""Local atool-style snapshots; no shell execution or system installation."""

import argparse
import datetime
import fnmatch
import getpass
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import signal
import socket
import stat
import sys
import tempfile
import uuid


MAIN_FOLDERS = ("Desktop", "Documents", "Downloads", "Pictures", "Videos", "Music")
MAX_METADATA_SIZE = 64 * 1024 * 1024
PROTECTED = ("/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/boot",
             "/dev", "/proc", "/sys", "/run", "/var")


def inside(path, parent):
    return path == parent or parent in path.parents


def absolute(value):
    path = Path(value).expanduser()
    if not path.is_absolute():
        raise ValueError("Use an absolute path.")
    if ".." in path.parts:
        raise ValueError("Parent traversal is not allowed.")
    return Path("/", *path.parts[1:])


def no_links(path):
    for part in (path, *path.parents):
        if part.is_symlink():
            raise ValueError(f"Symlinked path component is not allowed: {part}")


def relative(value):
    path = PurePosixPath(value)
    if not value or path.is_absolute() or any(p in (".", "..", "") for p in value.split("/")):
        raise ValueError(f"Invalid relative path: {value!r}")
    return Path(*path.parts)


def username(value):
    if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]*\$?", value):
        raise ValueError("Invalid backup user name.")
    return value


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d_%H%M%S")


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def inventory(root):
    result = {}

    def visit(directory):
        for item in sorted(directory.iterdir()):
            key = item.relative_to(root).as_posix()
            mode = item.lstat().st_mode
            if stat.S_ISLNK(mode):
                result[key] = ["link", os.readlink(item)]
            elif stat.S_ISDIR(mode):
                result[key] = ["directory"]
                visit(item)
            elif stat.S_ISREG(mode):
                result[key] = ["file", digest(item)]
            else:
                raise ValueError(f"Unsupported special file in backup: {key}")

    visit(root)
    return result


def copy_entry(source, target, home, excluded, patterns, skipped):
    rel = source.relative_to(home).as_posix()
    if any(inside(source, item) for item in excluded):
        return
    if any(fnmatch.fnmatchcase(rel, p) or fnmatch.fnmatchcase(source.name, p) for p in patterns):
        return
    mode = source.lstat().st_mode
    if stat.S_ISLNK(mode):
        target.symlink_to(os.readlink(source))
    elif stat.S_ISDIR(mode):
        target.mkdir(mode=0o700)
        for item in sorted(source.iterdir()):
            copy_entry(item, target / item.name, home, excluded, patterns, skipped)
        shutil.copystat(source, target, follow_symlinks=False)
        target.chmod(stat.S_IMODE(mode) & 0o777)
    elif stat.S_ISREG(mode):
        shutil.copy2(source, target, follow_symlinks=False)
        target.chmod(stat.S_IMODE(mode) & 0o777)
    else:
        skipped.append(rel)


def backup_plan(home, destination, kind, paths):
    home = absolute(home).resolve(strict=True)
    destination = absolute(destination)
    no_links(destination)
    if not home.is_dir() or inside(home, destination):
        raise ValueError("Backup destination cannot be the source home or its ancestor.")
    if destination.exists() and not destination.is_dir():
        raise ValueError("Backup destination must be a directory.")
    if kind in ("safe", "full"):
        selected = sorted(home.iterdir())
    elif kind == "main":
        selected = [home / name for name in MAIN_FOLDERS if (home / name).is_dir()]
    elif kind == "custom":
        selected = sorted({home / relative(p) for p in paths})
        if not selected:
            raise ValueError("Custom backups require at least one relative path.")
    else:
        raise ValueError("Unknown backup type.")
    for item in selected:
        no_links(item.parent)
        if not item.exists() and not item.is_symlink():
            raise ValueError(f"Selected path does not exist: {item}")
        if kind == "custom" and inside(item, destination):
            raise ValueError("A selected path is inside the backup destination.")
    selected = [p for p in selected if not inside(p, destination)
                and not any(other in p.parents for other in selected)]
    if not selected:
        raise ValueError("No matching folders found.")
    return home, destination, selected


def create_backup(home, destination, kind="safe", paths=(), excludes=None, user=None, dry_run=False):
    home, destination, selected = backup_plan(home, destination, kind, paths)
    user = username(user or getpass.getuser())
    patterns = []
    if kind == "safe" and excludes:
        patterns = [line.strip().rstrip("/") for line in Path(excludes).read_text().splitlines()
                    if line.strip() and not line.lstrip().startswith("#")]
    if dry_run:
        return {"dry_run": True, "type": kind, "home": str(home), "destination": str(destination),
                "selected": [str(p) for p in selected], "excludes": patterns}
    host = re.sub(r"[^A-Za-z0-9_.-]", "_", socket.gethostname()) or "localhost"
    parent = destination / "atool-backups" / host
    parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".pending-", dir=parent))
    final = parent / f"{stamp()}-{uuid.uuid4().hex[:12]}"
    try:
        data = stage / "users" / user
        data.mkdir(mode=0o700, parents=True)
        skipped = []
        for source in selected:
            target = data / source.relative_to(home)
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            copy_entry(source, target, home, [destination], patterns, skipped)
        records = inventory(data)
        if not records:
            raise ValueError("No files or directories remain after exclusions.")
        metadata = {"schema_version": "1.0", "app_name": "ATM - Backup & Restore",
                    "app_version": "0.0.1", "action": "backup", "status": "success",
                    "dry_run": False, "hostname": host, "backup_date": stamp(),
                    "users": [user], "successful_users": [user], "failed_users": [],
                    "type": kind, "source_home": str(home), "custom_paths": list(paths),
                    "atm_format": 1, "inventory": records, "skipped_special_files": skipped}
        encoded = (json.dumps(metadata, indent=2) + "\n").encode("utf-8")
        if len(encoded) > MAX_METADATA_SIZE:
            raise ValueError("Backup metadata exceeds the 64 MiB limit; select fewer paths.")
        (stage / "atool-backup.json").write_bytes(encoded)
        (stage / "atool-backup.json").chmod(0o600)
        logs = stage / "logs"
        logs.mkdir(mode=0o700)
        (logs / "summary.txt").write_text(f"success\nEntries: {len(records)}\nSkipped special files: {len(skipped)}\n")
        stage.rename(final)
        return {"backup": str(final), "entries": len(records), "skipped_special_files": skipped}
    finally:
        if stage.exists():
            shutil.rmtree(stage)


def load_backup(root, user=None, verify=True):
    root = absolute(root)
    no_links(root)
    meta = root / "atool-backup.json"
    if not meta.is_file() or meta.is_symlink() or meta.stat().st_size > MAX_METADATA_SIZE:
        raise ValueError("Missing or invalid atool-backup.json.")
    obj = json.loads(meta.read_text())
    if not isinstance(obj, dict) or obj.get("schema_version") != "1.0" or obj.get("status") != "success" or obj.get("dry_run") is not False:
        raise ValueError("Only successful, real schema 1.0 backups can be restored.")
    users = obj.get("users")
    if not isinstance(users, list) or not users or not all(isinstance(u, str) for u in users):
        raise ValueError("Invalid backup users.")
    for item in users:
        username(item)
    successful = obj.get("successful_users")
    if (not isinstance(successful, list) or successful != users
            or obj.get("failed_users") != [] or len(set(users)) != len(users)):
        raise ValueError("Backup user status is inconsistent.")
    if user is None:
        if len(users) != 1:
            raise ValueError("Multi-user atool backup: specify --user explicitly.")
        user = users[0]
    if user not in users:
        raise ValueError("Selected user was not successfully backed up.")
    data = root / "users" / username(user)
    no_links(data)
    if not data.is_dir():
        raise ValueError("Backup user directory is missing.")
    if "atm_format" in obj:
        if type(obj["atm_format"]) is not int or obj["atm_format"] != 1 or len(users) != 1 or not isinstance(obj.get("inventory"), dict):
            raise ValueError("Unsupported ATM backup format.")
        if verify and inventory(data) != obj["inventory"]:
            raise ValueError("Backup integrity check failed. No files replaced.")
    elif verify:
        inventory(data)
    return root, data, obj


def restore_plan(root, target, home, user=None):
    root, data, obj = load_backup(root, user)
    target = absolute(target)
    home = absolute(home).resolve(strict=True)
    no_links(target)
    protected = [Path(p) for p in PROTECTED]
    if (len(target.parts) < 3 or target == home or inside(home, target)
            or any(inside(target, p) for p in protected)
            or inside(target, root) or inside(root, target)
            or inside(Path(__file__).resolve().parents[2], target)
            or inside(target, Path(__file__).resolve().parents[2])):
        raise ValueError("Unsafe or overlapping restore target. Choose a separate user directory.")
    if target.exists() and not target.is_dir():
        raise ValueError("Restore target must be a directory.")
    return root, data, target, obj


def overlay(source, target):
    for item in sorted(source.iterdir()):
        dest = target / item.name
        if item.is_dir() and not item.is_symlink() and dest.is_dir() and not dest.is_symlink():
            overlay(item, dest)
            shutil.copystat(item, dest, follow_symlinks=False)
            dest.chmod(stat.S_IMODE(dest.stat().st_mode) & 0o777)
            continue
        if dest.is_symlink() or dest.is_file():
            dest.unlink()
        elif dest.exists():
            shutil.rmtree(dest)
        if item.is_symlink():
            dest.symlink_to(os.readlink(item))
        elif item.is_dir():
            shutil.copytree(item, dest, symlinks=True)
        else:
            shutil.copy2(item, dest, follow_symlinks=False)


def restore_backup(root, target, home, user=None, dry_run=False):
    root, data, target, obj = restore_plan(root, target, home, user)
    result = {"source": str(root), "target": str(target),
              "integrity": "sha256" if "atm_format" in obj else "legacy: no stored checksums"}
    if dry_run:
        return dict(result, dry_run=True)
    # Stage the complete merge next to the target; retain the old tree as recovery.
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".atm-restore-", dir=target.parent))
    recovery = target.with_name(f"{target.name}.before-atm-restore-{stamp()}-{uuid.uuid4().hex[:12]}")
    moved = False
    try:
        if target.exists():
            inventory(target)  # Refuse sockets/devices instead of following them.
            overlay(target, stage)
        overlay(data, stage)
        # A mutable external backup must not change between verification and staging.
        if "atm_format" in obj and inventory(data) != obj["inventory"]:
            raise ValueError("Backup changed during restore.")
        staged_records = inventory(stage)
        source_records = obj["inventory"] if "atm_format" in obj else inventory(data)
        if any(staged_records.get(key) != value for key, value in source_records.items()):
            raise ValueError("Staged restore integrity check failed.")
        for key, value in staged_records.items():
            if value[0] != "link":
                item = stage / key
                item.chmod(stat.S_IMODE(item.stat().st_mode) & 0o777)
        previous_mask = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGTERM})
        try:
            if target.exists():
                target.rename(recovery)
                moved = True
            try:
                stage.rename(target)
            except BaseException:
                if moved:
                    try:
                        recovery.rename(target)
                    except OSError as error:
                        raise OSError(f"Rollback failed; original data retained at {recovery}") from error
                raise
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
        result["recovery"] = str(recovery) if moved else None
        return result
    finally:
        if stage.exists():
            shutil.rmtree(stage)


def list_backups(destination):
    base = absolute(destination) / "atool-backups"
    no_links(base)
    if not base.is_dir():
        return []
    result = []
    for host in sorted(base.iterdir()):
        if not host.is_dir() or host.is_symlink():
            continue
        for root in sorted(host.iterdir(), reverse=True):
            if root.name.startswith(".") or not root.is_dir() or root.is_symlink():
                continue
            try:
                load_backup(root, verify=False)
            except (ValueError, OSError, TypeError):
                continue
            result.append(str(root))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("backup", "restore", "list", "verify"))
    parser.add_argument("--home", default=str(Path.home()))
    parser.add_argument("--destination", required=True)
    parser.add_argument("--type", default="safe", choices=("safe", "full", "main", "custom"))
    parser.add_argument("--path", action="append", default=[])
    parser.add_argument("--source")
    parser.add_argument("--target")
    parser.add_argument("--user")
    parser.add_argument("--excludes")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if args.action == "backup":
            result = create_backup(args.home, args.destination, args.type, args.path,
                                   args.excludes, args.user, args.dry_run)
        elif args.action == "list":
            for path in list_backups(args.destination):
                print(path)
            return 0
        elif args.action == "verify":
            _, _, obj = load_backup(args.source, args.user)
            result = {"valid": True, "integrity": "sha256" if "atm_format" in obj else "legacy: no stored checksums"}
        else:
            if not args.source or not args.target:
                raise ValueError("Restore requires --source and --target.")
            result = restore_backup(args.source, args.target, args.home, args.user, args.dry_run)
        print(json.dumps(result, indent=2))
        return 0
    except (OSError, ValueError, TypeError, shutil.Error) as error:
        print(f"Backup/restore failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

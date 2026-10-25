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
import pwd
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


def discover_users(home_base, current_home):
    base = absolute(home_base)
    no_links(base)
    result = {}
    if base.is_dir():
        for item in sorted(base.iterdir()):
            try:
                name = username(item.name)
                if name in ("root", "nobody", "nogroup", "lost+found") or item.is_symlink() or not item.is_dir():
                    continue
                try:
                    account = pwd.getpwnam(name)
                except KeyError:
                    account = None
                if account and (account.pw_uid < 1000 or account.pw_uid >= 65534):
                    continue
                no_links(item)
                if not any(c in str(item) for c in "\t\n\r"):
                    result[name] = item
            except ValueError:
                continue
    account = pwd.getpwuid(os.getuid())
    home = absolute(current_home)
    if 1000 <= account.pw_uid < 65534 and home.is_dir() and not home.is_symlink():
        no_links(home)
        if account.pw_name in result or home not in result.values():
            result[username(account.pw_name)] = home
    return sorted(result.items())


def select_users(home_base, current_home, user=None, all_users=False):
    if user and all_users:
        raise ValueError("Choose --user or --all-users, not both.")
    if not user and not all_users:
        return [(username(pwd.getpwuid(os.getuid()).pw_name), absolute(current_home))]
    available = dict(discover_users(home_base, current_home))
    if all_users:
        if not available:
            raise ValueError("No user homes found.")
        return list(available.items())
    username(user)
    if user not in available:
        raise ValueError(f"User home not found or excluded as a system account: {user}")
    return [(user, available[user])]


def mounted_devices(media_root, mountinfo):
    media = absolute(media_root)
    no_links(media)
    result = []
    try:
        lines = Path(mountinfo).read_text().splitlines()
    except FileNotFoundError:
        return []
    for line in lines:
        parts = line.split()
        try:
            separator = parts.index("-")
            if separator < 6 or len(parts) < separator + 4 or not parts[0].isdigit():
                continue
            decode = lambda value: re.sub(r"\\(040|011|012|134)", lambda m: chr(int(m[1], 8)), value)
            target = absolute(decode(parts[4]))
            source = decode(parts[separator + 2])
            fstype = parts[separator + 1]
            if target.parent != media or any(ord(c) < 32 or ord(c) == 127 for c in str(target) + source):
                continue
            no_links(target)
            if not target.is_dir():
                continue
            free = shutil.disk_usage(target).free
            writable = "rw" in parts[5].split(",") and "ro" not in parts[separator + 3].split(",")
            result.append({"path": target, "source": source, "fstype": fstype,
                           "free": free, "id": parts[0], "writable": writable})
        except (ValueError, OSError):
            continue
    # Stacked mounts at one path are ambiguous; do not offer a hidden lower mount.
    counts = {}
    for item in result:
        counts[item["path"]] = counts.get(item["path"], 0) + 1
    return sorted((item for item in result if counts[item["path"]] == 1), key=lambda item: str(item["path"]))


def validate_device(destination, media_root, mountinfo, mount_id=None):
    destination = absolute(destination)
    media = absolute(media_root)
    if not inside(destination, media):
        if mount_id:
            raise ValueError("Selected device does not match the destination.")
        return None
    for device in mounted_devices(media, mountinfo):
        if inside(destination, device["path"]):
            if mount_id and mount_id != device["id"]:
                raise ValueError("Selected device mount changed. Choose it again.")
            if not device["writable"]:
                raise ValueError("Selected device is read-only.")
            return device["id"]
    raise ValueError("No mounted device at this destination. No local fallback will be created.")


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


def create_backup(home, destination, kind="safe", paths=(), excludes=None, user=None, dry_run=False,
                  sources=None, media_root=None, mountinfo="/proc/self/mountinfo", mount_id=None):
    sources = sources if sources is not None else [(user or getpass.getuser(), home)]
    if not sources or len({name for name, _ in sources}) != len(sources):
        raise ValueError("Backup requires distinct selected users.")
    plans = []
    for name, source_home in sources:
        username(name)
        try:
            source_home, destination, selected = backup_plan(source_home, destination, kind, paths)
        except PermissionError as error:
            raise PermissionError(f"Cannot read home for {name}. All selected homes must be readable; no users were skipped.") from error
        plans.append((name, source_home, selected))

    def check_device():
        if media_root is not None:
            return validate_device(destination, media_root, mountinfo, mount_id)
        return None

    mount_id = check_device()
    patterns = []
    if kind == "safe" and excludes:
        patterns = [line.strip().rstrip("/") for line in Path(excludes).read_text().splitlines()
                    if line.strip() and not line.lstrip().startswith("#")]
    if dry_run:
        return {"dry_run": True, "type": kind, "destination": str(destination),
                "users": [{"user": name, "home": str(h), "selected": [str(p) for p in selected]}
                          for name, h, selected in plans], "excludes": patterns, "mount_id": mount_id}
    host = re.sub(r"[^A-Za-z0-9_.-]", "_", socket.gethostname()) or "localhost"
    parent = destination / "atool-backups" / host
    no_links(parent)
    parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    ignore = parent.parent / ".gitignore"
    if ignore.is_symlink() or (ignore.exists() and ignore.read_text() != "*\n"):
        raise ValueError("Backup repository .gitignore must contain only '*'. Review it before backing up.")
    if not ignore.exists():
        with ignore.open("x") as stream:
            stream.write("*\n")
    stage = Path(tempfile.mkdtemp(prefix=".pending-", dir=parent))
    final = parent / f"{stamp()}-{uuid.uuid4().hex[:12]}"
    try:
        inventories, skipped_by_user = {}, {}
        for name, source_home, selected in plans:
            check_device()
            data = stage / "users" / name
            data.mkdir(mode=0o700, parents=True)
            skipped = []
            for source in selected:
                target = data / source.relative_to(source_home)
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                copy_entry(source, target, source_home, [destination], patterns, skipped)
            records = inventory(data)
            if not records:
                raise ValueError(f"No files or directories remain after exclusions for {name}.")
            inventories[name] = records
            skipped_by_user[name] = skipped
        names = [name for name, _, _ in plans]
        metadata = {"schema_version": "1.0", "app_name": "ATM - Backup & Restore",
                    "app_version": "0.0.1", "action": "backup", "status": "success",
                    "dry_run": False, "hostname": host, "backup_date": stamp(),
                    "users": names, "successful_users": names, "failed_users": [],
                    "type": kind, "custom_paths": list(paths)}
        if len(plans) == 1:
            metadata.update(atm_format=1, inventory=inventories[names[0]], source_home=str(plans[0][1]),
                            skipped_special_files=skipped_by_user[names[0]])
        else:
            metadata.update(atm_format=2, inventories=inventories,
                            source_homes={name: str(h) for name, h, _ in plans},
                            skipped_special_files=skipped_by_user)
        encoded = (json.dumps(metadata, indent=2) + "\n").encode("utf-8")
        if len(encoded) > MAX_METADATA_SIZE:
            raise ValueError("Backup metadata exceeds the 64 MiB limit; select fewer paths.")
        (stage / "atool-backup.json").write_bytes(encoded)
        (stage / "atool-backup.json").chmod(0o600)
        logs = stage / "logs"
        logs.mkdir(mode=0o700)
        count = sum(len(records) for records in inventories.values())
        (logs / "summary.txt").write_text(f"success\nUsers: {', '.join(names)}\nEntries: {count}\n")
        check_device()
        stage.rename(final)
        return {"backup": str(final), "users": names, "entries": count,
                "skipped_special_files": metadata["skipped_special_files"]}
    finally:
        try:
            check_device()
        except ValueError:
            pass  # A replacement/unmounted device must not be cleaned by path.
        else:
            if stage.exists():
                shutil.rmtree(stage)


def backup_metadata(root):
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
    return obj


def user_records(obj, user):
    if "atm_format" not in obj:
        return None
    version = obj["atm_format"]
    if type(version) is not int:
        raise ValueError("Unsupported ATM backup format.")
    if version == 1 and len(obj["users"]) == 1:
        records = obj.get("inventory")
    elif version == 2 and isinstance(obj.get("inventories"), dict) and set(obj["inventories"]) == set(obj["users"]):
        records = obj["inventories"].get(user)
    else:
        raise ValueError("Unsupported ATM backup format.")
    if not isinstance(records, dict):
        raise ValueError("Invalid backup inventory.")
    return records


def load_backup(root, user=None, verify=True):
    root = absolute(root)
    obj = backup_metadata(root)
    users = obj["users"]
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
    records = user_records(obj, user)
    if verify:
        actual = inventory(data)
        if records is not None and actual != records:
            raise ValueError("Backup integrity check failed. No files replaced.")
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


def restore_backup(root, target, home, user=None, dry_run=False, all_users=False, home_base=None):
    if user and all_users:
        raise ValueError("Choose --user or --all-users, not both.")
    metadata = backup_metadata(root)
    names = metadata["users"] if all_users else [user]
    plans = [restore_plan(root, target, home, name) for name in names]
    root, _, target, obj = plans[0]
    if home_base is not None:
        if any(target == h or inside(h, target) for _, h in discover_users(home_base, home)):
            raise ValueError("Restore over a live user home is not allowed.")
    sources = [(plan[1].name, plan[1]) for plan in plans]
    result = {"source": str(root), "target": str(target),
              "users": [name for name, _ in sources], "grouped_by_user": all_users,
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
        expected = {}
        for name, data in sources:
            output = stage / name if all_users else stage
            if all_users:
                if output.is_symlink() or output.is_file():
                    output.unlink()
                output.mkdir(mode=0o700, exist_ok=True)
                expected[name] = ["directory"]
            overlay(data, output)
            # External backups can change while staging; verify again before commit.
            stored = user_records(obj, name)
            records = inventory(data)
            if stored is not None and records != stored:
                raise ValueError("Backup changed during restore.")
            prefix = name + "/" if all_users else ""
            expected.update({prefix + key: value for key, value in records.items()})
        staged_records = inventory(stage)
        if any(staged_records.get(key) != value for key, value in expected.items()):
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
                for user in backup_metadata(root)["users"]:
                    load_backup(root, user, verify=False)
            except (ValueError, OSError, TypeError):
                continue
            result.append(str(root))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("backup", "restore", "list", "verify", "users", "devices", "backup-users"))
    parser.add_argument("--home", default=str(Path.home()))
    parser.add_argument("--destination")
    parser.add_argument("--home-base", default="/home")
    parser.add_argument("--media-root", default=f"/media/{pwd.getpwuid(os.getuid()).pw_name}")
    parser.add_argument("--mountinfo", default="/proc/self/mountinfo")
    parser.add_argument("--mount-id")
    parser.add_argument("--type", default="safe", choices=("safe", "full", "main", "custom"))
    parser.add_argument("--path", action="append", default=[])
    parser.add_argument("--source")
    parser.add_argument("--target")
    parser.add_argument("--user")
    parser.add_argument("--all-users", action="store_true")
    parser.add_argument("--excludes")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if args.user and args.all_users:
            raise ValueError("Choose --user or --all-users, not both.")
        if args.action in ("users", "backup-users"):
            if args.action == "users":
                rows = discover_users(args.home_base, args.home)
            else:
                rows = [(name, absolute(args.source) / "users" / name)
                        for name in backup_metadata(args.source)["users"]]
                for name, _ in rows:
                    load_backup(args.source, name, verify=False)
            for name, path in rows:
                print(f"{name}\t{path}")
            return 0
        if args.action == "devices":
            for device in mounted_devices(args.media_root, args.mountinfo):
                print(f"{device['path']}\t{device['source']}\t{device['fstype']}\t"
                      f"{device['free'] / (1024 ** 3):.1f} GiB\t{device['id']}\t{int(device['writable'])}")
            return 0
        if args.action == "backup":
            if not args.destination:
                raise ValueError("Backup requires --destination.")
            selected = select_users(args.home_base, args.home, args.user, args.all_users)
            result = create_backup(args.home, args.destination, args.type, args.path,
                                   args.excludes, dry_run=args.dry_run, sources=selected,
                                   media_root=args.media_root, mountinfo=args.mountinfo, mount_id=args.mount_id)
        elif args.action == "list":
            for path in list_backups(args.destination):
                print(path)
            return 0
        elif args.action == "verify":
            names = backup_metadata(args.source)["users"] if args.all_users else [args.user]
            for name in names:
                _, _, obj = load_backup(args.source, name)
            result = {"valid": True, "integrity": "sha256" if "atm_format" in obj else "legacy: no stored checksums"}
        else:
            if not args.source or not args.target:
                raise ValueError("Restore requires --source and --target.")
            result = restore_backup(args.source, args.target, args.home, args.user, args.dry_run,
                                    args.all_users, args.home_base)
        print(json.dumps(result, indent=2))
        return 0
    except (OSError, ValueError, TypeError, shutil.Error) as error:
        print(f"Backup/restore failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

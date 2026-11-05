"""Space-checked Apps archives, separate from the atool snapshot format."""

import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "lib"))
from atm_plugin_i18n import translator

tr = translator(__file__)

from state import absolute, inside, no_links, stamp, validate_device


def readable(size):
    return f"{size / (1024 ** 3):.2f} GiB ({size} bytes)"


def measure(home):
    source = absolute(home) / "Apps"
    no_links(source)
    if not source.is_dir():
        raise ValueError(tr('ATM_PLUGIN_BACKUP_RESTORE_APPS_NOT_FOUND', source=source))
    size = 0
    entries = 0
    tar_bytes = 10240
    blockers = []
    blocker_count = 0

    def block(message, path=None):
        nonlocal blocker_count
        blocker_count += 1
        if len(blockers) >= 50:
            return
        if path is not None:
            parts = path.relative_to(source).parts
            folder = parts[0] if parts else "Apps"
            hint = "VS Code" if folder.lower() in ("vscode", "code", "visual-studio-code") else folder
            message += tr("ATM_PLUGIN_BACKUP_RESTORE_APP_PATH_DETAIL", hint=hint, path=str(path))
        blockers.append(message)

    for command in ("tar", "gzip"):
        if not shutil.which(command):
            block(tr('ATM_PLUGIN_BACKUP_RESTORE_COMMAND_NOT_FOUND', command=command))
    pending = [source]
    while pending:
        path = pending.pop()
        try:
            info = path.lstat()
            entries += 1
            # Budget for TAR padding, PAX headers and long UTF-8 names/link targets.
            tar_bytes += 4096 + 4 * len(os.fsencode(str(path)))
            if stat.S_ISREG(info.st_mode):
                if not os.access(path, os.R_OK):
                    block(tr('ATM_PLUGIN_BACKUP_RESTORE_UNREADABLE_FILE'), path)
                size += info.st_size
                tar_bytes += ((info.st_size + 511) // 512) * 512
            elif stat.S_ISDIR(info.st_mode):
                if not os.access(path, os.R_OK | os.X_OK):
                    block(tr('ATM_PLUGIN_BACKUP_RESTORE_UNREADABLE_DIRECTORY'), path)
                    continue
                with os.scandir(path) as children:
                    pending.extend(Path(child.path) for child in children)
            elif stat.S_ISLNK(info.st_mode):
                tar_bytes += 4 * len(os.fsencode(os.readlink(path)))
            else:
                kind = (tr("ATM_PLUGIN_BACKUP_RESTORE_FILE_KIND_SOCKET") if stat.S_ISSOCK(info.st_mode)
                        else tr("ATM_PLUGIN_BACKUP_RESTORE_FILE_KIND_FIFO") if stat.S_ISFIFO(info.st_mode)
                        else tr("ATM_PLUGIN_BACKUP_RESTORE_FILE_KIND_DEVICE"))
                block(tr('ATM_PLUGIN_BACKUP_RESTORE_SPECIAL_FILE_BLOCKED', kind=kind), path)
        except OSError as error:
            block(tr('ATM_PLUGIN_BACKUP_RESTORE_INSPECTION_FAILED', error=error.strerror or str(error)), path)
    if blocker_count:
        details = "\n".join(f"  - {item}" for item in blockers)
        if blocker_count > len(blockers):
            details += "\n" + tr("ATM_PLUGIN_BACKUP_RESTORE_MORE_ISSUES", remaining=blocker_count - len(blockers))
        raise ValueError(
            tr('ATM_PLUGIN_BACKUP_RESTORE_PREFLIGHT_BLOCKED', blocker_count=blocker_count, details=details, newline='\n'))
    # Incompressible/sparse files must fit too; never assume a compression ratio.
    required = tar_bytes + (tar_bytes + 99) // 100 + 16 * 1024 * 1024
    return {"source": str(source), "source_bytes": size, "source_size": readable(size),
            "entries": entries, "required_bytes": required, "required_space": readable(required)}


def capacity(destination, required):
    free = shutil.disk_usage(destination).free
    if free < required:
        raise ValueError(tr('ATM_PLUGIN_BACKUP_RESTORE_INSUFFICIENT_SPACE', required=readable(required), free=readable(free), destination=destination))
    return free


def backup(home, destination, media_root, mountinfo, mount_id=None, dry_run=False):
    plan = measure(home)
    source = Path(plan["source"])
    if not destination:
        raise ValueError(tr('ATM_PLUGIN_BACKUP_RESTORE_APPS_DESTINATION_REQUIRED'))
    destination = absolute(destination)
    no_links(destination)
    if inside(destination, source):
        raise ValueError(tr('ATM_PLUGIN_BACKUP_RESTORE_APPS_DESTINATION_OVERLAP'))
    if not destination.is_dir():
        raise ValueError(tr('ATM_PLUGIN_BACKUP_RESTORE_EXISTING_DESTINATION_REQUIRED'))
    selected = validate_device(destination, media_root, mountinfo, mount_id)
    plan.update(destination=str(destination), dry_run=dry_run,
                available_bytes=capacity(destination, plan["required_bytes"]))
    plan["available_space"] = readable(plan["available_bytes"])
    if dry_run:
        return plan

    validate_device(destination, media_root, mountinfo, selected)
    capacity(destination, plan["required_bytes"])
    # A private, exclusively created directory also works on filesystems without
    # hard links. Only the final .tar.gz name denotes a completed archive.
    output = Path(tempfile.mkdtemp(prefix=f"atm-apps-{stamp()}-", dir=destination))
    partial = output / "Apps.tar.gz.part"
    complete = output / "Apps.tar.gz"
    published = False
    try:
        validate_device(destination, media_root, mountinfo, selected)
        environment = dict(os.environ)
        for key in ("TAR_OPTIONS", "GZIP"):
            environment.pop(key, None)
        with partial.open("xb") as stream:
            os.chmod(partial, 0o600)
            result = subprocess.run(
                ["tar", "--format=posix", "--hard-dereference", "-czf", "-",
                 "-C", str(source.parent), "--", source.name],
                stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.PIPE,
                env=environment, check=False)
            if result.returncode:
                detail = result.stderr.decode(errors="replace").strip()
                raise OSError(tr('ATM_PLUGIN_BACKUP_RESTORE_TAR_FAILED', returncode=result.returncode, detail=detail))
            stream.flush()
            os.fsync(stream.fileno())
        validate_device(destination, media_root, mountinfo, selected)
        partial.rename(complete)
        published = True
        plan.update(backup=str(complete), archive_bytes=complete.stat().st_size)
        return plan
    finally:
        if not published:
            partial.unlink(missing_ok=True)
            try:
                output.rmdir()
            except FileNotFoundError:
                pass

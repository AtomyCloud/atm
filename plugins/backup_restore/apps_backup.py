"""Space-checked Apps archives, separate from the atool snapshot format."""

import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

from state import absolute, inside, no_links, stamp, validate_device


def readable(size):
    return f"{size / (1024 ** 3):.2f} GiB ({size} bytes)"


def measure(home):
    source = absolute(home) / "Apps"
    no_links(source)
    if not source.is_dir():
        raise ValueError(f"Apps directory not found: {source}")
    size = 0
    entries = 0
    tar_bytes = 10240
    pending = [source]
    while pending:
        path = pending.pop()
        info = path.lstat()
        entries += 1
        # Budget for TAR padding, PAX headers and long UTF-8 names/link targets.
        tar_bytes += 4096 + 4 * len(os.fsencode(str(path)))
        if stat.S_ISREG(info.st_mode):
            size += info.st_size
            tar_bytes += ((info.st_size + 511) // 512) * 512
        elif stat.S_ISDIR(info.st_mode):
            with os.scandir(path) as children:
                pending.extend(Path(child.path) for child in children)
        elif stat.S_ISLNK(info.st_mode):
            tar_bytes += 4 * len(os.fsencode(os.readlink(path)))
        else:
            raise ValueError(f"Unsupported special file; close its application first: {path}")
    # Incompressible/sparse files must fit too; never assume a compression ratio.
    required = tar_bytes + (tar_bytes + 99) // 100 + 16 * 1024 * 1024
    return {"source": str(source), "source_bytes": size, "source_size": readable(size),
            "entries": entries, "required_bytes": required, "required_space": readable(required)}


def capacity(destination, required):
    free = shutil.disk_usage(destination).free
    if free < required:
        raise ValueError(f"Not enough space: required {readable(required)}, available {readable(free)} "
                         f"at {destination}. No successful archive created.")
    return free


def backup(home, destination, media_root, mountinfo, mount_id=None, dry_run=False):
    plan = measure(home)
    source = Path(plan["source"])
    if not destination:
        raise ValueError("Choose a mounted device or an existing directory outside ~/Apps.")
    destination = absolute(destination)
    no_links(destination)
    if inside(destination, source):
        raise ValueError("Backup destination must be outside ~/Apps, including ~/Apps/Backups.")
    if not destination.is_dir():
        raise ValueError("Backup destination must be an existing directory.")
    if not shutil.which("tar") or not shutil.which("gzip"):
        raise ValueError("The tar and gzip commands are required for Apps backups.")
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
                raise OSError(f"tar failed ({result.returncode}); backup not published: {detail}")
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

#!/usr/bin/env python3
"""Snapshot and restore Zsh state without loading shell configuration."""

import argparse
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import tarfile
import tempfile


BEGIN = "# >>> ATM Oh My Zsh >>>"
END = "# <<< ATM Oh My Zsh <<<"
FORMAT = "atm-zsh-backup-v1"
DOTFILES = (".zshrc", ".zshenv", ".zprofile", ".zlogin", ".zlogout", ".zsh_history", ".zshrc.pre-oh-my-zsh")
LOADER = re.compile(r'''^\s*(?:source|\.)\s+(?:"[^"\n]*oh-my-zsh\.sh"|'[^'\n]*oh-my-zsh\.sh'|[^\s;]+oh-my-zsh\.sh)\s*(?:#.*)?$''')


def layout(args):
    home, dot, omz, custom, backups = (
        Path(value).expanduser().resolve()
        for value in (args.home, args.dot_dir, args.omz, args.custom, args.backups)
    )
    if omz == Path("/") or omz == home or omz == dot or omz in home.parents or omz in dot.parents:
        raise ValueError("Invalid Oh My Zsh directory")
    if backups == omz or omz in backups.parents:
        raise ValueError("Backups must be outside the Oh My Zsh directory")
    targets = {name[1:]: dot / name for name in DOTFILES}
    targets.update({"p10k.zsh": home / ".p10k.zsh", "shell.pre-oh-my-zsh": home / ".shell.pre-oh-my-zsh",
                    "oh-my-zsh": omz, "custom": custom})
    if custom != omz and omz not in custom.parents:
        if custom == home or custom == dot or custom in home.parents or custom in dot.parents or custom in omz.parents:
            raise ValueError("Invalid custom directory")
        if backups == custom or custom in backups.parents:
            raise ValueError("Backups must be outside the custom directory")
    return targets, backups


def read_archive(filename, targets, stage=None):
    seen, roots, metadata = set(), set(), None
    directory_modes = []
    with tarfile.open(filename, "r:gz") as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not name.parts or str(name) in seen:
                raise ValueError("Unsafe or duplicate archive path")
            seen.add(str(name))
            if not (member.isfile() or member.isdir()):
                raise ValueError("Archive links and special files are not supported")
            if str(name) == "metadata.json":
                if not member.isfile() or member.size > 65536:
                    raise ValueError("Invalid backup metadata")
                metadata = json.load(archive.extractfile(member))
                continue
            root = name.parts[0]
            if root not in targets or (root not in ("oh-my-zsh", "custom") and (len(name.parts) != 1 or not member.isfile())):
                raise ValueError("Unexpected backup content")
            roots.add(root)
            if stage is not None:
                destination = stage.joinpath(*name.parts)
                if member.isdir():
                    destination.mkdir(parents=True, exist_ok=True)
                    directory_modes.append((destination, member.mode & 0o777))
                else:
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    with archive.extractfile(member) as source, destination.open("xb") as output:
                        shutil.copyfileobj(source, output)
                    destination.chmod(member.mode & 0o777)
            elif member.isfile():
                with archive.extractfile(member) as source:
                    while source.read(1024 * 1024):
                        pass
        while archive.fileobj.read(1024 * 1024):
            pass
    if not isinstance(metadata, dict) or metadata.get("format") != FORMAT:
        raise ValueError("Not an ATM Zsh backup")
    entries = metadata.get("entries")
    if not isinstance(entries, list) or any(not isinstance(item, str) for item in entries):
        raise ValueError("Invalid backup entries")
    if len(entries) != len(set(entries)) or set(entries) != roots:
        raise ValueError("Incomplete backup")
    destinations = [targets[key].resolve() for key in entries]
    for index, dest in enumerate(destinations):
        for other in destinations[index + 1:]:
            if dest == other or dest in other.parents or other in dest.parents:
                raise ValueError("Overlapping restore destinations")
    for destination, mode in reversed(directory_modes):
        destination.chmod(mode)
    return entries


def backup(args):
    targets, directory = layout(args)
    entries = {key: path for key, path in targets.items() if path.exists()}
    if targets["oh-my-zsh"] == targets["custom"] or targets["oh-my-zsh"] in targets["custom"].parents:
        entries.pop("custom", None)
    directory.mkdir(parents=True, exist_ok=True)
    prefix = "zsh-" + datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S-%f-")
    fd, temporary = tempfile.mkstemp(prefix=prefix, suffix=".tar.gz.part", dir=directory)
    temporary = Path(temporary)
    final = temporary.with_suffix("")
    try:
        with os.fdopen(fd, "wb") as output, tarfile.open(fileobj=output, mode="w:gz", dereference=True) as archive:
            data = json.dumps({"format": FORMAT, "entries": list(entries)}).encode()
            info = tarfile.TarInfo("metadata.json")
            info.size, info.mode = len(data), 0o600
            archive.addfile(info, io.BytesIO(data))
            for key, path in entries.items():
                archive.add(path, arcname=key)
        read_archive(temporary, targets)
        os.replace(temporary, final)
        return final
    finally:
        temporary.unlink(missing_ok=True)


def restore(args):
    targets, _ = layout(args)
    with tempfile.TemporaryDirectory(prefix="atm-zsh-read-") as scratch:
        stage = Path(scratch)
        entries = read_archive(args.archive, targets, stage)
        prepared, applied, workdirs = [], [], []
        retain_recovery = False
        try:
            for key in entries:
                dest = targets[key].resolve()
                dest.parent.mkdir(parents=True, exist_ok=True)
                work = Path(tempfile.mkdtemp(prefix=".atm-zsh-restore-", dir=dest.parent))
                workdirs.append(work)
                source, replacement = stage / key, work / "new"
                if source.is_dir():
                    shutil.copytree(source, replacement)
                else:
                    shutil.copy2(source, replacement)
                prepared.append((dest, replacement, work / "old"))
            for dest, replacement, old in prepared:
                existed = dest.exists()
                if existed:
                    os.replace(dest, old)
                applied.append((dest, old, existed))
                os.replace(replacement, dest)
        except OSError as error:
            failed = []
            for dest, old, existed in reversed(applied):
                try:
                    if dest.is_dir():
                        shutil.rmtree(dest)
                    else:
                        dest.unlink(missing_ok=True)
                    if existed:
                        os.replace(old, dest)
                except OSError:
                    failed.append(str(old.parent))
            if failed:
                retain_recovery = True
                raise OSError("Restore rollback failed; recovery files retained in: " + ", ".join(failed)) from error
            raise
        finally:
            if not retain_recovery:
                for work in workdirs:
                    shutil.rmtree(work)


def configure(args, remove=False):
    targets, _ = layout(args)
    path = targets["zshrc"].resolve()
    text = path.read_text(encoding="utf-8") if path.exists() else ""
    lines, inside = [], False
    for line in text.splitlines(keepends=True):
        if line.rstrip("\r\n") == BEGIN:
            inside = True
        elif line.rstrip("\r\n") == END:
            inside = False
        elif not inside:
            if remove and LOADER.match(line.rstrip("\r\n")):
                line = "# ATM disabled Oh My Zsh loader: " + line
            lines.append(line)
    if inside:
        raise ValueError("Unclosed ATM configuration block")
    content = "".join(lines)
    if not remove and not any(LOADER.match(line.rstrip("\r\n")) for line in lines):
        content = content.rstrip("\n") + "\n\n" + BEGIN + "\n"
        content += "export ZSH=" + shlex.quote(str(targets["oh-my-zsh"])) + "\n"
        content += "export ZSH_CUSTOM=" + shlex.quote(str(targets["custom"])) + "\n"
        content += 'if [[ -r "$ZSH/oh-my-zsh.sh" ]]; then source "$ZSH/oh-my-zsh.sh"; fi\n' + END + "\n"
    if content == text:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".atm-zshrc-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as output:
            output.write(content)
            if path.exists():
                os.fchmod(output.fileno(), path.stat().st_mode & 0o777)
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("backup", "validate", "restore", "configure", "unconfigure"))
    for option in ("home", "dot-dir", "omz", "custom", "backups"):
        parser.add_argument("--" + option, required=True)
    parser.add_argument("--archive")
    args = parser.parse_args()
    if args.action in ("validate", "restore") and not args.archive:
        parser.error("--archive is required")
    if args.action == "backup":
        print(backup(args))
    elif args.action == "validate":
        read_archive(args.archive, layout(args)[0])
    elif args.action == "restore":
        restore(args)
    else:
        configure(args, args.action == "unconfigure")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, EOFError, RecursionError, tarfile.TarError) as error:
        raise SystemExit(str(error))

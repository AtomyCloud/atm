#!/usr/bin/env python3
"""Check playback dependencies and stage checksum-verified official spotatui binaries."""

import argparse
from datetime import date
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid


sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "lib"))
from atm_plugin_i18n import translator

tr = translator(__file__)

VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+\Z")
BASE = "https://github.com/LargeModGames/spotatui/releases/download/"


class HelpFormatter(argparse.HelpFormatter):
    def _format_usage(self, usage, actions, groups, prefix=None):
        return super()._format_usage(
            usage, actions, groups,
            tr("ATM_PLUGIN_SPOTATUI_HELP_USAGE") if prefix is None else prefix)


class ArgumentParser(argparse.ArgumentParser):
    def __init__(self, *args, **kwargs):
        kwargs.setdefault("formatter_class", HelpFormatter)
        kwargs.setdefault("add_help", False)
        super().__init__(*args, **kwargs)
        self._positionals.title = tr("ATM_PLUGIN_SPOTATUI_HELP_ARGUMENTS")
        self._optionals.title = tr("ATM_PLUGIN_SPOTATUI_HELP_OPTIONS")
        self.add_argument("-h", "--help", action="help",
                          help=tr("ATM_PLUGIN_SPOTATUI_HELP_EXIT"))


def command(args):
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    return subprocess.run(args, capture_output=True, text=True, timeout=30, env=env)


def executable(name):
    path = shutil.which(name)
    if not path or not os.path.isabs(path) or any(ord(c) < 32 for c in path):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_MISSING_EXECUTABLE", name=name))
    return path


def dependencies():
    executable("curl")
    ffmpeg = executable("ffmpeg")
    executable("ffprobe")
    ytdlp = executable("yt-dlp")
    result = command([ytdlp, "--ignore-config", "--version"])
    match = re.fullmatch(r"(\d{4})\.(\d{2})\.(\d{2})(?:[.a-zA-Z0-9+_-]*)", result.stdout.strip())
    if result.returncode or not match or date(*map(int, match.groups())) < date(2025, 11, 12):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_OLD_YTDLP"))
    yt_version = result.stdout.strip()
    runtime, runtime_path, runtime_version = "", "", ""
    for name, minimum in (("deno", (2, 3, 0)), ("node", (22, 0, 0))):
        if not shutil.which(name):
            continue
        path = executable(name)
        output = command([path, "--version"])
        version = re.match(r"(?:deno |v)?(\d+)\.(\d+)\.(\d+)(?:\s|$)", output.stdout)
        if output.returncode == 0 and version and tuple(map(int, version.groups())) >= minimum:
            runtime, runtime_path, runtime_version = name, path, version.group(0).strip()
            break
    if not runtime:
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_MISSING_RUNTIME"))
    # With no URL yt-dlp initializes diagnostics then exits 2, without contacting YouTube.
    output = command([ytdlp, "--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--verbose",
                      "--simulate", "--js-runtimes", runtime + ":" + runtime_path])
    diagnostic = output.stderr
    if output.returncode != 2 or not re.search(r"\b(?:yt_dlp_ejs|yt-dlp-ejs)-[0-9]", diagnostic):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_MISSING_EJS"))
    if not re.search(r"JS runtimes:.*\b" + runtime + r"-[0-9]", diagnostic):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_RUNTIME_INIT"))
    return dict(ytdlp=ytdlp, yt_version=yt_version, runtime=runtime,
                runtime_path=runtime_path, runtime_version=runtime_version, ffmpeg=ffmpeg)


def resolve(release, platform):
    version = str(release.get("tag_name", "")).removeprefix("v")
    if not VERSION.fullmatch(version) or release.get("draft") is not False or release.get("prerelease") is not False:
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_STABLE_RELEASE"))
    if platform not in ("linux-x86_64", "linux-aarch64"):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_UNSUPPORTED_PLATFORM"))
    name = f"spotatui-{platform}.tar.gz"
    entries = release.get("assets")
    if not isinstance(entries, list) or any(not isinstance(a, dict) for a in entries):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_INVALID_ASSETS"))
    urls = []
    for asset in (name, name + ".sha256"):
        expected = BASE + "v" + version + "/" + asset
        matches = [a for a in entries if a.get("name") == asset]
        if len(matches) != 1 or matches[0].get("browser_download_url") != expected:
            raise ValueError(tr("ATM_PLUGIN_SPOTATUI_OFFICIAL_ASSET", asset=asset))
        urls.append(expected)
    return "\t".join((version, *urls))


def launcher_scripts(deps):
    for key in ("ytdlp", "runtime_path", "ffmpeg"):
        value = deps.get(key)
        if not isinstance(value, str) or not os.path.isabs(value) or any(ord(c) < 32 for c in value):
            raise ValueError(tr("ATM_PLUGIN_SPOTATUI_DEPENDENCY_PATH"))
    if deps.get("runtime") not in ("deno", "node"):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_INVALID_RUNTIME"))
    shim = "#!/usr/bin/env bash\nexec " + shlex.quote(deps["ytdlp"]) + " --js-runtimes "
    shim += shlex.quote(deps["runtime"] + ":" + deps["runtime_path"]) + ' "$@"\n'
    launcher = '#!/usr/bin/env bash\nset -e\nself="$(readlink -f -- "${BASH_SOURCE[0]}")"\nroot="$(cd -- "$(dirname -- "$self")/.." && pwd)"\n'
    launcher += 'export PATH="$root/shims":' + shlex.quote(str(Path(deps["ffmpeg"]).parent)) + ':"$PATH"\n'
    launcher += 'exec "$root/libexec/spotatui" "$@"\n'
    return {"shims/yt-dlp": shim, "bin/spotatui": launcher}


def configure(destination, deps):
    destination = Path(destination)
    scripts = launcher_scripts(deps)
    for path in (destination, destination / "bin", destination / "shims"):
        if path.is_symlink() or not path.is_dir():
            raise ValueError(tr("ATM_PLUGIN_SPOTATUI_UNSAFE_DIRECTORY"))
    marker = destination / ".atm-spotatui-managed"
    if marker.is_symlink() or not marker.is_file():
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_UNMANAGED_DIRECTORY"))
    for name, text in scripts.items():
        target = destination / name
        if target.is_symlink() or not target.is_file():
            raise ValueError(tr("ATM_PLUGIN_SPOTATUI_UNSAFE_LAUNCHER"))
        temporary = None
        try:
            with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", prefix=".atm-launcher-", dir=target.parent, delete=False) as output:
                temporary = Path(output.name)
                output.write(text)
            temporary.chmod(0o755)
            temporary.replace(target)
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)


def install(archive, checksum, destination, deps):
    archive, checksum, destination = Path(archive), Path(checksum), Path(destination)
    if os.path.lexists(destination):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_EXISTING_DIRECTORY"))
    lines = checksum.read_text(encoding="ascii").splitlines()
    if len(lines) != 1:
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_ONE_CHECKSUM"))
    fields = lines[0].split()
    if not fields or not re.fullmatch(r"[a-fA-F0-9]{64}", fields[0]) or len(fields) > 2:
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_INVALID_CHECKSUM"))
    if len(fields) == 2 and fields[1].lstrip("*") != archive.name:
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_CHECKSUM_FILENAME"))
    digest = hashlib.sha256()
    with archive.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    if digest.hexdigest() != fields[0].lower():
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_CHECKSUM_MISMATCH"))
    scripts = launcher_scripts(deps)
    with tarfile.open(archive, "r:gz") as package:
        binaries = []
        for member in package:
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts or not path.parts:
                raise ValueError(tr("ATM_PLUGIN_SPOTATUI_ARCHIVE_PATH"))
            if not (member.isfile() or member.isdir()):
                raise ValueError(tr("ATM_PLUGIN_SPOTATUI_ARCHIVE_LINKS"))
            if str(path) == "spotatui":
                if not member.isfile() or not 0 < member.size <= 256 * 1024 * 1024:
                    raise ValueError(tr("ATM_PLUGIN_SPOTATUI_INVALID_BINARY"))
                binaries.append(member)
        if len(binaries) != 1:
            raise ValueError(tr("ATM_PLUGIN_SPOTATUI_ONE_BINARY"))
        destination.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix=".atm-spotatui-", dir=destination.parent) as scratch:
            stage = Path(scratch)
            for folder in ("bin", "libexec", "shims"):
                (stage / folder).mkdir()
            with package.extractfile(binaries[0]) as source, (stage / "libexec/spotatui").open("xb") as target:
                shutil.copyfileobj(source, target)
            (stage / "libexec/spotatui").chmod(0o755)
            for name, text in scripts.items():
                (stage / name).write_text(text, encoding="utf-8")
                (stage / name).chmod(0o755)
            (stage / ".atm-spotatui-managed").write_text(fields[0].lower() + "\n", encoding="ascii")
            if os.path.lexists(destination):
                raise ValueError(tr("ATM_PLUGIN_SPOTATUI_EXISTING_DIRECTORY"))
            stage.rename(destination)


def installed_version(path):
    if not os.path.isabs(path) or not os.access(path, os.X_OK):
        return ""
    try:
        result = subprocess.run([path, "--version"], stdin=subprocess.DEVNULL, capture_output=True,
                                text=True, timeout=3, env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))
    except (OSError, subprocess.SubprocessError):
        return ""
    match = re.fullmatch(r"spotatui\s+v?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?)", result.stdout.strip())
    return match.group(1) if result.returncode == 0 and match else ""


def fingerprint(path):
    try:
        info = Path(path).lstat()
    except FileNotFoundError:
        return "missing"
    if not (stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode)):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_LOCAL_FILE_TYPE"))
    return ":".join(map(str, (info.st_dev, info.st_ino, info.st_mode, info.st_size, info.st_mtime_ns, info.st_ctime_ns)))


def publish_entry(entry, target, expected):
    entry, target = Path(entry), Path(target)
    if not entry.is_absolute() or not target.is_absolute() or not target.is_file():
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_LOCAL_TARGET"))
    if fingerprint(entry) != expected:
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_LOCAL_CHANGED"))
    entry.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".atm-spotatui-entry-", dir=entry.parent) as scratch:
        staged = Path(scratch) / "spotatui"
        staged.symlink_to(target)
        if expected == "missing":
            # link() creates the entry without replacing a concurrently created file.
            os.link(staged, entry, follow_symlinks=False)
        else:
            owned = entry.is_symlink() and os.readlink(entry) == str(target)
            if not owned:
                backup = entry.with_name(".spotatui-before-atm-" + uuid.uuid4().hex)
                os.link(entry, backup, follow_symlinks=False)
                if fingerprint(entry) != expected:
                    # Creating a hard link changes ctime; compare identity and content metadata.
                    if fingerprint(entry).rsplit(":", 1)[0] != expected.rsplit(":", 1)[0]:
                        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_LOCAL_CHANGED_BACKUP"))
                print(backup)
            staged.replace(entry)


def main():
    if sys.version_info < (3, 9):
        raise ValueError(tr("ATM_PLUGIN_SPOTATUI_PYTHON_VERSION"))
    parser = ArgumentParser(description=tr("ATM_PLUGIN_SPOTATUI_HELP_DESCRIPTION"))
    commands = parser.add_subparsers(dest="action", required=True)
    commands.add_parser("dependencies")
    resolver = commands.add_parser("resolve")
    resolver.add_argument("platform")
    configuration = commands.add_parser("configure")
    configuration.add_argument("destination")
    probe = commands.add_parser("installed-version")
    probe.add_argument("path")
    identity = commands.add_parser("fingerprint")
    identity.add_argument("path")
    publisher = commands.add_parser("publish-entry")
    publisher.add_argument("entry")
    publisher.add_argument("target")
    publisher.add_argument("expected")
    installer = commands.add_parser("install")
    installer.add_argument("archive")
    installer.add_argument("checksum")
    installer.add_argument("destination")
    args = parser.parse_args()
    if args.action == "dependencies":
        print(json.dumps(dependencies()))
    elif args.action == "resolve":
        release = json.load(sys.stdin)
        if not isinstance(release, dict):
            raise ValueError(tr("ATM_PLUGIN_SPOTATUI_RELEASE_JSON"))
        print(resolve(release, args.platform))
    elif args.action == "configure":
        configure(args.destination, json.load(sys.stdin))
    elif args.action == "installed-version":
        print(installed_version(args.path))
    elif args.action == "fingerprint":
        print(fingerprint(args.path))
    elif args.action == "publish-entry":
        publish_entry(args.entry, args.target, args.expected)
    else:
        install(args.archive, args.checksum, args.destination, json.load(sys.stdin))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, EOFError, tarfile.TarError, subprocess.SubprocessError) as error:
        raise SystemExit(str(error))

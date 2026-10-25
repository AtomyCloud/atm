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
import subprocess
import sys
import tarfile
import tempfile


VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+\Z")
BASE = "https://github.com/LargeModGames/spotatui/releases/download/"


def command(args):
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    return subprocess.run(args, capture_output=True, text=True, timeout=30, env=env)


def executable(name):
    path = shutil.which(name)
    if not path or not os.path.isabs(path) or any(ord(c) < 32 for c in path):
        raise ValueError("Missing executable on PATH: " + name)
    return path


def dependencies():
    executable("curl")
    ffmpeg = executable("ffmpeg")
    executable("ffprobe")
    ytdlp = executable("yt-dlp")
    result = command([ytdlp, "--ignore-config", "--version"])
    match = re.fullmatch(r"(\d{4})\.(\d{2})\.(\d{2})(?:[.a-zA-Z0-9+_-]*)", result.stdout.strip())
    if result.returncode or not match or date(*map(int, match.groups())) < date(2025, 11, 12):
        raise ValueError("yt-dlp is too old or unreadable; install/upgrade yt-dlp[default] with pipx")
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
        raise ValueError("Missing supported runtime: install Deno >=2.3 or Node >=22")
    # With no URL yt-dlp initializes diagnostics then exits 2, without contacting YouTube.
    output = command([ytdlp, "--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--verbose",
                      "--simulate", "--js-runtimes", runtime + ":" + runtime_path])
    diagnostic = output.stderr
    if output.returncode != 2 or not re.search(r"\b(?:yt_dlp_ejs|yt-dlp-ejs)-[0-9]", diagnostic):
        raise ValueError("yt-dlp EJS solver was not detected; use yt-dlp[default] and a current yt-dlp")
    if not re.search(r"JS runtimes:.*\b" + runtime + r"-[0-9]", diagnostic):
        raise ValueError("yt-dlp could not initialize the selected JavaScript runtime")
    return dict(ytdlp=ytdlp, yt_version=yt_version, runtime=runtime,
                runtime_path=runtime_path, runtime_version=runtime_version, ffmpeg=ffmpeg)


def resolve(release, platform):
    version = str(release.get("tag_name", "")).removeprefix("v")
    if not VERSION.fullmatch(version) or release.get("draft") is not False or release.get("prerelease") is not False:
        raise ValueError("Expected a stable spotatui release")
    if platform not in ("linux-x86_64", "linux-aarch64"):
        raise ValueError("Unsupported spotatui platform")
    name = f"spotatui-{platform}.tar.gz"
    entries = release.get("assets")
    if not isinstance(entries, list) or any(not isinstance(a, dict) for a in entries):
        raise ValueError("Invalid release assets")
    urls = []
    for asset in (name, name + ".sha256"):
        expected = BASE + "v" + version + "/" + asset
        matches = [a for a in entries if a.get("name") == asset]
        if len(matches) != 1 or matches[0].get("browser_download_url") != expected:
            raise ValueError("Missing official asset/checksum or unexpected source: " + asset)
        urls.append(expected)
    return "\t".join((version, *urls))


def launcher_scripts(deps):
    for key in ("ytdlp", "runtime_path", "ffmpeg"):
        value = deps.get(key)
        if not isinstance(value, str) or not os.path.isabs(value) or any(ord(c) < 32 for c in value):
            raise ValueError("Invalid dependency path")
    if deps.get("runtime") not in ("deno", "node"):
        raise ValueError("Invalid JavaScript runtime")
    shim = "#!/usr/bin/env bash\nexec " + shlex.quote(deps["ytdlp"]) + " --js-runtimes "
    shim += shlex.quote(deps["runtime"] + ":" + deps["runtime_path"]) + ' "$@"\n'
    launcher = '#!/usr/bin/env bash\nset -e\nroot="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"\n'
    launcher += 'export PATH="$root/shims":' + shlex.quote(str(Path(deps["ffmpeg"]).parent)) + ':"$PATH"\n'
    launcher += 'exec "$root/libexec/spotatui" "$@"\n'
    return {"shims/yt-dlp": shim, "bin/spotatui": launcher}


def configure(destination, deps):
    destination = Path(destination)
    scripts = launcher_scripts(deps)
    for path in (destination, destination / "bin", destination / "shims"):
        if path.is_symlink() or not path.is_dir():
            raise ValueError("Unsafe managed spotatui directory")
    marker = destination / ".atm-spotatui-managed"
    if marker.is_symlink() or not marker.is_file():
        raise ValueError("Unmanaged spotatui directory")
    for name, text in scripts.items():
        target = destination / name
        if target.is_symlink() or not target.is_file():
            raise ValueError("Unsafe managed launcher")
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
        raise ValueError("Existing spotatui directory will not be overwritten")
    lines = checksum.read_text(encoding="ascii").splitlines()
    if len(lines) != 1:
        raise ValueError("Expected exactly one SHA-256 checksum")
    fields = lines[0].split()
    if not fields or not re.fullmatch(r"[a-fA-F0-9]{64}", fields[0]) or len(fields) > 2:
        raise ValueError("Invalid SHA-256 checksum")
    if len(fields) == 2 and fields[1].lstrip("*") != archive.name:
        raise ValueError("Checksum filename does not match archive")
    digest = hashlib.sha256()
    with archive.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    if digest.hexdigest() != fields[0].lower():
        raise ValueError("spotatui SHA-256 mismatch")
    scripts = launcher_scripts(deps)
    with tarfile.open(archive, "r:gz") as package:
        binaries = []
        for member in package:
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts or not path.parts:
                raise ValueError("Unsafe archive path")
            if not (member.isfile() or member.isdir()):
                raise ValueError("Archive links/special files are not accepted")
            if str(path) == "spotatui":
                if not member.isfile() or not 0 < member.size <= 256 * 1024 * 1024:
                    raise ValueError("Invalid spotatui binary")
                binaries.append(member)
        if len(binaries) != 1:
            raise ValueError("Expected exactly one root spotatui binary")
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
                raise ValueError("Existing spotatui directory will not be overwritten")
            stage.rename(destination)


def main():
    if sys.version_info < (3, 9):
        raise ValueError("Python 3.9 or newer is required")
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    commands.add_parser("dependencies")
    resolver = commands.add_parser("resolve")
    resolver.add_argument("platform")
    configuration = commands.add_parser("configure")
    configuration.add_argument("destination")
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
            raise ValueError("Invalid release JSON")
        print(resolve(release, args.platform))
    elif args.action == "configure":
        configure(args.destination, json.load(sys.stdin))
    else:
        install(args.archive, args.checksum, args.destination, json.load(sys.stdin))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, EOFError, tarfile.TarError, subprocess.SubprocessError) as error:
        raise SystemExit(str(error))

#!/usr/bin/env python3
"""Read official Linux Desktop releases and clean Desktop-only CLI settings."""

import argparse
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import sys
import tempfile
from urllib.parse import urlsplit, urlunsplit


class DesktopReleases(HTMLParser):
    def __init__(self):
        super().__init__()
        self.heading = None
        self.version = None
        self.releases = {}

    def handle_starttag(self, tag, attrs):
        if tag == "h2":
            self.heading = []
            self.version = None
        if tag != "a" or self.version is None:
            return
        link = urlsplit(dict(attrs).get("href", ""))
        if (
            link.scheme == "https"
            and link.netloc == "desktop.docker.com"
            and re.fullmatch(
                r"/linux/main/amd64/[0-9]+/docker-desktop-amd64\.deb", link.path
            )
        ):
            self.releases.setdefault(
                self.version, urlunsplit((link.scheme, link.netloc, link.path, "", ""))
            )

    def handle_data(self, data):
        if self.heading is not None:
            self.heading.append(data)

    def handle_endtag(self, tag):
        if tag == "h2" and self.heading is not None:
            title = "".join(self.heading).strip()
            self.version = title if re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", title) else None
            self.heading = None


def clean_config(path, check_only):
    path = path.resolve()
    if not path.exists():
        return
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("Docker config must be a JSON object")
    changed = False
    for key, value in (("credsStore", "desktop"), ("currentContext", "desktop-linux")):
        if data.get(key) == value:
            del data[key]
            changed = True
    if check_only or not changed:
        return

    metadata = path.stat()
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", dir=path.parent, prefix=".atm-desktop-", delete=False
        ) as output:
            temporary = Path(output.name)
            if os.geteuid() == 0:
                os.fchown(output.fileno(), metadata.st_uid, metadata.st_gid)
            os.fchmod(output.fileno(), metadata.st_mode & 0o777)
            json.dump(data, output, indent=2)
            output.write("\n")
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("releases", "check-config", "clean-config"))
    parser.add_argument("path", nargs="?")
    args = parser.parse_args()
    if args.action == "releases":
        releases = DesktopReleases()
        releases.feed(sys.stdin.read())
        if not releases.releases:
            return 1
        for version in sorted(
            releases.releases, key=lambda value: tuple(map(int, value.split("."))), reverse=True
        ):
            print(f"{version}\t{releases.releases[version]}")
    else:
        if args.path is None:
            parser.error("config path is required")
        clean_config(Path(args.path), args.action == "check-config")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError):
        sys.exit(1)

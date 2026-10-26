#!/usr/bin/env python3
"""Read and download plugins exclusively from AtomyCloud/atm main/plugins."""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import unicodedata
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import HTTPRedirectHandler, Request, build_opener


SOURCE = "https://github.com/AtomyCloud/atm/tree/main/plugins"
API = "https://api.github.com/repos/AtomyCloud/atm"
RAW = "https://raw.githubusercontent.com/AtomyCloud/atm"
ID = re.compile(r"[a-z][a-z0-9_-]{0,63}\Z")
SHA = re.compile(r"[0-9a-f]{40}\Z")
MAX_FILE = 4 * 1024 * 1024
MAX_TOTAL = 32 * 1024 * 1024


def core_path(path):
    return path == "core-api.json" or path.startswith(("bin/", "lib/", "lang/"))


class CatalogError(ValueError):
    pass


class NoRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise CatalogError("Unexpected redirect from the official plugin repository")


def get_bytes(url, limit, token=""):
    if token and any(ord(char) < 33 or ord(char) > 126 for char in token):
        raise CatalogError("GITHUB_TOKEN contains invalid characters")
    headers = {"User-Agent": "ATM-plugin-catalog/0.0.1", "Accept": "application/vnd.github+json"}
    if token:
        if not url.startswith(API + "/"):
            raise CatalogError("Credentials may only be sent to the official GitHub API")
        headers["Authorization"] = "Bearer " + token
    try:
        with build_opener(NoRedirects()).open(Request(url, headers=headers), timeout=20) as response:
            data = response.read(limit + 1)
    except HTTPError as error:
        if error.code in (403, 429):
            raise CatalogError("GitHub access denied or rate limit reached; try later or set GITHUB_TOKEN") from None
        if error.code == 404:
            raise CatalogError("Official repository or plugin unavailable; private access requires GITHUB_TOKEN") from None
        raise CatalogError("GitHub HTTP error: " + str(error.code)) from None
    except URLError as error:
        raise CatalogError("Cannot reach the official GitHub repository: " + str(error.reason)) from None
    if len(data) > limit:
        raise CatalogError("Repository response exceeds the download limit")
    return data


def safe_path(value):
    if not isinstance(value, str) or not value or "\\" in value:
        raise CatalogError("Invalid plugin path")
    parts = value.split("/")
    if any(part in ("", ".", "..", ".git") for part in parts):
        raise CatalogError("Unsafe plugin path")
    if any(unicodedata.category(char).startswith("C") for char in value):
        raise CatalogError("Control characters in plugin path")
    return value


def clean(value):
    return "".join(" " if unicodedata.category(char).startswith("C") else char for char in value)


def parse_metadata(data, plugin_id):
    values = {}
    for line in data.decode("utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        match = re.fullmatch(r"(ATM_PLUGIN_[A-Z_]+)=(.*)", line.strip())
        if not match or "$" in match[2] or "`" in match[2]:
            raise CatalogError("Metadata must contain literal ATM_PLUGIN_ assignments")
        lexer = shlex.shlex(match[2], posix=True, punctuation_chars=";&|<>()")
        lexer.whitespace_split = True
        words = list(lexer)
        if len(words) != 1 or match[1] in values:
            raise CatalogError("Invalid or duplicate plugin metadata")
        values[match[1]] = words[0]
    if values.get("ATM_PLUGIN_ID") != plugin_id:
        raise CatalogError("Plugin ID differs from its official directory name")
    entrypoint = safe_path(values.get("ATM_PLUGIN_ENTRYPOINT", "plugin.sh"))
    if not entrypoint.endswith(".sh"):
        raise CatalogError("Plugin entrypoint must be a Bash file")
    return values


class Catalog:
    def __init__(self, revision=None):
        self.token = os.environ.get("GITHUB_TOKEN", "")
        if revision is None:
            revision = self.json(API + "/commits/main").get("sha", "")
        if not isinstance(revision, str) or not SHA.fullmatch(revision):
            raise CatalogError("Invalid main revision")
        self.revision = revision
        tree = self.json(API + "/git/trees/" + revision + "?recursive=1")
        if tree.get("truncated") is not False or not isinstance(tree.get("tree"), list):
            raise CatalogError("Incomplete GitHub tree; no plugin will be downloaded")
        self.plugins = {}
        self.core_files = {}
        self.cache = {}
        seen = set()
        for entry in tree["tree"]:
            if not isinstance(entry, dict) or not isinstance(entry.get("path"), str):
                raise CatalogError("Invalid repository tree entry")
            path = entry["path"]
            if not path.startswith("plugins/") and not core_path(path):
                continue
            safe_path(path)
            if path in seen:
                raise CatalogError("Duplicate repository path")
            seen.add(path)
            if core_path(path):
                if entry.get("type") != "tree":
                    self.core_files[path] = entry
                continue
            parts = path.split("/")
            if not ID.fullmatch(parts[1]):
                continue
            if len(parts) > 2 and entry.get("type") != "tree":
                self.plugins.setdefault(parts[1], {})["/".join(parts[2:])] = entry
        self.plugins = {key: files for key, files in self.plugins.items() if "plugin.metadata" in files}
        if len(self.plugins) > 200:
            raise CatalogError("Plugin catalog exceeds the supported size")

    def json(self, url):
        value = json.loads(get_bytes(url, 16 * 1024 * 1024, self.token))
        if not isinstance(value, dict):
            raise CatalogError("Invalid GitHub response")
        return value

    def blob(self, entry):
        sha, size = entry.get("sha", ""), entry.get("size")
        if entry.get("type") != "blob" or entry.get("mode") not in ("100644", "100755"):
            raise CatalogError("Plugin links, submodules, and special files are not supported")
        if not isinstance(sha, str) or not SHA.fullmatch(sha):
            raise CatalogError("Invalid Git blob identity")
        if type(size) is not int or not 0 <= size <= MAX_FILE:
            raise CatalogError("Plugin file exceeds the size limit")
        if sha not in self.cache:
            if self.token:
                blob = self.json(API + "/git/blobs/" + sha)
                if blob.get("encoding") != "base64" or not isinstance(blob.get("content"), str):
                    raise CatalogError("Invalid Git blob encoding")
                data = base64.b64decode("".join(blob.get("content", "").split()), validate=True)
            else:
                path = quote(safe_path(entry["path"]), safe="/")
                data = get_bytes(RAW + "/" + self.revision + "/" + path, MAX_FILE)
            actual = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
            if len(data) != size or actual != sha:
                raise CatalogError("Downloaded file does not match its pinned Git blob")
            self.cache[sha] = data
        if len(self.cache[sha]) != size:
            raise CatalogError("Cached file size differs from the pinned Git blob")
        return self.cache[sha]

    def metadata(self, plugin_id):
        files = self.plugins.get(plugin_id)
        if files is None:
            raise CatalogError("Plugin not found in the official main/plugins directory")
        values = parse_metadata(self.blob(files["plugin.metadata"]), plugin_id)
        for required in ("plugin.conf", "lang/en-us.lang", values.get("ATM_PLUGIN_ENTRYPOINT", "plugin.sh")):
            if required not in files:
                raise CatalogError("Required plugin file missing: " + required)
        return values

    def search(self, query):
        query = query.casefold()
        rows = []
        for plugin_id, files in sorted(self.plugins.items()):
            values = self.metadata(plugin_id)
            name = values.get("ATM_PLUGIN_NAME_VALUE", plugin_id) or plugin_id
            version = values.get("ATM_PLUGIN_VERSION_VALUE", "-") or "-"
            description = values.get("ATM_PLUGIN_DESCRIPTION_VALUE", "-") or "-"
            paths = " ".join("plugins/" + plugin_id + "/" + relative for relative in files)
            references = " ".join((plugin_id, name, description, paths))
            if query in references.casefold():
                rows.append("\t".join(clean(field) for field in (plugin_id, name, version, description, self.revision)))
        return rows

    def download(self, plugin_id, root):
        if not ID.fullmatch(plugin_id):
            raise CatalogError("Invalid plugin ID")
        files = self.plugins.get(plugin_id)
        if files is None:
            raise CatalogError("Plugin not found in the official main/plugins directory")
        self.metadata(plugin_id)
        total = 0
        for relative, entry in files.items():
            safe_path(relative)
            if relative == ".atm-origin.json":
                raise CatalogError("Reserved plugin filename")
            size = entry.get("size")
            if entry.get("type") != "blob" or entry.get("mode") not in ("100644", "100755"):
                raise CatalogError("Plugin links and submodules are not supported")
            if type(size) is not int or not 0 <= size <= MAX_FILE:
                raise CatalogError("Invalid plugin file size")
            total += size
        if len(files) > 1000 or total > MAX_TOTAL:
            raise CatalogError("Plugin exceeds the download limit")
        root = Path(root).expanduser().resolve()
        target = root / plugin_id
        if os.path.lexists(target):
            raise CatalogError("Existing local plugin will not be overwritten")
        root.mkdir(parents=True, exist_ok=True)
        lock = root / (".atm-download-lock-" + plugin_id)
        try:
            lock.mkdir(mode=0o700)
        except FileExistsError:
            raise CatalogError("Another download holds this plugin lock: " + str(lock)) from None
        stage = None
        try:
            if os.path.lexists(target):
                raise CatalogError("Existing local plugin will not be overwritten")
            stage = Path(tempfile.mkdtemp(prefix=".atm-download-" + plugin_id + "-", dir=root))
            for relative, entry in sorted(files.items()):
                path = stage / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(self.blob(entry))
                path.chmod(0o755 if entry["mode"] == "100755" else 0o644)
                if path.suffix in (".sh", ".conf", ".metadata", ".lang"):
                    result = subprocess.run(["bash", "-n", str(path)], capture_output=True, timeout=10)
                    if result.returncode:
                        raise CatalogError("Bash syntax validation failed: " + relative)
            receipt = {"schema": 1, "source": SOURCE, "branch": "main", "commit": self.revision, "plugin": plugin_id,
                       "files": {name: {"sha": entry["sha"], "mode": entry["mode"]} for name, entry in files.items()}}
            (stage / ".atm-origin.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
            if os.path.lexists(target):
                raise CatalogError("Existing local plugin will not be overwritten")
            stage.rename(target)
            return target
        finally:
            if stage is not None and stage.exists():
                shutil.rmtree(stage)
            lock.rmdir()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="action", required=True)
    search = subparsers.add_parser("search")
    search.add_argument("query", nargs="?", default="")
    download = subparsers.add_parser("download")
    download.add_argument("plugin_id")
    download.add_argument("--root", required=True)
    download.add_argument("--revision")
    args = parser.parse_args()
    catalog = Catalog(getattr(args, "revision", None))
    if args.action == "search":
        for row in catalog.search(args.query):
            print(row)
    else:
        print(catalog.download(args.plugin_id, args.root))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TimeoutError, subprocess.SubprocessError) as error:
        print("Plugin catalog: " + str(error), file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)

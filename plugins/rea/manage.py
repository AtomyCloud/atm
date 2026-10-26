"""Scoped REA prefixes, ownership checks and data-only installation receipts."""

import json
import os
from pathlib import Path
import re
import shlex
import shutil
import sys
import tempfile
import uuid


MAGIC = "atm-rea-root-v1\n"
VERSION = re.compile(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\Z")


def version(value):
    if not isinstance(value, str) or not VERSION.fullmatch(value):
        raise ValueError("Expected an exact stable X.Y.Z version")
    return value


def regular_json(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError("Expected a regular JSON file: " + str(path))
    data = json.loads(path.read_text())
    if not isinstance(data, dict):
        raise ValueError("Expected a JSON object: " + str(path))
    return data


def root_path(value):
    path = Path(value)
    def unsafe(text):
        return any(ord(c) < 32 or ord(c) == 127 or c in ':"$`\\' for c in text)
    if not path.is_absolute() or unsafe(value):
        raise ValueError("Use an absolute path without control characters, colons or shell-expansion characters")
    if path.is_symlink():
        raise ValueError("The REA root cannot be a symlink")
    path = path.resolve()
    if unsafe(str(path)):
        raise ValueError("Resolved path contains shell-expansion characters")
    forbidden = {Path("/"), Path.home().resolve(), Path("/usr"), Path("/usr/local"),
                 Path("/etc"), Path("/var"), Path("/opt"), Path("/tmp"), Path("/home"),
                 Path(os.environ.get("ATM_APPS_DIR", str(Path.home() / "Apps"))).resolve(),
                 Path(os.environ.get("ATM_HOME", str(Path.home() / "Apps/atm"))).resolve()}
    if path in forbidden:
        raise ValueError("Choose a dedicated REA subdirectory")
    return path


def owned_root(root):
    marker = root / ".atm-rea-root"
    return not marker.is_symlink() and marker.is_file() and marker.read_text() == MAGIC


def child_dir(path):
    if path.is_symlink() or (path.exists() and not path.is_dir()):
        raise ValueError("Unsafe managed directory: " + str(path))


def managed(root, selected):
    version(selected)
    prefix = root / "versions" / selected
    child_dir(root / "versions")
    child_dir(prefix)
    data = regular_json(prefix / ".atm-rea-version.json")
    package = regular_json(prefix / "lib/node_modules/rea-agents/package.json")
    if data.get("version") != selected or package.get("name") != "rea-agents" or package.get("version") != selected:
        raise ValueError("REA package/ownership mismatch")
    launcher = prefix / "bin/rea"
    if launcher.is_symlink() or not launcher.is_file():
        raise ValueError("Missing owned REA launcher")
    return prefix


def current_version(root):
    if not owned_root(root):
        return ""
    current = root / "current"
    if not current.exists() and not current.is_symlink():
        return ""
    if not current.is_symlink():
        raise ValueError("Refusing an unmanaged current entry")
    target = os.readlink(current)
    if not target.startswith("versions/"):
        raise ValueError("Unmanaged current link")
    selected = target[len("versions/"):]
    managed(root, selected)
    return selected


def read_state(state):
    if not state.exists() and not state.is_symlink():
        return None
    data = regular_json(state)
    if data.get("schema") != 1 or not isinstance(data.get("root"), str):
        raise ValueError("Invalid REA receipt; inspect it before continuing")
    root_path(data["root"])
    return data


def atomic_json(path, data):
    if path.is_symlink():
        raise ValueError("Refusing a linked state file")
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".rea-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(data, stream)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def check_entries(root):
    child_dir(root / "bin")
    child_dir(root / "versions")
    current_version(root)
    entry = root / "bin/rea"
    if (entry.exists() or entry.is_symlink()) and (not entry.is_symlink() or os.readlink(entry) != "../current/bin/rea"):
        raise ValueError("Refusing to replace a foreign rea entry")


def begin(root):
    if root.exists():
        child_dir(root)
        if not owned_root(root) and any(root.iterdir()):
            raise ValueError("Destination is not empty or owned by ATM REA")
    else:
        root.mkdir(parents=True)
    if not owned_root(root):
        with (root / ".atm-rea-root").open("x") as stream:
            stream.write(MAGIC)
    check_entries(root)
    lock = root / ".atm-rea-lock"
    lock.mkdir(mode=0o700)
    stage = lock / "stage"
    stage.mkdir()
    print(stage)


def finish(root):
    lock = root / ".atm-rea-lock"
    if not owned_root(root):
        raise ValueError("Unknown root")
    child_dir(lock)
    if lock.exists():
        shutil.rmtree(lock)


def write_launcher(launcher, node, script):
    temporary = launcher.with_name(".rea-" + uuid.uuid4().hex)
    try:
        temporary.write_text("#!/bin/sh\nexport PATH=" + shlex.quote(str(node.parent)) + ':"$PATH"\nexec '
                             + shlex.quote(str(node)) + " " + shlex.quote(str(script)) + ' "$@"\n')
        temporary.chmod(0o755)
        os.replace(temporary, launcher)
    finally:
        if temporary.exists():
            temporary.unlink()


def activate(root, state, selected, node):
    check_entries(root)
    prefix = managed(root, selected)
    node = Path(node)
    script = prefix / "lib/node_modules/rea-agents/scripts/rea.mjs"
    if not node.is_absolute() or not os.access(node, os.X_OK) or not script.is_file() or script.is_symlink():
        raise ValueError("Missing selected Node or REA entrypoint")
    if prefix.resolve() not in script.resolve().parents:
        raise ValueError("Package entrypoint escapes prefix")
    write_launcher(prefix / "bin/rea", node, script)
    atomic_json(prefix / ".atm-rea-version.json", {"version": selected, "node": str(node)})
    (root / "bin").mkdir(exist_ok=True)
    entry = root / "bin/rea"
    if not entry.is_symlink():
        entry.symlink_to("../current/bin/rea")
    link = root / (".current-" + uuid.uuid4().hex)
    try:
        link.symlink_to("versions/" + selected)
        atomic_json(state, {"schema": 1, "root": str(root)})
        os.replace(link, root / "current")
    finally:
        if link.is_symlink():
            link.unlink()


def publish(root, state, selected, node):
    if not owned_root(root):
        raise ValueError("Unknown REA root")
    version(selected)
    check_entries(root)
    stage = root / ".atm-rea-lock/stage"
    package = regular_json(stage / "lib/node_modules/rea-agents/package.json")
    script = stage / "lib/node_modules/rea-agents/scripts/rea.mjs"
    if package.get("name") != "rea-agents" or package.get("version") != selected or not script.is_file() or script.is_symlink():
        raise ValueError("Downloaded package identity/entrypoint mismatch")
    if stage.resolve() not in script.resolve().parents:
        raise ValueError("Package entrypoint escapes prefix")
    node = Path(node)
    if not node.is_absolute() or not os.access(node, os.X_OK):
        raise ValueError("Missing selected Node executable")
    versions = root / "versions"
    versions.mkdir(exist_ok=True)
    dest = versions / selected
    if dest.exists() or dest.is_symlink():
        raise ValueError("Version already exists; refusing replacement")
    bin_dir = stage / "bin"
    child_dir(bin_dir)
    bin_dir.mkdir(exist_ok=True)
    launcher = bin_dir / "rea"
    if launcher.is_symlink() and launcher.resolve() == script.resolve():
        launcher.unlink()
    elif launcher.exists() or launcher.is_symlink():
        raise ValueError("Unexpected npm launcher")
    write_launcher(launcher, node, dest / "lib/node_modules/rea-agents/scripts/rea.mjs")
    atomic_json(stage / ".atm-rea-version.json", {"version": selected, "node": str(node)})
    os.rename(stage, dest)
    activate(root, state, selected, str(node))


def remove(root, state):
    if not owned_root(root):
        raise ValueError("Refusing to remove an unmanaged REA directory")
    if (root / ".atm-rea-lock").exists() or (root / ".atm-rea-lock").is_symlink():
        raise ValueError("An install/update lock exists; do not remove during installation")
    check_entries(root)
    versions = root / "versions"
    candidates = []
    if versions.exists():
        for prefix in versions.iterdir():
            candidates.append(managed(root, prefix.name))
    data = read_state(state)
    if data is not None and root_path(data["root"]) != root:
        raise ValueError("Receipt points at another installation")
    for prefix in candidates:
        shutil.rmtree(prefix)
    for entry in (root / "current", root / "bin/rea"):
        if entry.is_symlink():
            entry.unlink()
    if data is not None:
        state.unlink()


def main():
    action, *args = sys.argv[1:]
    if action == "release":
        print(version(json.load(sys.stdin)))
        return
    if action == "root":
        state = read_state(Path(args[0]))
        print(root_path(state["root"] if state else args[1]))
        return
    root = root_path(args[0])
    if action == "validate":
        print(root)
    elif action == "version":
        print(current_version(root))
    elif action == "begin":
        begin(root)
    elif action == "finish":
        finish(root)
    elif action == "publish":
        publish(root, Path(args[1]), version(args[2]), args[3])
    elif action == "activate":
        activate(root, Path(args[1]), version(args[2]), args[3])
    elif action == "remove":
        remove(root, Path(args[1]))
    else:
        raise ValueError("Unknown operation")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError) as error:
        print("REA: " + str(error), file=sys.stderr)
        sys.exit(1)

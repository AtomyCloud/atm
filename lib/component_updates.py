#!/usr/bin/env python3
"""Independent, fixed-origin ATM component updates; never execute fetched code."""

import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import uuid

from plugin_catalog import API, SOURCE, ID, SHA, MAX_FILE, MAX_TOTAL, Catalog, clean, core_path, parse_metadata, safe_path


SCHEMA = 1
UPDATES_SOURCE = "https://github.com/AtomyCloud/atm/tree/main"
CORE_UNITS = ("bin", "lib", "lang", "core-api.json")
STATES = {"current", "available", "modified", "unknown", "unmanaged", "removed", "blocked", "ahead", "diverged"}
UUID = re.compile(r"[0-9a-f]{32}\Z")


def component_source(component):
    return UPDATES_SOURCE if component == "core" else SOURCE


def read_json(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_TOTAL:
        raise ValueError("Unsafe JSON file: " + str(path))
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("Expected a JSON object")
    return data


def atomic_json(path, data):
    if path.is_symlink():
        raise ValueError("Refusing a linked state file")
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".atm-update-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(data, stream, sort_keys=True, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def component_id(value):
    if value != "core" and (not value.startswith("plugin:") or not ID.fullmatch(value[7:])):
        raise ValueError("Invalid component ID")
    return value


def descriptor(data):
    if not isinstance(data, dict) or not isinstance(data.get("sha"), str) or not SHA.fullmatch(data["sha"]) \
            or data.get("mode") not in ("100644", "100755"):
        raise ValueError("Invalid file fingerprint")
    return {"sha": data["sha"], "mode": data["mode"]}


def manifest(files, component):
    if not isinstance(files, dict) or not files or len(files) > 2000:
        raise ValueError("Invalid component manifest")
    result = {}
    for name, entry in files.items():
        safe_path(name)
        if name == ".atm-origin.json" or (component == "core" and not core_path(name)):
            raise ValueError("File outside component boundary")
        result[name] = descriptor(entry)
    names = set(result)
    if any(any(parent.as_posix() in names for parent in Path(name).parents if parent.as_posix() != ".") for name in names):
        raise ValueError("Conflicting file/directory paths")
    return result


def file_descriptor(path):
    fd = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_FILE:
            raise ValueError("Unsupported or oversized local file: " + str(path))
        data = stream.read(MAX_FILE + 1)
    if len(data) != info.st_size:
        raise ValueError("File changed while reading: " + str(path))
    return {"sha": hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest(),
            "mode": "100755" if info.st_mode & 0o111 else "100644"}


def snapshot(path):
    """Include unknown files and links for conflict detection; never follow links."""
    if not os.path.lexists(path):
        return None
    result = {}
    total = 0
    def visit(item, relative):
        nonlocal total
        if len(result) >= 10000:
            raise ValueError("Local component exceeds the file limit")
        info = item.lstat()
        if stat.S_ISLNK(info.st_mode):
            result[relative] = {"kind": "link", "target": os.readlink(item)}
        elif stat.S_ISDIR(info.st_mode):
            result[relative] = {"kind": "directory"}
            for child in sorted(item.iterdir()):
                if child.name == ".git":
                    raise ValueError("Nested Git checkout in component")
                visit(child, (relative + "/" if relative else "") + child.name)
        elif stat.S_ISREG(info.st_mode):
            total += info.st_size
            if total > 4 * MAX_TOTAL:
                raise ValueError("Local component exceeds the backup limit")
            result[relative] = dict(file_descriptor(item), kind="file")
        else:
            raise ValueError("Unsupported local special file")
    visit(path, "")
    return result


def copy_item(source, destination):
    if source.is_dir() and not source.is_symlink():
        shutil.copytree(source, destination, symlinks=True)
    elif source.is_symlink():
        destination.symlink_to(os.readlink(source))
    else:
        shutil.copy2(source, destination, follow_symlinks=False)


def git_root(path):
    for parent in (path, *path.parents):
        marker = parent / ".git"
        if os.path.lexists(marker):
            if parent == path or marker.is_symlink() or not marker.is_dir():
                return parent
            try:
                if next(marker.iterdir(), None) is not None:
                    return parent
            except OSError:
                return parent
    return None


def git_manifest(root, component):
    repository = git_root(root)
    if repository is None:
        return None
    env = dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_NO_LAZY_FETCH="1", GIT_TERMINAL_PROMPT="0")
    def git(*args):
        return subprocess.run(["git", "-C", str(repository), *args], env=env, check=True,
                              capture_output=True, timeout=8).stdout
    revision = git("rev-parse", "--verify", "HEAD").decode().strip()
    prefix = root.relative_to(repository).as_posix()
    prefix = "" if prefix == "." else prefix + "/"
    files = {}
    for record in git("ls-tree", "-rz", "--full-tree", revision, "--", prefix or ".").split(b"\0"):
        if not record:
            continue
        identity, raw_path = record.split(b"\t", 1)
        mode, kind, sha = identity.decode().split()
        path = raw_path.decode("utf-8")
        if not path.startswith(prefix):
            continue
        name = path[len(prefix):]
        if component == "core" and not core_path(name):
            continue
        if kind != "blob":
            raise ValueError("Git component includes a submodule")
        files[name] = {"mode": mode, "sha": sha}
    return {"schema": SCHEMA, "source": component_source(component), "branch": "main", "commit": revision,
            "component": component, "files": manifest(files, component)}


class Updates:
    def __init__(self, home, plugins, state, cache, factory=Catalog):
        self.home = Path(home).absolute()
        self.plugins = Path(plugins).absolute()
        if self.home.is_symlink() or self.plugins.is_symlink():
            raise ValueError("Linked installation roots cannot be updated")
        self.home = self.home.resolve()
        self.plugins = self.plugins.resolve()
        if self.plugins == self.home or self.plugins in self.home.parents \
                or any(self.home / unit == self.plugins or self.home / unit in self.plugins.parents for unit in CORE_UNITS[:3]):
            raise ValueError("CORE and plugin installation scopes must not overlap")
        identity = hashlib.sha256((str(self.home) + "\0" + str(self.plugins)).encode()).hexdigest()[:24]
        self.state = Path(state).absolute() / "component-updates" / identity
        self.cache = Path(cache).absolute() / "component-updates" / (identity + ".json")
        protected = [self.home / unit for unit in CORE_UNITS[:3]] + [self.root(name) for name in self.components()[1:]]
        if any(root == destination.resolve() or root in destination.resolve().parents
               for destination in (self.state, self.cache) for root in protected):
            raise ValueError("Update state/cache must be outside replaceable component directories")
        self.factory = factory
        self.catalogs = {}
        self.relations = {}

    def components(self):
        result = ["core"]
        if self.plugins.is_dir():
            for path in sorted(self.plugins.iterdir()):
                if ID.fullmatch(path.name) and os.path.lexists(path / "plugin.metadata"):
                    result.append("plugin:" + path.name)
        return result

    def root(self, component):
        component_id(component)
        return self.home if component == "core" else self.plugins / component[7:]

    def receipt_file(self, component):
        component_id(component)
        return self.state / "receipts" / (component.replace(":", "-") + ".json")

    def catalog(self, revision):
        if revision not in self.catalogs:
            self.catalogs[revision] = self.factory(revision)
        return self.catalogs[revision]

    def remote_files(self, remote, component):
        files = remote.core_files if component == "core" else remote.plugins.get(component[7:])
        if files is None:
            return None
        if component == "core":
            if not {"bin/atm", "lib/atm_core.sh", "lang/en-us.lang"}.issubset(files):
                raise ValueError("Incomplete remote CORE")
        else:
            remote.metadata(component[7:])
        result = manifest(files, component)
        if any(type(entry.get("size")) is not int or not 0 <= entry["size"] <= MAX_FILE for entry in files.values()):
            raise ValueError("Remote component exceeds the size limit")
        if sum(entry["size"] for entry in files.values()) > MAX_TOTAL:
            raise ValueError("Remote component exceeds the size limit")
        return result

    def baseline(self, component, remote=None):
        local = self.receipt_file(component)
        root = self.root(component)
        origin = root / (".atm-core-origin.json" if component == "core" else ".atm-origin.json")
        for path in (local, origin):
            if not os.path.lexists(path):
                continue
            data = read_json(path)
            if data.get("source") != component_source(component) or data.get("branch") != "main" or not SHA.fullmatch(str(data.get("commit", ""))):
                raise ValueError("Unverified component origin")
            recorded = data.get("component", "plugin:" + str(data.get("plugin", "")))
            if recorded != component:
                raise ValueError("Component receipt identity mismatch")
            files = data.get("files")
            if files is None:
                files = self.remote_files(self.catalog(data["commit"]), component)
            return dict(data, component=component, files=manifest(files, component))
        git = git_manifest(root, component)
        if git is not None:
            return git
        if remote is not None:
            files = self.remote_files(remote, component)
            if files and self.clean(component, files) and self.inventory(component) == files:
                data = {"schema": SCHEMA, "source": component_source(component), "branch": "main", "commit": remote.revision,
                        "component": component, "files": files}
                atomic_json(local, data)
                return data
        return None

    def inventory(self, component):
        root = self.root(component)
        result = {}
        paths = (root / name for name in CORE_UNITS) if component == "core" else (root,)
        for path in paths:
            prefix = path.name + "/" if component == "core" and path.is_dir() else ""
            entries = snapshot(path) or {}
            for relative, data in entries.items():
                if data["kind"] != "file":
                    if data["kind"] == "link":
                        raise ValueError("Unowned local link requires review")
                    continue
                name = prefix + relative if component == "core" and path.is_dir() else path.name if component == "core" else relative
                if name == ".atm-origin.json" or "__pycache__" in Path(name).parts or name.endswith(".pyc"):
                    continue
                result[name] = descriptor(data)
        return result

    def clean(self, component, files):
        root = self.root(component)
        if root.is_symlink():
            return False
        for name, expected in files.items():
            path = root / name
            if any(parent.is_symlink() for parent in (path, *path.parents) if parent != root and root in parent.parents):
                return False
            if not path.is_file() or path.is_symlink() or file_descriptor(path) != expected:
                return False
        return True

    def relationship(self, remote, revision):
        if revision == remote.revision:
            return "identical"
        key = (revision, remote.revision)
        if key not in self.relations:
            data = remote.json(API + "/compare/" + revision + "..." + remote.revision)
            value = data.get("status")
            if value not in ("ahead", "behind", "identical", "diverged"):
                raise ValueError("Cannot establish official revision ancestry")
            self.relations[key] = value
        return self.relations[key]

    def api(self, remote=None):
        if remote is None:
            path = self.home / "core-api.json"
            data = read_json(path) if os.path.lexists(path) else {"schema": 1, "plugin_api": 1}
        else:
            entry = remote.core_files.get("core-api.json")
            data = json.loads(remote.blob(entry)) if entry else {"schema": 1, "plugin_api": 1}
        if not isinstance(data, dict) or data.get("schema") != 1 or type(data.get("plugin_api")) is not int \
                or not 1 <= data["plugin_api"] <= 1000000:
            raise ValueError("Invalid CORE plugin API declaration")
        return data

    def required_api(self, values):
        value = values.get("ATM_PLUGIN_MIN_CORE_API_VALUE", "1")
        if not isinstance(value, str) or not re.fullmatch(r"[1-9][0-9]{0,5}", value):
            raise ValueError("Invalid plugin CORE API requirement")
        return int(value)

    def status(self, component, remote):
        base = self.baseline(component, remote)
        latest = self.remote_files(remote, component)
        if base is None:
            return "unmanaged", "No verified installation baseline"
        if not self.clean(component, base["files"]):
            return "modified", "Local tracked files differ from the installation baseline"
        if latest is None:
            return "removed", "No longer present in the official catalog; retained locally"
        if component != "core":
            required = self.required_api(remote.metadata(component[7:]))
            if required > self.api()["plugin_api"]:
                return "blocked", "Requires CORE plugin API " + str(required)
        if latest == base["files"]:
            return "current", "Component files match"
        relation = self.relationship(remote, base["commit"])
        if relation in ("behind", "diverged"):
            return ("ahead" if relation == "behind" else "diverged"), "Local origin is not behind official main"
        root = self.root(component)
        for name in latest.keys() - base["files"].keys():
            path = root / name
            if os.path.lexists(path) or any(parent.is_symlink() or (parent.exists() and not parent.is_dir())
                                          for parent in path.parents if parent != root and root in parent.parents):
                return "modified", "New upstream path collides with an untracked local entry: " + name
        if component == "core":
            api = self.api(remote)
            if api.get("update_protocol") != 1:
                return "blocked", "Remote CORE does not declare update protocol 1"
            for plugin in self.components()[1:]:
                values = parse_metadata((self.root(plugin) / "plugin.metadata").read_bytes(), plugin[7:])
                if self.required_api(values) > api["plugin_api"]:
                    return "blocked", "CORE API would break installed " + plugin
        return "available", "Verified component changes available"

    def check(self, ttl=0):
        if ttl and self.cache.exists():
            try:
                cached = self.read_cache(ttl)
                if cached:
                    return cached
            except (OSError, ValueError):
                pass
        rows = []
        revision = ""
        try:
            remote = self.factory()
            revision = remote.revision
            self.catalogs[revision] = remote
            for component in self.components():
                try:
                    status, detail = self.status(component, remote)
                except (OSError, ValueError, subprocess.SubprocessError) as error:
                    status, detail = "unknown", str(error)
                rows.append({"component": component, "status": status, "detail": detail})
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            rows = [{"component": name, "status": "unknown", "detail": str(error)} for name in self.components()]
        data = {"schema": SCHEMA, "home": str(self.home), "plugins": str(self.plugins), "source": UPDATES_SOURCE,
                "revision": revision, "checked_at": time.time(), "rows": rows}
        atomic_json(self.cache, data)
        return data

    def read_cache(self, ttl=300):
        if not self.cache.exists():
            return None
        data = read_json(self.cache)
        if data.get("schema") != SCHEMA or data.get("home") != str(self.home) or data.get("plugins") != str(self.plugins) \
                or data.get("source") != UPDATES_SOURCE or type(data.get("checked_at")) not in (int, float) \
                or not 0 <= time.time() - data["checked_at"] <= ttl:
            return None
        rows = data.get("rows")
        if not isinstance(rows, list) or len(rows) > 201:
            raise ValueError("Invalid update cache")
        for row in rows:
            component_id(row["component"])
            if row.get("status") not in STATES or not isinstance(row.get("detail"), str):
                raise ValueError("Invalid cached status")
        return data

    def units(self, component):
        return list(CORE_UNITS) if component == "core" else [component[7:]]

    def unit_parent(self, component):
        return self.home if component == "core" else self.plugins

    def validate_backup(self, backup):
        if not UUID.fullmatch(backup):
            raise ValueError("Invalid backup ID")
        path = self.state / "backups" / backup
        if path.is_symlink():
            raise ValueError("Linked backup refused")
        data = read_json(path / "metadata.json")
        component_id(data["component"])
        if data.get("schema") != SCHEMA or data.get("home") != str(self.home) or data.get("plugins") != str(self.plugins) \
                or set(data.get("before", {})) != set(self.units(data["component"])) \
                or set(data.get("after", {})) != set(self.units(data["component"])):
            raise ValueError("Backup belongs to another installation or has invalid scope")
        return path, data

    def recover(self, backup, interrupted=False):
        path, data = self.validate_backup(backup)
        component = data["component"]
        pending = self.home / ".atm-core-update-pending"
        if component == "core" and os.path.lexists(pending) and read_json(pending).get("backup") != str(path):
            raise ValueError("Another CORE transaction is pending; use its recovery command")
        parent = self.unit_parent(component)
        for name in self.units(component):
            current = snapshot(parent / name)
            allowed = (data["before"][name], data["after"][name], None) if interrupted else (data["after"][name],)
            if current not in allowed or snapshot(path / "before" / name) != data["before"][name]:
                raise ValueError("Recovery refused: current files or backup changed: " + name)
        stage = Path(tempfile.mkdtemp(prefix=".atm-recovery-", dir=parent))
        try:
            for name in self.units(component):
                if data["before"][name] is not None:
                    copy_item(path / "before" / name, stage / name)
            for name in self.units(component):
                target = parent / name
                if os.path.lexists(target):
                    os.replace(target, stage / ("discard-" + name))
                if data["before"][name] is not None:
                    os.replace(stage / name, target)
            receipt = self.receipt_file(component)
            if data.get("receipt_before") is None:
                if receipt.exists():
                    receipt.unlink()
            else:
                atomic_json(receipt, data["receipt_before"])
            data["phase"] = "recovered"
            atomic_json(path / "metadata.json", data)
            if component == "core":
                if pending.exists():
                    pending.unlink()
            if self.cache.exists():
                self.cache.unlink()
        finally:
            shutil.rmtree(stage)

    @contextlib.contextmanager
    def lock(self, recovery=False):
        self.state.mkdir(parents=True, exist_ok=True)
        path = self.state / "lock"
        if recovery and path.is_dir() and not path.is_symlink():
            owner = read_json(path / "owner.json").get("pid")
            if type(owner) is not int or owner <= 0:
                raise ValueError("Invalid update lock owner; inspect before recovery")
            try:
                os.kill(owner, 0)
            except ProcessLookupError:
                (path / "owner.json").unlink()
                path.rmdir()
        path.mkdir(mode=0o700)
        try:
            atomic_json(path / "owner.json", {"pid": os.getpid()})
            yield
        finally:
            (path / "owner.json").unlink()
            path.rmdir()

    def apply_one(self, component, remote):
        root = self.root(component)
        if git_root(root) is not None:
            raise ValueError("Selective updates refuse Git checkouts; --git updates the entire repository")
        status, detail = self.status(component, remote)
        if status != "available":
            raise ValueError(status + ": " + detail)
        base = self.baseline(component)
        latest = self.remote_files(remote, component)
        entries = remote.core_files if component == "core" else remote.plugins[component[7:]]
        parent = self.unit_parent(component)
        before = {name: snapshot(parent / name) for name in self.units(component)}
        backup_id = uuid.uuid4().hex
        backup = self.state / "backups" / backup_id
        backup.mkdir(parents=True, mode=0o700)
        (backup / "before").mkdir()
        (backup / "engine").mkdir()
        for name in ("component_updates.py", "plugin_catalog.py"):
            shutil.copy2(Path(__file__).parent / name, backup / "engine" / name)
        stage = Path(tempfile.mkdtemp(prefix=".atm-component-stage-", dir=parent))
        data = None
        changed = False
        retain_stage = False
        try:
            for name in self.units(component):
                source = parent / name
                if before[name] is not None:
                    if source.is_symlink():
                        raise ValueError("Linked component unit refused")
                    copy_item(source, backup / "before" / name)
                    copy_item(source, stage / name)
            stage_root = stage if component == "core" else stage / component[7:]
            for name in base["files"].keys() - latest.keys():
                (stage_root / name).unlink()
            for name, expected in latest.items():
                target = stage_root / name
                if any(parent_path.is_symlink() for parent_path in (target, *target.parents) if stage_root in parent_path.parents):
                    raise ValueError("Staging path includes a link")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(remote.blob(entries[name]))
                target.chmod(0o755 if expected["mode"] == "100755" else 0o644)
                if target.suffix in (".sh", ".conf", ".metadata", ".lang") or name == "bin/atm":
                    subprocess.run(["bash", "-n", str(target)], capture_output=True, check=True, timeout=10)
                elif target.suffix == ".py":
                    try:
                        compile(target.read_bytes(), str(target), "exec")
                    except SyntaxError:
                        raise ValueError("Python syntax validation failed: " + name) from None
            receipt = {"schema": SCHEMA, "source": component_source(component), "branch": "main", "commit": remote.revision,
                       "component": component, "files": latest}
            if component != "core":
                atomic_json(stage_root / ".atm-origin.json", dict(receipt, plugin=component[7:]))
            after = {name: snapshot(stage / name) for name in self.units(component)}
            receipt_path = self.receipt_file(component)
            recovery = ["python3", "-B", str(backup / "engine/component_updates.py"), "--home", str(self.home),
                        "--plugins", str(self.plugins), "--state", str(self.state.parent.parent),
                        "--cache", str(self.cache.parent.parent), "recover", backup_id, "--interrupted"]
            data = {"schema": SCHEMA, "home": str(self.home), "plugins": str(self.plugins), "component": component,
                    "revision": remote.revision, "before": before, "after": after, "phase": "prepared",
                    "receipt_before": read_json(receipt_path) if receipt_path.exists() else None,
                    "recovery_command": shlex.join(recovery)}
            atomic_json(backup / "metadata.json", data)
            if any(snapshot(parent / name) != before[name] for name in self.units(component)):
                raise ValueError("Local files changed while staging; update refused")
            pending = self.home / ".atm-core-update-pending"
            if component == "core":
                if os.path.lexists(pending):
                    raise ValueError("A CORE recovery is already pending")
                atomic_json(pending, {"backup": str(backup), "recovery_command": data["recovery_command"]})
            data["phase"] = "applying"
            atomic_json(backup / "metadata.json", data)
            changed = True
            for name in self.units(component):
                target = parent / name
                if os.path.lexists(target):
                    os.replace(target, stage / ("displaced-" + name))
                    if snapshot(stage / ("displaced-" + name)) != before[name]:
                        os.replace(stage / ("displaced-" + name), target)
                        retain_stage = True
                        raise ValueError("Local files changed during publication; retained stage: " + str(stage))
                if after[name] is not None:
                    os.replace(stage / name, target)
            if any(snapshot(parent / name) != after[name] for name in self.units(component)):
                raise ValueError("Published component verification failed")
            atomic_json(receipt_path, receipt)
            data["phase"] = "applied"
            atomic_json(backup / "metadata.json", data)
            if component == "core":
                pending.unlink()
            if self.cache.exists():
                self.cache.unlink()
            return backup_id
        except BaseException:
            if changed and data is not None:
                try:
                    self.recover(backup_id, interrupted=True)
                except BaseException:
                    retain_stage = True
                    raise
            raise
        finally:
            if not retain_stage:
                shutil.rmtree(stage)

    def apply(self, targets, revision):
        if "core" in targets and len(targets) != 1:
            raise ValueError("Update CORE separately and restart ATM before updating plugins")
        if os.path.lexists(self.home / ".atm-core-update-pending"):
            raise ValueError("CORE recovery is pending; no component updates are permitted")
        if not SHA.fullmatch(revision):
            raise ValueError("A checked, pinned official revision is required")
        remote = self.catalog(revision)
        results = []
        with self.lock():
            for component in targets:
                try:
                    if os.path.lexists(self.home / ".atm-core-update-pending"):
                        raise ValueError("CORE recovery is pending; update refused")
                    status, detail = self.status(component, remote)
                    if status != "available":
                        results.append({"component": component, "result": "skipped", "detail": status + ": " + detail})
                        continue
                    backup = self.apply_one(component, remote)
                    results.append({"component": component, "result": "updated", "detail": backup})
                except (OSError, ValueError, subprocess.SubprocessError) as error:
                    results.append({"component": component, "result": "failed", "detail": str(error)})
        return results

    def record(self, source):
        source = Path(source).resolve()
        for component in self.components():
            source_root = source if component == "core" else source / "plugins" / component[7:]
            base = git_manifest(source_root, component)
            if base is not None and self.clean(component, base["files"]):
                origin = self.root(component) / (".atm-core-origin.json" if component == "core" else ".atm-origin.json")
                atomic_json(origin, base)

    def verify(self, component, revision):
        component_id(component)
        if component not in self.components() or not SHA.fullmatch(revision):
            raise ValueError("An installed component and exact official commit are required")
        latest = self.factory()
        if self.relationship(latest, revision) not in ("ahead", "identical"):
            raise ValueError("Baseline must be on the official main history")
        remote = self.catalog(revision)
        files = self.remote_files(remote, component)
        if files is None or not self.clean(component, files):
            raise ValueError("Installed files do not match the selected official baseline; no receipt was changed")
        data = {"schema": SCHEMA, "source": component_source(component), "branch": "main", "commit": revision,
                "component": component, "files": files}
        with self.lock():
            if os.path.lexists(self.home / ".atm-core-update-pending"):
                raise ValueError("Resolve the pending CORE recovery first")
            atomic_json(self.receipt_file(component), data)
            if self.cache.exists():
                self.cache.unlink()

    def changes(self):
        data = self.read_cache()
        if data is None or not SHA.fullmatch(data.get("revision", "")):
            raise ValueError("Run an update check first")
        remote = self.catalog(data["revision"])
        changes = []
        for component in self.components():
            base = self.baseline(component)
            latest = self.remote_files(remote, component)
            if base is None or latest is None:
                continue
            for name in sorted(base["files"].keys() | latest.keys()):
                old, new = base["files"].get(name), latest.get(name)
                if old != new:
                    changes.append((component, "added" if old is None else "removed" if new is None else "changed", name))
        return changes


def rows(data):
    for row in data["rows"]:
        fields = (row["component"], row["status"], data.get("revision") or "-", row["detail"])
        print("\t".join(clean(field) for field in fields))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--home", required=True)
    parser.add_argument("--plugins", required=True)
    parser.add_argument("--state", required=True)
    parser.add_argument("--cache", required=True)
    sub = parser.add_subparsers(dest="action", required=True)
    check = sub.add_parser("check")
    check.add_argument("--ttl", type=int, default=0)
    sub.add_parser("cached")
    apply = sub.add_parser("apply")
    apply.add_argument("target")
    apply.add_argument("--revision", required=True)
    recover = sub.add_parser("recover")
    recover.add_argument("backup")
    recover.add_argument("--interrupted", action="store_true")
    record = sub.add_parser("record")
    record.add_argument("--source", required=True)
    sub.add_parser("backups")
    sub.add_parser("changes")
    verify = sub.add_parser("verify")
    verify.add_argument("component")
    verify.add_argument("--revision", required=True)
    args = parser.parse_args()
    engine = Updates(args.home, args.plugins, args.state, args.cache)
    if os.environ.get("ATM_DRY_RUN") == "1":
        print("DRY-RUN: component updates " + args.action + "; no network, probes or writes")
        return
    if args.action == "check":
        rows(engine.check(args.ttl))
    elif args.action == "cached":
        data = engine.read_cache()
        if data:
            rows(data)
    elif args.action == "apply":
        targets = [name for name in engine.components() if name != "core"] if args.target == "all-plugins" else [component_id(args.target)]
        results = engine.apply(targets, args.revision)
        for row in results:
            print("\t".join(clean(field) for field in (row["component"], row["result"], row["detail"])))
        if any(row["result"] == "failed" for row in results):
            sys.exit(1)
    elif args.action == "recover":
        if git_root(engine.home) is not None or git_root(engine.plugins) is not None:
            raise ValueError("Recovery refuses Git checkouts")
        with engine.lock(recovery=True):
            engine.recover(args.backup, args.interrupted)
        print("Recovered " + args.backup + "; restart ATM")
    elif args.action == "record":
        engine.record(args.source)
    elif args.action == "changes":
        for row in engine.changes():
            print("\t".join(row))
    elif args.action == "verify":
        engine.verify(args.component, args.revision)
        print("Verified baseline: " + args.component + "@" + args.revision + "; component files were not changed")
    else:
        for path in sorted((engine.state / "backups").glob("*/metadata.json")):
            _, data = engine.validate_backup(path.parent.name)
            print("\t".join(clean(str(value)) for value in (path.parent.name, data["component"], data["phase"], data["revision"])))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print("ATM updates: " + clean(str(error)), file=sys.stderr)
        sys.exit(1)

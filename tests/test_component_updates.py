"""Independent updates against an in-memory official repository; no fetched code runs."""

import hashlib
import json
import multiprocessing
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import quote

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "lib"))
import plugin_catalog as catalog
import component_updates as updates

A, B = "a" * 40, "b" * 40


class UpdateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-component-updates-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "ATM with spaces"
        self.home.mkdir()
        self.plugins = self.home / "plugins"
        self.marker = self.root / "fetched-code-must-not-run"
        self.files = {"bin/atm": (b"#!/bin/bash\nexit 0\n", "100755"),
                      "lib/atm_core.sh": (b'ATM_CORE_PLUGIN_API=1\n', "100644"),
                      "lang/en-us.lang": (b'ATM_TEST="Test"\n', "100644"),
                      "core-api.json": (b'{"schema":1,"plugin_api":1,"update_protocol":1}\n', "100644")}
        for name in ("alpha", "beta"):
            self.files.update({
                f"plugins/{name}/plugin.metadata": (f'ATM_PLUGIN_ID="{name}"\nATM_PLUGIN_NAME_VALUE="{name}"\nATM_PLUGIN_VERSION_VALUE="0.0.1"\nATM_PLUGIN_ENTRYPOINT="plugin.sh"\n'.encode(), "100644"),
                f"plugins/{name}/plugin.sh": (f'#!/bin/bash\ntouch "{self.marker}"\n'.encode(), "100755"),
                f"plugins/{name}/plugin.conf": (b'ATM_TEST_DEFAULT="original"\n', "100644"),
                f"plugins/{name}/lang/en-us.lang": (b'ATM_TEST_LABEL="Test"\n', "100644")})
        self.versions = {A: dict(self.files), B: dict(self.files)}
        self.remote = A
        self.relationship = "ahead"
        self.requests = []
        self.on_blob = None
        for name, (data, mode) in self.files.items():
            path = self.home / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            path.chmod(0o755 if mode == "100755" else 0o644)
        env = patch.dict(os.environ, {"GITHUB_TOKEN": ""})
        env.start()
        self.addCleanup(env.stop)
        transport = patch.object(catalog, "get_bytes", side_effect=self.fetch)
        transport.start()
        self.addCleanup(transport.stop)
        self.engine = self.new_engine()

    def new_engine(self):
        return updates.Updates(self.home, self.plugins, self.root / "state", self.root / "cache")

    def entries(self, revision):
        result = []
        for path, (data, mode) in self.versions[revision].items():
            sha = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
            result.append({"path": path, "type": "blob", "mode": mode, "size": len(data), "sha": sha})
        return result

    def fetch(self, url, limit, token=""):
        self.requests.append(url)
        if url == catalog.API + "/commits/main":
            return json.dumps({"sha": self.remote}).encode()
        for revision in (A, B):
            if url == catalog.API + "/git/trees/" + revision + "?recursive=1":
                return json.dumps({"truncated": False, "tree": self.entries(revision)}).encode()
            for name, (data, _) in self.versions[revision].items():
                if url == catalog.RAW + "/" + revision + "/" + quote(name, safe="/"):
                    if self.on_blob:
                        self.on_blob(name)
                    return data
        if url.startswith(catalog.API + "/compare/"):
            return json.dumps({"status": self.relationship}).encode()
        raise AssertionError("Unexpected network endpoint: " + url)

    def statuses(self, engine=None):
        return {row["component"]: row["status"] for row in (engine or self.engine).check()["rows"]}

    def bootstrap(self):
        before = updates.snapshot(self.home)
        self.assertEqual(set(self.statuses().values()), {"current"})
        self.assertEqual(updates.snapshot(self.home), before)

    def advance(self, changes):
        for name, value in changes.items():
            if value is None:
                self.versions[B].pop(name, None)
            else:
                data = value.encode() if isinstance(value, str) else value
                self.versions[B][name] = (data, self.versions[B].get(name, (None, "100644"))[1])
        self.remote = B
        self.engine = self.new_engine()

    def test_plugin_only_commit_does_not_flag_core_or_unchanged_plugins(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# new implementation\n"})
        self.requests.clear()
        self.assertEqual(self.statuses(), {"core": "current", "plugin:alpha": "available", "plugin:beta": "current"})
        self.assertEqual(self.requests.count(catalog.API + "/commits/main"), 1)
        self.assertFalse(self.marker.exists())
        self.assertIn(("plugin:alpha", "changed", "plugin.sh"), self.engine.changes())

    def test_single_update_preserves_other_components_and_unknown_user_files(self):
        self.bootstrap()
        extra = self.plugins / "alpha/user notes.txt"
        extra.write_text("keep\n")
        beta = updates.snapshot(self.plugins / "beta")
        core = updates.snapshot(self.home / "lib")
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# update\n", "plugins/alpha/new.txt": "new"})
        result = self.engine.apply(["plugin:alpha"], B)[0]
        self.assertEqual(result["result"], "updated", result)
        self.assertEqual(extra.read_text(), "keep\n")
        self.assertEqual(updates.snapshot(self.plugins / "beta"), beta)
        self.assertEqual(updates.snapshot(self.home / "lib"), core)
        self.assertTrue((self.plugins / "alpha/new.txt").is_file())
        self.assertFalse(self.marker.exists())
        self.assertEqual(self.statuses()["plugin:alpha"], "current")
        self.engine.recover(result["detail"])
        self.assertFalse((self.plugins / "alpha/new.txt").exists())
        self.assertEqual(extra.read_text(), "keep\n")

    def test_local_configuration_edits_and_new_file_collisions_block_replacement(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# updated\n", "plugins/beta/new.txt": "official"})
        (self.plugins / "alpha/plugin.conf").write_text('ATM_CUSTOM="my settings"\n')
        (self.plugins / "beta/new.txt").write_text("my unrelated file")
        before = updates.snapshot(self.home)
        self.assertEqual(self.statuses()["plugin:alpha"], "modified")
        self.assertEqual(self.statuses()["plugin:beta"], "modified")
        results = self.engine.apply(["plugin:alpha", "plugin:beta"], B)
        self.assertEqual([row["result"] for row in results], ["skipped", "skipped"])
        self.assertEqual(updates.snapshot(self.home), before)

    def test_legacy_receipt_is_verified_and_missing_baseline_is_not_invented(self):
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# updated\n"})
        receipt = {"source": catalog.SOURCE, "branch": "main", "commit": A, "plugin": "alpha"}
        (self.plugins / "alpha/.atm-origin.json").write_text(json.dumps(receipt))
        states = self.statuses()
        self.assertEqual(states["plugin:alpha"], "available")
        self.assertEqual(states["core"], "current")
        (self.plugins / "alpha/.atm-origin.json").unlink()
        self.assertEqual(self.statuses()["plugin:alpha"], "unmanaged")

    def test_removed_plugin_is_reported_without_deletion(self):
        self.bootstrap()
        self.versions[B] = {name: value for name, value in self.versions[B].items() if not name.startswith("plugins/alpha/")}
        self.remote = B
        self.assertEqual(self.statuses()["plugin:alpha"], "removed")
        self.assertEqual(self.engine.apply(["plugin:alpha"], B)[0]["result"], "skipped")
        self.assertTrue((self.plugins / "alpha/plugin.sh").exists())

    def test_batch_reports_failure_and_success_independently(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "if then\n", "plugins/beta/plugin.sh": "#!/bin/bash\n# good update\n"})
        alpha = updates.snapshot(self.plugins / "alpha")
        result = self.engine.apply(["plugin:alpha", "plugin:beta"], B)
        self.assertEqual([row["result"] for row in result], ["failed", "updated"], result)
        self.assertEqual(updates.snapshot(self.plugins / "alpha"), alpha)
        self.assertFalse(self.marker.exists())

    def test_integrity_failure_and_local_change_during_staging_publish_nothing(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# update\n"})
        original = updates.snapshot(self.plugins / "alpha")
        with patch.object(catalog.Catalog, "blob", side_effect=ValueError("integrity mismatch")):
            result = self.engine.apply(["plugin:alpha"], B)[0]
        self.assertEqual(result["result"], "failed")
        self.assertEqual(updates.snapshot(self.plugins / "alpha"), original)
        self.engine = self.new_engine()
        def concurrent(name):
            if name == "plugins/alpha/plugin.sh":
                (self.plugins / "alpha/local.txt").write_text("new user data")
        self.on_blob = concurrent
        result = self.engine.apply(["plugin:alpha"], B)[0]
        self.assertEqual(result["result"], "failed", result)
        self.assertEqual((self.plugins / "alpha/local.txt").read_text(), "new user data")
        self.assertEqual((self.plugins / "alpha/plugin.sh").read_bytes(), self.files["plugins/alpha/plugin.sh"][0])

    def test_core_then_plugin_api_gate_and_core_boundary(self):
        self.bootstrap()
        self.advance({"core-api.json": '{"schema":1,"plugin_api":2,"update_protocol":1}',
                      "plugins/alpha/plugin.metadata": self.files["plugins/alpha/plugin.metadata"][0] + b'ATM_PLUGIN_MIN_CORE_API_VALUE="2"\n'})
        before = updates.snapshot(self.plugins)
        self.assertEqual(self.statuses()["plugin:alpha"], "blocked")
        self.assertEqual(self.statuses()["core"], "available")
        result = self.engine.apply(["core"], B)[0]
        self.assertEqual(result["result"], "updated", result)
        self.assertEqual(updates.snapshot(self.plugins), before)
        self.assertFalse((self.home / ".atm-core-update-pending").exists())
        self.assertEqual(self.statuses()["plugin:alpha"], "available")
        self.assertEqual(self.engine.apply(["plugin:alpha"], B)[0]["result"], "updated")

    def test_core_protocol_and_installed_plugin_requirements_prevent_unsafe_update(self):
        self.bootstrap()
        self.advance({"core-api.json": '{"schema":1,"plugin_api":1}', "lib/new.sh": "# changed\n"})
        self.assertEqual(self.statuses()["core"], "blocked")
        self.versions[B]["core-api.json"] = (b'{"schema":1,"plugin_api":1,"update_protocol":1}', "100644")
        (self.plugins / "alpha/plugin.metadata").write_bytes(self.files["plugins/alpha/plugin.metadata"][0] + b'ATM_PLUGIN_MIN_CORE_API_VALUE="2"\n')
        self.engine = self.new_engine()
        self.assertEqual(self.statuses()["core"], "blocked")

    def test_core_publication_failure_rolls_back_and_clears_guard(self):
        self.bootstrap()
        self.advance({"lib/atm_core.sh": "ATM_CORE_PLUGIN_API=1\n# updated\n"})
        before = {name: updates.snapshot(self.home / name) for name in updates.CORE_UNITS}
        replace = os.replace
        failed = False
        def once(source, target):
            nonlocal failed
            if Path(target) == self.home / "lang" and not failed:
                failed = True
                raise OSError("simulated publication failure")
            return replace(source, target)
        with patch.object(updates.os, "replace", side_effect=once):
            result = self.engine.apply(["core"], B)[0]
        self.assertEqual(result["result"], "failed", result)
        self.assertEqual({name: updates.snapshot(self.home / name) for name in updates.CORE_UNITS}, before)
        self.assertFalse((self.home / ".atm-core-update-pending").exists())

    def test_interrupted_core_update_uses_standalone_recovery_and_reclaims_dead_lock(self):
        self.bootstrap()
        self.advance({"lib/atm_core.sh": "ATM_CORE_PLUGIN_API=1\n# updated\n"})
        before = {name: updates.snapshot(self.home / name) for name in updates.CORE_UNITS}
        def crash():
            replace = os.replace
            def terminate(source, target):
                if Path(target) == self.home / "lang":
                    os._exit(91)
                return replace(source, target)
            updates.os.replace = terminate
            self.engine.apply(["core"], B)
        process = multiprocessing.get_context("fork").Process(target=crash)
        process.start()
        process.join(10)
        self.assertEqual(process.exitcode, 91)
        pending = updates.read_json(self.home / ".atm-core-update-pending")
        command = shlex.split(pending["recovery_command"])
        result = subprocess.run(command, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual({name: updates.snapshot(self.home / name) for name in updates.CORE_UNITS}, before)
        self.assertFalse((self.home / ".atm-core-update-pending").exists())

    def test_recovery_refuses_user_edits_after_update(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# update\n"})
        result = self.engine.apply(["plugin:alpha"], B)[0]
        (self.plugins / "alpha/plugin.conf").write_text("# my later edit\n")
        before = updates.snapshot(self.plugins / "alpha")
        with self.assertRaisesRegex(ValueError, "Recovery refused"):
            self.engine.recover(result["detail"])
        self.assertEqual(updates.snapshot(self.plugins / "alpha"), before)

    def test_git_checkout_ahead_and_diverged_states_are_not_silently_replaced(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# update\n"})
        for relationship, status in (("behind", "ahead"), ("diverged", "diverged")):
            self.relationship = relationship
            self.engine = self.new_engine()
            self.assertEqual(self.statuses()["plugin:alpha"], status)
        self.relationship = "ahead"
        self.engine = self.new_engine()
        (self.home / ".git").mkdir()
        (self.home / ".git/HEAD").write_text("ref: refs/heads/main\n")
        before = updates.snapshot(self.plugins / "alpha")
        result = self.engine.apply(["plugin:alpha"], B)[0]
        self.assertEqual(result["result"], "failed")
        self.assertIn("Git checkouts", result["detail"])
        self.assertEqual(updates.snapshot(self.plugins / "alpha"), before)

    def test_cache_ttl_offline_and_invalid_receipts_never_claim_current(self):
        self.bootstrap()
        before = len(self.requests)
        self.engine.check(ttl=300)
        self.assertEqual(len(self.requests), before)
        data = updates.read_json(self.engine.cache)
        data["checked_at"] -= 301
        updates.atomic_json(self.engine.cache, data)
        with patch.object(self.engine, "factory", side_effect=catalog.CatalogError("offline")):
            self.assertEqual(set(self.statuses().values()), {"unknown"})
        self.assertEqual({row["status"] for row in self.engine.read_cache()["rows"]}, {"unknown"})
        receipt = self.engine.receipt_file("plugin:alpha")
        data = updates.read_json(receipt)
        data["files"]["../escape"] = next(iter(data["files"].values()))
        updates.atomic_json(receipt, data)
        self.assertEqual(self.statuses()["plugin:alpha"], "unknown")

    def test_python_validation_failure_does_not_stop_later_components(self):
        self.bootstrap()
        self.advance({"plugins/alpha/bad.py": "if :\n", "plugins/beta/plugin.sh": "#!/bin/bash\n# updated\n"})
        before = updates.snapshot(self.home / "lib")
        results = self.engine.apply(["plugin:alpha", "plugin:beta"], B)
        self.assertEqual([row["result"] for row in results], ["failed", "updated"])
        self.assertIn("Python syntax", results[0]["detail"])
        self.assertEqual(updates.snapshot(self.home / "lib"), before)

    def test_owned_deletions_modes_and_disabled_plugins_are_updated(self):
        for revision in (A, B):
            self.versions[revision]["plugins/alpha/old.txt"] = (b"old file", "100644")
        (self.plugins / "alpha/old.txt").write_text("old file")
        self.bootstrap()
        self.advance({"plugins/alpha/old.txt": None, "plugins/alpha/new.sh": "#!/bin/sh\nexit 0\n",
                      "plugins/beta/plugin.sh": "#!/bin/bash\n# updated\n"})
        self.versions[B]["plugins/alpha/new.sh"] = (b"#!/bin/sh\nexit 0\n", "100755")
        with patch.dict(os.environ, {"ATM_PLUGIN_ALPHA_ENABLED": "0"}):
            result = self.engine.apply(self.engine.components()[1:], B)
        self.assertEqual([row["result"] for row in result], ["updated", "updated"])
        self.assertFalse((self.plugins / "alpha/old.txt").exists())
        self.assertEqual((self.plugins / "alpha/new.sh").stat().st_mode & 0o777, 0o755)

    def test_scope_overlap_links_and_newer_core_requirement_are_refused(self):
        with self.assertRaisesRegex(ValueError, "overlap"):
            updates.Updates(self.home, self.home / "lib/plugins", self.root / "state", self.root / "cache")
        with self.assertRaisesRegex(ValueError, "state/cache"):
            updates.Updates(self.home, self.plugins, self.home / "lib/state", self.root / "cache")
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# updated\n"})
        config = self.plugins / "alpha/plugin.conf"
        config.unlink()
        outside = self.root / "outside.txt"
        outside.write_text("untouched")
        config.symlink_to(outside)
        self.assertEqual(self.statuses()["plugin:alpha"], "modified")
        self.assertEqual(self.engine.apply(["plugin:alpha"], B)[0]["result"], "skipped")
        self.assertEqual(outside.read_text(), "untouched")

    def test_loader_skips_future_api_plugin_without_sourcing_its_code(self):
        path = self.plugins / "alpha/plugin.metadata"
        path.write_bytes(path.read_bytes() + b'ATM_PLUGIN_MIN_CORE_API_VALUE="2"\n')
        script = r'''
set -Eeuo pipefail
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/lib/atm_plugin_loader.sh"
source "$ATM_HOME/lang/en-us.lang"
atm_plugin_arrays_init
atm_plugin_load_metadata "$TEST_PLUGIN"
[[ ${#ATM_PLUGIN_IDS[@]} == 0 ]]
'''
        env = dict(os.environ, ATM_HOME=str(REPO), TEST_PLUGIN=str(path.parent), ATM_LANG="en-us")
        result = subprocess.run(["bash", "-c", script], env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("requires CORE API 2", result.stderr)
        self.assertFalse(self.marker.exists())

    def test_active_update_lock_and_corrupt_backup_refuse_changes(self):
        self.bootstrap()
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# updated\n"})
        with self.engine.lock():
            with self.assertRaises(OSError):
                self.engine.apply(["plugin:alpha"], B)
        result = self.engine.apply(["plugin:alpha"], B)[0]
        before = updates.snapshot(self.plugins / "alpha")
        backup = self.engine.state / "backups" / result["detail"]
        (backup / "before/alpha/plugin.conf").write_text("tampered")
        with self.assertRaisesRegex(ValueError, "Recovery refused"):
            self.engine.recover(result["detail"])
        self.assertEqual(updates.snapshot(self.plugins / "alpha"), before)

    def test_explicit_old_baseline_verification_requires_matching_official_files(self):
        self.advance({"plugins/alpha/plugin.sh": "#!/bin/bash\n# update\n"})
        self.assertEqual(self.statuses()["plugin:alpha"], "unmanaged")
        self.engine.verify("plugin:alpha", A)
        self.assertEqual(self.statuses()["plugin:alpha"], "available")
        receipt = self.engine.receipt_file("plugin:alpha").read_bytes()
        (self.plugins / "alpha/plugin.conf").write_text("# local customization\n")
        with self.assertRaisesRegex(ValueError, "do not match"):
            self.engine.verify("plugin:alpha", A)
        self.assertEqual(self.engine.receipt_file("plugin:alpha").read_bytes(), receipt)
        self.relationship = "diverged"
        self.engine = self.new_engine()
        with self.assertRaisesRegex(ValueError, "official main history"):
            self.engine.verify("plugin:beta", A)

    def test_project_api_constant_matches_the_published_declaration(self):
        import re
        declaration = json.loads((REPO / "core-api.json").read_text())
        match = re.search(r"^ATM_CORE_PLUGIN_API=([0-9]+)$", (REPO / "lib/atm_core.sh").read_text(), re.M)
        self.assertIsNotNone(match)
        self.assertEqual(int(match[1]), declaration["plugin_api"])
        self.assertEqual(declaration["update_protocol"], 1)

    def test_git_detection_ignores_empty_ancestor_but_protects_direct_and_real_markers(self):
        ancestor = self.root / ".git"
        ancestor.mkdir()
        self.assertIsNone(updates.git_root(self.home))
        (ancestor / "HEAD").write_text("ref: refs/heads/main\n")
        self.assertEqual(updates.git_root(self.home), self.root)
        (ancestor / "HEAD").unlink()
        (self.home / ".git").mkdir()
        self.assertEqual(updates.git_root(self.home), self.home)

    def test_portable_setup_records_only_clean_git_source_components(self):
        source = self.root / "source"
        shutil.copytree(self.home, source)
        env = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL="/dev/null",
                   GIT_AUTHOR_NAME="ATM Test", GIT_AUTHOR_EMAIL="atm@example.invalid",
                   GIT_COMMITTER_NAME="ATM Test", GIT_COMMITTER_EMAIL="atm@example.invalid")
        for args in (("init", "-q"), ("add", "."), ("commit", "-qm", "baseline")):
            subprocess.run(["git", "-C", str(source), *args], env=env, check=True, capture_output=True)
        (self.plugins / "alpha/plugin.conf").write_text("# custom\n")
        self.engine.record(source)
        self.assertTrue((self.home / ".atm-core-origin.json").exists())
        self.assertTrue((self.plugins / "beta/.atm-origin.json").exists())
        self.assertFalse((self.plugins / "alpha/.atm-origin.json").exists())

    def test_real_cli_dry_run_and_pending_guard_are_write_and_network_free(self):
        personal = self.root / "personal"
        personal.mkdir()
        env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
        env.update(HOME=str(personal), ATM_APPS_DIR=str(self.root / "Apps"), ATM_STATE_DIR=str(self.root / "cli-state"),
                   ATM_CACHE_DIR=str(self.root / "cli-cache"), ATM_CONFIG_DIR=str(self.root / "cli-config"),
                   ATM_LANG="de", ATM_UPDATES_AUTO_CHECK="0")
        before = updates.snapshot(self.root)
        for args in (("updates", "check"), ("updates", "core"), ("updates", "all"), ("updates", "plugin", "rea"),
                     ("plugins", "check-updates"), ("plugins", "update", "--all"), ("self-update", "--check")):
            result = subprocess.run(["bash", str(REPO / "bin/atm"), "--dry-run", *args], env=env,
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("DRY-RUN", result.stdout)
            self.assertEqual(updates.snapshot(self.root), before)
        (self.home / "bin/atm").write_bytes((REPO / "bin/atm").read_bytes())
        (self.home / ".atm-core-update-pending").write_text("{}")
        result = subprocess.run(["bash", str(self.home / "bin/atm"), "--version"], env=env,
                                capture_output=True, text=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("normal startup is blocked", result.stderr)


if __name__ == "__main__":
    unittest.main()

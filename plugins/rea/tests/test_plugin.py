"""REA lifecycle/dependency tests using disposable HOME and fake npm/Node/APT."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[3]
HELPER = REPO / "plugins/rea/manage.py"
PRELUDE = r'''
set -Eeuo pipefail
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_manifest.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/lang/en-us.lang"
source "$ATM_HOME/plugins/rea/plugin.conf"
source "$ATM_HOME/plugins/rea/plugin.sh"
clear() { :; }
atm_prompt_continue() { :; }
'''
NPM = r'''#!/usr/bin/python3
import json, os
from pathlib import Path
import sys
args = sys.argv[1:]
if args == ["--version"]:
    print("11.20.0")
    sys.exit(0)
with open(os.environ["TEST_ACTIONS"], "a") as stream:
    stream.write(json.dumps(args) + "\n")
if args[0] == "view":
    if os.environ.get("TEST_FAIL") == "view": sys.exit(1)
    print(json.dumps(os.environ.get("TEST_VERSION", "6.8.0")))
elif args[0] == "install":
    if os.environ.get("TEST_FAIL") == "install": sys.exit(1)
    prefix = Path(args[args.index("--prefix") + 1])
    package = prefix / "lib/node_modules/rea-agents"
    (package / "scripts").mkdir(parents=True)
    selected = args[-1].split("@")[-1]
    if os.environ.get("TEST_FAIL") == "identity": selected = "0.0.0"
    (package / "package.json").write_text(json.dumps({"name": "rea-agents", "version": selected}))
    (package / "scripts/rea.mjs").write_text("// mocked REA\n")
    (prefix / "bin").mkdir()
    (prefix / "bin/rea").symlink_to("../lib/node_modules/rea-agents/scripts/rea.mjs")
else:
    sys.exit(99)
'''


class PluginTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-rea-tests-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.apps = self.root / "Apps with spaces"
        self.prefix = self.apps / "rea"
        self.tools = self.root / "runtime tools"
        self.tools.mkdir()
        self.make_tool("node", '''#!/bin/sh
if [ "$1" = --version ]; then printf 'v26.12.1\\n'; else printf 'RUNTIME26\\n'; printf '%s\\n' "$@"; fi
''')
        self.make_tool("npm", NPM)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
        self.env.update(ATM_HOME=str(REPO), HOME=str(self.home), ATM_APPS_DIR=str(self.apps),
                        ATM_STATE_DIR=str(self.root / "state"), ATM_CACHE_DIR=str(self.root / "cache"),
                        ATM_CONFIG_DIR=str(self.root / "config"), ATM_LANG="en-us", TERM="dumb",
                        PATH=str(self.tools) + ":/usr/bin:/bin", TEST_ACTIONS=str(self.home / "actions"))

    def make_tool(self, name, source):
        target = self.tools / name
        target.write_text(source)
        target.chmod(0o755)
        return target

    def run_shell(self, script, input=""):
        return subprocess.run(["/bin/bash", "-c", PRELUDE + script], env=self.env, cwd=REPO,
                              input=input, capture_output=True, text=True, timeout=10)

    def assert_ok(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def snapshot(self):
        return {str(p.relative_to(self.root)): ("link", os.readlink(p)) if p.is_symlink()
                else ("file", p.read_bytes()) if p.is_file() else ("directory",)
                for p in self.root.rglob("*")}

    def install(self, extra=""):
        result = self.run_shell("atm_rea_install " + extra + "\n")
        self.assert_ok(result)
        return result

    def test_install_update_launcher_and_removal_preserve_data_and_external_programs(self):
        external = self.home / ".local/bin/rea"
        external.parent.mkdir(parents=True)
        external.write_text("external CLI, untouched\n")
        settings = self.home / ".config/agent/settings.json"
        settings.parent.mkdir(parents=True)
        settings.write_text('{"rea": "keep registration"}\n')
        self.install()
        self.assertEqual(os.readlink(self.prefix / "current"), "versions/6.8.0")
        actions = [json.loads(line) for line in (self.home / "actions").read_text().splitlines()]
        self.assertIn("--registry=https://registry.npmjs.org", actions[0])
        for option in ("--engine-strict", "--ignore-scripts", "--no-audit", "--no-fund"):
            self.assertIn(option, actions[1])
        self.assertEqual(actions[1][-1], "rea-agents@6.8.0")
        self.assertIn("ATM_CURRENT_VERSION=6.8.0", (self.root / "state/manifests/rea.manifest").read_text())
        self.assertEqual(json.loads((self.root / "state/rea/install.json").read_text())["root"], str(self.prefix))
        self.env["TEST_VERSION"] = "6.9.0"
        self.install("--update")
        self.assertEqual(os.readlink(self.prefix / "current"), "versions/6.9.0")
        self.assertTrue((self.prefix / "versions/6.8.0").is_dir())
        probe = subprocess.run([str(self.prefix / "bin/rea"), "analyze-javascript-application", "target with spaces"],
                               env=dict(self.env, PATH="/usr/bin:/bin"), capture_output=True, text=True, timeout=5)
        self.assert_ok(probe)
        self.assertIn("RUNTIME26", probe.stdout)
        self.assertIn("target with spaces", probe.stdout)
        note = self.prefix / "my-analysis.txt"
        note.write_text("retain unrelated user data\n")
        self.assert_ok(self.run_shell("atm_rea_uninstall\n", "y\n"))
        self.assertFalse((self.prefix / "current").exists())
        self.assertEqual(list((self.prefix / "versions").iterdir()), [])
        self.assertTrue(note.is_file())
        self.assertEqual(external.read_text(), "external CLI, untouched\n")
        self.assertEqual(settings.read_text(), '{"rea": "keep registration"}\n')
        self.assertFalse((self.root / "state/rea/install.json").exists())

    def test_custom_destination_persists_for_update_status_path_and_remove(self):
        custom = self.root / "Other Apps/REA custom's folder"
        self.env["CUSTOM_ROOT"] = str(custom)
        self.install('--path "$CUSTOM_ROOT"')
        self.assertFalse(self.prefix.exists())
        result = self.run_shell("atm_rea_status\natm_rea_path_entries\n")
        self.assert_ok(result)
        self.assertIn(str(custom / "bin"), result.stdout)
        priority = self.run_shell(r'''
source "$ATM_HOME/lib/atm_plugin_loader.sh"
source "$ATM_HOME/lib/atm_path.sh"
atm_plugin_arrays_init
atm_plugin_load_metadata "$ATM_HOME/plugins/rea"
atm_path_collect_entries
''')
        self.assert_ok(priority)
        self.assertEqual(priority.stdout.splitlines()[:2], [str(custom / "bin"), str(self.home / ".local/bin")])
        self.env["TEST_VERSION"] = "6.9.0"
        self.install("--update")
        self.assertEqual(os.readlink(custom / "current"), "versions/6.9.0")
        self.assert_ok(self.run_shell("atm_rea_uninstall\n", "yes\n"))

    def test_failed_updates_keep_active_version_and_clean_staging(self):
        self.install()
        receipt = (self.root / "state/rea/install.json").read_bytes()
        manifest = (self.root / "state/manifests/rea.manifest").read_bytes()
        self.env["TEST_VERSION"] = "6.9.0"
        for failure in ("view", "install", "identity"):
            with self.subTest(failure=failure):
                self.env["TEST_FAIL"] = failure
                result = self.run_shell("atm_rea_install --update\n")
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("REA installed/updated:", result.stdout)
                self.assertEqual(os.readlink(self.prefix / "current"), "versions/6.8.0")
                self.assertEqual((self.root / "state/rea/install.json").read_bytes(), receipt)
                self.assertEqual((self.root / "state/manifests/rea.manifest").read_bytes(), manifest)
                self.assertFalse((self.prefix / ".atm-rea-lock").exists())

    def test_same_version_update_refreshes_a_removed_runtime_without_reinstall(self):
        self.install()
        alternative = self.root / "new runtime"
        alternative.mkdir()
        for name in ("node", "npm"):
            shutil.copy2(self.tools / name, alternative / name)
        (self.tools / "node").unlink()
        self.env["PATH"] = str(alternative) + ":" + self.env["PATH"]
        self.install("--update")
        marker = json.loads((self.prefix / "versions/6.8.0/.atm-rea-version.json").read_text())
        self.assertEqual(marker["node"], str(alternative / "node"))
        probe = subprocess.run([str(self.prefix / "bin/rea"), "--help"], env=self.env,
                               capture_output=True, text=True, timeout=5)
        self.assert_ok(probe)
        actions = [json.loads(line) for line in (self.home / "actions").read_text().splitlines()]
        self.assertEqual(sum(action[0] == "install" for action in actions), 1)

    def test_dependency_and_removal_cancellation_make_no_changes(self):
        for input in ("n\n", "", "\n"):
            before = self.snapshot()
            result = self.run_shell("atm_rea_ps_ready() { return 1; }\natm_rea_install\n", input)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("procps", result.stdout)
            self.assertEqual(before, self.snapshot())
        self.install()
        for input in ("n\n", "", "\n"):
            before = self.snapshot()
            self.assert_ok(self.run_shell("atm_rea_uninstall\n", input))
            self.assertEqual(before, self.snapshot())

    def test_dependency_repair_prompts_and_rechecks_apt_and_node(self):
        result = self.run_shell(r'''
ready=0
atm_rea_ps_ready() { ((ready)); }
atm_rea_select_runtime() { ATM_REA_NODE_PATH=/mock/node; ATM_REA_NPM_PATH=/mock/npm; ((ready)); }
atm_rea_is_apt() { return 0; }
sudo() { [[ "$1" == --validate ]] && return 0; shift; "$@"; }
apt-get() { printf 'APT:%s\n' "$*"; ready=1; }
atm_rea_dependencies
''', "y\n")
        self.assert_ok(result)
        self.assertIn("APT:--no-remove install procps", result.stdout)
        result = self.run_shell(r'''
ready=0
atm_rea_select_runtime() { ATM_REA_NODE_PATH=/mock/node; ATM_REA_NPM_PATH=/mock/npm; ((ready)); }
atm_node_install() { printf 'NODE:%s\n' "$*"; ready=1; }
atm_rea_dependencies
''', "yes\n")
        self.assert_ok(result)
        self.assertIn("NODE:--location atm --major 26", result.stdout)

    def test_dependency_repair_failures_abort_without_install(self):
        for failure in ("apt", "sudo", "unsupported"):
            script = r'''
atm_rea_ps_ready() { return 1; }
atm_rea_is_apt() { [[ "$FAILURE" != unsupported ]]; }
sudo() { [[ "$FAILURE" != sudo ]] || return 1; [[ "$1" == --validate ]] && return 0; shift; "$@"; }
apt-get() { return 1; }
atm_rea_install
'''
            self.env["FAILURE"] = failure
            before = self.snapshot()
            result = self.run_shell(script, "y\n")
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(before, self.snapshot())

    def test_supported_node_boundaries_and_atm_fallback(self):
        for observed, expected in (("22.23.3", False), ("22.23.4", True), ("22.24.0", True),
                                   ("24.22.0", False), ("24.22.1", True), ("25.9.0", False),
                                   ("26.12.0", False), ("26.12.1", True), ("28.0.0", True),
                                   ("26.12.1-rc.1", False), ("garbage", False)):
            result = self.run_shell('atm_rea_node_supported "' + observed + '"\n')
            self.assertEqual(result.returncode == 0, expected, observed)
        atm_bin = self.apps / "node/26.12.1/bin"
        atm_bin.mkdir(parents=True)
        shutil.copy2(self.tools / "node", atm_bin / "node")
        shutil.copy2(self.tools / "npm", atm_bin / "npm")
        (self.apps / "node/current").symlink_to(atm_bin.parent)
        self.make_tool("node", "#!/bin/sh\nprintf 'v22.22.3\\n'\n")
        result = self.run_shell("atm_rea_select_runtime\nprintf '%s\\n' \"$ATM_REA_NODE_PATH\"\n")
        self.assert_ok(result)
        self.assertEqual(result.stdout.strip(), str(atm_bin / "node"))

    def test_missing_node_plugin_or_failed_node_repair_does_not_install_rea(self):
        for repair in ("", "atm_node_install() { return 1; }\n"):
            before = self.snapshot()
            result = self.run_shell("atm_rea_select_runtime() { return 1; }\n" + repair + "atm_rea_install\n", "y\n")
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(before, self.snapshot())

    def test_invalid_paths_versions_foreign_entries_and_corrupt_receipts_are_preserved(self):
        for args in ("--path relative", "--path /", '--path "$HOME"', "--version ../escape", "--version 1.2.3-rc.1", "--unknown"):
            before = self.snapshot()
            result = self.run_shell("atm_rea_install " + args + "\n")
            self.assertNotEqual(result.returncode, 0, args)
            self.assertEqual(before, self.snapshot(), args)
        for name in ('bad"quote', 'bad$(touch marker)', 'bad`command`', 'bad\\slash', 'bad:colon'):
            self.env["REJECT_ROOT"] = str(self.home / name)
            before = self.snapshot()
            result = self.run_shell('atm_rea_install --path "$REJECT_ROOT"\n')
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(before, self.snapshot())
        self.prefix.mkdir(parents=True)
        foreign = self.prefix / "important.txt"
        foreign.write_text("keep\n")
        result = self.run_shell("atm_rea_install\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(foreign.read_text(), "keep\n")
        state = self.root / "state/rea/install.json"
        state.parent.mkdir(parents=True)
        state.write_text("$(touch must-not-run)\n")
        before = self.snapshot()
        result = self.run_shell("atm_rea_install\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, self.snapshot())

    def test_removal_refuses_foreign_version_or_link_and_lock(self):
        self.install()
        foreign = self.prefix / "versions/foreign-notes"
        foreign.mkdir()
        before = self.snapshot()
        result = self.run_shell("atm_rea_uninstall\n", "y\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, self.snapshot())
        foreign.rmdir()
        lock = self.prefix / ".atm-rea-lock"
        lock.mkdir()
        before = self.snapshot()
        result = self.run_shell("atm_rea_uninstall\n", "y\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, self.snapshot())
        lock.rmdir()
        entry = self.prefix / "bin/rea"
        entry.unlink()
        entry.write_text("foreign executable\n")
        before = self.snapshot()
        result = self.run_shell("atm_rea_uninstall\n", "y\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, self.snapshot())

    def test_picker_and_menu_keep_selection_visible_and_handle_eof(self):
        result = self.run_shell("atm_rea_choose_path\n", "2\n/tmp/rea path\n")
        self.assert_ok(result)
        self.assertEqual(result.stdout, "/tmp/rea path\n")
        self.assertIn("Choose destination", result.stderr)
        for input in ("b\n", "", "2\n\n", "oops\n"):
            result = self.run_shell("atm_rea_choose_path\n", input)
            self.assertNotEqual(result.returncode, 0)
        result = self.run_shell("atm_rea_menu\n", "4\nb\n")
        self.assert_ok(result)
        self.assertIn("4) Check / install missing dependencies", result.stdout)
        self.assert_ok(self.run_shell("atm_rea_menu\n"))

    def test_actual_cli_discovery_and_no_write_dry_run(self):
        self.make_tool("node", "#!/bin/sh\nprintf 'UNEXPECTED_NODE\\n' >&2\nexit 99\n")
        self.make_tool("npm", "#!/bin/sh\nprintf 'UNEXPECTED_NPM\\n' >&2\nexit 99\n")
        for locale in ("en-us", "de"):
            self.env["ATM_LANG"] = locale
            for args in (("plugins", "list"), ("install", "rea"), ("install", "rea", "--update"),
                         ("install", "rea", "--dependencies"), ("uninstall", "rea")):
                before = self.snapshot()
                result = subprocess.run(["/bin/bash", str(REPO / "bin/atm"), "--dry-run", *args],
                                        env=self.env, cwd=REPO, capture_output=True, text=True, timeout=10)
                self.assert_ok(result)
                self.assertNotIn("UNEXPECTED_", result.stdout + result.stderr)
                self.assertNotIn("[missing:", result.stdout + result.stderr)
                self.assertEqual(before, self.snapshot())
                if args[0] == "plugins":
                    self.assertIn("REA: Reverse Engineer Anything", result.stdout)


if __name__ == "__main__":
    unittest.main()

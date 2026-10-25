"""Node version selection regressions using disposable installations and a PTY."""

import os
from pathlib import Path
import pty
import select
import signal
import subprocess
import tempfile
import time
import unittest


REPO = Path(__file__).resolve().parents[3]
PRELUDE = r'''
set -Eeuo pipefail
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_manifest.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/lang/en-us.lang"
source "$ATM_HOME/plugins/node/plugin.conf"
source "$ATM_HOME/plugins/node/plugin.sh"
source "$ATM_HOME/plugins/node/lang/en-us.lang"
clear() { :; }
'''


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-node-selection-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.apps = self.root / "Apps with spaces"
        self.node_root = self.apps / "node"
        for version in ("24.16.0", "26.2.0"):
            binary = self.node_root / version / "bin/node"
            binary.parent.mkdir(parents=True)
            binary.write_text('#!/bin/sh\nprintf "v' + version + '\\n"\n')
            binary.chmod(0o755)
        (self.node_root / "current").symlink_to(self.node_root / "24.16.0")
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
        self.env.update(ATM_HOME=str(REPO), HOME=str(self.home), ATM_APPS_DIR=str(self.apps),
                        ATM_STATE_DIR=str(self.root / "state"), ATM_CACHE_DIR=str(self.root / "cache"),
                        ATM_CONFIG_DIR=str(self.root / "config"), ATM_LANG="en-us", TERM="dumb")
        self.env["ATM_NODE_SYSTEM_BINARY"] = str(self.root / "absent-system-node")

    def run_shell(self, script, input=""):
        return subprocess.run(["bash", "-c", PRELUDE + script], input=input,
                              env=self.env, cwd=REPO, capture_output=True, text=True, timeout=5)

    def test_picker_displays_prompt_before_input_in_command_substitution(self):
        master, slave = pty.openpty()
        process = subprocess.Popen(["bash", "-c", PRELUDE + r'''
version="$(atm_node_pick_installed_version 'CHOOSE_INSTALLED_NODE')"
printf '\nSELECTED=%s\n' "$version"
'''], stdin=slave, stdout=slave, stderr=slave, env=self.env, cwd=REPO,
                                   close_fds=True, start_new_session=True)
        os.close(slave)
        output = b""
        try:
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline and b"2) Node.js 26.2.0" not in output:
                if select.select([master], [], [], 0.1)[0]:
                    output += os.read(master, 8192)
            self.assertIn(b"CHOOSE_INSTALLED_NODE", output, "Picker waits for input while its prompt is hidden")
            self.assertIn(b"2) Node.js 26.2.0", output)
            os.write(master, b"2\n")
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline and b"SELECTED=26.2.0" not in output:
                if select.select([master], [], [], 0.1)[0]:
                    try:
                        output += os.read(master, 8192)
                    except OSError:
                        break
            self.assertIn(b"SELECTED=26.2.0", output)
            self.assertEqual(process.wait(timeout=2), 0)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                process.wait(timeout=2)
            os.close(master)

    def test_picker_stdout_contains_only_selected_version(self):
        for choice in ("2", "26.2.0", "v26.2.0", "node-v26.2.0"):
            with self.subTest(choice=choice):
                result = self.run_shell("atm_node_pick_installed_version 'PICK'\n", choice + "\n")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(result.stdout, "26.2.0\n")
                self.assertIn("PICK", result.stderr)

    def test_invalid_cancel_empty_eof_and_no_installed_versions(self):
        script = "if value=\"$(atm_node_pick_installed_version 'PICK')\"; then exit 99; fi\n[[ -z \"$value\" ]]\n"
        for choice in ("b\n", "B\n", "\n", "", "8\n", "08\n", "999999999999999999999\n", "25.1.0\n", "oops\n"):
            with self.subTest(choice=choice):
                result = self.run_shell(script, choice)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        result = self.run_shell('ATM_NODE_INSTALL_ROOT="$HOME/empty"\n' + script)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("No Node.js versions", result.stderr)

    def test_main_option_five_switches_current_and_updates_manifest(self):
        result = self.run_shell("atm_node_menu\n", "5\n2\nxb\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.node_root / "current").resolve(), self.node_root / "26.2.0")
        self.assertIn("Using Node.js: 26.2.0", result.stdout)
        manifest = (self.root / "state/manifests/node.manifest").read_text()
        self.assertIn("ATM_CURRENT_VERSION=26.2.0\n", manifest)

    def test_main_cancel_invalid_and_eof_return_cleanly(self):
        for choice in ("b", "", "8", "08", "25.1.0", "oops"):
            with self.subTest(choice=choice):
                result = self.run_shell("atm_node_menu\n", "5\n" + choice + "\nxb\n")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        for input in ("", "5\n"):
            result = self.run_shell("atm_node_menu\n", input)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.node_root / "current").resolve(), self.node_root / "24.16.0")
        self.assertFalse((self.root / "state").exists())

    def test_major_menu_switch_and_remove_and_main_remove(self):
        result = self.run_shell("ATM_DRY_RUN=1 atm_node_major_menu 26 v26.x\n", "c\n2\nxb\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Using Node.js: 26.2.0", result.stdout)
        result = self.run_shell("ATM_DRY_RUN=1 atm_node_major_menu 26 v26.x\n", "r\n2\nxb\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Node.js version removed: 26.2.0", result.stdout)
        result = self.run_shell("atm_node_menu\n", "6\n2\nxb\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.node_root / "26.2.0").exists())
        self.assertTrue((self.node_root / "24.16.0/bin/node").exists())

    def test_switch_dry_run_changes_no_files_and_executes_no_node(self):
        for binary in self.node_root.glob("*/bin/node"):
            if binary.parent.parent.name != "current":
                binary.write_text("#!/bin/sh\nprintf 'UNEXPECTED_NODE_CALL\\n' >&2\nexit 99\n")
        def snapshot():
            return {str(p.relative_to(self.root)): ("link", os.readlink(p)) if p.is_symlink()
                    else ("file", p.read_bytes()) if p.is_file() else ("directory",)
                    for p in self.root.rglob("*")}
        before = snapshot()
        result = self.run_shell("ATM_DRY_RUN=1 atm_node_use 26.2.0\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("UNEXPECTED_NODE_CALL", result.stdout + result.stderr)
        self.assertEqual(before, snapshot())


if __name__ == "__main__":
    unittest.main()

"""Node origin detection and system-install tests; no real sudo/APT is executed."""

import os
from pathlib import Path
import subprocess
import unittest

import test_selection as selection


MOCKS = r'''
atm_node_require_apt() { return 0; }
sudo() {
    printf 'sudo %s\n' "$*" >> "$HOME/actions"
    if [[ "$1" == --validate ]]; then [[ "${FAIL_SUDO:-0}" == 0 ]]; return; fi
    [[ "$1" == -- ]] || return 99
    shift
    "$@"
}
apt-get() {
    printf 'apt-get %s\n' "$*" >> "$HOME/actions"
    [[ "${FAIL_APT:-}" != "$1" ]] || return 100
    case "$*" in update|'--no-remove install nodejs npm') return 0 ;; *) return 99 ;; esac
}
curl() { printf 'UNEXPECTED_DOWNLOAD\n' >&2; return 99; }
'''


class SystemTests(unittest.TestCase):
    setUp = selection.SelectionTests.setUp
    run_shell = selection.SelectionTests.run_shell

    def prepare_binaries(self):
        system = self.root / "system bin/node"
        npm = self.root / "system bin/npm"
        local = self.home / ".local/bin/node"
        for binary, output in ((system, "v18.19.1"), (npm, "9.2.0"), (local, "v22.22.3")):
            binary.parent.mkdir(parents=True, exist_ok=True)
            binary.write_text('#!/bin/sh\nprintf "' + output + '\\n"\n')
            binary.chmod(0o755)
        self.env.update(ATM_NODE_SYSTEM_BINARY=str(system), ATM_NODE_SYSTEM_NPM=str(npm))
        return system, npm, local

    def snapshot(self):
        return {str(p.relative_to(self.root)): ("link", os.readlink(p)) if p.is_symlink()
                else ("file", p.read_bytes()) if p.is_file() else ("directory",)
                for p in self.root.rglob("*")}

    def test_detects_all_origins_paths_and_invalid_or_missing_versions(self):
        system, npm, local = self.prepare_binaries()
        result = self.run_shell("atm_node_show_installations\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        for text in ("ATM: 24.16.0", "User local: 22.22.3", "System package: 18.19.1", str(local), str(system),
                     "Command selected by PATH:"):
            self.assertIn(text, result.stdout)
        local.write_text('#!/bin/sh\nprintf "invalid output\\n"\n')
        result = self.run_shell("atm_node_status\n")
        self.assertIn("User local: version unavailable", result.stdout)
        local.unlink()
        system.unlink()
        result = self.run_shell("atm_node_status\n")
        self.assertNotIn("User local:", result.stdout)
        self.assertNotIn("System package:", result.stdout)

    def test_location_choice_defaults_cancel_and_version_flags(self):
        for input, expected in (("\n", "atm\n"), ("1\n", "atm\n"), ("2\n", "system\n")):
            result = self.run_shell("atm_node_choose_location\n", input)
            self.assertEqual(result.stdout, expected)
            self.assertIn("Choose installation location", result.stderr)
        for input in ("b\n", "oops\n", ""):
            result = self.run_shell("if atm_node_choose_location; then exit 99; fi\n", input)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        for flags in ("--location system --version 26.2.0", "--location=system --major 26", "--location local", "--bad"):
            result = self.run_shell(MOCKS + 'if atm_node_install ' + flags + '; then exit 99; fi\n')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertFalse((self.home / "actions").exists())

    def test_system_install_requires_confirmation_and_preserves_portable_and_local(self):
        system, npm, local = self.prepare_binaries()
        before = self.snapshot()
        for answer in ("n\n", ""):
            result = self.run_shell(MOCKS + "atm_node_install --location system\n", answer)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(before, self.snapshot())
        result = self.run_shell(MOCKS + "atm_node_install --location system\n", "y\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        actions = (self.home / "actions").read_text()
        self.assertIn("apt-get update\n", actions)
        self.assertIn("apt-get --no-remove install nodejs npm\n", actions)
        manifest = self.root / "state/manifests/node_system.manifest"
        self.assertIn("ATM_CURRENT_VERSION=18.19.1", manifest.read_text())
        result = self.run_shell('source "$ATM_MANIFEST_DIR/node_system.manifest"\n[[ "$ATM_CURRENT_PATH" == "$ATM_NODE_SYSTEM_BINARY" ]]\n')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.root / "state/manifests/node.manifest").exists())
        self.assertEqual((self.node_root / "current").resolve(), self.node_root / "24.16.0")
        self.assertEqual(local.read_bytes(), before[str(local.relative_to(self.root))][1])

    def test_update_install_verification_and_sudo_failures_never_report_success(self):
        system, npm, local = self.prepare_binaries()
        for failure in ("update", "--no-remove"):
            result = self.run_shell(MOCKS + 'FAIL_APT=' + failure + '\natm_node_install --location system\n', "y\n")
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("System Node.js package installed/updated:", result.stdout)
            self.assertFalse((self.root / "state").exists())
        npm.unlink()
        result = self.run_shell(MOCKS + "atm_node_install --location system\n", "y\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "state").exists())
        if os.geteuid() != 0:
            (self.home / "actions").unlink()
            result = self.run_shell(MOCKS + "FAIL_SUDO=1\natm_node_install --location system\n", "y\n")
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("apt-get ", (self.home / "actions").read_text())

    def test_detection_and_system_dry_run_are_write_and_execution_free(self):
        self.prepare_binaries()
        before = self.snapshot()
        script = MOCKS + r'''
timeout() { printf 'UNEXPECTED_VERSION_CALL\n' >&2; return 99; }
ATM_DRY_RUN=1 atm_node_show_installations
ATM_DRY_RUN=1 atm_node_install --location system
'''
        result = self.run_shell(script)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("UNEXPECTED_VERSION_CALL", result.stdout + result.stderr)
        self.assertIn("apt-get --no-remove install nodejs npm", result.stdout)
        self.assertEqual(before, self.snapshot())

    def test_main_menu_offers_location_and_system_install(self):
        self.prepare_binaries()
        default = self.run_shell(MOCKS + "atm_node_menu\n", "8\n\nxb\n")
        self.assertEqual(default.returncode, 0, default.stdout + default.stderr)
        self.assertFalse((self.home / "actions").exists())
        portable_manifest = (self.root / "state/manifests/node.manifest").read_bytes()
        result = self.run_shell(MOCKS + "atm_node_menu\n", "8\n2\ny\nxb\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("8) Install Node.js (ATM / system package)", result.stdout)
        self.assertIn("System Node.js package installed/updated:", result.stdout)
        self.assertEqual(portable_manifest, (self.root / "state/manifests/node.manifest").read_bytes())

    def test_unsupported_system_fails_without_packages_or_portable_fallback(self):
        before = self.snapshot()
        result = self.run_shell(r'''
uname() { printf 'Darwin\n'; }
apt-get() { printf 'UNEXPECTED_APT\n'; return 99; }
if atm_node_install --location system; then exit 99; fi
''', "y\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("supports Linux Debian/Ubuntu/Zorin", result.stderr)
        self.assertNotIn("UNEXPECTED_APT", result.stdout + result.stderr)
        self.assertEqual(before, self.snapshot())

    def test_actual_cli_system_and_fresh_portable_dry_run_create_nothing(self):
        tools = self.root / "blockers"
        tools.mkdir()
        for name in ("sudo", "apt-get", "curl", "node"):
            path = tools / name
            path.write_text("#!/bin/sh\nprintf 'UNEXPECTED_COMMAND\\n' >&2\nexit 99\n")
            path.chmod(0o755)
        self.env["PATH"] = str(tools) + os.pathsep + self.env.get("PATH", os.defpath)
        before = self.snapshot()
        for args in (("--location", "system"), ("--location", "atm", "--version", "25.9.0")):
            result = subprocess.run(["bash", str(selection.REPO / "bin/atm"), "--dry-run", "install", "node", *args],
                                    env=self.env, cwd=selection.REPO, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn("UNEXPECTED_COMMAND", result.stdout + result.stderr)
            self.assertIn("DRY-RUN:", result.stdout)
            self.assertEqual(before, self.snapshot())


if __name__ == "__main__":
    unittest.main()

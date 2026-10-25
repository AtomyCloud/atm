"""PATH activation with a competing local Node, real shells, and the actual CLI."""

import os
from pathlib import Path
import shutil
import subprocess
import unittest

import test_selection as selection


class PathTests(unittest.TestCase):
    setUp = selection.SelectionTests.setUp
    run_shell = selection.SelectionTests.run_shell

    def prepare_profiles(self):
        local_node = self.home / ".local/bin/node"
        local_node.parent.mkdir(parents=True)
        external = self.root / "external/bin/node"
        external.parent.mkdir(parents=True)
        external.write_text('#!/bin/sh\nprintf "v22.22.3\\n"\n')
        external.chmod(0o755)
        local_node.symlink_to(external)
        self.env["PATH"] = str(local_node.parent) + ":/usr/bin:/bin"
        for name in (".profile", ".bashrc", ".zshrc"):
            (self.home / name).write_text("# keep outside ATM block\n")
        result = self.run_shell(r'''
ATM_PLUGIN_PATH_PRIORITY[node]=default
atm_path_apply_user_profiles "$(atm_path_generate_block)"
''')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return local_node, external

    def snapshot(self):
        return {str(p.relative_to(self.root)): ("link", os.readlink(p)) if p.is_symlink()
                else ("file", p.read_bytes()) if p.is_file() else ("directory",)
                for p in self.root.rglob("*")}

    def test_actual_cli_switch_refreshes_profiles_and_preserves_external_runtime(self):
        local_node, external = self.prepare_profiles()
        before = (os.readlink(local_node), external.read_bytes())
        result = subprocess.run(["bash", str(selection.REPO / "bin/atm"), "use", "node", "26.2.0"],
                                env=self.env, cwd=selection.REPO, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("already-open terminal", result.stdout)
        for name in (".profile", ".bashrc", ".zshrc"):
            profile = self.home / name
            content = profile.read_text()
            self.assertEqual(content.count("# >>> ATM - Atomy Tools Modules >>>"), 1)
            self.assertIn("# keep outside ATM block", content)
            for shell in ("bash", "zsh"):
                executable = shutil.which(shell)
                if not executable:
                    continue
                with self.subTest(profile=name, shell=shell):
                    env = dict(self.env, TEST_PROFILE=str(profile))
                    probe = subprocess.run([executable, "-c", '. "$TEST_PROFILE"; hash -r; command -v node; node --version'],
                                           env=env, capture_output=True, text=True, timeout=5)
                    self.assertEqual(probe.returncode, 0, probe.stdout + probe.stderr)
                    self.assertEqual(probe.stdout.splitlines(), [str(self.node_root / "current/bin/node"), "v26.2.0"])
        self.assertEqual(before, (os.readlink(local_node), external.read_bytes()))
        self.assertFalse((self.home / ".local/bin/npm").exists())

    def test_printed_refresh_command_corrects_cached_parent_then_switch_tracks_current(self):
        self.prepare_profiles()
        result = self.run_shell(r'''
node --version
hash node
output="$(bash "$ATM_HOME/bin/atm" use node 26.2.0)"
node --version
refresh="$(printf '%s\n' "$output" | sed -n '/^  export PATH=/p; /^  hash -r$/p')"
[[ "$refresh" == *'export PATH='* && "$refresh" == *'hash -r'* ]]
eval "$refresh"
node --version
bash "$ATM_HOME/bin/atm" use node 24.16.0 >/dev/null
node --version
''')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["v22.22.3", "v22.22.3", "v26.2.0", "v24.16.0"])

    def test_existing_portable_install_activates_path_and_probes_npm_with_selected_node(self):
        self.prepare_profiles()
        npm = self.node_root / "26.2.0/bin/npm"
        npm.write_text('#!/usr/bin/env node\n')
        npm.chmod(0o755)
        result = self.run_shell('atm_node_install --location atm --version 26.2.0\n')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("v22.22.3", result.stdout)
        self.assertEqual(result.stdout.count("v26.2.0"), 2)
        result = self.run_shell('. "$HOME/.bashrc"\nnode --version\n')
        self.assertEqual(result.stdout, "v26.2.0\n")

    def test_dry_run_does_not_rewrite_profiles_current_or_execute_node(self):
        self.prepare_profiles()
        for binary in self.node_root.glob("*/bin/node"):
            if binary.parent.parent.name != "current":
                binary.write_text("#!/bin/sh\nprintf 'UNEXPECTED_NODE_CALL\\n' >&2\nexit 99\n")
        before = self.snapshot()
        for action in ("use", "install"):
            args = [action, "node", "26.2.0"] if action == "use" else [action, "node", "--version", "26.2.0"]
            result = subprocess.run(["bash", str(selection.REPO / "bin/atm"), "--dry-run", *args],
                                    env=self.env, cwd=selection.REPO, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn("UNEXPECTED_NODE_CALL", result.stdout + result.stderr)
            self.assertIn("DRY-RUN: update PATH block", result.stdout)
            self.assertEqual(before, self.snapshot())

    def test_generic_path_priority_keeps_other_order_and_calls_callbacks_once(self):
        result = self.run_shell(r'''
atm_plugin_arrays_init
ATM_PLUGIN_IDS=(normal early late)
ATM_PLUGIN_ORDER[normal]=1
ATM_PLUGIN_ORDER[early]=2
ATM_PLUGIN_ORDER[late]=3
ATM_PLUGIN_PATH_PRIORITY[early]=prepend
ATM_PLUGIN_PATH_PRIORITY[late]=prepend
atm_test_path() { printf '/%s\n' "$id"; printf '%s\n' "$id" >> "$HOME/callbacks"; }
for id in "${ATM_PLUGIN_IDS[@]}"; do ATM_PLUGIN_PATH_FUNC[$id]=atm_test_path; done
ATM_PLUGIN_SORT_ORDER=za atm_path_collect_entries
''')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["/early", "/late", str(self.home / ".local/bin"),
                                                     str(self.home / "Apps/atm/bin"), "/normal"])
        self.assertEqual((self.home / "callbacks").read_text().splitlines(), ["early", "late", "normal"])

    def test_metadata_priority_defaults_do_not_leak_and_invalid_values_fail(self):
        for name, priority in (("first", "prepend"), ("second", None), ("bad", "invalid")):
            plugin = self.home / name
            plugin.mkdir()
            metadata = 'ATM_PLUGIN_ID="' + name + '"\n'
            if priority:
                metadata += 'ATM_PLUGIN_PATH_PRIORITY_VALUE="' + priority + '"\n'
            (plugin / "plugin.metadata").write_text(metadata)
            (plugin / "plugin.sh").write_text("#!/usr/bin/env bash\n")
        result = self.run_shell(r'''
atm_plugin_arrays_init
atm_plugin_load_metadata "$HOME/first"
atm_plugin_load_metadata "$HOME/second"
printf '%s\n' "${ATM_PLUGIN_PATH_PRIORITY[first]}" "${ATM_PLUGIN_PATH_PRIORITY[second]}"
atm_plugin_arrays_init
[[ ${#ATM_PLUGIN_PATH_PRIORITY[@]} == 0 ]]
''')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["prepend", "default"])
        result = self.run_shell('atm_plugin_load_metadata "$HOME/bad"\n')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Invalid plugin PATH priority", result.stderr)


if __name__ == "__main__":
    unittest.main()

"""Guards for native package smoke tests; never execute application binaries."""
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest

from check_go_packages import (isolated_environment, seed_user_state, snapshot,
                               verify_read_only_backend, verify_read_only_trace)


class NativeSmokeTests(unittest.TestCase):
    def check_backend(self, mutation=None, status="success", choices=None, duplicate=False):
        manifest = {"id": "fixture", "operations": ["menu", "status"]}
        calls = []
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)

            def run(*args):
                calls.append(args)
                terminal = {"type": "result", "status": status}
                if args[-1] == "menu":
                    terminal["choices"] = choices if choices is not None else [
                        {"id": "status", "operation": "status", "label": "Status"}]
                events = [{"type": "started"}, terminal]
                if duplicate:
                    events.append(terminal)
                if mutation:
                    mutation(root)
                return SimpleNamespace(stdout="\n".join(json.dumps(event) for event in events))

            verify_read_only_backend(run, manifest, root)
        return calls

    def test_only_menu_and_status_execute(self):
        self.assertEqual(self.check_backend(), [
            ("--yes", "plugin", "fixture", "menu"),
            ("--yes", "plugin", "fixture", "status"),
        ])

    def test_rejects_failed_or_duplicate_results(self):
        with self.assertRaises(AssertionError):
            self.check_backend(status="error")
        with self.assertRaises(ValueError):
            self.check_backend(duplicate=True)

    def test_rejects_empty_or_undeclared_choices(self):
        for choices in ([], [{"operation": "install"}]):
            with self.subTest(choices=choices), self.assertRaises(AssertionError):
                self.check_backend(choices=choices)

    def test_rejects_backend_writes(self):
        with self.assertRaises(AssertionError):
            self.check_backend(mutation=lambda root: (root / "unexpected").write_text("changed"))

    def test_isolates_system_probe_and_search_path(self):
        root = Path("/tmp/atm-smoke-fixture")
        env = isolated_environment(root)
        self.assertEqual(env["PATH"], "")
        self.assertEqual(env["HOME"], str(root / "user"))
        self.assertEqual(env["ATM_NODE_SYSTEM_BINARY"], str(root / "absent-system-node"))
        self.assertEqual(env["JAVA_HOME"], str(root / "absent-java"))
        self.assertEqual(env["ANDROID_SDK_ROOT"], str(root / "absent-sdk"))
        self.assertEqual(env["ANDROID_USER_HOME"], str(root / "android-user"))

    def test_snapshot_records_link_identity_without_following(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            link = root / "dangling"
            link.symlink_to(root / "missing")
            before = snapshot(root)
            self.assertEqual(before["dangling"][1], str(root / "missing"))
            link.unlink()
            link.symlink_to(root / "other-missing")
            self.assertNotEqual(snapshot(root), before)

    def test_snapshot_does_not_traverse_linked_directory(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as external:
            root = Path(directory)
            (root / "external").symlink_to(external, target_is_directory=True)
            before = snapshot(root)
            (Path(external) / "outside").write_text("not part of test root")
            self.assertEqual(snapshot(root), before)
            self.assertEqual(set(before), {"external"})

    def test_existing_user_state_and_permissions_are_observed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            seed_user_state(root)
            before = snapshot(root)
            self.assertIn("apps/unrelated-application/data/settings.json", before)
            self.assertIn("user/unavailable-volume", before)
            (root / "user" / ".zshrc").chmod(0o644)
            self.assertNotEqual(snapshot(root), before)

    def test_trace_allows_only_core_and_backend_execution(self):
        trace = '1 execve("/tmp/atm", [], []) = 0\n2 execve("/tmp/backend", [], []) = 0\n'
        verify_read_only_trace(trace, Path("/tmp/atm"), Path("/tmp/backend"))
        for extra in ('3 connect(3, {}, 16) = -1\n', '3 execve("/bin/sh", [], []) = 0\n',
                      '3 execveat(3, "", [], [], 0) = 0\n'):
            with self.subTest(extra=extra), self.assertRaises(AssertionError):
                verify_read_only_trace(trace + extra, "/tmp/atm", "/tmp/backend")

    def test_trace_requires_backend_and_correct_order(self):
        for trace in ('', '1 execve("/tmp/atm", [], []) = 0\n',
                      '1 execve("/tmp/backend", [], []) = 0\n2 execve("/tmp/atm", [], []) = 0\n'):
            with self.subTest(trace=trace), self.assertRaises(AssertionError):
                verify_read_only_trace(trace, "/tmp/atm", "/tmp/backend")

    def test_trace_handles_non_ascii_filesystem_names(self):
        trace = '1 execve("/tmp/atm", [], []) = 0\n2 execve("/tmp/caf\\303\\251/backend", [], []) = 0\n'
        verify_read_only_trace(trace, "/tmp/atm", "/tmp/caf\u00e9/backend")


if __name__ == "__main__":
    unittest.main()

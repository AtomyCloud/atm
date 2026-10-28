"""Guards for native package smoke tests; never execute application binaries."""
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest

from check_go_packages import isolated_environment, verify_read_only_backend


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


if __name__ == "__main__":
    unittest.main()

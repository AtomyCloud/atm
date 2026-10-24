import os
from pathlib import Path
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[3]


class CoreTests(unittest.TestCase):
    def test_discovery_menu_and_no_write_cli(self):
        with tempfile.TemporaryDirectory(prefix="atm-backup-core-") as temp:
            root = Path(temp)
            home = root / "home"
            (home / "Documents").mkdir(parents=True)
            (home / "Documents" / "file").write_text("test")
            # The host's Brave --version wrapper creates MIME files; isolate it.
            tools = root / "tools"
            tools.mkdir()
            brave = tools / "brave-browser"
            brave.write_text("#!/usr/bin/env bash\nprintf 'Brave Browser (fixture)\\n'\n")
            brave.chmod(0o700)
            env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_") and k not in ("SUDO_USER", "ZDOTDIR", "ZSH", "ZSH_CUSTOM")}
            env.update(HOME=str(home), ATM_APPS_DIR=str(home / "Apps"),
                       ATM_CONFIG_DIR=str(root / "config"), ATM_STATE_DIR=str(root / "state"),
                       ATM_CACHE_DIR=str(root / "cache"), ATM_LANG="en-us",
                       ATM_PLUGIN_SORT_ORDER="default", PYTHONDONTWRITEBYTECODE="1", TERM="xterm",
                       PATH=str(tools) + os.pathsep + env.get("PATH", os.defpath))

            def snapshot():
                return {str(p.relative_to(root)): p.read_bytes() if p.is_file() else None
                        for p in root.rglob("*")}

            def run(args, input=None):
                result = subprocess.run(["bash", str(REPO / "bin/atm"), "--dry-run", *args],
                                        input=input, text=True, capture_output=True, env=env,
                                        cwd=REPO, timeout=30)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                return result.stdout

            before = snapshot()
            self.assertIn("backup_restore", run(["plugins", "list"]))
            plan = run(["install", "backup_restore", "--action", "backup", "--type", "custom", "--path", "Documents"])
            self.assertIn('"dry_run": true', plan)
            self.assertIn(str(home / "Apps/Backups"), plan)
            menu = run([], "14\nq\n")
            self.assertIn("1) Create Safe backup", menu)
            self.assertIn("7) Verify backup", menu)
            self.assertEqual(before, snapshot())


if __name__ == "__main__":
    unittest.main()

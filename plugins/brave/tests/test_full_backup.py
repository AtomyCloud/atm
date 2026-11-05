"""Full-backup tests use disposable homes, never real browser data."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("full_backup", ROOT / "plugins/brave/full_backup.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class FullBackupTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="atm-brave-full-")
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name) / "home with spaces"
        self.home.mkdir()
        self.output = self.home / "Apps/Backups"
        self.standard = self.home / ".config/BraveSoftware/Brave-Browser"
        self.flatpak = self.home / ".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser"
        self.snap = self.home / "snap/brave/123/.config/BraveSoftware/Brave-Browser"
        self.closed = mock.patch.object(module, "require_closed")
        self.closed.start()
        self.addCleanup(self.closed.stop)

    def profile(self, root):
        for name, data in {"Local State": "state", "Default/History": "history",
                           "Default/Bookmarks": "bookmarks", "Default/Login Data": "logins",
                           "Profile 12/Preferences": "preferences", ".hidden": "hidden"}.items():
            file = root / name
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text(data)

    def create(self):
        return module.backup(self.home, self.output)

    def test_all_installations_and_snap_current_link(self):
        for source in [self.standard, self.flatpak, self.snap]:
            self.profile(source)
        (self.home / "snap/brave/current").symlink_to("123")
        archive_path = self.create()
        self.assertRegex(archive_path.name, r"^brave-\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}-.+\.tar\.gz$")
        self.assertEqual(archive_path.stat().st_mode & 0o777, 0o600)
        with tarfile.open(archive_path) as archive:
            for kind in ["standard", "flatpak", "snap"]:
                for name in ["Local State", "Default/History", "Default/Bookmarks", "Default/Login Data", "Profile 12/Preferences", ".hidden"]:
                    self.assertTrue(archive.getmember(f"{kind}/Brave-Browser/{name}").isfile())
                self.assertEqual(archive.extractfile(f"{kind}/Brave-Browser/Default/History").read(), b"history")
            manifest = json.load(archive.extractfile("backup.json"))
            self.assertEqual(len(manifest["sources"]), 3)
            self.assertEqual(manifest["sources"][2]["source"], str(self.snap))
            self.assertTrue(all(not name.startswith("/") and ".." not in Path(name).parts for name in archive.getnames()))
        self.assertEqual((self.standard / "Default/History").read_text(), "history")
        self.assertTrue((self.home / "snap/brave/current").is_symlink())

    def test_single_sources_and_no_binary_requirement(self):
        for source, kind in [(self.standard, "standard"), (self.flatpak, "flatpak"), (self.snap, "snap")]:
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as root:
                home = Path(root)
                relative = source.relative_to(self.home)
                self.profile(home / relative)
                if kind == "snap":
                    (home / "snap/brave/current").symlink_to("123")
                result = module.backup(home, home / "Apps/Backups")
                with tarfile.open(result) as archive:
                    self.assertEqual(len(json.load(archive.extractfile("backup.json"))["sources"]), 1)
                    self.assertIn(f"{kind}/Brave-Browser/Local State", archive.getnames())

    def test_repeat_does_not_replace_and_dry_run_writes_nothing(self):
        self.profile(self.standard)
        self.assertIsNone(module.backup(self.home, self.output, dry_run=True))
        self.assertFalse(self.output.exists())
        first = self.create()
        before = first.read_bytes()
        second = self.create()
        self.assertNotEqual(first, second)
        self.assertEqual(first.read_bytes(), before)
        self.assertFalse(list(self.output.glob("*.part")))

    def test_root_links_resolved_internal_links_not_followed(self):
        actual = self.home / "external profile"
        self.profile(actual)
        self.standard.parent.mkdir(parents=True)
        self.standard.symlink_to(actual, target_is_directory=True)
        outside = self.home / "private unrelated.txt"
        outside.write_text("do not copy me")
        (actual / "external-link").symlink_to(outside)
        (actual / "broken-link").symlink_to("missing")
        with tarfile.open(self.create()) as archive:
            self.assertTrue(archive.getmember("standard/Brave-Browser").isdir())
            self.assertTrue(archive.getmember("standard/Brave-Browser/external-link").issym())
            self.assertTrue(archive.getmember("standard/Brave-Browser/broken-link").issym())
            self.assertNotIn("private unrelated.txt", archive.getnames())

    def test_overlap_missing_and_alias_destinations(self):
        with self.assertRaises(module.BackupError):
            self.create()
        self.assertFalse(self.output.exists())
        self.profile(self.standard)
        for dest in [self.standard, self.standard / "backup"]:
            with self.assertRaises(module.BackupError):
                module.backup(self.home, dest)
        alias = self.home / "alias"
        alias.symlink_to(self.standard, target_is_directory=True)
        with self.assertRaises(module.BackupError):
            module.backup(self.home, alias / "backup")
        self.assertFalse((self.standard / "backup").exists())

    def test_custom_standard_and_duplicate_physical_source(self):
        self.profile(self.flatpak)
        self.assertEqual(module.detect_sources(self.home, self.flatpak), [("standard", self.flatpak)])
        result = module.backup(self.home, self.output, self.flatpak)
        with tarfile.open(result) as archive:
            self.assertIn("standard/Brave-Browser/Local State", archive.getnames())
            self.assertNotIn("flatpak/Brave-Browser", archive.getnames())

    def test_failure_cleanup_and_no_overwrite(self):
        self.profile(self.standard)
        with mock.patch.object(module.tarfile.TarFile, "add", side_effect=OSError("read failure")):
            with self.assertRaises(OSError):
                self.create()
        self.assertEqual(list(self.output.iterdir()), [])
        original_link = os.link

        def collide(src, dest):
            Path(dest).write_text("existing backup")
            original_link(src, dest)

        with mock.patch.object(module.os, "link", side_effect=collide):
            with self.assertRaises(FileExistsError):
                self.create()
        files = list(self.output.iterdir())
        self.assertEqual(len(files), 1)
        self.assertEqual(files[0].read_text(), "existing backup")

    def test_running_browser_blocks_before_write_and_before_publish(self):
        self.profile(self.standard)
        with mock.patch.object(module, "require_closed", side_effect=module.BackupError("running")):
            with self.assertRaises(module.BackupError):
                self.create()
        self.assertFalse(self.output.exists())
        with mock.patch.object(module, "require_closed", side_effect=[None, module.BackupError("started")]):
            with self.assertRaises(module.BackupError):
                self.create()
        self.assertEqual(list(self.output.iterdir()), [])

    def test_process_guard_name_wrapper_errors_and_uid(self):
        self.closed.stop()
        for codes, failure in [([0], True), ([1, 0], True), ([1, 1], False), ([2], True)]:
            with self.subTest(codes=codes), mock.patch.object(module.subprocess, "run", side_effect=[subprocess.CompletedProcess([], c) for c in codes]) as run:
                if failure:
                    with self.assertRaises(module.BackupError):
                        module.require_closed()
                else:
                    module.require_closed()
                self.assertEqual(run.call_args_list[0].args[0][1:3], ["-u", str(os.getuid())])
        with mock.patch.object(module.subprocess, "run", side_effect=FileNotFoundError):
            with self.assertRaises(FileNotFoundError):
                module.require_closed()

    @unittest.skipUnless(shutil.which("pgrep") and Path("/bin/sleep").exists(), "pgrep/sleep required")
    def test_executable_pattern_does_not_match_backup_arguments(self):
        self.closed.stop()
        with mock.patch.object(module.subprocess, "run", return_value=subprocess.CompletedProcess([], 1)) as run:
            module.require_closed()
        argv_check = run.call_args_list[1].args[0]
        browser = subprocess.Popen(["/fixture/brave-browser", "30"], executable="/bin/sleep")
        unrelated = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)", "/backups/brave"])
        try:
            result = subprocess.run(argv_check, capture_output=True, text=True, check=False, timeout=5)
            matches = result.stdout.splitlines()
            self.assertIn(str(browser.pid), matches)
            self.assertNotIn(str(unrelated.pid), matches)
        finally:
            for process in [browser, unrelated]:
                process.terminate()
                process.wait(timeout=5)

    def test_menu_routes_and_default_destination(self):
        self.profile(self.standard)
        script = '''
set -Eeuo pipefail
ATM_HOME="$PWD"; ATM_APPS_DIR="$HOME/Apps"; ATM_MANIFEST_DIR="$HOME/manifests"
source lib/atm_core.sh
source lib/atm_i18n.sh
source lang/en-us.lang
source plugins/brave/plugin.conf
source plugins/brave/plugin.sh
source plugins/brave/lang/pt-br.lang
clear() { :; }
atm_brave_status() { printf 'fixture\\n'; }
ATM_DRY_RUN=1
atm_brave_menu <<< $'6\\nxb'
atm_brave_backup_menu <<< $'5\\nxb'
'''
        env = dict(os.environ, HOME=str(self.home), PYTHONDONTWRITEBYTECODE="1")
        for key in ["ATM_BRAVE_PROFILE_DIR", "ATM_BRAVE_FULL_BACKUP_DIR", "SUDO_USER"]:
            env.pop(key, None)
        result = subprocess.run(["bash", "-c", script], cwd=ROOT, env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("6) Backup completo", result.stdout)
        self.assertIn("5) Backup completo", result.stdout)
        self.assertEqual(result.stdout.count("DRY-RUN: create brave-"), 2)
        self.assertIn(str(self.output), result.stdout)
        self.assertNotIn("[missing:", result.stdout)
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()

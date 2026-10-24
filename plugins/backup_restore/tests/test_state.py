import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("backup_restore_state", Path(__file__).parents[1] / "state.py")
state = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(state)


class StateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-backup-test-")
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.dest = self.home / "Apps" / "Backups"
        self.target = self.root / "restored"
        (self.home / "Documents").mkdir()
        (self.home / "Documents" / "file with spaces").write_text("original\n")
        (self.home / ".zshrc").write_text("do not execute this\n")
        (self.home / ".zshrc").chmod(0o600)
        self.excludes = Path(__file__).parents[1] / "excludes.txt"
        self.mask = os.umask(0o077)

    def tearDown(self):
        os.umask(self.mask)
        self.temp.cleanup()

    def backup(self, kind="safe", paths=()):
        result = state.create_backup(self.home, self.dest, kind, paths, self.excludes, "tester")
        return Path(result["backup"])

    def tree(self):
        return state.inventory(self.root)

    def test_all_modes_and_permissions(self):
        (self.home / ".cache").mkdir()
        (self.home / ".cache" / "secret").write_text("cache")
        (self.home / "Documents" / "temp.tmp").write_text("temporary")
        for kind in ("safe", "full", "main", "custom"):
            with self.subTest(kind=kind):
                root = self.backup(kind, ["Documents", "Documents/file with spaces"] if kind == "custom" else [])
                data = root / "users" / "tester"
                self.assertTrue((data / "Documents" / "file with spaces").is_file())
                self.assertEqual((data / ".zshrc").exists(), kind in ("safe", "full"))
                self.assertEqual((data / ".cache").exists(), kind == "full")
                self.assertEqual((data / "Documents" / "temp.tmp").exists(), kind != "safe")
                self.assertFalse((data / "Apps" / "Backups").exists())
                self.assertEqual(stat.S_IMODE(root.stat().st_mode), 0o700)
                self.assertEqual(stat.S_IMODE((root / "atool-backup.json").stat().st_mode), 0o600)
                state.load_backup(root)
        self.assertEqual(len(state.list_backups(self.dest)), 4)

    def test_merge_restore_and_recovery(self):
        root = self.backup()
        self.target.mkdir()
        (self.target / "unrelated").write_text("keep")
        (self.target / ".zshrc").write_text("old")
        result = state.restore_backup(root, self.target, self.home)
        self.assertEqual((self.target / "unrelated").read_text(), "keep")
        self.assertEqual((self.target / ".zshrc").read_text(), "do not execute this\n")
        self.assertEqual(stat.S_IMODE((self.target / ".zshrc").stat().st_mode), 0o600)
        recovery = Path(result["recovery"])
        self.assertEqual((recovery / ".zshrc").read_text(), "old")

    def test_dry_run_and_list_are_no_write(self):
        before = self.tree()
        state.create_backup(self.home, self.dest, dry_run=True, excludes=self.excludes)
        self.assertEqual(state.list_backups(self.dest), [])
        self.assertEqual(before, self.tree())
        root = self.backup()
        before = self.tree()
        state.restore_backup(root, self.target, self.home, dry_run=True)
        self.assertEqual(before, self.tree())

    def test_symlinks_preserved_never_followed(self):
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "keep").write_text("outside")
        (self.home / "link").symlink_to(outside, target_is_directory=True)
        root = self.backup()
        self.assertTrue((root / "users" / "tester" / "link").is_symlink())
        self.target.mkdir()
        (self.target / "Documents").symlink_to(outside, target_is_directory=True)
        state.restore_backup(root, self.target, self.home)
        self.assertEqual((outside / "keep").read_text(), "outside")
        self.assertFalse((outside / "file with spaces").exists())
        self.assertFalse((self.target / "Documents").is_symlink())
        self.assertTrue((self.target / "link").is_symlink())

    def test_custom_traversal_symlink_ancestor_missing_and_empty(self):
        (self.home / "escape").symlink_to(self.root)
        for paths in (["../outside"], ["/etc"], ["."], ["Documents/../.zshrc"],
                      ["escape/home/.zshrc"], ["missing"], []):
            with self.subTest(paths=paths):
                before = self.tree()
                with self.assertRaises(ValueError):
                    self.backup("custom", paths)
                self.assertEqual(before, self.tree())

    def test_corruption_fails_without_target_write(self):
        root = self.backup()
        (root / "users" / "tester" / ".zshrc").write_text("tampered")
        before = self.tree()
        with self.assertRaisesRegex(ValueError, "integrity"):
            state.restore_backup(root, self.target, self.home)
        self.assertEqual(before, self.tree())

    def test_unsafe_targets(self):
        root = self.backup()
        for target in ("/", "/home", "/etc/test", "//etc/test", "/usr/local/bad", self.home,
                       self.home.parent, root, root / "users", root.parent,
                       Path(__file__).parents[2] / "brave"):
            with self.subTest(target=target), self.assertRaises(ValueError):
                state.restore_backup(root, target, self.home, dry_run=True)
        link = self.root / "link"
        link.symlink_to(self.root)
        with self.assertRaises(ValueError):
            state.restore_backup(root, link / "new", self.home)

    def test_symlink_metadata_and_data_roots_rejected(self):
        root = self.backup()
        metadata = root / "atool-backup.json"
        original = metadata.read_text()
        metadata.unlink()
        elsewhere = self.root / "metadata.json"
        elsewhere.write_text(original)
        metadata.symlink_to(elsewhere)
        with self.assertRaises(ValueError):
            state.load_backup(root)
        metadata.unlink()
        metadata.write_text(original)
        data = root / "users" / "tester"
        shutil.rmtree(data)
        data.symlink_to(self.home)
        with self.assertRaises(ValueError):
            state.load_backup(root)

    def test_legacy_atool_and_multiuser_selection(self):
        root = self.backup()
        meta = root / "atool-backup.json"
        obj = json.loads(meta.read_text())
        obj.pop("atm_format")
        obj.pop("inventory")
        obj["users"].append("other")
        obj["successful_users"].append("other")
        shutil.copytree(root / "users" / "tester", root / "users" / "other")
        meta.write_text(json.dumps(obj))
        with self.assertRaisesRegex(ValueError, "Multi-user"):
            state.restore_backup(root, self.target, self.home)
        result = state.restore_backup(root, self.target, self.home, user="other")
        self.assertIn("legacy", result["integrity"])
        self.assertIsNone(result["recovery"])

    def test_invalid_metadata(self):
        root = self.backup()
        meta = root / "atool-backup.json"
        original = json.loads(meta.read_text())
        for updates in ({"status": "partial"}, {"dry_run": True}, {"users": ["../../bad"]},
                        {"schema_version": "9000"}, {"successful_users": []}, {"inventory": []},
                        {"atm_format": 8}, {"atm_format": None}, {"atm_format": True},
                        {"successful_users": "tester"}):
            meta.write_text(json.dumps(dict(original, **updates)))
            with self.subTest(updates=updates), self.assertRaises(ValueError):
                state.load_backup(root)
        meta.write_text("not json")
        self.assertEqual(state.list_backups(self.dest), [])

    def test_special_files_logged_not_read(self):
        os.mkfifo(self.home / "pipe")
        root = self.backup("full")
        obj = json.loads((root / "atool-backup.json").read_text())
        self.assertEqual(set(obj["skipped_special_files"]), {"pipe"})
        os.mkfifo(root / "users" / "tester" / "unsafe")
        with self.assertRaises(ValueError):
            state.restore_backup(root, self.target, self.home)

    def test_failed_backup_never_published(self):
        with mock.patch.object(state.shutil, "copy2", side_effect=OSError("disk full")):
            with self.assertRaises(OSError):
                self.backup()
        self.assertEqual(state.list_backups(self.dest), [])
        self.assertEqual(list(self.dest.rglob(".pending-*")), [])

    def test_oversize_metadata_never_published(self):
        with mock.patch.object(state, "MAX_METADATA_SIZE", 128):
            with self.assertRaisesRegex(ValueError, "metadata exceeds"):
                self.backup()
        self.assertEqual(state.list_backups(self.dest), [])
        self.assertEqual(list(self.dest.rglob(".pending-*")), [])

    def test_stage_failure_preserves_existing_target(self):
        root = self.backup()
        self.target.mkdir()
        (self.target / "keep").write_text("original")
        with mock.patch.object(state, "overlay", side_effect=OSError("disk full")):
            with self.assertRaises(OSError):
                state.restore_backup(root, self.target, self.home)
        self.assertEqual((self.target / "keep").read_text(), "original")
        self.assertEqual(list(self.root.glob(".atm-restore-*")), [])

    def test_failed_publish_rolls_back(self):
        root = self.backup()
        self.target.mkdir()
        (self.target / "keep").write_text("original")
        rename = Path.rename

        def fail_stage(path, dest):
            if path.name.startswith(".atm-restore-"):
                raise OSError("publish failed")
            return rename(path, dest)

        with mock.patch.object(Path, "rename", fail_stage), self.assertRaises(OSError):
            state.restore_backup(root, self.target, self.home)
        self.assertEqual((self.target / "keep").read_text(), "original")

    def test_failed_rollback_retains_recovery(self):
        root = self.backup()
        self.target.mkdir()
        (self.target / "keep").write_text("original")
        rename = Path.rename

        def fail_stage_and_recovery(path, dest):
            if path.name.startswith(".atm-restore-") or ".before-atm-restore-" in path.name:
                raise OSError("rename failed")
            return rename(path, dest)

        with mock.patch.object(Path, "rename", fail_stage_and_recovery):
            with self.assertRaisesRegex(OSError, "original data retained"):
                state.restore_backup(root, self.target, self.home)
        recovery, = self.root.glob("restored.before-atm-restore-*")
        self.assertEqual((recovery / "keep").read_text(), "original")

    def test_unique_snapshots_no_hardlink_mutation(self):
        first = self.backup()
        second = self.backup()
        self.assertNotEqual(first, second)
        original = first / "users" / "tester" / ".zshrc"
        (self.home / ".zshrc").write_text("changed")
        self.assertEqual(original.read_text(), "do not execute this\n")
        state.load_backup(first)

    def test_staged_corruption_never_published(self):
        root = self.backup()
        self.target.mkdir()
        (self.target / "keep").write_text("original")
        original_overlay = state.overlay

        def damage_copy(source, target):
            original_overlay(source, target)
            if source == root / "users" / "tester":
                (target / ".zshrc").write_text("corrupt copy")

        with mock.patch.object(state, "overlay", damage_copy):
            with self.assertRaisesRegex(ValueError, "Staged restore integrity"):
                state.restore_backup(root, self.target, self.home)
        self.assertEqual((self.target / "keep").read_text(), "original")
        self.assertFalse((self.target / ".zshrc").exists())

    def test_file_directory_conflicts_are_staged_safely(self):
        root = self.backup()
        self.target.mkdir()
        (self.target / ".zshrc").mkdir()
        (self.target / ".zshrc" / "old").write_text("recovery")
        (self.target / "Documents").write_text("old file")
        result = state.restore_backup(root, self.target, self.home)
        self.assertTrue((self.target / ".zshrc").is_file())
        self.assertTrue((self.target / "Documents").is_dir())
        self.assertEqual((Path(result["recovery"]) / ".zshrc" / "old").read_text(), "recovery")

    def test_destination_links_and_ancestors_rejected(self):
        (self.root / "link").symlink_to(self.root)
        for dest in (self.home, self.root, self.root / "link" / "backups"):
            with self.subTest(dest=dest), self.assertRaises(ValueError):
                state.create_backup(self.home, dest)


if __name__ == "__main__":
    unittest.main()

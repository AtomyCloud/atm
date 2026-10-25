import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

from test_state import state


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-backup-users-")
        self.root = Path(self.temp.name)
        self.homes = self.root / "homes"
        self.sources = []
        for name in ("alpha", "beta"):
            home = self.homes / name
            (home / "Documents").mkdir(parents=True)
            (home / "Documents" / "file").write_text(name)
            self.sources.append((name, home))
        self.home = self.sources[0][1]
        self.dest = self.root / "ATM" / "Backups"
        self.media = self.root / "media"
        self.device = self.media / "USB Disk"
        self.device.mkdir(parents=True)
        self.mountinfo = self.root / "mountinfo"
        self.mounts()
        self.mask = os.umask(0o077)

    def tearDown(self):
        os.umask(self.mask)
        self.temp.cleanup()

    def mounts(self, mount_id="42", options="rw", super_options="rw"):
        path = str(self.device).replace("\\", "\\134").replace(" ", "\\040")
        self.mountinfo.write_text(f"{mount_id} 1 8:1 / {path} {options} - ext4 /dev/test {super_options}\n")

    def backup(self, **kwargs):
        return Path(state.create_backup(self.home, self.dest, sources=self.sources, **kwargs)["backup"])

    def test_discover_and_resolve_regular_homes(self):
        for name in ("root", "nobody", "lost+found", "bad user", "daemon"):
            (self.homes / name).mkdir()
        (self.homes / "link").symlink_to(self.home)
        (self.homes / "file").touch()
        users = state.discover_users(self.homes, self.home)
        self.assertEqual([name for name, _ in users], ["alpha", "beta"])
        self.assertEqual(state.select_users(self.homes, self.home, "beta"), [self.sources[1]])
        self.assertEqual(state.select_users(self.homes, self.home, all_users=True), self.sources)
        for name in ("root", "../beta", "bad user", "missing"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                state.select_users(self.homes, self.home, name)
        with self.assertRaises(ValueError):
            state.select_users(self.homes, self.home, "alpha", True)

    def test_multiuser_integrity_list_and_restore(self):
        root = self.backup()
        obj = state.backup_metadata(root)
        self.assertEqual(obj["atm_format"], 2)
        self.assertEqual(obj["users"], ["alpha", "beta"])
        self.assertEqual(state.list_backups(self.dest), [str(root)])
        for name, _ in self.sources:
            state.load_backup(root, name)
        with self.assertRaises(ValueError):
            state.load_backup(root)
        target = self.root / "restored"
        target.mkdir()
        (target / "keep").write_text("keep")
        result = state.restore_backup(root, target, self.home, all_users=True)
        for name, _ in self.sources:
            self.assertEqual((target / name / "Documents/file").read_text(), name)
        self.assertTrue(Path(result["recovery"]).is_dir())
        self.assertEqual((target / "keep").read_text(), "keep")
        self.assertTrue(result["grouped_by_user"])
        single = self.root / "beta-only"
        state.restore_backup(root, single, self.home, user="beta")
        self.assertEqual((single / "Documents/file").read_text(), "beta")
        self.assertFalse((single / "alpha").exists())

    def test_corrupt_one_user_prevents_all_restore(self):
        root = self.backup()
        (root / "users/beta/Documents/file").write_text("corrupt")
        target = self.root / "not-created"
        with self.assertRaises(ValueError):
            state.restore_backup(root, target, self.home, all_users=True)
        self.assertFalse(target.exists())
        state.load_backup(root, "alpha")

    def test_multiuser_dry_run_no_writes(self):
        before = state.inventory(self.root)
        result = state.create_backup(self.home, self.dest, sources=self.sources, dry_run=True)
        self.assertEqual([row["user"] for row in result["users"]], ["alpha", "beta"])
        self.assertEqual(before, state.inventory(self.root))
        root = self.backup()
        before = state.inventory(self.root)
        state.restore_backup(root, self.root / "dry-restore", self.home, all_users=True, dry_run=True)
        self.assertEqual(before, state.inventory(self.root))

    def test_one_user_copy_failure_prevents_publication(self):
        copy = state.copy_entry

        def fail_beta(source, *args):
            if self.sources[1][1] in source.parents:
                raise PermissionError("cannot read beta")
            return copy(source, *args)

        with mock.patch.object(state, "copy_entry", fail_beta), self.assertRaises(PermissionError):
            self.backup()
        self.assertEqual(state.list_backups(self.dest), [])
        self.assertEqual(list(self.dest.rglob(".pending-*")), [])

    def test_unreadable_preflight_creates_no_output(self):
        plan = state.backup_plan

        def unreadable(home, *args):
            if home == self.sources[1][1]:
                raise PermissionError("private home")
            return plan(home, *args)

        with mock.patch.object(state, "backup_plan", unreadable):
            with self.assertRaisesRegex(PermissionError, "no users were skipped"):
                self.backup()
        self.assertFalse(self.dest.exists())

    def test_devices_decode_spaces_and_ignore_plain_dirs(self):
        (self.media / "not-mounted").mkdir()
        (self.media / "link").symlink_to(self.device)
        devices = state.mounted_devices(self.media, self.mountinfo)
        self.assertEqual(len(devices), 1)
        self.assertEqual(devices[0]["path"], self.device)
        self.assertEqual(devices[0]["id"], "42")
        self.assertTrue(devices[0]["writable"])
        self.assertEqual(state.validate_device(self.device, self.media, self.mountinfo, "42"), "42")
        with self.assertRaises(ValueError):
            state.validate_device(self.media / "not-mounted", self.media, self.mountinfo)

    def test_readonly_missing_and_changed_device_create_nothing(self):
        for mount_id, options, super_options, expected in (
                ("42", "ro", "ro", "42"), ("43", "rw", "rw", "42")):
            self.mounts(mount_id, options, super_options)
            with self.assertRaises(ValueError):
                state.create_backup(self.home, self.device, sources=self.sources,
                                    media_root=self.media, mountinfo=self.mountinfo, mount_id=expected)
            self.assertFalse((self.device / "atool-backups").exists())
        self.mountinfo.write_text("")
        with self.assertRaises(ValueError):
            state.create_backup(self.home, self.device, media_root=self.media, mountinfo=self.mountinfo)
        self.assertFalse((self.device / "atool-backups").exists())

    def test_ambiguous_stacked_mounts_are_not_offered(self):
        record = self.mountinfo.read_text()
        self.mountinfo.write_text(record + record.replace("42 1", "43 1"))
        self.assertEqual(state.mounted_devices(self.media, self.mountinfo), [])
        with self.assertRaises(ValueError):
            state.validate_device(self.device, self.media, self.mountinfo)

    def test_multiuser_restore_publish_failure_rolls_back(self):
        root = self.backup()
        target = self.root / "restore-existing"
        target.mkdir()
        (target / "keep").write_text("original")
        rename = Path.rename

        def fail_stage(path, dest):
            if path.name.startswith(".atm-restore-"):
                raise OSError("publish failed")
            return rename(path, dest)

        with mock.patch.object(Path, "rename", fail_stage), self.assertRaises(OSError):
            state.restore_backup(root, target, self.home, all_users=True)
        self.assertEqual((target / "keep").read_text(), "original")
        self.assertFalse((target / "alpha").exists())

    def test_mounted_backup_and_loss_during_copy(self):
        kwargs = dict(sources=self.sources, media_root=self.media, mountinfo=self.mountinfo, mount_id="42")
        root = Path(state.create_backup(self.home, self.device, **kwargs)["backup"])
        self.assertEqual(state.backup_metadata(root)["users"], ["alpha", "beta"])
        copy = state.copy_entry

        def disconnect(source, *args):
            result = copy(source, *args)
            self.mountinfo.write_text("")
            return result

        with mock.patch.object(state, "copy_entry", disconnect), self.assertRaises(ValueError):
            state.create_backup(self.home, self.device, **kwargs)
        self.mounts()
        self.assertEqual(state.list_backups(self.device), [str(root)])

    def test_private_gitignore_and_no_symlink_repository(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_NOSYSTEM="1")
        if shutil.which("git"):
            subprocess.run(["git", "init", "--quiet", str(self.dest.parent)], env=env, check=True)
        root = self.backup()
        self.assertEqual((self.dest / "atool-backups/.gitignore").read_text(), "*\n")
        if shutil.which("git"):
            status = subprocess.run(["git", "-C", str(self.dest.parent), "status", "--porcelain"],
                                    env=env, check=True, text=True, capture_output=True)
            self.assertEqual(status.stdout, "")
        other = self.root / "other-output"
        other.mkdir()
        (other / "atool-backups").symlink_to(root.parent)
        with self.assertRaises(ValueError):
            state.create_backup(self.home, other)

    def test_multiuser_restore_cannot_target_live_home(self):
        root = self.backup()
        with self.assertRaisesRegex(ValueError, "live user home"):
            state.restore_backup(root, self.sources[1][1], self.home,
                                 user="alpha", home_base=self.homes)

    def test_v2_malformed_inventory_rejected(self):
        root = self.backup()
        path = root / "atool-backup.json"
        original = json.loads(path.read_text())
        for inventories in ({"alpha": {}}, {"alpha": {}, "beta": []}, []):
            path.write_text(json.dumps(dict(original, inventories=inventories)))
            with self.assertRaises(ValueError):
                state.load_backup(root, "beta")


if __name__ == "__main__":
    unittest.main()

import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).parents[1]))
import apps_backup as apps


class AppsBackupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-apps-test-")
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.source = self.home / "Apps"
        (self.source / "atm").mkdir(parents=True)
        (self.source / "atm/bin").write_bytes(b"ATM fixture\n")
        (self.source / ".settings").write_bytes(b"private\n")
        (self.source / "Backups").mkdir()
        (self.source / "Backups/old backup.tar.gz").write_bytes(b"old backup")
        self.media = self.root / "media"
        self.device = self.media / "USB Disk"
        self.device.mkdir(parents=True)
        self.mountinfo = self.root / "mountinfo"
        path = str(self.device).replace(" ", r"\040")
        self.mountinfo.write_text(f"42 1 8:1 / {path} rw - ext4 /dev/test rw\n")

    def tearDown(self):
        self.temp.cleanup()

    def backup(self, **kwargs):
        return apps.backup(self.home, self.device, self.media, self.mountinfo, "42", **kwargs)

    def test_measure_counts_logical_sizes_and_does_not_follow_links(self):
        outside = self.root / "outside"
        outside.write_bytes(b"external")
        (self.source / "link").symlink_to(outside)
        (self.source / "loop").symlink_to(self.source)
        with (self.source / "sparse").open("wb") as stream:
            stream.truncate(8 * 1024 * 1024)
        info = apps.measure(self.home)
        expected = 8 * 1024 * 1024 + len(b"ATM fixture\nprivate\nold backup")
        self.assertEqual(info["source_bytes"], expected)
        self.assertGreater(info["required_bytes"], expected + 16 * 1024 * 1024)
        self.assertEqual(list(self.device.iterdir()), [])

    def test_archive_content_privacy_links_and_repeat(self):
        (self.source / "link").symlink_to("atm/bin")
        (self.source / "atm/bin").chmod(0o755)
        first = Path(self.backup()["backup"])
        second = Path(self.backup()["backup"])
        self.assertNotEqual(first, second)
        self.assertEqual(stat.S_IMODE(first.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(first.parent.stat().st_mode), 0o700)
        with tarfile.open(first) as archive:
            self.assertEqual(archive.extractfile("Apps/atm/bin").read(), b"ATM fixture\n")
            self.assertEqual(archive.getmember("Apps/atm/bin").mode, 0o755)
            self.assertEqual(archive.extractfile("Apps/.settings").read(), b"private\n")
            self.assertEqual(archive.extractfile("Apps/Backups/old backup.tar.gz").read(), b"old backup")
            self.assertTrue(archive.getmember("Apps/link").issym())
            self.assertTrue(all(name == "Apps" or name.startswith("Apps/") for name in archive.getnames()))
        self.assertEqual(list(self.device.rglob("*.part")), [])

    def test_low_space_zero_and_boundary(self):
        required = apps.measure(self.home)["required_bytes"]
        for free in (0, required - 1):
            with mock.patch.object(apps.shutil, "disk_usage", return_value=mock.Mock(free=free)):
                with mock.patch.object(apps.subprocess, "run") as run:
                    with self.assertRaisesRegex(ValueError, "Not enough space"):
                        self.backup()
                    run.assert_not_called()
            self.assertEqual(list(self.device.iterdir()), [])
        with mock.patch.object(apps.shutil, "disk_usage", return_value=mock.Mock(free=required)):
            self.assertEqual(self.backup(dry_run=True)["available_bytes"], required)
        self.assertEqual(list(self.device.iterdir()), [])

    def test_space_rechecked_immediately_before_output(self):
        with mock.patch.object(apps, "capacity", side_effect=[10**12, ValueError("Not enough space")]):
            with self.assertRaisesRegex(ValueError, "Not enough space"):
                self.backup()
        self.assertEqual(list(self.device.iterdir()), [])

    def test_destination_overlap_links_missing_and_manual(self):
        link = self.root / "alias"
        link.symlink_to(self.source / "Backups")
        for dest in (self.source, self.source / "Backups", link, self.root / "missing", None):
            with self.subTest(dest=dest), self.assertRaises(ValueError):
                apps.backup(self.home, dest, self.media, self.mountinfo, dry_run=True)
        result = apps.backup(self.home, self.root, self.media, self.mountinfo, dry_run=True)
        self.assertTrue(result["dry_run"])
        self.assertFalse((self.root / "missing").exists())

    def test_unmounted_changed_readonly_devices(self):
        original = self.mountinfo.read_text()
        for value in ("", original.replace("42 1", "43 1"), original.replace("rw", "ro")):
            self.mountinfo.write_text(value)
            with self.assertRaises(ValueError):
                self.backup()
            self.assertEqual(list(self.device.iterdir()), [])

    def test_device_lost_during_archive_has_no_published_backup(self):
        run = subprocess.run

        def disconnect(*args, **kwargs):
            result = run(*args, **kwargs)
            self.mountinfo.write_text("")
            return result

        with mock.patch.object(apps.subprocess, "run", side_effect=disconnect):
            with self.assertRaises(ValueError):
                self.backup()
        self.assertEqual(list(self.device.iterdir()), [])

    def test_tar_failure_and_write_failure_cleanup(self):
        for failure in (subprocess.CompletedProcess([], 1, stderr=b"file changed as we read it"),
                        OSError("No space left on device")):
            kwargs = {"side_effect": failure} if isinstance(failure, Exception) else {"return_value": failure}
            with mock.patch.object(apps.subprocess, "run", **kwargs), self.assertRaises(OSError):
                self.backup()
            self.assertEqual(list(self.device.iterdir()), [])

    def test_environment_cannot_exclude_apps(self):
        with mock.patch.dict(os.environ, TAR_OPTIONS="--exclude=Apps", GZIP="--invalid"):
            archive = self.backup()["backup"]
        with tarfile.open(archive) as stream:
            self.assertIn("Apps/atm/bin", stream.getnames())

    def test_missing_source_and_special_files_fail_without_output(self):
        with self.assertRaisesRegex(ValueError, "not found"):
            apps.measure(self.root / "missing-home")
        os.mkfifo(self.source / "fifo")
        with self.assertRaisesRegex(ValueError, "special file"):
            self.backup()
        self.assertEqual(list(self.device.iterdir()), [])

    def test_preflight_collects_socket_fifo_and_access_blockers(self):
        path = self.source / "vscode/data/1.14-main.sock"
        path.parent.mkdir(parents=True)
        fifo = self.source / "another-app.fifo"
        os.mkfifo(fifo)
        access = os.access
        with socket.socket(socket.AF_UNIX) as server:
            server.bind(str(path))
            with mock.patch.object(apps.os, "access", side_effect=lambda p, mode: False if Path(p).name == ".settings" else access(p, mode)):
                with mock.patch.object(apps.subprocess, "run") as run:
                    with self.assertRaises(ValueError) as caught:
                        self.backup()
                    run.assert_not_called()
            message = str(caught.exception)
            for text in ("3 issue(s)", "VS Code", "socket", "FIFO", "Unreadable file", str(path), str(fifo)):
                self.assertIn(text, message)
        # Closing a UNIX socket does not remove its filesystem entry.
        with self.assertRaisesRegex(ValueError, "Sockets may be stale"):
            apps.measure(self.home)
        self.assertTrue(path.exists())
        self.assertEqual(list(self.device.iterdir()), [])
        path.unlink()
        fifo.unlink()
        self.assertGreater(apps.measure(self.home)["source_bytes"], 0)

    def test_preflight_collects_missing_tools_and_unreadable_directory(self):
        scan = os.scandir

        def denied(path):
            if path == self.source / "atm":
                raise PermissionError(13, "Permission denied")
            return scan(path)

        with mock.patch.object(apps.shutil, "which", return_value=None), mock.patch.object(apps.os, "scandir", side_effect=denied):
            with self.assertRaises(ValueError) as caught:
                self.backup()
        message = str(caught.exception)
        for text in ("3 issue(s)", "tar", "gzip", "Permission denied", str(self.source / "atm")):
            self.assertIn(text, message)
        self.assertEqual(list(self.device.iterdir()), [])

    def test_preflight_report_is_bounded_but_counts_all_issues(self):
        for index in range(55):
            os.mkfifo(self.source / f"fifo-{index}")
        with self.assertRaises(ValueError) as caught:
            apps.measure(self.home)
        message = str(caught.exception)
        self.assertIn("55 issue(s)", message)
        self.assertIn("5 more issues", message)
        self.assertEqual(message.count("Unsupported special file"), 50)

    def shell(self, commands, inputs="", dry_run="0"):
        plugin = Path(__file__).parents[1]
        env = dict(os.environ, HOME=str(self.home), ATM_APPS_DIR=str(self.source),
                   ATM_BACKUP_RESTORE_HOME=str(self.home), ATM_BACKUP_RESTORE_DIR=str(self.source / "Backups"),
                   ATM_BACKUP_RESTORE_HOME_BASE=str(self.root), ATM_BACKUP_RESTORE_MEDIA_ROOT=str(self.media),
                   ATM_BACKUP_RESTORE_MOUNTINFO=str(self.mountinfo), ATM_DRY_RUN=dry_run,
                   PYTHONDONTWRITEBYTECODE="1")
        env.pop("SUDO_USER", None)
        script = '''set -Eeuo pipefail
source "$1/plugin.conf"
source "$1/plugin.sh"
atm_t() { printf '%s\\n' "${!1:-$1}"; }
atm_warn() { printf '%s\\n' "$*"; }
atm_info() { printf '%s\\n' "$*"; }
atm_success() { printf '%s\\n' "$*"; }
atm_require_commands() { command -v "$1" >/dev/null; }
'''
        return subprocess.run(["bash", "-c", script + commands, "test", str(plugin)],
                              input=inputs, capture_output=True, text=True, env=env, timeout=20)

    def test_menu_measures_before_devices_dry_run_and_no_recursive_default(self):
        result = self.shell("atm_backup_restore_menu", "3\n1\nx0\n", "1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("3) Backup ATM / Apps", result.stdout)
        self.assertLess(result.stdout.index('"source_bytes"'), result.stdout.index("USB Disk"))
        self.assertIn('"available_bytes"', result.stdout)
        self.assertNotIn("Apps backup folder (default)", result.stdout)
        self.assertEqual(list(self.device.iterdir()), [])

    def test_menu_cancel_and_confirm(self):
        cancel = self.shell("atm_backup_restore_prompt_apps", "1\nn\n")
        self.assertEqual(cancel.returncode, 0, cancel.stderr)
        self.assertIn("Cancelled", cancel.stdout)
        self.assertEqual(list(self.device.iterdir()), [])
        default = self.shell("atm_backup_restore_prompt_apps", "\n")
        self.assertNotEqual(default.returncode, 0)
        self.assertEqual(list(self.device.iterdir()), [])
        result = self.shell("atm_backup_restore_prompt_apps", "1\ny\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(list(self.device.rglob("Apps.tar.gz"))), 1)

    def test_cli_action_and_sudo_guard(self):
        result = self.shell('atm_backup_restore_install --action backup-apps --destination "$ATM_BACKUP_RESTORE_MEDIA_ROOT/USB Disk"', dry_run="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('"dry_run": true', result.stdout)
        result = self.shell("atm_backup_restore_install --action backup-apps", dry_run="1")
        self.assertNotEqual(result.returncode, 0)
        result = self.shell("SUDO_USER=someone atm_backup_restore_prompt_apps")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('"source_bytes"', result.stdout)
        self.assertEqual(list(self.device.iterdir()), [])

    def test_preflight_menu_cancel_eof_and_dry_run_do_not_select_devices(self):
        os.mkfifo(self.source / "blocked.fifo")
        for inputs in ("\n", "q\n", ""):
            result = self.shell("atm_backup_restore_prompt_apps", inputs)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Recheck", result.stdout)
            self.assertNotIn("USB Disk", result.stdout)
            self.assertNotIn("Continue?", result.stdout)
        result = self.shell("atm_backup_restore_prompt_apps", dry_run="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("Recheck", result.stdout)
        result = self.shell('atm_backup_restore_install --action backup-apps --destination "$ATM_BACKUP_RESTORE_MEDIA_ROOT/USB Disk"')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Apps preflight blocked", result.stderr)
        self.assertNotIn("Continue?", result.stdout)
        self.assertTrue((self.source / "blocked.fifo").exists())
        self.assertEqual(list(self.device.iterdir()), [])

    def test_preflight_retry_after_socket_resolved_proceeds_to_backup(self):
        path = self.source / "vscode/main.sock"
        path.parent.mkdir()
        with socket.socket(socket.AF_UNIX) as server:
            server.bind(str(path))
        # The fixture simulates the application cleaning up on user-requested retry.
        script = '''read() {
    builtin read "$@" || return 1
    if [[ "${choice:-}" == r && -S "$HOME/Apps/vscode/main.sock" ]]; then
        rm -- "$HOME/Apps/vscode/main.sock"
    fi
}
atm_backup_restore_prompt_apps
'''
        result = self.shell(script, "r\n1\ny\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("VS Code", result.stderr)
        self.assertIn("Recheck", result.stdout)
        self.assertIn("Preflight passed", result.stdout)
        self.assertLess(result.stdout.index("Preflight passed"), result.stdout.index("USB Disk"))
        self.assertEqual(len(list(self.device.rglob("Apps.tar.gz"))), 1)

    def test_blocker_appearing_after_confirmation_prevents_archive(self):
        script = '''read() {
    builtin read "$@" || return 1
    if [[ "${answer:-}" == y ]]; then
        mkfifo "$HOME/Apps/appeared.fifo"
    fi
}
atm_backup_restore_prompt_apps
'''
        result = self.shell(script, "1\ny\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Preflight passed", result.stdout)
        self.assertIn("appeared.fifo", result.stderr)
        self.assertEqual(list(self.device.iterdir()), [])


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Isolated Zsh snapshots: no shell configuration is executed."""

import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("zsh_state", Path(__file__).parents[1] / "state.py")
state = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(state)


class StateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-zsh-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "user home"
        self.home.mkdir()
        self.dot = self.home / "dot files"
        self.dot.mkdir()
        self.omz = self.home / ".oh-my-zsh"
        (self.omz / "custom").mkdir(parents=True)
        (self.omz / "oh-my-zsh.sh").write_text("# framework\n")
        (self.omz / "custom" / "theme.zsh").write_text("# theme\n")
        self.rc = self.dot / ".zshrc"
        self.rc.write_text("# personal\nexport PERSONAL=1\n")
        self.rc.chmod(0o600)
        self.args = argparse.Namespace(home=str(self.home), dot_dir=str(self.dot),
                                       omz=str(self.omz), custom=str(self.omz / "custom"),
                                       backups=str(self.root / "backups"), archive=None)

    def snapshot(self):
        self.args.archive = str(state.backup(self.args))
        return Path(self.args.archive)

    def archive(self, members, entries=None):
        path = self.root / "input.tar.gz"
        with tarfile.open(path, "w:gz") as output:
            metadata = json.dumps({"format": state.FORMAT, "entries": entries or ["zshrc"]}).encode()
            info = tarfile.TarInfo("metadata.json")
            info.size = len(metadata)
            output.addfile(info, io.BytesIO(metadata))
            for name, kind, content in members:
                info = tarfile.TarInfo(name)
                info.type = kind
                info.size = len(content) if kind == tarfile.REGTYPE else 0
                info.linkname = "/tmp/outside"
                output.addfile(info, io.BytesIO(content) if info.isfile() else None)
        self.args.archive = str(path)
        return path

    def test_round_trip_private_files_custom_and_permissions(self):
        (self.dot / ".zsh_history").write_text("history\n")
        (self.home / ".p10k.zsh").write_text("# prompt\n")
        self.omz.chmod(0o750)
        archive = self.snapshot()
        self.assertEqual(archive.stat().st_mode & 0o777, 0o600)
        self.rc.write_text("changed\n")
        (self.omz / "new-file").write_text("not in snapshot")
        state.restore(self.args)
        self.assertEqual(self.rc.read_text(), "# personal\nexport PERSONAL=1\n")
        self.assertEqual(self.rc.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.omz.stat().st_mode & 0o777, 0o750)
        self.assertEqual((self.dot / ".zsh_history").read_text(), "history\n")
        self.assertTrue((self.home / ".p10k.zsh").is_file())
        self.assertTrue((self.omz / "custom" / "theme.zsh").is_file())
        self.assertFalse((self.omz / "new-file").exists())
        self.assertFalse(list(self.home.glob(".atm-zsh-restore-*")))

    def test_external_custom_directory(self):
        custom = self.home / "custom themes"
        custom.mkdir()
        (custom / "mine.zsh").write_text("theme")
        self.args.custom = str(custom)
        self.snapshot()
        (custom / "mine.zsh").unlink()
        state.restore(self.args)
        self.assertEqual((custom / "mine.zsh").read_text(), "theme")

    def test_symlink_content_is_portable_and_destination_link_preserved(self):
        target = self.root / "actual rc"
        self.rc.rename(target)
        self.rc.symlink_to(target)
        self.snapshot()
        target.write_text("changed")
        state.restore(self.args)
        self.assertTrue(self.rc.is_symlink())
        self.assertIn("PERSONAL", target.read_text())
        with tarfile.open(self.args.archive) as archive:
            self.assertTrue(archive.getmember("zshrc").isfile())

    def test_configure_is_idempotent_and_never_executes_config(self):
        marker = self.root / "must-not-exist"
        original = f"touch '{marker}'\n# >>> ATM PATH >>>\nexport KEEP=1\n# <<< ATM PATH <<<\n"
        self.rc.write_text(original)
        state.configure(self.args)
        first = self.rc.read_text()
        state.configure(self.args)
        self.assertEqual(first, self.rc.read_text())
        self.assertIn(original, first)
        self.assertEqual(first.count(state.BEGIN), 1)
        self.assertIn("export ZSH_CUSTOM=", first)
        self.assertEqual(self.rc.stat().st_mode & 0o777, 0o600)
        self.assertFalse(marker.exists())
        state.configure(self.args, remove=True)
        self.assertEqual(self.rc.read_text(), original + "\n")

    def test_native_loader_preserved_on_install_disabled_on_remove(self):
        original = 'export ZSH="$HOME/.oh-my-zsh"\nsource "$ZSH/oh-my-zsh.sh"\nexport KEEP=1\n'
        self.rc.write_text(original)
        state.configure(self.args)
        self.assertEqual(self.rc.read_text(), original)
        state.configure(self.args, remove=True)
        self.assertIn('# ATM disabled Oh My Zsh loader: source "$ZSH/oh-my-zsh.sh"', self.rc.read_text())
        self.assertIn("export KEEP=1", self.rc.read_text())

    def test_invalid_layout_cannot_snapshot_home_or_put_backup_in_framework(self):
        for omz, backups in ((self.home, self.root / "backups"), (self.omz, self.omz / "backups")):
            with self.subTest(omz=omz, backups=backups):
                self.args.omz, self.args.backups = str(omz), str(backups)
                with self.assertRaises(ValueError):
                    state.backup(self.args)

    def test_unsafe_archives_rejected_without_touching_user_files(self):
        for name, kind in (("../escape", tarfile.REGTYPE), ("/escape", tarfile.REGTYPE),
                           ("unknown", tarfile.REGTYPE), ("zshrc", tarfile.SYMTYPE),
                           ("zshrc", tarfile.LNKTYPE), ("zshrc", tarfile.FIFOTYPE)):
            with self.subTest(name=name, kind=kind):
                self.archive([(name, kind, b"bad")])
                with self.assertRaises(ValueError):
                    state.restore(self.args)
                self.assertEqual(self.rc.read_text(), "# personal\nexport PERSONAL=1\n")

    def test_duplicates_and_incomplete_metadata_rejected(self):
        for members, entries in (([("zshrc", tarfile.REGTYPE, b"x")] * 2, ["zshrc"]),
                                  ([("zshrc", tarfile.REGTYPE, b"x")], ["zshrc", "zshenv"])):
            self.archive(members, entries)
            with self.assertRaises(ValueError):
                state.restore(self.args)

    def test_corrupt_archive_rejected(self):
        path = self.snapshot()
        content = path.read_bytes()
        path.write_bytes(content[:len(content) // 2])
        with self.assertRaises((EOFError, OSError, ValueError, tarfile.TarError)):
            state.restore(self.args)

    def test_failed_replace_rolls_back_all_applied_entries(self):
        self.snapshot()
        self.rc.write_text("current")
        (self.omz / "oh-my-zsh.sh").write_text("current framework")
        replace = os.replace

        def fail(source, destination):
            if Path(source).name == "new" and Path(destination) == self.omz:
                raise OSError("simulated replacement failure")
            return replace(source, destination)

        with patch.object(state.os, "replace", side_effect=fail):
            with self.assertRaises(OSError):
                state.restore(self.args)
        self.assertEqual(self.rc.read_text(), "current")
        self.assertEqual((self.omz / "oh-my-zsh.sh").read_text(), "current framework")

    def test_failed_rollback_retains_original_recovery_files(self):
        self.snapshot()
        self.rc.write_text("current")
        replace = os.replace

        def fail(source, destination):
            if Path(source).name == "old" or (Path(source).name == "new" and Path(destination) == self.omz):
                raise OSError("simulated disk failure")
            return replace(source, destination)

        with patch.object(state.os, "replace", side_effect=fail):
            with self.assertRaisesRegex(OSError, "recovery files retained"):
                state.restore(self.args)
        recovered = list(self.dot.glob(".atm-zsh-restore-*/old"))
        self.assertEqual(len(recovered), 1)
        self.assertEqual(recovered[0].read_text(), "current")


if __name__ == "__main__":
    unittest.main()

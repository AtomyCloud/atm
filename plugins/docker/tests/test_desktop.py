#!/usr/bin/env python3
"""Offline tests for Desktop release links and Docker CLI config cleanup."""

import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


HELPER = Path(__file__).resolve().parents[1] / "desktop.py"
MODULE = runpy.run_path(str(HELPER))


class ReleaseTests(unittest.TestCase):
    def test_versions_keep_their_linux_build_links(self):
        html = """
        <a href="https://desktop.docker.com/linux/main/amd64/99/docker-desktop-amd64.deb">nav</a>
        <h2><a>4.9.0</a></h2>
        <a href="https://desktop.docker.com/linux/main/amd64/90/docker-desktop-amd64.deb?utm_source=docs">DEB</a>
        <h3>Updates</h3>
        <a href="https://desktop.docker.com/linux/main/amd64/91/docker-desktop-amd64.deb">duplicate</a>
        <h2>4.10.0</h2>
        <a href="https://desktop.docker.com/linux/main/amd64/100/docker-desktop-amd64.deb">DEB</a>
        <a href="https://desktop.docker.com/linux/main/amd64/100/checksums.txt">checksum</a>
        <h2>4.11.0-rc1</h2>
        <a href="https://desktop.docker.com/linux/main/amd64/110/docker-desktop-amd64.deb">preview</a>
        <h2>4.12.0</h2>
        <a href="https://desktop.docker.com/mac/main/arm64/120/Docker.dmg">Mac only</a>
        <a href="https://example.com/linux/main/amd64/120/docker-desktop-amd64.deb">wrong host</a>
        """
        result = subprocess.run(
            [sys.executable, "-B", str(HELPER), "releases"],
            input=html, text=True, capture_output=True, check=True,
        )
        self.assertEqual(result.stdout.splitlines(), [
            "4.10.0\thttps://desktop.docker.com/linux/main/amd64/100/docker-desktop-amd64.deb",
            "4.9.0\thttps://desktop.docker.com/linux/main/amd64/90/docker-desktop-amd64.deb",
        ])

    def test_no_downloads_returns_failure_for_shell_fallback(self):
        result = subprocess.run(
            [sys.executable, "-B", str(HELPER), "releases"],
            input="<h2>4.99.0</h2><p>No Linux download</p>", text=True, capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")


class ConfigTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "config.json"
        self.original = {
            "credsStore": "desktop", "currentContext": "desktop-linux",
            "auths": {"registry.example": {}}, "plugins": {"buildx": {"test": True}},
        }
        self.path.write_text(json.dumps(self.original), encoding="utf-8")
        self.path.chmod(0o600)

    def test_removes_desktop_keys_preserving_other_settings_and_permissions(self):
        MODULE["clean_config"](self.path, False)
        self.assertEqual(json.loads(self.path.read_text()), {
            "auths": {"registry.example": {}}, "plugins": {"buildx": {"test": True}},
        })
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)

    def test_keeps_non_desktop_credentials_and_context(self):
        self.original.update(credsStore="pass", currentContext="production")
        self.path.write_text(json.dumps(self.original), encoding="utf-8")
        before = self.path.read_bytes()
        MODULE["clean_config"](self.path, False)
        self.assertEqual(self.path.read_bytes(), before)

    def test_preflight_does_not_write(self):
        before = self.path.read_bytes()
        MODULE["clean_config"](self.path, True)
        self.assertEqual(self.path.read_bytes(), before)

    def test_invalid_config_is_not_overwritten(self):
        for value in ("invalid JSON", "[]", '"not an object"'):
            with self.subTest(value=value):
                self.path.write_text(value, encoding="utf-8")
                with self.assertRaises(ValueError):
                    MODULE["clean_config"](self.path, False)
                self.assertEqual(self.path.read_text(), value)

    def test_missing_config_does_not_create_files(self):
        self.path.unlink()
        MODULE["clean_config"](self.path, False)
        self.assertFalse(self.path.exists())

    def test_symlink_to_config_is_preserved(self):
        link = self.path.parent / "link.json"
        link.symlink_to(self.path)
        MODULE["clean_config"](link, False)
        self.assertTrue(link.is_symlink())
        self.assertNotIn("credsStore", json.loads(self.path.read_text()))

    def test_failed_replace_keeps_original_and_cleans_temporary_file(self):
        before = self.path.read_bytes()
        with patch("os.replace", side_effect=OSError("test failure")):
            with self.assertRaises(OSError):
                MODULE["clean_config"](self.path, False)
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(list(self.path.parent.iterdir()), [self.path])


if __name__ == "__main__":
    unittest.main()

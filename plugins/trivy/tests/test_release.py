#!/usr/bin/env python3
"""Trivy release and extraction tests using temporary, inert payloads."""

import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location("trivy_release", Path(__file__).parents[1] / "release.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def metadata(self):
        names = ["trivy_0.76.1_Linux-64bit.tar.gz", "trivy_0.76.1_checksums.txt"]
        return {"tag_name": "v0.76.1", "prerelease": False, "draft": False,
                "assets": [{"name": name, "browser_download_url": release.BASE + "v0.76.1/" + name} for name in names]}

    def test_resolve_official_release_and_docker_version(self):
        data = self.metadata()
        self.assertEqual(release.resolve(data, "docker"), "0.76.1")
        version, archive, checksums = release.resolve(data, "Linux-64bit").split("\t")
        self.assertEqual(version, "0.76.1")
        self.assertTrue(archive.endswith("Linux-64bit.tar.gz"))
        self.assertTrue(checksums.endswith("_checksums.txt"))

    def test_previews_drafts_malformed_versions_missing_assets_and_foreign_urls(self):
        for changes in ({"prerelease": True}, {"draft": True}, {"tag_name": "v0.76.1-rc1"}, {"assets": []}):
            data = self.metadata()
            data.update(changes)
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                release.resolve(data, "Linux-64bit")
        data = self.metadata()
        data["assets"][0]["browser_download_url"] = "https://example.com/trivy.tar.gz"
        with self.assertRaises(ValueError):
            release.resolve(data, "Linux-64bit")
        data = self.metadata()
        data["assets"].append(dict(data["assets"][0]))
        with self.assertRaises(ValueError):
            release.resolve(data, "Linux-64bit")


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="atm-trivy-release-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive = self.root / "trivy_0.76.1_Linux-64bit.tar.gz"
        self.checksums = self.root / "trivy_0.76.1_checksums.txt"
        self.destination = self.root / "apps with spaces/0.76.1"

    def package(self, members=None):
        members = members if members is not None else [("trivy", tarfile.REGTYPE, b"not executed"), ("README.md", tarfile.REGTYPE, b"ignored")]
        with tarfile.open(self.archive, "w:gz") as output:
            for name, kind, content in members:
                member = tarfile.TarInfo(name)
                member.type = kind
                member.size = len(content) if kind == tarfile.REGTYPE else 0
                member.linkname = "/tmp/outside"
                output.addfile(member, io.BytesIO(content) if member.isfile() else None)
        self.checksums.write_text(hashlib.sha256(self.archive.read_bytes()).hexdigest() + "  " + self.archive.name + "\n")

    def test_checksum_verified_binary_only_install_and_no_overwrite(self):
        self.package()
        release.install(self.archive, self.checksums, self.destination)
        self.assertEqual((self.destination / "bin/trivy").read_bytes(), b"not executed")
        self.assertEqual((self.destination / "bin/trivy").stat().st_mode & 0o777, 0o755)
        self.assertTrue((self.destination / ".atm-trivy-managed").is_file())
        self.assertFalse((self.destination / "README.md").exists())
        with self.assertRaises(ValueError):
            release.install(self.archive, self.checksums, self.destination)
        self.assertFalse(list(self.destination.parent.glob(".atm-trivy-*")))

    def test_bad_missing_duplicate_and_malformed_checksums(self):
        self.package()
        correct = self.checksums.read_text()
        for text in ("0" * 64 + "  " + self.archive.name, "", correct + correct, "bad  " + self.archive.name):
            with self.subTest(text=text):
                self.checksums.write_text(text)
                with self.assertRaises(ValueError):
                    release.install(self.archive, self.checksums, self.destination)
                self.assertFalse(self.destination.exists())

    def test_unsafe_paths_links_special_files_missing_or_duplicate_binary(self):
        for members in ([('../outside', tarfile.REGTYPE, b"bad")], [("/outside", tarfile.REGTYPE, b"bad")],
                        [("trivy", tarfile.SYMTYPE, b"")], [("trivy", tarfile.LNKTYPE, b"")],
                        [("trivy", tarfile.FIFOTYPE, b"")], [("readme", tarfile.REGTYPE, b"none")],
                        [("trivy", tarfile.REGTYPE, b"one"), ("./trivy", tarfile.REGTYPE, b"two")]):
            with self.subTest(members=members):
                self.package(members)
                with self.assertRaises(ValueError):
                    release.install(self.archive, self.checksums, self.destination)
                self.assertFalse(self.destination.exists())


if __name__ == "__main__":
    unittest.main()

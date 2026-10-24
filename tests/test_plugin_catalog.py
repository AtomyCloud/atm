#!/usr/bin/env python3
"""Official-catalog tests; network and downloaded code are never executed."""

import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError
from urllib.parse import quote


SPEC = importlib.util.spec_from_file_location("plugin_catalog", Path(__file__).parents[1] / "lib/plugin_catalog.py")
catalog = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(catalog)
REVISION = "a" * 40


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory(prefix="atm-catalog-test-")
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        self.marker = self.root / "must-not-execute"
        self.files = {}
        self.entries = []
        self.requests = []
        self.truncated = False
        self.add_plugin("brave", "Brave Browser", "Official browser")
        self.add_plugin("browser_tools", "Browser Tools", "Brave profile helpers")
        self.add_plugin("go", "Go", "Go development tools")
        env = patch.dict(os.environ, {"GITHUB_TOKEN": ""})
        env.start()
        self.addCleanup(env.stop)
        transport = patch.object(catalog, "get_bytes", side_effect=self.fetch)
        transport.start()
        self.addCleanup(transport.stop)

    def add_file(self, plugin, relative, data, mode="100644"):
        if isinstance(data, str):
            data = data.encode()
        path = "plugins/" + plugin + "/" + relative
        self.entries[:] = [entry for entry in self.entries if entry["path"] != path]
        self.files[path] = data
        sha = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        self.entries.append({"path": path, "type": "blob", "mode": mode, "size": len(data), "sha": sha})

    def add_plugin(self, plugin, name, description):
        metadata = (f'ATM_PLUGIN_ID="{plugin}"\nATM_PLUGIN_NAME_VALUE="{name}"\n'
                    f'ATM_PLUGIN_DESCRIPTION_VALUE="{description}"\nATM_PLUGIN_VERSION_VALUE="0.0.1"\n'
                    'ATM_PLUGIN_ENTRYPOINT="plugin.sh"\n')
        self.add_file(plugin, "plugin.metadata", metadata)
        self.add_file(plugin, "plugin.conf", f"touch '{self.marker}'\n")
        self.add_file(plugin, "plugin.sh", f"#!/usr/bin/env bash\ntouch '{self.marker}'\n", "100755")
        self.add_file(plugin, "lang/en-us.lang", 'ATM_TEST_LABEL="Test"\n')
        self.add_file(plugin, "assets/file with spaces.txt", "plugin data")

    def fetch(self, url, limit, token=""):
        self.requests.append((url, token))
        if url == catalog.API + "/commits/main":
            return json.dumps({"sha": REVISION}).encode()
        if url == catalog.API + "/git/trees/" + REVISION + "?recursive=1":
            return json.dumps({"truncated": self.truncated, "tree": self.entries}).encode()
        for path, data in self.files.items():
            if url == catalog.RAW + "/" + REVISION + "/" + quote(path, safe="/"):
                return data
        if url.startswith(catalog.API + "/git/blobs/"):
            sha = url.rsplit("/", 1)[1]
            for entry in self.entries:
                if entry["sha"] == sha:
                    return json.dumps({"encoding": "base64", "content": base64.b64encode(self.files[entry["path"]]).decode()}).encode()
        raise AssertionError("Unexpected network destination: " + url)

    def test_search_matches_ids_names_descriptions_and_file_paths(self):
        remote = catalog.Catalog()
        self.assertEqual([row.split("\t")[0] for row in remote.search("BRAVE")], ["brave", "browser_tools"])
        self.assertEqual(len(remote.search("Browser")), 2)
        self.assertEqual(len(remote.search("spaces.txt")), 3)
        self.assertEqual(len(remote.search("plugins/brave/plugin.sh")), 1)
        self.assertEqual(len(remote.search("")), 3)
        self.assertEqual(remote.search("not-present"), [])
        self.assertFalse(self.marker.exists())
        self.assertFalse((self.root / "plugins").exists())
        self.assertTrue(all(REVISION in row for row in remote.search("go")))

    def test_complete_download_preserves_files_modes_and_receipt_without_execution(self):
        target = catalog.Catalog().download("brave", self.root / "plugins")
        for path, data in self.files.items():
            if path.startswith("plugins/brave/"):
                self.assertEqual((target / path.removeprefix("plugins/brave/")).read_bytes(), data)
        self.assertEqual((target / "plugin.sh").stat().st_mode & 0o777, 0o755)
        self.assertEqual((target / "plugin.conf").stat().st_mode & 0o777, 0o644)
        receipt = json.loads((target / ".atm-origin.json").read_text())
        self.assertEqual(receipt["commit"], REVISION)
        self.assertEqual(receipt["source"], catalog.SOURCE)
        self.assertEqual(list(target.parent.iterdir()), [target])
        self.assertFalse(self.marker.exists())

    def test_search_snapshot_remains_pinned_for_download(self):
        remote = catalog.Catalog(REVISION)
        remote.download("go", self.root / "plugins")
        self.assertNotIn(catalog.API + "/commits/main", [url for url, _ in self.requests])
        self.assertTrue(all(REVISION in url for url, _ in self.requests))

    def test_existing_directory_and_broken_symlink_are_not_overwritten(self):
        plugins = self.root / "plugins"
        plugins.mkdir()
        target = plugins / "brave"
        target.mkdir()
        (target / "local.conf").write_text("keep")
        with self.assertRaisesRegex(catalog.CatalogError, "not be overwritten"):
            catalog.Catalog().download("brave", plugins)
        self.assertEqual((target / "local.conf").read_text(), "keep")
        (plugins / "go").symlink_to(self.root / "missing")
        with self.assertRaises(catalog.CatalogError):
            catalog.Catalog().download("go", plugins)
        self.assertTrue((plugins / "go").is_symlink())

    def test_missing_plugin_invalid_id_and_revision_are_rejected(self):
        remote = catalog.Catalog()
        for plugin in ("absent", "../escape", "brave;bad", "https://other.example/test"):
            with self.subTest(plugin=plugin), self.assertRaises(catalog.CatalogError):
                remote.download(plugin, self.root / "plugins")
        self.assertFalse((self.root / "plugins").exists())
        with self.assertRaises(catalog.CatalogError):
            catalog.Catalog("main;bad")

    def test_malicious_metadata_is_never_executed(self):
        for content in (f'ATM_PLUGIN_ID="brave"\nATM_PLUGIN_NAME_VALUE="$(touch {self.marker})"\n',
                        f'ATM_PLUGIN_ID="brave"\nATM_PLUGIN_NAME_VALUE="ok";touch {self.marker}\n',
                        'ATM_PLUGIN_ID="another"\n', 'ATM_PLUGIN_ID="brave"\nATM_PLUGIN_ENTRYPOINT="../outside.sh"\n'):
            with self.subTest(content=content):
                self.add_file("brave", "plugin.metadata", content)
                with self.assertRaises(catalog.CatalogError):
                    catalog.Catalog().search("brave")
                self.assertFalse(self.marker.exists())

    def test_unsafe_repository_paths_and_duplicate_paths_are_rejected(self):
        for path in ("plugins/brave/../../escape", "plugins/brave/.git/config", "plugins/brave/evil\npath", "plugins/brave//file"):
            self.entries.append({"path": path, "type": "blob"})
            with self.subTest(path=path), self.assertRaises(catalog.CatalogError):
                catalog.Catalog()
            self.entries.pop()
        self.entries.append(dict(self.entries[0]))
        with self.assertRaises(catalog.CatalogError):
            catalog.Catalog()

    def test_links_submodules_and_oversize_files_are_rejected(self):
        for changes in ({"mode": "120000"}, {"mode": "160000", "type": "commit"}, {"size": catalog.MAX_FILE + 1}):
            self.add_file("brave", "unsafe", "data")
            self.entries[-1].update(changes)
            with self.subTest(changes=changes), self.assertRaises(catalog.CatalogError):
                catalog.Catalog().download("brave", self.root / "plugins")
            self.assertFalse((self.root / "plugins").exists())

    def test_bad_hash_and_network_failure_clean_staging(self):
        for failure in ("hash", "network"):
            original = self.fetch

            def broken(url, limit, token=""):
                if url.endswith("/plugin.sh"):
                    if failure == "hash":
                        return b"changed contents"
                    raise catalog.CatalogError("simulated network failure")
                return original(url, limit, token)

            with self.subTest(failure=failure), patch.object(catalog, "get_bytes", side_effect=broken):
                with self.assertRaises(catalog.CatalogError):
                    catalog.Catalog().download("brave", self.root / "plugins")
            self.assertEqual(list((self.root / "plugins").iterdir()), [])

    def test_bad_bash_syntax_and_missing_required_files_are_rejected(self):
        self.add_file("brave", "plugin.sh", "if then\n")
        with self.assertRaisesRegex(catalog.CatalogError, "syntax"):
            catalog.Catalog().download("brave", self.root / "plugins")
        self.assertEqual(list((self.root / "plugins").iterdir()), [])
        self.entries[:] = [entry for entry in self.entries if entry["path"] != "plugins/brave/plugin.conf"]
        with self.assertRaisesRegex(catalog.CatalogError, "Required"):
            catalog.Catalog().metadata("brave")

    def test_truncated_tree_and_active_download_lock_are_rejected(self):
        self.truncated = True
        with self.assertRaisesRegex(catalog.CatalogError, "Incomplete"):
            catalog.Catalog()
        self.truncated = False
        lock = self.root / "plugins/.atm-download-lock-brave"
        lock.mkdir(parents=True)
        with self.assertRaisesRegex(catalog.CatalogError, "lock"):
            catalog.Catalog().download("brave", lock.parent)
        self.assertTrue(lock.is_dir())

    def test_token_only_uses_official_api_and_control_characters_are_sanitized(self):
        with patch.dict(os.environ, {"GITHUB_TOKEN": "test-token"}):
            catalog.Catalog().download("brave", self.root / "plugins")
        self.assertTrue(all(url.startswith(catalog.API + "/") and token == "test-token" for url, token in self.requests))
        self.assertEqual(catalog.clean("name\n\x1b[31m"), "name  [31m")


class TransportTests(unittest.TestCase):
    def test_redirects_and_official_host_credential_boundary(self):
        with self.assertRaises(catalog.CatalogError):
            catalog.NoRedirects().redirect_request(None, None, 302, "redirect", None, "https://other.example/")
        with self.assertRaises(catalog.CatalogError):
            catalog.get_bytes("https://other.example/file", 10, "test-token")
        with self.assertRaisesRegex(catalog.CatalogError, "invalid characters"):
            catalog.get_bytes(catalog.API + "/commits/main", 10, "secret\ninvalid")

    def test_http_rate_limit_missing_repository_and_network_errors(self):
        for error in (HTTPError(catalog.API, 403, "rate limit", {}, None),
                      HTTPError(catalog.API, 404, "missing", {}, None), URLError("offline")):
            with self.subTest(error=error), patch.object(catalog, "build_opener") as opener:
                opener.return_value.open.side_effect = error
                with self.assertRaises(catalog.CatalogError):
                    catalog.get_bytes(catalog.API + "/commits/main", 100)

    def test_response_size_limit(self):
        with patch.object(catalog, "build_opener") as opener:
            opener.return_value.open.return_value.__enter__.return_value.read.return_value = b"12345"
            with self.assertRaises(catalog.CatalogError):
                catalog.get_bytes(catalog.API + "/commits/main", 4)


if __name__ == "__main__":
    unittest.main()

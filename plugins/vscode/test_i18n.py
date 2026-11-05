"""Isolated localization coverage for Go, Java, Flutter, and VS Code."""
import configparser
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog

PLUGINS = ("go", "java", "flutter", "vscode")
VERSIONS = {"go": "1.26.2", "java": "26.0.1", "flutter": "3.41.8", "vscode": "1.118.1"}
PRODUCTS = {"go": "Go", "java": "Java", "flutter": "Flutter", "vscode": "VS Code"}
PRINTF = re.compile(r"%(?:[-+#0 ']*)(?:\d+|\*)?(?:\.(?:\d+|\*))?([diouxXeEfFgGaAcspqb%])")
SETUP = r'''
set -Eeuo pipefail
ATM_PLUGIN_DIR="$1/plugins"
ATM_APPS_DIR="$2/apps"
ATM_DOWNLOAD_DIR="$2/downloads"
ATM_MANIFEST_DIR="$2/manifests"
HOME="$2/home"
ATM_DRY_RUN=1
plugin="$3"
source "$1/lib/atm_i18n.sh"
source "$ATM_PLUGIN_DIR/$plugin/plugin.conf"
atm_i18n_load_plugin_lang "$plugin"
source "$ATM_PLUGIN_DIR/$plugin/plugin.sh"
atm_fail() { printf '%s\n' "$*" >&2; exit 1; }
atm_warn() { printf '%s\n' "$*"; }
atm_success() { printf '%s\n' "$*"; }
atm_manifest_write() { :; }
atm_download_file() { :; }
'''

class PluginLocalizationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-toolchain-i18n-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def catalog(self, plugin, locale):
        return read_catalog(REPO / "plugins" / plugin / "lang" / (locale + ".lang"))

    def run_shell(self, plugin, body, locale=None, input_text=None):
        env = {k: v for k, v in os.environ.items()
               if k != "ATM_LANG" and not k.startswith("ATM_PLUGIN_")}
        if locale is not None:
            env["ATM_LANG"] = locale
        return subprocess.run(
            ["bash", "-c", SETUP + "\n" + body, "test", str(REPO), str(self.root), plugin],
            input=input_text, text=True, capture_output=True, env=env, timeout=10)

    def test_catalog_contracts_and_shell_parser(self):
        for plugin in PLUGINS:
            english = self.catalog(plugin, "en-us")
            prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
            self.assertIn(prefix + "NAME", english)
            self.assertIn(prefix + "DESCRIPTION", english)
            source = (REPO / "plugins" / plugin / "plugin.sh").read_text()
            references = set(re.findall(r"\batm_t\s+(ATM_PLUGIN_[A-Z0-9_]+)", source))
            self.assertFalse(references - english.keys())
            subprocess.run(["bash", "-n", str(REPO / "plugins" / plugin / "plugin.sh")], check=True)
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale):
                    catalog = self.catalog(plugin, locale)
                    self.assertEqual(set(catalog), set(english))
                    for key, value in catalog.items():
                        self.assertEqual(PRINTF.findall(value), PRINTF.findall(english[key]), key)
                        if locale != "en-us" and not key.endswith("_NAME"):
                            self.assertNotEqual(value, english[key], key)
                    result = self.run_shell(plugin, r"""for key in $(compgen -A variable "ATM_PLUGIN_${plugin^^}_"); do printf '%s\0%s\0' "$key" "${!key}"; done""", locale)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    fields = result.stdout.split("\0")[:-1]
                    self.assertEqual(dict(zip(fields[::2], fields[1::2])), catalog)

    def test_errors_and_status_in_every_locale(self):
        for plugin in PLUGINS:
            prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
            for locale in LOCALES:
                catalog = self.catalog(plugin, locale)
                cases = [
                    (f"atm_{plugin}_normalize_version 'bad%value'", "INVALID_VERSION", ("bad%value",)),
                    (f"atm_{plugin}_version_from_args --version", "MISSING_VERSION", ()),
                    (f"atm_{plugin}_use", "USE_USAGE", ()),
                    (f"atm_{plugin}_remove", "REMOVE_USAGE", ()),
                ]
                for body, key, values in cases:
                    with self.subTest(plugin=plugin, locale=locale, key=key):
                        result = self.run_shell(plugin, body, locale)
                        self.assertNotEqual(result.returncode, 0)
                        expected = catalog[prefix + key]
                        self.assertEqual(result.stderr, (expected % values if values else expected) + "\n")
                result = self.run_shell(plugin, f"atm_{plugin}_status", locale)
                self.assertEqual(result.stdout, catalog[prefix + "STATUS_NOT_INSTALLED"] + "\n")

    def test_default_english_errors_are_unchanged(self):
        for plugin in PLUGINS:
            example = {"go": ". Expected format: 1.26.2", "java": "",
                       "flutter": ". Expected format: 3.41.8",
                       "vscode": ". Expected format: 1.118 or 1.118.0"}[plugin]
            result = self.run_shell(plugin, f"atm_{plugin}_normalize_version bad")
            self.assertEqual(result.stderr, f"Invalid {PRODUCTS[plugin]} version: bad{example}\n")
            result = self.run_shell(plugin, f"atm_{plugin}_version_from_args --version")
            self.assertEqual(result.stderr, "Missing value for --version\n")

    def test_cancelled_uninstall_preserves_fixture(self):
        sentinel = self.root / "apps/keep.txt"
        sentinel.parent.mkdir()
        sentinel.write_text("keep")
        for plugin in PLUGINS:
            prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
            for locale in LOCALES:
                catalog = self.catalog(plugin, locale)
                result = self.run_shell(plugin, f"atm_{plugin}_uninstall", locale, "n\n")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(catalog[prefix + "CONFIRM_CONTINUE"], result.stdout)
                self.assertIn("[y/N]", catalog[prefix + "CONFIRM_CONTINUE"])
                self.assertIn(catalog[prefix + "CANCELLED"], result.stdout)
                self.assertEqual(sentinel.read_text(), "keep")
                result = self.run_shell(plugin, f"atm_{plugin}_uninstall", locale, "y\n")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(catalog[prefix + "DRY_RUN"] + ": rm -rf ", result.stdout)
                self.assertIn(catalog[prefix + "UNINSTALLED"], result.stdout)
                self.assertEqual(sentinel.read_text(), "keep")

    def test_dry_installs_only_use_temporary_paths(self):
        for plugin in PLUGINS:
            prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
            for locale in LOCALES:
                catalog = self.catalog(plugin, locale)
                result = self.run_shell(plugin, f"atm_{plugin}_install --version {VERSIONS[plugin]}", locale)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(catalog[prefix + "DRY_RUN"] + ": ln -sfn ", result.stdout)
                self.assertIn(catalog[prefix + "INSTALLING"], result.stdout)
                self.assertNotIn("[missing:", result.stdout)
                self.assertFalse(list(self.root.rglob("current")))
                self.assertFalse(list(self.root.rglob("*.manifest")))

    def test_protocol_outputs_are_locale_independent(self):
        for plugin in PLUGINS:
            body = f"atm_{plugin}_version_from_args --version {VERSIONS[plugin]}; atm_{plugin}_path_entries"
            baseline = self.run_shell(plugin, body).stdout
            for locale in LOCALES:
                result = self.run_shell(plugin, body, locale)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, baseline)
        for locale in LOCALES:
            result = self.run_shell("java", "atm_java_vendor_slug amazon; atm_java_vendor_slug custom", locale)
            self.assertEqual(result.stdout, "corretto\ncustom\n")

    def test_dry_migration_does_not_copy_or_back_up_data(self):
        source = self.root / "apps/vscode/code_1.117.0/data"
        target = self.root / "apps/vscode/code_1.118.1/data"
        source.mkdir(parents=True)
        target.mkdir(parents=True)
        (source / "source.txt").write_text("source")
        (target / "target.txt").write_text("target")
        for locale in LOCALES:
            result = self.run_shell("vscode", "atm_vscode_migrate_data 1.117.0 1.118.1", locale)
            expected = self.catalog("vscode", locale)["ATM_PLUGIN_VSCODE_DRY_MIGRATE"] % (source, target)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, expected + "\n")
            self.assertEqual(list(target.iterdir()), [target / "target.txt"])
            self.assertFalse(list(target.parent.glob("data.backup.*")))

    def test_desktop_locales_and_execution_fields(self):
        locales = ("pt_BR", "pt_PT", "es", "it", "fr", "de", "ru", "ja", "zh_CN", "ko")
        directory = REPO / "plugins/vscode"
        paths = list(directory.glob("*.desktop.in")) + list((directory / "atalhos vscode").glob("*"))
        for path in paths:
            parser = configparser.ConfigParser(interpolation=None)
            parser.optionxform = str
            parser.read(path, encoding="utf-8")
            entry = parser["Desktop Entry"]
            self.assertIn("Exec", entry)
            for locale in locales:
                self.assertIn("Comment[" + locale + "]", entry)
                self.assertIn("GenericName[" + locale + "]", entry)
                if "URL Handler" in entry["Name"]:
                    self.assertIn("Name[" + locale + "]", entry)
                if parser.has_section("Desktop Action new-empty-window"):
                    self.assertIn("Name[" + locale + "]", parser["Desktop Action new-empty-window"])


if __name__ == "__main__":
    unittest.main()

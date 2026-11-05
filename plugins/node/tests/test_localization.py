"""Localization contracts for Node and the Android plugins, using temporary fixtures."""

import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog

PLUGINS = ("android_sdk", "android_studio", "node")
PRINTF = re.compile(r"%(?:[-+#0 ']*)(?:\d+|\*)?(?:\.(?:\d+|\*))?([diouxXeEfFgGaAcspqb%])")
PRELUDE = r'''
set -Eeuo pipefail
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/lang/en-us.lang"
source "$ATM_HOME/lang/$ATM_LANG.lang"
source "$ATM_PLUGIN_DIR/$PLUGIN/plugin.conf"
source "$ATM_PLUGIN_DIR/$PLUGIN/plugin.sh"
atm_i18n_load_plugin_lang "$PLUGIN"
atm_fail() { printf '%s\n' "$*" >&2; exit 1; }
atm_warn() { printf '%s\n' "$*" >&2; }
atm_info() { printf '%s\n' "$*"; }
atm_success() { printf '%s\n' "$*"; }
clear() { :; }
curl() { printf 'UNEXPECTED_NETWORK\n' >&2; return 99; }
wget() { printf 'UNEXPECTED_NETWORK\n' >&2; return 99; }
sudo() { printf 'UNEXPECTED_PRIVILEGE\n' >&2; return 99; }
update-desktop-database() { :; }
'''


class LocalizationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-assigned-i18n-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith("ATM_")}
        self.env.update(
            HOME=str(self.root), ATM_HOME=str(REPO),
            ATM_PLUGIN_DIR=str(REPO / "plugins"),
            ATM_APPS_DIR=str(self.root / "Apps with spaces"),
            ATM_DOWNLOAD_DIR=str(self.root / "downloads"),
            ATM_MANIFEST_DIR=str(self.root / "manifests"),
            ATM_NODE_LOCAL_BINARY=str(self.root / "local/node"),
            ATM_NODE_SYSTEM_BINARY=str(self.root / "system/node"),
            ATM_NODE_SYSTEM_NPM=str(self.root / "system/npm"),
            TERM="dumb", PYTHONDONTWRITEBYTECODE="1",
        )

    def run_shell(self, plugin, locale, script, input=""):
        env = dict(self.env, PLUGIN=plugin, ATM_LANG=locale)
        return subprocess.run(
            ["bash", "-c", PRELUDE + script], env=env, input=input,
            cwd=REPO, text=True, capture_output=True, timeout=10,
        )

    def catalog(self, plugin, locale):
        return read_catalog(REPO / "plugins" / plugin / "lang" / (locale + ".lang"))

    def test_catalogs_keys_formats_references_and_real_translations(self):
        for plugin in PLUGINS:
            prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
            english = self.catalog(plugin, "en-us")
            self.assertIn(prefix + "NAME", english)
            self.assertIn(prefix + "DESCRIPTION", english)
            source = (REPO / "plugins" / plugin / "plugin.sh").read_text()
            references = set(re.findall(r"\batm_t\s+(ATM_PLUGIN_[A-Z0-9_]+)", source))
            self.assertFalse(references - english.keys())
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale):
                    messages = self.catalog(plugin, locale)
                    self.assertEqual(messages.keys(), english.keys())
                    for key, value in messages.items():
                        self.assertEqual(PRINTF.findall(value), PRINTF.findall(english[key]), key)
                        if locale != "en-us" and key != prefix + "NAME":
                            self.assertNotEqual(value, english[key], key)
                        if "[y/N]" in english[key]:
                            self.assertIn("[y/N]", value, key)
                        for token in re.findall(r"--[a-z-]+|<[^>]+>", english[key]):
                            self.assertIn(token, value, key)

    def test_bash_and_python_catalog_parsing_match(self):
        for plugin in PLUGINS:
            prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale):
                    result = self.run_shell(plugin, locale, r'''
for key in $(compgen -A variable "ATM_PLUGIN_${PLUGIN^^}_"); do
    printf '%s\0%s\0' "$key" "${!key}"
done
''')
                    self.assertEqual(result.returncode, 0, result.stderr)
                    values = result.stdout.split("\0")[:-1]
                    parsed = dict(zip(values[::2], values[1::2]))
                    self.assertEqual(parsed, self.catalog(plugin, locale), prefix)

    def test_menus_open_and_cancel_without_side_effects(self):
        for plugin in PLUGINS:
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale):
                    result = self.run_shell(plugin, locale, f"atm_{plugin}_menu\n", "b\n")
                    self.assertEqual(result.returncode, 0, result.stderr)
                    title = self.catalog(plugin, locale)["ATM_PLUGIN_" + plugin.upper() + "_MENU_TITLE"]
                    self.assertIn(title, result.stdout)
                    self.assertNotIn("[missing:", result.stdout + result.stderr)
                    self.assertNotIn("UNEXPECTED_", result.stdout + result.stderr)
                    self.assertEqual(list(self.root.iterdir()), [])

    def test_errors_are_localized_and_english_text_is_unchanged(self):
        cases = (
            ("node", "atm_node_normalize_version invalid", "INVALID_VERSION", ("invalid",),
             "Invalid Node.js version: invalid. Expected format: 26.2.0"),
            ("android_studio", "atm_android_studio_normalize_version invalid", "INVALID_VERSION", ("invalid",),
             "Invalid Android Studio version: invalid. Expected format: 2025.3.4.6"),
            ("android_sdk", "atm_android_sdk_stack_packages invalid", "UNKNOWN_STACK", ("invalid",),
             "Unknown Android SDK stack: invalid"),
            ("android_sdk", "atm_android_sdk_version_from_args --platform", "MISSING_VALUE", ("--platform",),
             "Missing value for --platform"),
        )
        for plugin, command, key, args, english in cases:
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale, key=key):
                    result = self.run_shell(plugin, locale, command)
                    self.assertNotEqual(result.returncode, 0)
                    expected = self.catalog(plugin, locale)["ATM_PLUGIN_" + plugin.upper() + "_" + key] % args
                    self.assertEqual(result.stderr, expected + "\n")
                    if locale == "en-us":
                        self.assertEqual(result.stderr, english + "\n")

    def test_machine_output_stays_invariant(self):
        cases = (
            ("node", "atm_node_normalize_version node-v26.2.0", "", "26.2.0\n"),
            ("node", "atm_node_choose_location", "2\n", "system\n"),
            ("android_sdk", "atm_android_sdk_version_from_args --platform android-36",
             "", "custom|android-36|||\n"),
            ("android_sdk", "atm_android_sdk_stack_packages api36", "",
             "platform-tools\nemulator\nplatforms;android-36\nbuild-tools;36.0.0\ncmake;3.31.6\nndk;29.0.14206865\n"),
            ("android_studio", "atm_android_studio_version_from_args --id custom --name 'Android Studio' "
             "--version 2025.3.4.6 --package studio.tar.gz --url https://example.invalid/studio.tar.gz",
             "", "custom|Android Studio|2025.3.4.6|studio.tar.gz|https://example.invalid/studio.tar.gz\n"),
        )
        for plugin, command, input, expected in cases:
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale, command=command):
                    result = self.run_shell(plugin, locale, command, input)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout, expected)

    def test_dry_run_commands_and_confirmation_tokens_stay_invariant(self):
        for plugin in PLUGINS:
            for locale in LOCALES:
                with self.subTest(plugin=plugin, locale=locale):
                    result = self.run_shell(plugin, locale, f"ATM_DRY_RUN=1 atm_{plugin}_uninstall\n", "y\n")
                    self.assertEqual(result.returncode, 0, result.stderr)
                    messages = self.catalog(plugin, locale)
                    prefix = "ATM_PLUGIN_" + plugin.upper() + "_"
                    self.assertIn(messages[prefix + "CONFIRM_CONTINUE"], result.stdout)
                    self.assertIn(messages[prefix + "DRY_RUN"] + ": rm -rf ", result.stdout)
                    self.assertIn(messages[prefix + "DRY_RUN"] + ": rm -f ", result.stdout)
                    self.assertEqual(list(self.root.iterdir()), [])

    def test_desktop_comments_use_selected_locale_without_changing_exec(self):
        for locale in LOCALES:
            with self.subTest(locale=locale):
                result = self.run_shell(
                    "android_studio", locale,
                    "atm_android_studio_create_desktop_entry 'Android Studio' 2025.3.4.6\n",
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                desktop = next((self.root / ".local/share/applications").glob("*.desktop"))
                content = desktop.read_text()
                messages = self.catalog("android_studio", locale)
                self.assertIn("Comment=" + messages["ATM_PLUGIN_ANDROID_STUDIO_DESKTOP_COMMENT"] + "\n", content)
                self.assertIn("GenericName=" + messages["ATM_PLUGIN_ANDROID_STUDIO_DESKTOP_GENERIC_NAME"] + "\n", content)
                self.assertIn("Exec=" + self.env["ATM_APPS_DIR"] + "/AndroidStudio/current/bin/studio.sh\n", content)


if __name__ == "__main__":
    unittest.main()

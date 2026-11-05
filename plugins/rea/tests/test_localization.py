"""Plugin-local locale contracts; only temporary fixtures and read-only commands."""

import os
from pathlib import Path
import re
import string
import subprocess
import sys
import tempfile
import unittest

PLUGIN = Path(__file__).resolve().parents[1]
REPO = PLUGIN.parents[1]
PREFIX = "ATM_PLUGIN_" + PLUGIN.name.upper() + "_"
sys.path.insert(0, str(REPO / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog


class LocalizationTests(unittest.TestCase):
    def test_catalogs_are_complete_literal_and_preserve_parameters(self):
        english = read_catalog(PLUGIN / "lang/en-us.lang")
        formatter = string.Formatter()
        for locale in LOCALES:
            with self.subTest(locale=locale):
                path = PLUGIN / "lang" / (locale + ".lang")
                catalog = read_catalog(path)
                self.assertEqual(catalog.keys(), english.keys())
                self.assertIn(PREFIX + "NAME", catalog)
                self.assertIn(PREFIX + "DESCRIPTION", catalog)
                for key, value in catalog.items():
                    self.assertTrue(value.strip(), key)
                    self.assertEqual(re.findall(r"%[sq]", value), re.findall(r"%[sq]", english[key]), key)
                    self.assertEqual(
                        sorted(field for _, field, _, _ in formatter.parse(value) if field),
                        sorted(field for _, field, _, _ in formatter.parse(english[key]) if field), key)
                    if "[y/N]" in english[key]:
                        self.assertIn("[y/N]", value, key)
                env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_PLUGIN_")}
                output = subprocess.run(
                    ["bash", "-c", 'set -eu; source "$1"; for key in ${!ATM_PLUGIN_@}; do '
                     'printf "%s\\0%s\\0" "$key" "${!key}"; done', "test", str(path)],
                    env=env, capture_output=True, check=True, timeout=5).stdout.decode().split("\0")[:-1]
                self.assertEqual(dict(zip(output[::2], output[1::2])), catalog)

    def test_helper_errors_are_translated_and_versions_stay_machine_readable(self):
        for locale in (*LOCALES, "en_GB", "unknown-locale"):
            with self.subTest(locale=locale):
                selected = locale if locale in LOCALES else "en-us"
                catalog = read_catalog(PLUGIN / "lang" / (selected + ".lang"))
                env = dict(os.environ, ATM_LANG=locale)
                for value, status in (('"not-a-version"', 1), ('"6.8.0"', 0)):
                    result = subprocess.run(
                        [sys.executable, "-B", str(PLUGIN / "manage.py"), "release"],
                        input=value, env=env, capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, status)
                    if status:
                        self.assertEqual(result.stderr.strip(), "REA: " + catalog[PREFIX + "EXACT_VERSION"])
                    else:
                        self.assertEqual(result.stdout, "6.8.0\n")
                        self.assertEqual(result.stderr, "")

    def test_shell_dry_run_and_named_paths_in_every_locale(self):
        script = r'''
set -Eeuo pipefail
ATM_HOME="$1"
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/plugins/rea/plugin.conf"
source "$ATM_HOME/plugins/rea/plugin.sh"
source "$ATM_HOME/plugins/rea/lang/$ATM_LANG.lang"
atm_rea_install --path "$HOME/REA path with spaces"
atm_rea_install --update --path "$HOME/REA path with spaces"
atm_rea_uninstall
atm_rea_manage validate relative
'''
        with tempfile.TemporaryDirectory(prefix="atm-rea-locales-") as temp:
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    catalog = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                    env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
                    env.update(HOME=temp, ATM_LANG=locale, ATM_DRY_RUN="1")
                    result = subprocess.run(
                        ["bash", "-c", script, "test", str(REPO)], env=env,
                        capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn(catalog[PREFIX + "PLAN"], result.stdout)
                    self.assertIn("rea-agents@latest", result.stdout)
                    self.assertIn("REA\\ path\\ with\\ spaces", result.stdout)
                    self.assertIn(catalog[PREFIX + "ABSOLUTE_PATH"], result.stderr)
                    self.assertNotIn("[missing:", result.stdout + result.stderr)
                    self.assertEqual(list(Path(temp).iterdir()), [])


if __name__ == "__main__":
    unittest.main()

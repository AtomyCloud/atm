"""Plugin-local locale contracts; only temporary fixtures and read-only commands."""

import os
import json
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

    def test_helper_errors_help_and_release_protocol_in_every_locale(self):
        base = "https://github.com/LargeModGames/spotatui/releases/download/v0.46.0/"
        asset = "spotatui-linux-x86_64.tar.gz"
        release = dict(tag_name="v0.46.0", draft=False, prerelease=False,
                       assets=[dict(name=name, browser_download_url=base + name)
                               for name in (asset, asset + ".sha256")])
        for locale in (*LOCALES, "en_GB", "unknown-locale"):
            with self.subTest(locale=locale):
                selected = locale if locale in LOCALES else "en-us"
                catalog = read_catalog(PLUGIN / "lang" / (selected + ".lang"))
                env = dict(os.environ, ATM_LANG=locale)
                command = [sys.executable, "-B", str(PLUGIN / "release.py")]
                for value, status in (({}, 1), (release, 0)):
                    result = subprocess.run(
                        command + ["resolve", "linux-x86_64"], input=json.dumps(value),
                        env=env, capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, status)
                    if status:
                        self.assertEqual(result.stderr.strip(), catalog[PREFIX + "STABLE_RELEASE"])
                    else:
                        self.assertEqual(result.stdout, "\t".join(("0.46.0", base + asset, base + asset + ".sha256")) + "\n")
                help_result = subprocess.run(command + ["--help"], env=env, capture_output=True,
                                             text=True, check=True, timeout=5)
                self.assertTrue(help_result.stdout.startswith(catalog[PREFIX + "HELP_USAGE"]))
                self.assertIn(catalog[PREFIX + "HELP_ARGUMENTS"], help_result.stdout)
                self.assertIn(catalog[PREFIX + "HELP_EXIT"], " ".join(help_result.stdout.split()))
                self.assertIn("--help", help_result.stdout)

    def test_shell_dry_run_does_not_probe_or_write_in_every_locale(self):
        script = r'''
set -Eeuo pipefail
ATM_HOME="$1"
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/plugins/spotatui/plugin.conf"
source "$ATM_HOME/plugins/spotatui/plugin.sh"
source "$ATM_HOME/plugins/spotatui/lang/$ATM_LANG.lang"
python3() { printf 'UNEXPECTED_PROBE\n' >&2; return 99; }
curl() { printf 'UNEXPECTED_NETWORK\n' >&2; return 99; }
atm_spotatui_check_dependencies
atm_spotatui_install --location local
'''
        with tempfile.TemporaryDirectory(prefix="atm-spotatui-locales-") as temp:
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    catalog = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                    env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
                    env.update(HOME=temp, ATM_LANG=locale, ATM_DRY_RUN="1")
                    result = subprocess.run(
                        ["bash", "-c", script, "test", str(REPO)], env=env,
                        capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn(catalog[PREFIX + "PLAN"], result.stdout)
                    self.assertIn(catalog[PREFIX + "ENTRY_PLAN"], result.stdout)
                    self.assertNotIn("[missing:", result.stdout + result.stderr)
                    self.assertNotIn("UNEXPECTED_", result.stdout + result.stderr)
                    self.assertEqual(list(Path(temp).iterdir()), [])


if __name__ == "__main__":
    unittest.main()

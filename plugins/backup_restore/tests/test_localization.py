"""Locale coverage with temporary fixtures and no real user backups."""

import ast
from collections import Counter
import json
import os
from pathlib import Path
import re
import string
import subprocess
import sys
import tempfile
import unittest

PLUGIN = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PLUGIN.parents[1] / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog

PREFIX = "ATM_PLUGIN_BACKUP_RESTORE_"


class LocalizationTests(unittest.TestCase):
    def env(self, locale):
        env = {key: value for key, value in os.environ.items()
               if not key.startswith("ATM_") and key != "SUDO_USER"}
        env.update(ATM_LANG=locale, PYTHONDONTWRITEBYTECODE="1")
        return env

    def helper(self, locale, *args):
        return subprocess.run([sys.executable, "-B", str(PLUGIN / "state.py"), *args],
                              env=self.env(locale), capture_output=True, text=True, timeout=10)

    def test_catalogs_references_placeholders_and_shell_literals(self):
        english = read_catalog(PLUGIN / "lang/en-us.lang")
        self.assertIn(PREFIX + "NAME", english)
        self.assertIn(PREFIX + "DESCRIPTION", english)
        for path in PLUGIN.glob("*.py"):
            for node in ast.walk(ast.parse(path.read_text())):
                if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                        and node.func.id == "tr" and node.args):
                    self.assertIn(node.args[0].value, english)
                    self.assertNotIn("key", [arg.arg for arg in node.keywords])
        shell = (PLUGIN / "plugin.sh").read_text()
        for key in re.findall(r"atm_t (ATM_PLUGIN_[A-Z0-9_]+)", shell):
            self.assertIn(key, english)
        formatter = string.Formatter()
        for locale in LOCALES:
            with self.subTest(locale=locale):
                path = PLUGIN / "lang" / (locale + ".lang")
                catalog = read_catalog(path)
                self.assertEqual(set(english), set(catalog))
                for key, value in catalog.items():
                    self.assertTrue(value.strip(), key)
                    fields = lambda text: Counter((field, spec, conv) for _, field, spec, conv
                                                  in formatter.parse(text) if field is not None)
                    self.assertEqual(fields(value), fields(english[key]), key)
                result = subprocess.run(["bash", "-c", 'source "$1"; for key in $(compgen -A variable ATM_PLUGIN_); do printf "%s\\0%s\\0" "$key" "${!key}"; done',
                                         "test", str(path)], env=self.env(locale), capture_output=True, check=True)
                pairs = result.stdout.decode().split("\0")[:-1]
                self.assertEqual(dict(zip(pairs[::2], pairs[1::2])), catalog)

    def test_diagnostics_menus_help_and_confirmation_tokens(self):
        for locale in LOCALES:
            with self.subTest(locale=locale):
                catalog = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                result = self.helper(locale, "backup")
                self.assertEqual(result.returncode, 1)
                expected = catalog[PREFIX + "ERROR_DETAIL"].format(error=catalog[PREFIX + "DESTINATION_REQUIRED"])
                self.assertEqual(result.stderr.strip(), expected)
                self.assertEqual(result.stdout, "")
                help_result = self.helper(locale, "--help")
                self.assertEqual(help_result.returncode, 0)
                self.assertIn(" ".join(catalog[PREFIX + "HELP_DESCRIPTION"].split()),
                              " ".join(help_result.stdout.split()))
                script = '''set -Eeuo pipefail
source "$1/lang/$ATM_LANG.lang"
source "$1/plugin.sh"
atm_t() { printf '%s\\n' "${!1:-$1}"; }
atm_backup_restore_menu <<< 0
atm_backup_restore_confirm <<< y
if atm_backup_restore_confirm <<< n; then exit 99; fi
'''
                menu = subprocess.run(["bash", "-c", script, "test", str(PLUGIN)],
                                      env=self.env(locale), capture_output=True, text=True, timeout=10)
                self.assertEqual(menu.returncode, 0, menu.stderr)
                self.assertIn(catalog[PREFIX + "TITLE"], menu.stdout)
                self.assertIn("[y/N]", menu.stdout)
                self.assertNotIn("[missing:", menu.stdout)

    def test_json_plans_invariant_and_preflight_localized(self):
        with tempfile.TemporaryDirectory(prefix="atm-backup-locale-") as temp:
            root = Path(temp)
            home = root / "home"
            (home / "Documents").mkdir(parents=True)
            (home / "Apps").mkdir()
            (home / "Documents/file").write_text("fixture")
            args = ["backup", "--home", str(home), "--destination", str(root / "backups"),
                    "--type", "custom", "--path", "Documents", "--dry-run",
                    "--media-root", str(root / "media")]
            baseline = json.loads(self.helper("en-us", *args).stdout)
            sizes = json.loads(self.helper("en-us", "apps-size", "--home", str(home)).stdout)
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    plan = self.helper(locale, *args)
                    self.assertEqual(plan.returncode, 0, plan.stderr)
                    self.assertEqual(json.loads(plan.stdout), baseline)
                    self.assertEqual(json.loads(self.helper(locale, "apps-size", "--home", str(home)).stdout), sizes)
                    fifo = home / "Apps/blocked.fifo"
                    os.mkfifo(fifo)
                    blocked = self.helper(locale, "apps-size", "--home", str(home))
                    fifo.unlink()
                    self.assertEqual(blocked.returncode, 1)
                    catalog = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                    self.assertIn(catalog[PREFIX + "SPECIAL_FILE_BLOCKED"].format(kind="FIFO"), blocked.stderr)
                    self.assertNotIn("{newline}", blocked.stderr)
            self.assertFalse((root / "backups").exists())
            self.assertEqual(list((home / "Apps").iterdir()), [])


if __name__ == "__main__":
    unittest.main()

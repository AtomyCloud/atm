"""Locale coverage without installations or real shell configuration changes."""

import ast
from collections import Counter
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

PREFIX = "ATM_PLUGIN_ZSH_"


class LocalizationTests(unittest.TestCase):
    def env(self, locale):
        env = {key: value for key, value in os.environ.items()
               if not key.startswith("ATM_") and key != "SUDO_USER"}
        env.update(ATM_LANG=locale, PYTHONDONTWRITEBYTECODE="1")
        return env

    def test_catalogs_references_placeholders_and_shell_literals(self):
        english = read_catalog(PLUGIN / "lang/en-us.lang")
        self.assertIn(PREFIX + "NAME", english)
        self.assertIn(PREFIX + "DESCRIPTION", english)
        for node in ast.walk(ast.parse((PLUGIN / "state.py").read_text())):
            if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                    and node.func.id == "tr" and node.args):
                self.assertIn(node.args[0].value, english)
        for key in re.findall(r"atm_t (ATM_PLUGIN_[A-Z0-9_]+)", (PLUGIN / "plugin.sh").read_text()):
            self.assertIn(key, english)
        formatter = string.Formatter()
        for locale in LOCALES:
            with self.subTest(locale=locale):
                path = PLUGIN / "lang" / (locale + ".lang")
                catalog = read_catalog(path)
                self.assertEqual(set(catalog), set(english))
                for key, value in catalog.items():
                    self.assertTrue(value.strip(), key)
                    fields = lambda text: Counter((field, spec, conv) for _, field, spec, conv
                                                  in formatter.parse(text) if field is not None)
                    self.assertEqual(fields(value), fields(english[key]), key)
                    self.assertEqual(re.findall(r"%[a-z%]", value), re.findall(r"%[a-z%]", english[key]), key)
                result = subprocess.run(["bash", "-c", 'source "$1"; for key in $(compgen -A variable ATM_PLUGIN_); do printf "%s\\0%s\\0" "$key" "${!key}"; done',
                                         "test", str(path)], env=self.env(locale), capture_output=True, check=True)
                pairs = result.stdout.decode().split("\0")[:-1]
                self.assertEqual(dict(zip(pairs[::2], pairs[1::2])), catalog)

    def test_menus_dry_run_prompts_and_subprocess_locale(self):
        with tempfile.TemporaryDirectory(prefix="atm-zsh-locale-") as temp:
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    catalog = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                    script = '''set -Eeuo pipefail
source "$1/lang/$ATM_LANG.lang"
source "$1/plugin.sh"
atm_t() { printf '%s\\n' "${!1:-$1}"; }
atm_warn() { printf '%s\\n' "$*"; }
clear() { :; }
atm_zsh_status() { :; }
ATM_DRY_RUN=1 ATM_ZSH_BACKUP_DIR="$2/backups"
atm_zsh_menu <<< q
'''
                    menu = subprocess.run(["bash", "-c", script, "test", str(PLUGIN), temp],
                                          env=self.env(locale), capture_output=True, text=True, timeout=10)
                    self.assertEqual(menu.returncode, 0, menu.stderr)
                    self.assertIn(catalog[PREFIX + "INSTALL"], menu.stdout)
                    self.assertNotIn("[missing:", menu.stdout)
                    script = script.replace('atm_zsh_menu <<< q', '''atm_zsh_backup
atm_zsh_confirm "" <<< y
if atm_zsh_confirm "" <<< n; then exit 99; fi
export -n ATM_LANG
ATM_ZSH_USER_HOME="$2/home" ATM_ZSH_DOT_DIR="$2/dot" ATM_ZSH_OMZ_DIR=/ ATM_ZSH_CUSTOM_DIR="$2/custom"
if atm_zsh_state validate --archive "$2/unused"; then exit 98; fi''')
                    run = subprocess.run(["bash", "-c", script, "test", str(PLUGIN), temp],
                                         env=self.env(locale), capture_output=True, text=True, timeout=10)
                    self.assertEqual(run.returncode, 0, run.stderr)
                    self.assertIn(catalog[PREFIX + "DRY_RUN"] % (catalog[PREFIX + "BACKUP_PLAN"], temp + "/backups"), run.stdout)
                    self.assertIn("[y/N]", run.stdout)
                    self.assertEqual(run.stderr.strip(), catalog[PREFIX + "INVALID_OMZ_DIRECTORY"])
            self.assertEqual(list(Path(temp).iterdir()), [])

    def test_help_and_localized_disabled_loader_remain_safe(self):
        with tempfile.TemporaryDirectory(prefix="atm-zsh-comment-") as temp:
            root = Path(temp)
            args = ["--home", temp, "--dot-dir", temp, "--omz", str(root / "omz"),
                    "--custom", str(root / "omz/custom"), "--backups", str(root / "backups")]
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    catalog = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                    help_result = subprocess.run([sys.executable, "-B", str(PLUGIN / "state.py"), "--help"],
                                                 env=self.env(locale), capture_output=True, text=True, timeout=10)
                    self.assertEqual(help_result.returncode, 0)
                    self.assertIn(" ".join(catalog[PREFIX + "HELP_DESCRIPTION"].split()),
                                  " ".join(help_result.stdout.split()))
                    original = 'source "$ZSH/oh-my-zsh.sh"\n'
                    (root / ".zshrc").write_text(original)
                    result = subprocess.run([sys.executable, "-B", str(PLUGIN / "state.py"), "unconfigure", *args],
                                            env=self.env(locale), capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual((root / ".zshrc").read_text(), catalog[PREFIX + "DISABLED_LOADER"] + original)
                    self.assertTrue((root / ".zshrc").read_text().startswith("# "))
            self.assertEqual(list(root.iterdir()), [root / ".zshrc"])


if __name__ == "__main__":
    unittest.main()

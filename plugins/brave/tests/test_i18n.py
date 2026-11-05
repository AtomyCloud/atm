"""Localized backup output with disposable profiles and no real backups."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

PLUGIN = Path(__file__).resolve().parents[1]
ROOT = PLUGIN.parents[1]
sys.path.insert(0, str(ROOT / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog


class BackupLocaleTests(unittest.TestCase):
    def test_dry_run_and_errors_in_every_locale(self):
        with tempfile.TemporaryDirectory(prefix="atm-brave-locales-") as temporary:
            home = Path(temporary)
            profile = home / "profile with {braces}"
            profile.mkdir()
            output = home / "backups"
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    messages = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                    env = dict(os.environ, ATM_LANG=locale, PYTHONDONTWRITEBYTECODE="1")
                    command = [sys.executable, "-B", str(PLUGIN / "full_backup.py"),
                               "--home", str(home), "--backups", str(output), "--dry-run"]
                    result = subprocess.run(command + ["--standard-profile", str(profile)],
                                            env=env, capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn(messages["ATM_PLUGIN_BRAVE_FULL_DRY_RUN"], result.stdout)
                    self.assertIn(messages["ATM_PLUGIN_BRAVE_FULL_SOURCE"].format(
                        kind="standard", source=profile), result.stdout)
                    self.assertFalse(output.exists())
                    result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 1)
                    self.assertEqual(result.stderr.strip(), messages["ATM_PLUGIN_BRAVE_FULL_ERROR"].format(
                        error=messages["ATM_PLUGIN_BRAVE_FULL_NO_SOURCES"]))

    def test_shell_passes_unexported_locale_to_helper(self):
        script = '''set -Eeuo pipefail
ATM_HOME="$PWD"
source lib/atm_core.sh
source lib/atm_i18n.sh
source plugins/brave/plugin.sh
source plugins/brave/lang/pt-br.lang
export -n ATM_LANG
ATM_LANG=pt-br ATM_DRY_RUN=1
ATM_BRAVE_PROFILE_DIR="$HOME/profile"
ATM_BRAVE_FULL_BACKUP_DIR="$HOME/backups"
atm_brave_full_backup
'''
        with tempfile.TemporaryDirectory(prefix="atm-brave-locale-handoff-") as temporary:
            (Path(temporary) / "profile").mkdir()
            env = {key: value for key, value in os.environ.items()
                   if not key.startswith("ATM_") and key != "SUDO_USER"}
            env.update(HOME=temporary, PYTHONDONTWRITEBYTECODE="1")
            result = subprocess.run(["bash", "-c", script], cwd=ROOT, env=env,
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            messages = read_catalog(PLUGIN / "lang/pt-br.lang")
            self.assertIn(messages["ATM_PLUGIN_BRAVE_FULL_DRY_RUN"], result.stdout)
            self.assertFalse((Path(temporary) / "backups").exists())

    def test_english_default_and_locale_aliases(self):
        for locale in (None, "en_GB.UTF-8", "unavailable"):
            with self.subTest(locale=locale), tempfile.TemporaryDirectory() as temporary:
                env = dict(os.environ)
                env.pop("ATM_LANG", None)
                if locale is not None:
                    env["ATM_LANG"] = locale
                result = subprocess.run(
                    [sys.executable, "-B", str(PLUGIN / "full_backup.py"), "--home", temporary,
                     "--backups", str(Path(temporary) / "backups"), "--dry-run"],
                    env=env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stderr.strip(),
                                 "Brave full backup: No Brave data directories found (standard, Flatpak or Snap).")


if __name__ == "__main__":
    unittest.main()

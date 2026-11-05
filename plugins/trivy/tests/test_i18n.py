"""Locale-aware diagnostics must not change release protocol output."""

import json
import os
from pathlib import Path
import subprocess
import sys
import unittest

PLUGIN = Path(__file__).resolve().parents[1]
ROOT = PLUGIN.parents[1]
sys.path.insert(0, str(ROOT / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog


class ReleaseLocaleTests(unittest.TestCase):
    def run_helper(self, locale, data, platform="Linux-64bit"):
        env = dict(os.environ, ATM_LANG=locale, PYTHONDONTWRITEBYTECODE="1")
        return subprocess.run([sys.executable, "-B", str(PLUGIN / "release.py"), "resolve", platform],
                              input=json.dumps(data), env=env, capture_output=True, text=True, timeout=10)

    def test_localized_errors_and_invariant_release_fields(self):
        version = "0.76.1"
        base = "https://github.com/aquasecurity/trivy/releases/download/v" + version + "/"
        names = ["trivy_0.76.1_Linux-64bit.tar.gz", "trivy_0.76.1_checksums.txt"]
        metadata = {"tag_name": "v" + version, "draft": False, "prerelease": False,
                    "assets": [{"name": name, "browser_download_url": base + name} for name in names]}
        expected = "\t".join([version, *(base + name for name in names)]) + "\n"
        for locale in LOCALES:
            with self.subTest(locale=locale):
                messages = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                result = self.run_helper(locale, metadata)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, expected)
                self.assertEqual(result.stderr, "")
                result = self.run_helper(locale, metadata, "docker")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, version + "\n")
                result = self.run_helper(locale, {})
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, "")
                self.assertEqual(result.stderr.strip(), messages["ATM_PLUGIN_TRIVY_EXPECTED_STABLE"])
                result = self.run_helper(locale, dict(metadata, assets=[]))
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stderr.strip(),
                                 messages["ATM_PLUGIN_TRIVY_ASSET_SOURCE_MISMATCH"].format(name=names[0]))

    def test_shell_passes_unexported_locale_and_confirmation_tokens(self):
        script = '''set -Eeuo pipefail
source lib/atm_i18n.sh
source plugins/trivy/plugin.sh
source "plugins/trivy/lang/$1.lang"
export -n ATM_LANG
ATM_LANG="$1"
atm_require_commands() { :; }
atm_warn() { printf '%s\\n' "$1"; }
curl() { printf '{}\\n'; }
if atm_trivy_release latest docker; then exit 99; fi
[[ "$ATM_PLUGIN_TRIVY_CONFIRM" == *'[y/N]:' ]]
atm_trivy_confirm fixture <<< y
if atm_trivy_confirm fixture <<< n; then exit 99; fi
'''
        for locale in LOCALES:
            with self.subTest(locale=locale):
                messages = read_catalog(PLUGIN / "lang" / (locale + ".lang"))
                result = subprocess.run(["bash", "-c", script, "test", locale], cwd=ROOT,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(result.stderr.strip(), messages["ATM_PLUGIN_TRIVY_EXPECTED_STABLE"])
                self.assertIn(messages["ATM_PLUGIN_TRIVY_CONFIRM"], result.stdout)
                self.assertIn(messages["ATM_PLUGIN_TRIVY_CANCELLED"], result.stdout)


if __name__ == "__main__":
    unittest.main()

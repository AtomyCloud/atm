"""Exercise localized AI output without installing or executing AI tools."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog


class AiLocalizationTests(unittest.TestCase):
    def test_ollama_help_dry_run_and_confirmation_in_every_locale(self):
        script = r'''set -Eeuo pipefail
source "$1/lib/atm_i18n.sh"
source "$1/plugins/ai/lang/en-us.lang"
source "$1/plugins/ai/lang/$2.lang"
source "$1/plugins/ai/plugin.sh"
atm_ai_ollama_installed() { return 1; }
atm_warn() { printf '%s\n' "$1"; }
atm_ai_print_ollama_usage
ATM_DRY_RUN=1 atm_ai_run_elevated 'apt-get update'
atm_ai_codex_confirm_remove fixture <<< y
if atm_ai_codex_confirm_remove fixture <<< n; then exit 90; fi
'''
        with tempfile.TemporaryDirectory(prefix="atm-ai-locales-") as temp:
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    messages = read_catalog(REPO / "plugins/ai/lang" / (locale + ".lang"))
                    env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
                    env.update(HOME=temp, ATM_LANG=locale)
                    result = subprocess.run(
                        ["bash", "-c", script, "test", str(REPO), locale],
                        env=env, capture_output=True, text=True, timeout=10,
                    )
                    self.assertEqual(result.returncode, 0, result.stderr)
                    for key in ("OLLAMA_INSTALLED", "OLLAMA_USAGE", "VERSION_DRY_RUN",
                                "CODEX_CONFIRM", "CODEX_CANCELLED"):
                        self.assertIn(messages["ATM_PLUGIN_AI_" + key], result.stdout)
                    self.assertIn(messages["ATM_PLUGIN_AI_DRY_RUN"] + " apt-get update", result.stdout)
                    self.assertIn("ollama run <model-name>", result.stdout)
                    self.assertIn("ollama pull llama3", result.stdout)
                    self.assertIn("[y/N]", messages["ATM_PLUGIN_AI_CODEX_CONFIRM"])
                    self.assertNotIn("[missing:", result.stdout)
            self.assertEqual(list(Path(temp).iterdir()), [])


if __name__ == "__main__":
    unittest.main()

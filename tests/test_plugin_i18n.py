"""Locale contracts and isolated plugin-loader tests; no installations or network."""

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
from unittest import mock


REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "lib"))
from atm_plugin_i18n import LOCALES, normalize, read_catalog, translator

PRINTF = re.compile(r"%(?:[-+#0 ']*)(?:\d+|\*)?(?:\.(?:\d+|\*))?([diouxXeEfFgGaAcspqb%])")


class TranslatorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-i18n-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.plugin = self.root / "plugins/demo"
        (self.plugin / "lang").mkdir(parents=True)
        (self.plugin / "lang/en-us.lang").write_text(
            'ATM_PLUGIN_DEMO_HELLO="Hello {name}"\nATM_PLUGIN_DEMO_KEEP="Keep {name}"\n', encoding="utf-8")
        (self.plugin / "lang/pt-br.lang").write_text('ATM_PLUGIN_DEMO_HELLO="Ol\u00e1 {name}"\n', encoding="utf-8")

    def test_normalize_selected_locale_and_missing_key_fallback(self):
        for raw, expected in (("pt_BR.UTF-8", "pt-br"), ("en_GB", "en-us"), ("pt", "pt-pt"),
                              ("zh_Hans", "zh-cn"), ("../../evil", "en-us")):
            self.assertEqual(normalize(raw), expected)
        with mock.patch.dict(os.environ, ATM_LANG="pt_BR.UTF-8"):
            tr = translator(self.plugin / "helper.py")
        self.assertEqual(tr("ATM_PLUGIN_DEMO_HELLO", name="ATM"), "Ol\u00e1 ATM")
        self.assertEqual(tr("ATM_PLUGIN_DEMO_KEEP", name="ATM"), "Keep ATM")

    def test_malformed_locale_and_bad_placeholders_fall_back(self):
        path = self.plugin / "lang/pt-br.lang"
        for content in ('ATM_PLUGIN_DEMO_HELLO="unterminated\n', 'ATM_PLUGIN_DEMO_HELLO="Oi {wrong}"\n'):
            path.write_text(content, encoding="utf-8")
            with mock.patch.dict(os.environ, ATM_LANG="pt-br"):
                self.assertEqual(translator(self.plugin / "helper.py")("ATM_PLUGIN_DEMO_HELLO", name="ATM"), "Hello ATM")

    def test_catalog_does_not_execute_shell_and_rejects_duplicates(self):
        path = self.root / "untrusted.lang"
        marker = self.root / "must-not-exist"
        path.write_text(f'ATM_PLUGIN_DEMO_TEXT="$(touch {marker})"\n', encoding="utf-8")
        self.assertIn("$(touch", read_catalog(path)["ATM_PLUGIN_DEMO_TEXT"])
        self.assertFalse(marker.exists())
        path.write_text('ATM_PLUGIN_DEMO_TEXT="one"\nATM_PLUGIN_DEMO_TEXT="two"\n', encoding="utf-8")
        with self.assertRaises(ValueError):
            read_catalog(path)

    def test_bash_fallback_and_python_subprocess_share_locale(self):
        script = '''set -Eeuo pipefail
source "$1/lib/atm_i18n.sh"
ATM_PLUGIN_DIR="$2/plugins"
ATM_LANG=pt-br
atm_i18n_load_plugin_lang demo
[[ "$(atm_t ATM_PLUGIN_DEMO_HELLO)" == 'Ol\u00e1 {name}' ]]
[[ "$(atm_t ATM_PLUGIN_DEMO_KEEP)" == 'Keep {name}' ]]
python3 -B -c 'import os; assert os.environ["ATM_LANG"] == "pt-br"'
'''
        result = subprocess.run(["bash", "-c", script, "test", str(REPO), str(self.root)],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_loader_uses_localized_name_and_description(self):
        (self.plugin / "plugin.metadata").write_text('ATM_PLUGIN_ID="demo"\nATM_PLUGIN_NAME_VALUE="Demo"\nATM_PLUGIN_DESCRIPTION_VALUE="English"\n')
        (self.plugin / "plugin.sh").write_text("#!/usr/bin/env bash\n")
        (self.plugin / "lang/pt-br.lang").write_text('ATM_PLUGIN_DEMO_NAME="Exemplo"\nATM_PLUGIN_DEMO_DESCRIPTION="Descri\u00e7\u00e3o"\n', encoding="utf-8")
        script = '''set -Eeuo pipefail
source "$1/lib/atm_i18n.sh"
source "$1/lib/atm_plugin_loader.sh"
atm_debug() { :; }
ATM_PLUGIN_DIR="$2/plugins" ATM_LANG=pt-br
atm_plugin_arrays_init
atm_plugin_load_metadata "$ATM_PLUGIN_DIR/demo"
[[ "${ATM_PLUGIN_NAME[demo]}" == Exemplo ]]
[[ "${ATM_PLUGIN_DESCRIPTION[demo]}" == Descri\u00e7\u00e3o ]]
'''
        result = subprocess.run(["bash", "-c", script, "test", str(REPO), str(self.root)],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_language_switch_refreshes_plugins_and_names_in_same_session(self):
        (self.plugin / "plugin.metadata").write_text('ATM_PLUGIN_ID="demo"\nATM_PLUGIN_NAME_VALUE="Demo"\n')
        (self.plugin / "plugin.sh").write_text("#!/usr/bin/env bash\n")
        (self.plugin / "lang/en-us.lang").write_text('ATM_PLUGIN_DEMO_NAME="Demo"\nATM_PLUGIN_DEMO_DESCRIPTION="Description"\nATM_PLUGIN_DEMO_LABEL="Ready"\n')
        (self.plugin / "lang/pt-br.lang").write_text('ATM_PLUGIN_DEMO_NAME="Exemplo"\nATM_PLUGIN_DEMO_DESCRIPTION="Descri\u00e7\u00e3o"\nATM_PLUGIN_DEMO_LABEL="Pronto"\n', encoding="utf-8")
        script = '''set -Eeuo pipefail
source "$1/lib/atm_i18n.sh"
source "$1/lib/atm_plugin_loader.sh"
atm_debug() { :; }
ATM_PLUGIN_DIR="$2/plugins" ATM_LANG=en-us
atm_plugin_arrays_init
atm_plugin_load_metadata "$ATM_PLUGIN_DIR/demo"
ATM_LANG=pt-br
atm_plugin_reload_languages
[[ "${ATM_PLUGIN_NAME[demo]}" == Exemplo && "$ATM_PLUGIN_DEMO_LABEL" == Pronto ]]
ATM_LANG=en-us
atm_plugin_reload_languages
[[ "${ATM_PLUGIN_NAME[demo]}" == Demo && "$ATM_PLUGIN_DEMO_LABEL" == Ready ]]
'''
        result = subprocess.run(["bash", "-c", script, "test", str(REPO), str(self.root)],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


class PluginCatalogTests(unittest.TestCase):
    def test_all_plugins_have_matching_locale_keys_and_printf_parameters(self):
        for directory in sorted((REPO / "plugins").iterdir()):
            if not (directory / "plugin.metadata").exists():
                continue
            english = read_catalog(directory / "lang/en-us.lang")
            prefix = "ATM_PLUGIN_" + directory.name.upper().replace("-", "_")
            self.assertIn(prefix + "_NAME", english, directory.name)
            self.assertIn(prefix + "_DESCRIPTION", english, directory.name)
            for locale in LOCALES:
                with self.subTest(plugin=directory.name, locale=locale):
                    translated = read_catalog(directory / "lang" / (locale + ".lang"))
                    self.assertEqual(set(translated), set(english))
                    for key, source in english.items():
                        self.assertTrue(translated[key].strip(), key)
                        self.assertEqual(PRINTF.findall(translated[key]), PRINTF.findall(source), key)

    def test_literal_references_resolve_and_python_placeholders_match(self):
        formatter = string.Formatter()
        for directory in sorted((REPO / "plugins").iterdir()):
            if not (directory / "plugin.metadata").exists():
                continue
            english = read_catalog(directory / "lang/en-us.lang")
            python_keys = set()
            for path in directory.glob("*.py"):
                for node in ast.walk(ast.parse(path.read_text(encoding="utf-8"))):
                    if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                            and node.func.id in ("tr", "_") and node.args
                            and isinstance(node.args[0], ast.Constant) and isinstance(node.args[0].value, str)
                            and node.args[0].value.startswith("ATM_PLUGIN_")):
                        python_keys.add(node.args[0].value)
            shell_keys = set()
            for path in directory.glob("*.sh"):
                shell_keys.update(re.findall(r"\batm_t\s+(ATM_PLUGIN_[A-Z0-9_]+)(?=[\s)'\"])", path.read_text(encoding="utf-8")))
            self.assertFalse((python_keys | shell_keys) - english.keys(), directory.name)
            for locale in LOCALES:
                translated = read_catalog(directory / "lang" / (locale + ".lang"))
                for key in python_keys:
                    with self.subTest(plugin=directory.name, locale=locale, key=key):
                        source_fields = Counter((field, spec, conversion) for _, field, spec, conversion in formatter.parse(english[key]) if field is not None)
                        target_fields = Counter((field, spec, conversion) for _, field, spec, conversion in formatter.parse(translated[key]) if field is not None)
                        self.assertEqual(target_fields, source_fields)

    def test_bash_and_python_read_identical_literal_values(self):
        for path in sorted((REPO / "plugins").glob("*/lang/*.lang")):
            with self.subTest(path=str(path.relative_to(REPO))):
                parsed = read_catalog(path)
                script = '''set -Eeuo pipefail
source "$1"
for key in $(compgen -A variable ATM_PLUGIN_); do printf '%s\\0%s\\0' "$key" "${!key}"; done
'''
                env = {key: value for key, value in os.environ.items() if not key.startswith("ATM_PLUGIN_")}
                result = subprocess.run(["bash", "-c", script, "test", str(path)], env=env,
                                        capture_output=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                pairs = result.stdout.decode("utf-8").split("\0")[:-1]
                self.assertEqual(dict(zip(pairs[::2], pairs[1::2])), parsed)

    def test_all_plugin_menus_open_in_every_locale_without_actions(self):
        with tempfile.TemporaryDirectory(prefix="atm-locale-menus-") as temp:
            home = Path(temp) / "home"
            home.mkdir()
            script = '''set -Eeuo pipefail
ATM_HOME="$1" ATM_LANG="$3" ATM_DRY_RUN=1
source "$ATM_HOME/lib/atm_core.sh"
atm_core_source_libs
source "$ATM_LANG_DIR/en-us.lang"
source "$ATM_LANG_DIR/$ATM_LANG.lang"
atm_plugin_arrays_init
atm_plugin_load_metadata "$ATM_PLUGIN_DIR/$2"
status_func="${ATM_PLUGIN_STATUS_FUNC[$2]}"
[[ "$status_func" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]
eval "$status_func() { printf 'fixture-status\\\\n'; }"
clear() { :; }
curl() { printf 'UNEXPECTED_NETWORK\\n' >&2; return 99; }
wget() { printf 'UNEXPECTED_NETWORK\\n' >&2; return 99; }
sudo() { printf 'UNEXPECTED_PRIVILEGE\\n' >&2; return 99; }
"${ATM_PLUGIN_MENU_FUNC[$2]}" <<< q
'''
            for directory in sorted((REPO / "plugins").iterdir()):
                if not (directory / "plugin.metadata").exists():
                    continue
                prefix = "ATM_PLUGIN_" + directory.name.upper().replace("-", "_")
                for locale in LOCALES:
                    with self.subTest(plugin=directory.name, locale=locale):
                        messages = read_catalog(directory / "lang" / (locale + ".lang"))
                        env = {key: value for key, value in os.environ.items()
                               if not key.startswith("ATM_") and key not in ("SUDO_USER", "ZSH", "ZSH_CUSTOM", "ZDOTDIR")}
                        env.update(HOME=str(home), TERM="dumb", PYTHONDONTWRITEBYTECODE="1")
                        result = subprocess.run(["bash", "-c", script, "test", str(REPO), directory.name, locale],
                                                env=env, text=True, capture_output=True, timeout=15)
                        output = result.stdout + result.stderr
                        self.assertEqual(result.returncode, 0, output)
                        self.assertNotIn("[missing:", output)
                        self.assertNotIn("UNEXPECTED_", output)
                        title = messages.get(prefix + "_MENU_TITLE", messages.get(prefix + "_TITLE"))
                        self.assertIsNotNone(title)
                        self.assertIn(title, output)
                        self.assertEqual(list(home.iterdir()), [], "Menu must not create personal files")


if __name__ == "__main__":
    unittest.main()

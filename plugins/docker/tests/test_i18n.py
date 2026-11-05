"""Localized helper diagnostics must not change Docker's machine contracts."""

import contextlib
import io
import json
import os
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[3]
HELPER = REPO / "plugins/docker/desktop.py"
sys.path.insert(0, str(REPO / "lib"))
from atm_plugin_i18n import LOCALES, read_catalog


class DockerLocalizationTests(unittest.TestCase):
    def test_shell_dry_run_templates_and_confirmation_tokens(self):
        script = r'''set -Eeuo pipefail
source "$1/lib/atm_i18n.sh"
source "$1/plugins/docker/lang/en-us.lang"
source "$1/plugins/docker/lang/$2.lang"
source "$1/plugins/docker/plugin.sh"
ATM_DRY_RUN=1
ATM_DOWNLOAD_DIR="$HOME/downloads with spaces"
atm_info() { printf '%s\n' "$1"; }
atm_warn() { printf '%s\n' "$1"; }
atm_success() { printf '%s\n' "$1"; }
atm_docker_desktop_release_url() { printf '%s\n' 'https://fixture.invalid/package.deb'; }
atm_docker_desktop_version() { :; }
atm_docker_desktop_installed() { return 1; }
atm_docker_require_desktop_platform() { :; }
atm_docker_write_manifest() { :; }
curl() { return 99; }
atm_run() { return 99; }
atm_docker_install_desktop 1.2.3
atm_docker_images_confirm <<< y
if atm_docker_images_confirm <<< n; then exit 90; fi
'''
        with tempfile.TemporaryDirectory(prefix="atm-docker-shell-locales-") as temp:
            for locale in LOCALES:
                with self.subTest(locale=locale):
                    messages = read_catalog(HELPER.parent / "lang" / (locale + ".lang"))
                    env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
                    env.update(HOME=temp, ATM_LANG=locale)
                    result = subprocess.run(
                        ["bash", "-c", script, "test", str(REPO), locale],
                        env=env, capture_output=True, text=True, timeout=10,
                    )
                    self.assertEqual(result.returncode, 0, result.stderr)
                    expected = messages["ATM_PLUGIN_DOCKER_DOWNLOAD_PLAN"] % (
                        "https://fixture.invalid/package.deb",
                        temp + "/downloads with spaces/docker-desktop-1.2.3-amd64.deb",
                    )
                    self.assertIn(expected, result.stdout)
                    self.assertIn(messages["ATM_PLUGIN_DOCKER_STOP_PLAN"], result.stdout)
                    self.assertIn("apt-get install -y --allow-downgrades", result.stdout)
                    for key in ("DESKTOP_CONFIRM", "IMAGE_CONFIRM"):
                        self.assertIn("[y/N]", messages["ATM_PLUGIN_DOCKER_" + key])
                    self.assertNotIn("[missing:", result.stdout)
            self.assertEqual(list(Path(temp).iterdir()), [])

    def test_helper_help_errors_and_config_contract_in_every_locale(self):
        with tempfile.TemporaryDirectory(prefix="atm-docker-locales-") as temp:
            config = Path(temp) / "config.json"
            for locale in LOCALES:
                with self.subTest(locale=locale), patch.dict(os.environ, ATM_LANG=locale):
                    module = runpy.run_path(str(HELPER))
                    messages = read_catalog(HELPER.parent / "lang" / (locale + ".lang"))
                    cases = [
                        (["--help"], "HELP_EXIT", 0),
                        ([], "HELP_ACTION_REQUIRED", 2),
                        (["check-config"], "CONFIG_PATH_REQUIRED", 2),
                        (["invalid"], "HELP_INVALID_ACTION", 2),
                        (["releases", "--invalid"], "HELP_UNKNOWN_ARGUMENTS", 2),
                    ]
                    for args, suffix, code in cases:
                        output = io.StringIO()
                        with patch.object(sys, "argv", ["desktop.py", *args]), \
                                contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                            with self.assertRaises(SystemExit) as caught:
                                module["main"]()
                        self.assertEqual(caught.exception.code, code)
                        expected = messages["ATM_PLUGIN_DOCKER_" + suffix].format(
                            value=repr("invalid"), arguments="--invalid",
                        )
                        self.assertIn(expected, output.getvalue())
                        self.assertNotIn("[missing:", output.getvalue())
                    config.write_text("[]", encoding="utf-8")
                    with self.assertRaises(ValueError) as caught:
                        module["clean_config"](config, False)
                    self.assertEqual(str(caught.exception), messages["ATM_PLUGIN_DOCKER_CONFIG_OBJECT_REQUIRED"])
                    original = {"credsStore": "desktop", "currentContext": "desktop-linux", "auths": {"fixture": {}}}
                    config.write_text(json.dumps(original), encoding="utf-8")
                    module["clean_config"](config, False)
                    self.assertEqual(json.loads(config.read_text()), {"auths": {"fixture": {}}})

    def test_release_tsv_is_identical_across_locales(self):
        url = "https://desktop.docker.com/linux/main/amd64/12345/docker-desktop-amd64.deb"
        fixture = '<h2>1.2.3</h2><a href="' + url + '">Linux</a>'
        for locale in LOCALES:
            with self.subTest(locale=locale):
                env = dict(os.environ, ATM_LANG=locale, PYTHONDONTWRITEBYTECODE="1")
                result = subprocess.run(
                    [sys.executable, "-B", str(HELPER), "releases"], input=fixture,
                    env=env, capture_output=True, text=True, timeout=10,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, "1.2.3\t" + url + "\n")
                self.assertEqual(result.stderr, "")


if __name__ == "__main__":
    unittest.main()

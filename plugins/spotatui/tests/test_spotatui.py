import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


REPO = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("spotatui_release", REPO / "plugins/spotatui/release.py")
helper = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(helper)


class DependencyTests(unittest.TestCase):
    def probe(self, *, missing=(), yt="2026.10.21", node="v22.22.3", deno="", ejs=True, initialized=True):
        def which(name):
            if name in missing or name == "deno" and not deno:
                return None
            return "/fixture/" + name

        def command(args):
            self.assertNotIn("http", " ".join(args))
            name = Path(args[0]).name
            if args[-1] == "--version":
                return SimpleNamespace(returncode=0, stdout={"yt-dlp": yt, "node": node, "deno": deno}[name] + "\n")
            self.assertIn("--ignore-config", args)
            self.assertIn("--no-cache-dir", args)
            self.assertIn("--no-plugin-dirs", args)
            selected = args[-1].split(":", 1)[0]
            return SimpleNamespace(returncode=2, stderr=("Optional libraries: yt_dlp_ejs-0.8.0\n" if ejs else "")
                                   + ("JS runtimes: " + selected + "-22.22.3\n" if initialized else "JS runtimes: none\n"))

        with patch.object(helper.shutil, "which", side_effect=which), patch.object(helper, "command", side_effect=command):
            return helper.dependencies()

    def test_current_node_and_deno_preference(self):
        self.assertEqual(self.probe()["runtime"], "node")
        self.assertEqual(self.probe(deno="deno 2.3.0")["runtime"], "deno")
        self.assertEqual(self.probe(deno="deno 2.2.0")["runtime"], "node")

    def test_missing_tools_old_extractor_runtime_and_ejs_fail(self):
        for name in ("curl", "ffmpeg", "ffprobe", "yt-dlp"):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "Missing executable"):
                self.probe(missing=(name,))
        for value in ("2024.04.09", "bad", "2025.10.01"):
            with self.subTest(yt=value), self.assertRaisesRegex(ValueError, "too old"):
                self.probe(yt=value)
        with self.assertRaisesRegex(ValueError, "runtime"):
            self.probe(node="v18.19.1")
        with self.assertRaisesRegex(ValueError, "EJS"):
            self.probe(ejs=False)
        with self.assertRaisesRegex(ValueError, "initialize"):
            self.probe(initialized=False)


class ReleaseTests(unittest.TestCase):
    def release(self, platform="linux-x86_64"):
        name = "spotatui-" + platform + ".tar.gz"
        return dict(tag_name="v0.46.0", draft=False, prerelease=False,
                    assets=[dict(name=n, browser_download_url=helper.BASE + "v0.46.0/" + n)
                            for n in (name, name + ".sha256")])

    def test_resolve_supported_platforms_and_refuse_foreign_missing_duplicate_assets(self):
        for platform in ("linux-x86_64", "linux-aarch64"):
            self.assertTrue(helper.resolve(self.release(platform), platform).startswith("0.46.0\thttps://github.com/"))
        base = self.release()
        invalid = []
        for key, value in (("draft", True), ("prerelease", True), ("tag_name", "v0.46.0-rc.1"), ("assets", [])):
            data = copy.deepcopy(base)
            data[key] = value
            invalid.append(data)
        data = copy.deepcopy(base)
        data["assets"][0]["browser_download_url"] = "https://example.com/payload"
        invalid.append(data)
        data = copy.deepcopy(base)
        data["assets"].append(data["assets"][0])
        invalid.append(data)
        for data in invalid:
            with self.subTest(data=data), self.assertRaises(ValueError):
                helper.resolve(data, "linux-x86_64")
        with self.assertRaises(ValueError):
            helper.resolve(base, "windows-x86_64")


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="atm-spotatui-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / "spotatui-linux-x86_64.tar.gz"
        self.checksum = self.root / (self.archive.name + ".sha256")
        self.destination = self.root / "apps/0.46.0"
        self.tools = self.root / "tools with spaces ' $literal"
        self.tools.mkdir()
        self.deps = dict(ytdlp=str(self.tools / "yt-dlp"), runtime="node",
                         runtime_path=str(self.tools / "node"), ffmpeg=str(self.tools / "ffmpeg"))

    def package(self, names=("spotatui",)):
        with tarfile.open(self.archive, "w:gz") as package:
            for name in names:
                member = tarfile.TarInfo(name)
                if name == "link":
                    member.type = tarfile.SYMTYPE
                    member.linkname = "spotatui"
                    package.addfile(member)
                else:
                    data = b'#!/usr/bin/env bash\nprintf "ffmpeg=%s\\n" "$(command -v ffmpeg)"\nexec yt-dlp "$@"\n'
                    member.size = len(data)
                    package.addfile(member, io.BytesIO(data))
        self.checksum.write_text(hashlib.sha256(self.archive.read_bytes()).hexdigest() + "  " + self.archive.name + "\n")

    def test_checked_install_and_scoped_launcher_preserve_arguments_and_external_files(self):
        self.package()
        for name, script in (("yt-dlp", '#!/usr/bin/env bash\nprintf "<%s>\\n" "$@"\n'),
                             ("node", "#!/usr/bin/env bash\nexit 0\n"),
                             ("ffmpeg", "#!/usr/bin/env bash\nexit 0\n")):
            path = self.tools / name
            path.write_text(script)
            path.chmod(0o755)
        helper.install(self.archive, self.checksum, self.destination, self.deps)
        self.assertTrue((self.destination / ".atm-spotatui-managed").is_file())
        result = subprocess.run([str(self.destination / "bin/spotatui"), "--example", "value with spaces"],
                                capture_output=True, text=True, check=True)
        self.assertIn("ffmpeg=" + self.deps["ffmpeg"], result.stdout)
        self.assertIn("<--js-runtimes>\n<node:" + self.deps["runtime_path"] + ">", result.stdout)
        self.assertIn("<--example>\n<value with spaces>", result.stdout)
        with self.assertRaisesRegex(ValueError, "overwritten"):
            helper.install(self.archive, self.checksum, self.destination, self.deps)

    def test_reject_bad_hash_paths_links_duplicates_and_missing_binary(self):
        for names in (("../escape", "spotatui"), ("/absolute", "spotatui"), ("link", "spotatui"),
                      ("spotatui", "spotatui"), ("readme",)):
            with self.subTest(names=names):
                self.package(names)
                with self.assertRaises(ValueError):
                    helper.install(self.archive, self.checksum, self.destination, self.deps)
                self.assertFalse(self.destination.exists())
        self.package()
        self.checksum.write_text("0" * 64 + "\n")
        with self.assertRaisesRegex(ValueError, "mismatch"):
            helper.install(self.archive, self.checksum, self.destination, self.deps)
        self.assertFalse(self.destination.parent.exists())

    def test_failed_publish_cleans_stage_and_destination_symlink_is_preserved(self):
        self.package()
        with patch.object(Path, "rename", side_effect=OSError("fixture rename failure")), self.assertRaises(OSError):
            helper.install(self.archive, self.checksum, self.destination, self.deps)
        self.assertEqual(list(self.destination.parent.iterdir()), [])
        self.destination.symlink_to(self.tools)
        with self.assertRaises(ValueError):
            helper.install(self.archive, self.checksum, self.destination, self.deps)
        self.assertEqual(self.destination.readlink(), self.tools)

    def test_existing_launcher_refresh_and_failed_replace_preserve_original(self):
        self.package()
        helper.install(self.archive, self.checksum, self.destination, self.deps)
        new = dict(self.deps, runtime="deno", runtime_path="/new/deno")
        helper.configure(self.destination, new)
        shim = self.destination / "shims/yt-dlp"
        self.assertIn("deno:/new/deno", shim.read_text())
        before = shim.read_bytes()
        with patch.object(Path, "replace", side_effect=OSError("fixture failure")), self.assertRaises(OSError):
            helper.configure(self.destination, self.deps)
        self.assertEqual(shim.read_bytes(), before)
        self.assertEqual(list(self.destination.rglob(".atm-launcher-*")), [])
        shim.unlink()
        shim.symlink_to(self.checksum)
        with self.assertRaises(ValueError):
            helper.configure(self.destination, self.deps)

    def isolated_env(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
        env.update(HOME=str(self.root / "home"), ATM_HOME=str(REPO),
                   ATM_APPS_DIR=str(self.root / "Apps"), ATM_CACHE_DIR=str(self.root / "cache"),
                   ATM_CONFIG_DIR=str(self.root / "config"), ATM_STATE_DIR=str(self.root / "state"),
                   ATM_LANG="en-us", TERM="dumb", FIXTURE=str(self.root))
        Path(env["HOME"]).mkdir(exist_ok=True)
        return env

    def test_shell_lifecycle_preflight_failures_and_menu(self):
        self.package()
        (self.root / "deps.json").write_text(json.dumps(self.deps))
        script = r'''
set -Eeuo pipefail
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_manifest.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/lang/en-us.lang"
source "$ATM_HOME/plugins/spotatui/plugin.conf"
source "$ATM_HOME/plugins/spotatui/plugin.sh"
source "$ATM_HOME/plugins/spotatui/lang/en-us.lang"
clear() { :; }
atm_spotatui_dependency_info() { cat "$FIXTURE/deps.json"; }
atm_spotatui_release() {
    printf 'release\n' >> "$FIXTURE/calls"
    local version="${1/latest/0.46.0}"
    printf '%s\thttps://fixture/spotatui-linux-x86_64.tar.gz\thttps://fixture/spotatui-linux-x86_64.tar.gz.sha256\n' "$version"
}
atm_download_file() { mkdir -p "${2%/*}"; cp "$FIXTURE/${1##*/}" "$2"; }
[[ "$(atm_spotatui_status)" == 'Not installed by ATM' ]]
if atm_spotatui_install --version '../escape'; then exit 1; fi
if (atm_spotatui_dependency_info() { return 1; }; atm_spotatui_install); then exit 1; fi
[[ ! -e "$FIXTURE/calls" && ! -e "$ATM_SPOTATUI_INSTALL_ROOT" ]]
if (atm_spotatui_release() { return 1; }; atm_spotatui_install); then exit 1; fi
[[ ! -e "$ATM_SPOTATUI_INSTALL_ROOT" ]]
mkdir -p "$HOME/.local/bin" "$HOME/.config/spotatui"
printf 'external binary\n' > "$HOME/.local/bin/spotatui"
printf 'keep settings\n' > "$HOME/.config/spotatui/config.yml"
atm_spotatui_install --version=0.46.0
[[ "$(atm_spotatui_current_version)" == 0.46.0 ]]
[[ "$(atm_spotatui_path_entries)" == "$ATM_SPOTATUI_INSTALL_ROOT/current/bin" ]]
grep -q 'ATM_PLUGIN_VERSION="0.0.1"' "$ATM_SPOTATUI_MANIFEST_FILE"
if atm_spotatui_remove 0.46.0; then exit 1; fi
atm_spotatui_install --version v0.45.0
atm_spotatui_use 0.46.0
atm_spotatui_remove 0.45.0
[[ ! -e "$ATM_SPOTATUI_INSTALL_ROOT/0.45.0" ]]
mkdir -p "$ATM_SPOTATUI_INSTALL_ROOT/0.1.0"
printf 'unmanaged\n' > "$ATM_SPOTATUI_INSTALL_ROOT/0.1.0/user-file"
atm_spotatui_uninstall <<< n
[[ "$(atm_spotatui_current_version)" == 0.46.0 ]]
output="$(ATM_DRY_RUN=1 atm_spotatui_menu <<< $'3\nxb')"
[[ "$output" == *'Check playback dependencies'* && "$output" == *'DRY-RUN:'* ]]
atm_spotatui_uninstall <<< y
[[ ! -e "$ATM_SPOTATUI_MANIFEST_FILE" && ! -e "$ATM_SPOTATUI_INSTALL_ROOT/current" ]]
[[ -f "$ATM_SPOTATUI_INSTALL_ROOT/0.1.0/user-file" ]]
[[ "$(<"$HOME/.local/bin/spotatui")" == 'external binary' ]]
[[ "$(<"$HOME/.config/spotatui/config.yml")" == 'keep settings' ]]
'''
        result = subprocess.run(["bash", "-c", script], env=self.isolated_env(), cwd=REPO,
                                text=True, capture_output=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_actual_cli_dry_run_is_network_version_command_and_write_free(self):
        env = self.isolated_env()
        tools = self.root / "forbidden-tools"
        tools.mkdir()
        for name in ("curl", "python3", "yt-dlp", "node", "deno", "ffmpeg", "ffprobe", "spotatui", "sudo", "pipx"):
            path = tools / name
            path.write_text("#!/bin/sh\nprintf 'UNEXPECTED_TOOL_CALL\\n' >&2\nexit 99\n")
            path.chmod(0o755)
        env["PATH"] = str(tools) + os.pathsep + os.environ.get("PATH", os.defpath)
        before = {str(p.relative_to(self.root)): p.read_bytes() if p.is_file() else None
                  for p in self.root.rglob("*")}
        for version, locale in (("latest", "en-us"), ("0.46.0", "de")):
            env["ATM_LANG"] = locale
            result = subprocess.run(["bash", str(REPO / "bin/atm"), "--dry-run", "install", "spotatui",
                                     "--version", version], env=env, cwd=REPO, capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("DRY-RUN:", result.stdout)
            self.assertNotIn("UNEXPECTED_TOOL_CALL", result.stdout + result.stderr)
            self.assertNotIn("[missing:", result.stdout + result.stderr)
        after = {str(p.relative_to(self.root)): p.read_bytes() if p.is_file() else None
                 for p in self.root.rglob("*")}
        self.assertEqual(before, after)


if __name__ == "__main__":
    unittest.main()

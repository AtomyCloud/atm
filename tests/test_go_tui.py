"""Linux PTY checks against a built Go binary; never install real applications."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
BINARY = Path(sys.argv.pop(1)).resolve() if __name__ == "__main__" and len(sys.argv) > 1 else None
NATIVE = Path(os.environ["ATM_TEST_NATIVE_FIXTURE"]).resolve() if os.environ.get("ATM_TEST_NATIVE_FIXTURE") else None


class Terminal:
    def __init__(self, home, config, width=80, height=24, extra=()):
        self.master, self.slave = pty.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        self.before = termios.tcgetattr(self.slave)
        env = os.environ.copy()
        for key in list(env):
            if key.startswith("ATM_") or key in ("NO_COLOR", "BASH_ENV", "ENV"):
                env.pop(key)
        env.update(TERM="xterm-256color", COLORTERM="truecolor", ATM_CONFIG_DIR=str(config))
        self.proc = subprocess.Popen(
            [str(BINARY), "--home", str(home), *extra], stdin=self.slave,
            stdout=self.slave, stderr=self.slave, env=env, start_new_session=True,
        )
        self.output = b""

    def send(self, value):
        os.write(self.master, value)

    def wait_for(self, value, timeout=8):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            ready, _, _ = select.select([self.master], [], [], 0.1)
            if ready:
                try:
                    self.output += os.read(self.master, 65536)
                except OSError:
                    break
            if value in self.output:
                self.output = b""
                return
            if self.proc.poll() is not None:
                break
        raise AssertionError(f"missing {value!r}: {self.output[-2000:]!r}")

    def close(self):
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait(timeout=5)
        os.close(self.master)
        os.close(self.slave)


@unittest.skipUnless(sys.platform.startswith("linux") and BINARY, "Linux PTY and binary required")
class TUITests(unittest.TestCase):
    def native_home(self, root):
        home = root / "Native ATM with spaces"
        plugin = home / "plugins" / "native_fixture"
        (plugin / "bin").mkdir(parents=True)
        shutil.copy(ROOT / "tests/fixtures/native_fixture/plugin.json", plugin / "plugin.json")
        shutil.copy(NATIVE, plugin / "bin/fixture")
        shutil.copy(ROOT / "core-api.json", home / "core-api.json")
        shutil.copytree(ROOT / "lang", home / "lang")
        return home

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_cli_stream_confirmation_and_dryrun(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = self.native_home(root)
            env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
            env.update(ATM_CONFIG_DIR=str(root / "config"), ATM_APPS_DIR=str(root / "Apps"))
            def run(*args):
                return subprocess.run([str(BINARY), "--home", str(home), *args],
                                      env=env, cwd=root, capture_output=True, text=True, timeout=10)
            refused = run("--json", "install", "native_fixture")
            self.assertNotEqual(refused.returncode, 0)
            self.assertIn("--yes", refused.stderr)
            dry = run("--dry-run", "--json", "install", "native_fixture")
            self.assertEqual(dry.returncode, 0, dry.stderr)
            self.assertTrue(json.loads(dry.stdout)["dry_run"])
            result = run("--yes", "--json", "install", "native_fixture", "echo", "$(touch sentinel)")
            self.assertEqual(result.returncode, 0, result.stderr)
            events = [json.loads(line) for line in result.stdout.splitlines()]
            self.assertEqual(events[0]["type"], "started")
            self.assertEqual(events[-1]["status"], "success")
            self.assertEqual(len({event["request_id"] for event in events}), 1)
            self.assertFalse((root / "sentinel").exists())
            timeout = run("--yes", "--json", "--timeout", "200ms", "update", "native_fixture")
            self.assertEqual(timeout.returncode, 124, timeout.stderr)
            self.assertEqual(json.loads(timeout.stdout.splitlines()[-1])["code"], "timeout")
            self.assertFalse((root / "config").exists())
            self.assertFalse((root / "Apps").exists())

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_progress_cancel_and_terminal_restore(self):
        for width, height in ((80, 24), (40, 14), (120, 35)):
            with self.subTest(size=(width, height)), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                home = self.native_home(root)
                terminal = Terminal(home, root / "config", width, height, ("--no-animation",))
                try:
                    terminal.wait_for(b"ATM 0.0.2")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Inspect installation")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b"Step")
                    terminal.wait_for(b"success: Fixture completed")
                    terminal.send(b"\x1b")
                    terminal.wait_for(b"Search")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Update application")
                    terminal.send(b"\x1b[B\x1b[B\r")
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b"Fixture started")
                    if width == 80:
                        terminal.send(b"\x1b[<0;2;7M\x1b[<0;2;7m")
                        terminal.wait_for(b"cancelled:")
                        terminal.send(b"q")
                    elif width == 120:
                        terminal.send(b"\x03")
                    else:
                        terminal.send(b"q")
                    self.assertEqual(terminal.proc.wait(timeout=5), 0)
                    self.assertEqual(termios.tcgetattr(terminal.slave), terminal.before)
                finally:
                    terminal.close()

    def test_real_bridge_uses_isolated_legacy_runtime(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = root / "atm"
            home.mkdir()
            for directory in ("lib", "lang", "libexec"):
                shutil.copytree(ROOT / directory, home / directory)
            shutil.copy(ROOT / "core-api.json", home / "core-api.json")
            plugin = home / "plugins" / "fixture"
            plugin.mkdir(parents=True)
            (plugin / "plugin.metadata").write_text(
                'ATM_PLUGIN_ID="fixture"\nATM_PLUGIN_NAME_VALUE="Fixture"\n'
                'ATM_PLUGIN_VERSION_VALUE="0.0.1"\nATM_PLUGIN_ENTRYPOINT="plugin.sh"\n'
                'ATM_PLUGIN_INSTALL_FUNC_VALUE="atm_fixture_install"\n'
            )
            (plugin / "plugin.sh").write_text(
                'atm_fixture_install() { printf "ARG=<%s>\\n" "$@"; return 17; }\n'
            )
            env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
            env.update(HOME=str(root / "user"), XDG_CONFIG_HOME=str(root / "config"),
                       ATM_CONFIG_DIR=str(root / "config"), ATM_LANG="en-us")
            result = subprocess.run(
                [str(BINARY), "--home", str(home), "install", "fixture", "a; touch sentinel"],
                env=env, cwd=root, text=True, capture_output=True, timeout=15,
            )
            self.assertEqual(result.returncode, 17, result.stderr)
            self.assertIn("ARG=<a; touch sentinel>", result.stdout)
            self.assertFalse((root / "sentinel").exists())

    def test_keyboard_mouse_and_terminal_restore(self):
        for width, height in ((80, 24), (40, 14), (120, 35)):
            with self.subTest(size=(width, height)), tempfile.TemporaryDirectory() as temp:
                terminal = Terminal(ROOT, Path(temp) / "config", width, height, ("--dry-run", "--no-animation"))
                try:
                    terminal.wait_for(b"ATM 0.0.2")
                    terminal.send(b"/node\r")
                    terminal.wait_for(b"Node.js")
                    terminal.send(b"\x1b[<0;2;7M\x1b[<0;2;7m")
                    terminal.wait_for(b"Open plugin menu")
                    terminal.send(b"q")
                    self.assertEqual(terminal.proc.wait(timeout=5), 0)
                    self.assertEqual(termios.tcgetattr(terminal.slave), terminal.before)
                    self.assertFalse((Path(temp) / "config").exists())
                finally:
                    terminal.close()

    def test_native_catalog_dry_run_and_terminal_restore(self):
        with tempfile.TemporaryDirectory() as temp:
            terminal = Terminal(ROOT, Path(temp) / "config", 40, 14,
                                ("--dry-run", "--no-animation"))
            try:
                terminal.wait_for(b"ATM 0.0.2")
                terminal.send(b"u")
                terminal.wait_for(b"Official plugin catalog")
                terminal.send(b"\x1b[B\x1b[B\x1b[B\r")
                terminal.wait_for(b"plugins search")
                terminal.send(b"q")
                self.assertEqual(terminal.proc.wait(timeout=5), 0)
                self.assertEqual(termios.tcgetattr(terminal.slave), terminal.before)
                self.assertFalse((Path(temp) / "config").exists())
            finally:
                terminal.close()

    def test_interactive_handoff_and_nonzero_exit(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = root / "ATM with spaces"
            (home / "plugins" / "fake").mkdir(parents=True)
            (home / "libexec").mkdir()
            shutil.copytree(ROOT / "lang", home / "lang")
            (home / "core-api.json").write_text('{"schema":1}')
            (home / "plugins" / "fake" / "plugin.json").write_text(json.dumps({
                "schema": 1, "id": "fake", "name": "Fake", "version": "0.0.1",
                "description": "Test fixture", "order": 1, "min_core_api": 1,
                "platforms": ["linux"], "executor": "legacy-bash", "protocol": "tty-v1",
                "operations": ["menu"],
            }))
            (home / "libexec" / "atm-legacy.sh").write_text(
                "printf 'FAKE_INPUT: '\nread -r value\nprintf 'VALUE=%s\\n' \"$value\"\nexit 7\n"
            )
            terminal = Terminal(home, root / "config", extra=("--no-animation",))
            try:
                terminal.wait_for(b"ATM 0.0.2")
                terminal.send(b"\r")
                terminal.wait_for(b"Open plugin menu")
                terminal.send(b"\r")
                terminal.wait_for(b"Run this operation?")
                terminal.send(b"\x1b[B\r")
                terminal.wait_for(b"FAKE_INPUT:")
                terminal.send(b"hello world\r")
                terminal.wait_for(b"Press Enter to return to ATM")
                terminal.send(b"\r")
                terminal.wait_for(b"exit status 7")
                terminal.send(b"q")
                self.assertEqual(terminal.proc.wait(timeout=5), 0)
                self.assertEqual(termios.tcgetattr(terminal.slave), terminal.before)
            finally:
                terminal.close()


if __name__ == "__main__":
    unittest.main()

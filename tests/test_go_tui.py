"""Linux PTY checks against a built Go binary; never install real applications."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import select
import shutil
import socket
import socketserver
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
BINARY = Path(sys.argv.pop(1)).resolve() if __name__ == "__main__" and len(sys.argv) > 1 else None
NATIVE = Path(os.environ["ATM_TEST_NATIVE_FIXTURE"]).resolve() if os.environ.get("ATM_TEST_NATIVE_FIXTURE") else None
STRACE = shutil.which("strace")


def isolated_env(root):
    env = os.environ.copy()
    for key in list(env):
        if (key.startswith(("ATM_", "XDG_")) or key.lower().endswith("_proxy") or
                key in ("NO_COLOR", "BASH_ENV", "ENV", "GITHUB_TOKEN", "GH_TOKEN", "SSH_AUTH_SOCK")):
            env.pop(key)
    for name in ("user", "tmp", "runtime"):
        (root / name).mkdir(mode=0o700, exist_ok=True)
    env.update(HOME=str(root / "user"), TMPDIR=str(root / "tmp"),
               XDG_CONFIG_HOME=str(root / "xdg-config"), XDG_DATA_HOME=str(root / "xdg-data"),
               XDG_STATE_HOME=str(root / "xdg-state"), XDG_CACHE_HOME=str(root / "xdg-cache"),
               XDG_RUNTIME_DIR=str(root / "runtime"), ATM_CONFIG_DIR=str(root / "config"),
               ATM_STATE_DIR=str(root / "state"), ATM_APPS_DIR=str(root / "user/Apps"), ATM_LANG="en-us",
               HTTP_PROXY="http://127.0.0.1:9", HTTPS_PROXY="http://127.0.0.1:9", NO_PROXY="")
    return env


def snapshot(root):
    result = {}
    for path in sorted(root.rglob("*")):
        info = path.lstat()
        digest = hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None
        result[str(path.relative_to(root))] = (info.st_mode, info.st_mtime_ns, digest)
    return result


class StalledProxy(socketserver.ThreadingTCPServer):
    """Hold a local CONNECT request until the TUI cancels; never forward traffic."""
    daemon_threads = True

    def __init__(self):
        self.started = threading.Event()
        self.disconnected = threading.Event()
        self.stopping = threading.Event()

        class Handler(socketserver.BaseRequestHandler):
            def handle(handler):
                handler.request.settimeout(0.1)
                while not self.stopping.is_set():
                    try:
                        data = handler.request.recv(4096)
                    except socket.timeout:
                        continue
                    except ConnectionError:
                        data = b""
                    if not data:
                        self.disconnected.set()
                        return
                    self.started.set()

        super().__init__(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.serve_forever, kwargs={"poll_interval": 0.05})
        self.thread.start()

    @property
    def url(self):
        return f"http://127.0.0.1:{self.server_address[1]}"

    def __exit__(self, *args):
        self.stopping.set()
        self.shutdown()
        self.thread.join(timeout=5)
        super().__exit__(*args)


class Terminal:
    def __init__(self, home, config, width=80, height=24, extra=(), environment=None, trace=None):
        self.master, self.slave = pty.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        self.before = termios.tcgetattr(self.slave)
        env = isolated_env(config.parent) if environment is None else environment.copy()
        env.update(TERM="xterm-256color", COLORTERM="truecolor", ATM_CONFIG_DIR=str(config))
        command = [str(BINARY), "--home", str(home), *extra]
        if trace is not None:
            command = [STRACE, "-f", "-qq", "-e", "trace=execve,execveat,connect", "-o", str(trace), *command]
        self.proc = subprocess.Popen(
            command, stdin=self.slave, stdout=self.slave, stderr=self.slave,
            env=env, cwd=config.parent, start_new_session=True,
        )
        self.output = b""
        self.transcript = b""

    def send(self, value):
        os.write(self.master, value)

    def wait_for(self, value, timeout=8):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if value in self.output:
                self.output = self.output.split(value, 1)[1]
                return
            ready, _, _ = select.select([self.master], [], [], 0.1)
            if ready:
                try:
                    data = os.read(self.master, 65536)
                    self.output += data
                    self.transcript += data
                except OSError:
                    break
            elif self.proc.poll() is not None:
                break
        raise AssertionError(f"missing {value!r}: {self.output[-2000:]!r}")

    def close(self):
        if self.proc.poll() is None:
            os.killpg(self.proc.pid, 9)
        self.proc.wait(timeout=5)
        os.close(self.master)
        os.close(self.slave)


@unittest.skipUnless(sys.platform.startswith("linux") and BINARY, "Linux PTY and binary required")
class TUITests(unittest.TestCase):
    def core_home(self, root):
        root.mkdir(mode=0o700, parents=True, exist_ok=True)
        home = root / "CORE ATM with spaces \u00e7"
        home.mkdir(mode=0o700)
        (home / "plugins").mkdir()
        (home / "libexec").mkdir()
        shutil.copy(ROOT / "core-api.json", home / "core-api.json")
        shutil.copytree(ROOT / "lang", home / "lang")
        (home / "libexec/atm-legacy.sh").write_text('printf unexpected > "$HOME/shell-executed"\nexit 99\n')
        return home

    def open_core_row(self, terminal, index):
        # Navigation resets the cursor even if CORE is already the current view.
        terminal.send(b"u" + b"\x1b[B" * index + b"\r")

    def assert_terminal_restored(self, terminal):
        terminal.wait_for(b"\x1b[?1049l")
        self.assertEqual(terminal.proc.wait(timeout=5), 0)
        self.assertEqual(termios.tcgetattr(terminal.slave), terminal.before)
        self.assertIn(b"\x1b[?25h", terminal.transcript)
        self.assertIn(b"\x1b[?1006l", terminal.transcript)

    def assert_no_execution_or_network(self, trace):
        calls = trace.read_text()
        executions = [line for line in calls.splitlines() if "execve(" in line or "execveat(" in line]
        self.assertEqual(len(executions), 1, calls)
        self.assertIn(str(BINARY), executions[0])
        self.assertNotIn("connect(", calls)

    @unittest.skipUnless(STRACE, "strace required for subprocess/network assertions")
    def test_native_core_dry_run_menu_routes(self):
        cases = ((1, None), (2, None), (5, None), (6, None), (7, "fixture"),
                 (8, None), (9, None), (10, "backup-123"), (11, None),
                 (12, "a" * 64), (13, None), (14, None), (15, None))
        for number, (index, value) in enumerate(cases):
            size = ((80, 24), (40, 14), (120, 35))[number % 3]
            with self.subTest(row=index, size=size), tempfile.TemporaryDirectory() as temp:
                root = Path(temp) / "data"
                home = self.core_home(root)
                env = isolated_env(root)
                env["PATH"] = "/nonexistent"
                before = snapshot(root)
                trace = Path(temp) / "syscalls"
                terminal = Terminal(home, root / "config", *size, ("--dry-run", "--no-animation"),
                                    environment=env, trace=trace)
                try:
                    terminal.wait_for(b"ATM 0.0.2")
                    self.open_core_row(terminal, index)
                    if value is not None:
                        terminal.wait_for(b"Plugin ID / backup ID")
                        terminal.send(value.encode() + b"\r")
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b"Dry-run: no execution or writes")
                    terminal.send(b"q")
                    self.assert_terminal_restored(terminal)
                finally:
                    terminal.close()
                self.assertEqual(snapshot(root), before)
                self.assert_no_execution_or_network(trace)

    @unittest.skipUnless(STRACE, "strace required for subprocess/network assertions")
    def test_native_core_readonly_refusal_and_confirmation(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "data"
            home = self.core_home(root)
            env = isolated_env(root)
            env["PATH"] = "/nonexistent"
            before = snapshot(root)
            trace = Path(temp) / "syscalls"
            terminal = Terminal(home, root / "config", 120, 35, ("--no-animation",),
                                environment=env, trace=trace)
            try:
                terminal.wait_for(b"ATM 0.0.2")
                for index, result in ((0, b"Compiled CORE updater:"), (9, b'"operation": "updates backups"'),
                                      (14, b"SHA256:jNhv8Ehni4ENGvQaar4/l8vQQPHcVZDnCmcmTvkFXVM")):
                    self.open_core_row(terminal, index)
                    terminal.wait_for(result)
                self.open_core_row(terminal, 13)
                terminal.wait_for(b"signed installed baseline required")
                for cancel in (b"\r", b"\x1b", b"\x1b[<0;2;7M\x1b[<0;2;7m"):
                    self.open_core_row(terminal, 6)
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(cancel)
                    terminal.wait_for(b"No plugins found")
                self.open_core_row(terminal, 6)
                terminal.wait_for(b"Run this operation?")
                terminal.send(b"\x1b[B\r")
                terminal.wait_for(b"failed:")
                terminal.send(b"h")
                terminal.wait_for(b"path apply: Operation failed")
                terminal.send(b"q")
                self.assert_terminal_restored(terminal)
            finally:
                terminal.close()
            self.assertEqual(snapshot(root), before)
            self.assert_no_execution_or_network(trace)

    def test_native_core_invalid_input_can_be_corrected(self):
        for index, valid in ((7, "fixture"), (10, "backup-123"), (12, "a" * 64)):
            with self.subTest(row=index), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                home = self.core_home(root)
                env = isolated_env(root)
                env["PATH"] = "/nonexistent"
                before = snapshot(root)
                terminal = Terminal(home, root / "config", 120, 35, ("--dry-run", "--no-animation"), environment=env)
                try:
                    terminal.wait_for(b"ATM 0.0.2")
                    self.open_core_row(terminal, index)
                    terminal.wait_for(b"Plugin ID / backup ID")
                    terminal.send(b"/\r")
                    terminal.wait_for(b"unsupported native CORE operation")
                    terminal.send(b"\x7f" + valid.encode() + b"\r")
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\r")
                    terminal.wait_for(b"No plugins found")
                    terminal.send(b"q")
                    self.assert_terminal_restored(terminal)
                finally:
                    terminal.close()
                self.assertEqual(snapshot(root), before)

    def test_native_core_pending_cancel_timeout_and_terminal_restore(self):
        for action in ("escape", "mouse", "quit", "interrupt", "timeout"):
            with self.subTest(action=action), tempfile.TemporaryDirectory() as temp, StalledProxy() as proxy:
                root = Path(temp)
                home = self.core_home(root)
                env = isolated_env(root)
                env.update(PATH="/nonexistent", HTTP_PROXY=proxy.url, HTTPS_PROXY=proxy.url)
                before = snapshot(root)
                extra = ("--no-animation", "--timeout", "750ms" if action == "timeout" else "10s")
                terminal = Terminal(home, root / "config", 120, 35, extra, environment=env)
                try:
                    terminal.wait_for(b"ATM 0.0.2")
                    self.open_core_row(terminal, 1)
                    self.assertTrue(proxy.started.wait(5), "native CORE check never reached local proxy")
                    terminal.wait_for(b"Operation running")
                    if action == "timeout":
                        terminal.wait_for(b"context deadline exceeded")
                        terminal.send(b"q")
                    else:
                        terminal.send({"escape": b"\x1b", "mouse": b"\x1b[<0;2;7M\x1b[<0;2;7m",
                                       "quit": b"q", "interrupt": b"\x03"}[action])
                        if action in ("escape", "mouse"):
                            terminal.wait_for(b"cancelled")
                            terminal.send(b"q")
                    self.assert_terminal_restored(terminal)
                    self.assertTrue(proxy.disconnected.wait(5), "CORE left its request running after exit")
                finally:
                    terminal.close()
                self.assertEqual(snapshot(root), before)

    def native_home(self, root):
        home = root / "Native ATM with spaces"
        home.mkdir(mode=0o700)
        plugin = home / "plugins" / "native_fixture"
        (plugin / "bin").mkdir(parents=True)
        shutil.copy(ROOT / "tests/fixtures/native_fixture/plugin.json", plugin / "plugin.json")
        shutil.copy(NATIVE, plugin / "bin/fixture")
        shutil.copy(ROOT / "core-api.json", home / "core-api.json")
        shutil.copytree(ROOT / "lang", home / "lang")
        return home

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_core_full_setup_confirmation_and_result(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = self.native_home(root)
            manifest_path = home / "plugins/native_fixture/plugin.json"
            manifest = json.loads(manifest_path.read_text())
            manifest["full_setup"] = True
            manifest["operations"].append("integrate")
            manifest_path.write_text(json.dumps(manifest))
            env = isolated_env(root)
            env["PATH"] = "/nonexistent"
            before = snapshot(root)
            terminal = Terminal(home, root / "config", 120, 35, ("--no-animation",), environment=env)
            try:
                terminal.wait_for(b"ATM 0.0.2")
                for index, operation in ((5, b"install"), (15, b"path apply --plugins")):
                    self.open_core_row(terminal, index)
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Native fixture")
                    self.open_core_row(terminal, index)
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b'"status": "success"')
                    terminal.send(b"h")
                    terminal.wait_for(operation + b": Operation finished")
                terminal.send(b"q")
                self.assert_terminal_restored(terminal)
            finally:
                terminal.close()
            self.assertEqual(snapshot(root), before)

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_full_setup_without_shell(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = self.native_home(root)
            manifest_path = home / "plugins/native_fixture/plugin.json"
            manifest = json.loads(manifest_path.read_text())
            manifest["full_setup"] = True
            manifest_path.write_text(json.dumps(manifest))
            env = isolated_env(root)
            env["PATH"] = "/nonexistent"
            result = subprocess.run([str(BINARY), "--home", str(home), "--yes", "--json", "install"],
                                    env=env, cwd=root, capture_output=True, text=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)["items"], [{"id": "native_fixture", "status": "success"}])
            self.assertFalse(Path(env["ATM_APPS_DIR"]).exists())
            self.assertFalse((root / "state").exists())

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_cli_stream_confirmation_and_dryrun(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = self.native_home(root)
            env = isolated_env(root)
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
            self.assertFalse(Path(env["ATM_APPS_DIR"]).exists())

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_menu_choices_custom_version_and_cancel(self):
        for width, height in ((80, 24), (40, 14), (120, 35)):
            with self.subTest(size=(width, height)), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                home = self.native_home(root)
                path = home / "plugins/native_fixture/plugin.json"
                manifest = json.loads(path.read_text())
                manifest["operations"].insert(0, "menu")
                path.write_text(json.dumps(manifest))
                env = isolated_env(root)
                env["PATH"] = "/nonexistent"
                before = snapshot(root)
                terminal = Terminal(home, root / "config", width, height,
                                    ("--no-animation", "plugin", "native_fixture", "menu"), env)
                try:
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b"Choose a version")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Version:")
                    terminal.send(b"26.2.0\r")
                    terminal.wait_for(b"Run this operation?")
                    # Default Cancel must not execute the selected installation.
                    terminal.send(b"\r")
                    terminal.wait_for(b"Search")
                    terminal.send(b"q")
                    self.assert_terminal_restored(terminal)
                    self.assertEqual(snapshot(root), before)
                finally:
                    terminal.close()

    @unittest.skipUnless(NATIVE, "ATM_TEST_NATIVE_FIXTURE required")
    def test_native_full_output_review_and_return(self):
        for width, height in ((40, 14), (80, 24), (120, 35)):
            with self.subTest(size=(width, height)), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                home = self.native_home(root)
                path = home / "plugins/native_fixture/plugin.json"
                manifest = json.loads(path.read_text())
                manifest["operations"].insert(0, "menu")
                path.write_text(json.dumps(manifest))
                env = isolated_env(root)
                env["PATH"] = "/nonexistent"
                before = snapshot(root)
                terminal = Terminal(home, root / "config", width, height,
                                    ("--no-animation", "plugin", "native_fixture", "menu", "review"), env)
                try:
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b"View full output")
                    terminal.send(b"\x1b[B\r")
                    terminal.wait_for(b"Operation output")
                    terminal.wait_for(b"REVIEW-BEGIN")
                    terminal.send(b"\x1b[F")
                    terminal.wait_for(b"REVIEW-END")
                    terminal.send(b"\x1b")
                    terminal.wait_for(b"Custom version")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Version:")
                    terminal.send(b"26.2.0\r")
                    terminal.wait_for(b"Run this operation?")
                    terminal.send(b"\r")
                    terminal.wait_for(b"Search")
                    terminal.send(b"q")
                    self.assert_terminal_restored(terminal)
                    self.assertEqual(snapshot(root), before)
                finally:
                    terminal.close()

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
            env = isolated_env(root)
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
                home = self.core_home(Path(temp))
                plugin = home / "plugins/fixture"
                plugin.mkdir()
                (plugin / "plugin.json").write_text(json.dumps({
                    "schema": 1, "id": "fixture", "name": "Fixture", "version": "0.0.1",
                    "description": "Legacy navigation fixture", "order": 1, "min_core_api": 1,
                    "platforms": ["linux"], "executor": "legacy-bash", "protocol": "tty-v1",
                    "operations": ["menu"],
                }))
                terminal = Terminal(home, Path(temp) / "config", width, height, ("--dry-run", "--no-animation"))
                try:
                    terminal.wait_for(b"ATM 0.0.2")
                    terminal.send(b"/fixture\r")
                    terminal.wait_for(b"Fixture")
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

    def test_native_download_dry_run_and_terminal_restore(self):
        with tempfile.TemporaryDirectory() as temp:
            terminal = Terminal(ROOT, Path(temp) / "config", 40, 14,
                                ("--dry-run", "--no-animation"))
            try:
                terminal.wait_for(b"ATM 0.0.2")
                terminal.send(b"u")
                terminal.wait_for(b"Official plugin catalog")
                terminal.send(b"\x1b[B\x1b[B\x1b[B\x1b[B\r")
                terminal.wait_for(b"Plugin ID:")
                terminal.send(b"atm_download_fixture\r")
                terminal.wait_for(b"plugins download")
                terminal.send(b"q")
                self.assertEqual(terminal.proc.wait(timeout=5), 0)
                self.assertEqual(termios.tcgetattr(terminal.slave), terminal.before)
                self.assertFalse((Path(temp) / "config").exists())
                self.assertFalse((ROOT / "plugins" / "atm_download_fixture").exists())
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

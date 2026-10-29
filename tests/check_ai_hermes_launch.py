"""Compiled direct-launch guards; never run Hermes, Python, or a provider."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from check_go_packages import (isolated_environment, seed_user_state, snapshot,
                               verify_read_only_trace)


def check(binary):
    tracer = shutil.which("strace")
    assert tracer, "strace is required"
    with tempfile.TemporaryDirectory(prefix="atm-hermes-launch ' ") as temporary:
        root = Path(temporary)
        seed_user_state(root)
        env = isolated_environment(root)
        for key in tuple(env):
            if key.startswith(("HERMES_", "PYTHON", "OPENAI_", "ANTHROPIC_")):
                env.pop(key)
        env["OPENAI_API_KEY"] = "synthetic-secret-must-not-be-logged"
        private = root / "user/.hermes/config.yaml"
        private.parent.mkdir(mode=0o700)
        private.write_text("synthetic config; do not read\n")
        private.chmod(0o600)
        apps = str(root / "uncreated-apps")
        prefix = ["--hermes-cli", "--apps-dir", apps]
        consent = "--accept-runtime-execution"
        count = 0

        def run(arguments, success=False, dry_env=False):
            nonlocal count
            before = snapshot(root)
            current_env = dict(env)
            if dry_env:
                current_env["ATM_DRY_RUN"] = "1"
            with tempfile.TemporaryDirectory(prefix="atm-hermes-launch-trace-") as trace_dir:
                trace = Path(trace_dir) / "trace"
                result = subprocess.run(
                    [tracer, "-f", "-qq", "-s", "4096", "-e",
                     "trace=execve,execveat,connect,openat,openat2", "-o", str(trace),
                     str(binary), *arguments], env=current_env, input="", text=True,
                    capture_output=True, timeout=20)
                assert (result.returncode == 0) == success, (arguments, result.returncode,
                                                           result.stdout, result.stderr)
                traced = trace.read_text()
                verify_read_only_trace(traced, binary, None)
                assert not any(json.dumps(str(private)) in line for line in traced.splitlines()
                               if "openat(" in line or "openat2(" in line), "read private Hermes config"
            assert snapshot(root) == before, "launch refusal/dry-run changed user state"
            assert not (root / "uncreated-apps").exists(), "created absent installation"
            assert env["OPENAI_API_KEY"] not in result.stdout + result.stderr
            count += 1

        run([*prefix, consent, "--dry-run", "--"], success=True)
        run([*prefix, consent, "--", "--help"], success=True, dry_env=True)
        run([*prefix, consent, "--dry-run", "--", "chat", "spaces and 'quotes'"], success=True)
        run([*prefix, consent, "--", "--help"])  # Missing managed installation.
        for arguments in (
            [*prefix, "--"],
            ["--hermes-cli", consent, "--"],
            ["--hermes-cli", "--apps-dir", "relative", consent, "--"],
            [*prefix, consent],
            [*prefix, consent, consent, "--"],
            [*prefix, "--apps-dir", apps, consent, "--"],
            [*prefix, consent, "--unknown", "--"],
            [*prefix, "--trust-download", "--"],
            ["--hermes-cli", "--apps-dir", "", consent, "--"],
            ["--unknown"],
        ):
            run(arguments)
        run([*prefix, "--"], dry_env=True)
        print(f"PASS Hermes direct launch: {count} compiled guard/dry-run cases; no provider/network/state changes")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: check_ai_hermes_launch.py AI_PREVIEW_BINARY")
    check(Path(sys.argv[1]).resolve(strict=True))

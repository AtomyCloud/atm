"""Compiled REA CLI checks using synthetic manual installs and private fixtures."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from check_go_packages import (host_target, isolated_environment, seed_user_state,
                               snapshot, verify_read_only_trace)


def check(executable, backend):
    assert host_target() in {"linux/amd64", "linux/arm64"}
    source = Path(__file__).resolve().parents[1]
    manifest = json.loads((source / "plugins/rea/plugin.json").read_text())
    tracer = shutil.which("strace")
    assert tracer, "strace required to verify no REA/Node/npm/network execution"
    with tempfile.TemporaryDirectory(prefix="atm-rea CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/rea"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/rea/plugin.json", plugin / "plugin.json")
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        command = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True, backend_started=True):
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-rea-trace-") as trace_dir:
                trace = Path(trace_dir) / "calls"
                invocation = [tracer, "-f", "-qq", "-s", "4096", "-e",
                              "trace=execve,execveat,connect", "-o", str(trace),
                              *command, *args]
                result = subprocess.run(invocation, input="", env=env, text=True,
                                        capture_output=True, timeout=20)
                if success is not None:
                    assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
                try:
                    verify_read_only_trace(trace.read_text(), executable,
                                           target if backend_started else None)
                except AssertionError as error:
                    raise AssertionError((args, result.stdout, result.stderr, str(error))) from error
            assert snapshot(root) == before, (args, "changed user/private/installation state")
            return result

        run("--yes", "plugin", "rea")
        run("status", "rea")
        for submenu in ("install", "version", "dependencies", "details"):
            run("--yes", "plugin", "rea", "menu", submenu)
        run("--yes", "install", "rea", "--dependencies")
        run("--yes", "install", "rea", "--status")
        run("install", "rea", success=False, backend_started=False)
        for operation in manifest["operations"]:
            plan = json.loads(run("--dry-run", "plugin", "rea", operation,
                                  backend_started=False).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"
        for operation, args in (
            ("install", ["--version"]),
            ("install", ["--version", "../../unexpected"]),
            ("install", ["--path", "relative"]),
            ("update", ["--unknown"]),
            ("remove", ["--version", "6.8.0"]),
            ("uninstall", ["--purge-data"]),
            ("integrate", ["--system"]),
            ("status", ["--unknown"]),
        ):
            run("--yes", "plugin", "rea", operation, *args, success=False)

        for relative in (".codex/config.toml", ".claude/settings.json",
                         ".agents/skills/private/SKILL.md", "Captures/private/evidence.txt"):
            path = root / "user" / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("private synthetic fixture; preserve\n")

        def manual(prefix):
            package = prefix / "lib/node_modules/rea-agents"
            script = package / "scripts/rea.mjs"
            script.parent.mkdir(parents=True)
            script.write_text("#!/usr/bin/env node\nthrow new Error('must never execute');\n")
            script.chmod(0o700)
            (package / "package.json").write_text(json.dumps({
                "name": "rea-agents", "version": "6.8.0", "bin": {"rea": "scripts/rea.mjs"},
            }))
            entry = prefix / "bin/rea"
            entry.parent.mkdir(parents=True)
            entry.symlink_to(script)
            (prefix / "personal.txt").write_text("manual install, not native-owned\n")
            return entry, package

        entry, package = manual(root / "apps/rea")
        status = run("status", "rea")
        assert str(entry) in status.stdout and "6.8.0" in status.stdout, status.stdout
        run("--yes", "plugin", "rea")
        for operation in ("install", "update", "remove", "uninstall", "integrate"):
            run("--yes", "plugin", "rea", operation, success=False)

        for metadata in ('{"name":"unrelated","version":"6.8.0"}', "not JSON"):
            (package / "package.json").write_text(metadata)
            status = run("status", "rea")
            assert str(entry) in status.stdout, "unknown metadata hid existing manual executable"
        entry.unlink()
        local_entry, _ = manual(root / "user/.local")
        assert str(local_entry) in run("status", "rea").stdout
        local_entry.unlink()
        home_entry, _ = manual(root / "user/Apps/rea")
        assert str(home_entry) in run("status", "rea").stdout
        home_entry.unlink()
        path_entry, _ = manual(root / "external tools")
        env["PATH"] = str(path_entry.parent)
        assert str(path_entry) in run("status", "rea").stdout
        path_entry.unlink()

        legacy_entry, _ = manual(root / "legacy custom destination")
        legacy_state = Path(env["ATM_STATE_DIR"]) / "rea/install.json"
        legacy_state.parent.mkdir(parents=True, exist_ok=True)
        legacy_state.write_text(json.dumps({"schema": 1,
                                            "root": str(legacy_entry.parent.parent)}))
        legacy_state.chmod(0o600)
        assert str(legacy_entry) in run("status", "rea").stdout
        run("--yes", "plugin", "rea", "uninstall", success=False)
        legacy_entry.unlink()

        # A legacy CORE config can supply the state directory without an env override.
        custom_entry, _ = manual(root / "custom destination")
        state = root / "custom state/rea/native-install.json"
        state.parent.mkdir(parents=True)
        state.write_text(json.dumps({"schema": 1, "plugin": "rea",
                                     "base": str(custom_entry.parent.parent)}))
        state.chmod(0o600)
        config = root / "legacy.conf"
        config.write_text(f'ATM_STATE_DIR="{state.parent.parent}"\n')
        env["ATM_CONFIG_FILE"] = str(config)
        env.pop("ATM_STATE_DIR", None)
        assert str(custom_entry) in run("status", "rea").stdout
        state.write_text("not JSON")
        run("status", "rea", success=False)
        print("PASS REA CLI: offline manual detection without execution/PATH, metadata fallback, "
              "configured state path, consent/dry-run, argument refusals and preservation "
              "of external installs/private agent data")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

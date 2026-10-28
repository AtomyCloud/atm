"""Compiled spotatui CLI guards using synthetic external installations only."""
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
    manifest = json.loads((source / "plugins/spotatui/plugin.json").read_text())
    tracer = shutil.which("strace")
    assert tracer, "strace required to prove no player/dependency/network execution"
    with tempfile.TemporaryDirectory(prefix="atm-spotatui CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/spotatui"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/spotatui/plugin.json", plugin / "plugin.json")
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        command = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True, trace_backend=True):
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-spotatui-trace-") as trace_dir:
                trace = Path(trace_dir) / "calls"
                invocation = command + list(args)
                invocation = [tracer, "-f", "-qq", "-s", "4096", "-e",
                              "trace=execve,execveat,connect", "-o", str(trace), *invocation]
                result = subprocess.run(invocation, input="", env=env, text=True,
                                        capture_output=True, timeout=20)
                if success is not None:
                    assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
                try:
                    verify_read_only_trace(trace.read_text(), executable, target if trace_backend else None)
                except AssertionError as error:
                    raise AssertionError((args, result.stdout, result.stderr, str(error))) from error
            assert snapshot(root) == before, (args, "changed isolated user state")
            return result

        def readonly_queries():
            run("--yes", "plugin", "spotatui")
            return run("status", "spotatui")

        readonly_queries()
        for route in ("actions", "versions", "dependencies", "usage"):
            run("--yes", "plugin", "spotatui", "menu", route)
        run("install", "spotatui", success=False, trace_backend=False)
        run("--yes", "install", "spotatui", "--location", "invalid", success=False)
        run("--yes", "install", "spotatui", "--version", "../../escape", success=False)
        for location in ("atm", "local"):
            for operation, args in (("install", []), ("update", []), ("use", ["0.1.0"])):
                run("--yes", "plugin", "spotatui", operation, *args,
                    "--location", location, success=False)
            for operation in manifest["operations"]:
                args = ["0.1.0"] if operation in {"use", "remove"} else []
                plan = json.loads(run("--dry-run", "plugin", "spotatui", operation,
                                      *args, "--location", location, trace_backend=False).stdout)
                assert plan["dry_run"] and plan["executor"] == "native"

        # Foreign players and their support directories are data, not test tools.
        atm_root = root / "apps/spotatui"
        version = atm_root / "0.1.0"
        local_entry = root / "user/.local/bin/spotatui"
        for path in (version / "bin/spotatui", local_entry):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("#!/bin/sh\nexit 97\n")
            path.chmod(0o700)
        (atm_root / "current").symlink_to("0.1.0", target_is_directory=True)
        (version / ".atm-spotatui-managed").write_text("legacy fixture\n")
        (version / "personal.txt").write_text("preserve legacy installation\n")
        local_root = root / "user/.local/share/atm/spotatui/0.1.0"
        local_root.mkdir(parents=True)
        (local_root / "personal.txt").write_text("preserve local support files\n")
        for config in (root / "config/spotatui", root / "user/.config/spotatui"):
            config.mkdir(parents=True)
            (config / "config.toml").write_text('[behavior]\nytdlp_path="/private/custom-tool"\n')
            (config / "credentials.json").write_text('{"fixture": "private"}\n')

        status = readonly_queries()
        assert str(local_entry) in status.stdout, "external user-local player not reported"
        run("--yes", "install", "spotatui", "--location", "local",
            "--trust-dependencies", success=False)
        for location in ("atm", "local"):
            run("--yes", "remove", "spotatui", "0.1.0", "--location", location, success=False)
            run("--yes", "uninstall", "spotatui", "--location", location, success=None)
            run("--yes", "plugin", "spotatui", "integrate", "--location", location,
                success=False)

        for base in (atm_root, local_root.parent):
            native_root = base / "native-v1"
            native_root.mkdir()
            (native_root / "personal.txt").write_text("unowned native-shaped root\n")
        for location in ("atm", "local"):
            for operation in ("install", "update"):
                run("--yes", operation, "spotatui", "--location", location,
                    "--trust-dependencies", success=False)
        print("PASS spotatui CLI: offline menus/status, foreign player detection without execution, "
              "consent/dry-run, dependency trust and legacy/local/private-data preservation")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

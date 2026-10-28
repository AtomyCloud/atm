"""Read-only compiled CLI checks. Never acknowledge a real system mutation."""
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
    manifest = json.loads((source / "plugins/os_packages/plugin.json").read_text())
    tracer = shutil.which("strace")
    assert tracer, "strace required"
    with tempfile.TemporaryDirectory(prefix="atm-os-packages CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/os_packages"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        for name in ("plugin.json", "packages.txt"):
            shutil.copyfile(source / "plugins/os_packages" / name, plugin / name)
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        base = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True, started=True):
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-package-trace-") as trace_dir:
                trace = Path(trace_dir) / "trace"
                result = subprocess.run(
                    [tracer, "-f", "-qq", "-s", "4096", "-e",
                     "trace=execve,execveat,connect", "-o", str(trace), *base, *args],
                    env=env, input="", capture_output=True, text=True, timeout=20,
                )
                assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
                verify_read_only_trace(trace.read_text(), executable, target if started else None)
            assert snapshot(root) == before, (args, "unexpected user-state writes")
            return result

        run("status", "os_packages")
        menu = run("--yes", "plugin", "os_packages")
        for view in ("install", "remove", "commands"):
            result = run("--yes", "plugin", "os_packages", "menu", view)
            terminal = json.loads(result.stdout.splitlines()[-1])
            if view != "commands":
                choice = terminal["choices"][0]
                assert "--accept-system-packages" in choice["arguments"]
                assert "--plan" in choice["arguments"]
                if view == "remove":
                    assert "--accept-removal" in choice["arguments"]
        if '"ubuntu"' in menu.stdout:
            run("--yes", "plugin", "os_packages", "menu", "install", "--ubuntu-sources")
        run("install", "os_packages", success=False, started=False)
        for operation in ("install", "remove", "uninstall"):
            run("--yes", "plugin", "os_packages", operation, success=False)
        for operation in manifest["operations"]:
            plan = json.loads(run("--dry-run", "plugin", "os_packages", operation,
                                  started=False).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"
        (plugin / "packages.txt").write_text("[apt-get]\n--allow-unauthenticated\n")
        run("--yes", "plugin", "os_packages", "menu", "install", success=False)
        print("PASS OS Packages CLI: offline reviews/status, explicit consent gates, "
              "CORE-only dry-run, malformed data refusal, no sudo/manager/network or writes")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

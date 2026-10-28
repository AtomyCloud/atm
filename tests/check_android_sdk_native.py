"""Android SDK CLI guards with synthetic unowned state and no vendor execution."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from check_go_packages import (host_target, isolated_environment, read_only_runner,
                               seed_user_state, snapshot, verify_read_only_backend,
                               verify_read_only_trace)


def check(executable, backend):
    assert host_target() == "linux/amd64", "Android SDK host acceptance requires Linux amd64"
    source = Path(__file__).resolve().parents[1]
    manifest = json.loads((source / "plugins/android_sdk/plugin.json").read_text())
    tracer = shutil.which("strace")
    assert tracer, "strace required to prove Java/sdkmanager/network are not invoked"
    with tempfile.TemporaryDirectory(prefix="atm-sdk CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/android_sdk"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/android_sdk/plugin.json", plugin / "plugin.json")
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        command = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True, trace_backend=False):
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-sdk-trace-") as trace_dir:
                trace = Path(trace_dir) / "calls"
                invocation = command + list(args)
                if trace_backend:
                    invocation = [tracer, "-f", "-qq", "-s", "4096", "-e",
                                  "trace=execve,execveat,connect", "-o", str(trace), *invocation]
                result = subprocess.run(invocation, input="", env=env, text=True,
                                        capture_output=True, timeout=20)
                assert (result.returncode == 0) == success, (result.stdout, result.stderr)
                if trace_backend:
                    verify_read_only_trace(trace.read_text(), executable, target)
            assert snapshot(root) == before, "read-only/refused SDK action changed isolated state"
            return result

        readonly = read_only_runner(command, env, target)
        verify_read_only_backend(readonly, manifest, root)
        run("install", "android_sdk", success=False)
        run("--yes", "install", "android_sdk", success=False, trace_backend=True)
        run("--yes", "update", "android_sdk", success=False, trace_backend=True)
        run("--yes", "install", "android_sdk", "--accept-licenses", "yes",
            success=False, trace_backend=True)
        run("--yes", "install", "android_sdk", "--stack", "api37",
            success=False, trace_backend=True)

        java = root / "apps/Java/current/bin/java"
        java.parent.mkdir(parents=True)
        java.write_text("#!/bin/sh\nexit 99\n")
        java.chmod(0o700)
        sdk = root / "apps/AndroidStudio/SDK"
        tools = sdk / "cmdline-tools/latest"
        (tools / "bin").mkdir(parents=True)
        (tools / "bin/sdkmanager").write_text("#!/bin/sh\nexit 99\n")
        (tools / "bin/sdkmanager").chmod(0o700)
        (tools / "source.properties").write_text("Pkg.Revision=20.0\nPkg.Desc=Android SDK Command-line Tools\n")
        (sdk / "platform-tools").mkdir()
        (sdk / "platform-tools/source.properties").write_text("Pkg.Revision=37.0.0\n")
        private = sdk / ".android/avd"
        private.mkdir(parents=True)
        (private / "userdata.img").write_bytes(b"private fixture, preserve")
        (sdk / "user-notes.txt").write_text("existing unowned SDK\n")

        verify_read_only_backend(readonly, manifest, root)
        run("--yes", "plugin", "android_sdk", "menu", "java", trace_backend=True)
        run("--yes", "plugin", "android_sdk", "menu", "stacks", trace_backend=True)
        for operation, args in (
            ("install", ["--accept-licenses", "a" * 64]),
            ("update", ["--accept-licenses", "a" * 64]),
            ("remove", ["platform-tools"]),
            ("uninstall", []),
            ("integrate", []),
        ):
            run("--yes", "plugin", "android_sdk", operation, *args,
                success=False, trace_backend=True)
            plan = json.loads(run("--dry-run", "plugin", "android_sdk", operation, *args).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"
        print("PASS Android SDK CLI: no consent/ack refusal, offline root/status/Java/stack menus, "
              "unowned SDK and private data preservation, dry-run and no Java/sdkmanager/network execution")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

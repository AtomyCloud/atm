"""Linux CLI acceptance with synthetic editor files; never run a real VS Code."""
import json
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile

from check_go_packages import (host_target, isolated_environment, read_only_runner,
                               seed_user_state, snapshot, verify_read_only_backend)


def legacy_editor(root, version):
    editor = root / ("code_" + version)
    app = editor / "resources" / "app"
    (app / "out").mkdir(parents=True)
    (app / "resources" / "linux").mkdir(parents=True)
    (editor / "bin").mkdir()
    (editor / "bin" / "code").write_text("#!/bin/sh\nexit 99\n")
    (editor / "bin" / "code").chmod(0o700)
    machine = {"linux/amd64": 62, "linux/arm64": 183}[host_target()]
    header = b"\x7fELF\x02\x01\x01" + bytes(9)
    header += struct.pack("<HHIQQQIHHHHHH", 3, machine, 1, 0, 0, 0, 0, 64, 56, 0, 64, 0, 0)
    (editor / "code").write_bytes(header)
    (editor / "code").chmod(0o700)
    (app / "package.json").write_text(json.dumps({"name": "Code", "version": version}))
    (app / "product.json").write_text(json.dumps({
        "applicationName": "code", "quality": "stable", "commit": "a" * 40}))
    (app / "out" / "cli.js").write_text("throw new Error('fixture must never run');\n")
    (app / "resources" / "linux" / "code.png").write_bytes(b"fixture icon")
    data = editor / "data" / "user-data" / "User"
    data.mkdir(parents=True)
    (data / "settings.json").write_text(json.dumps({"fixture": version}))
    return editor


def check(executable, backend):
    source = Path(__file__).resolve().parents[1]
    manifest = json.loads((source / "plugins/vscode/plugin.json").read_text())
    with tempfile.TemporaryDirectory(prefix="atm-vscode CLI 100%-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins" / "vscode"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/vscode/plugin.json", plugin / "plugin.json")
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        command = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True):
            result = subprocess.run(command + list(args), input="", env=env, text=True,
                                    capture_output=True, timeout=20)
            assert (result.returncode == 0) == success, (result.stdout, result.stderr)
            return result

        versions = root / "apps" / "vscode"
        old = legacy_editor(versions, "1.118.0")
        new = legacy_editor(versions, "1.118.1")
        migrate = ("plugin", "vscode", "integrate", "migrate-data", "1.118.0", "1.118.1", "--closed")
        before = snapshot(root)
        run(*migrate, success=False)
        assert snapshot(root) == before, "unconfirmed migration wrote files"
        plan = json.loads(run("--dry-run", *migrate).stdout)
        assert plan["dry_run"] is True and plan["operation"] == "integrate"
        assert snapshot(root) == before, "migration dry-run wrote files"
        run("--yes", *migrate[:-1], success=False)
        assert snapshot(root) == before, "missing closed-editor acknowledgement wrote files"

        run("--yes", "use", "vscode", "1.118.1")
        assert (versions / "current").resolve() == new
        assert not (root / "user" / ".local").exists(), "selection implicitly integrated"
        verify_read_only_backend(read_only_runner(command, env, target), manifest, root)

        relative = Path("data/user-data/User/settings.json")
        original = (old / relative).read_bytes()
        displaced = (new / relative).read_bytes()
        run("--yes", *migrate)
        assert (old / relative).read_bytes() == original, "source data changed"
        assert (new / relative).read_bytes() == original, "target data not migrated"
        retained = [p for p in versions.rglob("settings.json") if p.read_bytes() == displaced]
        assert len(retained) == 1, "displaced target data was not retained exactly once"
        before = snapshot(root)
        run("--yes", "remove", "vscode", "1.118.0", success=False)
        run("--yes", "uninstall", "vscode")
        assert snapshot(root) == before, "refused legacy deletion changed data"

        (old / "data" / "external").symlink_to(root / "user", target_is_directory=True)
        before = snapshot(root)
        run("--yes", *migrate, success=False)
        assert snapshot(root) == before, "unsafe migration modified source/target/backup"

        desktop_dir = root / "user" / ".local" / "share" / "applications"
        desktop_dir.mkdir(parents=True)
        desktop = desktop_dir / "code.desktop"
        desktop.write_text("existing user launcher\n")
        before = snapshot(root)
        run("--yes", "plugin", "vscode", "integrate", success=False)
        assert snapshot(root) == before, "integration overwrote an unowned launcher"
        desktop.unlink()
        run("--yes", "plugin", "vscode", "integrate")
        assert (root / "user/.local/bin/code").resolve() == new / "bin/code"
        validator = shutil.which("desktop-file-validate")
        for name in ("code.desktop", "code-url-handler.desktop"):
            entry = desktop_dir / name
            assert '100%%-' in entry.read_text(), "desktop Exec percent not escaped"
            if validator:
                subprocess.run([validator, str(entry)], check=True, capture_output=True, timeout=10)
        run("--yes", "use", "vscode", "1.118.0")
        assert (root / "user/.local/bin/code").resolve() == old / "bin/code"
        desktop.write_text(desktop.read_text() + "# user edit\n")
        before = snapshot(root)
        run("--yes", "use", "vscode", "1.118.1", success=False)
        assert snapshot(root) == before, "selection overwrote changed integration"
        print("PASS VS Code CLI: consent/dry-run, legacy selection, offline read-only queries, "
              "portable data copy and retained destination, legacy preservation, unsafe-link refusal, "
              "explicit integration, owned desktop guards and paths with spaces/percent")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

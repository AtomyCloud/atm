"""Compiled Brave CLI guards using private synthetic browser data only."""
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile

from check_go_packages import (host_target, isolated_environment, seed_user_state,
                               snapshot, verify_read_only_trace)


def check(executable, backend):
    assert host_target() in {"linux/amd64", "linux/arm64"}
    source = Path(__file__).resolve().parents[1]
    manifest = json.loads((source / "plugins/brave/plugin.json").read_text())
    tracer = shutil.which("strace")
    assert tracer, "strace required to rule out browser/system-tool execution"
    with tempfile.TemporaryDirectory(prefix="atm-brave CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/brave"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/brave/plugin.json", plugin / "plugin.json")
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        profile = root / "user/.config/BraveSoftware/Brave-Browser"
        for name in ("Default", "Profile 0", "Profile 12", "Guest Profile"):
            path = profile / name
            path.mkdir(parents=True, mode=0o700)
            (path / "Bookmarks").write_text(json.dumps({"fixture": name}))
            (path / "History").write_bytes(b"synthetic private history\x00")
        (profile / "Local State").write_text('{"fixture": "preserve"}')
        env["ATM_BRAVE_PROFILE_DIR"] = str(profile)
        env["ATM_BRAVE_BACKUP_DIR"] = str(root / "apps/backups/brave")
        # Even a discovered wrapper must never run during status or reviews.
        fake_bin = root / "fake-bin"
        fake_bin.mkdir()
        marker = root / "unexpected-browser-launch"
        wrapper = fake_bin / "brave-browser"
        wrapper.write_text(f'#!/bin/sh\nprintf unexpected > "{marker}"\nexit 97\n')
        wrapper.chmod(0o700)
        env["PATH"] = str(fake_bin)
        for path in root.rglob("*"):
            if path.is_dir() and not path.is_symlink():
                path.chmod(0o700)
        base = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True, started=True, read_only=True):
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-brave-trace-") as trace_dir:
                trace = Path(trace_dir) / "trace"
                result = subprocess.run(
                    [tracer, "-f", "-qq", "-s", "4096", "-e",
                     "trace=execve,execveat,connect", "-o", str(trace), *base, *args],
                    env=env, input="", capture_output=True, text=True, timeout=20,
                )
                if success is not None:
                    assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
                verify_read_only_trace(trace.read_text(), executable, target if started else None)
            if read_only:
                assert snapshot(root) == before, (args, "unexpected profile/backup/state writes")
            else:
                after = snapshot(root)
                allowed = ("user/.config/BraveSoftware/", "apps/backups/", "state/")
                changed = {p for p in before.keys() | after.keys() if before.get(p) != after.get(p)}
                assert all(p.startswith(allowed) or p in {"apps/backups", "state"}
                           for p in changed), (args, "write outside operation roots", changed)
            assert not marker.exists(), "browser wrapper executed"
            return result

        run("status", "brave")
        menu = run("--yes", "plugin", "brave")
        terminal = json.loads(menu.stdout.splitlines()[-1])
        assert terminal["choices"], "native menu is empty"
        for view in ("install", "remove", "commands", "backups"):
            result = run("--yes", "plugin", "brave", "menu", view,
                         success=None if view in {"install", "commands"} else True)
            if result.returncode:
                terminal = json.loads(result.stdout.splitlines()[-1])
                assert terminal["status"] == "error" and terminal.get("message", "").startswith(
                    ("existing Brave source declaration:", "untrusted system file/ancestor:")), terminal
                print("NOTE: installation review safely refused host repository state:", terminal["message"])
        run("install", "brave", success=False, started=False)
        for operation in ("install", "remove", "uninstall"):
            run("--yes", "plugin", "brave", operation, success=False)
        for operation in manifest["operations"]:
            plan = json.loads(run("--dry-run", "plugin", "brave", operation,
                                  started=False).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"
        for operation, args in (
            ("status", ["--unknown"]),
            ("install", ["--repository", "https://invalid.example/repo"]),
            ("remove", ["--purge-data"]),
            ("menu", ["../../unexpected"]),
            ("menu", ["backup-profile", "--unexpected"]),
            ("menu", ["restore", "../outside", "--sha256", "0" * 64, "--accept-restore"]),
            ("menu", ["restore", "brave-profile-missing.tar.gz", "--accept-restore"]),
        ):
            run("--yes", "plugin", "brave", operation, *args, success=False)

        # Full archive transactions are restricted to synthetic profiles and backups.
        original = snapshot(profile)
        def content_inventory(path):
            return {name: (stat.S_IFMT(mode), identity)
                    for name, (mode, identity) in snapshot(path).items()}

        original_content = content_inventory(profile)
        result = run("--yes", "plugin", "brave", "menu", "backup-profile",
                     success=None, read_only=False)
        if result.returncode:
            terminal = json.loads(result.stdout.splitlines()[-1])
            message = terminal.get("message", "")
            assert terminal["type"] == "result" and terminal["status"] == "error", terminal
            process_error = re.sub(r"^owned process [0-9]+: ", "", message)
            assert process_error.startswith(("close Brave first", "cannot inspect owned process:",
                                       "cannot resolve owned process executable:",
                                       "cannot resolve owned process executable; unknown identity:",
                                       "restricted proc visibility;", "process visibility unknown",
                                       "cannot establish process visibility:")), terminal
            assert snapshot(profile) == original
            print("NOTE: host process guard refused synthetic CLI backup; CLI archive lifecycle not run:", message)
        else:
            assert snapshot(profile) == original, "backup modified the source profile"
            backups = Path(env["ATM_BRAVE_BACKUP_DIR"])
            archives = list(backups.glob("*.tar.gz"))
            assert len(archives) == 1, archives
            full = archives[0]
            assert full.stat().st_mode & 0o777 == 0o600
            with tarfile.open(full) as archive:
                for name in ("Default/Bookmarks", "Profile 0/Bookmarks", "Profile 12/Bookmarks", "Local State"):
                    member = next(m for m in archive.getmembers() if m.name.endswith(name))
                    assert archive.extractfile(member).read() == (profile / name).read_bytes()
            full_bytes = full.read_bytes()
            run("--yes", "plugin", "brave", "menu", "backup-bookmarks", read_only=False)
            bookmarks = next(p for p in backups.glob("*.tar.gz") if p != full)
            assert bookmarks.stat().st_mode & 0o777 == 0o600
            with tarfile.open(bookmarks) as archive:
                files = [m.name for m in archive.getmembers() if m.isfile()]
                assert len(files) == 3 and all(n.endswith("/Bookmarks") for n in files), files
                assert not any("Guest Profile" in n for n in files)
                for name in ("Default", "Profile 0", "Profile 12"):
                    assert archive.extractfile(name + "/Bookmarks").read() == (profile / name / "Bookmarks").read_bytes()
            assert full.read_bytes() == full_bytes and snapshot(profile) == original
            run("--yes", "plugin", "brave", "menu", "backups")

            def restore(path, sha=None, success=True):
                return run("--yes", "plugin", "brave", "menu", "restore", path.name,
                           "--sha256", sha or hashlib.sha256(path.read_bytes()).hexdigest(),
                           "--accept-restore", success=success, read_only=False)

            restore(full, "0" * 64, success=False)
            restore(bookmarks, success=False)
            for index, name in enumerate(("../escape", "Brave-Browser/../../escape", str(root / "escape"))):
                hostile = backups / f"brave-profile-hostile-{index}.tar.gz"
                with tarfile.open(hostile, "w:gz") as archive:
                    member = tarfile.TarInfo(name)
                    member.size = 3
                    archive.addfile(member, io.BytesIO(b"bad"))
                hostile.chmod(0o600)
                restore(hostile, success=False)
                assert snapshot(profile) == original
            changed = b'{"fixture": "newer data must survive in recovery"}'
            (profile / "Default/Bookmarks").write_bytes(changed)
            displaced_content = content_inventory(profile)
            restore(full)
            assert content_inventory(profile) == original_content, "restored tree is incomplete or changed"
            recovery = [p for p in profile.parent.iterdir() if p != profile and p.is_dir()]
            assert any(content_inventory(p) == displaced_content for p in recovery), "displaced recovery tree is incomplete"
            assert full.read_bytes() == full_bytes
            print("PASS Brave CLI synthetic profile/bookmark archive lifecycle, unsafe archive refusal and retained recovery")
        print("PASS Brave CLI: read-only status/menu, no browser/tool/network execution, "
              "explicit consent, CORE-only dry-run and private profile preservation")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

"""Compiled Zsh CLI checks with private synthetic settings and no system changes."""
import copy
import gzip
import json
import hashlib
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile

from check_go_packages import (host_target, isolated_environment, seed_user_state,
                               snapshot, verify_read_only_trace)


def check(executable, backend):
    assert host_target() in {"linux/amd64", "linux/arm64"}
    source = Path(__file__).resolve().parents[1]
    manifest = json.loads((source / "plugins/zsh/plugin.json").read_text())
    tracer = shutil.which("strace")
    assert tracer, "strace required to detect unexpected execution or networking"
    with tempfile.TemporaryDirectory(prefix="atm-zsh CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/zsh"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        shutil.copyfile(source / "plugins/zsh/plugin.json", plugin / "plugin.json")
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        for key in ("ZSH", "ZDOTDIR", "ZSH_CUSTOM", "SUDO_USER", "SUDO_UID", "SUDO_GID"):
            env.pop(key, None)
        home = root / "user"
        omz = home / ".oh-my-zsh"
        (omz / "custom").mkdir(parents=True)
        marker = root / "unexpected-shell-execution"
        payload = f'printf unexpected > "{marker}"\n'
        (home / ".zshrc").write_text("# Synthetic private configuration\n" + payload)
        (home / ".zsh_history").write_text("synthetic history\n")
        (omz / "oh-my-zsh.sh").write_text(payload)
        (omz / "custom/plugin.zsh").write_text("# synthetic custom data\n")
        env.update(ATM_ZSH_USER_HOME=str(home), ATM_ZSH_DOT_DIR=str(home),
                   ATM_ZSH_OMZ_DIR=str(omz), ATM_ZSH_CUSTOM_DIR=str(omz / "custom"),
                   ATM_ZSH_BACKUP_DIR=str(root / "apps/Backups"))
        for path in root.rglob("*"):
            if path.is_dir() and not path.is_symlink():
                path.chmod(0o700)
            elif path.is_file() and not path.is_symlink() and path != target:
                path.chmod(0o600)
        (home / ".zsh_history").chmod(0o640)
        (omz / "custom").chmod(0o750)
        base = [str(executable), "--home", str(runtime), "--json"]

        def run(*args, success=True, started=True, read_only=True):
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-zsh-trace-") as trace_dir:
                trace = Path(trace_dir) / "trace"
                result = subprocess.run(
                    [tracer, "-f", "-qq", "-s", "4096", "-e",
                     "trace=execve,execveat,connect", "-o", str(trace), *base, *args],
                    env=env, input="", capture_output=True, text=True, timeout=20,
                )
                assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
                verify_read_only_trace(trace.read_text(), executable, target if started else None)
            after = snapshot(root)
            if read_only:
                assert after == before, (args, "unexpected writes to user state")
            else:
                changed = {p for p in before.keys() | after.keys() if before.get(p) != after.get(p)}
                assert all(p.startswith(("user/", "state/", "apps/Backups/"))
                           or p in {"state", "apps/Backups"} for p in changed), (args, changed)
            assert not marker.exists(), "user shell configuration executed"
            return result

        run("status", "zsh")
        menu = run("--yes", "plugin", "zsh")
        terminal = json.loads(menu.stdout.splitlines()[-1])
        assert terminal["choices"], "native menu is empty"
        run("install", "zsh", success=False, started=False)
        for operation in ("install", "update", "remove", "uninstall"):
            run("--yes", "plugin", "zsh", operation, success=False)
        run("--yes", "plugin", "zsh", "status", "--unexpected", success=False)
        run("--yes", "plugin", "zsh", "menu", "unknown-view", success=False)
        for view in ("remove", "backup", "backups"):
            run("--yes", "plugin", "zsh", "menu", view)
        # A synthetic HOME must not be accepted as the local account's identity.
        run("--yes", "plugin", "zsh", "menu", "shells", success=False)
        run("--yes", "plugin", "zsh", "use", "/bin/bash", success=False)
        run("--yes", "plugin", "zsh", "menu", "restore", "../outside.tar.gz", success=False)
        for operation in manifest["operations"]:
            plan = json.loads(run("--dry-run", "plugin", "zsh", operation,
                                  started=False).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"

        def file_content(path):
            return {name: identity for name, (mode, identity) in snapshot(path).items()
                    if stat.S_ISREG(mode)}

        original = file_content(home)
        original_state = snapshot(home)
        run("--yes", "plugin", "zsh", "menu", "backup", "--accept-snapshot", read_only=False)
        assert file_content(home) == original, "backup modified configuration"
        archives = list((root / "apps/Backups").glob("zsh-native-*.json.gz"))
        assert len(archives) == 1, archives
        archive = archives[0]
        assert archive.stat().st_mode & 0o777 == 0o600
        archive_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
        listing = run("--yes", "plugin", "zsh", "menu", "backups")
        choices = json.loads(listing.stdout.splitlines()[-1])["choices"]
        restore = next(c for c in choices if "restore" in c.get("arguments", []))
        assert str(archive) in restore["arguments"] and archive_hash in restore["arguments"]
        args = list(restore["arguments"])
        args[args.index(archive_hash)] = "0" * 64
        run("--yes", "plugin", "zsh", restore["operation"], *args, success=False)
        original_archive = archive.read_bytes()
        document = json.loads(gzip.decompress(original_archive))
        damaged_archives = [original_archive[:-8], original_archive + b"unexpected trailing data"]
        for mode in ("traversal", "foreign-root", "duplicate-entry", "directory-dotfile", "writable-mode", "unknown-field"):
            invalid = copy.deepcopy(document)
            entry = next(e for e in invalid["entries"] if e["root"] == "zshrc")
            if mode == "traversal":
                entry["path"] = "../outside"
            elif mode == "foreign-root":
                entry["root"] = "unrelated"
            elif mode == "duplicate-entry":
                invalid["entries"].append(copy.deepcopy(entry))
            elif mode == "directory-dotfile":
                entry["dir"] = True
                entry.pop("sha256", None)
                entry.pop("data", None)
            elif mode == "writable-mode":
                entry["mode"] = 0o777
            elif mode == "unknown-field":
                invalid["unexpected"] = True
            damaged_archives.append(gzip.compress(json.dumps(invalid).encode()))
        duplicate_json = json.dumps(document).replace(
            '"format":', '"format": "atm-zsh-native-v1", "format":', 1)
        damaged_archives.append(gzip.compress(duplicate_json.encode()))
        for damaged in damaged_archives:
            archive.write_bytes(damaged)
            args = list(restore["arguments"])
            args[args.index(archive_hash)] = hashlib.sha256(damaged).hexdigest()
            run("--yes", "plugin", "zsh", restore["operation"], *args, success=False)
            assert file_content(home) == original
        archive.write_bytes(original_archive)
        (home / ".zshrc").write_text("# Later synthetic configuration; retain recovery\n")
        later_hash = hashlib.sha256((home / ".zshrc").read_bytes()).hexdigest()
        run("--yes", "plugin", "zsh", restore["operation"], *restore["arguments"], read_only=False)
        for name, identity in original.items():
            assert file_content(home).get(name) == identity, (name, "restore content mismatch")
        restored_state = snapshot(home)
        for name, state in original_state.items():
            assert restored_state.get(name) == state, (name, "restore changed original permissions or content")
        assert any(identity == later_hash for name, (mode, identity) in snapshot(root).items()
                   if stat.S_ISREG(mode)), "displaced configuration recovery was not retained"
        assert hashlib.sha256(archive.read_bytes()).hexdigest() == archive_hash
        print("PASS Zsh CLI: manual-install status/menu, no shell/config/network execution, "
              "explicit consent, CORE-only dry-run, private backup and hash-bound restore/recovery")


if __name__ == "__main__":
    check(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())

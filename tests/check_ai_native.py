"""Opt-in AI preview checks through CORE; never execute legacy or provider code."""
import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from check_go_packages import (host_target, isolated_environment, native_manifest,
                               seed_user_state, snapshot, verify_read_only_trace)


def check(executable, backend):
    source = Path(__file__).resolve().parents[1]
    production = json.loads((source / "plugins/ai/plugin.json").read_bytes())
    assert production["executor"] == "legacy-bash", "revisit preview gate after full AI parity"
    data = (source / "plugins/ai/tests/native-preview.json").read_bytes()
    manifest = native_manifest(data, "ai")
    assert not manifest.get("full_setup", False)
    assert host_target() in manifest["executables"]
    tracer = shutil.which("strace")
    assert tracer, "strace required to verify no subprocess/network activity"
    with tempfile.TemporaryDirectory(prefix="atm-ai ' CLI-") as directory:
        root = Path(directory)
        runtime = root / "runtime"
        plugin = runtime / "plugins/ai"
        plugin.mkdir(parents=True)
        seed_user_state(root)
        shutil.copyfile(source / "core-api.json", runtime / "core-api.json")
        shutil.copytree(source / "lang", runtime / "lang")
        (plugin / "plugin.json").write_bytes(data)
        target = plugin / manifest["executables"][host_target()]
        target.parent.mkdir(parents=True)
        shutil.copyfile(backend, target)
        target.chmod(0o700)
        env = isolated_environment(root)
        for key in tuple(env):
            if key.startswith(("HERMES_", "OLLAMA_", "PICOCLAW_", "CODEX_", "OPENAI_")):
                env.pop(key)
        marker = root / "unexpected-provider-execution"
        payload = f'#!/bin/sh\nprintf unexpected > "{marker}"\n'
        opaque_paths = []
        for relative in ("user/.local/bin/hermes", "user/.local/bin/hermes-agent", "user/.local/bin/ollama",
                         "user/.local/bin/codex", "user/.local/bin/picoclaw",
                         "apps/ai/picoclaw/picoclaw", "apps/ai/codex/cli/bin/codex",
                         "apps/hermes/hermes-desktop/current.AppImage"):
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(payload)
            path.chmod(0o700)
            opaque_paths.append(path)
        for relative in ("user/.codex/auth.json", "user/.hermes/config.yaml",
                         "user/.picoclaw/config.json", "user/.ollama/models/sentinel"):
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("synthetic private data\n")
            path.chmod(0o600)
            opaque_paths.append(path)
        for path in root.rglob("*"):
            if path.is_dir():
                path.chmod(0o700)
        base = [str(executable), "--home", str(runtime), "--json"]
        count = 0

        def run(*args, success=True, started=True, read_only=True,
                write_roots=("apps/ai/picoclaw/native-v1",)):
            nonlocal count
            before = snapshot(root)
            with tempfile.TemporaryDirectory(prefix="atm-ai-trace-") as trace_dir:
                trace = Path(trace_dir) / "trace"
                result = subprocess.run(
                    [tracer, "-f", "-qq", "-s", "4096", "-e",
                     "trace=execve,execveat,connect,openat,openat2", "-o", str(trace), *base, *args],
                    env=env, input="", capture_output=True, text=True, timeout=20)
                assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
                traced = trace.read_text()
                verify_read_only_trace(traced, executable, target if started else None)
                for line in traced.splitlines():
                    if "openat(" in line or "openat2(" in line:
                        assert not any(json.dumps(str(p)) in line for p in opaque_paths), (args, "opened external executable/private data", line)
            after = snapshot(root)
            if read_only:
                assert after == before, (args, "unexpected user-state change")
            else:
                changed = {p for p in before.keys() | after.keys() if before.get(p) != after.get(p)}
                assert all(any(p == allowed or p.startswith(allowed + "/")
                               for allowed in write_roots) for p in changed), (args, changed)
            assert not marker.exists(), "provider executable ran"
            count += 1
            return result

        def messages(result):
            return "\n".join(json.loads(line).get("message", "") for line in result.stdout.splitlines())

        initial = messages(run("status", "ai"))
        for relative in ("user/.local/bin/hermes", "user/.local/bin/ollama",
                         "user/.local/bin/codex", "user/.local/bin/picoclaw",
                         "apps/ai/picoclaw/picoclaw", "apps/ai/codex/cli/bin/codex",
                         "apps/hermes/hermes-desktop/current.AppImage"):
            assert str(root / relative) in initial, ("missing external candidate outside PATH", relative, initial)
        assert "unverified" in initial.lower(), "external evidence must not claim a verified installation"

        # Discovery must ignore cwd/relative PATH entries and inspect absolute
        # entries without running or opening their executable content.
        cwd_candidate = plugin / "codex"
        cwd_candidate.write_text(payload)
        cwd_candidate.chmod(0o700)
        opaque_paths.append(cwd_candidate)
        outside = root / "external-bin"
        outside.mkdir(mode=0o700)
        external = outside / "ollama"
        external.write_text(payload)
        external.chmod(0o700)
        opaque_paths.append(external)
        env["PATH"] = f".:relative::{outside}:{outside}"
        discovered = messages(run("status", "ai"))
        assert str(external) in discovered, "absolute PATH candidate not reported"
        assert str(cwd_candidate) not in discovered, "cwd executable was considered installed"
        assert discovered.count(str(external)) == 1, "duplicate PATH candidate reported"
        env["PATH"] = ""

        # Special files and broken links must not hang discovery or get adopted.
        unusual = root / "user/.local/bin/hermes-desktop"
        os.mkfifo(unusual, 0o600)
        opaque_paths.append(unusual)
        special = messages(run("status", "ai"))
        assert str(unusual) in special, "special-file diagnostic missing"
        unusual.unlink()
        unusual.symlink_to("missing-hermes-desktop")
        broken = messages(run("status", "ai"))
        assert str(unusual) in broken, "broken-link diagnostic missing"
        unusual.unlink()
        unusual.symlink_to(unusual.name)
        cyclic = messages(run("status", "ai"))
        assert str(unusual) in cyclic, "cyclic-link diagnostic missing"
        unusual.unlink()

        menu = run("--yes", "plugin", "ai")
        assert json.loads(menu.stdout.splitlines()[-1])["choices"], "preview menu empty"
        for provider in ("picoclaw", "hermes-desktop", "codex-cli", "ollama"):
            submenu = run("--yes", "plugin", "ai", "menu", provider)
            choices = json.loads(submenu.stdout.splitlines()[-1])["choices"]
            assert {c["id"] for c in choices} >= {"install", "custom", "update", "uninstall"}
            for action in ("install", "update"):
                run("--yes", "plugin", "ai", action, "--provider", provider, success=False)
            run("--yes", "plugin", "ai", "use", "--provider", provider,
                "--version", "0.1.0", success=False)
            run("--yes", "plugin", "ai", "remove", "--provider", provider,
                "--version", "0.1.0", success=False)
            run("--yes", "plugin", "ai", "uninstall", "--provider", provider, success=False)
            run("--yes", "plugin", "ai", "install", "--provider", provider,
                "--version", "../../escape", "--trust-download", success=False)
            run("--yes", "plugin", "ai", "install", "--provider", provider,
                "--provider", provider, "--trust-download", success=False)
            run("--yes", "plugin", "ai", "menu", provider, "--trust-download", success=False)
        # ROCm is an explicit portable companion, never a driver installer.
        ollama_menu = run("--yes", "plugin", "ai", "menu", "ollama")
        ollama_choices = json.loads(ollama_menu.stdout.splitlines()[-1])["choices"]
        rocm_choices = [c for c in ollama_choices if "--rocm" in c.get("arguments", [])]
        if host_target() == "linux/amd64":
            assert rocm_choices, "ROCm install choice missing on amd64"
            assert all(c["operation"] in ("install", "update") and
                       "--trust-download" in c["arguments"] for c in rocm_choices)
        else:
            assert not rocm_choices, "ROCm offered on unsupported architecture"
        for operation in ("install", "update"):
            run("--yes", "plugin", "ai", operation, "--provider", "ollama",
                "--rocm", success=False)  # Download consent remains mandatory.
            plan = json.loads(run("--dry-run", "plugin", "ai", operation,
                                  "--provider", "ollama", "--rocm",
                                  "--trust-download", started=False).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"
            run("plugin", "ai", operation, "--provider", "ollama", "--rocm",
                "--trust-download", success=False, started=False)
        for operation in ("use", "remove", "uninstall", "integrate", "status"):
            run("--yes", "plugin", "ai", operation, "--provider", "ollama",
                "--rocm", success=False)
        for provider in ("picoclaw", "codex-cli", "hermes-desktop",
                         "hermes-desktop-system", "hermes-agent"):
            run("--yes", "plugin", "ai", "install", "--provider", provider,
                "--rocm", "--trust-download", success=False)
        for flags in (("--rocm=true",), ("--rocm", "--rocm"),
                      ("--rocm", "--version", "../outside"),
                      ("--rocm", "--accept-system-packages"),
                      ("--rocm", "--accept-dependency-execution")):
            run("--yes", "plugin", "ai", "install", "--provider", "ollama",
                "--trust-download", *flags, success=False)
        run("--yes", "plugin", "ai", "install", "--rocm", "--trust-download",
            success=False)
        system = "hermes-desktop-system"
        submenu = run("--yes", "plugin", "ai", "menu", system)
        assert json.loads(submenu.stdout.splitlines()[-1])["choices"], "system review menu empty"
        for flags in ((), ("--trust-download",), ("--accept-system-packages",)):
            run("--yes", "plugin", "ai", "install", "--provider", system, *flags, success=False)
        for operation in ("update", "use", "remove", "uninstall"):
            run("--yes", "plugin", "ai", operation, "--provider", system, success=False)
        for version in ("../../outside", "1.2.3;touch /tmp/not-allowed"):
            run("--yes", "plugin", "ai", "install", "--provider", system,
                "--trust-download", "--accept-system-packages", "--version", version, success=False)
        run("--yes", "plugin", "ai", "install", "--provider", system,
            "--trust-download", "--accept-system-packages", "--accept-system-packages", success=False)
        run("--yes", "plugin", "ai", "install", "--provider", "picoclaw",
            "--trust-download", "--accept-system-packages", success=False)
        plan = json.loads(run("--dry-run", "plugin", "ai", "install", "--provider", system,
                             "--trust-download", "--accept-system-packages", started=False).stdout)
        assert plan["dry_run"] and plan["executor"] == "native"
        desktop = "codex-desktop-system"
        desktop_menu = run("--yes", "plugin", "ai", "menu", desktop)
        desktop_choices = json.loads(desktop_menu.stdout.splitlines()[-1])["choices"]
        assert desktop_choices, "native desktop review menu missing"
        assert all(c["operation"] in ("menu", "install", "update", "status") for c in desktop_choices)
        for c in desktop_choices:
            if c["operation"] in ("install", "update"):
                assert {"--trust-download", "--accept-system-packages"} <= set(c["arguments"])
        for operation in ("use", "remove", "uninstall", "integrate"):
            run("--yes", "plugin", "ai", operation, "--provider", desktop, success=False)
        for operation in ("install", "update"):
            for flags in ((), ("--trust-download",), ("--accept-system-packages",)):
                run("--yes", "plugin", "ai", operation, "--provider", desktop, *flags, success=False)
            for flags in (("--version", "1.2.3"), ("--version", "../../outside"),
                          ("--rocm",), ("--accept-dependency-execution",),
                          ("--accept-system-packages",), ("--trust-download=true",)):
                run("--yes", "plugin", "ai", operation, "--provider", desktop,
                    "--trust-download", "--accept-system-packages", *flags, success=False)
            run("plugin", "ai", operation, "--provider", desktop,
                "--trust-download", "--accept-system-packages", success=False, started=False)
            plan = json.loads(run("--dry-run", "plugin", "ai", operation, "--provider", desktop,
                                 "--trust-download", "--accept-system-packages", started=False).stdout)
            assert plan["dry_run"] and plan["executor"] == "native"
        for provider in ("picoclaw", "codex-cli", "ollama", "hermes-agent", system):
            run("--yes", "plugin", "ai", "update", "--provider", provider,
                "--trust-download", "--accept-system-packages", success=False)
        agent = "hermes-agent"
        agent_menu = run("--yes", "plugin", "ai", "menu", agent)
        choices = json.loads(agent_menu.stdout.splitlines()[-1])["choices"]
        assert choices and all("--accept-dependency-execution" in c.get("arguments", [])
                               for c in choices if c["operation"] in ("install", "update"))
        assert not any(c["operation"] == "use" for c in choices), "unowned Hermes preparation offered for selection"
        for flags in ((), ("--trust-download",), ("--accept-dependency-execution",)):
            run("--yes", "plugin", "ai", "install", "--provider", agent, *flags, success=False)
        run("plugin", "ai", "install", "--provider", agent, "--trust-download",
            "--accept-dependency-execution", success=False, started=False)
        run("--dry-run", "plugin", "ai", "install", "--provider", agent,
            "--trust-download", "--accept-dependency-execution", started=False)
        for operation in ("use", "remove", "uninstall"):
            run("--yes", "plugin", "ai", operation, "--provider", agent, "--version", "0.22.5", success=False)
        # Selection is local-only, exact, and still needs CORE confirmation.
        run("plugin", "ai", "use", "--provider", agent, "--version", "0.22.5",
            success=False, started=False)
        plan = json.loads(run("--dry-run", "plugin", "ai", "use", "--provider", agent,
                              "--version", "0.22.5", started=False).stdout)
        assert plan["dry_run"] and plan["executor"] == "native"
        for flags in ((), ("--version", "latest"), ("--version", "../../outside"),
                      ("--version", "0.22.5", "--version", "0.22.5"),
                      ("--version", "0.22.5", "--trust-download"),
                      ("--version", "0.22.5", "--accept-dependency-execution"),
                      ("--version", "0.22.5", "--accept-system-packages")):
            run("--yes", "plugin", "ai", "use", "--provider", agent, *flags, success=False)
        run("--yes", "plugin", "ai", "update", "--provider", agent,
            "--trust-download", "--accept-dependency-execution", success=False)
        for provider in ("picoclaw", "codex-cli", "ollama", system):
            run("--yes", "plugin", "ai", "install", "--provider", provider,
                "--trust-download", "--accept-dependency-execution", success=False)
        for flags in (("--accept-dependency-execution=true",),
                      ("--accept-dependency-execution", "--accept-dependency-execution"),
                      ("--accept-dependency-execution", "--version", "../../outside")):
            run("--yes", "plugin", "ai", "install", "--provider", agent, "--trust-download", *flags, success=False)
        for pending in ("codex-desktop",):
            run("--yes", "plugin", "ai", "menu", pending, success=False)
            run("--yes", "plugin", "ai", "install", "--provider", pending,
                "--trust-download", success=False)
        run("plugin", "ai", "install", "--provider", "ollama", "--trust-download",
            success=False, started=False)
        run("--yes", "plugin", "ai", "install", "--provider", "ollama",
            "--trust-download", "--accept-system-packages", success=False)
        run("--dry-run", "plugin", "ai", "install", "--provider", "ollama",
            "--trust-download", started=False)
        for provider in ("picoclaw", "codex-cli", "ollama", agent):
            run("--yes", "plugin", "ai", "integrate", "--provider", provider, success=False)
            run("plugin", "ai", "integrate", "--provider", provider, started=False, success=False)
            for flags in (("--version", "0.1.0"), ("--trust-download",),
                          ("--accept-system-packages",), ("--provider", provider)):
                run("--yes", "plugin", "ai", "integrate", "--provider", provider,
                    *flags, success=False)
        run("--dry-run", "plugin", "ai", "integrate", "--provider", agent, started=False)
        run("--dry-run", "plugin", "ai", "integrate", "--provider", agent,
            "--remove-path", started=False)
        run("--dry-run", "plugin", "ai", "integrate", "--provider", agent,
            "--refresh-launcher", started=False)
        run("plugin", "ai", "integrate", "--provider", agent,
            "--refresh-launcher", started=False, success=False)
        run("--yes", "plugin", "ai", "integrate", "--provider", agent,
            "--refresh-launcher", success=False)
        for flags in (("--remove-path",), ("--refresh-launcher",),
                      ("--version", "0.22.5"), ("--trust-download",),
                      ("--accept-dependency-execution",), ("--accept-system-packages",)):
            run("--yes", "plugin", "ai", "integrate", "--provider", agent,
                "--refresh-launcher", *flags, success=False)
        for provider in ("picoclaw", "codex-cli", "ollama", "hermes-desktop", system):
            run("--yes", "plugin", "ai", "integrate", "--provider", provider,
                "--refresh-launcher", success=False)
        run("--yes", "plugin", "ai", "integrate", "--refresh-launcher", success=False)
        run("--yes", "plugin", "ai", "integrate", "--provider", agent,
            "--refresh-launcher=true", success=False)
        for operation in ("install", "update", "use", "remove", "uninstall"):
            run("--yes", "plugin", "ai", operation, "--provider", agent,
                "--refresh-launcher", success=False)
        for provider in ("hermes-desktop", system, "codex-desktop"):
            run("--yes", "plugin", "ai", "integrate", "--provider", provider, success=False)
        run("--dry-run", "plugin", "ai", "uninstall", "--provider", agent,
            "--keep-preparation", started=False)
        run("plugin", "ai", "uninstall", "--provider", agent,
            "--keep-preparation", started=False, success=False)
        run("--yes", "plugin", "ai", "uninstall", "--provider", agent,
            "--keep-preparation", success=False)
        run("--yes", "plugin", "ai", "uninstall", "--provider", agent, success=False)
        for flags in (("--keep-preparation",), ("--remove-path",), ("--refresh-launcher",),
                      ("--version", "0.22.5"), ("--trust-download",),
                      ("--accept-dependency-execution",), ("--accept-system-packages",)):
            run("--yes", "plugin", "ai", "uninstall", "--provider", agent,
                "--keep-preparation", *flags, success=False)
        for provider in ("picoclaw", "codex-cli", "ollama", "hermes-desktop", system):
            run("--yes", "plugin", "ai", "uninstall", "--provider", provider,
                "--keep-preparation", success=False)
        run("--yes", "plugin", "ai", "uninstall", "--keep-preparation", success=False)
        run("--yes", "plugin", "ai", "uninstall", "--provider", agent,
            "--keep-preparation=true", success=False)
        for operation in ("install", "update", "use", "remove", "integrate"):
            run("--yes", "plugin", "ai", operation, "--provider", agent,
                "--keep-preparation", success=False)
        run("install", "ai", success=False, started=False)
        run("--yes", "plugin", "ai", "status", "--unexpected", success=False)
        run("--yes", "plugin", "ai", "menu", "unknown-view", success=False)
        for operation in ("install", "update", "remove", "uninstall"):
            if operation in manifest["operations"]:
                run("--yes", "plugin", "ai", operation, "--unexpected", success=False)
        for operation in manifest["operations"]:
            result = run("--dry-run", "plugin", "ai", operation, started=False)
            plan = json.loads(result.stdout)
            assert plan["dry_run"] and plan["executor"] == "native"

        # Seed receipt-owned ELF fixtures, never downloaded or run. This tests the
        # compiled selection/removal paths independently of HTTP/install unit tests.
        owned = root / "apps/ai/picoclaw/native-v1"
        owned.mkdir(mode=0o700)
        versions = owned / "versions"
        versions.mkdir(mode=0o700)

        def state_file(path, value):
            path.write_text(json.dumps(value) + "\n")
            path.chmod(0o600)

        state_file(owned / "owner.json", {"Schema": 1, "Plugin": "ai", "Provider": "picoclaw"})
        payload = Path(backend).read_bytes()
        digest = hashlib.sha256(payload).hexdigest()
        for version in ("0.1.0", "0.2.0"):
            directory = versions / version
            directory.mkdir(mode=0o700)
            tool = directory / "tool"
            tool.write_bytes(payload)
            tool.chmod(0o755)
            arch = host_target().split("/")[1]
            asset_arch = "x86_64" if arch == "amd64" else "arm64"
            state_file(directory / "receipt.json", {
                "Schema": 1, "Plugin": "ai", "BinarySHA256": digest, "BinarySize": len(payload),
                "Artifact": {"Provider": "picoclaw", "Version": version, "Arch": arch,
                             "URL": f"https://github.com/sipeed/picoclaw/releases/download/v{version}/picoclaw_Linux_{asset_arch}.tar.gz",
                             "SHA256": "a" * 64, "Format": "tar.gz", "Entry": "picoclaw", "Size": 100},
            })
        active = owned / "current"
        active.symlink_to("versions/0.1.0/tool")

        codex_owned = root / "apps/ai/codex/cli/native-v1"
        codex_owned.mkdir(mode=0o700)
        codex_version = codex_owned / "versions/0.1.0"
        codex_version.mkdir(mode=0o700, parents=True)
        codex_version.parent.chmod(0o700)
        state_file(codex_owned / "owner.json", {"Schema": 1, "Plugin": "ai", "Provider": "codex-cli"})
        (codex_version / "tool").write_bytes(payload)
        (codex_version / "tool").chmod(0o755)
        codex_arch = "x86_64" if arch == "amd64" else "aarch64"
        codex_entry = f"codex-{codex_arch}-unknown-linux-musl"
        state_file(codex_version / "receipt.json", {
            "Schema": 1, "Plugin": "ai", "BinarySHA256": digest, "BinarySize": len(payload),
            "Artifact": {"Provider": "codex-cli", "Version": "0.1.0", "Arch": arch,
                         "URL": f"https://github.com/openai/codex/releases/download/rust-v0.1.0/{codex_entry}.tar.gz",
                         "SHA256": "a" * 64, "Format": "tar.gz", "Entry": codex_entry, "Size": 100},
        })
        (codex_owned / "current").symlink_to("versions/0.1.0/tool")
        run("status", "ai")
        alias = root / "user/.local/bin/picoclaw"
        saved_candidate = alias.read_bytes()
        alias.unlink()
        alias.symlink_to(active)
        aliased = messages(run("status", "ai"))
        alias_lines = [line for line in aliased.splitlines() if str(alias) in line]
        assert len(alias_lines) == 1 and "native-preview alias" in alias_lines[0], alias_lines
        alias.unlink()
        alias.write_bytes(saved_candidate)
        alias.chmod(0o700)

        profile_modes = {".profile": 0o640, ".bashrc": 0o600, ".zshrc": 0o644}
        originals = {}
        for name, mode in profile_modes.items():
            path = root / "user" / name
            path.chmod(mode)
            originals[name] = path.read_bytes()
        path_writes = ("apps/ai/picoclaw/native-v1", "apps/ai/codex/cli/native-v1",
                       "user/.profile", "user/.bashrc", "user/.zshrc")
        zsh = root / "user/.zshrc"
        external_profile = root / "external-profile"
        external_profile.write_text("# Must not be read or changed\n")
        external_profile.chmod(0o600)
        opaque_paths.append(external_profile)
        zsh.unlink()
        zsh.symlink_to(external_profile)
        run("--yes", "plugin", "ai", "integrate", "--provider", "picoclaw", success=False)
        zsh.unlink()
        zsh.write_bytes(originals[".zshrc"])
        zsh.chmod(profile_modes[".zshrc"])
        hardlink = root / "profile-hardlink"
        os.link(root / "user/.profile", hardlink)
        run("--yes", "plugin", "ai", "integrate", "--provider", "picoclaw", success=False)
        hardlink.unlink()

        # CORE's generic integration route calls the backend with no arguments.
        core_plan = json.loads(run("--dry-run", "path", "apply", "--plugins", started=False).stdout)
        assert core_plan["dry_run"] and core_plan["executor"] == "native-core"
        run("--yes", "path", "apply", "--plugins", read_only=False, write_roots=path_writes)
        for native, command in ((owned, "picoclaw"), (codex_owned, "codex")):
            assert (native / "bin" / command).readlink().as_posix() == "../current"
            assert (native / "path.json").stat().st_mode & 0o777 == 0o600
        integrated = {}
        for name, mode in profile_modes.items():
            path = root / "user" / name
            data = path.read_bytes()
            integrated[name] = data
            assert data.startswith(originals[name]), "unrelated profile content changed"
            assert path.stat().st_mode & 0o777 == mode, "profile permissions changed"
            for provider, native in (("picoclaw", owned), ("codex-cli", codex_owned)):
                assert data.count(f"# >>> ATM native AI {provider} PATH >>>".encode()) == 1
                quoted = "'" + str(native / "bin").replace("'", "'\"'\"'") + "'"
                assert ("export PATH=" + quoted + '${PATH:+":$PATH"}').encode() in data
        run("--yes", "plugin", "ai", "integrate")
        run("status", "ai")

        bash_profile = root / "user/.bashrc"
        bash_profile.write_bytes(integrated[".bashrc"].replace(b"export PATH=", b"# manually edited PATH=", 1))
        run("--yes", "plugin", "ai", "integrate", "--provider", "picoclaw", "--remove-path", success=False)
        run("--yes", "plugin", "ai", "uninstall", "--provider", "picoclaw", success=False)
        assert active.exists(), "edited profile allowed destructive uninstall"
        bash_profile.write_bytes(integrated[".bashrc"])
        run("--yes", "plugin", "ai", "integrate", "--provider", "picoclaw", "--remove-path",
            read_only=False, write_roots=path_writes)
        assert not (owned / "bin/picoclaw").is_symlink()
        assert not (owned / "path.json").exists()
        assert (codex_owned / "bin/codex").exists()
        run("--yes", "plugin", "ai", "integrate", "--provider", "picoclaw",
            read_only=False, write_roots=path_writes)
        before_switch = {name: (root / "user" / name).read_bytes() for name in profile_modes}
        run("--yes", "plugin", "ai", "use", "--provider", "picoclaw", "--version", "0.2.0", read_only=False)
        assert active.readlink().as_posix() == "versions/0.2.0/tool"
        assert (owned / "bin/picoclaw").resolve() == (versions / "0.2.0/tool")
        assert all((root / "user" / name).read_bytes() == data for name, data in before_switch.items())
        run("--yes", "plugin", "ai", "remove", "--provider", "picoclaw", "--version", "0.2.0", success=False)
        inactive = versions / "0.1.0/tool"
        inactive.write_bytes(payload[:-1] + bytes([payload[-1] ^ 1]))
        run("--yes", "plugin", "ai", "use", "--provider", "picoclaw", "--version", "0.1.0", success=False)
        run("--yes", "plugin", "ai", "uninstall", "--provider", "picoclaw", success=False)
        inactive.write_bytes(payload)
        foreign = owned / "manual-notes"
        foreign.write_text("unknown files must block removal\n")
        run("--yes", "plugin", "ai", "uninstall", "--provider", "picoclaw", success=False)
        foreign.unlink()
        run("--yes", "plugin", "ai", "remove", "--provider", "picoclaw", "--version", "0.1.0", read_only=False)
        assert not (versions / "0.1.0").exists() and active.exists()
        run("--yes", "plugin", "ai", "uninstall", "--provider", "picoclaw", read_only=False,
            write_roots=path_writes)
        assert not active.is_symlink() and not list(versions.iterdir())
        run("--yes", "plugin", "ai", "uninstall", "--provider", "codex-cli", read_only=False,
            write_roots=path_writes)
        for name, mode in profile_modes.items():
            path = root / "user" / name
            assert path.read_bytes() == originals[name], "uninstall failed to preserve original profile"
            assert path.stat().st_mode & 0o777 == mode

        # Independent full-tree receipt fixtures exercise compiled Ollama
        # selection/removal without downloading or running a provider.
        ollama = root / "apps/ai/ollama/native-v1"
        ollama.mkdir(parents=True, mode=0o700)
        ollama.parent.chmod(0o700)
        (ollama / "versions").mkdir(mode=0o700)
        state_file(ollama / "owner.json", {"Schema": 1, "Plugin": "ai", "Provider": "ollama"})
        library = b"synthetic shared library\n"
        for version in ("0.1.0", "0.2.0"):
            version_dir = ollama / "versions" / version
            tree = version_dir / "tree"
            for relative in ("bin", "lib/ollama"):
                (tree / relative).mkdir(parents=True, mode=0o700)
            for directory in (version_dir, tree, tree / "lib"):
                directory.chmod(0o700)
            (tree / "bin/ollama").write_bytes(payload)
            (tree / "bin/ollama").chmod(0o755)
            (tree / "lib/ollama/libfixture.so.1").write_bytes(library)
            (tree / "lib/ollama/libfixture.so.1").chmod(0o600)
            (tree / "lib/ollama/libfixture.so").symlink_to("libfixture.so.1")
            state_file(version_dir / "receipt.json", {
                "Schema": 1, "Plugin": "ai",
                "Artifact": {"Provider": "ollama", "Version": version, "Arch": arch,
                             "URL": f"https://github.com/ollama/ollama/releases/download/v{version}/ollama-linux-{arch}.tar.zst",
                             "SHA256": "a" * 64, "Format": "tar.zst", "Entry": "bin/ollama", "Size": 100},
                "Files": {
                    "bin": {"mode": (1 << 31) | 0o700},
                    "bin/ollama": {"mode": 0o755, "size": len(payload), "sha256": digest},
                    "lib": {"mode": (1 << 31) | 0o700},
                    "lib/ollama": {"mode": (1 << 31) | 0o700},
                    "lib/ollama/libfixture.so.1": {"mode": 0o600, "size": len(library),
                                                  "sha256": hashlib.sha256(library).hexdigest()},
                    "lib/ollama/libfixture.so": {"mode": (1 << 27) | 0o777, "link": "libfixture.so.1"},
                },
            })
        # Exercise a mixed base/schema-2 store through the real compiled backend.
        # This fixture is not a provider binary and is never executed.
        if arch == "amd64":
            rocm_version = ollama / "versions/0.2.0"
            rocm_dir = rocm_version / "tree/lib/ollama/rocm_v7"
            rocm_dir.mkdir(mode=0o700)
            rocm_data = b"synthetic ROCm shared library\n"
            (rocm_dir / "libhip.so.7").write_bytes(rocm_data)
            (rocm_dir / "libhip.so.7").chmod(0o600)
            (rocm_dir / "libhip.so").symlink_to("libhip.so.7")
            rocm_receipt = json.loads((rocm_version / "receipt.json").read_bytes())
            rocm_receipt["Schema"] = 2
            rocm_receipt["ROCm"] = {
                "Provider": "ollama", "Version": "0.2.0", "Arch": arch,
                "URL": "https://github.com/ollama/ollama/releases/download/v0.2.0/ollama-linux-amd64-rocm.tar.zst",
                "SHA256": "b" * 64, "Format": "tar.zst", "Entry": "lib/ollama", "Size": 100,
            }
            rocm_receipt["Files"].update({
                "lib/ollama/rocm_v7": {"mode": (1 << 31) | 0o700},
                "lib/ollama/rocm_v7/libhip.so.7": {"mode": 0o600, "size": len(rocm_data),
                                                 "sha256": hashlib.sha256(rocm_data).hexdigest()},
                "lib/ollama/rocm_v7/libhip.so": {"mode": (1 << 27) | 0o777, "link": "libhip.so.7"},
            })
            state_file(rocm_version / "receipt.json", rocm_receipt)
            assert "base + ROCm" in messages(run("status", "ai"))
            for field, value in (("Version", "0.1.0"), ("Arch", "arm64"), ("SHA256", "invalid")):
                modified = json.loads(json.dumps(rocm_receipt))
                modified["ROCm"][field] = value
                state_file(rocm_version / "receipt.json", modified)
                run("--yes", "plugin", "ai", "use", "--provider", "ollama",
                    "--version", "0.2.0", success=False)
            state_file(rocm_version / "receipt.json", rocm_receipt)
        ollama_current = ollama / "current"
        ollama_current.symlink_to("versions/0.1.0/tree")
        run("status", "ai")
        external_ollama = root / "user/.local/bin/ollama"
        original_candidate = external_ollama.read_bytes()
        external_ollama.unlink()
        external_ollama.symlink_to(ollama_current / "bin/ollama")
        lines = messages(run("status", "ai")).splitlines()
        assert any(str(external_ollama) in line and "native-preview alias" in line for line in lines)
        external_ollama.unlink()
        external_ollama.write_bytes(original_candidate)
        external_ollama.chmod(0o700)
        submenu = run("--yes", "plugin", "ai", "menu", "ollama")
        assert "use-0.2.0" in {c["id"] for c in json.loads(submenu.stdout.splitlines()[-1])["choices"]}
        ollama_writes = ("apps/ai/ollama/native-v1", "user/.profile", "user/.bashrc", "user/.zshrc")
        run("--dry-run", "plugin", "ai", "integrate", "--provider", "ollama", started=False)
        run("--yes", "path", "apply", "--plugins", read_only=False, write_roots=ollama_writes)
        command = ollama / "bin/ollama"
        assert command.readlink().as_posix() == "../current/bin/ollama"
        assert command.resolve() == ollama / "versions/0.1.0/tree/bin/ollama"
        assert (ollama / "path.json").stat().st_mode & 0o777 == 0o600
        ollama_profiles = {}
        for name, mode in profile_modes.items():
            path = root / "user" / name
            data = path.read_bytes()
            ollama_profiles[name] = data
            assert data.startswith(originals[name]) and path.stat().st_mode & 0o777 == mode
            assert data.count(b"# >>> ATM native AI ollama PATH >>>") == 1
            quoted = "'" + str(ollama / "bin").replace("'", "'\"'\"'") + "'"
            assert ("export PATH=" + quoted + '${PATH:+":$PATH"}').encode() in data
        run("--yes", "plugin", "ai", "integrate", "--provider", "ollama")
        run("status", "ai")
        submenu = run("--yes", "plugin", "ai", "menu", "ollama")
        assert {"integrate", "remove-path"} <= {c["id"] for c in json.loads(submenu.stdout.splitlines()[-1])["choices"]}
        bash_profile.write_bytes(ollama_profiles[".bashrc"].replace(b"export PATH=", b"# edited PATH=", 1))
        run("--yes", "plugin", "ai", "uninstall", "--provider", "ollama", success=False)
        run("--yes", "plugin", "ai", "integrate", "--provider", "ollama", "--remove-path", success=False)
        assert ollama_current.readlink().as_posix() == "versions/0.1.0/tree"
        bash_profile.write_bytes(ollama_profiles[".bashrc"])
        command.unlink()
        command.symlink_to("../current")
        run("--yes", "plugin", "ai", "uninstall", "--provider", "ollama", success=False)
        command.unlink()
        command.symlink_to("../current/bin/ollama")
        run("--yes", "plugin", "ai", "integrate", "--provider", "ollama", "--remove-path",
            read_only=False, write_roots=ollama_writes)
        assert not command.is_symlink() and not (ollama / "path.json").exists()
        assert ollama_current.exists(), "detach removed the owned Ollama bundle"
        for name in profile_modes:
            assert (root / "user" / name).read_bytes() == originals[name]
        run("--yes", "plugin", "ai", "integrate", "--provider", "ollama",
            read_only=False, write_roots=ollama_writes)
        run("--yes", "plugin", "ai", "use", "--provider", "ollama", "--version", "0.2.0",
            read_only=False, write_roots=("apps/ai/ollama/native-v1",))
        assert ollama_current.readlink().as_posix() == "versions/0.2.0/tree"
        assert command.resolve() == ollama / "versions/0.2.0/tree/bin/ollama"
        assert all((root / "user" / name).read_bytes() == data for name, data in ollama_profiles.items())
        run("--yes", "plugin", "ai", "remove", "--provider", "ollama", "--version", "0.2.0", success=False)
        old_library = ollama / "versions/0.1.0/tree/lib/ollama/libfixture.so.1"
        old_library.write_bytes(b"modified library")
        run("--yes", "plugin", "ai", "use", "--provider", "ollama", "--version", "0.1.0", success=False)
        run("--yes", "plugin", "ai", "uninstall", "--provider", "ollama", success=False)
        old_library.write_bytes(library)
        unknown = ollama / "versions/0.1.0/tree/lib/ollama/personal-notes"
        unknown.write_text("keep this unknown file\n")
        unknown.chmod(0o600)
        run("--yes", "plugin", "ai", "uninstall", "--provider", "ollama", success=False)
        unknown.unlink()
        run("--yes", "plugin", "ai", "remove", "--provider", "ollama", "--version", "0.1.0",
            read_only=False, write_roots=("apps/ai/ollama/native-v1",))
        assert not (ollama / "versions/0.1.0").exists()
        run("--yes", "plugin", "ai", "uninstall", "--provider", "ollama",
            read_only=False, write_roots=ollama_writes)
        assert not command.is_symlink() and not (ollama / "path.json").exists()
        assert not ollama_current.is_symlink()
        assert not list((ollama / "versions").iterdir())
        for name, mode in profile_modes.items():
            path = root / "user" / name
            assert path.read_bytes() == originals[name] and path.stat().st_mode & 0o777 == mode
        run("status", "ai")
        print(f"PASS AI preview: {count} isolated CLI routes including owned selection/removal; no provider/shell/network execution")
        for flag in ("--internal-codex-apt-guard", "--internal-codex-apt-guard-early"):
            for extra in ([], ["/tmp/not-authority"]):
                before = snapshot(root)
                with tempfile.TemporaryDirectory(prefix="atm-ai-guard-trace-") as trace_dir:
                    trace = Path(trace_dir) / "trace"
                    result = subprocess.run(
                        [tracer, "-f", "-qq", "-s", "4096", "-e",
                         "trace=execve,execveat,connect", "-o", str(trace),
                         str(target), flag, *extra],
                        env=env, input="", capture_output=True, text=True, timeout=20)
                    assert result.returncode != 0, (flag, result.stdout, result.stderr)
                    verify_read_only_trace(trace.read_text(), target, None)
                assert snapshot(root) == before, "direct privileged guard wrote user state"
        print("PASS Codex APT guards: 4 direct-invocation refusals; no subprocess/network/state changes")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: check_ai_native.py CORE_BINARY AI_PREVIEW_BINARY")
    check(Path(sys.argv[1]).resolve(strict=True), Path(sys.argv[2]).resolve(strict=True))

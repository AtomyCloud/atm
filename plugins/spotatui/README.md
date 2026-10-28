# spotatui

ATM plugin `spotatui`, version `0.0.1`, order `15`, Full Setup disabled.
Official website: [spotatui.com](https://spotatui.com/).

## Native Backend

Compiled ATM uses a plugin-owned Go backend (schema 2 / JSONL-v1) on Linux
amd64 and arm64. It manages the terminal player, not the upstream desktop app.
No Bash, Python, curl, tar or remote installer implements the native management
operations. External playback dependencies can themselves use Python; running
them requires the explicit trust described below.

Root menu, status and version listing are offline/read-only. They never execute
an installed player, even with `--version`. External or legacy versions are
reported as unprobed rather than guessed. Opening the menu does not install,
probe dependencies, modify profiles or contact release services.

### Routes

Global ATM flags precede the command. Mutations require normal CORE confirmation
(or `--yes`); that confirmation does not replace dependency-execution trust.

```bash
atm plugin spotatui
atm status spotatui
atm plugin spotatui menu actions --location local
atm plugin spotatui menu versions
atm plugin spotatui menu dependencies
atm plugin spotatui menu dependencies --trust-dependencies
atm plugin spotatui menu usage --location local

atm install spotatui --location atm --trust-dependencies
atm install spotatui --location local --version 0.47.0 --trust-dependencies
atm update spotatui --location local --trust-dependencies
atm use spotatui 0.46.0 --location local --trust-dependencies
atm remove spotatui 0.46.0 --location local
atm uninstall spotatui --location local
atm plugin spotatui integrate --location atm
atm --dry-run install spotatui --location local --trust-dependencies
```

Operations are `menu/status/install/update/use/remove/uninstall/integrate`.
The root menu offers ATM/local action menus, versions, dependency inspection and
usage. Action menus include latest/custom install, update, use, remove, uninstall
and integration. Probe/install/use choices explicitly name dependency trust and
are still confirmed by CORE. Custom versions use the native input prompt.

Location defaults to `atm` per operation. Install/update default to `latest`;
`--version=X.Y.Z` and a leading `v` are accepted. Use/remove require a positional
numeric version. Update refuses downgrades; explicit install/use can select an
older version. Status takes no arguments and describes both locations.
The native backend does not evaluate legacy `plugin.conf` or shell expressions.

CORE dry-run does not start the backend. Direct JSONL `ATM_DRY_RUN=1` also returns
a plan without inspecting application paths, looking up dependencies, networking,
creating locks/files or spawning subprocesses. It is not an installation check.

### Locations And Ownership

Defaults:

```text
$ATM_APPS_DIR/spotatui/native-v1/
$HOME/.local/share/atm/spotatui/native-v1/
  <version>/
    libexec/spotatui
    bin/spotatui
    shims/yt-dlp
    .atm-dependencies.json
    .atm-spotatui-receipt.json
  current -> <version>
  .atm-path.json             (only after explicit PATH integration)
  .atm-local-entry.json      (local entry ownership, local location only)

$HOME/.local/bin/spotatui
  -> $HOME/.local/share/atm/spotatui/native-v1/current/bin/spotatui
```

Environment overrides `ATM_SPOTATUI_INSTALL_ROOT`, `ATM_SPOTATUI_LOCAL_ROOT`
and `ATM_SPOTATUI_LOCAL_ENTRY` remain supported. The first two are base roots;
native management appends `native-v1`. Roots must be clean, absolute, separate,
non-nested paths without symlinked ancestors or PATH separators. A custom local
entry filename such as `music` is supported. Legacy default-version/cache/manifest
settings are not imported; native defaults to latest and uses private staging.

Legacy version directories, current links and manifests outside `native-v1` are
not adopted, selected, moved or removed. Both origins may coexist. **A foreign
local entry, including a legacy launcher/link, is refused even with `--yes`.**
There is no native backup-and-replace flow. Move an external entry yourself or
choose another explicit local entry path before installing locally. Existing
unowned entries pointing at the expected native target are not silently adopted.

Installation selects a native version; local installation also publishes its
guarded command symlink. Neither location implicitly edits shell profiles.
Explicit `integrate` adds a location-specific owned PATH block to `.profile`,
`.bashrc`, and an existing `.zshrc`. It does not add desktop entries. Existing
unowned/edited blocks or symlinked profiles are refused. Other profile text and
permissions are preserved; terminals already open need a shell refresh.

Native receipts inventory bytes, modes and directories. Use/refresh/remove/
uninstall refuse missing, changed, linked or user-added payloads. Remove protects
the current version. Uninstall removes only unchanged native-owned versions,
the selected location's owned integration and local entry. Unknown root files,
legacy/external installations, dependencies, caches, credentials and player
configuration/listening data are retained. A changed foreign local entry blocks
local mutations rather than being deleted. Empty roots and newly created empty
profile files may remain.

### Release Verification

Only stable numeric releases from
[LargeModGames/spotatui](https://github.com/LargeModGames/spotatui/releases)
are accepted. The resolver checks exact official metadata/asset URLs, sizes and
matching `spotatui-linux-x86_64.tar.gz.sha256` or
`spotatui-linux-aarch64.tar.gz.sha256`. These contracts were checked against
official v0.47.0 metadata on 2026-10-28; no real player archive was installed or run.

Requests have time/size limits and restricted HTTPS release-storage redirects.
SHA-256 is mandatory before extraction. Every tar entry is checked; traversal,
duplicates, links, special files and special permissions are refused. Only the
root `spotatui` executable is published, after an ELF architecture/type check.
Downloads are bounded to 256 MiB, expansion to 1 GiB and 10,000 entries.
Checksums plus official HTTPS are the trust boundary, not publisher signatures.

Existing same-version artifacts must match the previously recorded source/hash.
Their dependency metadata is refreshed with guarded rollback instead of
redownloading/replacing the player. Use also rechecks dependencies and refreshes
their recorded absolute paths. Generated launchers are compiled Go executables,
not shell scripts. Updating ATM itself does not replace old installed launcher
copies automatically.

### Playback Dependencies And Trust

The plugin retains its YouTube-ready dependency gate even though upstream has
other playback sources that do not need it:

- Executable `ffmpeg` and `ffprobe` on an absolute PATH.
- `yt-dlp` version >=2025.11.12 and locally reported EJS libraries.
- Deno >=2.3.0 (preferred), otherwise Node >=22.0.0.

Dependency path discovery alone does not execute tools. Install/update/use and
actual diagnostics require `--trust-dependencies`: consent to run external
executables and their subprocesses, not a security sandbox. No dependency is
installed, upgraded or overwritten automatically; no sudo/package manager runs.

Install missing FFmpeg/ffprobe through your distribution's package manager and
provide a supported Node or Deno on PATH. For a pipx-managed extractor:

```bash
# First installation (requires pipx):
pipx install 'yt-dlp[default]'
# Upgrade an existing pipx installation instead:
pipx upgrade yt-dlp
hash -r
```

Choose the appropriate installation or upgrade command, then rerun the explicit
diagnostic action. These are manual remediation examples, never ATM side effects.

Diagnostics ignore yt-dlp configuration/plugins, disable cache and remote
components, set `PYTHONDONTWRITEBYTECODE=1`, supply no URL, and discard raw debug
output. Each probe has a 20-second deadline, a 64 KiB limit per output stream and
bounded pipe waiting. Runtime execution errors immediately abort the operation;
only absent or successfully probed unsuitable runtime versions permit fallback.
The selected runtime is checked in yt-dlp's no-URL verbose diagnostics.

**Use the backend through compiled ATM.** Probes inherit CORE's process group;
CORE terminates descendants on cancellation and normal backend exit. Direct
standalone backend/probe invocation has no descendant-cleanup guarantee.
Tools deliberately escaping the process group are outside this containment.
External trusted tools could ignore requested no-network/no-write options;
this is not an OS-enforced network/filesystem sandbox.

The native launcher prepends its shim and FFmpeg/ffprobe directories only to the
player child's PATH. The shim forwards literal arguments with
`--no-js-runtimes --js-runtimes <runtime>:<absolute-path>`, clearing yt-dlp's
default Deno selection first. Normal playback keeps user yt-dlp config/plugins,
unlike diagnostics. Arguments, stdin, stdout/stderr and exit codes are preserved.
Explicit player `behavior.ytdlp_path` and later user runtime flags can override
this behavior. Configuration is never rewritten.

EJS diagnostic presence is **not** proof of solver compatibility, YouTube
reachability, successful playback, audio-device availability or account access.
Keep yt-dlp and its matching EJS package updated. Upstream contracts:
[spotatui YouTube routing](https://github.com/LargeModGames/spotatui/blob/main/src/infra/youtube/dispatch.rs),
[yt-dlp EJS requirements](https://github.com/yt-dlp/yt-dlp/wiki/EJS),
[yt-dlp diagnostic/runtime options](https://github.com/yt-dlp/yt-dlp/blob/master/yt_dlp/options.py).

### Limitations

Human playback acceptance and Linux ARM64 runtime acceptance remain pending.
Windows/macOS backends are not implemented. Tests use synthetic native processes
and temporary roots, never real player/media/accounts or system changes.
Host dynamic-library/glibc/audio compatibility is not established by ELF checks.
UI labels currently use embedded en-us; translations remain pending.

No automatic legacy adoption, foreign-entry replacement, desktop installation,
process termination for player upgrades, or player self-update management.
Avoid the player's self-updater for native-owned versions: changed bytes block
later management. ATM does not change the player's auto-update configuration.

Private staging and an exclusive per-location lock protect normal operations.
A crash can leave a stale lock, published inactive version or incomplete refresh/
removal; manual inspection/recovery is required. Published versions survive
activation failure. Dependency metadata refresh uses two guarded file replacements,
not a power-loss-atomic transaction. Uninstall may partially complete on I/O
failure/cancellation. Profiles outside owned blocks are preserved, but concurrent
same-user edits and hostile same-user filesystem races are not fully transactional.

## Legacy Bash Backend

The original `plugin.sh`, `plugin.conf`, `plugin.metadata`, `release.py`,
locale and legacy tests remain unchanged for the separate Bash ATM entrypoint.
Its Python/curl requirements, shell launchers and old layout still apply.
Unlike native status, legacy status may run a foreign local player's `--version`.
Unlike native local installation, legacy replacement offers explicit confirmation
and retains a sibling backup. Those behaviors are not silently carried into the
native backend. Use the legacy entrypoint to manage old installations separately;
native ownership receipts do not grant the legacy implementation ownership.

## Verification

```bash
go test ./plugins/spotatui/native
go test -race ./plugins/spotatui/native
go vet ./plugins/spotatui/native
bash -n plugins/spotatui/plugin.sh plugins/spotatui/plugin.conf plugins/spotatui/plugin.metadata plugins/spotatui/lang/en-us.lang
python3 -B -m unittest discover -s plugins/spotatui/tests -p 'test_*.py'
```

Focused fixtures cover TLS metadata/download failures, archive/ownership refusals,
both locations, profile/private-data preservation, refresh rollback, late foreign
local entries, dry-run, explicit trust and native JSONL menus. Real compiled
synthetic processes cover absolute/current/local/custom-name launcher dispatch,
direct shim use, literal argv/streams/exit status, scoped environment, recursion,
and CORE-supervised probe cancellation/failure cleanup. Loopback TLS listeners
are required for these tests. They do not establish real application acceptance.

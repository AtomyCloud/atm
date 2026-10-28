# ATM 0.0.2: Go Migration

Status: **Native CORE migration complete for Linux amd64; accepted and Stable.**
The maintainer confirmed the final binary test passed on 2026-10-27. This closes
the Linux-first milestone, not other-platform acceptance or release publication.
The Go entrypoint uses native administrative services, not a Bash CORE fallback.
The legacy entrypoint and Linux application compatibility remain intentionally
available; this is not a completed migration of application plugin backends.
The native executable reports `0.0.2`. Existing Bash CORE and application
plugins retain `0.0.1`; no installed applications are migrated automatically.

## Current Completion Priority

The Linux-first CORE milestone is complete. Linux amd64 is the validated runtime
reference, not a claim that every Linux architecture or platform has been accepted.

| Runtime target | Acceptance status |
| --- | --- |
| Linux amd64 | Complete / Stable: native implementation, automated evidence and maintainer-confirmed final binary test. |
| Linux arm64 | Pending: package build and signature verification do not establish runtime acceptance. |
| Windows amd64 | Pending: runtime, file-in-use replacement and standard-user acceptance deferred. |
| macOS Intel (amd64) | Pending: target-host acceptance deferred. |
| macOS Apple Silicon (arm64) | Pending: target-host acceptance deferred. |

Existing cross-platform code, builds and CI remain in place. Deferred host tests
do not block the Linux amd64 milestone, but prevent claiming all-platform completion.
Automatic Windows external-helper handoff is separately **Pending implementation**.
See [Signed Linux Release Test](LINUX-SIGNED-RELEASE-TEST.md#two-version-acceptance)
for the automated upgrade evidence and private-artifact reproduction requirements.
Linux completion requires CLI/TUI CORE parity without Bash/Python delegation,
isolated signed upgrade/recovery evidence, regression tests and human acceptance
of the changed flow. Application plugin ports and release/publication approval
remain separate. No installed copy is updated automatically. Use the
[isolated Linux checklist](LINUX-CORE-ACCEPTANCE.md) for final human acceptance.

## Available Now

- Native Go executable with Bubble Tea v2, Bubbles and Lip Gloss.
- Mouse selection/scroll, arrow-key navigation, search, sorting, language
  selection, action confirmation and a time-based animated ASCII logo.
- Native, read-only plugin discovery, configuration import and diagnostics.
- Native official catalog search in the CLI and TUI, with pinned metadata,
  Git blob verification, filters and cancellation; no Bash/Python required.
  See [Native Plugin Catalog](https://github.com/AtomyCloud/atm/blob/main/docs/en-us/NATIVE-PLUGIN-CATALOG.md).
- Native plugin code downloads with confirmation, pinned files, private staging,
  Go-based Bash syntax checks, origin receipts and non-replacing publication.
  Restart ATM to load downloaded plugins; no application is installed by download.
- Declarative `plugin.json` contracts for all 16 existing plugins. Metadata-only
  fallback supports older official plugins without executing their shell files.
- Native component inspection, guarded plugin updates/backups/recovery, portable
  package setup, ATM user PATH integration and Full Setup orchestration. See
  [Native CORE Administration](https://github.com/AtomyCloud/atm/blob/main/docs/en-us/NATIVE-CORE-ADMINISTRATION.md) for scope and limits.
- Linux compatibility executor for existing application plugin operations only.
  Interactive processes temporarily own the terminal, including sudo
  prompts, then return to the TUI. The selected operation's exit status is retained.
- Native preferences in `atm-go.json`, separate from legacy configuration.
- External native executable contract (schema 2 / JSONL v1), real asynchronous
  progress/logs, confirmation, timeout and cancellation with process cleanup.
  Validated with an isolated fixture; production application ports remain pending.
- Internal native installation services: bounded HTTPS downloads, SHA-256,
  confined archive extraction, owned receipts, locks and recoverable activation.
  These are not wired to production plugin menus yet; see
  [Native Installation Services](https://github.com/AtomyCloud/atm/blob/main/docs/en-us/NATIVE-INSTALLATION-SERVICES.md).
- Linux amd64/arm64, macOS Intel/Apple Silicon and Windows amd64 packages with SHA-256 checksums and
  a `release.json` file mapping the build's source commit and payload hashes.

## Platform Support

| Capability | Linux | Windows |
| --- | --- | --- |
| Native CLI/TUI, discovery, preferences and locale selection | Implemented | Compiles; real Windows validation pending |
| Application install/remove/version switching | Native Node.js; other plugins use Bash compatibility | Node.js backend pending |
| Native external plugin executor and JSONL events | Fixture tested | Compiles; runtime validation pending |
| Official catalog search | Native Go | Native Go; real Windows validation pending |
| Official plugin code download | Native Go | Cross-compiled; real Windows validation pending |
| Plugin code inspection/update/recovery | Native Go | Native Go; real validation pending |
| Portable setup and ATM user PATH | Native Go | Native Go; real validation pending |
| Full Setup orchestration | Native Go; legacy apps explicitly bridged | Native plugins only; app ports pending |
| Compiled CORE self-update | Signed enrollment/update/recovery enabled with consent; actual newer-release acceptance pending | Enabled with external trusted executable; automated handoff and host acceptance pending |
| Native Node.js application backend | Implemented; Linux human acceptance pending | Pending |
| Native Go and VS Code application backends | Pending | Pending |

macOS now has `darwin/amd64` (Intel) and `darwin/arm64` (Apple Silicon) Terminal
executables, native process-group execution and CI test jobs for both architectures.
Local cross-builds are not macOS runtime approval. Existing Bash plugins remain
Linux-only on macOS too. This delivery does not create a Finder `.app`, DMG,
Developer ID signature or notarized release, and does not bypass Gatekeeper.

The frontend does not need Go installed. Linux application operations still
require Bash and each plugin's existing dependencies. The Windows frontend does
not invoke Bash, WSL or Python and does not request elevation. This does **not**
claim that the current application plugins can install Windows applications.

## Build And Run

Development requires Go 1.26 or newer. From the repository root:

```bash
go build -o /tmp/atm-v002/atm ./cmd/atm
/tmp/atm-v002/atm --home "$PWD" --version
/tmp/atm-v002/atm --home "$PWD" --dry-run tui
/tmp/atm-v002/atm --home "$PWD" --demo tui
```

`--demo` simulates confirmed TUI operations; `--dry-run` shows execution plans.
Neither mode runs plugins or persists preferences. Ordinary TUI mode executes
only explicitly selected and confirmed operations. It does not install anything
merely by listing plugins.

For a packaged distribution, extract into a new directory and run `./atm` on
Linux/macOS or `.\atm.exe` in PowerShell. Keep the bundled runtime directories beside
the executable. Do not overwrite an existing `~/Apps/atm` installation with this
migration build. The existing `bin/atm` remains the original Bash entrypoint.

```bash
./atm plugins list
./atm plugins list --json
./atm doctor
./atm plugin node
./atm --dry-run --json install node --version 24.16.0
./atm lang set pt-br
./atm plugins sort az
./atm --no-mouse --no-animation tui
```

Global flags go before the command; application arguments go after the plugin
ID. Legacy TUI operation output is shown in the temporarily restored terminal.
Press Enter after completion to return. Native operations stay inside the TUI,
with progress, bounded logs and cancellation. Session results retain outcomes, not full
subprocess output or passwords. Existing backend logging remains unchanged.

Keyboard navigation: arrows or j/k select; Enter opens/confirms; Escape returns;
`/` focuses search; p opens plugins; u opens CORE actions; l opens languages;
s opens sorting; h opens session results; q or Ctrl+C exits. Page Up/Down and
Home/End navigate long lists. The footer and visible list rows are clickable.
Confirmation defaults to Cancel. Very small terminals display a resize notice.

## Configuration And Compatibility

- Official catalog search/download is native on Linux/macOS/Windows. Search
  remains read-only; download requires consent before staging and publication.
  Neither operation executes plugins or installs their applications. Dry-run/demo
  skip network and writes. Existing component updates still use the separate
  Linux legacy backend. Downloaded legacy plugins remain Linux-only.
- Resolve ATM home from `--home`, then `ATM_HOME`, then beside the executable
  (or its parent when placed in `bin`). The current directory is not silently
  adopted, and home is never persisted as an application installation path.
- Import literal `ATM_LANG`, `ATM_PLUGIN_SORT_ORDER`, `ATM_APPS_DIR`, `ATM_STATE_DIR` and disabled
  plugin settings from legacy configuration. Shell expressions are rejected,
  never evaluated. Runtime plugins remain trusted executable code.
- Native preferences override imported values; explicit environment variables
  override native preferences. `ATM_CONFIG_DIR` can isolate testing.
- Native preferences live in the OS user configuration directory under `atm`,
  or `ATM_CONFIG_DIR`. Existing Bash files are not rewritten by native settings.
  When running a Bash operation, the native selection is passed in its environment.
- Existing locale files are parsed as literal assignments. New TUI messages
  currently fall back to embedded en-us JSON; translations remain pending.
- No application ownership, PATH entry, profile, desktop shortcut or active
  version changes occur until an applicable operation is explicitly executed.
- The bridge clears `BASH_ENV`/`ENV`, forwards an argument array (not `bash -c`),
  checks pending CORE recovery and preserves plugin failures.
- No administrative Go route invokes the legacy Bash CORE. The separate original
  `bin/atm` remains available for legacy administration and legacy backup recovery.
- Native checks distinguish installed plugin code from compiled CORE identity.
  CORE freshness remains unknown until a trusted release policy is operational;
  source commits are not authenticated binary releases. Plugin replacement refuses
  Git worktrees and unverified/modified baselines. No baseline is auto-adopted.

## Plugin Contract

See `docs/schema/plugin-v1.schema.json` in the source repository. Each plugin
directory owns its `plugin.json`; its ID must match the directory name. Existing
`plugin.metadata`, `plugin.conf` and `plugin.sh` remain available to Bash.

The existing application executor is `legacy-bash`, protocol `tty-v1`, Linux
only. Declared operations map to the existing menu/status/install/remove/
uninstall/use entrypoints. No arbitrary shell command is accepted in JSON.
The declaration of an operation does not change the underlying plugin's existing
requirements or add capabilities to its stubs.

Optional `full_setup: true` opts into Go-orchestrated Full Setup (default false).
Enabled, opted-in plugins run in metadata order, regardless of display sorting.
Unavailable entries and entries not run after a failure are reported explicitly.
Legacy metadata fallback reads the existing literal FULL_SETUP flag as data.

Unknown schemas, duplicate actions, mismatched IDs, symlink plugin directories
and unsupported executors fail validation. Invalid JSON never silently falls
back to shell metadata. Directory listing and catalog metadata are not executed.

Native schema 2 adds `executor: native`, `protocol: jsonl-v1` and an executable
map by OS/architecture. See [Native Plugin Protocol](https://github.com/AtomyCloud/atm/blob/main/docs/en-us/NATIVE-PLUGIN-PROTOCOL.md)
for event formats, bounds, confirmation, cancellation and the isolated fixture.
This leaves legacy schema/API 1 untouched. `--dry-run --json` emits one plan;
native execution with `--json` emits real JSONL events and a validated result.
CLI mutations require confirmation or `--yes`; the default timeout is 15 minutes
(`--timeout 5m` overrides it). Cancellation is process termination, not rollback.
Repository tests still enforce parity for all 16 legacy declarations.

## Local Packages

```bash
go run ./tools/package -os linux -arch amd64 -out /tmp/atm-packages
go run ./tools/package -os linux -arch arm64 -out /tmp/atm-packages
go run ./tools/package -os windows -arch amd64 -out /tmp/atm-packages
go run ./tools/package -os darwin -arch amd64 -out /tmp/atm-packages
go run ./tools/package -os darwin -arch arm64 -out /tmp/atm-packages
```

Existing output archives are never overwritten. Packages contain the executable,
runtime files, plugins, locales, license, this guide and payload hashes. They do
not include developer notes, Git history or user-level configuration. Build only
from a reviewed checkout: local changes under runtime directories are included
and builds from a dirty checkout are marked `-dirty`.

Checksums detect corruption; they are **not signatures or proof of origin**.
The maintainer-approved Ed25519 public key is embedded, and signed full-package
verification is available offline. Local Linux0.0.2 packages were signed and the
human dry-run test was approved. Patch v27.9 enables signed enrollment/update/recovery
in source with explicit consent. Existing signed artifacts keep their original
disabled gate; no real installation or published release was replaced. Actual
newer-version upgrade and target-host acceptance remain pending. CI runs
Linux/Windows/macOS tests. With the repository owner's explicit
approval, pushes and manual runs in `AtomyCloud/atm` also upload the five
platform packages to GitHub Actions Artifacts for 14 days. Pull requests and
fork repositories do not upload packages. There is no automatic public release
creation or installation. No workflow has been triggered by this local work.

## Validation

```bash
go test ./...
go test -race ./...
go vet ./...
bash -n libexec/atm-legacy.sh
go build -o /tmp/atm-v002/atm ./cmd/atm
go build -o /tmp/atm-v002/native-fixture ./tools/native-fixture
ATM_TEST_NATIVE_FIXTURE=/tmp/atm-v002/native-fixture python3 tests/test_go_tui.py /tmp/atm-v002/atm -v
```

The PTY suite uses temporary fixtures to validate mouse hit testing, search,
interactive handoff, native progress/cancellation, JSONL output, nonzero subprocess results and terminal restoration. It
does not run real installations. Cross-compilation alone does not validate
Windows terminal behavior, ARM64 execution or standard-user application installs.

## Remaining Migration Work

1. Human acceptance of the native Linux Node.js backend. Its menus, official
   archive download/extraction, ownership receipts, removal and PATH integration
   are implemented separately from the already accepted Linux CORE milestone.
2. Port other application plugins, beginning with Go and VS Code. Preserve
   plugin-owned application behavior and the generic native menu/integration contracts.
   Legacy backup import remains separate from native plugin code backups.
3. Implement/test Node.js on other hosts; Linux ARM64 runtime acceptance is pending.
4. Finish the Windows external updater helper and target-host acceptance. Linux
   signed full-package upgrade/recovery has already passed its separate acceptance;
   new signing/publication/deployment is not implied by application plugin work.
5. Validate on real Windows standard-user, macOS and ARM64 environments, finish
   translations and approve release publication before replacing legacy ATM.

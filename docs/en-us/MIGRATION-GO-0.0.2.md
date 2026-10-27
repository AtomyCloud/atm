# ATM 0.0.2: Go Migration

Status: **In progress, opt-in migration build.** This is the first working
delivery of the Go migration, not a completed replacement of the Bash CORE.
The native executable reports `0.0.2`. Existing Bash CORE and application
plugins retain `0.0.1`; no installed applications are migrated automatically.

## Available Now

- Native Go executable with Bubble Tea v2, Bubbles and Lip Gloss.
- Mouse selection/scroll, arrow-key navigation, search, sorting, language
  selection, action confirmation and a time-based animated ASCII logo.
- Native, read-only plugin discovery, configuration import and diagnostics.
- Declarative `plugin.json` contracts for all 16 existing plugins. Metadata-only
  fallback supports older official plugins without executing their shell files.
- Linux compatibility executor for existing plugin operations and selected CORE
  commands. Interactive processes temporarily own the terminal, including sudo
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
| Existing application install/remove/version switching | Bash compatibility | Unavailable |
| Native external plugin executor and JSONL events | Fixture tested | Compiles; runtime validation pending |
| Official catalog and plugin code updates | Existing Bash/Python backend | Pending native implementation |
| Compiled CORE self-update | Pending | Pending |
| Native Node, Go and VS Code application backends | Pending | Pending |

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

- Resolve ATM home from `--home`, then `ATM_HOME`, then beside the executable
  (or its parent when placed in `bin`). The current directory is not silently
  adopted, and home is never persisted as an application installation path.
- Import literal `ATM_LANG`, `ATM_PLUGIN_SORT_ORDER`, `ATM_APPS_DIR` and disabled
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
- The legacy CORE updater, whole-repository updater, setup and recovery commands
  are not exposed through Go: they cannot update a compiled migration distribution
  safely yet. Run the original `bin/atm` explicitly for legacy administration.
- Update checks refer to legacy source files and plugin code, not the compiled
  Go binary. Existing dirty-worktree and ownership guards still apply. A bundle's
  `release.json` is not a legacy updater receipt; no baseline is auto-adopted.

## Plugin Contract

See `docs/schema/plugin-v1.schema.json` in the source repository. Each plugin
directory owns its `plugin.json`; its ID must match the directory name. Existing
`plugin.metadata`, `plugin.conf` and `plugin.sh` remain available to Bash.

The existing application executor is `legacy-bash`, protocol `tty-v1`, Linux
only. Declared operations map to the existing menu/status/install/remove/
uninstall/use entrypoints. No arbitrary shell command is accepted in JSON.
The declaration of an operation does not change the underlying plugin's existing
requirements or add capabilities to its stubs.

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
Release signing, source-to-artifact verification and an updater trust root are
pending. CI runs Linux/Windows/macOS tests. With the repository owner's explicit
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

1. Integrate the native download/extraction/receipt/activation services with the
   first real plugin; add its required archive-link handling and managed removal.
2. Port the official catalog to Go and migrate application-specific interactive
   menus into native screens, preserving pinning and existing ownership guards.
3. Deliver native Node, then Go and VS Code backends with verified official
   artifacts, stable launchers, ownership checks and version switching.
4. Implement compiled CORE updates, signing/trust verification, file-in-use
   replacement, rollback and independent recovery, especially on Windows.
5. Validate on real Windows standard-user and ARM64 environments, finish
   translations and approve release publication before replacing legacy ATM.

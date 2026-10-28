# Native Plugin Protocol

Status: implemented in the opt-in ATM 0.0.2 migration. Linux fixture tests cover
execution and the terminal UI. Windows builds compile; Windows runtime and
standard-user acceptance remain pending. Node.js provides a native Go Linux
backend accepted by the maintainer, as does spotatui; the Go toolchain, Java/JDK, Flutter, Android
Studio, VS Code, Android SDK, Trivy, REA and OS Packages backends have passed
automated Linux checks, with human acceptance pending. Remaining application plugins retain
explicit Bash compatibility backends until individually migrated. The separate
legacy entrypoint is retained.

## Building Local Backends

Native source lives in `plugins/<id>/native`. The packager discovers schema-2
manifests, builds each declared executable for the selected target, and verifies
its architecture. A missing local native source fails the build. Unsupported
targets contain no native executable for that plugin and cannot fall back to Bash.
Packages exclude development sources, tests and prebuilt checkout binaries.

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
```

This creates ignored development binaries and checksums at the manifest paths.
`--target all` covers all five CORE build targets, skipping undeclared plugin
targets. The older `--node-local` option remains a Node-only compatibility alias.
The official source catalog does not compile plugins: use a complete platform
package or build the local source before execution.

## Manifest And Execution

External executable plugins use `schema: 2`, `min_core_api: 2`, `executor: native`
and `protocol: jsonl-v1`. This **does not change** the legacy `core-api.json`.
See [the schema](../schema/plugin-v2.schema.json) and the repository fixture at
`tests/fixtures/native_fixture/plugin.json`.

```json
{
  "schema": 2,
  "id": "example",
  "name": "Example",
  "version": "0.0.1",
  "description": "Native application management",
  "order": 1,
  "min_core_api": 2,
  "platforms": ["linux", "windows", "darwin"],
  "executor": "native",
  "protocol": "jsonl-v1",
  "operations": ["status", "install", "update", "uninstall"],
  "executables": {
    "linux/amd64": "bin/linux-amd64/example",
    "linux/arm64": "bin/linux-arm64/example",
    "windows/amd64": "bin/windows-amd64/example.exe",
    "darwin/amd64": "bin/darwin-amd64/example",
    "darwin/arm64": "bin/darwin-arm64/example"
  }
}
```

Each declared OS needs at least one executable target. Unsupported targets,
missing binaries and nonexecutable Linux files are unavailable before launch.
Entrypoints must be regular files inside the plugin directory: absolute paths,
traversal, symlink entrypoints/parents and Windows reserved names are refused.
Portable relative paths use `/` even on Windows. Spaces and Unicode are allowed.
ATM rechecks the manifest, executable path and ELF/PE/Mach-O architecture at execution.
Discovery reads metadata and file attributes; it never probes plugin commands.

The executable starts directly, without Bash, PowerShell, `cmd.exe`, command
string evaluation or elevation. Its working directory is the plugin directory;
the current user's environment is inherited. Request fields, not ambient
`ATM_*` variables, are authoritative for the selected ATM home/apps/language.
App-specific URLs, package selection and installation remain plugin-owned.

Operations are `menu`, `status`, `install`, `update`, `remove`, `uninstall`, `use`, and
optional `integrate`.

`integrate` owns application-specific user PATH/CLI/desktop integration. CORE
invokes it without arguments through `path apply --plugins` or the TUI, after
explicit confirmation, in metadata order. Disabled/unavailable/legacy plugins
are reported as unavailable; there is no fallback to shell integration. A backend
must report unsupported, failed or incomplete integration as a non-success result.
It must preserve unrelated user settings and avoid system-wide changes. Declaring
this operation is optional and does not mean existing application ports are done.
Each native operation accepts up to 128 literal arguments within the request cap.
The backend validates required versions, locations and other option semantics;
CORE does not impose the legacy bridge's positional argument counts on native
operations. Declared-operation, protocol, confirmation and execution guards remain.
`atm plugin ID` selects menu when declared, otherwise status or the first operation.
Native plugins never read interactive user prompts on stdin: it contains one request.
The optional `menu` operation accepts literal arguments for plugin-owned submenus.
Interactive `atm plugin ID menu` opens the generic TUI at confirmation; noninteractive
use requires `--yes` and displays choices (structured JSONL with `--json`).

### Structured Menus

A successful terminal result may include `choices`. These are displayed only after
the process has exited successfully and its complete stream has been validated:

```json
{"protocol":1,"request_id":"0123456789abcdef0123456789abcdef","type":"result","status":"success","message":"Versions","choices":[{"id":"custom","label":"Custom version","operation":"install","arguments":["--version"],"prompt":"Version"}]}
```

At most 100 choices are allowed. IDs must be unique/nonempty (64 bytes maximum),
labels nonblank (200 bytes), prompts optional (200 bytes), and arguments at most
127 strings of 4096 bytes each, without control characters. The 64 KiB frame limit
still applies. `prompt` collects one value, appended to the argument array. Each
choice goes through normal declared-operation checks and default-Cancel confirmation;
it cannot select a different plugin, run a shell command, or bypass dry-run. Backend
arguments must still be validated independently. Choices are reliable terminal data,
not droppable log/progress events. Failed/cancelled results cannot offer choices.

CORE builds predating this extension reject manifests declaring `menu` rather than
falling back to Bash. Install matching CORE/plugin packages together. Existing
non-menu JSONL-v1 plugins remain compatible; no global protocol version changed.

Node source checkouts require a local backend build; complete Linux ATM packages
include it. The official source catalog does not compile code at runtime. A remote
native plugin lacking its declared executable is rejected by download validation;
do not treat a source-only catalog entry as an installable binary distribution.

## Request And Events

One process handles one operation. ATM writes one UTF-8 JSON request, followed
by a newline, then closes stdin. It includes:

```json
{"protocol":1,"request_id":"0123456789abcdef0123456789abcdef","plugin_id":"example","operation":"install","arguments":["1.2.3"],"home":"/opt/atm","apps_dir":"/home/user/Apps","language":"en-us"}
```

The request ID is a fresh random 128-bit identifier, encoded as 32 hex digits.
It is correlation data, not an authentication secret. Stdout is exclusively
newline-terminated JSONL. Flush events promptly. Every event must echo protocol
and request ID; event-specific fields cannot be used on other event types.

```jsonl
{"protocol":1,"request_id":"0123456789abcdef0123456789abcdef","type":"started","message":"Preparing"}
{"protocol":1,"request_id":"0123456789abcdef0123456789abcdef","type":"progress","progress":0.5,"message":"Downloading"}
{"protocol":1,"request_id":"0123456789abcdef0123456789abcdef","type":"log","message":"Checksum verified"}
{"protocol":1,"request_id":"0123456789abcdef0123456789abcdef","type":"result","status":"success","message":"Completed"}
```

- Exactly one `started` first and one terminal `result` last.
- `progress` is numeric from 0 to 1, supplied by the plugin, not a UI timer.
- Result status is `success`, `error` or `cancelled`; optional `code` is a short
  machine-readable diagnostic. `message` is optional on all event types.
- Unknown fields, duplicate keys, invalid UTF-8/JSON, wrong identity, out-of-order
  events, unterminated lines and output after result fail the operation.
- Request limit: 64 KiB including its newline. Event line: 64 KiB excluding its
  newline. Message: 8 KiB. Code: 64 bytes. Total stdout: 16 MiB per operation.
- Stderr is diagnostic only: ATM forwards at most 32 KiB and fails the operation
  above 1 MiB. Control characters are sanitized before human-readable display.
  Do not emit credentials, tokens or private data in logs or diagnostics.
- The TUI retains 100 log entries and queues at most 64 events. Under sustained
  output it may drop intermediate logs/progress; the terminal outcome has a
  separate reliable channel. Logs are session-only, not durable audit records.

After completion, **View full output** opens the retained log messages in
chronological order with wrapping and scrolling, including when a result offers
follow-up menu choices. Back/Escape returns without selecting or confirming an
action. The normal row preview remains compact; the full-output view does not
restore entries already dropped by the limits above. A plugin presenting terms
for consent must bound the complete review below those limits and refuse an
oversized review rather than returning acceptance choices for truncated terms.
Control characters (including original line breaks) remain sanitized in both views.

ATM withholds the terminal event until the process exits and the stream has been
validated. A success result with nonzero exit is failure; exit zero without a
result is also failure. Error with exit zero becomes exit 1; otherwise the plugin
exit code is retained. Cancellation is exit 130; deadline expiration is 124.
On protocol/start/validation errors ATM produces a normalized error result.
These parent-generated failures can occur without a `started` event.

## Confirmation And Cancellation

The TUI confirms every operation, defaults to Cancel, and stays active while a
native process runs. Escape or Cancel stops the operation and retains its result;
q/Ctrl+C/Exit requests cancellation and waits for cleanup before leaving ATM.
The legacy Bash executor still hands the terminal to the subprocess.

CLI mutations require `--yes` or an interactive `y` response. JSON/noninteractive
mutations require `--yes`; status does not prompt. Global flags precede commands:

```bash
atm --dry-run --json install example 1.2.3
atm --yes --json --timeout 5m install example 1.2.3
atm --json status example
```

Dry-run emits a single plan and never launches, probes, writes or downloads.
Real native `--json` emits JSONL events and one validated terminal outcome.
Default deadline is 15 minutes; `--timeout` changes it for CLI and TUI operations.
CLI Ctrl+C cancels; no interactive stdin is forwarded after confirmation.

Linux and macOS use a dedicated process group. Windows creates a suspended process,
assigns it to a kill-on-close Job Object, then resumes its initial thread. Job
assignment failure aborts before plugin code runs. See Microsoft's
[process creation flags](https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags)
and [Job Object assignment](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-assignprocesstojobobject).
Cancellation/timeout attempts to terminate the group/job and waits for the backend
process. Cleanup also targets remaining descendants after normal backend exit;
inherited output pipes may add up to one second of cleanup time. Privileged
children may not be signalable by ATM, and sudo or other tools can create a new
session. Cancellation does not prove that privileged/detached work stopped or
that system changes rolled back. Inspect the package manager before retrying an
interrupted system operation. Plugins must
not leave background services or detach children from the process group/job.

**This is trusted-code execution, not a sandbox or an installation transaction.**
The plugin has the current user's privileges. Escaped Linux process groups,
concurrent replacement of trusted files and hostile native code are not isolated.
Cancellation can leave partial application changes. Recoverable staging,
installation locks, receipts and recoverable activation are now available as
[internal services](NATIVE-INSTALLATION-SERVICES.md), but existing plugins do not
use them automatically; do not port real installers without those safeguards. Native plugin
distribution/code updates are not implemented by the legacy catalog/updater.

## Isolated Validation

The fixture in `tools/native-fixture` never installs applications. Runner tests
compile it into a temporary directory, exercise protocol failures and literal
arguments, and check that descendant heartbeats stop after cancellation/exit.
For Linux terminal tests:

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o /tmp/atm-native-check/atm ./cmd/atm
go build -o /tmp/atm-native-check/fixture ./tools/native-fixture
ATM_TEST_NATIVE_FIXTURE=/tmp/atm-native-check/fixture python3 tests/test_go_tui.py /tmp/atm-native-check/atm -v
```

The fixture is not added to `plugins/` or shipped as an application plugin. CI
runs Go tests on Linux and Windows; actual results must be checked separately.
Cross-compilation is not evidence of Windows/ARM64 runtime acceptance.

After building local backends, check all declared operations without execution,
then run only each backend's root menu and status in an isolated HOME. The optional
Linux syscall gate requires `strace` and permission to trace local child processes:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
ATM_TEST_NATIVE_TRACE=1 python3 -B tests/check_go_packages.py --source /tmp/atm-native-check/atm
```

For dry-run the gate permits only CORE execution; for root menu/status it requires
exactly CORE and the backend. Both forbid network connection attempts and require
unchanged profiles, links and unrelated application data. It does
not execute downloaded application binaries. The same environment flag also applies
to host-target menu/status checks when a package directory is passed to the checker.

# AI Tools

Version: `0.0.1`.

The production AI plugin still uses its existing Bash implementation. The Go
migration is **in progress**, not a completed plugin migration. The production
`plugin.json` is intentionally unchanged; normal ATM builds and packages do not
activate the preview described below.

## Native Preview

Portable providers are implemented for Linux amd64 and arm64. Hermes system DEB
supports amd64; the separate ChatGPT Desktop DEB/RPM routes support amd64 and arm64:

| Provider | Artifact | Native operations |
| --- | --- | --- |
| PicoClaw CLI | Official architecture-specific `tar.gz` | Install, update, change active version, remove inactive version, uninstall owned preview versions, explicit user PATH integration |
| Hermes Desktop portable | Official architecture-specific AppImage | Install, update, change active version, remove inactive version, uninstall owned preview versions; no PATH integration |
| Codex CLI standalone | Official architecture-specific musl `tar.gz` | Same lifecycle and explicit user PATH integration as PicoClaw |
| Hermes Desktop system | Official amd64 DEB | Install latest or exact stable version with explicit system-package consent |
| Codex Desktop system (ChatGPT Desktop) | Official latest amd64/arm64 `chatgpt` DEB | Initial installation, explicit update and separately consented nonpurge removal of a configured system package; no downgrade or repair |
| Codex Desktop RPM (ChatGPT Desktop) | Official latest x86_64/aarch64 `chatgpt` RPM | Initial installation/update and separate removal; optional new dependencies and separately consented same-name/architecture dependency upgrades, with mandatory local signature checks |
| Ollama portable | Official base `tar.zst` bundle, optional amd64 ROCm companion | Install, update preserving variant, change active version, remove inactive version, uninstall owned preview versions, explicit user PATH integration and separately consented user-service management |
| Hermes Agent | Pinned official source and official PM | Explicit dependency preparation, local launcher publication, completed-preparation selection, consented native terminal bridge, explicit PATH enrollment/detach, native launcher refresh and deactivation preserving the entire preparation; reviewed contract only |

For PicoClaw and Codex portable archives, only the standalone executable is installed.
PicoClaw's
optional `picoclaw-launcher` WebUI, sample configuration and documentation are
not copied into the installation. PicoClaw CLI does not require that launcher.
Codex CLI uses the standalone release binary, not npm in this preview.

Pending before the entire AI plugin can switch to native Go:

- Hermes Agent remaining installer/runtime acceptance beyond the reviewed
  source/PM contract described below. External-data migration and physical
  preparation deletion are outside the selected retention policy.
- Ollama real user-service/runtime/GPU acceptance, drivers and non-ROCm add-on
  workflows. The selected service mode is the portable user service described
  below; root/system installation is outside this chosen scope.
- Codex Desktop DNF4 and Arch package management; the Arch
  shell bootstrap is not ported. Initial APT installation, explicit DEB updates and guarded removal
  are described below.
- PATH registration for providers other than the owned PicoClaw, standalone
  Codex CLI, Ollama and Hermes Agent previews, including external/legacy installations, is not implemented.
- PicoClaw WebUI/launcher support if added to the native provider scope.
- Real provider/runtime acceptance, Linux arm64 execution, other platforms and
  translations of the preview's English labels.

The preview does not fall back to Bash or an upstream installer script for any
of these pending operations. Unsupported providers fail explicitly.

## Hermes Agent PM Preview

This route prepares CLI dependencies using the **official Hermes PM**, then
publishes its official local launchers. It does not run the upstream shell
installer or reimplement Python dependency resolution. This is not full installer
parity: frontend builds, services, optional browser/computer-use tools, user data
migration remain outside it. Explicit local selection, separate native PATH
registration and controlled deactivation are available for completed owned
preparations. Deactivation preserves all prepared code, dependencies and data;
physical deletion is not part of that operation.

Only the reviewed `0.22.5` contract at source commit
`bf0203a794a07aac3ff764972ab9087baa4e4f4e` is enabled. `latest` resolves upstream
metadata but **fails closed if a different contract is returned**. `update`
requires a completed owned version; the same version does not reinstall or repair
dependencies. Future versions need explicit contract review before activation.
This restriction is intentional, not automatic compatibility with future PM APIs.

Use the isolated `preview_atm` setup documented later in this guide:

```bash
preview_atm plugin ai menu hermes-agent
preview_atm plugin ai install --provider hermes-agent --version 0.22.5 \
  --trust-download --accept-dependency-execution
preview_atm plugin ai update --provider hermes-agent \
  --trust-download --accept-dependency-execution
preview_atm plugin ai use --provider hermes-agent --version 0.22.5
preview_atm plugin ai status
```

`use` requires an exact version and CORE confirmation, but neither download nor
dependency-execution flags. It checks the existing preparation and changes only
ATM's selection pointer; it does not download, run PM, launch Hermes, edit PATH or
copy user data. Selecting the current verified preparation is a no-op. Incomplete,
modified, foreign or locked preparations are refused. This is not automatic
recovery of an interrupted installation: inspect retained locks and state manually.
If selection fails after publication starts, `native-v1.lock/selection.json`
retains the previous pointer and requested version for manual inspection. The
pointer may already have changed; ATM does not automatically roll it back.
Only the reviewed `0.22.5` contract is selectable today; this does not enable
unreviewed versions or establish cross-version runtime compatibility.

For install/update, CORE confirmation is required in addition to both flags. Dependency preparation
downloads and executes upstream tools and package build code with your user's
permissions. A private HOME **is not a security sandbox**. Test in a disposable
Linux glibc environment first. Root is refused; trusted `setpriv --no-new-privs`
prevents subprocess privilege elevation. A curated build-tool PATH omits `sudo`
and system package managers; missing host libraries/build prerequisites must be
installed separately. There is no automatic privileged repair. Timeout/output
bounds and process-group cancellation apply, but detached descendants and all
possible user-level side effects cannot be contained by those mechanisms.

State lives under `Apps/ai/hermes-agent/native-v1/versions/0.22.5/`:

- `source/`: complete pinned source, verified against Git tree/blob identities.
- `bootstrap/` and `python/`: verified uv and uv-managed bootstrap Python.
- `home/.hermes/`: private PM tools, dependency generations and application data.
- `host-bin/` and `tmp/`: curated host-tool links and temporary working files.
- `source.json` and `complete.json`: ATM provenance and preparation evidence.

These paths are established **before** PM runs and never renamed afterward.
Each future version has independent private data; switching pointers does not
migrate settings or conversations. Existing `~/.hermes` and external installations
are not imported, replaced or removed. Private paths alone do not prohibit trusted
upstream code from accessing other user-owned files.

Bootstrap uv is checked against `pm/lock.json` and separately staged in PM's
discovery slot. Bootstrap Python acquisition is delegated to that verified uv,
with the pinned version/build selector; ATM does not independently rehash uv's
Python download against Hermes's artifact digest. The application runtime is
managed by PM. Consent trusts GitHub HTTPS source identity and upstream pins,
not an independent release signature.

Completion requires successful PM install/doctor, a fresh successful sync receipt,
source-bound environment facts, exact `current` currency verdict and the two local
launchers. Read-only ATM status compares recorded evidence; it never runs PM,
repairs dependencies or claims the entire environment is immutable. Runtime may
change PM state; changed evidence is refused rather than silently adopted.
Interrupted attempts and locks are retained for manual inspection. Do not remove
a lock until all child processes have stopped. No automatic rollback/retry or
power-loss recovery is promised, and downloaded private data is never silently
deleted to retry installation. A failure after selection publication can leave
that selection in place with the retained lock; inspect rather than assuming the
previous version was restored. Hostile same-user filesystem races are not fully
contained by these ownership checks.

The native direct terminal bridge below uses the official machine command helper
and PM store Python, without executing the upstream POSIX shell launcher. Do not
use the bootstrap Python as the application interpreter or run a launcher's
`--version` as a read-only ATM probe. Actual provider compatibility remains Pending.

Contract references: [PM documentation](https://hermes-agent.nousresearch.com/docs/reference/package-management),
[pinned PM CLI](https://github.com/NousResearch/hermes-agent/blob/bf0203a794a07aac3ff764972ab9087baa4e4f4e/pm/cli.py),
[local launchers](https://github.com/NousResearch/hermes-agent/blob/bf0203a794a07aac3ff764972ab9087baa4e4f4e/hermes_cli/_launchers.py).

## Hermes Direct Terminal Preview

After preparing and selecting the reviewed version in the isolated setup below,
invoke the **AI preview executable**, not the CORE executable. This separate mode
preserves terminal stdin/stdout/stderr; it is not a JSONL plugin operation or a
menu action. It does not register a `hermes` command on PATH.

```bash
"$preview/runtime/plugins/ai/bin/linux-$arch/atm-ai" \
  --hermes-cli --apps-dir "$preview/apps" --accept-runtime-execution \
  --dry-run -- --help

# Executes the reviewed upstream helper and Hermes runtime. Use a disposable VM.
"$preview/runtime/plugins/ai/bin/linux-$arch/atm-ai" \
  --hermes-cli --apps-dir "$preview/apps" --accept-runtime-execution -- --help
```

The `--` delimiter is mandatory; following arguments go to the Hermes CLI.
Both explicit `--dry-run` and `ATM_DRY_RUN=1` validate syntax without inspecting
the installation, writing files, contacting the network or starting subprocesses.
Runtime consent is required even for `--help`. Root execution is refused.

Launch validates the selected completed preparation, source, recorded PM evidence
and ownership under the existing provider lock. It then asks the pinned official
`_launchers.py --print-runtime-command` helper for a bounded JSON argument array,
checks the exact store interpreter and unchanged CLI arguments, and rechecks the
original preparation before execution. The returned bootstrap is trusted upstream
code, not independently sandboxed Python. Native orchestration adds `-B` alongside
`-I` to suppress bytecode writes; it does not execute the shell launcher or run PM
installation/repair. Upstream runtime itself can access the network and mutate
data, and its full behavior is not confined by ATM.

The application keeps the caller's working directory, which must be outside the
managed Hermes root. HOME/HERMES_HOME, XDG paths and tool PATH remain private to
the prepared version. Caller API keys, Python/Hermes overrides and credential
variables are not inherited. Configure providers inside that private environment.
A valid terminal `TERM` is retained; preparation's forced CI/no-color settings are
not. Private HOME is not a sandbox for application code or agent actions.

ATM holds the provider lock while the immediate child runs, preventing its own
selection/installation operations from overlapping. Interrupted or uncertain
execution, changed preparation evidence or cleanup failure retains the lock and
`launch.json` for manual inspection. This journal records no arguments, environment
or credentials. Detached descendants are not contained; ensure all children have
stopped before manually clearing a retained lock. No automatic repair, rollback or
lock recovery is provided. Legitimate upstream changes to recorded PM evidence can
cause later checks to refuse that preparation. Real provider/terminal acceptance
remains Pending.

Normal child exit codes are preserved; signal termination is reported as the
conventional `128 + signal` exit code. TERM/HUP and context cancellation have a
one-second grace period before the immediate child is killed. Terminal INT/QUIT
are not forwarded a second time to a shared foreground process group. Because Go's
signal notification does not identify the sender, a targeted supervisor-only
INT/QUIT is not forwarded while that group owns a controlling terminal; use TERM
for targeted supervisor shutdown. Any observed interruption conservatively retains
the lock, even if the application later exits normally. Terminal resizing remains
the operating system's foreground-group behavior, not a synthetic resize channel.

## Hermes Native PATH Preview

After preparing and selecting the reviewed version, explicitly enroll its native
command through the isolated runtime:

```bash
preview_atm plugin ai integrate --provider hermes-agent
"$preview/apps/ai/hermes-agent/native-v1/bin/hermes" \
  --accept-runtime-execution --dry-run -- --help
preview_atm plugin ai integrate --provider hermes-agent --remove-path
```

CORE confirmation is required. Enrollment copies the running native AI backend
to `native-v1/bin/hermes` (0755), with a SHA-256/size/architecture ownership receipt
`bin/launcher.json` (0600). This is a native executable, not a shell wrapper or a
link to the upstream POSIX launcher. The private bin directory is published as
one unit. Its potentially multi-MiB payload is not stored in the small profile
rollback journal. Neither enrollment nor detach starts Hermes, PM or downloads.

The exact profile blocks and `path.json` follow the rules below. A new shell can
resolve `hermes`; the caller's current shell is not changed. The alias derives its
Apps root from its owned layout and refuses caller root overrides. Real invocation
still requires `hermes --accept-runtime-execution -- <arguments>`; even `--help`
executes upstream code. Syntax-only dry-run does not validate an installation.
Before execution, the alias requires intact enrollment, profiles and bridge
receipt, the matching running executable, and no interrupted HOME PATH operation.
These checks accompany the existing selected-preparation and provider-lock guards.

Detach removes only the enrolled blocks and `path.json`. It retains the verified
native copy and its independent receipt; execution through the detached alias is
refused. Reenrollment reuses that copy without silently updating it. Upgrading the
AI backend does **not** automatically refresh an existing Hermes bridge. Use the
separate explicit action below to replace that owned copy. Controlled deactivation
can remove the bridge while retaining the entire preparation, as described below.

Failures may leave the bridge, staging, HOME/provider locks and
`hermes-bridge.json` evidence for manual inspection, even when profile changes
were restored. Never assume an interrupted enrollment completed or that all
state was rolled back. Modified/foreign state is refused and preserved. No
automatic recovery or power-loss consistency is promised. Real shell/provider
acceptance, Linux arm64 execution and other platforms remain Pending.

### Explicit Launcher Refresh

After rebuilding or replacing the preview backend, run:

```bash
preview_atm --dry-run plugin ai integrate --provider hermes-agent --refresh-launcher
preview_atm plugin ai integrate --provider hermes-agent --refresh-launcher
```

The Hermes submenu also offers the refresh action when a verified owned bridge
exists. CORE confirmation is required. This copies the **currently running AI
backend**, not a downloaded Hermes release, and does not update Hermes, run PM,
start the provider, fetch metadata or modify profiles. An identical verified
image is a no-op. `--provider hermes-agent` is mandatory; `--remove-path`, version,
download-trust and dependency/system execution flags cannot be combined with it.
An absent bridge must first be enrolled separately; foreign/changed copies and
locked/interrupted operations are refused rather than adopted or repaired.

Both enrolled and detached copies can be refreshed. Existing profile bytes,
permissions and file identities, `path.json`, active selection and private
provider data are preserved. A detached alias stays detached and cannot launch
the provider until explicitly reenrolled. Refresh does not waive runtime consent.
The action verifies local ownership and executable identity; it is not a remote
release-signature check or a guarantee that the supplied backend is newer.

Refresh holds provider and HOME PATH locks, pins the original bridge and
preparation, and verifies the new source and staged copy. It exchanges the whole
private bin directory atomically, keeping executable and receipt together. Only
the two exact verified retired files and their empty directory are cleaned up;
there is no recursive removal. The filesystem must support this atomic exchange.
Uncertain failures preserve locks, remaining staging and small
`native-v1.lock/hermes-refresh*.json` records for manual inspection. The new
bridge may already be active and the old copy may be partly cleaned up: **no
automatic rollback or crash recovery is promised**. Never clear retained locks
while processes may still be using the installation. Large binaries are not
serialized into the profile journal. Linux arm64 execution and real provider
acceptance remain Pending.

### Deactivate and Retain Preparation

The Hermes submenu offers **Deactivate Hermes (keep preparation and data)**.
The CLI uses an explicit retention flag on the generic uninstall operation:

```bash
preview_atm --dry-run plugin ai uninstall --provider hermes-agent --keep-preparation
preview_atm plugin ai uninstall --provider hermes-agent --keep-preparation

# Reactivate an unchanged retained preparation without rerunning PM:
preview_atm plugin ai use --provider hermes-agent --version 0.22.5
preview_atm plugin ai integrate --provider hermes-agent
```

CORE confirmation and `--keep-preparation` are required. This is **deactivation,
not deletion of the installation**: source, Python, PM tools, dependencies, settings
and conversations stay under `native-v1/versions/0.22.5/` at their original paths.
It does not reclaim their disk space, migrate data or remove an external Hermes
installation. Do not delete the retained tree as a supposed cache. Repeating an
already completed and verified deactivation is a no-op.

The operation detaches only exact ATM-owned profile blocks, removes the owned
native launcher and its receipt, and clears `current.json`. Foreign commands,
unrelated profile text and the entire preparation are preserved. Enrollment is
not required: a selected preparation with a detached or absent bridge can also
be deactivated. Missing, incomplete, modified, foreign or locked preparations are
refused without adopting or repairing them. Runtime consent is still required
after reactivation; selection and enrollment do not execute Hermes or PM.

Both provider and HOME PATH locks remain held throughout the operation. Original
selection, preparation receipts, directory identities, bridge and profile state
are checked across phases. It neither stops external processes/services nor runs
the upstream uninstaller. A Hermes process launched through ATM holds the provider
lock and blocks deactivation; this is not detection of every external process.

On detach-phase failure, the existing profile transaction may restore only its
own unchanged writes. There is **no whole-deactivation rollback**: after later
failure, PATH, launcher or selection may already be removed. Locks, remaining
staging and `native-v1.lock/hermes-deactivate*.json` evidence are retained for
manual inspection, with the phase reported. The preparation remains in place.
No automatic recovery, arbitrary journal-driven cleanup or crash-consistency
guarantee is provided. Physical preparation deletion is deliberately outside the
selected retention policy. Real provider and other-platform acceptance remain
Pending.

## Existing Installation Discovery

Aggregate `status` and matching provider submenus report bounded, read-only
filesystem evidence separately from preview receipts. A regular file with an
executable permission bit is an **unverified external executable candidate**,
not a validated installation: version and runtime remain unknown. Scripts and
symlinks may be candidates; their contents, interpreter, architecture,
dependencies, package provenance and usability are not checked. Discovery grants
no ownership, update/removal permission, adoption or new provider menu routes.

Known legacy default candidates are relative to the request's selected Apps
directory, not a hardcoded `~/Apps`:

- `ai/picoclaw/picoclaw`
- `hermes/hermes-desktop/current.AppImage`
- `ai/codex/cli/bin/codex` (legacy npm prefix; package metadata is not read)

The names `hermes-agent`, `hermes`, `ollama`, `picoclaw`, `hermes-desktop` and
`codex` are also checked in the current environment's `HOME/.local/bin`, fixed
`/usr/local/bin`, `/usr/bin`, `/bin`, and bounded absolute PATH directories.
The native runner inherits HOME/PATH; request `Home` identifies the ATM runtime,
not the user's home. Known paths are checked even with empty PATH. These are
filename heuristics, not provider identification. An `/opt` directory alone is
never evidence. Hermes Agent and Ollama external evidence is visible in aggregate
status without authorizing ownership or lifecycle operations on those paths.
Codex Desktop has a separate native, bounded read of the root-trusted dpkg status
file for package `chatgpt`. It reports package-record evidence, not ownership,
application health or a running process. An unavailable, malformed or changing
database means unknown, not absent. RPM/Arch presence remains unknown; `codex`
is not treated as Codex Desktop evidence. No package-query subprocess is run.

The three single-file `native-v1/current` paths and Ollama's
`native-v1/current/bin/ollama` are reported separately. Literal
duplicate candidates are removed; resolved aliases and hard links to a preview
current executable are excluded from external evidence. Paths resolving inside
a preview root are also excluded, without inferring ownership of that root.
Other inode aliases are identified as duplicates; distinct candidate files are
all reported rather than stopping at the first PATH match. Unrecognized files
with multiple hard links are reported as `ambiguous-hardlink`, with unknown
ownership, rather than external candidates: an inactive preview version could
share that inode. No wider ownership scan is performed.

Missing paths, inaccessible metadata, broken links, cyclic/excessive links,
non-executable files and directories/special files have distinct evidence states.
Paths are quoted/escaped for display. Empty, relative, unclean, unsafe-control
PATH entries and current-directory entries/aliases are skipped; unsafe symlink
targets are refused, and symlink target trailing slashes/dots retain their
directory requirements. Search is limited to the first 32 KiB of PATH, at most
64 PATH entries, 8 fixed-system directories and 448 total candidates. Individual
paths are at most 2000 bytes, with at most 40 symlinks and 512 resolution steps.
Truncated or skipped searches are disclosed; missing candidates never prove
that the application is absent from all locations.

Discovery uses only file attributes and symlink metadata: no executable/content
reads, directory walks, shell sourcing, package scans, commands, network or
writes. Cancellation is checked between metadata operations; a blocked kernel
filesystem call cannot itself be interrupted. Results are snapshots, not a
guarantee against concurrent filesystem changes. Ordinary user-owned/manual
paths do not need root ownership to be reported as unverified evidence.
Legacy `plugin.conf` and `ATM_AI_*` path overrides are not sourced or imported;
custom locations are visible only when their command is in an eligible PATH
directory or one of the known locations. Credentials, models and settings are
not examined. Existing preview receipt/journal checks remain separate.

## Storage and Safety

The single-file portable providers use new private directories beneath `ATM_APPS_DIR`:

```text
ai/picoclaw/native-v1/
hermes/hermes-desktop/native-v1/
ai/codex/cli/native-v1/
```

Each root holds `owner.json`, `versions/<X.Y.Z>/{tool,receipt.json}` and a
`current` symlink to `versions/<X.Y.Z>/tool`. **`current` points directly to an
executable**, not a directory. Invoke the exact path printed after selection.
Portable operations do not execute a downloaded application, launch a browser,
authenticate, install dependencies, or configure system services. Install,
update and version selection do not edit PATH or shell profiles. Only explicit
CLI integration edits profiles; uninstall detaches an existing exact owned block.
For AppImage execution, arrange any required FUSE/runtime dependencies separately.

Existing Bash installations, manually installed binaries, application settings,
models, credentials and user profiles are never adopted or removed. Legacy
`ATM_AI_*` path overrides are not imported into this isolated preview; the roots
above use ATM's configured Apps directory.

Downloads require both CORE's operation confirmation and the explicit
`--trust-download` argument. The resolver requires one exact stable release asset
for the requested architecture, a valid size and the asset's GitHub API SHA-256
digest. Older custom versions without that digest fail closed. GitHub HTTPS
metadata is the trust source; this is **not an independent signature**. No
checksum-free fallback exists.

Downloads and archive extraction are bounded. These single-file archive routes
use ATM's shared native installation library, rejecting traversal, links, duplicates and
unsupported entries. The binary's ELF architecture/type is checked without
execution; an AppImage must also carry the type-2 AppImage marker. Only a fully
verified staged version is published. Selection changes atomically, so an error
before selection leaves the previous active version intact. A failure after
publication can leave a verified inactive version available for explicit `use`.

Mutation requires private receipt ownership and an unchanged payload. Unknown
files, foreign links, modified executable contents, unsafe permissions and
symlinked/writable directory ancestors cause refusal rather than deletion.
Removing the active version requires selecting another version first, or using
the explicit uninstall action. Uninstall preserves the private store marker and
empty versions directory, as well as all user data. Cancellation or failure
during uninstall may leave some inactive versions; it is not reported as a full
rollback.

Concurrent operations are excluded by the sibling `native-v1.lock` directory.
This coordinates ATM processes only; do not edit the preview store concurrently.
Activation rechecks the candidate before selection. Removal rechecks a detached
version and deletes only its verified files, never recursively deleting unknown
contents. A failed cleanup retains the remaining detached files and reports their
path for inspection; that failure is not a rollback.
The remainder may contain only a receipt after the executable was deleted, not a
complete installation that can be restored. These guards do not isolate the
store from continuous hostile changes by another process under the same user.
After a crash, a lock or private `.atm-ai-stage-*`, `.atm-ai-select-*`, or
`.atm-ai-remove-*` directory may remain for inspection. There is no automatic
force unlock or cleanup of unknown remnants. Verify that no installer is active
and inspect the relevant paths before manual recovery; do not recursively delete
the provider's existing installation directory.

## Ollama Portable Bundle

Provider ID: `ollama`. This isolated preview implements native portable lifecycle
and a separately consented user service, not the upstream root/system installer.
It preserves the complete
official `bin/ollama` and `lib/ollama` base tree, including library links, helper
executables and licenses. Keeping these paths adjacent matters: Ollama resolves
its executable path to locate sibling libraries. No downloaded shell is executed.

The [official Linux guide](https://docs.ollama.com/linux) documents `.tar.zst`
base bundles for amd64 and arm64. ATM resolves stable `ollama/ollama` GitHub
releases, selecting exactly `ollama-linux-amd64.tar.zst` or
`ollama-linux-arm64.tar.zst` with the release asset's SHA-256 digest and exact
size. `latest` and custom stable `X.Y.Z` are supported; prereleases, older release
formats, missing digests and alternative URLs are refused. ROCm is an explicit
amd64-only companion option described below; JetPack and MLX are not selected.
GitHub metadata plus its fixed HTTPS
download/storage origins is the trust boundary, not an independent signature.
CORE confirmation and `--trust-download` are required before install/update.

Storage beneath `ATM_APPS_DIR` is dedicated and never adopts external files:

```text
ai/ollama/native-v1/
  owner.json
  versions/<X.Y.Z>/receipt.json
  versions/<X.Y.Z>/tree/bin/ollama
  versions/<X.Y.Z>/tree/lib/ollama/...
  current -> versions/<X.Y.Z>/tree
```

Unlike single-file previews, Ollama's `current` selects a **directory**.
Receipts bind provider, stable version, host architecture, source URL, compressed
size/digest and a complete tree inventory of file hashes, sizes, modes and raw
symlink targets. Directories are private 0700; files normalize to 0600 or 0755
when executable. ELF architecture/type validation applies to `bin/ollama` without
execution; libraries/helpers are inventoried as data, not runtime-validated.

Each compressed download is bounded to **4 GiB**. Decompressed tar streams
(including headers/padding) share a **12 GiB** budget across the base and optional
companion, with a combined inventory of at most 10,000 entries and a per-archive
header limit of 10,000. Paths are at most 1024 bytes/32 separators. The dedicated
extractor uses `github.com/klauspost/compress/zstd`, single concurrency, low-memory
mode and a 128 MiB decoder window/memory limit. Exact response size and SHA-256
must match before extraction; tar, zstd checksum and cancellation errors refuse
publication. A single leading `./` and an optional root directory are accepted.
Traversal, duplicate normalized names, hardlinks, sparse/special files, unsafe
modes and entries outside the bin/lib layout are rejected. Library symlinks are
created only after checking the full graph for escapes, dangling targets, cycles,
ancestor-directory targets and links used as extraction parents.

The download timeout is ten minutes, within CORE's existing fifteen-minute
operation timeout. There is no download resume. The two compressed archives can
total up to **8 GiB**. These bounds are not a total-operation I/O budget:
mutations repeatedly rehash the store and selected
tree at validation/commit boundaries. Ollama menu/status also hash every recorded
tree, so a large store can be slow or time out. Their verified version/selection
claims come from receipts and inventory only; runtime remains unknown. Network
lookup, process/service probes and private model/configuration reads never occur
in these views. Status performance with large real stores remains unaccepted.

Installation stages privately and atomically publishes each version; selection
atomically replaces the directory link after validating its exact staged identity
and target. Update requires an owned active version and refuses downgrades.
`use` requires an exact owned unchanged version. `remove` refuses the active
version; `uninstall` validates all versions before deselection. Modified files,
unknown entries, foreign ownership, unsafe ancestors, changed permissions or
hardlinks fail closed. Removal detaches and re-verifies the tree, then removes
only recorded entries in postorder; it never recursively sweeps unknown data.
Failure may retain an incomplete detached remainder, or inactive versions after
uninstall deselection. The error identifies retained staging for manual inspection.
The owner marker and empty versions directory remain after successful uninstall.

The sibling `native-v1.lock` serializes cooperating ATM operations. Interrupted
operations can retain that lock and `.atm-ollama-stage-*`, `.atm-ollama-select-*`
or `.atm-ollama-remove-*` siblings. No automatic recovery, complete rollback,
power-loss durability or hostile same-user isolation is promised. Inspect the
reported paths and confirm no installer is active before manual recovery.

**Manually close/stop processes using this owned bundle before updating,
switching or removing it.** Also remove any ATM user-service registration first;
stopping it alone does not unlock version mutations. Lifecycle commands do not
probe, start or stop external Ollama processes or services. No model download,
GPU setup, system-user creation, system package operation or global PATH write
occurs. Install, update and use never edit
profiles. Explicit `integrate` enables only the owned preview command in user
profiles; uninstall detaches that exact integration before removing versions.
Existing external/legacy installations, models, credentials and
settings remain untouched. A separate explicit runtime invocation can change
user data or conflict with an already running server; review upstream settings
and existing servers yourself first.

### Optional AMD ROCm Companion

On Linux amd64, the Ollama submenu also offers an explicit ROCm installation.
This downloads the base archive plus `ollama-linux-amd64-rocm.tar.zst`, following
the [official additional-package layout](https://docs.ollama.com/linux).
Both artifacts come from one release-metadata response and must match the same
stable version and architecture, each with its own exact size and SHA-256.
There is no architecture fallback or automatic GPU detection.

```bash
preview_atm --dry-run plugin ai install --provider ollama --rocm --trust-download
preview_atm plugin ai install --provider ollama --rocm --trust-download
preview_atm plugin ai install --provider ollama --version X.Y.Z --rocm --trust-download
# An installed ROCm variant remains ROCm on ordinary updates:
preview_atm plugin ai update --provider ollama --trust-download
# Explicitly select ROCm when updating a base installation to a newer release:
preview_atm plugin ai update --provider ollama --rocm --trust-download
```

The pair is assembled only in unpublished private staging. The companion is
limited to `lib/ollama/rocm_v<digits>/` trees and their parent directories, matching
the [reviewed official packaging layout](https://github.com/ollama/ollama/blob/v0.40.13/scripts/build_linux.sh).
Other layouts are refused. Shared directories are permitted, but files and links
cannot replace base entries. Combined path,
link-graph and inventory checks must pass before publication. Base-only schema-1
receipts remain readable; ROCm uses schema 2 with a mandatory companion record.
An older preview that does not support schema 2 refuses that version.

Each version directory holds one variant. ATM refuses to convert an existing
same-version directory in place. Prefer a newer release; switching an existing
version's variant requires explicit removal first. Normal `use`, `remove`,
`uninstall` and PATH integration apply to the complete recorded bundle. Status
reports the stored variant, not whether the GPU or drivers work.

This installs **application libraries, not GPU drivers**. It never runs Ollama,
starts a service, probes hardware or downloads models. Real ROCm archive/runtime
and hardware acceptance remain Pending; automated tests use synthetic archives
and read-only official metadata. Large paired downloads may exceed the existing
operation timeout on slower connections and will not publish a partial version.

With the isolated `preview_atm` helper below, the base commands remain:

```bash
preview_atm plugin ai menu ollama
preview_atm plugin ai install --provider ollama --trust-download
preview_atm plugin ai install --provider ollama --version X.Y.Z --trust-download
preview_atm plugin ai update --provider ollama --trust-download
preview_atm plugin ai use --provider ollama --version X.Y.Z
preview_atm plugin ai remove --provider ollama --version X.Y.Z
preview_atm plugin ai integrate --provider ollama
preview_atm plugin ai integrate --provider ollama --remove-path
preview_atm plugin ai uninstall --provider ollama
```

The menu/selection logs print shell-literal optional manual examples using the
full `current/bin/ollama` path: `serve`, `run llama3` and `pull llama3`. Printing these
examples does not execute them. The separate user-service start action below
explicitly executes `serve`. `run`/`pull` may download models independently of the bundle
installation. Real upstream bundle download/extraction and runtime acceptance,
active-process handling, GPU behavior and Linux arm64 execution remain Pending;
synthetic archives and public metadata checks do not establish those claims.

## Ollama User Service

This preview's selected service mode is a **Linux systemd user service**, not the
official root-based installation. It uses only an owned, selected and completely
verified portable bundle. It does not adopt `ollama.service`, external units,
aliases, overrides, existing model directories or an external Ollama installation.

The Ollama submenu offers these separate actions. CLI arguments for the native
preview are:

```text
plugin ai integrate --provider ollama --service-action register --accept-service-execution
plugin ai integrate --provider ollama --service-action start --accept-service-execution
plugin ai integrate --provider ollama --service-action status
plugin ai integrate --provider ollama --service-action stop --accept-service-execution
plugin ai integrate --provider ollama --service-action remove --accept-service-execution
```

Mutations require the bare consent flag plus CORE confirmation. Do not combine
service actions with PATH, download, version, ROCm or system-package options.
`status` rejects the consent flag; it is an explicit bounded manager query.
Ordinary menu/aggregate status never contact the manager or execute Ollama.

Registration writes the dedicated `~/.config/systemd/user/atm-ollama.service`
and reloads the user manager. It never starts/enables the unit or changes linger.
The unit pins an exact version, binds to `127.0.0.1:11434` and uses a clean
environment with private HOME and models beneath `Apps/ai/ollama/service-v1/data`.
The data and ownership evidence survive service removal and portable uninstall.
ATM never automatically imports or deletes your existing models or credentials.
Private HOME is not a sandbox; starting the server executes the selected provider.

All Ollama install/update/use/remove/uninstall operations are blocked while the
service is registered or its state is uncertain, even when it is stopped. Stop,
remove the registration, change versions, then register/start again. Removal
requires the owned service to be stopped and preserves data. Changed unit files,
drop-ins, aliases, pending jobs or ambiguous manager output refuse mutation.
Interrupted operations retain a journal for manual inspection; there is no
automatic recovery or guaranteed whole-operation rollback.

The user must be non-root with a reachable login user manager. Only default
XDG configuration/data paths are supported, and the unit directory must appear
in that manager's search paths. A temporary `HOME` does **not** redirect a running
user manager: the isolated `preview_atm` wrapper below is for lifecycle/guard
testing, not a real service test. Do not bypass a path/ownership diagnostic.
Real service acceptance needs a separately reviewed preview using the login
user's actual HOME; no real service was started during automated verification.

`active/running` means only that the manager reports the process running, not
that inference, model access or GPU acceleration works. There is no automatic
model download, driver setup, restart policy, auto-start or port-conflict repair.
Real Linux service/GPU and ARM64 runtime acceptance remain Pending.

## Explicit CLI PATH Integration

Owned PicoClaw, standalone Codex CLI, Ollama and Hermes Agent previews can be integrated:

```text
integrate [--provider picoclaw|codex-cli|ollama|hermes-agent] [--remove-path]
integrate --provider hermes-agent --refresh-launcher
```

Use `preview_atm plugin ai integrate ...` with the isolated runtime below.
CORE confirmation or its explicit `--yes` authorizes this mutation; no download
or system-package flags apply. `--version` is not accepted. Without a provider,
the operation processes selected owned PicoClaw, Codex CLI, Ollama, then Hermes Agent previews.
The generic `preview_atm path apply --plugins` route also includes these owned
previews through provider-less integration. Missing
previews are skipped, but unsafe/foreign stores are refused, never adopted.
Each provider commits separately: if a later provider fails, earlier completed
providers remain changed and the error lists them. There is no aggregate rollback.

For the first three providers, integration creates `native-v1/bin/picoclaw -> ../current`,
`native-v1/bin/codex -> ../current`, or
`native-v1/bin/ollama -> ../current/bin/ollama`, plus `native-v1/path.json` (0600). The bin
directory is private (0700). The receipt binds the provider, root, default user
HOME and exact enrolled profile blocks. It deliberately follows the selected
`current` executable rather than pinning one version. Enabling verifies the
selected executable's receipt/SHA-256 and rechecks them before publication and
completion. Ollama verifies and pins its complete bin/lib inventory and receipt,
not just the command. Its `current -> versions/X.Y.Z/tree` directory link and
adjacent libraries are preserved; the stable command follows version switches.
Later install/update/use operations change selection but do not edit profiles.

Profiles are `$HOME/.profile`, `$HOME/.bashrc`, and **existing** `$HOME/.zshrc`.
HOME comes from the normal user environment, not request `Home` (the ATM
runtime). Each provider has separate exact markers, for example:

```sh
# >>> ATM native AI picoclaw PATH >>>
export PATH='/absolute/apps/ai/picoclaw/native-v1/bin'${PATH:+":$PATH"}
# <<< ATM native AI picoclaw PATH <<<
```

Paths use literal shell quoting, including embedded apostrophes. Empty/unset
PATH does not gain a trailing colon/current-directory search entry. Prepending
this directory **can shadow an existing external command**; external command
files are not replaced. The current shell is not modified. Reload/new-shell
behavior and actual provider execution require separate human acceptance.

All three profile candidates are preflighted before any profile write. Links,
special files, multiple hardlinks, foreign ownership, unsafe writable modes,
special permission bits, files over 1 MiB and unsafe ancestors are refused.
Exact receipt-owned blocks are required: missing, edited, duplicate, partial or
unowned blocks are preserved and refused. Unrelated content and file modes are
preserved. New `.profile`/`.bashrc` files use 0600 and remain as empty files after
detach. A newline inserted after unterminated original text is removed only if
that will not join remaining content to the original line. If another provider
or later content needs it, that newline remains, possibly after the final detach.

For those three providers, `--remove-path` detaches only exact owned blocks, the command link and receipt;
it retains all provider versions and the empty bin directory. Uninstall performs
that same guarded detach before deselecting or deleting versions, or refuses
before destructive work. Failures after detach can leave PATH detached while
versions remain; no whole-uninstall rollback is promised.

The existing sibling `native-v1.lock/` and shared `$HOME/.atm-ai-path.lock/`
(0700) are fail-fast locks. The HOME lock coordinates all four CLI providers and
distinct Apps roots using the same markers. A different Apps root cannot adopt
an existing block. This lock does not coordinate profile writers in other plugins
or external tools. Per-file staging uses `.atm-ai-path-write-*/entry` beneath
the target's parent (HOME, native-v1, or native-v1/bin). Before/after file bytes,
modes, existence and link targets are backed up in
`$HOME/.atm-ai-path.lock/journal.json` (0600) before publication.

Failure/cancellation attempts restoration only while published state still
matches. Intervening content, mode or inode changes are preserved; incomplete
restoration retains the journal and shared lock with a recovery-path error.
Successful restoration removes the journal. Interrupted processes can leave a
lock, journal, staging or partially published state. These are inspection aids,
**not automatic recovery**; editable journal paths never authorize cleanup.
Only locally known staging entries are removed, not arbitrary saved directories.
Journal data and staged files are synced, but directory entries are not: there
is no power-loss durability guarantee or isolation from continuous hostile
same-user mutation. No real profiles or applications were used in automated tests.

## Hermes Desktop System DEB

Provider ID: `hermes-desktop-system`. Only `install` is supported, with optional
`--version latest` (default) or `--version X.Y.Z`. The fixed upstream is
`fathah/hermes-desktop`; release metadata must identify exactly
`hermes-desktop_<version>_amd64.deb` and provide its SHA-256 digest and size.
No arm64 DEB or AppImage fallback is attempted. Normal CORE confirmation plus
**both** `--trust-download` and `--accept-system-packages` are required before
network access, writes or command execution.

The provider submenu and aggregate status are offline, read only, and never
query APT/dpkg or execute installed apps. System status reports ATM's operation
journal and separate unverified executable candidates, **not current
installed-package status**. Release resolution and system
checks happen only during a consented install; the menu does not present a
pre-resolved review plan. An existing exact version is a no-op; a newer existing
version is preserved. Installation may upgrade an older system package, but
never records ownership of that package. `update`, `use`, `remove`, and
`uninstall` are intentionally unsupported for this provider.

The download goes to private user staging and must match both digest and exact
size. Trusted `dpkg-deb` inspects package name, exact version and architecture,
without running package scripts. Trusted root commands create a private
`/run/atm-ai-hermes-*` directory and copy the DEB there. Its hash and metadata are
checked again on the root-owned copy **before APT uses that copy**. Commands use
absolute root-trusted paths and literal arguments, never a shell. Non-root use
requires `sudo -n`; ATM never prompts for or reads a password. Authentication, if
needed and permitted by policy, is a separate action in the user's terminal.

APT retains administrator configuration, sources and package policy; ATM does
not rewrite repositories, refresh indexes or force installation. **A local DEB
can bypass package pins**, so this preview conservatively refuses any nonempty
APT preferences (comments/blank lines are allowed) or redirected preference
paths. This includes unrelated pins until complete policy evaluation is
implemented. The command environment is allowlisted and does not inherit
`APT_CONFIG`. APT is passed explicit no-removal, no-downgrade, no-held-change and
authenticated-repository guards. The installed package record is checked before
and after APT; held, incomplete, foreign-architecture or unsupported version
records cause refusal. Do not change package policy concurrently.

**APT executes package maintainer scripts and triggers as root**, and may install
or upgrade dependencies. Their effects can include services and desktop
integration. These are not portable operations and are not transactional. ATM
does not launch Hermes Desktop itself or directly edit user settings, models,
credentials or portable installations; upstream package-script behavior remains
part of the explicit system-package trust decision.

Operation state is private under
`ATM_APPS_DIR/ai/hermes-desktop-system/native-v1/`, with `owner.json` and
`journal.json`, plus a sibling lock. This is an **operation journal, not an
ownership receipt for system files**. Failures retain input staging for manual
inspection. Once any privileged command is attempted, an error or cancellation
retains the lock and an unknown/incomplete journal, plus any remaining root
staging. Privileged descendants may still be running; there is no automatic
rollback, force unlock or recovery. Even a successful APT installation followed
by failed cleanup/journaling is not reported as a completed ATM operation.
Inspect processes, package state and staging before a manual recovery. Saved
journal paths never authorize automatic privileged deletion. Successful cleanup
only deletes this transaction's verified temporary file and then its empty
directory, never recursively removes an installation.

## Codex Desktop System DEB

Provider ID: `codex-desktop-system`. The official Linux application is currently
branded **ChatGPT Desktop**, includes Codex, and uses package name `chatgpt`.
This preview supports **initial installation, explicit update and nonpurge removal** on Linux
amd64/arm64 with APT. It downloads the architecture-specific latest DEB from the fixed official
`https://persistent.oaistatic.com/codex-app-prod/linux/deb/latest/` origin.
Custom URLs and exact version selection are refused. A separate
DNF5 RPM install/update/removal preview is described below; Arch remains Pending.
`install` still refuses any existing `chatgpt` record,
including held, incomplete and residual-configuration states. Only a separately
confirmed `update` can change an eligible installed package. System dependencies
must already be available through
the administrator's working APT configuration.

`update` requires a fully configured `install ok installed` package with a valid
Debian version and matching architecture. Missing, unknown, held, incomplete or
foreign-architecture state is refused, not repaired or implicitly installed.
The menu offers update only for eligible observed state; it does not look up a
remote version. The exact original status/version/architecture is bound to the
transaction and rechecked by both locked APT guards. A concurrent package change
causes refusal rather than updating a different baseline. This explicit system
operation can update a manually installed package, but grants no ATM ownership,
automatic removal authority or permission to migrate application settings.

Trusted `dpkg --compare-versions` supplies Debian ordering, including epochs and
revisions. A newer candidate is required for package changes. An equal candidate
reports that no update was performed, without privileged commands or reinstall;
a lower candidate or comparison error is refused. No-op does not establish
application health. The update APT invocation is upgrade-only; it is not a general
distribution upgrade. Existing install journals remain readable and are not
reinterpreted as update authorization.

The upstream guide recommends updates through the signed package repository.
This ATM preview deliberately reuses the fixed latest **direct DEB** and the
staging/transaction checks below instead. It does not refresh APT indexes or manage
repository updates. The direct-download trust boundary is not a claim of
independent package-signature verification.

The submenu and aggregate status read bounded dpkg records without running
applications or package managers. Missing/unreadable database evidence or pending
dpkg updates are unknown and disable installation/update, rather than assuming absence.
The official desktop support matrix is narrower than "all APT systems"; use a
supported Ubuntu/Debian release from the
[official Linux guide](https://learn.chatgpt.com/docs/linux/linux-app).

Installation and update require CORE confirmation and **both** `--trust-download` and
`--accept-system-packages`. Native HTTP refuses redirects, encoded responses,
missing/invalid lengths and downloads over 512 MiB. The exact received length is
checked. No independently published checksum or signature is asserted for this
direct DEB: trust comes from the fixed official HTTPS origin. The locally
computed SHA-256 only checks that staging did not change those downloaded bytes.
Trusted `dpkg-deb` verifies package name, Debian version and architecture without
executing package scripts. Root staging under `/run/atm-ai-codex-desktop-*` is
checked again for hash and metadata before APT. A private copy of the running
native backend, with verified hash and transaction-bound metadata, acts as an
APT guard. An early hook checks the expected package state before preconfiguration;
a second hook checks that state again and the exact staged DEB in the plan. Install
requires absence; update requires the exact original configured package. Both require the
kernel's whole-file frontend lock to belong to a root ancestor running the trusted
APT executable. Fresh root-owned acknowledgements are required after APT returns;
a skipped hook or APT no-op without that evidence is not a successful package change.
This is separate from the pre-APT equal-version no-op. The exact installed
package version/architecture and configured state are checked afterward.

As with Hermes system DEB, custom APT pins are refused and non-root execution
requires separately authorized `sudo -n`. This provider additionally refuses
database/root redirection, disabled locking and unsupported dpkg configuration.
Existing `DPkg::Pre-Invoke` hooks are refused so no such hook precedes the early
guard; stock `DPkg::Pre-Install-Pkgs` preconfiguration remains supported afterward.
A trusted `env -i` fixes the post-sudo environment, including `HOME=/`.
APT internally uses `/bin/sh` to invoke fixed literal hooks that execute the
root-staged Go guard; there is no downloaded shell installer or Bash fallback.
**APT runs maintainer scripts and triggers as root**, may change
dependencies and desktop integration, and the official package can configure
OpenAI's package repository. ATM does not itself launch/sign in to the app, edit
profiles or refresh package indexes. Neither a temporary HOME nor Apps directory
isolates these system effects. The guard relies on the APT/dpkg frontend-lock
protocol; arbitrary root scripts and tools that ignore those locks are outside
that coordination. Do not change package policy during the operation.

The private `ATM_APPS_DIR/ai/codex-desktop-system/native-v1/` journal records the
operation, not ownership of system files. Errors retain input evidence; after a
privileged attempt they also retain the lock and uncertain root staging for
manual inspection. Cancellation does not prove privileged descendants stopped.
There is no automatic rollback, retry cleanup, force-unlock or recovery. Stored
journal paths never authorize deletion; successful cleanup removes only the
current transaction's verified DEB, native guard, metadata and acknowledgements,
followed by their empty staging directory.

### Explicit System Removal

`uninstall --provider codex-desktop-system --accept-system-packages` is a separate
confirmed system operation. It does not require an ATM install receipt and can
remove an eligible manually installed `chatgpt` package. This is explicit consent,
not ownership inferred from discovery or a previous update. Only a configured,
unheld package with a valid version and matching architecture is eligible.
Absent, residual-configuration-only, incomplete or unknown states are refused.
No download, `--trust-download`, version selection or implicit repair is involved.

The original package baseline is checked by native root-staged guards under the
APT frontend lock. The removal plan must identify only `chatgpt` with its original
version and architecture; extra removals, installs and configuration actions are
refused. The operation uses `remove`, never `purge` or `autoremove`, and does not
download packages or refresh package indexes. Unrelated pending-operation sweeps
and selection rewrites are disabled; normal removal triggers remain enabled.
A package removal is successful only after required
guard acknowledgements and an absent package or matching residual-configuration
record are observed. Residual configuration is preserved, not silently purged.

ATM does not delete personal profiles, credentials, conversations, Codex CLI
installations or repository configuration as part of this operation. Package
maintainer scripts and triggers still execute with system privileges and may
affect services and desktop integration; this is not a sandbox or rollback.
Unexpected state and interruptions retain evidence for manual inspection.
The same provider lock serializes install, update and removal; incomplete earlier
operations block new ones. Journal files never authorize recursive cleanup.

For a human test, build the isolated preview below **inside a disposable supported
Ubuntu/Debian VM with a snapshot**, not on a workstation. Only after accepting
system changes and arranging sudo authorization, run:

```bash
preview_atm plugin ai menu codex-desktop-system
# Human authorization in the VM; ATM does not collect a password.
sudo -v
preview_atm plugin ai install --provider codex-desktop-system \
  --trust-download --accept-system-packages
preview_atm status ai

# Separate optional update test in the same disposable VM:
preview_atm plugin ai update --provider codex-desktop-system \
  --trust-download --accept-system-packages

# Separate optional nonpurge removal test in the disposable VM:
preview_atm plugin ai uninstall --provider codex-desktop-system \
  --accept-system-packages
```

Verify the package and GUI separately in the VM, then restore its snapshot.
Automated checks use synthetic downloads and recording command substitutes;
real APT/GUI and arm64 runtime acceptance remain Pending.

## Codex Desktop System RPM

Provider ID: `codex-desktop-rpm`. This Linux amd64/arm64 preview supports
**initial installation, explicit update and removal with DNF5**. Installation/update use the architecture-specific latest
RPM linked by the [official Linux application documentation](https://learn.chatgpt.com/docs/linux/linux-app).
Installation/update require CORE confirmation, `--trust-download` and `--accept-system-packages`.
Removal requires CORE confirmation and `--accept-system-packages` only; download
trust and version selection are rejected, and no RPM artifact is downloaded.
There is no DNF4 fallback or upstream shell installer. Existing `chatgpt` RPM
records block initial installation. `update` and `uninstall` require exactly one installed,
matching-architecture `chatgpt` record; it never implicitly installs a missing
package. Unknown query results, including a successful RPM exit with diagnostics,
and incomplete ATM operations block all flows. A manually installed system
package can be explicitly updated or removed without granting ATM ownership or adopting
personal data.

The fixed official HTTPS URL is the download trust source. A bounded download,
RPM header checks and repeated SHA-256 checks bind the staged bytes; that local
hash is **not an independent publisher signature**. Trusted RPM/DNF5 tools run
only after explicit consent, with a fixed environment and noninteractive sudo
when required. Administrator local-package signature policy, versionlocks and
excludes are not disabled; ATM does not provision package-signing keys. The
optional dependency mode below permits DNF5's repository-metadata key handling.

DNF5 stores a transaction before execution. Default initial installation permits exactly
one `Install` action. Explicit update requires exactly the candidate `Upgrade`
and original installed `Replaced` records, binding name, epoch, version, release
and architecture. The candidate must be the downloaded local RPM; the replaced
record must refer to the installed system package, with no artifact path.
RPM ordering comes from the trusted [DNF5 upgrade solver](https://dnf5.readthedocs.io/en/latest/commands/upgrade.8.html),
not a semantic-version approximation. Downgrade, reinstall, implicit install,
additional removal and dependency-changing plans are refused.

Explicit removal pins that original installed identity, stores exactly one
`Remove` action from `@System` with no artifact path, and rejects all extra actions.
It uses [DNF5 remove](https://dnf5.readthedocs.io/en/latest/commands/remove.8.html)
with `--no-autoremove` and `clean_requirements_on_remove=False` during store/replay.
No dependency cleanup, download, manual personal-data deletion or old-preparation
cleanup is performed. Privileged package scripts may still have side effects.

By default all repositories are disabled during store/replay, so **dependencies must already
be satisfied**. Original installed identity (or absence for initial install),
effective policy and staged evidence are rechecked before execution. Afterwards,
the installed RPM identity must match the downloaded header for install/update;
removal requires a diagnostic-free query establishing absence. Changes to the
original installed version abort update/removal rather than silently selecting a new
baseline. Replay never uses flags that ignore installed-package mismatches.

An exactly identical downloaded package identity is a verified no-op before
privileged staging or DNF transactions; its private input/journal remain available.
Other empty plans are refused, not interpreted as a successful update. New schema2
journals distinguish install/update and record the original update baseline;
schema1 initial-install journals remain readable. Older preview binaries may
refuse these newer journals, rather than misinterpret them.
Artifact-free schema3 removal journals retain schema1/2 read compatibility and
require stage/plan evidence for advanced phases; they never claim a downloaded RPM.

### Installation And Update With New Dependencies

Separate install/update menu actions add `--accept-dependencies` to the two existing
consent flags and still require CORE confirmation. Initial installation requires
the exact local `chatgpt` Install. Explicit update requires the exact candidate
Upgrade and original installed Replaced pair; missing or changed targets are
refused. Both allow at most 64 new hard-dependency Install records, using host
architecture or noarch. Upgrading/replacing existing dependencies, duplicate
names/paths, removals, weak dependencies and groups are refused. Each dependency
must be absent before replay; an existing
package causes refusal rather than replacement. Uninstall leaves dependencies alone.
An identical target is a verified no-op before elevated staging: it does not
query, install or repair dependencies. This is not a general package repair tool.

During store, DNF5 may consult administrator-enabled repositories, refresh
metadata and download files before ATM can validate the resulting plan. This
includes normal repository-metadata signing-key handling. The plan-count bound
is **not a disk/download quota**. ATM does not add repositories or override
versionlocks/excludes. Stored files require private root staging, regular-file
identity, matching RPM headers and repeated SHA-256 continuity checks. Replay
disables external repositories and permits no extra actions. Afterwards every
new dependency must have exactly the planned installed identity; ambiguity or
diagnostics retain the uncertain journal/lock. Schema4 records initial installation;
schema5 records dependency-enabled update and its original target baseline.
Both record dependency count and plan hash; schemas1/2/3/4 remain readable.

### Installation And Update With Dependency Upgrades

Two additional menu actions add `--accept-dependency-upgrades` alongside
`--accept-dependencies`, download/system consent and CORE confirmation. The older
dependency-only actions remain new-installs-only. The broader mode allows at most
64 dependency names in total: new Install records or distinct, same-name,
same-architecture Upgrade/Replaced pairs. The trusted DNF solver supplies upgrade
ordering. Outgoing dependencies must come from `@System` without payload paths;
incoming records retain the wrapped-repository and unique staged-file requirements.
Downgrade, reinstall, obsoletes, removals, unpaired records and cross-name or
cross-architecture replacements are unsupported.

Original dependency identities come from the stored plan and must match exactly
one installed record after store and immediately before replay. This is not a
pre-resolution system snapshot. Incoming identities must match after replay;
concurrent changes or ambiguous queries retain failure evidence, never select a
new baseline. These dependencies may be shared with other applications, and their
upgrades or privileged package scripts can affect those applications and services.
There is no automatic rollback or exclusion against other package frontends.

Schema6 records install/update, total dependency count and upgrade count, retaining
schemas1..5 read compatibility. An identical target update still stops before
elevation with zero dependency counts: it does not repair dependencies. All
signature prerequisites and retained-artifact checks below remain mandatory.

Review-only example (the preview setup is described below):

```bash
preview_atm --dry-run plugin ai update --provider codex-desktop-rpm \
  --trust-download --accept-system-packages --accept-dependencies \
  --accept-dependency-upgrades
```

Actual acceptance remains Pending on a disposable Fedora VM. Removing `--dry-run`
requests a system operation and requires confirmation; automated tests use fixtures
only. See [DNF5 upgrade](https://dnf5.readthedocs.io/en/latest/commands/upgrade.8.html)
and [stored replay](https://dnf5.readthedocs.io/en/latest/commands/replay.8.html).

**Signature prerequisite:** stored repositories are reconstructed as command-line
repositories, not with their original source policy. Therefore this mode requires
`localpkg_gpgcheck=true` (or `1`) already enabled by the administrator and signing
keys already trusted by RPM. The requirement applies to **all RPMs, including
chatgpt**: unsigned or untrusted packages fail; ATM never disables checking to
make the operation succeed. There is no claim that the current upstream RPM
meets this prerequisite. Source: [DNF5 stored repository reconstruction](https://github.com/rpm-software-management/dnf5/blob/main/libdnf5/repo/repo_sack.cpp)
and [package signature policy](https://github.com/rpm-software-management/dnf5/blob/main/libdnf5/rpm/package.cpp).
Repository downloads/metadata keys may already have changed caches before a
later refusal. Package scripts and trusted plugins retain privileged side effects.

This mode requires dependency repository IDs wrapped as `@stored_transaction(id)`.
Older DNF5 plans with raw repository IDs are refused: replay can reuse a configured
repository and its different signature/key policy. Default single-package mode
keeps its existing compatibility; the stricter dependency mode does not silently
relax checks for older DNF5. See [DNF5 5.2.17 repository lookup](https://github.com/rpm-software-management/dnf5/blob/5.2.17.0/libdnf5/repo/repo_sack.cpp).

On a disposable supported Fedora VM, use this **instead of** the default initial
installation, before chatgpt is installed:

```bash
preview_atm --dry-run plugin ai install --provider codex-desktop-rpm \
  --trust-download --accept-system-packages --accept-dependencies
# Actual VM system operation; inspect signature policy/trusted keys first.
preview_atm plugin ai install --provider codex-desktop-rpm \
  --trust-download --accept-system-packages --accept-dependencies
```

For a separately confirmed update of an installed target in that disposable VM:

```bash
preview_atm --dry-run plugin ai update --provider codex-desktop-rpm \
  --trust-download --accept-system-packages --accept-dependencies
# Actual VM system operation; signature prerequisites remain mandatory.
preview_atm plugin ai update --provider codex-desktop-rpm \
  --trust-download --accept-system-packages --accept-dependencies
```

DNF5's [upgrade solver](https://dnf5.readthedocs.io/en/latest/commands/upgrade.8.html)
determines RPM ordering. ATM does not approximate it with semantic versions or
allow downgrade, reinstall, implicit target installation or relaxed replay flags.

### Transaction Boundaries

The approved boundary is **DNF5's own transaction coordination plus before/after
checks**, not an exclusive guard against concurrent RPM/other package frontends.
Do not run another package manager during the operation. Trusted system plugins
and RPM scripts still execute with privilege and may configure repositories or
services. Failure/cancellation can leave partial effects or privileged descendants;
ATM does not claim rollback or terminate unrelated processes.

Journal, private input (install/update only) and root transaction are retained for inspection, even
after success; `/run` staging is temporary across reboot. Failed/uncertain
operations retain the ATM lock and require manual inspection before retry.
Recorded paths never authorize automatic cleanup. Menu/status read historical
ATM evidence only, without running RPM/DNF or proving current package health.
The app is never launched and sign-in remains interactive.
All five menu actions are offered without probing RPM; eligibility is checked only
after explicit operation consent, not inferred from historical journal evidence.

With the isolated helper below, on a **disposable supported Fedora VM only**:

```bash
preview_atm plugin ai menu codex-desktop-rpm
preview_atm --dry-run plugin ai install --provider codex-desktop-rpm \
  --trust-download --accept-system-packages
# This changes actual VM system packages; isolated HOME does not contain that effect.
sudo -v
preview_atm plugin ai install --provider codex-desktop-rpm \
  --trust-download --accept-system-packages
preview_atm status ai
# Update an existing RPM package in this disposable VM, with separate confirmation.
preview_atm --dry-run plugin ai update --provider codex-desktop-rpm \
  --trust-download --accept-system-packages
preview_atm plugin ai update --provider codex-desktop-rpm \
  --trust-download --accept-system-packages
# Separate optional removal test; this changes actual VM system packages.
preview_atm --dry-run plugin ai uninstall --provider codex-desktop-rpm \
  --accept-system-packages
preview_atm plugin ai uninstall --provider codex-desktop-rpm \
  --accept-system-packages
```

DNF4 and Arch remain Pending. Dependency upgrades require the additional consent
described above; cross-name/cross-architecture replacements remain unsupported.
Automated validation uses synthetic packages and mocked tools, never real RPM
transactions. Actual Fedora/GUI/ARM64 acceptance remains Pending. This preview
does not change the production AI manifest or native plugin count.

## Isolated Preview Test

From the ATM source repository, create a separate runtime. These commands build
the preview but do not install a provider or replace the production manifest:

```bash
preview=$(mktemp -d /tmp/atm-ai-preview.XXXXXX)
arch=$(go env GOARCH)
(
  umask 077
  mkdir -p -m 700 "$preview/runtime/plugins/ai/bin/linux-$arch" \
    "$preview/apps" "$preview/user" "$preview/config" "$preview/state" \
    "$preview/xdg/config" "$preview/xdg/data" "$preview/xdg/cache" "$preview/xdg/state"
  cp core-api.json "$preview/runtime/"
  cp -R lang "$preview/runtime/"
  cp plugins/ai/tests/native-preview.json "$preview/runtime/plugins/ai/plugin.json"
  go build -o "$preview/atm" ./cmd/atm
  go build -o "$preview/runtime/plugins/ai/bin/linux-$arch/atm-ai" ./plugins/ai/native
)
preview_atm() {
  env HOME="$preview/user" \
    XDG_CONFIG_HOME="$preview/xdg/config" XDG_DATA_HOME="$preview/xdg/data" \
    XDG_CACHE_HOME="$preview/xdg/cache" XDG_STATE_HOME="$preview/xdg/state" \
    ATM_HOME="$preview/runtime" ATM_PLUGIN_DIR="$preview/runtime/plugins" \
    ATM_CONFIG_DIR="$preview/config" ATM_STATE_DIR="$preview/state" \
    ATM_CONFIG_FILE="$preview/config/atm.conf" \
    ATM_PLUGINS_CONFIG_FILE="$preview/config/plugins.conf" \
    ATM_APPS_DIR="$preview/apps" ATM_LANG=en-us ATM_DRY_RUN=0 \
    "$preview/atm" --home "$preview/runtime" "$@"
}
preview_atm status ai
preview_atm plugin ai
```

This preview manifest supports Linux only. An arm64 cross-build is not an arm64
runtime test. Do not copy the preview manifest over the production manifest.
Keep using `preview_atm` below so HOME, XDG directories, CORE preferences and
plugin state remain isolated. The setup's private `umask` applies only inside
the subshell; it does not change your terminal's existing `umask`.

Explicit CLI examples, using that isolated runtime:

```bash
# Read-only provider menu; metadata only, no release lookup or app execution.
preview_atm plugin ai menu picoclaw

# Optional human installation test: downloads trusted upstream executable code.
# Omit --yes so CORE asks for confirmation.
preview_atm plugin ai install --provider picoclaw --trust-download

# Substitute a version actually installed in this preview.
preview_atm plugin ai use --provider picoclaw --version X.Y.Z

# Optional, separately confirmed PATH edits in this temporary HOME only.
preview_atm plugin ai integrate --provider picoclaw
preview_atm plugin ai integrate --provider picoclaw --remove-path

# Removes only unchanged receipt-owned preview versions; preserves personal data.
preview_atm plugin ai uninstall --provider picoclaw
```

Substitute `codex-cli`, `hermes-desktop` or `ollama` for other portable lifecycle
providers, but do not use `integrate` for Hermes Desktop. Install
and update accept `--version latest` (default) or exact `X.Y.Z`; update refuses
downgrades. `use` and `remove` require exact installed versions. `status` takes no
arguments; `menu` takes no arguments or exactly one provider ID. Version status
comes from local receipts, not from executing installed applications, and does
not claim that their dependencies or interactive runtime have been validated.

### System DEB Human Test: Disposable VM Only

**A temporary HOME, XDG or Apps directory does not sandbox APT.** Running this
route changes the VM/host's real package database and system files. Do not use
the system installation example on a workstation as an "isolated" preview test.
Use a disposable Linux amd64 Debian/Ubuntu-family VM with a snapshot, working
APT sources, compatible dependencies and no custom preference rules. Build the
isolated runtime above inside that VM. First inspect the offline menu:

```bash
preview_atm plugin ai menu hermes-desktop-system
```

Only after accepting the VM's system changes, authenticate separately when
required by sudo policy, and invoke installation without `--yes` so CORE asks
for confirmation:

```bash
# Human action in the disposable VM, not performed by ATM.
sudo -v
preview_atm plugin ai install --provider hermes-desktop-system \
  --trust-download --accept-system-packages

# Exact release, as a separate optional test:
preview_atm plugin ai install --provider hermes-desktop-system \
  --version 0.7.7 --trust-download --accept-system-packages

# Hermes reports its journal/candidates; ChatGPT also reads the dpkg status file.
preview_atm status ai
```

The sudo policy must permit each literal staging/checksum/package-manager and
cleanup command; cached authentication alone does not guarantee permission.
Manually verify the package in the VM and launch the application separately if
desired. Restore the VM snapshot after testing. This documentation is not a
claim of actual APT, GUI or arm64 runtime acceptance.

## Tests and Sources

```bash
go test ./plugins/ai/native -count=1
go test -race ./plugins/ai/native -count=1
go vet ./plugins/ai/native
ATM_TEST_HERMES_PATH_TRACE=1 go test ./plugins/ai/native -run '^TestHermesPATHCompiledLifecycle$' -count=1 -v
python3 -B tests/check_ai_hermes_launch.py /path/to/compiled/atm-ai
# Optional network check: release metadata only, no bundle download or execution.
ATM_TEST_AI_PUBLIC_ROCM=1 go test ./plugins/ai/native -run '^TestOfficialOllamaROCmMetadata$' -count=1 -v
```

Normal tests use synthetic ELF/DEB bytes, in-memory HTTP fixtures, recording
substitutes for system commands and temporary installation directories only.
ChatGPT Desktop tests cover HTTP length/origin constraints, Debian metadata,
package-record ambiguity, consent, existing-state preservation, privileged copy
checks, package postconditions and retained failure/cancellation evidence.
Update checks additionally cover the original package baseline through both guards,
Debian version ordering, equal-version no-op, downgrade refusal and operation-bound
acknowledgements. Neither synthetic success nor cross-compilation proves a real
privileged update or GUI works on a target host.
Removal tests independently check architecture-qualified single-action plans,
effective nonpurge policy, canonical metadata and phase-specific acknowledgements,
changed original package records, cancellation and retained uncertain evidence.
Compiled CLI tests exercise missing consent, invalid flags, cancellation and
dry-run only; they never invoke a consented live system uninstall.
Ollama tests use tar.zst bin/lib fixtures, library/helper preservation, legitimate
link chains, malformed trees, exact response/hash limits, decoder bounds,
interrupted cleanup and a compiled test ELF read only as fixture data. Parent
review tests independently cover modified libraries and staged selection-link
substitution. ROCm coverage includes paired release evidence, cross-archive
collisions, shared extraction limits, schema compatibility, variant-preserving
updates and failed updates that leave the active tree unchanged. Compiled CLI
fixtures cover mixed base/ROCm stores, selection, PATH and guarded removal.
Neither fixture binaries nor helpers are executed as providers.
Discovery tests inject synthetic HOME/PATH/system roots, including broken/cyclic
links, FIFOs, permissions, search bounds, cancellation and preview aliases.
Integration tests use temporary profile fixtures, receipt/link corruption,
cross-provider/cross-root locking, cancellation and intervening-edit recovery.
Shell quoting is checked with the existing Go parser/expander without executing
a shell. Foreign-owner creation is skipped when the test user is not root;
tests never elevate to create that fixture.
They never run provider code, real sudo/APT or change system packages. The
Hermes launch tests use injected command results and harmless test-process helpers
for terminal/process behavior; compiled guard tests trace refusals and dry-run.
The compiled PATH lifecycle builds two distinct real multi-MiB native images,
enrolls and refreshes both enrolled and detached copies only in a temporary HOME,
checks verified same-image no-op and exact preservation of profiles/selection,
and rejects actual launch through a detached alias before upstream execution.
It also exercises enrolled, detached and bridge-free deactivation, repeated no-op
and offline reactivation while checking every synthetic prepared file, directory
and link for unchanged bytes, mode, identity and modification time.
Its optional trace check requires `strace` and verifies no child/network activity
for those operations. Unit checks also reject a mismatched running executable,
interrupted HOME PATH operations and altered enrollment profiles.
These checks are not actual Hermes runtime or dependency execution. The
separate opt-in public metadata test performs read-only official
release lookups, not provider downloads or execution.
The separate `ATM_TEST_AI_PUBLIC_ARCHIVE=1` opt-in test downloads and parses a
real PicoClaw archive in private temporary storage to check compatibility. It
does not install or execute PicoClaw and is not a runtime acceptance test.

- [PicoClaw installation](https://docs.picoclaw.io/docs/installation/)
- [PicoClaw project and optional WebUI](https://github.com/sipeed/picoclaw)
- [Hermes Desktop releases](https://github.com/fathah/hermes-desktop/releases/)
- [Codex CLI documentation](https://learn.chatgpt.com/docs/codex/cli)
- [Codex standalone releases](https://github.com/openai/codex/releases/)
- [ChatGPT Desktop with Codex on Linux](https://learn.chatgpt.com/docs/linux/linux-app)
- [APT frontend locking implementation](https://github.com/Debian/apt/blob/main/apt-pkg/deb/debsystem.cc)
- [APT package hook ordering](https://github.com/Debian/apt/blob/main/apt-pkg/deb/dpkgpm.cc)
- [Ollama Linux installation and base bundles](https://docs.ollama.com/linux)
- [Ollama releases](https://github.com/ollama/ollama/releases/)

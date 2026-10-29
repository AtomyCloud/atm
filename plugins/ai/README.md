# AI Tools

Version: `0.0.1`.

The production AI plugin still uses its existing Bash implementation. The Go
migration is **in progress**, not a completed plugin migration. The production
`plugin.json` is intentionally unchanged; normal ATM builds and packages do not
activate the preview described below.

## Native Preview

Portable providers are implemented for Linux amd64 and arm64; the system DEB
route supports Linux amd64 only:

| Provider | Artifact | Native operations |
| --- | --- | --- |
| PicoClaw CLI | Official architecture-specific `tar.gz` | Install, update, change active version, remove inactive version, uninstall owned preview versions, explicit user PATH integration |
| Hermes Desktop portable | Official architecture-specific AppImage | Install, update, change active version, remove inactive version, uninstall owned preview versions; no PATH integration |
| Codex CLI standalone | Official architecture-specific musl `tar.gz` | Same lifecycle and explicit user PATH integration as PicoClaw |
| Hermes Desktop system | Official amd64 DEB | Install latest or exact stable version with explicit system-package consent |
| Ollama portable | Official architecture-specific base `tar.zst` bundle | Install, update, change active version, remove inactive version, uninstall owned preview versions, explicit user PATH integration |

For PicoClaw and Codex portable archives, only the standalone executable is installed.
PicoClaw's
optional `picoclaw-launcher` WebUI, sample configuration and documentation are
not copied into the installation. PicoClaw CLI does not require that launcher.
Codex CLI uses the standalone release binary, not npm in this preview.

Pending before the entire AI plugin can switch to native Go:

- Hermes Agent's supported package-management/bootstrap workflow.
- Ollama system installation, runtime/service acceptance and GPU add-on workflows;
  portable base-bundle lifecycle is implemented separately below.
- Codex Desktop Linux package management; the Arch shell bootstrap is not ported.
- PATH registration for providers other than the owned PicoClaw, standalone
  Codex CLI and Ollama previews, including external/legacy installations, is not implemented.
- PicoClaw WebUI/launcher support if added to the native provider scope.
- Real provider/runtime acceptance, Linux arm64 execution, other platforms and
  translations of the preview's English labels.

The preview does not fall back to Bash or an upstream installer script for any
of these pending operations. Unsupported providers fail explicitly.

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
Codex Desktop remains explicitly
unknown because the legacy check queries a package database, which discovery
does not do; `codex` is not treated as Codex Desktop evidence.

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

Provider ID: `ollama`. This isolated preview implements native portable lifecycle,
not the upstream system installer or service setup. It preserves the complete
official `bin/ollama` and `lib/ollama` base tree, including library links, helper
executables and licenses. Keeping these paths adjacent matters: Ollama resolves
its executable path to locate sibling libraries. No downloaded shell is executed.

The [official Linux guide](https://docs.ollama.com/linux) documents `.tar.zst`
base bundles for amd64 and arm64. ATM resolves stable `ollama/ollama` GitHub
releases, selecting exactly `ollama-linux-amd64.tar.zst` or
`ollama-linux-arm64.tar.zst` with the release asset's SHA-256 digest and exact
size. `latest` and custom stable `X.Y.Z` are supported; prereleases, older release
formats, missing digests and alternative URLs are refused. Separate ROCm,
JetPack and MLX assets are not selected. GitHub metadata plus its fixed HTTPS
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

Compressed downloads are bounded to **4 GiB**, and the entire decompressed tar
stream (including headers/padding) to **12 GiB**, with at most 10,000 archive or
inventory entries. Paths are at most 1024 bytes/32 separators. The dedicated
extractor uses `github.com/klauspost/compress/zstd`, single concurrency, low-memory
mode and a 128 MiB decoder window/memory limit. Exact response size and SHA-256
must match before extraction; tar, zstd checksum and cancellation errors refuse
publication. A single leading `./` and an optional root directory are accepted.
Traversal, duplicate normalized names, hardlinks, sparse/special files, unsafe
modes and entries outside the bin/lib layout are rejected. Library symlinks are
created only after checking the full graph for escapes, dangling targets, cycles,
ancestor-directory targets and links used as extraction parents.

The download timeout is ten minutes, within CORE's existing fifteen-minute
operation timeout. There is no download resume. Bounds are **per bundle**, not
a total-operation I/O budget: mutations repeatedly rehash the store and selected
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
switching or removing it.** ATM does not probe, start or stop Ollama processes or
existing services. No model download, GPU setup, service/user creation, system
package operation or global PATH write occurs. Install, update and use never edit
profiles. Explicit `integrate` enables only the owned preview command in user
profiles; uninstall detaches that exact integration before removing versions.
Existing external/legacy installations, models, credentials and
settings remain untouched. A separate explicit runtime invocation can change
user data or conflict with an already running server; review upstream settings
and existing servers yourself first.

With the isolated `preview_atm` helper below, available commands are:

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
full `current/bin/ollama` path: `serve`, `run llama3` and `pull llama3`. ATM never
runs these commands. `run`/`pull` may download models independently of the bundle
installation. Real upstream bundle download/extraction and runtime acceptance,
active-process handling, GPU behavior and Linux arm64 execution remain Pending;
synthetic archives and public metadata checks do not establish those claims.

## Explicit CLI PATH Integration

Only the owned PicoClaw, standalone Codex CLI and Ollama previews can be integrated:

```text
integrate [--provider picoclaw|codex-cli|ollama] [--remove-path]
```

Use `preview_atm plugin ai integrate ...` with the isolated runtime below.
CORE confirmation or its explicit `--yes` authorizes this mutation; no download
or system-package flags apply. `--version` is not accepted. Without a provider,
the operation processes selected owned PicoClaw, Codex CLI, then Ollama previews.
The generic `preview_atm path apply --plugins` route also includes these owned
previews through provider-less integration. Missing
previews are skipped, but unsafe/foreign stores are refused, never adopted.
Each provider commits separately: if a later provider fails, earlier completed
providers remain changed and the error lists them. There is no aggregate rollback.

Integration creates `native-v1/bin/picoclaw -> ../current`,
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

`--remove-path` detaches only exact owned blocks, the command link and receipt;
it retains all provider versions and the empty bin directory. Uninstall performs
that same guarded detach before deselecting or deleting versions, or refuses
before destructive work. Failures after detach can leave PATH detached while
versions remain; no whole-uninstall rollback is promised.

The existing sibling `native-v1.lock/` and shared `$HOME/.atm-ai-path.lock/`
(0700) are fail-fast locks. The HOME lock coordinates all three CLI providers and
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

# This reports the journal and unverified candidates; no package query.
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
```

Normal tests use synthetic ELF/DEB bytes, in-memory HTTP fixtures, recording
substitutes for system commands and temporary installation directories only.
Ollama tests use tar.zst bin/lib fixtures, library/helper preservation, legitimate
link chains, malformed trees, exact response/hash limits, decoder bounds,
interrupted cleanup and a compiled test ELF read only as fixture data. Parent
review tests independently cover modified libraries and staged selection-link
substitution. Neither fixture binaries nor helpers are executed as providers.
Discovery tests inject synthetic HOME/PATH/system roots, including broken/cyclic
links, FIFOs, permissions, search bounds, cancellation and preview aliases.
Integration tests use temporary profile fixtures, receipt/link corruption,
cross-provider/cross-root locking, cancellation and intervening-edit recovery.
Shell quoting is checked with the existing Go parser/expander without executing
a shell. Foreign-owner creation is skipped when the test user is not root;
tests never elevate to create that fixture.
They never run provider code, real sudo/APT or change system packages. The
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
- [Ollama Linux installation and base bundles](https://docs.ollama.com/linux)
- [Ollama releases](https://github.com/ollama/ollama/releases/)

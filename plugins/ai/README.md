# AI Tools

Version: `0.0.1`.

The production AI plugin still uses its existing Bash implementation. The Go
migration is **in progress**, not a completed plugin migration. The production
`plugin.json` is intentionally unchanged; normal ATM builds and packages do not
activate the preview described below.

## Native Preview

Implemented for Linux amd64 and arm64:

| Provider | Artifact | Native operations |
| --- | --- | --- |
| PicoClaw CLI | Official architecture-specific `tar.gz` | Install, update, change active version, remove inactive version, uninstall owned preview versions |
| Hermes Desktop portable | Official architecture-specific AppImage | Same operations |
| Codex CLI standalone | Official architecture-specific musl `tar.gz` | Same operations |

Only the standalone executable is installed from each archive. PicoClaw's
optional `picoclaw-launcher` WebUI, sample configuration and documentation are
not copied into the installation. PicoClaw CLI does not require that launcher.
Codex CLI uses the standalone release binary, not npm in this preview.

Pending before the entire AI plugin can switch to native Go:

- Hermes Agent's supported package-management/bootstrap workflow.
- Ollama installation, runtime and system-service behavior.
- Hermes Desktop system DEB installation.
- Codex Desktop Linux package management; the Arch shell bootstrap is not ported.
- Native PATH registration and detection of existing legacy/manual installations.
- PicoClaw WebUI/launcher support if added to the native provider scope.
- Real provider/runtime acceptance, Linux arm64 execution, other platforms and
  translations of the preview's English labels.

The preview does not fall back to Bash or an upstream installer script for any
of these pending operations. Unsupported providers fail explicitly.

## Storage and Safety

Each provider uses a new private directory beneath `ATM_APPS_DIR`:

```text
ai/picoclaw/native-v1/
hermes/hermes-desktop/native-v1/
ai/codex/cli/native-v1/
```

Each root holds `owner.json`, `versions/<X.Y.Z>/{tool,receipt.json}` and a
`current` symlink to `versions/<X.Y.Z>/tool`. **`current` points directly to an
executable**, not a directory. Invoke the exact path printed after selection.
ATM does not execute a downloaded application, launch a browser, authenticate,
edit PATH or shell profiles, install dependencies, or configure system services.
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

Downloads and archive extraction are bounded. Archive extraction uses ATM's
shared native installation library, rejecting traversal, links, duplicates and
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
# Read-only provider menu; no release lookup or executable probing.
preview_atm plugin ai menu picoclaw

# Optional human installation test: downloads trusted upstream executable code.
# Omit --yes so CORE asks for confirmation.
preview_atm plugin ai install --provider picoclaw --trust-download

# Substitute a version actually installed in this preview.
preview_atm plugin ai use --provider picoclaw --version X.Y.Z

# Removes only unchanged receipt-owned preview versions; preserves personal data.
preview_atm plugin ai uninstall --provider picoclaw
```

Substitute `codex-cli` or `hermes-desktop` for other portable providers. Install
and update accept `--version latest` (default) or exact `X.Y.Z`; update refuses
downgrades. `use` and `remove` require exact installed versions. `status` takes no
arguments; `menu` takes no arguments or exactly one provider ID. Version status
comes from local receipts, not from executing installed applications, and does
not claim that their dependencies or interactive runtime have been validated.

## Tests and Sources

```bash
go test ./plugins/ai/native -count=1
go test -race ./plugins/ai/native -count=1
```

Normal tests use synthetic ELF files, in-memory HTTP fixtures and temporary
installation directories only. They never run provider code or change system
packages. The separate opt-in public metadata test performs read-only official
release lookups, not provider downloads or execution.
The separate `ATM_TEST_AI_PUBLIC_ARCHIVE=1` opt-in test downloads and parses a
real PicoClaw archive in private temporary storage to check compatibility. It
does not install or execute PicoClaw and is not a runtime acceptance test.

- [PicoClaw installation](https://docs.picoclaw.io/docs/installation/)
- [PicoClaw project and optional WebUI](https://github.com/sipeed/picoclaw)
- [Hermes Desktop releases](https://github.com/fathah/hermes-desktop/releases/)
- [Codex CLI documentation](https://learn.chatgpt.com/docs/codex/cli)
- [Codex standalone releases](https://github.com/openai/codex/releases/)

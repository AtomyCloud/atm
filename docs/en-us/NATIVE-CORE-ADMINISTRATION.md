# Native CORE Administration

ATM 0.0.2 migration build, Patch v27.5. CORE administrative commands now dispatch
to Go services rather than `bash bin/atm`. This does not claim that all application
plugins or production self-update have migrated. No installed copy is replaced
merely by starting the new executable.

## Commands

```bash
atm updates check
atm plugins check-updates
atm self-update --check
atm updates changes
atm updates backups
atm updates plugin brave
atm plugins update brave
atm updates all
atm plugins update --all
atm updates restore <native-backup-id>
atm updates recover plugins
atm updates verify brave <official-main-ancestor-commit>
atm path show
atm path apply
atm setup portable --manifest-sha256 <independently-approved-release-json-sha256>
atm install
```

Global flags precede commands. `--dry-run --json` returns an offline plan with no
network, processes or writes. Mutations require interactive confirmation or
`--yes`; noninteractive/JSON mutations require `--yes`. Core results are structured
JSON, including partial failures. Application-native operations retain their
separate JSONL protocol. `--timeout` bounds native service work; legacy interactive
application installers retain their existing terminal/process behavior.

The TUI CORE menu exposes checks, single/all plugin updates, changes, backups,
restore/recovery, setup and ATM PATH. Prepared plugin updates display pinned
revisions before confirmation, defaulting to Cancel. Cancel/quit waits for native
service cleanup. A successful plugin replacement requires restart before running
the changed plugin in that TUI session. Available plugin updates show a blue badge
after a check. Nothing silently installs applications during a code update.

## Plugin Code Updates

Only `AtomyCloud/atm`, branch `main`, directory `plugins` is supported. Inspection
uses pinned Git trees/blobs, not version labels alone. States distinguish current,
available, modified, unmanaged, incompatible, removed upstream, ahead/diverged
and unknown/offline. Compiled CORE is reported separately as unknown pending its
approved release identity and signing policy.

ALL includes installed disabled plugins and does not install new catalog entries
or update CORE. Every prepared plugin is an independent transaction with a result;
batch failure is not reported as overall success. Official origin receipts and
local files are checked, local edits block replacement, unrelated local files are
preserved, and unsafe links/path collisions/API mismatches are refused. Updates
inside Git worktrees are refused; use normal Git workflows for a source checkout.

Replacement uses bounded verified staging, a private backup, pending journal,
non-replacing directory publication and readback checks. Interrupted transactions
block plugin execution until explicit recovery. Recovery/restore reject changed
backups, changed current payloads and mismatched roots. Do not manually delete a
pending journal or lock to force an update.

State uses `ATM_STATE_DIR`, including the legacy literal setting. Its Unix default
is `~/.local/share/atm`; Windows defaults under ATM's configuration directory.
The identity includes runtime/plugin roots, so different installations are kept
separate. Existing origin receipts are supported; explicit baseline verification
never assumes that matching a version string establishes ownership. Native code
backups use a separate format/location. Old Bash backup restoration is not
silently reinterpreted as native recovery; retain the legacy tooling for it.

This is not a hostile-same-user filesystem sandbox or a power-loss guarantee.
Interrupted staging/locks may require inspection. Backup and receipt contents are
validated before recovery; no downloaded code is executed during inspection.

## Portable Setup And PATH

Setup accepts an extracted, reviewed distribution produced by the package tools,
not an arbitrary source checkout. The explicit manifest SHA-256 must be obtained
from an independently approved package/local build. Computing a hash of an
untrusted download does not authenticate it. This local-build approval mechanism
is separate from the production Ed25519 updater trust root.

The default destination is `~/Apps/atm`, respecting a configured Apps directory
within the selected user's home. Existing destinations are preserved, not adopted
or overwritten. The compiled executable is at the package root (`atm` or
`atm.exe`), not the legacy `bin/atm` shell entrypoint.

`path apply` requires a verified native setup receipt. Unix shell integration uses
managed blocks and private backups without discarding unrelated profile content;
Windows uses the current user's PATH, without administrator privileges. Open a
new terminal to observe changes. No `/etc/profile.d` or system installation is
required. This command currently integrates ATM only: it does not claim that
application PATH entries, VS Code CLI links or plugin desktop launchers migrated.
Those plugin-owned integration contracts remain pending.

## Full Setup

Optional `full_setup: true` selects a plugin. CORE orders enabled selected plugins
by metadata, executes the available install operation and retains per-plugin
outcomes. Unavailable plugins are reported; a failure stops later installs, which
are marked not run. Native fixtures work with no shell or Python on PATH.

On Linux, existing opted-in legacy application plugins still use the explicit
application bridge and may prompt for sudo. This is not a transaction that can
roll back all applications. The CORE selection/loop is Go; application-specific
installers remain separate migration work. On Windows/macOS, unavailable Linux
plugins are not run and their absence is not reported as a successful Full Setup.

## Compiled CORE Trust Gate

The maintainer approved the Ed25519 public key and full-package update scope on
2026-10-27. The embedded SSH key fingerprint is:

```text
SHA256:jNhv8Ehni4ENGvQaar4/l8vQQPHcVZDnCmcmTvkFXVM
```

Inspect it without network, shell commands or writes:

```bash
atm updates trust
atm --json doctor
atm updates verify-package manifest.json manifest.sig atm-0.0.3-linux-amd64.tar.gz
```

The last command is an example with explicit local files, not an available release.
It verifies the signature, matching host platform, archive hash, bounded safe
archive structure, signed `release.json` hash and complete indexed payload.
It does not extract, install, execute or establish that the release is the latest.
There is no CLI/config/environment key override. `--dry-run` does not read inputs.

The target is now the complete runtime package, including bundled plugin code,
not only the ATM executable. Package signatures have their own domain and schema;
old binary-only manifests cannot authorize a full-package update. Private data,
unmanaged files, edited configuration and independently updated plugins require
explicit conflict handling before production replacement can be enabled.

`updates core` still fails closed, including with `--yes`: approved identity is
not the same as completed installation/receipt/recovery integration. No private
key was accessed, created or published. See [release signing](RELEASE-SIGNING.md)
for the exact format and offline preparation tool.

The earlier binary-only library remains experimental. Its Linux/macOS transactions
can now be explicitly finalized into retained history after verification, allowing
another transaction with a separately trusted baseline. It is not wired into the
full-package updater. Windows replacement, production discovery, installed baseline,
conflict policy, standalone recovery and release signing remain pending.

## Validation Boundaries

Go tests exercise temporary runtime, state, profile and backup roots, including
corruption, cancellation, local edits and interrupted publication. CLI/TUI tests
cover offline plans, consent and shell-free native routes. Cross-build/package
checks cover Linux amd64/arm64, Windows amd64 and macOS Intel/Apple Silicon.
Cross-compilation is not real platform acceptance. Previously published workflow
results are not validation of unpushed local changes. Release publication and
replacement of a real installed ATM remain separate owner approvals.

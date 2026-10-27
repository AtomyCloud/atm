# Native CORE Administration

ATM 0.0.2 migration build, Patch v27.11. CORE administrative commands dispatch
to Go services rather than `bash bin/atm`. Linux native implementation and automated
acceptance are tracked separately from final human acceptance and release approval.
Application backend ports are a separate scope. No installed copy is replaced
merely by starting the new executable. See the
[Linux acceptance checklist](LINUX-CORE-ACCEPTANCE.md) for the isolated human test.

## Commands

```bash
atm updates check
atm updates check-core
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
atm path apply --plugins
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
and unknown/offline. Compiled CORE is checked separately against authenticated
release metadata after a signed installed baseline exists. Missing enrollment,
damaged/unknown state and pending recovery are not reported as current.

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

`path apply` requires a verified native setup or signed package receipt. The
destination recovery guard and indexed CORE hashes are checked before writes;
independently updated plugins are not ATM PATH inputs. Unix shell integration uses
managed blocks and private backups without discarding unrelated profile content;
Windows uses the current user's PATH, without administrator privileges. Open a
new terminal to observe changes. No `/etc/profile.d` or system installation is
required. This command integrates ATM only.

`path apply --plugins` and the TUI integration action dispatch each plugin's
optional native schema2 `integrate` operation in metadata order, after consent.
Unavailable/legacy backends are reported, failures stop the batch and an empty
or all-unavailable selection is not success. No Bash integration fallback is
used. The generic contract is implemented; existing application PATH/CLI/desktop
backends still need their separate native ports.

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
old binary-only manifests cannot authorize a full-package update. Unknown regular
files/directories are preserved; edited owned files, independently updated
bundled plugins and collisions block replacement.

After the maintainer accepted the signed Linux test and explicitly authorized
activation, these native routes are enabled in source. Every mutation requires
interactive confirmation or `--yes`, with the same signature and ownership checks:

```bash
atm updates enroll-core manifest.json manifest.sig archive.tar.gz
atm updates core
atm updates core manifest.json manifest.sig archive.tar.gz
atm --yes recover-core --root /absolute/path/to/atm
```

Enrollment authenticates the matching current package before registering its
baseline. Updates pin candidate/installed identities before confirmation and
revalidate before publication. Recovery works from a trusted external executable
even when the target root is missing, without loading its config/plugins. Windows
requires that external executable for replacement too; automatic handoff is not
implemented. After replacement or any result requiring recovery, the TUI locks
the session until exit. Other ATM sessions must be stopped before updating.

The signed package must match the enrolled installation. Source checkouts,
unsigned baselines and owned local edits are refused; updates must have a strictly
newer stable version. Approval did not authorize automatic installation, arbitrary
keys, forced replacement or automatic downgrade. `replacement_enabled` reports
the running verifier's capability, not a guarantee that any chosen package/root
is eligible. Package verification itself remains read-only.

The original signed 0.0.2 Linux candidate from commit 6c8b9be is unchanged and
still reports false; a newly built executable reports true. No real installed
ATM or signed artifact was overwritten as part of activation. `doctor` now emits
CORE trust/recovery diagnostics even if plugin discovery fails, with null plugin
count, explicit discovery error and nonzero exit status.

No private-key file was opened, created or published. See
[release signing](RELEASE-SIGNING.md) for formats, preservation limits,
independent plugin receipts and remaining acceptance gates. Signed local
enrollment/inspection and explicit plugin baseline verification remain CLI-only;
the TUI provides discovery/update/trust and plugin recovery. CORE retained-history
selection is not exposed as a menu; `recover-core` addresses the current journal.

The earlier binary-only library remains experimental. Its Linux/macOS transactions
can now be explicitly finalized into retained history after verification, allowing
another transaction with a separately trusted baseline. It is not wired into the
full-package updater. Fixture tests exercise the full-package path independently.
Real Linux package signatures and the human dry-run test were validated. Actual
signed 0.0.2 -> private QA 0.0.3 -> 0.0.2 upgrade/recovery and six process-interruption
scenarios passed on Linux amd64. Publication, actual installed-copy deployment and
other target-host acceptance remain separate and pending.

## Native Route Evidence

The shared dispatcher owns CORE plans for both CLI and TUI; the legacy runner
rejects administrative commands. The explicit application bridge remains separate.

| Area | Native routes / behavior | Regression evidence |
| --- | --- | --- |
| Inspection and preferences | doctor, plugin list/sort, language | CLI Linux administrative matrix; config/i18n tests |
| Official catalog | search/download with pinned metadata and consent | catalog service/CLI/TUI tests; PTY dry-run |
| Component checks | updates check/check-core, self-update --check, plugins check-updates | CLI matrix, release and update service tests |
| Plugin maintenance | single/all, changes/backups, restore/recover plugins, verify baseline | CLI matrix; actual update/restore/recovery tests |
| Signed full packages | trust/verify/enroll, update, early independent recovery | real-signature CLI lifecycle/upgrade and crash acceptance |
| Portable setup and ATM PATH | verified local package and per-user integration | setup receipt/profile/conflict tests; CLI/TUI route matrices |
| Plugin integration | native integrate contract, no Bash fallback | native subprocess integration tests |
| Full Setup | metadata-ordered native orchestration, stop on failure | native request subprocess tests and PTY fixture flow |
| Terminal UX | native plans, Cancel default, input correction, pending cancellation, timeout, cleanup | Go TUI matrix and Linux PTY tests |

`TestLinuxAdministrativeRouteMatrix` runs without a shell and with self-tested
executable traps, checks offline/read-only snapshots and alias normalization.
`TestCoreAdministrationCannotEnterLegacyRunner` guards both legacy entrypoints.
The TUI matrix checks every exposed native CORE action; strace-assisted PTY cases
also assert no child execution or socket connection for offline plans/refusals.
These instrumentation cases explicitly skip if strace is missing; a skip is not
equivalent to successful terminal acceptance.

Final parity fixes: planning now refuses unsupported `updates recover` targets
instead of accepting a command that execution cannot handle, and invalid TUI CORE
input retains focus so the user can correct it. Prepared/cancelled operations no
longer leave a stale running status. The en-us update-check label now names CORE
and plugins. These changes do not relax trust or confirmation requirements.

## Validation Boundaries

Optional real-signature CLI acceptance on a native Linux host:

```bash
ATM_TEST_SIGNED_RELEASE_DIR="$PWD/binary/v0.0.2-6c8b9be" \
  go test ./internal/cli -run '^TestSignedReleaseCLILifecycle$' -count=1 -v
```

This test copies the existing signed artifacts into temporary fixtures, enrolls
the verified package, exercises consent/dry-run/tamper/same-version refusals, then
recovers the exact pre-enrollment tree while retaining the displaced candidate.
It uses the real embedded public key without a fixture-key or gate override.
The actual artifacts, personal configuration and installed ATM are not modified.
This verifies undoing enrollment, not an upgrade between two real release versions.
The separate [two-version tests](LINUX-SIGNED-RELEASE-TEST.md#two-version-acceptance)
cover the latter using the approved private QA artifacts.

Go tests exercise temporary runtime, state, profile and backup roots, including
corruption, cancellation, local edits and interrupted publication. CLI/TUI tests
cover offline plans, consent and shell-free native routes. Cross-build/package
checks cover Linux amd64/arm64, Windows amd64 and macOS Intel/Apple Silicon.
Cross-compilation is not real platform acceptance. Previously published workflow
results are not validation of unpushed local changes. Release publication and
replacement of a real installed ATM remain separate owner approvals.

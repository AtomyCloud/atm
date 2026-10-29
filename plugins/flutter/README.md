# Flutter SDK Plugin

The compiled ATM v0.0.2 runtime uses this plugin's native Go backend on Linux.
Plugin version remains 0.0.1. The separate legacy Bash entrypoint and its
configuration are unchanged; native operations do not source `plugin.conf`.

## Commands

```bash
atm plugin flutter
atm plugin flutter menu --latest
atm install flutter
atm install flutter --version 3.47.8
atm install flutter --channel beta --version 3.50.0-0.2.pre
atm update flutter
atm update flutter --channel stable
atm status flutter
atm use flutter 3.47.8
atm remove flutter 3.47.7
atm plugin flutter integrate
atm uninstall flutter
```

The root menu and status are offline and read-only. Opening the latest-version
page retrieves the latest five unambiguous stable releases compatible with this
host, with a custom exact-version choice. An unavailable index still leaves the
custom choice and a route back to installed versions. Custom versions must exist
in the selected official channel; custom URLs and unofficial builds are not used.
Bare installation selects the newest stable release. An update retains the
active native SDK's recorded channel unless `--channel` is explicit. Updating an
unowned SDK requires an explicit channel. Updates refuse downgrades; use an
explicit installation or select an already installed version to downgrade.

Release discovery uses Flutter's official
[Linux metadata](https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json)
and [SDK archive](https://docs.flutter.dev/install/archive). Identical duplicate
metadata is deduplicated; conflicting artifacts for one version are refused,
without poisoning unrelated releases. An existing native version whose official
artifact has changed is preserved, never overwritten.

## Layout And PATH

```text
<AppsDir>/flutter/<version>/
<AppsDir>/flutter/current -> <version>/
<AppsDir>/flutter/<version>/.atm-flutter-receipt.json
```

Installation and selection change only this portable layout. They do not modify
shell profiles. Explicit `integrate` adds an ATM-owned PATH block to `.profile`,
`.bashrc`, and an existing `.zshrc` under the user's HOME, preserving unrelated
content. Symlinked profiles and malformed/duplicate owned blocks are refused.
Refresh the current shell with the printed PATH command or open a new terminal.
Uninstall removes only this plugin's PATH block when clearing its selection.

Status reads SDK files, the bundled Dart ELF architecture, and native receipts;
it never executes Flutter, Dart, or Git and is not a runtime health check.
External installations outside this managed root are not discovered or changed.
The vendor's own `bin/flutter` remains a shell script: the native ATM installer
does not execute it, but running Flutter yourself still requires Flutter's
documented runtime dependencies. Android SDKs, licenses, compilers, system
packages and user-level Pub caches are not installed or modified by this backend.

## Mutable SDKs And Removal

Flutter changes its caches and Git metadata during normal use. Selection checks
the launcher's initial hash, actual Git HEAD revision, SDK version metadata when
present, and Dart architecture, not an exact full-tree hash. Cache generation,
Git logs, and temporary removal of regenerable version metadata therefore do not
prevent selecting an otherwise identifiable native SDK. A changed revision or
launcher is refused. An invalid current SDK can still be switched away from.
Legacy SDKs with identifiable version metadata can be selected but are never
silently adopted into native ownership or deleted.

Removal is deliberately stricter: every path, hash, mode and link must match the
installation receipt. Any added, changed or removed file, including ordinary
generated cache files, blocks deletion. `uninstall` validates all native versions
before deleting any; it fails rather than silently deleting a modified SDK. Keep
or back up the affected SDK and inspect it manually. There is no force-delete or
automatic cache-cleaning option. Projects, unowned SDKs and unrelated root files
are preserved. A selected version must be switched away from before `remove`.

Do not run Flutter, Git, another ATM mutation or an external editor against an SDK
while installing/removing it. The ATM lock serializes ATM operations, not external
processes. Receipts guard accidental local modification, not a malicious account
owner who can rewrite both the payload and the receipt.

## Safety And Limits

- XZ decoding uses a fixed 64 KiB input buffer to avoid byte-at-a-time file reads.
  It remains native Go; dictionary/stream checks, cancellation, SHA-256, rooted
  extraction, per-file synchronization and ownership inventories are unchanged.
  Decompression still uses CPU; less total CPU time does not imply a lower peak
  utilization while decoding. Network and disk costs are separate.
- Native JSONL-v1 operations, with confirmation and dry-run controlled by CORE;
  no Bash/Python/curl/tar/xz subprocess delegation or installer execution.
- Fixed official HTTPS origin and exact archive path; bounded redirects cannot
  change origin, scheme, path, credentials or query. SHA-256 must match metadata.
  This trusts the official HTTPS metadata; it is not an independent signature.
- Private staging, per-root mutation lock, atomic `current` replacement, safe
  rooted tar extraction, deferred internal links and rejection of escapes,
  cycles, link parents, duplicate paths and special files.
- At most 3 GiB compressed, 8 GiB file contents, 2 GiB per file, 150,000 tar
  entries, 32 MiB receipt and 16 MiB release metadata. Tar stream overhead is also
  bounded. XZ is checked before decoding: at most 4,096 actual LZMA2 blocks and
  64 MiB dictionaries, single stream, no filter chains. XZ CRC/index/footer
  validation remains the library's responsibility. `DictCap` alone is not a cap.
- Downloads have a 30-minute timeout; metadata has a 20-second timeout. CORE's
  configured operation timeout also applies. Cancellation cleans private stages
  and leaves the old selection. A fully published SDK survives activation failure
  for inspection. No writes occur for native dry-run.

Uninstall is not a crash-transactional rollback operation. Interruption can leave
partially removed owned versions, an unselected installation, or a stale lock or
private stage. Inspect and back up affected files before manually clearing a
stale `.atm-native-lock`; do not blindly delete a lock from a live operation.

## Build And Acceptance

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
go test ./plugins/flutter/native
go test -race ./plugins/flutter/native
bash -n plugins/flutter/plugin.sh
```

Complete Linux packages include the compiled backend. A source checkout needs a
local native build; missing binaries never silently fall back to Bash. Linux
amd64 and arm64 backends can be built, but installation requires a matching
official artifact. The inspected Linux index on 2026-10-28 contains x64/legacy
x64 assets, not Linux arm64: ARM64 installation reports unavailable rather than
downloading x64. Windows/macOS backend implementation and all non-amd64 runtime
acceptance remain Pending. Linux human acceptance is separate from fixture tests.

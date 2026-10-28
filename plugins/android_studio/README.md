# Android Studio Plugin

Compiled ATM v0.0.2 uses this native Go plugin on Linux amd64. Plugin version
remains 0.0.1. The separate legacy Bash implementation and configuration are
unchanged. Native operations do not source `plugin.conf` or `.atm-meta`.

## Commands

```bash
atm plugin android_studio
atm install android_studio
atm install android_studio --id quail1_canary2
atm status android_studio
atm use android_studio panda4/2025.3.4.6
atm remove android_studio quail1_canary2/2026.1.1.2
atm plugin android_studio integrate
atm uninstall android_studio
```

The default remains `panda4`, not the newest upstream release. The five pinned
selectors preserve the legacy names and distribution versions:

| Selector | Distribution Version |
| --- | --- |
| panda4 | 2025.3.4.6 |
| quail1_canary2 | 2026.1.1.2 |
| quail1_canary1 | 2026.1.1.1 |
| panda4_rc1 | 2025.3.4.5 |
| panda3_patch1 | 2025.3.3.7 |

Their Linux archive URLs and SHA-256 hashes were verified against the
[official archive](https://developer.android.com/studio/archive) on 2026-10-28.
The native menu and status are offline and read-only. There is no automatic
latest-release lookup or `update` operation. Install another exact edition and
select it to upgrade; old editions remain until explicitly removed.

The custom menu collects name, distribution version, package filename, HTTPS URL
and a mandatory publisher-provided SHA-256, as literal values. CLI equivalent:

```bash
atm install android_studio --id custom \
  --name 'Android Studio Custom' --version 2025.3.4.6 \
  --package android-studio-linux.tar.gz \
  --url 'https://publisher.example/android-studio-linux.tar.gz' \
  --sha256 '<64-character-publisher-sha256>'
```

Supply complete custom name/version/URL/hash options; package defaults to a safe
name derived from the display name and version when omitted. Custom IDs must be
safe lowercase identifiers. Credentials, queries and fragments are not accepted
in archive URLs. Custom downloads are explicitly chosen, not catalog sources.

## Installation And Identity

```text
<AppsDir>/AndroidStudio/studio_<name-slug>_<distribution-version>/
<AppsDir>/AndroidStudio/current -> selected installation
```

Downloads use native HTTPS with exact-origin/path redirect restrictions, SHA-256
verification, private staging and rooted tar.gz extraction. Internal archive
links are resolved safely; traversal, escaping/cyclic links, link parents,
duplicate entries, reserved receipts and special files are refused. Limits are
3 GiB compressed, 8 GiB extracted file contents, 2 GiB per file, 150,000 entries
and a 32 MiB receipt. Tar-stream overhead is bounded separately. Download timeout
is 30 minutes; CORE's operation timeout also applies.

The four-component distribution label is distinct from the IDE's product version
and build. `product-info.json`, `build.txt`, the compatible launcher and bundled
JBR ELF must agree on Android Studio/Linux amd64 identity. Receipts record the
actual IDE version/build and initial inventory. Status and selection do not run
Studio, Java, Bash, Python, tar, curl or any other subprocess. Launcher scripts
shipped by the vendor remain payloads, not ATM installer logic.

Google's [product metadata stamper](https://android.googlesource.com/platform/tools/adt/idea/+/5eef5dac7e7037644690560976ba730748510d3a/studio/stamper.py)
sets `version` to `AI-<buildNumber>`. This exact representation is accepted only
when its numeric build matches both `buildNumber` and `build.txt`. Conventional
three/four-component product versions must match the distribution label. In the
stamped representation, the pinned official URL/hash binds the distribution
label; a custom archive's distribution label is supplied by its user, not inferred
from the stamped build. Receipts retain the actual stamped product version.
This format is evidenced by upstream build sources/tests, not inspection of the
five full archive payloads; those SDK archives were not downloaded during migration.

Identifiable legacy installations retain their `id/version` labels and can be
selected without adoption or deletion. Legacy metadata is parsed with ATM's
structured literal-assignment parser, never executed. Ambiguous duplicate labels
are refused. Existing native installs with different artifact identity are never
overwritten. Versions outside the managed root are not scanned or changed.

## PATH And Desktop

Installation/selection do not initially create desktop entries or edit shell
profiles. Explicit `integrate` adds an owned PATH block to `.profile`, `.bashrc`
and an existing `.zshrc`, plus:

```text
~/.local/share/applications/atm-android-studio-native.desktop
<AppsDir>/AndroidStudio/.atm-desktop.json
```

The desktop uses the stable `current` path. After explicit integration, switching
editions updates an unchanged owned desktop's launcher/icon transactionally before
committing the current link; failure rolls back desktop/receipt changes. Unowned,
changed, symlinked or unexpectedly missing configured entries block replacement.
Run explicit integration to recreate a missing owned entry. Legacy per-edition
desktop files are preserved. No system desktop database commands or sudo are used.

Desktop Exec quoting follows the
[Desktop Entry specification](https://specifications.freedesktop.org/desktop-entry/latest/exec-variables.html),
including percent escaping and separate scalar/command escaping. Paths containing
`=` cannot be desktop executables and cause explicit integration to fail without
preventing portable installation. Integration rollback preserves concurrent
content and permission changes instead of overwriting them.

## Data Preservation And Limits

Normal cache/plugin additions inside an otherwise identifiable SDK do not prevent
selection. Product/build/launcher identity changes do. Removal is stricter:
all files, modes and links must match the native receipt, so additions, edits or
deletions block removal. A selected edition cannot be removed individually.
Uninstall prechecks all owned editions before deletion; modified or unowned
SDKs and unrelated root files are preserved. There is no force-delete option.

External Android settings, projects, SDKs, licenses and user plugin directories
are not modified. This installer does not install system dependencies, run setup
wizards or accept Android licenses. Running the IDE requires its documented OS
dependencies. Close the IDE and avoid other writers during mutation: ATM's lock
does not lock external applications, and receipts are not a security boundary
against an account owner who can rewrite both payload and receipt.

Cancellation cleans private stages and preserves the previous selection. A fully
published SDK survives a later activation failure for inspection. Uninstall is
not crash-transactional: interruption can leave partial removals or a stale lock.
Inspect/back up affected files before manually clearing a stale `.atm-native-lock`.

## Build And Tests

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
go test ./plugins/android_studio/native
go test -race ./plugins/android_studio/native
bash -n plugins/android_studio/plugin.sh
```

Complete Linux amd64 packages include the native backend; source checkouts need
a local build. Missing backends never fall back silently to Bash. Linux ARM64,
Windows and macOS native backends are not declared/implemented in this phase.
Human acceptance remains separate from automated fixture verification.

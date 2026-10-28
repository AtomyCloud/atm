# Android SDK Plugin

Plugin version `0.0.1`. Native Go backend, schema 2 / JSONL-v1, ATM API 2.
The native target is **Linux amd64**. Linux arm64 and Windows/macOS native
implementations remain Pending. Automated Linux fixtures are not human acceptance
or proof of real Android Studio/sdkmanager interoperability.

## Native Commands

Use a compiled ATM package or build local backends:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
atm plugin android_sdk
atm status android_sdk
atm plugin android_sdk menu stacks
atm plugin android_sdk menu java
```

Root menu, status, stack choices and Java information are offline/read-only. They
never execute Java, sdkmanager, adb, emulator, shell wrappers or Python. Java is
**not required for native downloads**; SDK tools can require a compatible JDK when
you run them. Availability detection checks `<AppsDir>/Java/current/bin/java`,
`JAVA_HOME/bin/java`, then PATH, without executing or claiming a verified version.
Missing Java produces guidance to use `atm plugin java`, not a system installation.

There is no fake `use` operation: SDK packages coexist instead of switching a whole
SDK version. Native Full Setup intentionally excludes this plugin
(`full_setup=false`) because generic automatic installation cannot provide the
required explicit license review and plan-bound acknowledgement.

## Stacks and Licenses

| Stack | Platform | Build Tools | CMake | NDK |
| --- | --- | --- | --- | --- |
| `latest`, `api36` | android-36 | 36.0.0 | 3.31.6 | 29.0.14206865 |
| `api35` | android-35 | 35.0.1 | 3.31.6 | 29.0.14206865 |
| `api35_legacy` | android-35 | 35.0.0 | 3.22.1 | 28.2.13676358 |
| `api34` | android-34 | 34.0.0 | 3.22.1 | 27.3.13750724 |

Each stack includes stable platform-tools, emulator and pinned command-line tools
build **14742923**, package revision **20.0**. `latest` retains the legacy API 36
mapping; it does not mean an automatically selected Android API. The unused legacy
`api37` option has no mapping and is rejected. Custom fields select a custom stack;
platform-tools, emulator and the pinned command-line tools remain included.

Review the exact plan and **all** license terms before accepting:

```bash
atm plugin android_sdk menu review --stack api36
atm plugin android_sdk menu custom
atm plugin android_sdk menu review --platform android-36 --build-tools 36.0.0 --cmake 3.31.6 --ndk 29.0.14206865
```

In the native TUI, open **View full output** to read the complete wrapped plan and
terms before choosing acceptance. CLI/JSON output includes all review chunks.
Review does not write files, download package archives or accept licenses. It
returns a confirmation choice containing a SHA-256 digest of the operation, SDK
destination, resolved artifacts/dependencies and exact decoded license texts.

The equivalent explicit CLI form is:

```bash
atm install android_sdk --stack api36 --accept-licenses DIGEST_FROM_REVIEW
```

Use the digest from that exact review. Missing/malformed acknowledgement fails
before networking or filesystem mutation. Install/update resolves metadata again;
changed packages, hashes, dependency metadata, terms or destination require a new
review. CORE confirmation is still required; `--yes` alone does not accept licenses.
There is no automatic acceptance or `yes` pipeline.

Review refuses more than 32 resolved packages, eight distinct licenses, 128 KiB of
license text or oversized/unprintable output. Complete presentation is checked
before any acceptance choice: at most 40 UTF-8-safe log chunks, each at most 7,000
bytes, beneath the protocol/ring/output budgets. It does not silently truncate terms.

License persistence follows [AOSP License.java](https://android.googlesource.com/platform/tools/base/+/mirror-goog-studio-main/repository/src/main/java/com/android/repository/api/License.java):
SHA-1 of the exact XML-decoded text as UTF-8, with no trimming or whitespace collapse.
The [repository schema](https://android.googlesource.com/platform/tools/base/+/mirror-goog-studio-main/repository/src/main/resources/xsd/repo-common-02.xsd)
uses `xsd:string`. Accepted hashes are appended to `SDK/licenses/<license-id>`;
existing valid entries are preserved. These license hashes are separate from
artifact checksums and the review digest. Licenses remain after uninstall.

## Layout and Ownership

```text
<AppsDir>/AndroidStudio/
  SDK/
    .atm-native-sdk.json
    cmdline-tools/
      20.0/
      latest -> 20.0
    platform-tools/
    emulator/
    platforms/android-36/
    build-tools/36.0.0/
    cmake/3.31.6/
    ndk/29.0.14206865/
    licenses/
  .atm-sdk-history/
```

The SDK root remains `~/Apps/AndroidStudio/SDK` by default. New native command-line
tools use canonical `cmdline-tools/20.0`, matching package ID `cmdline-tools;20.0`,
while the downloaded build remains 14742923. Existing legacy `14742923` directories
are never moved or adopted. The `latest` compatibility link remains for CLI PATH.

Each package includes a SHA-256 file inventory receipt, original Google metadata
and an SDK-format `package.xml`. Local metadata retains revision, type details
(including platform API/extension fields), license and dependency minimums. Package
paths match SDK IDs rather than archive build filenames. AOSP's
[local package loader](https://android.googlesource.com/platform/tools/base/+/mirror-goog-studio-main/repository/src/main/java/com/android/repository/impl/manager/LocalRepoLoaderImpl.kt)
can still report a duplicate/inconsistent **lexical alias** when following
`latest -> 20.0`; this is not claimed to be eliminated or proven fatal. Actual
vendor recognition and GUI/tool execution remain a human validation item.

A nonempty SDK root without native ownership is reported as unmanaged and refused
for mutation **before network access**. No automatic adoption or destructive
sdkmanager command is attempted. An empty root can be initialized. Within an owned
SDK, existing target packages need matching native receipts; changed or unknown
package contents prevent replacement/removal. Root-level unknown data is preserved.

## Update, Remove and PATH

```bash
atm plugin android_sdk menu review-update
atm update android_sdk --accept-licenses DIGEST_FROM_REVIEW
atm remove android_sdk 'platforms;android-36'
atm plugin android_sdk integrate
atm plugin android_sdk integrate path
atm uninstall android_sdk
```

Update re-resolves only native-owned package IDs and their required dependencies,
not a new API/NDK stack. Stable platform-tools/emulator or revisions under the same
ID can advance; command-line tools remain pinned. Explicit stack installation can
also advance already owned shared packages to the reviewed revisions. Downgrades
and changed artifacts under the same installed revision are refused.

Dependencies are resolved recursively before mutation, in dependency-first order,
with minimum revision checks, stable-channel and Linux x64/64-bit archive filtering.
Cycles, unknown unsupported dependency families, unsatisfied minimum revisions or ambiguous
same-revision archives fail closed. Declared soft dependencies are conservatively
included rather than silently omitted. A package required by another native receipt
cannot be removed alone. Remove command-line tools through uninstall, not separately.

Updates retain the prior payload under the sibling `.atm-sdk-history` directory,
outside the SDK scanner's root, rather than deleting it. Replacement failure restores
that payload when safe; a conflicting new destination is preserved and the retained
path is reported. Removal deletes only verified receipt entries, nonrecursively.
Unknown files appearing during deletion prevent directory removal.

Integration is separate from installation. It adds guarded blocks to `.profile`,
`.bashrc` and an existing `.zshrc`, setting `ANDROID_HOME`, `ANDROID_SDK_ROOT` and
PATH for command-line tools, platform-tools, emulator and the highest native-owned
CMake version. Removing an integrated CMake package refreshes existing owned PATH
blocks to the highest remaining native CMake, or removes the CMake entry if none
remain. Blocks/profiles the user deleted are not recreated by package removal.
Re-run explicit integration after installing a newer CMake. Unowned or edited
native blocks are refused before package removal; unrelated profile content is retained. No
system profiles or root-owned directories are modified.

Uninstall preflights native package inventories and integrations, then removes
managed packages and owned integration only. It preserves SDK marker, accepted
licenses, retained update history, unknown files, `.android`, AVDs and all personal
data. It does not delete the entire SDK root or run vendor uninstall commands.

## Safety and Limits

All installation policy is Go native. No Java/sdkmanager command is invoked;
there is therefore no vendor package process whose external effects are silently
treated as transactional. Close IDEs and other SDK package managers during changes:
the ATM directory lock serializes ATM operations, not unrelated vendor processes.

The authoritative source is [Google's repository XML](https://dl.google.com/android/repository/repository2-3.xml)
and its exact HTTPS ZIP URLs under `dl.google.com/android/repository/`. Metadata
schema, stable channel, host restrictions, size and published SHA-1/SHA-256 checksums
are checked. Current inspected Google metadata publishes **SHA-1**; the local
SHA-256 receipt is not an invented published SHA-256 or independent vendor signature.
Transport authentication and Google's checksum remain the trust boundary. The
pinned command-line tools URL, size and published SHA-1 are additionally fixed.

ZIP extraction is private, bounded and rooted: it rejects traversal, duplicate
paths, special files/permissions, unsafe link chains and ambiguous layouts. It
supports Google's root or single-wrapper layout via `source.properties`, verifies
revision/package/API identity and checks native executable ELF architecture where
applicable. It writes local package metadata only after validation. Limits are
16 MiB repository XML, 4 GiB compressed archive, 16 GiB expanded package, 2 GiB per
file, 200,000 ZIP entries and 64 MiB receipt. JARs/tools are inspected, never run.

Publication is **per package**, not one atomic stack transaction. A later failure
or cancellation can leave earlier verified packages installed and licenses accepted.
Removal/uninstall can partially complete on I/O failure; a successful PATH refresh
may remain if a subsequent package deletion fails. There is no automatic crash-recovery
journal: an interrupted `.atm-native-lock`, `.atm-stage-*` or retained history needs
inspection before manual recovery. Never clear a lock by deleting personal SDK data.
Receipts protect against accidental modification, not a malicious local owner who
can rewrite the receipts themselves.

The original Bash script/config/metadata/locales are preserved, including legacy
Full Setup behavior. Native ATM does not source them, evaluate Bash configuration,
or fall back to Bash if the compiled backend is missing.

## Verification

```bash
go test ./plugins/android_sdk/native -count=1
go test -race ./plugins/android_sdk/native -count=1
go vet ./plugins/android_sdk/native
# Optional previously saved Google metadata, without downloading any SDK archive:
ATM_TEST_ANDROID_REPOSITORY=/tmp/atm-v29.7/repository2-3.xml go test ./plugins/android_sdk/native -run TestOfficialRepositorySnapshot -count=1
```

Fixtures isolate HOME/Apps/runtime, use synthetic ZIPs/ELF headers and local TLS,
and prove no Java/shell delegation. Coverage includes stack parity, recursive
dependencies, channel/architecture filtering, exact license whitespace/hash,
bounded full review, changed-plan refusal, local XML schema structure, ownership,
unknown/personal data preservation, ZIP/checksum/size failures, cancellation and
retained-package rollback. Synthetic command-line tools fixtures exercise internal
lifecycle functions without a production pin override; a separate metadata test
enforces the real 14742923 pin. No real SDK was downloaded, executed or installed.

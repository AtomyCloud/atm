# Java / JDK Plugin

Plugin `java`, version `0.0.1`. Native Linux amd64/arm64, schema 2, CORE API 2,
JSONL-v1. CORE remains `0.0.2`. Full Setup is enabled and installs the configured
default **OpenJDK 26.0.1**, not an automatically discovered newest major.

## Commands and Menu

```text
atm plugin java
atm plugin java menu
atm status java
atm install java
atm install java --vendor corretto --version 26
atm install java --vendor temurin --version 26
atm install java --vendor microsoft --version 25
atm install java --vendor graalvm --version 25
atm use java temurin/26.0.2.1
atm remove java openjdk/26.0.1
atm update java
atm uninstall java
atm plugin java integrate
```

CORE confirms operations. The offline menu provides the five configured vendor
buttons, a custom label/URL/hash prompt sequence, installed selection, status,
update, removal, uninstall, and integration. All prompt inputs remain literal
arguments, never shell commands. Back abandons custom entry without I/O. Root
menu/status do not fetch metadata, execute Java, or change disk. Labels are
embedded in `native/en-us.json`.

Vendor aliases remain supported: `jdk`; `amazon`, `amazon-corretto`,
`amazon_corretto`; `eclipse`, `eclipse-temurin`, `eclipse_temurin`; `ms`,
`microsoft-openjdk`, `microsoft_openjdk`; and `graal`. Case is normalized. Use and
remove take one canonical `vendor/version` target; aliases normalize to it.

## Official Sources

The native constants preserve the legacy configured vendor buttons without
sourcing `plugin.conf`:

| Vendor | Configured Selector | Integrity Source |
| --- | --- | --- |
| OpenJDK | `26.0.1` (`26` selects that same fixed build) | Fixed `download.java.net` archive plus `.sha256` |
| Amazon Corretto | `26` | `corretto.aws/downloads/latest/` and `latest_sha256/` |
| Eclipse Temurin | `26` | Adoptium assets/latest JSON package SHA-256 and exact size |
| Microsoft OpenJDK | `25` | `aka.ms/download-jdk/` archive plus `.sha256sum.txt` |
| GraalVM JDK | `25` | `download.oracle.com/graalvm/25/latest/` plus `.sha256` |

Linux architecture mappings are `amd64 -> x64` and `arm64 -> aarch64`. All five
official routes require stable payloads compatible with their requested major
or exact version. These are the only bundled selectors; unsupported selections
fail with explicit URL/hash guidance rather than inventing an archive location.

Official references: [OpenJDK](https://jdk.java.net/),
[Corretto downloads](https://docs.aws.amazon.com/corretto/latest/corretto-26-ug/downloads-list.html),
[Adoptium API](https://github.com/adoptium/api.adoptium.net),
[Microsoft downloads](https://learn.microsoft.com/en-us/java/openjdk/download),
and [GraalVM downloads](https://www.graalvm.org/downloads/).

Temurin uses `assets/latest/26/hotspot` with architecture, JDK, Linux, and Eclipse
filters, `User-Agent: ATM/0.0.2`, and `Accept: application/json`. Missing,
ambiguous, mismatched, or preview metadata fails closed. HTTP 403/offline failures
are reported without fabricated metadata or a bundled fallback. Four-component
versions such as `26.0.2.1` are supported. Provider semver is not assumed to equal
the payload's literal `JAVA_VERSION`; the actual payload version is recorded.

Downloads require HTTPS. Redirects are limited per vendor: `download.java.net`;
`corretto.aws`; `aka.ms` and `download.visualstudio.microsoft.com`;
`download.oracle.com`; or the Adoptium release repository and GitHub release
storage (`release-assets.githubusercontent.com`, `objects.githubusercontent.com`,
`github-releases.githubusercontent.com`). Adoptium metadata redirects are refused.
Credentials, fragments, plaintext, and foreign-host redirects are refused.
Sidecars must contain one unambiguous SHA-256; a supplied filename must match
the final archive basename. Hashes from HTTPS are not independent signatures.

## Custom JDKs

```text
atm install java --vendor custom --version team-ea --url https://example.org/jdk.tar.gz --sha256 <64-hex-archive-hash>
```

Custom requires an explicit safe label, HTTPS archive URL, and exact SHA-256.
Labels start with a letter/number and may contain letters, numbers, dot,
underscore, plus, and hyphen (up to 64 characters, no trailing dot). Standard EA
payload versions such as `27-ea` and `27-ea+4` are accepted for checksummed custom
downloads. Official vendor GA routes still reject EA payloads.

An explicit `--url` is also permitted with a named vendor and numeric version,
but always requires `--sha256`; the payload must match the requested version.
Custom URLs are explicit user actions, not additional catalog sources. Redirects
must stay on the exact supplied HTTPS origin. For another CDN origin, explicitly
provide its final URL and verified hash instead. No checksum-free path exists.

## Versions, Updates, and Ownership

The layout is `<AppsDir>/Java/<vendor>/<version>` and `<AppsDir>/Java/current`.
AppsDir normally means `~/Apps`. New official installs use the actual parsed
`JAVA_VERSION` directory, not a mutable major alias. Custom installs use the
explicit label and record the actual Java version separately. Receipts pin the
requested selector, initial/resolved URLs, checksum source, archive hash/size,
actual version, and exact payload inventory.

Bare update preserves the active vendor **and actual major**. If no official
resolver exists for that major (for example Temurin 21), it fails before download;
it never silently switches to 26. Explicit `--vendor` or `--version` selectors
may choose another configured major after CORE confirmation. OpenJDK 26 continues
to mean the fixed 26.0.1 archive. Updates within the same vendor refuse downgrades. Custom update
requires explicit label, URL, and hash; there is no automatic custom discovery.

Mutable URLs can yield a verified new actual version, installed alongside old
versions. An existing identical native-owned artifact can be selected. **Different
bytes under the same actual version or custom label are refused**, never silently
replaced or reported as a completed update. Use a new explicit custom label/hash
for a deliberately pinned alternative. Existing legacy directories are not
overwritten/adopted by install; select them with `use` instead.

Native `.atm-java-receipt.json` inventories contain hashes, sizes, modes,
directories, and confined symlinks. Changed/user-added files block activation,
removal, and uninstall. Safe legacy JDKs remain selectable without native
ownership, but are never deleted. Individual removal protects current. Uninstall
removes unchanged native-owned versions only; unrelated directories, user data,
legacy versions, and legacy manifests remain. A selected legacy JDK retains its
current link and integration. External system/PATH installations are untouched.

## Extraction and Shell Integration

Native Go performs HTTP, JSON, hashing, gzip/tar extraction, and ELF validation.
There is no Bash/Python/curl/tar delegation and no downloaded Java execution.
Limits: archive/file 512 MiB, expanded payload 2 GiB, 50,000 entries, metadata
4 MiB, checksum/release text 64 KiB, 30-second metadata resolution, five-minute
downloads, and at most five redirects. CORE deadlines may be shorter.

Archives must have one JDK root. Rooted extraction rejects traversal, special
files/permissions, duplicate paths, receipt injection, external or cyclic links,
and entries below link ancestors. Safe internal file/directory symlinks and
regular-file hardlinks are supported; bounded chains resolve to confined existing
targets. `bin/java` and `bin/javac` must be regular executable ELF files matching
the target architecture. `release` is parsed as bounded quoted literal fields,
never sourced/evaluated, and must contain one compatible `JAVA_VERSION`.

Root/ancestor symlinks and unsafe current targets are refused. Activation replaces
current atomically, then leaves the selected JDK available through the stable
path. Profiles use `os.UserHomeDir()`, not request `home` (ATM runtime). Only the
`ATM native java PATH` block in `.profile`, `.bashrc`, and an existing `.zshrc`
is managed. It exports `JAVA_HOME=current` and prepends `current/bin`, preserving
other ATM blocks/user text. Profile conflicts fail closed; ordinary activation
failures attempt guarded rollback. A shell-quoted refresh command is emitted;
the backend cannot change its parent shell environment.

CORE dry-run never executes the backend. `ATM_DRY_RUN=1` also prevents backend
network/filesystem/subprocess operations. Ordinary failures/cancellation clean
private staging. A published inactive JDK can remain after activation failure;
abrupt termination may leave a lock/stage requiring inspection. Multi-version
uninstall is not crash-transactional. Receipts are local evidence, not protection
against an attacker able to alter both receipt and payload; hostile same-user
concurrent edits and power-loss recovery are outside ordinary rollback guarantees.

## Development and Acceptance

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
bash scripts/build_manager.sh --plugins-local --target linux-arm64
go test ./plugins/java/native
go test -race ./plugins/java/native
go vet ./plugins/java/native
```

The helper produces ignored binaries/checksums at manifest paths. Tests use
temporary homes, TLS fixtures for each vendor/architecture, fake ELF payloads,
and compiled CORE runner lifecycle checks with empty PATH. Coverage includes
custom prompt arguments, EA versions, link/metadata/hash failures, cancellation,
ownership/profile preservation, mutable-version conflicts, and update-major
preservation. No real JDK is installed or executed and no real profiles are edited.

Linux amd64 automated coverage is local. Human acceptance and Linux arm64 runtime
acceptance remain Pending; cross-builds are not runtime acceptance. Windows/macOS
backends remain Pending. Parent owns packaging/full-repository gates/history.
The legacy Bash plugin/config/locales remain unchanged for the separate legacy
entrypoint and do not provide these native receipt/removal guarantees.

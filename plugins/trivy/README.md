# Trivy Plugin

Plugin `trivy`, version `0.0.1`. Native CORE API `2` (CORE `0.0.2`).
Full Setup remains disabled. Native Linux amd64/arm64 uses schema 2 and
`jsonl-v1`; other platforms are Pending implementation and runtime acceptance.

Official references: [Getting started](https://trivy.dev/docs/latest/getting-started/),
[installation](https://trivy.dev/docs/latest/getting-started/installation/), and
[releases](https://github.com/aquasecurity/trivy/releases).

## Native Commands and Menu

```text
atm plugin trivy
atm plugin trivy menu
atm status trivy
atm install trivy
atm install trivy --method binary
atm install trivy --method docker
atm install trivy --method binary --version v0.76.1
atm install trivy --method docker --version 0.76.1
atm update trivy --method binary
atm update trivy --method docker
atm use trivy 0.76.1
atm remove trivy 0.75.0
atm remove trivy --method docker --version 0.76.1
atm uninstall trivy
atm plugin trivy integrate
atm plugin trivy menu --usage
```

Without `--method`, install and update return binary/Docker choices and the
message "Choose installation method". They perform no lookup, pull, or install.
A custom version is preserved through the method chooser. CORE owns confirmation;
running trusted backend code directly is not an authentication boundary.

The root menu offers latest binary, latest Docker, custom version, update,
installed/use/remove, explicit recorded Docker removal, uninstall, integration,
and usage. Submenus include Back. The official documentation URL is visible in
the menu title. Root menu and status are offline/read-only: they neither invoke
Docker or Trivy nor access scan databases. Status reports recorded image IDs,
not a live daemon check. New labels are embedded in `native/en-us.json`.

## Binary Releases and Ownership

Native Go fetches fixed `api.github.com/repos/aquasecurity/trivy/releases/latest`
or `releases/tags/vX.Y.Z` metadata. Versions accept `latest` or exact numeric
`X.Y.Z` with optional `v`. Drafts, prereleases, missing stable flags, mismatched
exact tags, and missing/duplicate/wrong binary assets are rejected. There is no
offline version fallback. Update refuses to downgrade the active selection.

The required assets are `trivy_VERSION_Linux-64bit.tar.gz` or
`trivy_VERSION_Linux-ARM64.tar.gz`, and `trivy_VERSION_checksums.txt`, with exact
URLs under the official `aquasecurity/trivy` GitHub release. The archive's unique
SHA-256 entry and metadata size are checked. HTTPS redirects are restricted to
GitHub release storage: `release-assets.githubusercontent.com`,
`objects.githubusercontent.com`, and `github-releases.githubusercontent.com`.
API redirects, other hosts, plaintext redirects, credentials, and fragments fail.

Limits: metadata 4 MiB, checksums 1 MiB, download/file 256 MiB, expanded archive
1 GiB, 10,000 entries, 30-second release resolution, five-minute downloads.
Cancellation and the CORE deadline may stop work earlier. Every archive entry
is validated; traversal, duplicate paths, links, special files, and special
permissions are refused. Only the root executable `trivy` is staged into
`bin/trivy`; its ELF class/type/architecture are validated without execution.
Checksums use HTTPS release metadata, not independent Sigstore verification.

The native layout remains:

```text
<AppsDir>/trivy/<version>/bin/trivy
<AppsDir>/trivy/<version>/.atm-trivy-receipt.json
<AppsDir>/trivy/current -> <version directory>
<AppsDir>/trivy/.atm-trivy-docker.json
```

The default AppsDir is `~/Apps`. Root/ancestor symlinks, unsafe current links,
symlinked binaries/profiles, and incompatible executables are refused. Safe
legacy binaries remain selectable, including marker-only legacy versions, but
are never adopted or deleted. Native receipts record exact file/directory modes,
sizes, hashes, and artifact metadata. Changed or added files block native use,
removal, and uninstall. Individual removal refuses the current version.

Activation atomically replaces `current`, updates only the
`ATM native trivy PATH` block in `.profile`, `.bashrc`, and an existing
`.zshrc`, and prepends `current/bin` to PATH. Other ATM blocks and user content
are preserved. Profiles use `os.UserHomeDir()`, not the request's ATM runtime
`home`. Ordinary failures attempt guarded profile rollback. The emitted refresh
command is for the user to run; a child cannot alter its parent shell.

## Docker Ownership and Effects

Docker must already be installed and accessible. Commands use literal argument
arrays with closed stdin, bounded output and deadlines. No sudo, daemon start,
group changes, containers, scans, database operations, force removal, or pruning
are performed. The plugin does not install Docker or a Docker-backed wrapper.

Explicit Docker install resolves a stable official release, obtains the daemon
identity using `docker info --format '{{.ID}}'`, and checks the exact local tag
before pulling `aquasec/trivy:VERSION`. An existing tag without a native receipt
is refused and preserved, including tags mentioned by legacy Docker state.
There is no approved adoption operation. Existing native-owned tags must match
their recorded image ID before pull/update.

After a successful pull, exact image inspection must return a valid full
`sha256:...` image ID. A bounded, atomically replaced local receipt records
daemon ID, tag, and image ID. Multiple native-owned versions are retained;
the removal menu enumerates them. Update refuses a downgrade of the selected
recorded version.

Removal requires the same daemon ID and image ID before
`docker image rm aquasec/trivy:VERSION`. Switching Docker host/context to another
daemon is refused even if it has the same tag and image ID. Tags that changed,
unrecorded images, unrelated tags, and containers are never intentionally removed.
Docker may refuse removal, for example while an image is in use; the receipt
is retained on command failure.

Docker effects are **not transactional**. Cancellation, failed inspection, or
failed state publication after pull can leave an image without new ownership.
Removal can succeed before receipt persistence fails. Inspect and remove are
separate CLI calls: safety assumes no concurrent external tag, daemon, or
context mutation during an operation. Daemon identity is checked, not a lock
over external Docker administration.

Usage examples are displayed only, never executed automatically:

```text
trivy --version
trivy image alpine:latest
trivy fs .
docker run --rm aquasec/trivy:VERSION --version
docker run --rm aquasec/trivy:VERSION image alpine:latest
docker run --rm -v "$PWD:/work:ro" aquasec/trivy:VERSION fs /work
```

## Uninstall, Dry-Run, and Recovery

Uninstall preflights native binary ownership and profiles, then verifies recorded
Docker daemon/image identities before deleting native-owned tags and unchanged
binary versions. A Docker failure stops before binary deletion. Multi-version
uninstall may be partial after interruption; it is not a global transaction.

**Scan caches, databases, reports, other user data, external installations,
legacy binary markers/manifests, and legacy Docker state are preserved.**
A selected legacy binary retains its current link and integration. No legacy
configuration/path overrides are silently imported into the native backend.

CORE dry-run never starts the backend. Backend `ATM_DRY_RUN=1` additionally
prevents network, subprocesses, and filesystem operations. Downloads/extraction
and ordinary cancellation clean private staging. Published inactive versions can
remain after activation failure. Abrupt termination can retain a lock/staging
directory: inspect it before manual cleanup, with no backend running. Receipts
are local ownership evidence, not protection against an attacker who can rewrite
both state and payload. Power-loss recovery and hostile same-user concurrent
filesystem modification are outside ordinary rollback guarantees.

## Development and Verification

Build from source using the package helper, which creates ignored native outputs
and checksums at the manifest paths:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
bash scripts/build_manager.sh --plugins-local --target linux-arm64
go test ./plugins/trivy/native
go test -race ./plugins/trivy/native
go vet ./plugins/trivy/native
```

Linux-only tests use TLS fixtures, temporary user/ATM homes, inert ELF payloads,
and a fake Docker executable with literal-argv validation. They cover binary and
Docker lifecycle, legacy/external preservation, changed-file/tag/daemon refusal,
malformed receipts, archive/checksum failures and bounds, cancellation after
pull/download, profile rollback, no-method choices, both dry-run guards, and the
compiled backend via CORE with an empty PATH for binary operations. No real
application installation, Docker pull, scan, or personal profile edit is needed.

Linux amd64 automated tests pass. Human acceptance and Linux arm64 runtime
acceptance remain Pending; cross-compilation is not runtime acceptance. CORE,
packaging, and full-repository gates belong to the parent migration session.

## Legacy Bash Entry Point

The existing `plugin.sh`, `release.py`, configuration, locales, and legacy tests
are unchanged for the separate `bin/atm` entrypoint. Those historical routes
still require Bash/curl/Python and use marker-only binary ownership and a
last-version Docker state file. Their removal semantics are not the native
receipt guarantees described above. Do not use legacy uninstall as a substitute
for native ownership-checked removal. The native backend never delegates to them.

```bash
bash -n plugins/trivy/plugin.sh
bash plugins/trivy/tests/test_lifecycle.sh
python3 -B -m unittest discover -s plugins/trivy/tests -p 'test_*.py' -v
```

# Native Installation Services

Status: experimental internal services introduced in migration 0.0.2, Patch v27.2.
These services are tested with temporary artifacts and are not a stable third-party
SDK. This page documents `internal/install.Manager`, not every application's backend.
The native Node.js, Go, Java, Trivy, Flutter, Android Studio, Android SDK and VS Code backends use
plugin-owned lifecycle policies to preserve
their existing layouts; they do not silently adopt legacy roots into this Manager.
See the [Node](../../plugins/node/README.md), [Go](../../plugins/go/README.md),
[Java](../../plugins/java/README.md), [Trivy](../../plugins/trivy/README.md),
[Flutter](../../plugins/flutter/README.md),
[Android Studio](../../plugins/android_studio/README.md),
[Android SDK](../../plugins/android_sdk/README.md) and
[VS Code](../../plugins/vscode/README.md) guides.

## Ownership And API

`internal/install.Manager` operates on one dedicated absolute `Root`, identified
by `PluginID`. The parent directory must already exist. First installation creates
the root with private POSIX permissions and an `owner.json` marker. Windows uses
the parent directory's inherited ACLs; choose a private per-user parent. An existing directory
without a valid matching marker is refused, including empty directories and legacy
installations. A caller using this Manager must use a dedicated owned root.

The plugin owns release resolution, approved origins, checksum provenance, archive
selection, application validation and launchers. The generic service contains no
Node/Go/VS Code URLs or package-manager commands. Confirmation belongs to the
calling CLI/TUI; the service never asks for sudo or evaluates shell commands.

Methods:

- `Install(ctx, spec)`: download, verify, extract, publish a new version and activate.
- `Active(ctx)`: verify the active receipt and payload, then return the resolved path.
- `Activate(ctx, version)`: verify and activate a retained owned version, also usable
  for rollback. Modified active state blocks changes instead of being overwritten.
- `Recover(ctx)`: after caller confirmation, discard abandoned private staging.
  Retain every published version and the last committed activation.

`Spec` requires an explicit version, HTTPS URL, pinned lowercase SHA-256, `zip` or
`tar.gz` format and approved canonical HTTPS origins. Optional `StripPrefix`
requires every entry to be inside that archive root. Versions are portable,
lowercase identifiers of at most 64 characters, beginning with a letter/digit.

`DryRun` validates install/activation plans without filesystem or network I/O.
Dry-run status returns no invented installed version. Results identify a planned
or resolved payload path; no PATH or stable launcher is created automatically.

## Downloads And Archives

Downloads require HTTPS, verify certificates, have a maximum five-minute HTTP
deadline, respect context cancellation and allow at most five requests through
approved-origin redirects. Credentials/fragments in URLs and HTTP content encoding
are refused. The caller must obtain checksums from trusted upstream metadata:
SHA-256 equality by itself is not publisher authentication.

Default resource limits are 256 MiB downloaded, 1 GiB extracted, 256 MiB per file
and 10,000 archive entries. Explicit overrides have hard caps of 1 GiB downloaded,
8 GiB extracted, 2 GiB per file and 100,000 entries. Archive header/padding reads
are bounded too. Invalid length, checksums, ZIP CRCs or gzip trailers abort.

Extraction uses [Go's rooted filesystem APIs](https://pkg.go.dev/os#Root) to confine
writes to newly created staging. It rejects traversal, absolute paths, duplicate
entries, case/type collisions, Windows reserved names, symbolic/hard links, devices
and PAX extensions. Archive paths use a conservative portable ASCII subset,
at most 240 bytes and 16 components;
installation root paths can contain spaces and Unicode. Files request private POSIX modes, with
only the executable bit retained where supported; no owners/setuid bits are copied.

**This Manager intentionally rejects links and PAX archives.** The native Node
backend handles its npm/npx links in a separate confined, plugin-owned extractor.
Compressed tar.xz, app bundles, MSI/DEB/PKG installation and post-install scripts
are not implemented in this generic Manager. Flutter handles tar.xz in its own
bounded, plugin-owned Go extractor; this does not change the Manager contract.

## Transaction Layout

```text
<dedicated-root>/
  owner.json
  lock
  versions/<version>/
    receipt.json
    payload/...
  staging/txn-<random-id>/...
  activations/00000000000000000001.json
  activations/00000000000000000002.json
```

Linux/macOS use file locks; Windows uses a byte-range file lock. A second manager
fails promptly while the store is busy. Process termination releases the OS lock;
there is no unsafe age-based stale-lock deletion. These are local filesystem
semantics, not a distributed/network-filesystem locking protocol.

Each version has an immutable receipt with artifact identity and file hashes,
sizes and permissions. Activation verifies the complete file inventory, detects
modified/missing/extra files and references the receipt hash. Existing versions
are reused only when their artifact identity and payload match; they are never
overwritten or silently adopted.

Publishing a version and activating it are separate steps. The activation commit
is a same-filesystem rename of a flushed new record into an append-only numbered
history. No shared `current` symlink or overwrite of an open executable is needed.
The prior activation remains valid until that commit. The history is bounded at
10,000 entries; pruning/migration is not automatic.

On a failed or cancelled operation before commit, the previous active version is
unchanged. A published but inactive version can be verified and reused on retry.
Cancellation after commit cannot undo the new activation; use a later explicit
activation to roll back. Normal failures remove their own staging. A killed process
can leave staging behind, which blocks further mutations until explicit recovery.
Recovery refuses unknown staging entries and corrupt committed state. It never
deletes published versions, unrelated files or earlier activations.

These guarantees cover ordinary failures and process interruption on a local
filesystem. They do not claim power-loss durability across every filesystem,
hostile same-user isolation or protection against external writers modifying the
managed store concurrently. Incomplete/corrupt ownership metadata fails closed
and requires manual inspection rather than automatic adoption or repair.

## Validation And Next Work

Tests use local HTTPS servers with test certificates and disposable roots. They
exercise valid ZIP/tar.gz, integrity/size failures, unsafe entries, redirects,
concurrent locks, tampering, cancellation, failure injection and child processes
that exit abruptly before/after the activation commit. No actual application is
downloaded or installed. Linux results are local; Windows/macOS execution must be
confirmed by the expanded GitHub Actions matrix and manual standard-user testing.

Native Node.js is implemented and accepted on Linux; native Go, Java, Trivy,
Flutter, Android Studio, VS Code and Android SDK have automated Linux coverage. Remaining application
ports proceed separately. The native catalog
and signed CORE update/recovery services are already implemented; source catalog
downloads do not compile missing application backends. Never interpret these generic
services as authorization to adopt or delete an application's existing data.

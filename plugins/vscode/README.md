# VS Code Plugin

Plugin version: `0.0.1`. Native backend: Go, schema 2 / JSONL-v1, ATM API 2.
Linux amd64 and arm64 are declared. Linux amd64 automated fixtures are covered;
human acceptance and Linux arm64 execution remain Pending. Windows/macOS native
backends and translated native labels remain Pending.

## Native Commands

Use a compiled ATM runtime package or build local plugin executables first:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
atm plugin vscode
atm status vscode
atm plugin vscode menu releases
atm install vscode
atm install vscode --version 1.118.1
atm update vscode
atm use vscode 1.118.1
atm remove vscode 1.118.0
atm uninstall vscode
```

The root menu and status are offline/read-only and never execute the editor.
The release submenu queries the official stable release list and offers the
latest **seven** versions plus custom input. Custom input is also available in
the offline root menu. `v1.118`, `1.118` and `1.118.0` select `1.118.0`.
Unqualified install/update chooses the latest stable release. An update that
would downgrade the active version is refused; select an older version explicitly
with install/use. Failed release discovery is reported, not disguised as a fresh
hardcoded list. CORE confirmation remains required for every selected operation.

A newly installed version becomes active with fresh portable `data`. Installing
or updating does not transfer existing settings/extensions; use the separate data
migration action below to retain that working environment in the new version.

Installation preserves the existing portable layout:

```text
<AppsDir>/vscode/
  code_1.118.0/
    code
    bin/code
    resources/app/
    data/
    .atm-vscode-receipt.json
  code_1.118.1/
  current -> code_1.118.1
  preserved-data/
```

`AppsDir` defaults to `~/Apps`. The runtime request's `home` identifies ATM, not
the user's home directory. Shell profiles and user shortcuts use the OS user HOME.
Legacy `plugin.conf` remains available to the Bash entrypoint; the native backend
does not source Bash configuration or evaluate its environment expressions.

## Explicit Integration

Installation and selection do not automatically change profiles, create shortcuts
or install extensions. Select these independently:

```bash
atm plugin vscode integrate
atm plugin vscode integrate path
atm plugin vscode integrate cli
atm plugin vscode integrate desktop
atm plugin vscode integrate extensions
```

Bare `integrate` configures all three user integrations: a marked PATH block in
`.profile`/`.bashrc` and an existing `.zshrc`, an owned `~/.local/bin/code` symlink,
and `code.desktop` plus `code-url-handler.desktop` in
`~/.local/share/applications`. Launchers reference `current`, so switching versions
updates their destination without rewriting them. No system desktop directories,
MIME defaults or unrelated profile content are modified. The URL-handler desktop
entry declares `x-scheme-handler/vscode`; ATM does not execute `xdg-mime` or change
the user's preferred handler. Existing unowned files/links and edited owned
integrations are refused, not overwritten or adopted. Start a new shell or use the
refresh command printed after integration.

Extensions are read from the runtime's `plugins/vscode/extensions.txt`. Blank and
comment lines are ignored; only literal `publisher.extension` IDs are accepted,
deduplicated case-insensitively, with the entire list checked before execution.
The selected payload must match its native receipt. ATM invokes the native
`code` executable with `resources/app/out/cli.js` and `ELECTRON_RUN_AS_NODE=1`,
following the [official Linux launcher](https://github.com/microsoft/vscode/blob/main/resources/linux/bin/code.sh)
without running that shell script. Each extension receives literal arguments
`--install-extension ID --force`. Extension execution may access the network and
change portable data; it is not transactional, and earlier successful extensions
remain when a later one fails. Cancellation/timeout stops the directly invoked
process, not an independently detached process. No editor is executed for status,
install, update, version selection, removal, desktop or data migration.

## Portable Data Migration

The official Linux TAR.GZ supports a sibling `data` directory containing user
settings, session state and extensions. It overrides the corresponding CLI path
options. See [Microsoft's portable mode documentation](https://code.visualstudio.com/docs/setup/portable).

Close **both** editors before selecting the migration menu or running:

```bash
atm plugin vscode integrate migrate-data 1.118.0 1.118.1 --closed
```

The explicit `--closed` acknowledgement is required. ATM does not terminate your
editor or guarantee another process will not open it during the operation. Source
and target must be distinct valid installed versions. The source stays untouched.
The copy is staged, hashed, checked against the source and checked again for source
or destination changes before publication. Existing destination data is retained
under `preserved-data/code_VERSION-<unique>/data`; the exact path is logged. Failure
or cancellation before publication restores the old destination when safe. If a
new destination appeared, it is preserved and the retained backup is reported.

Data is never automatically erased by remove/uninstall. Native executable files
are removed only after exact receipt validation; portable `data` is first relocated
intact to `preserved-data`. Unowned legacy versions are retained. Unknown or
modified executable-tree files prevent deletion. Directory removal is nonrecursive,
so newly appearing data/unknown files are not silently swept away. Retained migration
backups are outside executable version directories and are not removed by uninstall.

Migration deliberately refuses symlinks (including Electron singleton links),
sockets/devices and special permission bits, rather than copying or following
external paths. This can require inspecting leftover locks or unusual extension
links after the editor is closed. It does not copy filesystem ownership, ACLs,
xattrs or hardlink identity. Regular file/directory modes and modification times
are retained. Limits: 16 GiB total data, 4 GiB per file and 200,000 entries.

The directory lock serializes ATM actions only. This is a guarded in-process
transaction, not an automatic crash-recovery journal. A crash during migration can
leave the old data in the logged `preserved-data` location and a staging directory;
inspect these before manually clearing `.atm-native-lock`. Multi-version uninstall
can partially complete on I/O failure. Never remove retained data to clear a lock.

## Download and Runtime Boundaries

- Release metadata: `https://update.code.visualstudio.com/api/releases/stable`
  and `/api/versions/<version>/<linux-x64|linux-arm64>/stable`.
- Artifact origin: the API's stable path on
  `https://vscode.download.prss.microsoft.com`, HTTPS only with bounded redirects.
- The version API supplies SHA-256 and build commit. It currently omits size;
  the backend obtains `Content-Length` via HEAD from the official artifact URL,
  checks it against any API size, then verifies exact transferred size and SHA-256.
- Private staging, rooted tar extraction, confined internal archive links,
  version/product/build identity and native ELF architecture are validated before
  publication. No install scripts, curl/tar commands or Bash/Python are delegated.
- Limits: 1 GiB compressed/file, 4 GiB expanded, 100,000 archive entries, 2 MiB
  metadata and 32 MiB ownership receipt. Unknown archive layouts fail closed.
- The vendor `chrome-sandbox` setuid bit is stripped during portable extraction.
  ATM never runs sudo, sets root ownership, grants setuid or uses `--no-sandbox`.
  A functioning Linux user-namespace sandbox is required unless an administrator
  separately configures a supported sandbox policy. Restricted distributions can
  block startup; this is not silently bypassed by the plugin.
- SHA-256 from authenticated Microsoft HTTPS detects artifact mismatch; it is not
  an independent vendor-signature verification. Local ownership receipts are not
  protection from a malicious user who can edit the same installation directory.
- Legacy installations may be selected after identity validation, but are not
  adopted for native deletion or extension execution. Native executable receipts
  exclude personal `data`, which is always treated as user-owned.

The original `plugin.sh`, `plugin.conf`, metadata, templates and translations remain
for the separate legacy Bash runtime. Native ATM never silently falls back to them
when a compiled VS Code backend is missing.

## Verification

```bash
go test ./plugins/vscode/native -count=1
go test -race ./plugins/vscode/native -count=1
go vet ./plugins/vscode/native
```

Tests use private temporary HOME/Apps/runtime directories, synthetic ELF headers,
local TLS metadata/archive fixtures and mocked editor execution. They cover
installation, latest/custom selection, identity/architecture/hash/size failures,
hostile archives, interrupted downloads, receipt-preserving removal, personal data
retention, cancellation rollback, integration ownership and literal extension
arguments. They do not download or execute the real editor, change system sandbox
configuration or claim real-host GUI acceptance.

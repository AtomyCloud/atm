# Native Plugin Catalog

The migration executable (`0.0.2`) implements official catalog **search and code
download** in Go on Linux, macOS and Windows. This does not complete the CORE
migration: native component maintenance and ATM PATH integration are described
in [Native CORE Administration](NATIVE-CORE-ADMINISTRATION.md). Application
integration and production compiled updates remain incomplete.
Compiled CORE self-update remains unavailable. The original `bin/atm` is unchanged.

## CLI

From an extracted migration package:

```bash
./atm plugins search
./atm plugins search brave
./atm --json plugins search "plugin.conf"
./atm --dry-run --json plugins search brave
./atm plugins download brave
./atm --yes --json plugins download brave
./atm --dry-run --json plugins download new_plugin
```

Use `.\atm.exe` in Windows PowerShell. For a development binary, supply
`--home /path/to/atm` before the command. Global flags go before `plugins`.
`--timeout 30s` can shorten the CLI request deadline. Ctrl+C cancels a search.

An empty query lists all entries. Matching is case-insensitive and examines the
plugin ID, name, description and repository file paths, not file bodies. Results
follow the saved `plugins sort az|za|default` preference and mark locally discovered
plugins, including disabled ones. JSON includes `source`, the pinned `commit`,
and an `entries` array containing validated manifests, paths and local presence.
Empty results are a successful search, not a network error.

Download requires a missing local plugin ID. Packages already containing Brave
will refuse the example download instead of replacing it. CLI interactive consent
shows source, commit, file count, size and destination. Non-interactive or JSON
downloads require `--yes`. The plan resolves official `main` once; there is no
arbitrary commit/URL flag. JSON success includes the destination and
`restart_required: true`. Downloading code does not install its application.

## Terminal Interface

Open **ATM > Official plugin catalog**. The request runs asynchronously; Cancel
or leaving the screen cancels it. Closing ATM also cancels and waits for pending
work. Network errors are reported without displaying partial results.

Use `/` or click the search field to filter the loaded snapshot without further
requests. Select a result with Enter or the mouse to inspect its metadata and
source commit. Escape returns from details to the filtered catalog. Reopening
the catalog fetches a new snapshot. No installation starts when selecting a row.

For a missing plugin, open its details and choose **Download plugin code**. This
keeps the searched commit even if `main` advances. Alternatively use the ATM
download action and enter an ID to resolve current `main`. Confirmation defaults
to Cancel; CLI `--yes` does not bypass TUI consent. Downloads run asynchronously.
Cancel or Quit waits for cleanup. A cancellation arriving after publication does
not misreport the completed download as rolled back. Restart ATM before using the
new plugin; it is not loaded during this session, including later list refreshes.

## Download Integrity

- Download verified Git blobs into a private staging directory inside the plugin
  root. Reject links/submodules, traversal, Windows-reserved paths, case collisions,
  file/directory conflicts and reserved `.atm-*` files.
- Limits: 1,000 files, 32 MiB per file, 128 MiB total and ten minutes for download.
  Manifest reads and preparation retain the stricter search limits below.
- Parse `.sh`, `.conf`, `.metadata` and `.lang` files using the existing Go Bash
  parser, without launching Bash. This is syntax validation, not a code sandbox.
  Validate legacy required files/identity; native manifests must include their
  declared executables with the expected ELF/PE/Mach-O architecture and Git mode.
  Source-only native plugins are not compiled automatically.
- Read back staged hashes, sizes and POSIX modes before publication. Record
  `.atm-origin.json` schema 1 with source, main commit and every file's Git hash/mode,
  preserving the legacy origin-receipt format. Windows retains Git modes in the
  receipt; POSIX permission bits are not claimed to exist on Windows.
- Publish with a native non-replacing directory rename. Existing files, links
  and directories, including empty directories created during download, are
  preserved. Unsupported filesystem semantics fail closed; there is no fallback
  that overwrites the target. Windows uses
  [FILE_RENAME_INFORMATION with replacement disabled](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ns-ntifs-_file_rename_information).
- A per-plugin `.atm-download-lock-<id>` directory coordinates with the legacy
  downloader. Normal failures/cancellation remove only owned staging. Abrupt
  termination may leave this hidden directory; inspect it after confirming no
  downloader is running. Locks are never automatically broken or recovered by age.
- No sudo, profile edits, application install, hooks or downloaded executable
  probes. Use a private per-user plugin root; Windows inherits parent ACLs.
  This is not a hostile-local-process sandbox or a power-loss durability guarantee.
  A newly created empty plugin root may remain after a failed download.

## Source And Boundaries

- Only [AtomyCloud/atm main/plugins](https://github.com/AtomyCloud/atm/tree/main/plugins)
  is queried. No custom URL, branch or source override is exposed.
- Resolve `main` once, then read its recursive tree and metadata blobs by Git
  identity. Reject truncated trees, unsafe/duplicate paths, invalid manifests,
  nonregular metadata, mismatched sizes and mismatched Git blob SHA-1 values.
- Prefer `plugin.json` with the existing native/legacy schema validation.
  Metadata-only plugins use the same literal assignment parser as local discovery.
  Shell expansion and commands are rejected; no Bash, Python or plugin runs.
- Bounds: 90 seconds overall, 20 seconds per HTTP request, a 16 MiB tree response,
  50,000 tree entries, 200 plugins and 1 MiB per decoded metadata blob.
- Requests use HTTPS GitHub API endpoints; redirects are refused. Optional
  `GITHUB_TOKEN` is sent only to that API, never saved in configuration or output.
  Rate-limit/access errors are reported; there is no silent unverified fallback.
- Git blob identity checks are not release signatures or a compiled updater
  trust mechanism. Official plugins remain trusted code when run after restart.
- Search has no disk cache or writes. Download writes only the confirmed plugin
  destination and its staging/receipt; neither operation writes preferences or
  executes plugins. `--dry-run` and `--demo` do not contact the network or write.
- A catalog result is not a guarantee of platform availability. Current legacy
  application plugins remain Linux-only. Native component code updates and
  compiled CORE replacement still need separate migration work and validation.

## Validation

Tests use an in-memory HTTP transport and isolated configuration directories:

```bash
go test ./internal/catalog ./internal/cli ./internal/tui ./internal/data ./internal/plugin ./internal/runner
go test -race ./...
go vet ./...
```

Linux tests and five-target cross-builds are not proof of execution on actual
Windows, macOS or ARM64 systems. Those acceptance checks remain separate.

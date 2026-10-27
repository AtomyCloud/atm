# Native Plugin Catalog Search

The migration executable (`0.0.2`) implements official catalog **search** in Go
on Linux, macOS and Windows. This does not complete the CORE migration: plugin
downloads/code updates and PATH integration still use the Linux legacy backend.
Compiled CORE self-update remains unavailable. The original `bin/atm` is unchanged.

## CLI

From an extracted migration package:

```bash
./atm plugins search
./atm plugins search brave
./atm --json plugins search "plugin.conf"
./atm --dry-run --json plugins search brave
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

## Terminal Interface

Open **ATM > Official plugin catalog**. The request runs asynchronously; Cancel
or leaving the screen cancels it. Closing ATM also cancels and waits for pending
work. Network errors are reported without displaying partial results.

Use `/` or click the search field to filter the loaded snapshot without further
requests. Select a result with Enter or the mouse to inspect its metadata and
source commit. Escape returns from details to the filtered catalog. Reopening
the catalog fetches a new snapshot. No installation starts when selecting a row.

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
- Git blob identity checks are not release signatures. This is read-only catalog
  discovery, not a signed distribution or updater trust mechanism.
- There is no disk cache, download staging, receipt, preference write or process
  execution. `--dry-run` and `--demo` do not contact the network.
- A catalog result is not a guarantee of platform availability. Current legacy
  application plugins remain Linux-only. Native download, plugin code update and
  compiled CORE replacement need separate migration work and validation.

## Validation

Tests use an in-memory HTTP transport and isolated configuration directories:

```bash
go test ./internal/catalog ./internal/cli ./internal/tui ./internal/data ./internal/plugin ./internal/runner
go test -race ./...
go vet ./...
```

Linux tests and five-target cross-builds are not proof of execution on actual
Windows, macOS or ARM64 systems. Those acceptance checks remain separate.

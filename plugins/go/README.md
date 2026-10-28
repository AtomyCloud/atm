# Go Plugin

Plugin version: `0.0.1`. Native CORE API: `2` (CORE `0.0.2`).

The native Linux amd64/arm64 backend implements `jsonl-v1` and the operations
`menu`, `status`, `install`, `update`, `use`, `remove`, `uninstall`, and `integrate`.
The legacy Bash source remains for the separate legacy entrypoint; the native
backend never delegates Go logic to Bash, Python, curl, tar, or Go subprocesses.

## Commands

With a packaged native ATM runtime and compiled Go backend:

```text
atm plugin go menu
atm status go
atm install go
atm install go --version 1.26.2
atm update go
atm use go 1.26.2
atm remove go 1.26.1
atm uninstall go
atm plugin go integrate
```

CORE owns confirmation and execution. The offline root menu offers latest-five
discovery, custom exact version, update, status, use, remove, uninstall, and
integration. Submenus include a back choice. The official website is visible in
the menu title. Only explicit latest/install/update operations fetch releases.
CORE dry-run does not start the backend; `ATM_DRY_RUN=1` also prevents backend
filesystem, network, and subprocess operations.

## Releases and Safety

Release metadata comes from the fixed official
[Go JSON index](https://go.dev/dl/?mode=json&include=all). The latest five are
stable numeric releases with a Linux archive for the host architecture. Exact
versions (optionally prefixed `go`, including historical two-part versions) must
exist in that index with a valid archive filename, SHA-256, and bounded size.
Default install and Full Setup choose the latest compatible stable release;
update refuses a downgrade from the active version, even with `--version`.

Downloads require HTTPS and the pinned metadata filename, exact size, and hash.
The only cross-host redirect allowed is `go.dev/dl/<filename>` to
`dl.google.com/go/<filename>`, as used by the
[official download service](https://go.googlesource.com/website/+/71c2d285b86a517fbac3e662cbf461d326846b4d/internal/dl/server.go).
Metadata is limited to 16 MiB with a 20-second deadline; archives to 256 MiB,
expanded payload to 1 GiB, individual files to 256 MiB, and entries to 50,000.
Rooted extraction rejects traversal, links, special entries, duplicate paths,
special permissions, and receipt injection. Before publication it checks the
regular executable `bin/go` ELF architecture/type and matching `VERSION` file.
No downloaded binary is executed during installation or status.

## Layout and Ownership

The default root is `~/Apps/go` (`<request.apps_dir>/go`), containing numeric
version directories, `current`, and `workspace/bin`, `workspace/pkg`, and
`workspace/src`. Request `home` is ATM's runtime, not the user's home; profiles
are resolved using `os.UserHomeDir()`. Legacy environment path overrides are not
imported. Root/ancestor symlinks, unsafe current targets, and symlinked profiles
are refused. A valid current link may be absolute or relative inside this root.

New versions have `.atm-go-receipt.json` with exact file/directory inventory,
modes, sizes, hashes, and pinned artifact metadata. Native switching, removal,
and uninstall refuse modified or user-added payload files. Existing legacy
versions can be detected and selected, but are never adopted or deleted.
Remove refuses the active version. Uninstall removes only unchanged native-owned
versions, preserving legacy versions, unrelated files, and **the entire workspace
under all circumstances**. If legacy Go remains selected, its current link and
integration remain in place. Legacy manifests are not changed or trusted as
native ownership evidence.

Activation atomically replaces `current`. It updates only the plugin-owned
`ATM native go PATH` block in `.profile`, `.bashrc`, and an existing `.zshrc`,
preserving other ATM blocks and user content. It sets `GOROOT` to `current`,
`GOPATH` to `workspace`, and prepends `current/bin` plus `workspace/bin` to PATH.
Profile conflicts refuse writes; activation failures attempt profile rollback.
The emitted refresh command is for the user's current shell; a subprocess cannot
modify its parent shell environment.

Mutations use a private staging directory and exclusive root lock. Failed
downloads clean their temporary payload. A published version survives activation
failure for inspection. Abrupt process termination can retain a lock/staging
directory; inspect it before manual cleanup. This is not crash-transactional
multi-version uninstall. Receipts are local ownership evidence, not signatures
or protection against an attacker able to rewrite both payload and receipt.
HTTPS metadata hashes are not independent release signatures and provide no
independent freshness guarantee. Hostile same-user concurrent filesystem changes
and power-loss recovery are not covered by the ordinary rollback guarantees.

## Development and Acceptance

Build source checkouts with the packaging helper before native use. It prepares
ignored plugin outputs and checksums at the manifest's executable paths:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
bash scripts/build_manager.sh --plugins-local --target linux-arm64
go test ./plugins/go/native
go test -race ./plugins/go/native
go vet ./plugins/go/native
```

Linux-only tests use temporary homes and TLS fixtures, including failed/cancelled
downloads, archive rejection, ownership conflicts, legacy compatibility,
workspace preservation, profile rollback, no downgrade, both dry-run guards,
and the compiled JSONL backend via CORE with an empty PATH. They do not install
real Go releases or alter real profiles/apps. Linux amd64 automated acceptance is
verified locally; human acceptance and Linux arm64 runtime acceptance are Pending.
Windows/macOS backend implementation and runtime acceptance are Pending. Package
integration and full-repository gates are owned by the parent migration session.

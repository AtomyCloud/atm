# Node.js Plugin

Plugin `node`, version `0.0.1`.

## Native Linux Backend

`native/` is the Go JSONL-v1 backend for the compiled CORE (`0.0.2`). It supports
Linux amd64 and arm64; ARM64 runtime acceptance and all other hosts remain
Pending. Unsupported hosts fail explicitly. Packaging and CORE dispatch are
separate from this backend. The legacy Bash entry point remains below for reference.

Operations: `menu`, `status`, `install`, `update`, `use`, `remove`, `uninstall`,
and `integrate`. CORE obtains confirmation before mutation. Backend requests are
not an authentication boundary: directly running trusted plugin code bypasses
CORE confirmation. CORE dry-run does not launch the backend; the backend also
honors `ATM_DRY_RUN=1` without network, probes or writes.

```text
atm install node                         # latest official Linux release in major 24
atm install node --major 25
atm install node --version 22.5.1         # custom exact stable versions remain supported
atm update node --major 24
atm use node 24.22.1                      # select an already installed version
atm remove node 24.21.0                   # never removes current
atm plugin node menu
```

Major pages dynamically select the latest three compatible stable releases in
24/25/26 from `https://nodejs.org/dist/index.json`, numerically sorted and
deduplicated. There are no hardcoded latest/fallback versions. Offline discovery
fails explicitly; selecting an installed version needs no network. Exact versions
accept `24.22.1`, `v24.22.1` and `node-v24.22.1`.

Bare `atm update node` stays within the active portable version's major (24/25/26).
With no active version or another major, update requires explicit `--major` or
`--version`; it never silently defaults to 24. Bare install still defaults to 24.

Menus return `protocol.Choice` entries only on the terminal successful result,
at most 100 per page. `menu --major N`, `menu --installed use`,
`menu --installed remove`, and `menu --locations` select pages. The custom
version prompt appends one input to `install --version`; CORE confirms the result.
Menus never probe external programs. `status` runs bounded direct `--version`
probes of user-local, system and PATH-selected Node executables. These are trusted
local programs, not certification or adoption of their ownership.

### Downloads And Ownership

Archives are official `node-vVERSION-linux-ARCH.tar.gz`, checked against the exact
filename's SHA-256 in official `SHASUMS256.txt`. Both are fetched over HTTPS from
the same fixed official origin. **HTTPS checksums are not independent release
signatures**: compromise of that origin can replace both. No release signature,
anti-rollback or independent freshness guarantee is claimed. Runtime environment
variables cannot replace the release origin; tests inject private HTTPS clients.

Limits: 256 MiB compressed, 1 GiB expanded, 256 MiB per file, 30,000 archive
entries, 8 MiB index, 1 MiB checksums, five redirects and five-minute downloads
(CORE cancellation may be earlier). Native Go handles HTTP, hashing and tar/gzip;
there is no Bash/Python/curl/tar/xz delegation. Rooted extraction rejects traversal,
special entries, duplicate paths, escaping links and unsafe permissions. npm/npx
relative links and safe hardlinks must resolve to regular files within the payload;
link chains and directory symlinks are refused.

The layout remains `AppsDir/node/VERSION` and `AppsDir/node/current`. Request
`Home` means the ATM runtime root, never the user's home. User profiles use
`os.UserHomeDir()`. Symlinked application roots/ancestors are refused. Private
staging, an exclusive directory lock and atomic current-link replacement keep
the old version selected until download, verification, extraction and profile
preflight succeed. Failed activation can leave a complete new inactive version.
Ordinary errors/cancellation clean private stages. A killed process can leave a
lock/stage: inspect `.atm-native-lock` and `.atm-stage-*` manually while no backend
is running; no automatic deletion/adoption is attempted. No power-loss durability
or protection against a hostile same-user concurrent filesystem editor is claimed.

Each new version gets a native receipt recording the artifact and exact directory,
file hash/mode and link inventory. Removal/uninstall require an unchanged inventory.
**Global npm additions and any user edits intentionally block removal**; move or
remove those additions explicitly before retrying. Uninstall preflights all owned
versions, removes only unchanged owned versions, and preserves unrelated root
files, external installs and legacy versions. An interrupted uninstall may be
partial; it is not an all-versions transaction.

Legacy real `VERSION/bin/node` directories are listed and switchable without
adoption. They have no native receipt and cannot be deleted by this backend.
An existing `current` link must resolve directly to a valid version in this root;
external/dangling links and real directories are not overwritten. No external
`~/.local/bin/node`, `/usr/bin/node`, npm global tree or system package is removed.

### Native PATH Integration

Activation and `integrate` append/update only the plugin's own marked block in
`.profile`, `.bashrc`, and an existing `.zshrc`, after existing ATM blocks. Existing
ATM blocks and user text are preserved. Symlinked/special/oversized profiles and
malformed duplicate Node blocks fail before any profile write. Ordinary write
failures trigger guarded rollback; concurrent edits are not silently overwritten
during rollback. Uninstall removes only this native block when clearing the owned
current selection; a selected legacy version keeps its integration.

The block prepends `current/bin` and activation prints a shell-quoted
`export PATH=...:"$PATH"; hash -r` refresh command. A child process cannot change
the parent shell. Later profile commands, aliases and functions can still override
selection. Repeatedly sourcing profiles may duplicate PATH entries.

### Native System APT

```text
sudo -v
atm install node --location system --accept-system-packages
```

This requires CORE confirmation **and** the explicit flag, with Debian-family
Linux and `apt-get`. The location menu includes that flag in its confirmed action.
Missing consent fails without a command. Non-root calls use fixed `sudo -n --`
argument arrays; the protocol consumes stdin, so no password/interactive package
prompt is supported. Failure directs the user to run `sudo -v` in their terminal.

Commands are `apt-get update`, then `apt-get --no-remove --yes install nodejs npm`,
with `DEBIAN_FRONTEND=noninteractive`. Node `--version` and executable npm are
verified afterward (the one probe exception outside `status`). Probe overrides
`ATM_NODE_LOCAL_BINARY`, `ATM_NODE_SYSTEM_BINARY`, and `ATM_NODE_SYSTEM_NPM` retain
their legacy meaning; they do not change APT's installation destination.
APT uses existing configured repositories, potentially third-party ones; there
are no repository edits, full upgrades, removals, purge or autoremove. Version and
major selectors are rejected in system mode. Successful APT output may be
truncated without changing success; command output is never logged verbatim.

APT can install/upgrade dependencies and is **not transactional**. Local command
deadlines kill the direct child; subprocesses stay in CORE's process group for
runner cancellation. Privileged package-manager descendants may outlive an
unprivileged cancellation and need administrator recovery. No privileged rollback
or kill guarantee is claimed. System operations do not alter portable current,
profiles or create an ownership receipt. Removal remains the system manager's job.

### Native Validation

```bash
GOCACHE=/tmp/atm-go-build-cache GOMODCACHE=/tmp/atm-go-mod-cache \
  go test ./plugins/node/native
GOCACHE=/tmp/atm-go-build-cache GOMODCACHE=/tmp/atm-go-mod-cache \
  go test -race ./plugins/node/native
```

Linux tests use temporary HOME/Apps, private TLS servers and compiled native
command fixtures. They build the backend and execute it through CORE's real
native runner with an empty PATH, including terminal menus and process-group
cancellation. No test downloads real Node, invokes real APT/sudo, edits real
profiles, signs or publishes anything. Cross-builds do not establish runtime
acceptance. Native menu labels use embedded `native/en-us.json`; other locales
fall back to English and translation work remains Pending.

## Legacy Bash Entry Point (Reference)

The remaining sections describe `plugin.sh`, not the native backend. In particular,
its interactive package confirmation, receipts and profile-block behavior differ
from the native policies above.

## Installed Origins

The status and Node menu distinguish:

- **ATM**: the active portable version under `~/Apps/node/current`.
- **User local**: `~/.local/bin/node`, including an external version-manager link.
- **System**: `/usr/bin/node`, the usual APT executable path.

Option 4 lists managed versions, both external paths and the command selected by
the current process's PATH. External executables are trusted local programs queried
with `--version`, closed stdin, and a three-second timeout (plus one-second kill
grace). Failed/nonexecutable/unexpected output is unavailable. Without the `timeout`
utility, probes are skipped. Dry-run never executes these programs; managed
versions use directory metadata. Detection does not adopt external installations,
certify package ownership, or edit their links. Interactive aliases/functions in
the parent shell can differ from the command selected in ATM's own process.

`ATM_NODE_LOCAL_BINARY`, `ATM_NODE_SYSTEM_BINARY`, and `ATM_NODE_SYSTEM_NPM` may
override probe/verification paths. These do not tell APT where to put packages.

## Install Location

Main option **8) Install Node.js (ATM / system package)** offers:

```text
1) ATM portable (default): ~/Apps/node
2) System package (APT)
```

Enter chooses ATM, `b` or EOF cancels. CLI without a location prompts only when
interactive and no version/major was specified; noninteractive use defaults to ATM.
An exact official version or major selects portable installation unless explicitly
overridden. System mode rejects `--version` and `--major` rather than ignoring them.

```bash
atm install node --location atm --version 26.2.0
atm install node --location system
atm --dry-run install node --location system
```

Portable mode preserves the existing official release archives, major-version
menus and default version. Activation also refreshes ATM's managed PATH blocks
in `~/.profile`, `~/.bashrc`, and an existing `~/.zshrc`.
Version switching/removal and plugin uninstall continue to manage **ATM portable
versions only**, not distribution packages or user-local files.

## Activate A Portable Version

```bash
atm use node 26.2.0
```

Portable install and version switching put `current/bin` before `~/.local/bin`
in the managed PATH block. External Node/npm/npx executables and links are never
replaced. Other plugins retain their usual order; desktop files/system profiles
are not touched. Version switches remain effective through the same `current`
link once this PATH is loaded.

An executable cannot change its parent terminal's environment or command cache.
ATM prints a shell-escaped refresh command for the already-open terminal, e.g.:

```bash
export PATH="$HOME/Apps/node/current/bin:$PATH"
hash -r
node --version
```

New terminals load the persisted PATH. Aliases/functions or later shell setup
may still override it; inspect `type -a node` if necessary. Global npm packages
belong to each runtime and are not migrated. Run the patched ATM copy: a separate
portable `~/Apps/atm` deployment must also be updated from the checkout.

## System Package

Currently supported on Linux Debian/Ubuntu/Zorin with `apt-get`. Unsupported
systems fail explicitly; DNF/pacman/APK and third-party repository setup are not
implemented. The [Ubuntu package file list](https://packages.ubuntu.com/noble/armhf/nodejs/filelist)
shows the system executable path. The package name is `nodejs`, not `node`.

After an explicit confirmation (default No), ATM requests sudo authorization if
not root and runs argument arrays equivalent to:

```bash
sudo apt-get update
sudo apt-get --no-remove install nodejs npm
```

APT retains its normal transaction confirmation; ATM does not pass `-y`.
`--no-remove` refuses transactions requiring package removals. Dependencies may be
installed/upgraded, and an installation failure is not transactional rollback.
ATM does not run full-system upgrade, purge, autoremove, or add/repair repositories.
APT uses the configured repositories, which can already include third-party ones.
See the [APT manual](https://manpages.ubuntu.com/manpages/noble/man8/apt-get.8.html).

The repository determines the version; selecting system installation does **not**
promise a portable v24/v25/v26 release. After success, ATM verifies the system Node
version and npm executable presence and records a separate `node_system.manifest`.
It leaves `node.manifest`, `~/Apps/node/current`, user-local binaries, npm global
packages and shell settings untouched. This receipt does not give ATM ownership of
APT packages; use the system package manager separately for removal.

Installing a package is not switching the selected shell command. Another Node
can still precede `/usr/bin/node` on PATH. Inspect option 4 or use an absolute
path, such as `/usr/bin/node --version`; ATM never silently replaces a user-local
link or changes the parent's PATH. Global npm packages can differ across runtimes.

Dry-run prints planned commands without sudo prompts, APT/index operations,
version probes, manifests, caches or directory creation. A fresh portable dry-run
also avoids the shared downloader's directory creation.

## Validation

```bash
bash -n plugins/node/plugin.sh plugins/node/plugin.conf plugins/node/lang/en-us.lang
python3 -B -m unittest discover -s plugins/node/tests -p 'test_*.py' -v
```

Fixtures use temporary HOME/Apps/state and fake Node/npm/sudo/APT commands.
They cover selector PTY visibility, switching/removal, origins/probe failures,
location defaults/cancel/dispatch, rejected version flags, confirmed system
installation, privilege/package/verification failures, separate manifests,
preserved external files/current, and actual CLI no-write dry-run in both modes.
PATH fixtures additionally reproduce a competing user-local Node, verify fresh
Bash/Zsh resolution, cached-parent refresh and subsequent switching, npm's selected
runtime, profile preservation, generic priority defaults/reset/order, and no-write
activation dry-run. Automated fixtures perform no real package installation or
user runtime switch. New labels
use en-us fallback until translations stabilize; plugin version stays `0.0.1`.

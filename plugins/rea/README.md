# REA: Reverse Engineer Anything

ATM plugin `rea`, version `0.0.1`, menu order `16`; excluded from Full Setup.

Official sources: [REA repository](https://github.com/morluto/rea),
[installation guide](https://github.com/morluto/rea/blob/main/docs/installation.md),
[npm package](https://www.npmjs.com/package/rea-agents).

## Native Go Backend (ATM 0.0.2)

The schema-2/jsonl-v1 backend supports Linux amd64 and arm64. The native
operations are `menu`, `status`, `install`, `update`, `remove`, `uninstall` and
`integrate`. There is no `use`, individual-version removal or Full Setup action.
Legacy Bash/Python files remain unchanged and are used only by legacy `bin/atm`.
Source checkouts need a local native build; complete Linux packages include it:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
atm --yes plugin rea status
atm --yes install rea --trust-dependencies
atm --yes install rea --path "$HOME/REA tools" --version 6.8.0 --trust-dependencies
atm --yes plugin rea update --trust-dependencies
atm --yes plugin rea menu dependencies
atm --yes plugin rea menu dependencies --trust-dependencies
atm --yes plugin rea integrate
atm --yes uninstall rea
```

`install --update`, `install --dependencies` and `install --status` remain aliases.
Untrusted dependency diagnostics describe requirements without executing anything.
`--trust-dependencies` permits local Node/npm/ps execution; CORE confirmation is
still required and does not imply trust by itself. Menus offer default/custom
destinations and exact/latest versions, with normal default-Cancel confirmation.
No native action reads an interactive answer from its protocol stdin.

### Native Layout And Detection

The default base is `ATM_APPS_DIR/rea`, normally `~/Apps/rea`. An absolute
`--path BASE` or `ATM_REA_INSTALL_ROOT` selects another base. Native files live
under `BASE/native-v1`, not in the legacy/manual prefix:

```text
BASE/native-v1/
  .atm-rea-native-root
  versions/X.Y.Z/              npm prefix, native launcher and strict receipt
  current -> versions/X.Y.Z
  bin/rea -> ../current/bin/rea
```

The remembered destination is `ATM_STATE_DIR/rea/native-install.json`. Native
configuration loading honors both the environment and persisted `ATM_STATE_DIR`
in the selected ATM configuration file. This receipt takes precedence over the
default base. Remove the native installation before selecting another base.
Legacy `install.json` is never adopted or overwritten. Native acquisition uses
private temporary npm caches; legacy `ATM_REA_CACHE_DIR`, `ATM_REA_STATE_FILE`
and `ATM_REA_DEFAULT_VERSION` do not redirect native installation state or select
a version. `ATM_REA_STATE_FILE` is consulted only for read-only legacy detection.

Use a dedicated base, not HOME, the Apps directory, ATM itself, or an agent
configuration directory. Native root ancestors must be real directories; symlink
roots, overlapping state paths, controls and PATH separators are refused. Spaces,
apostrophes and literal shell-expansion characters are supported. Existing manual
files outside `native-v1` remain untouched; an unowned nonempty native root fails.

Status and root menus are offline/read-only and never execute REA, Node, npm or
ps. Detection checks the remembered/configured base, the legacy receipt's custom
base, `ATM_APPS_DIR/rea/bin/rea`, `~/Apps/rea/bin/rea`, `~/.local/bin/rea` and PATH.
Manual npm versions are read from `rea-agents/package.json` through a recognized
`scripts/rea.mjs` layout. Unknown layouts report version unavailable, not absence.
Broken links and nonexecutable files do not count. Native status checks its root
marker and exact launcher linkage; it is not a provider/runtime-readiness probe.

### Native Dependencies And Acquisition

Official registry metadata checked on 2026-10-28 identifies `rea-agents@6.8.0`,
entrypoint `scripts/rea.mjs`, and Node engine
`^22.23.4 || ^24.22.1 || >=26.12.1`. Installation resolves official npm metadata
dynamically, accepting `latest` or an exact stable `X.Y.Z`, never prereleases.
The verified 6.8.0 archive passed native structural inspection: 2,398 entries,
49,251,336 uncompressed bytes, no links/special files, and no npm shrinkwrap.
This proves metadata/archive compatibility, not a real npm install or runtime test.

Compatible Node/npm and native procps-compatible `ps` are required. Upstream's
[Linux owned-process prerequisite](https://github.com/morluto/rea/blob/main/docs/installation.md#linux-owned-process-prerequisite)
applies to REA child supervision, including static JavaScript/Electron analysis.
Python and coreutils `timeout` are no longer ATM helper dependencies. Python,
Java, Ghidra, Hopper, IDA, Frida and other provider requirements remain task-specific.

**Native automatic prerequisite repair is not implemented.** Manage Node through
the native Node plugin separately and install `procps`/`procps-ng` through your
distribution. No cross-plugin dispatch, APT/sudo, repository changes or automatic
provider preparation occurs. This is an explicit reduction from legacy automatic
repair, not a claim of full dependency-repair parity.

Trusted diagnostics inspect PATH and ATM's active Node. Invalid/non-native local
candidates can fall back to ATM Node; a process execution error or timeout stops
immediately. Probes use bounded output, deadlines and a temporary HOME. Node/npm
must resolve to a compatible native Node executable and npm's `npm-cli.js`.

Go downloads bounded metadata and the exact official tarball, verifies its npm
SHA-512 integrity, and inspects package identity, paths and acquisition metadata
before running npm. These HTTPS/integrity checks are not independent signatures.
Explicit foreign/git/file dependency forms are refused. A present shrinkwrap must
be a supported complete v2/v3 registry-only graph; legacy/unsupported lock formats
fail closed. Packages without shrinkwrap are accepted and resolved by trusted npm.
No custom transitive dependency resolver or network/process sandbox is claimed.
Transitive acquisition and integrity enforcement remain npm's responsibility.

The selected Node invokes npm directly with a private staging prefix, temporary
HOME/cache, empty user/global config files and a fresh environment. Inherited
`npm_config_*`, `NODE_OPTIONS`, `NODE_PATH` and unrelated private variables are
not passed to management processes. Install explicitly sets `--engine-strict=true`,
`--force=false`, `--ignore-scripts=true`, and disables audit/funding notifications.
Even same-version refresh performs a fresh staged npm install to enforce the
package's actual engines before changing the pinned runtime. Lifecycle scripts
remain disabled; optional native bindings may therefore be unavailable.

### Native Activation, Integration And Removal

The native launcher pins Node's resolved absolute path and SHA-256. It forwards
literal arguments, stdin/stdout/stderr, exit status and the working directory,
without a shell. It prepends the pinned Node and ps directories to a filtered PATH
without introducing current-directory lookup. Removing/changing pinned Node causes
a refusal; successful ATM install/update refreshes the runtime record. Ordinary
REA execution retains the user's application environment and private settings.

Acquisition stages first, validates the payload and records file hashes, modes and
contained relative links. Older versions stay on disk. State and root locks block
concurrent changes; destination state is revalidated under lock. Ordinary activation
failures roll back owned changes and preserve the prior selection. An inactive
published version may remain for inspection. These are not power-loss transactions.

PATH integration is explicit and prepend-only, using owned blocks in `.profile`,
`.bashrc` and an existing `.zshrc`. It does not replace `~/.local/bin/rea` or any
manual command. Edited/unowned blocks and linked profiles are refused, preserving
their content and modes. Reload the shell after integration, or use the absolute
`BASE/native-v1/bin/rea` command. Installation never starts REA setup/doctor,
registers agents, copies skills or changes an existing agent integration.

Removal validates all versions before deletion and removes only receipt-listed
entries, never recursively deleting the application root. Unknown/edited version
files, foreign launchers and malformed receipts cause refusal. Unrelated root
files, legacy/manual REA, Node/npm/OS packages, caches, captures, Evidence and
private `.codex`, `.claude` and `.agents` state are preserved. REA's own agent
integration cleanup is a separate, user-reviewed operation; ATM never runs it.

All management subprocesses inherit CORE's process group. Compiled runner tests
cover probe cancellation, internal probe deadline, execution failure, and staged
npm cancellation with descendants. CORE kills that group; standalone backend
execution does not guarantee descendant cleanup. Abrupt interruption may leave
locks/staging directories; inspect them manually rather than breaking locks
automatically. Partial removal can leave retained state for recovery; no crash
journal, running-agent migration or all-provider readiness is promised.

### Native Validation

Focused tests use compiled synthetic Node/npm/ps tools, isolated HOME/state and
TLS registry fixtures. They cover lifecycle/refresh, custom roots, legacy/manual
detection, strict receipts, unknown-file preservation, npm isolation, rollback,
launcher streams/empty PATH and actual CORE-supervised descendant cleanup. No real
npm package or REA executable was installed or run by these tests. Linux ARM64
runtime, real REA/provider workflows and translations remain pending.

```bash
GOCACHE=/tmp/atm-go-build-cache GOMODCACHE=/tmp/atm-go-mod-cache go test ./plugins/rea/native
GOCACHE=/tmp/atm-go-build-cache GOMODCACHE=/tmp/atm-go-mod-cache go test -race ./plugins/rea/native
go vet ./plugins/rea/native
```

## Legacy Backend Reference

The remaining sections describe the unchanged legacy `bin/atm` backend, including
its automatic repair and different root/state/PATH behavior. They do not describe
the native backend above.

### Legacy Menu

```text
REA: Reverse Engineer Anything
https://github.com/morluto/rea
https://rea.tools/

1) Install REA (choose destination)
2) Update REA
3) Remove ATM-managed REA
4) Check / install missing dependencies
5) Installation details / usage
b) Back
q) Exit
```

Installation offers the ATM directory (`~/Apps/rea` by default) or an absolute
custom directory. Use a dedicated empty directory, not a shared npm prefix such
as `~/.local`. Spaces and apostrophes are supported; colons/control characters,
double quotes, dollar signs, backticks, backslashes, symlink roots and shared
top-level directories are refused. The character restrictions keep custom roots
compatible with ATM's shell PATH blocks. An existing directory
must be empty or already owned by this plugin. No sudo is used for REA itself.

The successful destination is remembered for future update/status/removal.
One installation is managed at a time; remove it before choosing another root.
Settings overrides: `ATM_REA_INSTALL_ROOT`, `ATM_REA_CACHE_DIR`,
`ATM_REA_STATE_FILE`, `ATM_REA_DEFAULT_VERSION`. A saved destination takes
precedence over the default root; use `--path` for an explicit install destination.

## Commands

```bash
atm install rea
atm install rea --path "$HOME/Apps/REA tools"
atm install rea --version 6.8.0
atm install rea --update
atm install rea --dependencies
atm install rea --status
atm uninstall rea
atm --dry-run install rea
```

Use `bin/atm` from the development checkout until the plugin has also been
deployed to the portable ATM copy. Interactive unqualified install asks for the
destination; unattended install uses the remembered/default directory. `latest`
and exact stable `X.Y.Z` versions are supported; prereleases are not. Update uses
the latest published release by default. `atm remove rea` is an uninstall alias;
individual retained-version removal and `atm use rea` are not implemented.

## Dependencies

Initial support is Linux. Required prerequisites are Python 3 (state/ownership
helper), coreutils `timeout`, a procps-compatible `ps`, and compatible Node.js/npm.
The current published package, REA `6.8.0`, declares:

- Node 22.x >=22.23.4;
- Node 24.x >=24.22.1;
- Node >=26.12.1, stable only.

The plugin checks PATH and ATM's active Node installation. The launcher pins a
verified compatible Node executable and places its directory first for children,
so an older user-local Node or a later ATM version switch does not shadow it.
Do not remove that pinned runtime while using REA; updating REA refreshes the
launcher to the currently available compatible runtime, even at the same version.

Missing/incompatible prerequisites are listed and require explicit Yes before
repair. Enter, No and EOF abort installation without acquiring REA. Automatic
OS repair supports Debian/Ubuntu/Zorin with APT: install only the listed
`python3`, `coreutils` and/or `procps`, after `apt-get update`, with sudo when
needed. `--no-remove` refuses package transactions requiring removals; APT keeps
its native confirmation (no `-y`). Failures stop; package transactions are not
rolled back. Other distributions receive manual-install guidance.

Missing/incompatible Node/npm is repaired through the enabled ATM `node` plugin,
using the latest portable v26 and its normal manifest/profile activation. This
effect is included in the dependency approval message. No external Node links
are overwritten. Repair rechecks prerequisites and stops if they are still
unavailable. It never runs a full-system upgrade or adds repositories.

Optional analysis providers (Ghidra, Hopper, IDA, JADX/Java and native bindings)
are task-specific, reported separately and not automatically installed. This
plugin does not claim every analysis operation is ready after the base install.

## Installation And Updates

The plugin resolves `rea-agents@latest` through the fixed official npm registry,
then installs the exact resolved version in a private staging prefix. npm checks
package integrity; ATM checks package name/version and the expected CLI entrypoint.
`--engine-strict` also enforces the actual downloaded package's engines, including
future changes beyond the current prerequisite snapshot. `--ignore-scripts`
disables dependency lifecycle scripts; optional native providers may therefore
need manual build/binding preparation. Registry/package caches are scoped to ATM.

The root contains owned `versions/<version>` npm prefixes, `current`, `bin/rea`
and ownership markers. A JSON receipt under `ATM_STATE_DIR/rea/install.json`
remembers the destination; a normal ATM manifest records the active version.
Neither JSON state nor downloaded metadata is sourced as shell code. Older owned
versions stay available on disk. Updates install before switching `current`,
and failed download/install/identity verification preserves the prior version.
Publication/receipt failures may leave an inactive owned package for recovery;
avoid concurrent edits to managed directories. A lock blocks simultaneous ATM
install/update/removal; investigate an abandoned lock after interruption.

Use ATM's update action, not `rea update`, for these versioned prefixes: updating
the upstream prefix in place can invalidate ATM's version/ownership metadata.

## PATH And Agent Setup

Installation status does not require `rea` on PATH. It checks the saved/configured
root, `ATM_APPS_DIR/rea/bin/rea`, `~/Apps/rea/bin/rea`, `~/.local/bin/rea` and the
PATH executable, in that order (an active ATM-managed installation takes priority).
Only executable files count; broken links and directories do not. Manual npm
installations are labelled manual/external, with their absolute command and the
version read from the entrypoint's `rea-agents/package.json`. Unknown layouts or
missing/invalid metadata show version unavailable, not an absent installation.
Detection never executes REA, runs npm, modifies PATH, or creates ownership state.
When no PATH executable exists, status says so; menu option 5 prints commands for
the detected executable. This reports installation presence, not runtime readiness.
Manual installations remain external: ATM refuses to overwrite, update or remove
their prefix. Use a different empty destination for an ATM-managed installation.

Run `atm path apply` and reload the shell to include the remembered `bin` path,
or use the absolute launcher printed after installation. REA opts into ATM's
prepend PATH priority so the selected managed command precedes a user-local
REA executable without replacing it. Existing shell aliases/functions or later
PATH edits can still override that selection.

```bash
export PATH="$HOME/Apps/rea/bin:$PATH"
hash -r
```

With the default destination, the absolute commands are:

```bash
"$HOME/Apps/rea/bin/rea" --version
"$HOME/Apps/rea/bin/rea" --help
"$HOME/Apps/rea/bin/rea" setup
```

ATM never runs REA setup/doctor, edits agent MCP registrations, installs skills
or launches an analysis target automatically. Run setup yourself, review its
plan and approve the selected changes, then restart the selected agent. New npm
versions do not automatically migrate registrations pinned to older REA releases.

## Removal

Removal asks for confirmation and validates every owned version before deletion.
Unknown version directories, foreign launchers/links or an active lock cause a
refusal. Only owned package versions, scoped launchers, and the destination
receipt are removed; the ATM manifest is marked uninstalled. Empty directories
and the root ownership marker may remain. Unrelated root files, external REA
installations, Node/npm/OS packages, caches, captures, Evidence, agent settings
and skills are preserved. No whole-root `rm -rf`, purge or autoremove is used.

REA's own `uninstall` means agent-integration cleanup, not npm package removal.
Review and run it separately before ATM package removal when those integrations
also need cleaning. ATM does not silently alter an existing REA setup.

## Validation

```bash
bash -n plugins/rea/plugin.sh plugins/rea/plugin.conf plugins/rea/plugin.metadata plugins/rea/lang/en-us.lang
python3 -B -m unittest discover -s plugins/rea/tests -p 'test_*.py' -v
ATM_LANG=en-us bin/atm --dry-run install rea
```

Fixtures use temporary HOME/prefixes and fake Node/npm/APT/sudo; no real system
dependency is installed. Coverage includes destination persistence/escaping,
lifecycle, cancellation, runtime floors/fallback, same-version runtime refresh,
failed-update cleanup, ownership refusals, menu EOF and actual CLI no-write
dry-run in en-us/de. A separate isolated smoke test installed official npm REA
6.8.0, verified `--version`/`--help`, and removed it without agent setup. User
workflow/privileged dependency repair and translations remain pending.

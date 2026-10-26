# REA: Reverse Engineer Anything

ATM plugin `rea`, version `0.0.1`, menu order `16`; excluded from Full Setup.

Official sources: [REA repository](https://github.com/morluto/rea),
[installation guide](https://github.com/morluto/rea/blob/main/docs/installation.md),
[npm package](https://www.npmjs.com/package/rea-agents).

## Menu

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

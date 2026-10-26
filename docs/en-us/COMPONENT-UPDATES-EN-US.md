# CORE And Plugin Updates

These actions update ATM's own code, not Node, Docker, REA or other applications
installed by plugins. The source is fixed to `AtomyCloud/atm`, branch `main`;
plugin downloads always use its `plugins/` directory.

## Update Center

Open `u) Updates` in the main menu:

```text
1) Check CORE / plugin updates
2) Update CORE only
3) Update one plugin
4) Update ALL installed plugins
5) View file changes / recovery backups
b) Back
```

`p) Plugins` also provides update checking, a single-plugin selector and All.
Available/blocked plugin updates get a blue `Update` label beside their normal
tool status. The update-center label counts CORE and plugin candidates separately.
`NO_COLOR=1`, redirected output and dumb terminals use plain labels.

```bash
atm updates check
atm updates core
atm updates plugin brave
atm updates all
atm updates changes
atm updates backups
atm plugins check-updates
atm plugins update brave
atm plugins update --all
```

`atm updates` opens the menu. `atm self-update --check` aliases the component
check. All means installed plugins only, including disabled plugins; it never
installs new plugins or updates CORE implicitly. CORE must be updated separately,
then ATM restarted before plugins requiring its newer API can be updated.

## Independent Detection

Each check pins one official main commit and compares file paths, Git blob
hashes and executable modes for each component. A plugin-only commit does not
mark CORE or unchanged plugins as outdated. Plugin version `0.0.1` is descriptive,
not the update detector; version bumps are not required for detection.

CORE owns tracked files under `bin/`, `lib/`, `lang/`, plus `core-api.json`.
It excludes `plugins/`, public/internal documentation, tests, Git metadata,
application directories and user configuration/state. Each plugin owns only
its own official tracked files. README/tests inside a plugin are part of that
plugin's payload. Root README/LICENSE/docs are not replaced by CORE-only updates.

Statuses are Up to date, Update available, Locally modified, Unknown/offline,
Unverified baseline, Removed upstream, Requires review/CORE, Ahead or Diverged.
Ahead/diverged components are not automatically downgraded. Offline results
never claim components are current, and upstream removal never deletes a plugin.

Python 3.8+ is required for the component engine. Main-menu automatic checks
reuse a five-minute disk cache and a short in-process cache; uncached automatic
lookups are bounded by an eight-second timeout plus a one-second kill grace.
An explicit check allows up to 90 seconds. Without `timeout`, the main menu reads
the cache only. Set `ATM_UPDATES_AUTO_CHECK=0` to use cached badges and explicit
checks only. `GITHUB_TOKEN` is optional for official API rate limits/private access;
it is never sent to raw-content or third-party hosts or included in receipts.

Receipts, cached reports and backups are scoped to the resolved ATM/plugin roots:

```text
ATM_STATE_DIR/component-updates/<installation-id>/receipts/
ATM_STATE_DIR/component-updates/<installation-id>/backups/<backup-id>/
ATM_CACHE_DIR/component-updates/<installation-id>.json
```

Central receipts take precedence over embedded installation origins. Downloaded
plugins carry `.atm-origin.json`; older commit-only receipts are reconstructed
against that official commit. Portable setup records verified clean Git-source
baselines as `.atm-core-origin.json` and per-plugin origins. A Git checkout can
use its tracked HEAD as the baseline for detection without changing Git files.

An unreceipted portable component is adopted automatically only when its file
inventory matches the checked official component exactly. Older/unverifiable
copies stay Unverified; the detector never invents an old baseline. When the
original commit is known, explicitly verify it:

```bash
atm updates verify core <official-main-commit>
atm updates verify brave <official-main-commit>
```

Verification requires confirmation, official main ancestry and matching owned
files. It changes only the receipt, not component files. Custom tracked settings
must be reconciled manually; verification does not bypass file matching.

## Protected Updates

Before applying, ATM refreshes the check, shows the target/pinned commit and asks
for approval. It downloads into staging using the same fixed-source hash-checked
catalog transport, validates Bash/Python syntax without executing fetched code,
backs up the current component and verifies local files again before replacement.
Unknown local files are preserved; new upstream paths colliding with them block
updates. Only clean, previously owned files may be removed upstream.

Edited `plugin.conf`, `packages.txt`, `extensions.txt` and other owned files block
automatic replacement. There is no force-overwrite or automatic configuration
merge. Review the change list and reconcile customizations explicitly. Symlinks,
special files, oversized files/components, invalid origins and overlapping
CORE/plugin/state/cache scopes are rejected where they could affect replacement.

The update engine does not execute install/uninstall callbacks, install system
dependencies, run application migrations, edit shell profiles or change tool
installations. CORE API requirements use optional literal metadata:

```bash
ATM_PLUGIN_MIN_CORE_API_VALUE="1"
```

The default is 1. CORE declares its backwards-compatible plugin API in
`core-api.json` and `ATM_CORE_PLUGIN_API`; declarations must agree. Higher
requirements block plugin activation/update until CORE supports them. CORE
downgrades below installed plugin requirements are refused. A CORE payload must
declare update protocol 1, so older unprepared official snapshots are not
installed through the selective CORE updater.

All-plugin updates use independent component transactions. Failures/skips are
reported per plugin; already successful updates remain installed. A batch is
not globally atomic. Success requires an ATM restart; interactive menus exit
instead of continuing with old loaded Bash functions.

## Git Checkouts

Selective replacement refuses Git checkouts, including worktrees/nested Git
roots. This intentionally avoids silently modifying a development working tree.
The detector still works there. Use selective actions in a non-Git deployed
runtime with verified origins, or update a Git installation as a whole:

```bash
atm self-update --git
```

That explicit legacy action retains configured repository/branch and fast-forward
behavior and can update CORE and plugins together. It is not CORE-only. ATM
does not remove `.git`, reset local work, or migrate an installation automatically.
After development, commit/publish the patch and update other Git installations
through their normal workflow. Do not copy uncommitted updater files into another
Git checkout as a substitute for updating it.

## Recovery

List backups, inspect changes, then restore a successful update if needed:

```bash
atm updates backups
atm updates restore <backup-id>
```

Restore asks for confirmation and refuses changes made since the update or
tampered backup files. It restores only the recorded component and receipt.
Backups remain available; no automatic pruning is performed.

CORE replacement spans several directories, so it is not a single atomic swap.
A pending journal blocks normal `bin/atm` startup until publication or recovery
completes. Ordinary failures attempt rollback. If interrupted or rollback cannot
complete, read `ATM_HOME/.atm-core-update-pending`: it names the private backup
and a standalone recovery command using the copied engine, independent of the
possibly incomplete CORE. That recovery can reclaim a lock only when its owner
process no longer exists; it never kills a process. Inspect a PID-reused or
otherwise active lock manually. Interrupted staging directories may remain for
inspection; never remove them while an updater is running.

Close other ATM sessions and do not edit component files while updating/recovering.
Fingerprints and locks detect conflicts but cannot lock out arbitrary external
writers. Recovery also refuses newer edits instead of silently discarding them.
Do not run unrelated updates while CORE recovery is pending. Resume normal use
only after recovery completes and ATM has restarted.

`--dry-run` performs no network lookup, program probes, code replacement, receipt,
cache or backup writes. During active development, new update labels are en-us
with fallback for other locales; translations await stabilization.

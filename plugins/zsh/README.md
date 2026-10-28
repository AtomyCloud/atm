# Zsh + Oh My Zsh

Plugin **0.0.1**, CORE **0.0.2**, native schema 2 / JSONL-v1. Patch v29.12 adds
a Go backend for Linux amd64/arm64. Linux ARM64 runtime acceptance, other-platform
implementations and real APT/login-shell/profile interoperability remain **Pending**.
Full Setup is disabled. Legacy `plugin.sh`, `state.py`, metadata, configuration
and tests remain unchanged; native execution never falls back to Bash/Python.

## Menu And CLI

Open `atm plugin zsh` after building the native backend. The menu provides install,
update, remove, private backup, backup listing/restore and default-shell selection.
Root menu/status are offline and inspect path existence, never execute Zsh/Git
or read profile contents. Versions remain unknown rather than probing tools.

Install/update offer **Resolve latest official commit (network)** and manual full
40-character lowercase commit entry. Only the explicit `latest` choice contacts
the bounded official GitHub commits API, then returns a pinned confirmation.
Manual review is offline. Download happens only on confirmed execution.

```text
atm --yes install zsh --commit COMMIT --trust-official-omz --accept-system-packages --accept-snapshot
atm --yes plugin zsh update --commit COMMIT --trust-official-omz --accept-system-packages --accept-snapshot
atm --yes remove zsh --accept-system-packages --accept-removal --accept-snapshot
atm --yes plugin zsh menu backup --accept-snapshot
atm --yes plugin zsh menu backups
atm --yes plugin zsh menu restore /absolute/ARCHIVE --sha256 SHA256 --accept-restore --accept-snapshot
atm --yes plugin zsh menu shells
atm --yes plugin zsh use /registered/shell --accept-shell-change
```

`uninstall` aliases `remove`. Menu views `install`, `update`, `remove`, `backup`
are reviews; `menu install latest` / `menu update latest` explicitly discover.
Restore choices bind the reviewed archive's SHA-256 and required acknowledgements.
Dry-run never downloads, invokes commands or writes files.

## Framework Trust And Ownership

The fixed source is [ohmyzsh/ohmyzsh](https://github.com/ohmyzsh/ohmyzsh).
Downloads use `https://codeload.github.com/ohmyzsh/ohmyzsh/tar.gz/<full-commit>`
over verified HTTPS without redirects. Consent trusts GitHub/the official repository
and the selected commit, **not an independently signed release**. Recorded SHA-256
identifies downloaded bytes; it is not publisher authentication.

Extraction is bounded, private and staged beside the target. No upstream shell
installer/updater is executed. Official relative aliases are materialized as
regular-file copies only when their direct target is a validated regular file
inside that archive. Escapes, absolute links, chains, cycles, directory targets,
hardlinks, special entries and unexpected metadata are refused. No symlinks are
written. Limits: 32 MiB download, 64 MiB file content, 16 MiB/file, 20,000 entries
and 100 MiB tar-stream input. Future incompatible layouts fail closed.

An existing `.oh-my-zsh`, even empty/manual/legacy, is **not adopted or overwritten**.
Update/removal require the native receipt and matching complete framework inventory.
Added/edited custom plugins/themes inside the framework therefore block replacement
instead of being discarded. Review/reconcile edits manually after backing up;
there is no automatic merge or adoption command. External custom content is backed
up but not replaced by install/removal.

An existing `.zshrc` remains byte-for-byte unchanged; installation explicitly reports
**manual activation required**. Review it before setting `ZSH` to the native path
and sourcing `"$ZSH/oh-my-zsh.sh"`. Set `zstyle ':omz:update' mode disabled` before
sourcing, use ATM for updates and keep runtime caches outside the owned framework.
ATM never evaluates shell configuration.

Only an absent `.zshrc` receives a loader, with OMZ auto-update disabled, bundled
`robbyrussell` theme / `git` plugin, and cache/completion dumps outside the framework.
Its hash **and original absolute path** are tracked. Edits or changing `DOTDIR`
block lifecycle ownership, not authorize deletion of another same-content file.

## APT And Default Shell

Run as a normal local account, not root/setuid. System actions verify the unique
local `/etc/passwd` entry for the real UID and require the configured home to match.
Remote/NSS-only accounts are unsupported. Executables resolve from fixed system
directories, with root-owned, non-writable files/ancestry, never caller `PATH`.
Commands have literal argv and a minimal environment.

Install refreshes APT indexes then installs `zsh` and `git`; update refreshes then
uses `--only-upgrade zsh`. Native download/backup need neither curl nor Python.
Administrator repositories/preferences remain unchanged. Arguments explicitly
disable unauthenticated/insecure acquisition, autoremove and purge; install/update
also use `--no-remove`. Refresh uses `APT::Update::Error-Mode=any`: **modern APT
supporting that option is required**; older APT compatibility is not claimed.
Administrator-configured trust/policy still applies. APT maintainer scripts are
normal system-package effects, not native delegation to shell installers.

Elevation is `sudo -n -- <trusted-command> ...`. Authenticate separately or provide
suitable administrator policy. ATM does not read passwords, invoke `sudo -v`, change
repositories, delete package locks or retry elevation. System changes may be partial
and cannot be transactionally rolled back. Privileged/detached processes may survive
cancellation; inspect them before recovery.

Shell selection reads trusted `/etc/shells`, validates executable paths and marks
the current shell. Selecting the current shell is a no-op. A change uses literal
`sudo -n -- chsh -s SHELL VERIFIED_USERNAME`, rechecks identity/registration first
and verifies account state afterward. Success requires **logout/login**.
Installation/removal never changes the login shell implicitly.

Removal requires a non-Zsh requesting account and refuses while **any local passwd
account** uses Zsh, including resolved aliases/hardlinks to the system binary.
Unresolved/untrusted shell aliases fail closed. Checks repeat after the snapshot,
before APT. Remote directory accounts are not covered. Explicit APT removal can
affect dependents; review that risk. There is no autoremove/purge or Git removal.
Only verified framework data and an unchanged generated loader are displaced into
retained recovery directories. Other dotfiles, history, external custom content,
backups and cache remain.

## Private Snapshots

Default: `~/Apps/Backups/zsh-native-<timestamp>-<random>.json.gz`, using CORE's
effective Apps directory. Backup directories must be owned/private `0700`; archives
are `0600`. Format **`atm-zsh-native-v1`** is gzip JSON/base64 with individual SHA-256
hashes. It is **not encrypted** and can contain history, credentials and tokens.
Hashes detect changes, not a malicious author with the same account's access.

Snapshots include existing dot-directory `.zshrc`, `.zshenv`, `.zprofile`, `.zlogin`,
`.zlogout`, `.zsh_history`, `.zshrc.pre-oh-my-zsh`; home `.p10k.zsh` and
`.shell.pre-oh-my-zsh`; the OMZ tree; and separately configured external custom data.
These fixed root types never permit archive-supplied arbitrary destination paths.

Close Zsh sessions before accepting snapshots/restores. ATM rechecks bounded
inventories/bytes during capture and before replacement, but does not freeze writers
or promise live-session consistency. Data must be owned, not group/world writable,
and link-free. Symlinks, hardlinked files, specials, unsafe ancestry and overlapping
data/state/runtime/backup roots are refused, never dereferenced.

Legacy **`atm-zsh-backup-v1` `.tar.gz` archives are not imported**. Native listing
reports legacy archives in the configured backup directory as incompatible without
moving/deleting them. It does not search `~/Apps/backups/zsh` automatically. Retained
legacy tooling remains separate, with different link/import semantics.

## Restore And Recovery

Restore accepts only a private native archive directly in the configured backup
directory and verifies the reviewed whole-file SHA-256 **before any writes**.
Unknown/duplicate JSON fields, duplicate entries, bad root types, traversal, content
hash/mode errors, oversized input, gzip corruption and trailing streams are refused.
Limits: 100 MiB compressed/decoded input, 64 MiB file content, 16 MiB/file and
20,000 entries. Validated file/directory permission bits are restored exactly;
group/world writable and setuid/setgid modes are not accepted.

Restore first snapshots current data, stages on each target filesystem and replaces
**only roots present in the selected snapshot**. Missing roots are not deletion
instructions. Every displaced root remains in a sibling `.atm-zsh-recovery-*`
directory. It does not install packages or change login shells.

**Restore invalidates native installation ownership.** Archives cannot grant
installation/deletion authority; the prior receipt is retained separately, never
automatically reactivated. A restored framework can consequently refuse native
update/removal until manual review/reinstallation.

Private state is `<effective CORE StateDir>/zsh/native-v1`. An exclusive `operation`
directory records the action/phase, snapshot, staging and replacement/recovery paths.
Existing operations block mutations, including retries after failure/cancellation.
No automatic lock clearing, adoption or destructive recovery command is provided.

On publication failure ATM attempts to restore the immediately displaced target.
**Multi-root restore is not all-or-nothing**: earlier roots may remain replaced on
later failure/cancellation. Errors report partial effects and the journal location;
originals and the old receipt remain available. Inspect every recorded path, package
process and account state before manual recovery/clearing an operation. Never delete
recovery copies merely to unblock retry. No power-loss atomicity or hostile-same-user
race isolation is claimed. Completed operations can retain empty private staging
directories; these are not installed ownership or authority.

## Configuration

Native reads environment configuration, not shell-sourced plugin files.

| Variable | Default |
| --- | --- |
| `ATM_ZSH_USER_HOME` | environment home; verified against account for system actions |
| `ATM_ZSH_DOT_DIR` | `$ZDOTDIR`, otherwise home |
| `ATM_ZSH_OMZ_DIR` | `$ZSH`, otherwise `~/.oh-my-zsh` |
| `ATM_ZSH_CUSTOM_DIR` | `$ZSH_CUSTOM`, otherwise `<omz>/custom` |
| `ATM_ZSH_BACKUP_DIR` | `<effective AppsDir>/Backups` |

Paths must be clean/absolute with trusted non-symlink ancestry. Data roots may not
overlap the ATM runtime, Apps root, backups or private state. Permissions are checked,
not silently relaxed or repaired.

## Validation

```bash
go test ./plugins/zsh/native -count=1
go test -race ./plugins/zsh/native -count=1
go vet ./plugins/zsh/native
bash -n plugins/zsh/plugin.sh
bash plugins/zsh/tests/test_lifecycle.sh
python3 -B -m unittest discover -s plugins/zsh/tests -p 'test_*.py' -v
```

Focused Go tests use synthetic temporary data and injected provider/APT/sudo/chsh/
account functions. They cover lifecycle, local edits, readonly/consent/dry-run,
account aliases, path-bound ownership, archives, exact modes, command interruption
and partial-restore cancellation. The optional parent-owned public archive test
reads only explicitly supplied `ATM_TEST_ZSH_OFFICIAL_ARCHIVE` and
`ATM_TEST_ZSH_OFFICIAL_COMMIT`; it never executes downloaded code or accesses a
profile. Actual installation/authentication/shell startup, ARM64 runtime and human
acceptance remain Pending. No publication approval is inferred from automated tests.

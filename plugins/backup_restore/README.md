# ATM - Backup & Restore

Plugin ID: `backup_restore`. Version: `0.0.1`. Full Setup is disabled.

Open **ATM - Backup & Restore** from ATM's main menu, or run:

```bash
atm install backup_restore
```

This is a portable adaptation of the local user/destination workflows in
`atool-1.0.0-hotfix7.zip`. Python 3 is required (standard library only).
There is no system installation, automatic dependency installation, network
download, sudo, shell configuration execution, or PATH modification.

## Menu

```text
🛡️ ATM — Backup & Restore Tool
Version: 0.0.1

🧭 Main menu
=========
1) 📦 Create backup
2) 🔄 Restore backup
0) Back
q) Exit
Choose option [0]:
```

**Create backup** opens User selection, Backup type, and Backup destination.
The user picker offers `0) All users`, numbered home-directory users,
`m) Manual username`, and `q) Cancel` (the default). Like the ZIP, discovery
starts with real directories under `/home`, ignores symlinked/unsafe names and
root/service accounts, and includes the caller's configured home when suitable.
Known accounts with UID below 1000 or at/above 65534 are excluded. Orphaned
regular home-directory names remain selectable, following the original tool.
Manual names must match a discovered home; arbitrary paths/accounts are refused.

Backup type offers Full, Main folders, Safe (default), and Custom. Safe uses
`excludes.txt`; Main includes existing Desktop, Documents, Downloads, Pictures,
Videos, Music; Custom takes relative paths, one per prompt, applied to every
selected user. The whole operation fails if any selected home/path is unreadable
or missing; there is no silent user skipping or automatic sudo/elevation.

**Restore backup** asks for a snapshot root, then lists the users stored in that
snapshot, with the same All/number/manual/cancel choices. Enter a separate
absolute restore target. One selected user restores directly into it; All users
restores into `TARGET/USERNAME/` for every stored user. It does not depend on
those accounts still existing on the destination system.

List and Verify remain available through CLI actions shown below.

All mutations show a plan and ask for confirmation. Close applications and avoid
concurrent changes while copying. This is a file-level backup, not a filesystem
snapshot; live databases and changing application files may be inconsistent.

## Storage

The default is **`~/Apps/Backups`** (`$ATM_APPS_DIR/Backups`), independent of
the ATM checkout. Choose `0` or press Enter in the destination picker to use
it; an existing `ATM_BACKUP_RESTORE_DIR` override is preserved. Existing
snapshots are not moved or deleted and remain valid restore sources.

Connected mounted devices directly under `/media/$USER/<DEVICE>` appear as
numbered destinations, including their source, filesystem, and free GiB.
Discovery reads Linux `/proc/self/mountinfo`, correctly decoding spaces, rather
than assuming every directory is a connected drive. Read-only devices are
marked and cannot be selected. Symlinks, unmounted directories, control-character
paths, and ambiguous stacked mounts are not offered. `m` accepts a manual
absolute destination; `q` cancels. Devices are neither mounted nor formatted.

For media destinations, the helper checks mount presence/identity before work,
between users, and before publication. Missing/replaced/read-only devices fail
instead of intentionally falling back to local storage. Do not unmount/unplug
devices while copying; checks cannot eliminate every concurrent-unmount race.
If a device disappears mid-copy, its private `.pending-*` directory may remain
for manual review when reconnected; no successful snapshot is published.
Manual paths outside the configured media root require the operator to verify
any expected mount. Listing uses the configured destination (or CLI override).

```text
DESTINATION/atool-backups/HOST/YYYY-MM-DD_HHMMSS-ID/
  atool-backup.json
  users/USERNAME/...
  logs/summary.txt
```

One operation publishes one snapshot for all selected users, only after every
copy succeeds. Single-user snapshots retain ATM format 1; multi-user snapshots
use format 2 with separate SHA-256 inventories. Readers accept both plus legacy
atool schema 1.0. The repository writes an owned `atool-backups/.gitignore`
containing `*` to keep new private snapshots out of accidental Git staging.
It refuses an incompatible/symlinked ignore file instead of overwriting it.
This does not untrack previously committed files or replace storage encryption.

Snapshots are independent directory copies, not compressed archives or hardlink
snapshots. The chosen destination subtree is always excluded, including for Full
backups, to prevent recursive copies. Safe exclusions apply to every matching
basename or relative path. Main folder names use the English names above;
use Custom for localized XDG folder names.

Snapshot roots are private (`0700`); metadata is `0600`. Regular file modes and
timestamps are copied. Setuid/setgid/sticky bits are stripped. Ownership, ACLs,
hardlink relationships, filesystem flags, and portable xattr preservation are not
guaranteed. Symlinks are copied as links and never traversed; their targets are
not backed up and links may point outside the restored directory. Sockets, FIFOs,
and devices are skipped during backup and listed in its metadata/result. Other
copy failures abort publication rather than report a successful snapshot.

**Backups are not encrypted.** They can contain keys, passwords, cookies,
history, and other sensitive information. Protect the storage medium.
SHA-256 detects accidental content changes, not an attacker who can also replace
metadata. A failed copy may leave newly created empty repository parents, but
no successful snapshot; cancellation and dry-run create no backup/restore artifacts.
Outside dry-run, ATM's normal startup may initialize its own configuration/logs.
Metadata is limited to 64 MiB; oversized inventories abort publication. Split
very large homes into several Custom backups if necessary.

## Restore

Restore accepts a successful, non-dry-run `atool-backup.json` schema 1.0 backup
root with real `users/USERNAME` directories. Select one user or All users;
CLI multi-user sources require `--user` or `--all-users`. Legacy
snapshots have no stored hashes: validation checks metadata/tree structure but
cannot prove that content is unchanged. Partial, failed, or malformed backups
are refused. No metadata or configuration is shell-sourced.

Choose a **separate destination**, for example `~/Restored/home`. Direct restore
over a discovered live user home, system directories, ATM's checkout, overlapping
backup paths, or symlinked destination components is blocked. Review restored
files before moving them back into active use, especially scripts/configuration.

For an existing destination, ATM stages a full merged copy beside it: matching
files are replaced, unrelated files remain. After validation, it renames the old
directory to `TARGET.before-atm-restore-TIMESTAMP-ID` and publishes the staged
copy. This recovery copy is retained, never automatically deleted. Rename
failures trigger rollback; if rollback also fails, its location is reported.
Original backup files are not modified. Allow disk space for both the staging
copy and retained original. Restore refuses special files in either tree.

## CLI

The `install` callback opens this built-in utility; it does not install software.
Explicit actions are also available (Backup and Restore still ask for confirmation):

```bash
atm install backup_restore --action backup --type safe
atm install backup_restore --action backup --user alice --type safe
atm install backup_restore --action backup --all-users --type safe
atm install backup_restore --action backup --type custom --path Documents --path .zshrc
atm install backup_restore --action backup --type full --destination /mnt/backup
atm install backup_restore --action list
atm install backup_restore --action verify --source /mnt/backup/atool-backups/HOST/ID
atm install backup_restore --action restore --source /mnt/backup/atool-backups/HOST/ID --target "$HOME/Restored/home"
atm install backup_restore --action restore --all-users --source /mnt/backup/atool-backups/HOST/ID --target "$HOME/Restored/users"
atm --dry-run install backup_restore --action backup --type safe
```

Use `--user USERNAME` or `--all-users` when restoring/verifying a multi-user
snapshot. They are mutually exclusive. On backup, `--user` now selects that
discovered user's actual home, not a label for the caller's home. With neither
flag, CLI backup still defaults to the caller's configured home.
Paths with spaces must be quoted. Dry-run validates and prints plans without
creating directories, temporary files, logs, manifests, or backups. Read-only
List/Verify remain read-only. Remove/uninstall/version-switch actions do not
delete backups; this utility ships with ATM and has no external install state.

Environment/config overrides:

| Variable | Default |
| --- | --- |
| `ATM_BACKUP_RESTORE_HOME` | `$HOME` |
| `ATM_BACKUP_RESTORE_DIR` | `$ATM_APPS_DIR/Backups` (normally `~/Apps/Backups`) |
| `ATM_BACKUP_RESTORE_EXCLUDES` | Plugin-local `excludes.txt` |
| `ATM_BACKUP_RESTORE_HOME_BASE` | `/home` |
| `ATM_BACKUP_RESTORE_MEDIA_ROOT` | `/media/$USER` |
| `ATM_BACKUP_RESTORE_MOUNTINFO` | `/proc/self/mountinfo` (fixture override for tests) |

Use the normal account. No automatic privileged access to other users is offered.

## Scope And Origin

Adapted from the supplied **atool 1.0.0 hotfix7**, by Atomy Cloud, whose source
declares `GPL-3.0-only`. Archive SHA-256:

```text
33e54de51b69afb7ac38214280fbb90769fe91131d6e0c0bfc0842f8af6c482f
```

The ZIP was inspected, not executed or installed. This plugin is an independent
implementation of its local file workflows and schema, not a bundled copy of
the GPL Bash program. Newly written files follow ATM's root MIT license;
the original archive remains GPL-3.0-only and is not redistributed here.

Still not ported: privileged root access, remote SSH/rclone
transfers, GPG/age/rclone-crypt, retention deletion, snapshot hardlinks, mirror
deletion, original system installer, and original locales. Existing local
atool snapshots can be restored as described above. This is not a complete
replacement for every original atool option. New ATM UI labels are en-us only
during development.

## Validation

```bash
bash -n plugins/backup_restore/plugin.sh
bash plugins/backup_restore/tests/test_lifecycle.sh
python3 -B -m unittest discover -s plugins/backup_restore/tests -p 'test_*.py' -v
```

Tests use disposable directories, never real personal files. Real environment
backup/restore validation remains manual.

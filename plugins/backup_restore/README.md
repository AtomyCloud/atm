# ATM - Backup & Restore

Plugin ID: `backup_restore`. Version: `0.0.1`. Full Setup is disabled.

Open **ATM - Backup & Restore** from ATM's main menu, or run:

```bash
atm install backup_restore
```

This is a portable, current-user adaptation of the local workflows in
`atool-1.0.0-hotfix7.zip`. Python 3 is required (standard library only).
There is no system installation, automatic dependency installation, network
download, sudo, shell configuration execution, or PATH modification.

## Menu

1. **Safe backup**: home contents, excluding the patterns in `excludes.txt`.
2. **Full backup**: home contents without those optional exclusions.
3. **Main folders**: existing Desktop, Documents, Downloads, Pictures, Videos, Music.
4. **Custom paths**: enter relative paths inside your home, one per prompt.
5. **Restore backup**: enter a backup root and an absolute destination directory.
6. **List backups**: successful single-user backups in the configured repository.
7. **Verify backup**: check metadata and, for new ATM snapshots, content hashes.

All mutations show a plan and ask for confirmation. Close applications and avoid
concurrent changes while copying. This is a file-level backup, not a filesystem
snapshot; live databases and changing application files may be inconsistent.

## Storage

The default destination is `~/Apps/Backups`. A different absolute destination can
be entered for each backup, including an already mounted external disk or NAS.
ATM does not mount drives or check that an expected mount is present; verify
the selected mount before proceeding. Listing uses the configured destination.

```text
DESTINATION/atool-backups/HOST/YYYY-MM-DD_HHMMSS-ID/
  atool-backup.json
  users/USERNAME/...
  logs/summary.txt
```

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
root with a real `users/USERNAME` directory. Original multi-user atool backups
require an explicit user name; one user is restored per operation. Legacy
snapshots have no stored hashes: validation checks metadata/tree structure but
cannot prove that content is unchanged. Partial, failed, or malformed backups
are refused. No metadata or configuration is shell-sourced.

Choose a **separate destination**, for example `~/Restored/home`. Direct restore
over the current live home, system directories, ATM's checkout, overlapping
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
atm install backup_restore --action backup --type custom --path Documents --path .zshrc
atm install backup_restore --action backup --type full --destination /mnt/backup
atm install backup_restore --action list
atm install backup_restore --action verify --source /mnt/backup/atool-backups/HOST/ID
atm install backup_restore --action restore --source /mnt/backup/atool-backups/HOST/ID --target "$HOME/Restored/home"
atm --dry-run install backup_restore --action backup --type safe
```

Use `--user USERNAME` when restoring/verifying an original multi-user snapshot.
Paths with spaces must be quoted. Dry-run validates and prints plans without
creating directories, temporary files, logs, manifests, or backups. Read-only
List/Verify remain read-only. Remove/uninstall/version-switch actions do not
delete backups; this utility ships with ATM and has no external install state.

Environment/config overrides:

| Variable | Default |
| --- | --- |
| `ATM_BACKUP_RESTORE_HOME` | `$HOME` |
| `ATM_BACKUP_RESTORE_DIR` | `$ATM_APPS_DIR/Backups` |
| `ATM_BACKUP_RESTORE_EXCLUDES` | Plugin-local `excludes.txt` |

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

Not ported in this initial patch: root/all-user backup, remote SSH/rclone
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

# Brave Backups (Legacy v0.0.1)

## Full Backup

Select **Brave > 6) Full Backup (standard / Flatpak / Snap)**, also available
under **4) Backup / Restore profile > 5) Full Backup**.

ATM detects existing data directories, even if the browser command is not on PATH:

| Installation | Data directory |
|---|---|
| Standard (APT / DEB / RPM / AUR) | `~/.config/BraveSoftware/Brave-Browser/` |
| Flatpak | `~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser/` |
| Snap | `~/snap/brave/current/.config/BraveSoftware/Brave-Browser/` |

If several exist, **all detected directories** go into one archive. Snap's
`current` link is resolved so the data, not just the link, is backed up.
`ATM_BRAVE_PROFILE_DIR` can override the standard location. Duplicate physical
sources are included once. No browser installation or sudo is required.

Default output (case-sensitive):

```text
~/Apps/Backups/brave-2026-11-04_14-30-00-<unique-id>.tar.gz
```

`ATM_BRAVE_FULL_BACKUP_DIR` overrides this new destination. The date/time is local;
the unique suffix prevents collisions. Archives have permission `0600` and are
not encrypted. Keep them private: they contain history, cookies, extensions,
preferences, bookmarks, saved-login databases and every other ordinary file in
the detected directories, including hidden files and all profiles.

Close **all Brave instances** first. A process check (requiring `pgrep`) runs
before compression and again before publication. This is not a live snapshot;
do not restart Brave during the backup. Read/write failures do not publish a final
archive. Completed backups never overwrite an existing file. Abrupt process
termination may leave a hidden `.part` file, not a completed backup.

Archive layout: `standard/Brave-Browser/`, `flatpak/Brave-Browser/`,
`snap/Brave-Browser/`, plus `backup.json` recording the detected source locations.
Internal symlinks are preserved, not followed; external data and OS keyrings are
not copied. Sockets are not archived. This backup alone does not guarantee that
encrypted credentials will work on a different system.

Python 3 is required. `--dry-run` reports detection and destination without writes.
The destination must not be inside any detected source. Existing profile restore
and bookmark actions are unchanged, including their old destination. **Restore
latest profile backup does not restore this new multi-installation format**;
automatic full-backup restore is not included in this feature.

## Bookmarks Backup

Select **Brave > 5) Backup bookmarks (all profiles)**. The same action is
available under **4) Backup / Restore profile > 4) Backup bookmarks (all profiles)**.

Close Brave before starting. ATM reads `Bookmarks` from `Default` and every
`Profile <number>` directory, including multi-digit profile numbers, under:

```text
~/.config/BraveSoftware/Brave-Browser/
```

`ATM_BRAVE_PROFILE_DIR` overrides the source location. Profiles without a
`Bookmarks` file are skipped. If no bookmarks are found, no archive is created.

All matching files go into one private, unencrypted archive (mode `0600`):

```text
~/Apps/backups/brave/brave-bookmarks-<timestamp>-<id>.tar.gz
```

`ATM_BRAVE_BACKUP_DIR` overrides the output location. The archive preserves each
profile's relative path, for example `Default/Bookmarks`, `Profile 1/Bookmarks`,
and `Profile 12/Bookmarks`. Symlinked bookmark files are saved as file contents.
Cookies, passwords, history, and other profile files are not included.

Existing profile backup/restore remains unchanged. Bookmarks archives have a
separate filename prefix so **Restore latest profile backup** cannot select one
by mistake. Bookmarks-only restore is not part of this action; the saved files
are Brave's native `Bookmarks` files, not an HTML bookmark export.

`--dry-run` shows included profiles and the archive plan without creating files.
The archive is published only after compression succeeds; failed temporary
archives are cleaned up. No sudo is used for bookmarks backups.

## Validation

```bash
bash -n plugins/brave/plugin.sh
bash plugins/brave/tests/test_bookmarks.sh
PYTHONDONTWRITEBYTECODE=1 python3 plugins/brave/tests/test_full_backup.py
```

Automated tests use temporary profiles and do not touch personal browser data.
Real profile validation remains manual. New menu labels are in `en-us` during
active development. Plugin version remains `0.0.1`.

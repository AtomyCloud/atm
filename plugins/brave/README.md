# Brave Bookmarks Backup

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
```

Automated tests use temporary profiles and do not touch personal browser data.
Real profile validation remains manual. New menu labels are in `en-us` during
active development. Plugin version remains `0.0.1`.

# Zsh + Oh My Zsh

ATM plugin version: `0.0.1`. Initial platform support: APT-based Linux
(Ubuntu, Debian, Zorin OS). Run ATM as your normal account; package operations
request sudo. Zsh is a system package, not a portable installation.

Open ATM's main menu and select **Zsh + Oh My Zsh**:

1. Install Zsh + Oh My Zsh
2. Update Zsh + Oh My Zsh
3. Remove Zsh + Oh My Zsh
4. Create backup
5. Restore backup
6. List backups
7. Set Zsh as default shell

Installation is also available with `atm install zsh`. The plugin is excluded
from Full Setup. APT manages Zsh's version; ATM does not switch shell versions.

## Installation And Updates

The plugin installs `zsh`, `git`, `curl`, and `python3` using APT, then uses the
[official Oh My Zsh installer](https://ohmyz.sh/#install) in unattended mode,
preserving an existing `.zshrc`. It adds a guarded loader only when no standard
Oh My Zsh loader is present. Your account's login shell is not changed unless
you select option 7 and confirm it. Updates use APT and Oh My Zsh's own updater.

## Backups And Restore

Default backup location: `~/Apps/backups/zsh/zsh-<timestamp>-<id>.tar.gz`.
Archives are private (mode `0600`), but **not encrypted**. They can contain shell
history, tokens, and private settings. Keep copies somewhere safe.

Snapshots include existing files from these locations:

- `$ZDOTDIR` (or home): `.zshrc`, `.zshenv`, `.zprofile`, `.zlogin`, `.zlogout`,
  `.zsh_history`, and `.zshrc.pre-oh-my-zsh`.
- Home: `.p10k.zsh` and `.shell.pre-oh-my-zsh`.
- The Oh My Zsh directory, including custom plugins and themes.
- A separately configured external custom directory.

Symlink contents are copied into the archive. Restore preserves an existing
top-level destination symlink by updating its target; nested links become
ordinary files/directories. File and directory permissions are preserved.
Archives containing links, special files, unexpected roots, or unsafe paths
are rejected. Only ATM Zsh backups can be restored, not arbitrary tar archives.

Close other Zsh sessions before backup/restore so they cannot rewrite history
or configuration during the operation. Install, update, removal, and restore
save the existing state first. Restore validates the selected archive before
creating this safety snapshot, stages files before replacement, and rolls back
on a replacement failure. If rollback itself fails, it reports and retains
recovery directories instead of discarding the old files.

Restore replaces only the entries present in the selected snapshot. It does
not reinstall the APT package or change the login shell. Unrelated settings
remain untouched. If necessary, install Zsh again before starting a new session.

## Removal

Removal requires confirmation, creates a backup, and switches a Zsh login shell
to Bash before removing the system Zsh package. A failed shell change aborts
package removal. Oh My Zsh's directory is removed, its loader is disabled, and
the manifest is cleared. Dotfiles, backups, and an external custom directory
are retained. Dependencies such as Git and Python are not removed.

## Configuration

Override these variables before launching ATM or in its trusted configuration:

| Variable | Default |
| --- | --- |
| `ATM_ZSH_USER_HOME` | `$HOME` |
| `ATM_ZSH_DOT_DIR` | `$ZDOTDIR`, otherwise home |
| `ATM_ZSH_OMZ_DIR` | `$ZSH`, otherwise `~/.oh-my-zsh` |
| `ATM_ZSH_CUSTOM_DIR` | `$ZSH_CUSTOM`, otherwise `<omz>/custom` |
| `ATM_ZSH_BACKUP_DIR` | `$ATM_APPS_DIR/backups/zsh` |

Backups must be outside the framework and external custom directories.
Shell configuration is never executed by the backup/restore helper.
`--dry-run` prints the plan without modifying files or invoking downloaded tools.

## Validation

```bash
bash -n plugins/zsh/plugin.sh
bash plugins/zsh/tests/test_lifecycle.sh
python3 -B -m unittest discover -s plugins/zsh/tests -p 'test_*.py' -v
ATM_LANG=en-us bin/atm --dry-run plugins list
ATM_LANG=en-us bin/atm --dry-run install zsh
```

The automated tests use temporary profiles and mocked package/shell commands.
Actual installation, authentication prompts, and login-shell changes require
manual verification. New menu text is currently in `en-us` during development.

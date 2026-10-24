#!/usr/bin/env bash
set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
temp="$(mktemp -d /tmp/atm-backup-lifecycle.XXXXXX)"
trap 'rm -rf -- "$temp"' EXIT
export PYTHONDONTWRITEBYTECODE=1
export HOME="$temp/home"
ATM_APPS_DIR="$HOME/Apps"
mkdir -p "$HOME/Documents"
printf 'temporary user document\n' > "$HOME/Documents/file"
source "$repo/plugins/backup_restore/plugin.conf"
source "$repo/plugins/backup_restore/lang/en-us.lang"
source "$repo/lang/en-us.lang"
source "$repo/plugins/backup_restore/plugin.sh"
atm_t() { printf '%s\n' "${!1:-$1}"; }
atm_info() { printf '%s\n' "$*"; }
atm_warn() { printf '%s\n' "$*"; }
atm_success() { printf '%s\n' "$*"; }
atm_require_commands() { command -v "$1" >/dev/null; }

ATM_DRY_RUN=1 atm_backup_restore_install --action backup --type custom --path Documents > "$temp/plan"
[[ ! -e "$ATM_BACKUP_RESTORE_DIR" ]]
grep -q '"dry_run": true' "$temp/plan"
atm_backup_restore_install --action backup --type full <<< n > "$temp/cancel"
[[ ! -e "$ATM_BACKUP_RESTORE_DIR" ]]
grep -q 'Cancelled' "$temp/cancel"
atm_backup_restore_install --action backup --type safe <<< y > "$temp/result"
backup="$(atm_backup_restore_state list --destination "$ATM_BACKUP_RESTORE_DIR")"
[[ -f "$backup/atool-backup.json" ]]
atm_backup_restore_install --action verify --source "$backup" > "$temp/verify"
grep -q '"valid": true' "$temp/verify"
ATM_DRY_RUN=1 atm_backup_restore_install --action restore --source "$backup" --target "$temp/restored" > "$temp/restore-plan"
[[ ! -e "$temp/restored" ]]
atm_backup_restore_install --action restore --source "$backup" --target "$temp/restored" <<< n > "$temp/cancel-restore"
[[ ! -e "$temp/restored" ]]
atm_backup_restore_install --action restore --source "$backup" --target "$temp/restored" <<< y > "$temp/restore"
cmp "$HOME/Documents/file" "$temp/restored/Documents/file"
if atm_backup_restore_install --action backup --unknown > "$temp/error"; then exit 1; fi
if atm_backup_restore_install --action > "$temp/error"; then exit 1; fi
if atm_backup_restore_install --action restore --source "$backup" --target "$HOME" > "$temp/error" 2>&1; then exit 1; fi
if SUDO_USER=someone atm_backup_restore_install --action backup > "$temp/error"; then exit 1; fi
atm_backup_restore_remove > "$temp/remove"
atm_backup_restore_uninstall > "$temp/uninstall"
[[ -f "$backup/atool-backup.json" ]]
atm_backup_restore_menu <<< b > "$temp/menu"
grep -q 'ATM - Backup & Restore' "$temp/menu"
[[ -z "$(atm_backup_restore_path_entries)" ]]
printf 'PASS backup/verify/restore, cancellation, no-write dry-run, CLI validation, sudo guard, retained backups, and menu\n'

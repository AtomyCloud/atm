#!/usr/bin/env bash
set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
temp="$(mktemp -d /tmp/atm-backup-lifecycle.XXXXXX)"
trap 'rm -rf -- "$temp"' EXIT
export PYTHONDONTWRITEBYTECODE=1
export HOME="$temp/home"
ATM_APPS_DIR="$HOME/Apps"
ATM_HOME="$ATM_APPS_DIR/atm"
ATM_BACKUP_RESTORE_HOME="$HOME"
ATM_BACKUP_RESTORE_EXCLUDES="$repo/plugins/backup_restore/excludes.txt"
unset ATM_BACKUP_RESTORE_DIR
ATM_BACKUP_RESTORE_HOME_BASE="$temp/home-base"
ATM_BACKUP_RESTORE_MEDIA_ROOT="$temp/media"
ATM_BACKUP_RESTORE_MOUNTINFO="$temp/mountinfo"
mkdir -p "$ATM_BACKUP_RESTORE_HOME_BASE" "$ATM_BACKUP_RESTORE_MEDIA_ROOT"
touch "$ATM_BACKUP_RESTORE_MOUNTINFO"
mkdir -p "$HOME/Documents"
printf 'temporary user document\n' > "$HOME/Documents/file"
source "$repo/plugins/backup_restore/plugin.conf"
(
    ATM_BACKUP_RESTORE_DIR="$temp/custom-backup-override"
    source "$repo/plugins/backup_restore/plugin.conf"
    [[ "$ATM_BACKUP_RESTORE_DIR" == "$temp/custom-backup-override" ]]
)
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
grep -q 'Backup & Restore Tool' "$temp/menu"
[[ -z "$(atm_backup_restore_path_entries)" ]]
printf 'PASS backup/verify/restore, cancellation, no-write dry-run, CLI validation, sudo guard, retained backups, and menu\n'

mkdir -p "$ATM_BACKUP_RESTORE_HOME_BASE/alpha/Documents" "$ATM_BACKUP_RESTORE_HOME_BASE/beta/Documents"
printf 'alpha data\n' > "$ATM_BACKUP_RESTORE_HOME_BASE/alpha/Documents/file"
printf 'beta data\n' > "$ATM_BACKUP_RESTORE_HOME_BASE/beta/Documents/file"
ATM_BACKUP_RESTORE_HOME="$ATM_BACKUP_RESTORE_HOME_BASE/alpha"
device="$ATM_BACKUP_RESTORE_MEDIA_ROOT/USB Disk"
mkdir -p "$device"
python3 -B -c 'from pathlib import Path
import sys
path=str(Path(sys.argv[2])).replace(" ", r"\040")
Path(sys.argv[1]).write_text(f"42 1 8:1 / {path} rw - ext4 /dev/test rw\n")
' "$ATM_BACKUP_RESTORE_MOUNTINFO" "$device"

atm_backup_restore_select_users backup <<< $'bad\nm\n../outside\nm\nbeta' > "$temp/user-picker"
[[ "${ATM_BACKUP_RESTORE_SELECTION_ARGS[*]}" == '--user beta' ]]
atm_backup_restore_select_users backup <<< 0 > "$temp/all-picker"
[[ "${ATM_BACKUP_RESTORE_SELECTION_ARGS[*]}" == --all-users ]]
if atm_backup_restore_select_users backup <<< '' > "$temp/cancel-picker"; then exit 1; fi
[[ "${#ATM_BACKUP_RESTORE_SELECTION_ARGS[@]}" == 0 ]]
atm_backup_restore_select_destination <<< '' > "$temp/default-picker"
[[ "${ATM_BACKUP_RESTORE_DESTINATION_ARGS[1]}" == "$ATM_APPS_DIR/Backups" ]]
atm_backup_restore_select_destination <<< 1 > "$temp/device-picker"
[[ "${ATM_BACKUP_RESTORE_DESTINATION_ARGS[1]}" == "$device" && "${ATM_BACKUP_RESTORE_DESTINATION_ARGS[3]}" == 42 ]]
atm_backup_restore_select_destination <<< $'m\n/tmp/manual backup' > "$temp/manual-picker"
[[ "${ATM_BACKUP_RESTORE_DESTINATION_ARGS[1]}" == '/tmp/manual backup' ]]
printf 'PASS numbered/manual/all user selection, safe defaults/cancellation, and destination paths with spaces\n'

before="$(find "$temp" -printf '%P %s %T@\n' | sort)"
output="$(ATM_DRY_RUN=1 atm_backup_restore_menu <<< $'1\n0\n3\n0\nxq')"
[[ "$output" == *'🧭 Main menu'* && "$output" == *'👤 User selection'* ]]
[[ "$output" == *'"user": "alpha"'* && "$output" == *'"user": "beta"'* ]]
[[ "$output" == *'USB Disk'* && "$output" == *'Version: 0.0.1'* ]]
[[ "$(find "$temp" -printf '%P %s %T@\n' | sort)" == "$before" ]]
output="$(atm_backup_restore_menu <<< $'1\nq\n0')"
[[ "$output" != *'💾 Backup destination'* ]]
printf 'PASS full main-menu backup dry-run creates nothing and user cancellation returns directly\n'

output="$(atm_backup_restore_menu <<< $'1\n2\n3\n1\ny\nxq')"
device_backup="$(atm_backup_restore_state list --destination "$device")"
[[ "$(atm_backup_restore_state backup-users --source "$device_backup" | cut -f1)" == beta ]]
cmp "$ATM_BACKUP_RESTORE_HOME_BASE/beta/Documents/file" "$device_backup/users/beta/Documents/file"
atm_backup_restore_install --action backup --all-users --destination "$temp/all-storage" <<< y > "$temp/all-created"
all_backup="$(atm_backup_restore_state list --destination "$temp/all-storage")"
atm_backup_restore_install --action verify --all-users --source "$all_backup" > "$temp/all-verified"
input="$(printf '2\n%s\n0\n%s\ny\nxq\n' "$all_backup" "$temp/all-restored")"
output="$(atm_backup_restore_menu <<< "$input")"
cmp "$ATM_BACKUP_RESTORE_HOME_BASE/alpha/Documents/file" "$temp/all-restored/alpha/Documents/file"
cmp "$ATM_BACKUP_RESTORE_HOME_BASE/beta/Documents/file" "$temp/all-restored/beta/Documents/file"
printf 'PASS selected-user backup to simulated mounted device and actual all-user backup/verify/menu restore\n'

#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-brave-bookmarks.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_HOME="$repo_root"
ATM_APPS_DIR="$task_root/apps with spaces"
ATM_MANIFEST_DIR="$task_root/manifests"
ATM_BRAVE_PROFILE_DIR="$task_root/Brave Browser"
ATM_BRAVE_BACKUP_DIR="$task_root/bookmark backups"
source lib/atm_core.sh
source lib/atm_i18n.sh
source lang/en-us.lang
source plugins/brave/plugin.conf
source plugins/brave/plugin.sh
source plugins/brave/lang/en-us.lang
clear() { :; }
atm_brave_status() { printf 'test\n'; }
atm_brave_browser_running() { [[ "${TEST_RUNNING:-0}" == 1 ]]; }
date() { printf '20261024-120000\n'; }

for profile in Default 'Profile 0' 'Profile 1' 'Profile 2' 'Profile 12' 'Profile 3extra' 'Guest Profile' 'Profile abc'; do
    mkdir -p "$ATM_BRAVE_PROFILE_DIR/$profile"
done
printf '{"profile":"Default"}\n' > "$ATM_BRAVE_PROFILE_DIR/Default/Bookmarks"
printf '{"profile":"Profile 0"}\n' > "$ATM_BRAVE_PROFILE_DIR/Profile 0/Bookmarks"
printf '{"profile":"Profile 1"}\n' > "$ATM_BRAVE_PROFILE_DIR/Profile 1/Bookmarks"
printf '{"profile":"Profile 12"}\n' > "$task_root/bookmarks-source"
ln -s "$task_root/bookmarks-source" "$ATM_BRAVE_PROFILE_DIR/Profile 12/Bookmarks"
for profile in 'Profile 3extra' 'Guest Profile' 'Profile abc'; do
    printf 'not a numbered profile\n' > "$ATM_BRAVE_PROFILE_DIR/$profile/Bookmarks"
done
printf 'not bookmarks\n' > "$ATM_BRAVE_PROFILE_DIR/Default/History"
printf 'not current bookmarks\n' > "$ATM_BRAVE_PROFILE_DIR/Default/Bookmarks.bak"
before="$(find "$ATM_BRAVE_PROFILE_DIR" -type f -print0 | sort -z | xargs -0 cksum)"

output="$(atm_brave_backup_bookmarks)"
mapfile -t archives < <(find "$ATM_BRAVE_BACKUP_DIR" -type f -name 'brave-bookmarks-*.tar.gz' | sort)
[[ "${#archives[@]}" == 1 ]]
archive="${archives[0]}"
expected="$(printf '%s\n' 'Default/Bookmarks' 'Profile 0/Bookmarks' 'Profile 1/Bookmarks' 'Profile 12/Bookmarks' | sort)"
[[ "$(tar -tzf "$archive" | sort)" == "$expected" ]]
[[ "$(stat -c %a "$archive")" == 600 ]]
[[ "$output" == *'Profiles with bookmarks: 4'* && "$output" == *'Brave bookmarks backup created:'* ]]
for profile in Default 'Profile 0' 'Profile 1' 'Profile 12'; do
    [[ "$(tar -xOf "$archive" "$profile/Bookmarks")" == "$(cat "$ATM_BRAVE_PROFILE_DIR/$profile/Bookmarks")" ]]
done
[[ "$(find "$ATM_BRAVE_PROFILE_DIR" -type f -print0 | sort -z | xargs -0 cksum)" == "$before" ]]
[[ -L "$ATM_BRAVE_PROFILE_DIR/Profile 12/Bookmarks" ]]
printf 'PASS all numbered profiles, exact archive contents, source preservation, symlink contents, spaces, and private permissions\n'

checksum="$(cksum "$archive")"
output="$(atm_brave_backup_bookmarks)"
[[ "$(find "$ATM_BRAVE_BACKUP_DIR" -name 'brave-bookmarks-*.tar.gz' | wc -l)" == 2 ]]
[[ "$(cksum "$archive")" == "$checksum" ]]
touch "$ATM_BRAVE_BACKUP_DIR/brave-profile-20000101-000000.tar.gz"
[[ "$(atm_brave_latest_backup)" == "$ATM_BRAVE_BACKUP_DIR/brave-profile-20000101-000000.tar.gz" ]]
printf 'PASS repeated same-second backups remain distinct and full-profile restore excludes bookmarks archives\n'

output="$(ATM_DRY_RUN=1 ATM_BRAVE_BACKUP_DIR="$task_root/no-write" atm_brave_backup_bookmarks)"
[[ "$output" == *'DRY-RUN: tar'* && ! -e "$task_root/no-write" ]]
[[ "$output" != *'Brave bookmarks backup created:'* ]]
output="$(ATM_DRY_RUN=1 atm_brave_menu <<< $'5\nxb')"
[[ "$output" == *'5) Backup bookmarks (all profiles)'* && "$output" == *'DRY-RUN: tar'* ]]
[[ "$output" != *'[missing:'* ]]
output="$(ATM_DRY_RUN=1 atm_brave_backup_menu <<< $'4\nxb')"
[[ "$output" == *'4) Backup bookmarks (all profiles)'* && "$output" == *'DRY-RUN: tar'* ]]
printf 'PASS no-write dry-run and routing from both Brave menus\n'

if output="$(TEST_RUNNING=1 ATM_BRAVE_BACKUP_DIR="$task_root/running" atm_brave_backup_bookmarks 2>&1)"; then exit 1; fi
[[ "$output" == *'Close Brave Browser'* && ! -e "$task_root/running" ]]
if output="$(ATM_BRAVE_PROFILE_DIR="$task_root/missing" atm_brave_backup_bookmarks 2>&1)"; then exit 1; fi
[[ "$output" == *'Brave profile directory not found'* ]]
mkdir -p "$task_root/empty/Guest Profile"
touch "$task_root/empty/Guest Profile/Bookmarks"
output="$(ATM_BRAVE_PROFILE_DIR="$task_root/empty" ATM_BRAVE_BACKUP_DIR="$task_root/empty-output" atm_brave_backup_bookmarks 2>&1)"
[[ "$output" == *'No Bookmarks files found'* && ! -e "$task_root/empty-output" ]]
printf 'PASS running-browser guard, missing profile root, and empty/nonmatching profiles create no backups\n'

(
    tar() { printf 'partial' > "$2"; return 2; }
    if output="$(atm_brave_backup_bookmarks 2>&1)"; then exit 1; fi
    [[ "$output" != *'Brave bookmarks backup created:'* ]]
)
(
    mv() { return 1; }
    if output="$(atm_brave_backup_bookmarks 2>&1)"; then exit 1; fi
    [[ "$output" != *'Brave bookmarks backup created:'* ]]
)
[[ -z "$(find "$ATM_BRAVE_BACKUP_DIR" -name '*.part' -print -quit)" ]]
[[ "$(cksum "$archive")" == "$checksum" ]]
printf 'PASS archive/write-publication failures clean temporary files and preserve older backups\n'

(
    final="$ATM_BRAVE_BACKUP_DIR/brave-bookmarks-collision.tar.gz"
    printf 'existing backup' > "$final"
    mktemp() {
        temporary="$ATM_BRAVE_BACKUP_DIR/.brave-bookmarks-collision.tar.gz.part"
        touch "$temporary"
        printf '%s\n' "$temporary"
    }
    if output="$(atm_brave_backup_bookmarks 2>&1)"; then exit 1; fi
    [[ "$(cat "$final")" == 'existing backup' ]]
    [[ -z "$(find "$ATM_BRAVE_BACKUP_DIR" -name '*.part' -print -quit)" ]]
)
printf 'PASS filename collisions cannot overwrite existing backups\n'

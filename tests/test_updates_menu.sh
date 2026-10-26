#!/usr/bin/env bash
set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp="$(mktemp -d /tmp/atm-updates-menu.XXXXXX)"
trap 'rm -rf -- "$temp"' EXIT
ATM_HOME="$repo"
ATM_LANG_DIR="$repo/lang"
ATM_PLUGIN_DIR="$temp/plugins"
ATM_STATE_DIR="$temp/state"
ATM_CACHE_DIR="$temp/cache"
ATM_APPS_DIR="$temp/apps"
source "$repo/lib/atm_core.sh"
source "$repo/lib/atm_i18n.sh"
source "$repo/lib/atm_plugin_loader.sh"
source "$repo/lib/atm_plugin_catalog.sh"
source "$repo/lib/atm_ui.sh"
source "$repo/lib/atm_updates.sh"
source "$repo/lang/en-us.lang"
ATM_UPDATES_ROWS=$'core\tcurrent\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tCore current\nplugin:alpha\tavailable\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tChanged\nplugin:beta\tblocked\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tRequires CORE API 2'
[[ "$(atm_updates_state core)" == current ]]
[[ "$(atm_updates_state plugin:alpha)" == available ]]
[[ "$(atm_updates_badge alpha)" == ' Update' ]]
[[ -z "$(atm_updates_badge absent)" ]]
[[ "$(NO_COLOR=1 atm_updates_menu_label)" == 'Updates Update [CORE: 0; Plugins: 2]' ]]
printf 'PASS independent plugin badges, CORE counter and NO_COLOR output\n'

atm_updates_refresh() { :; }
atm_updates_engine() {
    printf '%s\n' "$*" >> "$temp/actions"
    case "$1" in
        apply)
            if [[ "$2" == all-plugins ]]; then
                printf 'plugin:alpha\tupdated\t%s\nplugin:beta\tfailed\tSimulated failure\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
                return 1
            fi
            printf '%s\tupdated\t%s\n' "$2" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            ;;
        changes|backups) printf '%s detail\n' "$1" ;;
    esac
}
for reply in n '' $'\n'; do
    atm_updates_apply plugin:alpha <<< "$reply" > "$temp/cancel"
    [[ ! -e "$temp/actions" && "${ATM_UPDATES_RESTART_REQUIRED:-0}" == 0 ]]
done
printf 'PASS explicit consent, Enter/No cancellation, and no updater execution on cancel\n'

atm_updates_apply plugin:alpha <<< y > "$temp/update"
[[ "$ATM_UPDATES_RESTART_REQUIRED" == 1 ]]
grep -q 'apply plugin:alpha --revision bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' "$temp/actions"
grep -q 'Updated; backup:' "$temp/update"
printf 'PASS pinned single-plugin apply and required restart\n'

ATM_UPDATES_RESTART_REQUIRED=0
ATM_UPDATES_ROWS=$'core\tcurrent\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tCurrent\nplugin:alpha\tavailable\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tChanged'
if atm_updates_cli all <<< y > "$temp/batch"; then exit 1; fi
[[ "$ATM_UPDATES_RESTART_REQUIRED" == 1 ]]
grep -q 'alpha: Updated' "$temp/batch"
grep -q 'beta: Failed' "$temp/batch"
printf 'PASS partial batch failure retains per-plugin results and exits stale menus\n'

ATM_UPDATES_RESTART_REQUIRED=0
ATM_DRY_RUN=1 atm_plugins_cli update --all > "$temp/dry"
grep -q 'DRY-RUN:.*all-plugins' "$temp/dry"
ATM_DRY_RUN=1 atm_plugins_cli update alpha > "$temp/dry"
grep -q 'plugin:alpha' "$temp/dry"
atm_updates_menu < /dev/null > /dev/null
atm_updates_pick_plugin <<< b > /dev/null 2>&1
printf 'PASS plugin CLI aliases, no-write dry-run, menu cancellation/EOF\n'

(
    atm_menu_header() { :; }
    atm_plugin_list_sorted_ids() { printf 'alpha\n'; }
    atm_plugin_status() { printf 'tool status'; }
    atm_plugin_arrays_init
    ATM_PLUGIN_IDS=(alpha)
    ATM_PLUGIN_ICON[alpha]='*'
    ATM_PLUGIN_NAME[alpha]=Alpha
    ATM_UPDATES_ROWS=$'plugin:alpha\tavailable\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tChanged'
    output="$(atm_menu_main <<< $'u\nb\nq')"
    [[ "$output" == *'Alpha'*'tool status Update'* && "$output" == *'Update ALL installed plugins'* ]]
)
printf 'PASS actual main-menu plugin badge and u update-center routing\n'

(
    atm_updates_cli() { printf 'ROUTE:%s\n' "$*"; }
    atm_prompt_continue() { :; }
    clear() { :; }
    output="$(atm_plugins_menu <<< $'4\nb')"
    [[ "$output" == *'ROUTE:check'* && "$output" == *'6) Update ALL installed plugins'* ]]
)
printf 'PASS p Plugins exposes update check, single update and all updates\n'

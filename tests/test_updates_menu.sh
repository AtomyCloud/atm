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

(
    atm_updates_engine() {
        printf 'partial invalid output\n'
        printf 'ATM updates: fixture permission denied\n' >&2
        return 1
    }
    TMPDIR="$temp" atm_updates_refresh --force
    [[ -z "$ATM_UPDATES_ROWS" && "$ATM_UPDATES_ERROR" == *'fixture permission denied'* ]]
    [[ "$ATM_UPDATES_ERROR" == *'exit=1'* ]]
    atm_updates_show > "$temp/check-error"
    grep -q 'fixture permission denied' "$temp/check-error"
    [[ -z "$(find "$temp" -name 'atm-updates-error.*' -print)" ]]
    atm_updates_engine() { [[ "$ATM_UPDATES_CHECK_TIMEOUT" == 123 ]]; return 124; }
    ATM_UPDATES_CHECK_TIMEOUT=123 atm_updates_refresh --force
    [[ "$ATM_UPDATES_ERROR" == *'timed out'*'123s'* && -z "$ATM_UPDATES_ROWS" ]]
    atm_updates_engine() { printf 'plugin:secrets\tavailable\t%s\tChanged\n' bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb; }
    atm_updates_refresh --force
    [[ -z "$ATM_UPDATES_ERROR" && "$(atm_updates_state plugin:secrets)" == available ]]
    atm_updates_engine() { [[ "$1" == cached ]]; }
    ATM_UPDATES_KEY=''
    ATM_UPDATES_AUTO_CHECK=0 atm_updates_refresh
    [[ -z "$ATM_UPDATES_ROWS" && -z "$ATM_UPDATES_ERROR" ]]
    atm_updates_show > "$temp/no-cache"
    grep -q 'No current-version claim' "$temp/no-cache"
    command() { if [[ "$*" == '-v python3' ]]; then return 1; else builtin command "$@"; fi; }
    atm_updates_refresh --force
    [[ "$ATM_UPDATES_ERROR" == *'Python 3 is required'* && -z "$ATM_UPDATES_ROWS" ]]
)
printf 'PASS refresh diagnostics, timeout override, cleanup, recovery, empty cache and missing Python\n'

(
    atm_updates_engine() {
        case "$1" in
            check) printf 'plugin:secrets\tunknown\t\tGitHub rate limit fixture\n' ;;
            apply) printf 'must not apply\n' >> "$temp/unsafe-apply"; return 1 ;;
        esac
    }
    if atm_updates_apply plugin:secrets <<< y > "$temp/unknown" 2>&1; then exit 1; fi
    grep -q 'GitHub rate limit fixture' "$temp/unknown"
    [[ ! -e "$temp/unsafe-apply" ]]
)
printf 'PASS empty revision preserves diagnostic detail and blocks apply\n'

(
    atm_updates_engine() {
        case "$1" in
            check)
                printf '%s\n' "$*" >> "$temp/secrets-checks"
                printf 'core\tcurrent\t%s\tCurrent\nplugin:secrets\tavailable\t%s\tChanged\n' \
                    bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
            apply)
                [[ "$2" == plugin:secrets && "$4" == bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ]]
                printf '%s\n' "$*" > "$temp/secrets-apply"
                printf 'plugin:secrets\tupdated\tfixture-backup\n' ;;
        esac
    }
    atm_updates_refresh --force
    atm_updates_pick_plugin <<< $'1\ny' > "$temp/secrets-menu" 2>&1
    [[ -e "$temp/secrets-apply" && "$ATM_UPDATES_RESTART_REQUIRED" == 1 ]]
    [[ "$(wc -l < "$temp/secrets-checks")" == 1 ]]
    ATM_UPDATES_AT=$((SECONDS - 61))
    atm_updates_refresh --apply
    [[ "$(wc -l < "$temp/secrets-checks")" == 2 ]]
    [[ "$(tail -n 1 "$temp/secrets-checks")" == check ]]
    atm_updates_refresh --force
    [[ "$(wc -l < "$temp/secrets-checks")" == 3 ]]
)
printf 'PASS Secrets selection reuses fresh snapshot; expired/forced checks bypass cache\n'

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
atm_updates_cli verify alpha aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa <<< n > "$temp/verify"
grep -q 'official v0.0.1 commit' "$temp/verify"
[[ ! -e "$temp/actions" ]]
printf 'PASS explicit consent, Enter/No cancellation, and no updater execution on cancel\n'

atm_updates_apply plugin:alpha <<< y > "$temp/update"
[[ "$ATM_UPDATES_RESTART_REQUIRED" == 1 ]]
grep -q 'apply plugin:alpha --revision bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' "$temp/actions"
grep -q 'Updated; backup:' "$temp/update"
printf 'PASS pinned single-plugin apply and required restart\n'

(
    ATM_UPDATES_ROWS=$'core\tavailable\taaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\tLegacy\nplugin:alpha\tavailable\tbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\tMain'
    saved="$ATM_UPDATES_ROWS"
    atm_updates_apply core <<< y > "$temp/core-channel"
    ATM_UPDATES_ROWS="$saved"
    atm_updates_apply plugin:alpha <<< y > "$temp/plugin-channel"
    ATM_UPDATES_ROWS="$saved"
    if atm_updates_apply all-plugins <<< y > "$temp/all-channel"; then exit 1; fi
    grep -q 'apply core --revision aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$temp/actions"
    grep -q 'apply plugin:alpha --revision bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' "$temp/actions"
    grep -q 'apply all-plugins --revision bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' "$temp/actions"
    grep -q 'CORE: https://github.com/AtomyCloud/atm/tree/v0.0.1' "$temp/core-channel"
    grep -q 'Plugins: https://github.com/AtomyCloud/atm/tree/v0.0.1/plugins' "$temp/core-channel"
)
printf 'PASS per-target CORE/plugin/all revision selection from pinned rows\n'

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

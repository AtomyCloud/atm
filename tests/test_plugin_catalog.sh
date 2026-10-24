#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-plugin-menu.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_HOME="$repo_root"
ATM_PLUGIN_DIR="$task_root/local plugins"
source lib/atm_core.sh
source lib/atm_i18n.sh
source lib/atm_plugin_loader.sh
source lib/atm_plugin_catalog.sh
source lib/atm_ui.sh
source lang/en-us.lang
atm_plugin_arrays_init
clear() { :; }
log="$task_root/requests"
touch "$log"
fixture_revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

# Mock only the catalog process: no network or downloaded code is executed.
python3() {
    printf '%s\n' "$*" >> "$log"
    [[ "$1" == -B && "$2" == "$ATM_HOME/lib/plugin_catalog.py" ]]
    case "$3" in
        search)
            [[ "$4" == -- ]]
            [[ "${TEST_SEARCH_FAIL:-0}" == 0 ]] || return 2
            [[ "$5" != missing ]] || return 0
            printf 'brave\tBrave Browser\t0.0.1\tOfficial Brave browser\t%s\n' "$fixture_revision"
            printf 'browser_tools\tBrowser Tools\t0.0.1\tBrave profile helpers\t%s\n' "$fixture_revision"
            ;;
        download)
            [[ "${TEST_DOWNLOAD_FAIL:-0}" == 0 ]] || return 3
            [[ "$5" == --root && "$6" == "$ATM_PLUGIN_DIR" ]]
            [[ "${7:-}" != --revision || "$8" == "$fixture_revision" ]]
            mkdir -p "$ATM_PLUGIN_DIR/$4"
            printf '%s\n' "$ATM_PLUGIN_DIR/$4"
            ;;
        *) return 4 ;;
    esac
}

output="$(atm_plugins_cli search brave)"
[[ "$output" == *'brave | Brave Browser'* && "$output" == *'browser_tools | Browser Tools'* ]]
[[ "$output" == *'https://github.com/AtomyCloud/atm/tree/main/plugins'* && ! -e "$ATM_PLUGIN_DIR" ]]
output="$(atm_plugins_cli search missing)"
[[ "$output" == *'No matching official plugins'* ]]
output="$(atm_plugins_cli search --help)"
[[ "$output" == *'Brave Browser'* ]]
if output="$(TEST_SEARCH_FAIL=1 atm_plugins_cli search brave 2>&1)"; then exit 1; fi
[[ "$output" == *'Could not read the official plugin catalog'* ]]
printf 'PASS search lists all references and handles no results/network errors without writes\n'

before="$(cksum "$log")"
output="$(atm_plugins_cli download brave <<< n 2>&1)"
[[ ! -e "$ATM_PLUGIN_DIR" && "$(cksum "$log")" == "$before" ]]
[[ "$output" == *'Plugins contain Bash code'* ]]
output="$(atm_plugins_cli download brave < /dev/null)"
[[ ! -e "$ATM_PLUGIN_DIR" ]]
output="$(ATM_DRY_RUN=1 atm_plugins_cli download brave)"
[[ "$output" == *'DRY-RUN:'* && ! -e "$ATM_PLUGIN_DIR" && "$(cksum "$log")" == "$before" ]]
if output="$(atm_plugins_cli download ../escape 2>&1)"; then exit 1; fi
if output="$(atm_plugins_cli download brave --url https://other.example 2>&1)"; then exit 1; fi
[[ ! -e "$ATM_PLUGIN_DIR" ]]
printf 'PASS explicit trust confirmation, EOF/cancellation, fixed origin, input validation, and network-free download dry-run\n'

output="$(atm_plugins_cli download brave <<< y)"
[[ -d "$ATM_PLUGIN_DIR/brave" && "$output" == *'Restart ATM to load the new plugin'* ]]
before="$(cksum "$log")"
output="$(atm_plugins_cli download brave <<< y 2>&1)"
[[ "$output" == *'will not be overwritten'* && "$(cksum "$log")" == "$before" ]]
ATM_PLUGIN_IDS+=(custom_id)
output="$(atm_plugins_cli download custom_id <<< y 2>&1)"
[[ "$output" == *'will not be overwritten'* && ! -e "$ATM_PLUGIN_DIR/custom_id" ]]
if output="$(TEST_DOWNLOAD_FAIL=1 atm_plugins_cli download absent <<< y 2>&1)"; then exit 1; fi
[[ ! -e "$ATM_PLUGIN_DIR/absent" && "$output" == *'Plugin download failed'* ]]
printf 'PASS download restart notice, local-plugin preservation, alias-ID protection, and failure reporting\n'

output="$(atm_plugins_search_menu <<< $'brave\n2\ny')"
[[ -d "$ATM_PLUGIN_DIR/browser_tools" ]]
[[ "$output" == *'1) brave'* && "$output" == *'Already local'* && "$output" == *'2) browser_tools'* ]]
grep -q "download browser_tools --root .* --revision $fixture_revision" "$log"
before="$(cksum "$log")"
output="$(atm_plugins_search_menu <<< b)"
[[ "$(cksum "$log")" == "$before" ]]
output="$(atm_plugins_search_menu <<< $'brave\n1+1' 2>&1)"
[[ "$output" == *'Invalid option'* ]]
output="$(atm_plugins_menu <<< $'2\nbrave\nb\nxb')"
[[ "$output" == *'1) List local plugins'* && "$output" == *'2) Search and download official plugins'* ]]
[[ "$output" != *'[missing:'* ]]
printf 'PASS numbered choice, pinned search commit, cancellation, non-arithmetic input, and p submenu routing\n'

(
    atm_menu_header() { :; }
    atm_plugin_status() { printf 'test\n'; }
    atm_plugin_list_sorted_ids() { :; }
    atm_plugins_menu() { printf 'PLUGIN_SUBMENU_OPENED\n'; }
    output="$(atm_menu_main <<< $'p\nq')"
    [[ "$output" == *'PLUGIN_SUBMENU_OPENED'* ]]
)
printf 'PASS actual main-menu p handler opens plugin management\n'

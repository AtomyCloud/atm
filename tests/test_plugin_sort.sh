#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-plugin-sort.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_HOME="$repo_root"
ATM_CONFIG_FILE="$task_root/atm.conf"
ATM_PLUGINS_CONFIG_FILE="$task_root/plugins.conf"
ATM_PLUGIN_DIR="$task_root/plugins"
ATM_APPS_DIR="$task_root/apps"
ATM_CACHE_DIR="$task_root/cache"
ATM_STATE_DIR="$task_root/state"
ATM_PLUGIN_SORT_ORDER=default
source lib/atm_core.sh
source lib/atm_config.sh
source lib/atm_i18n.sh
source lib/atm_plugin_loader.sh
source lib/atm_plugin_catalog.sh
source lib/atm_ui.sh
source lib/atm_path.sh
source lang/en-us.lang
clear() { :; }
atm_plugin_arrays_init
ATM_PLUGIN_IDS=(zeta alpha a_same b_same charlie)
for id in "${ATM_PLUGIN_IDS[@]}"; do ATM_PLUGIN_ICON[$id]='*'; done
ATM_PLUGIN_NAME[zeta]=Zulu
ATM_PLUGIN_NAME[alpha]=apple
ATM_PLUGIN_NAME[a_same]=Echo
ATM_PLUGIN_NAME[b_same]=echo
ATM_PLUGIN_ORDER[zeta]=1
ATM_PLUGIN_ORDER[a_same]=10
ATM_PLUGIN_ORDER[b_same]=11
ATM_PLUGIN_ORDER[alpha]=20
ATM_PLUGIN_ORDER[charlie]=30
expected_default=$'zeta\na_same\nb_same\nalpha\ncharlie'
expected_az=$'alpha\ncharlie\na_same\nb_same\nzeta'
expected_za=$'zeta\na_same\nb_same\ncharlie\nalpha'

[[ "$(atm_plugin_list_sorted_ids)" == "$expected_default" ]]
[[ "$(ATM_PLUGIN_SORT_ORDER=az atm_plugin_list_sorted_ids)" == "$expected_az" ]]
[[ "$(ATM_PLUGIN_SORT_ORDER=za atm_plugin_list_sorted_ids)" == "$expected_za" ]]
[[ "$(ATM_PLUGIN_SORT_ORDER=bad atm_plugin_list_sorted_ids)" == "$expected_default" ]]
[[ "$(ATM_PLUGIN_SORT_ORDER=za atm_plugin_list_sorted_ids default)" == "$expected_default" ]]
[[ "$(ATM_PLUGIN_SORT_ORDER=az atm_plugins_cli list | awk 'NR>1 {print $1}')" == "$expected_az" ]]
(
    ATM_PLUGIN_IDS=()
    [[ -z "$(atm_plugin_list_sorted_ids az)" && -z "$(atm_plugin_list_sorted_ids za)" ]]
)
printf 'PASS display-name sorting, mixed case, deterministic ties, fallback names, metadata default, and empty lists\n'
ATM_PLUGIN_NAME[charlie]=charlie

(
    ATM_CONFIG_FILE="$task_root/new-config"
    ATM_PLUGINS_CONFIG_FILE="$task_root/new-plugins-config"
    unset ATM_PLUGIN_SORT_ORDER
    atm_config_init
    [[ "$(atm_plugin_sort_mode)" == default ]]
)

printf 'KEEP_SETTING="unchanged"\n' > "$ATM_CONFIG_FILE"
atm_plugins_set_sort A-Z > "$task_root/result"
[[ "$ATM_PLUGIN_SORT_ORDER" == az ]]
grep -q 'Plugin order saved: A-Z' "$task_root/result"
grep -q 'KEEP_SETTING="unchanged"' "$ATM_CONFIG_FILE"
(
    unset ATM_PLUGIN_SORT_ORDER
    atm_config_init
    [[ "$ATM_PLUGIN_SORT_ORDER" == az ]]
)
(
    export ATM_PLUGIN_SORT_ORDER=za
    atm_config_init
    [[ "$ATM_PLUGIN_SORT_ORDER" == za ]]
)
before="$(cksum "$ATM_CONFIG_FILE")"
if output="$(atm_plugins_set_sort invalid 2>&1)"; then exit 1; fi
[[ "$(cksum "$ATM_CONFIG_FILE")" == "$before" ]]
ATM_DRY_RUN=1 atm_plugins_set_sort za > "$task_root/plan"
[[ "$ATM_PLUGIN_SORT_ORDER" == az && "$(cksum "$ATM_CONFIG_FILE")" == "$before" ]]
grep -q 'DRY-RUN: set ATM_PLUGIN_SORT_ORDER=za' "$task_root/plan"
output="$(atm_plugins_cli sort)"
[[ "$output" == *'Current plugin order: A-Z'* ]]
printf 'PASS persistence, config reload, environment override, unrelated settings, validation, and no-write dry-run\n'

(
    atm_config_set_key() { return 1; }
    if output="$(atm_plugins_set_sort za 2>&1)"; then exit 1; fi
    [[ "$output" == *'Could not save the plugin order'* && "$ATM_PLUGIN_SORT_ORDER" == az ]]
    atm_config_set_key() { :; }
    if output="$(atm_plugins_set_sort za 2>&1)"; then exit 1; fi
    [[ "$output" == *'Could not save the plugin order'* && "$output" != *'Plugin order saved:'* ]]
    [[ "$ATM_PLUGIN_SORT_ORDER" == az ]]
)
printf 'PASS failed or ineffective configuration writes never claim success or change active order\n'

(
    atm_menu_header() { :; }
    atm_plugin_status() { printf 'test\n'; }
    atm_plugin_run_menu() { printf 'SELECTED:%s\n' "$1"; }
    atm_prompt_continue() { :; }
    output="$(ATM_PLUGIN_SORT_ORDER=az atm_menu_main <<< $'1\nq')"
    [[ "$output" == *'SELECTED:alpha'* ]]
    output="$(ATM_PLUGIN_SORT_ORDER=za atm_menu_main <<< $'1\nq')"
    [[ "$output" == *'SELECTED:zeta'* ]]
    atm_plugin_run_install() { printf 'INSTALL:%s\n' "$1"; }
    output="$(ATM_PLUGIN_SORT_ORDER=az atm_install_menu <<< 1)"
    [[ "$output" == *'INSTALL:alpha'* ]]
)
printf 'PASS main/install menu numbers resolve to the displayed sorted plugin\n'

(
    python3() {
        printf 'z_id\tAlpha browser\t0.0.1\tTest\t%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        printf 'a_id\tZulu browser\t0.0.1\tTest\t%s\n' bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    }
    [[ "$(ATM_PLUGIN_SORT_ORDER=az atm_plugins_catalog_rows | cut -f1)" == $'z_id\na_id' ]]
    [[ "$(ATM_PLUGIN_SORT_ORDER=za atm_plugins_catalog_rows | cut -f1)" == $'a_id\nz_id' ]]
    atm_plugins_remote_download() { printf 'DOWNLOAD:%s:%s\n' "$1" "$2"; }
    output="$(ATM_PLUGIN_SORT_ORDER=za atm_plugins_search_menu <<< $'browser\n1')"
    [[ "$output" == *'1) a_id | Zulu browser'* && "$output" == *'DOWNLOAD:a_id:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'* ]]
)
printf 'PASS official catalog sorting preserves displayed choices and pinned revisions\n'

(
    for id in "${ATM_PLUGIN_IDS[@]}"; do ATM_PLUGIN_FULL_SETUP[$id]=1; done
    atm_info() { :; }
    atm_plugin_run_install() { printf '%s\n' "$1"; }
    atm_plugin_run_desktop() { printf '%s\n' "$1"; }
    atm_test_sorted_path() { printf '%s\n' "$id"; }
    for id in "${ATM_PLUGIN_IDS[@]}"; do ATM_PLUGIN_PATH_FUNC[$id]=atm_test_sorted_path; done
    [[ "$(ATM_PLUGIN_SORT_ORDER=za atm_full_setup)" == "$expected_default" ]]
    [[ "$(ATM_PLUGIN_SORT_ORDER=za atm_plugin_run_all_desktop)" == "$expected_default" ]]
    [[ "$(ATM_PLUGIN_SORT_ORDER=za atm_path_collect_entries | tail -n +3)" == "$expected_default" ]]
)
printf 'PASS Full Setup, PATH precedence, and desktop processing retain operational metadata order\n'

atm_plugins_menu <<< $'3\n2\nxb' > "$task_root/menu"
[[ "$ATM_PLUGIN_SORT_ORDER" == za ]]
grep -q '3) Sort plugins' "$task_root/menu"
grep -q 'Plugin order saved: Z-A' "$task_root/menu"
if grep -q '\[missing:' "$task_root/menu"; then exit 1; fi
before="$(cksum "$ATM_CONFIG_FILE")"
atm_plugins_sort_menu <<< b > /dev/null
atm_plugins_sort_menu < /dev/null > /dev/null
atm_plugins_sort_menu <<< bad > /dev/null 2>&1
[[ "$(cksum "$ATM_CONFIG_FILE")" == "$before" ]]
atm_plugins_cli sort default > /dev/null
[[ "$ATM_PLUGIN_SORT_ORDER" == default ]]
printf 'PASS p menu preference changes, cancellation/EOF, and return to original ordering\n'

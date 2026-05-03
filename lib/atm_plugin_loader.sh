#!/usr/bin/env bash

atm_plugin_arrays_init() {
    unset ATM_PLUGIN_IDS
    unset ATM_PLUGIN_NAME
    unset ATM_PLUGIN_ICON
    unset ATM_PLUGIN_VERSION
    unset ATM_PLUGIN_ORDER
    unset ATM_PLUGIN_DESCRIPTION
    unset ATM_PLUGIN_FULL_SETUP
    unset ATM_PLUGIN_DEPENDS
    unset ATM_PLUGIN_OPTIONAL_DEPENDS
    unset ATM_PLUGIN_MENU_FUNC
    unset ATM_PLUGIN_STATUS_FUNC
    unset ATM_PLUGIN_INSTALL_FUNC
    unset ATM_PLUGIN_PATH_FUNC
    unset ATM_PLUGIN_DESKTOP_FUNC
    unset ATM_PLUGIN_REMOVE_FUNC
    unset ATM_PLUGIN_UNINSTALL_FUNC
    unset ATM_PLUGIN_USE_FUNC
    unset ATM_PLUGIN_DIR_BY_ID

    declare -ga ATM_PLUGIN_IDS=()

    declare -gA ATM_PLUGIN_NAME=()
    declare -gA ATM_PLUGIN_ICON=()
    declare -gA ATM_PLUGIN_VERSION=()
    declare -gA ATM_PLUGIN_ORDER=()
    declare -gA ATM_PLUGIN_DESCRIPTION=()
    declare -gA ATM_PLUGIN_FULL_SETUP=()
    declare -gA ATM_PLUGIN_DEPENDS=()
    declare -gA ATM_PLUGIN_OPTIONAL_DEPENDS=()
    declare -gA ATM_PLUGIN_MENU_FUNC=()
    declare -gA ATM_PLUGIN_STATUS_FUNC=()
    declare -gA ATM_PLUGIN_INSTALL_FUNC=()
    declare -gA ATM_PLUGIN_PATH_FUNC=()
    declare -gA ATM_PLUGIN_DESKTOP_FUNC=()
    declare -gA ATM_PLUGIN_REMOVE_FUNC=()
    declare -gA ATM_PLUGIN_UNINSTALL_FUNC=()
    declare -gA ATM_PLUGIN_USE_FUNC=()
    declare -gA ATM_PLUGIN_DIR_BY_ID=()
}

atm_plugin_upper_id() {
    printf '%s\n' "$1" | tr '[:lower:]-' '[:upper:]_'
}

atm_plugin_enabled() {
    local id="$1"
    local upper=""
    local var=""

    upper="$(atm_plugin_upper_id "$id")"
    var="ATM_PLUGIN_${upper}_ENABLED"

    [[ "${!var:-1}" == "1" ]]
}

atm_plugin_load_metadata() {
    local plugin_dir="$1"
    local metadata_file="$plugin_dir/plugin.metadata"
    local plugin_conf=""
    local plugin_sh=""
    local plugin_id=""

    [[ -f "$metadata_file" ]] || return 1

    ATM_PLUGIN_ID=""
    ATM_PLUGIN_NAME_VALUE=""
    ATM_PLUGIN_ICON_VALUE=""
    ATM_PLUGIN_VERSION_VALUE=""
    ATM_PLUGIN_ORDER_VALUE=""
    ATM_PLUGIN_DESCRIPTION_VALUE=""
    ATM_PLUGIN_ENTRYPOINT=""
    ATM_PLUGIN_FULL_SETUP_VALUE=""
    ATM_PLUGIN_DEPENDS_VALUE=""
    ATM_PLUGIN_OPTIONAL_DEPENDS_VALUE=""
    ATM_PLUGIN_MENU_FUNC_VALUE=""
    ATM_PLUGIN_STATUS_FUNC_VALUE=""
    ATM_PLUGIN_INSTALL_FUNC_VALUE=""
    ATM_PLUGIN_PATH_FUNC_VALUE=""
    ATM_PLUGIN_DESKTOP_FUNC_VALUE=""
    ATM_PLUGIN_REMOVE_FUNC_VALUE=""
    ATM_PLUGIN_UNINSTALL_FUNC_VALUE=""
    ATM_PLUGIN_USE_FUNC_VALUE=""

    # shellcheck source=/dev/null
    source "$metadata_file"

    [[ -n "${ATM_PLUGIN_ID:-}" ]] || atm_fail "Plugin metadata missing ATM_PLUGIN_ID: $metadata_file"

    plugin_id="$ATM_PLUGIN_ID"

    if ! atm_plugin_enabled "$plugin_id"; then
        atm_debug "Plugin disabled: $plugin_id"
        return 0
    fi

    plugin_conf="$plugin_dir/plugin.conf"
    plugin_sh="$plugin_dir/${ATM_PLUGIN_ENTRYPOINT:-plugin.sh}"

    # shellcheck source=/dev/null
    [[ -f "$plugin_conf" ]] && source "$plugin_conf"

    [[ -f "$plugin_sh" ]] || atm_fail "Plugin entrypoint not found: $plugin_sh"

    # shellcheck source=/dev/null
    source "$plugin_sh"

    atm_i18n_load_plugin_lang "$plugin_id"

    if [[ -n "${ATM_PLUGIN_DIR_BY_ID[$plugin_id]:-}" ]]; then
        atm_fail "Duplicate plugin ID detected: $plugin_id
    First: ${ATM_PLUGIN_DIR_BY_ID[$plugin_id]}
    Second: $plugin_dir

    Each plugin must have a unique ATM_PLUGIN_ID in plugin.metadata."
    fi

    ATM_PLUGIN_IDS+=("$plugin_id")

    ATM_PLUGIN_NAME["$plugin_id"]="${ATM_PLUGIN_NAME_VALUE:-$plugin_id}"
    ATM_PLUGIN_ICON["$plugin_id"]="${ATM_PLUGIN_ICON_VALUE:-🔌}"
    ATM_PLUGIN_VERSION["$plugin_id"]="${ATM_PLUGIN_VERSION_VALUE:-0.0.0}"
    ATM_PLUGIN_ORDER["$plugin_id"]="${ATM_PLUGIN_ORDER_VALUE:-999}"
    ATM_PLUGIN_DESCRIPTION["$plugin_id"]="${ATM_PLUGIN_DESCRIPTION_VALUE:-}"
    ATM_PLUGIN_FULL_SETUP["$plugin_id"]="${ATM_PLUGIN_FULL_SETUP_VALUE:-0}"
    ATM_PLUGIN_DEPENDS["$plugin_id"]="${ATM_PLUGIN_DEPENDS_VALUE:-}"
    ATM_PLUGIN_OPTIONAL_DEPENDS["$plugin_id"]="${ATM_PLUGIN_OPTIONAL_DEPENDS_VALUE:-}"
    ATM_PLUGIN_MENU_FUNC["$plugin_id"]="${ATM_PLUGIN_MENU_FUNC_VALUE:-}"
    ATM_PLUGIN_STATUS_FUNC["$plugin_id"]="${ATM_PLUGIN_STATUS_FUNC_VALUE:-}"
    ATM_PLUGIN_INSTALL_FUNC["$plugin_id"]="${ATM_PLUGIN_INSTALL_FUNC_VALUE:-}"
    ATM_PLUGIN_PATH_FUNC["$plugin_id"]="${ATM_PLUGIN_PATH_FUNC_VALUE:-}"
    ATM_PLUGIN_DESKTOP_FUNC["$plugin_id"]="${ATM_PLUGIN_DESKTOP_FUNC_VALUE:-}"
    ATM_PLUGIN_REMOVE_FUNC["$plugin_id"]="${ATM_PLUGIN_REMOVE_FUNC_VALUE:-}"
    ATM_PLUGIN_UNINSTALL_FUNC["$plugin_id"]="${ATM_PLUGIN_UNINSTALL_FUNC_VALUE:-}"
    ATM_PLUGIN_USE_FUNC["$plugin_id"]="${ATM_PLUGIN_USE_FUNC_VALUE:-}"
    ATM_PLUGIN_DIR_BY_ID["$plugin_id"]="$plugin_dir"

    atm_debug "Loaded plugin: $plugin_id"
}

atm_plugin_load_all() {
    local plugin_dir=""

    atm_plugin_arrays_init

    for plugin_dir in "$ATM_PLUGIN_DIR"/*; do
        [[ -d "$plugin_dir" ]] || continue
        atm_plugin_load_metadata "$plugin_dir"
    done
}

atm_plugin_list_sorted_ids() {
    local id=""

    for id in "${ATM_PLUGIN_IDS[@]}"; do
        printf '%s\t%s\n' "${ATM_PLUGIN_ORDER["$id"]:-999}" "$id"
    done | sort -n | awk -F'\t' '{ print $2 }'
}

atm_plugin_status() {
    local id="$1"
    local func=""

    func="${ATM_PLUGIN_STATUS_FUNC["$id"]:-}"

    if [[ -n "$func" ]] && declare -F "$func" >/dev/null 2>&1; then
        "$func"
    else
        printf '%s\n' "$(atm_t ATM_MSG_STATUS_UNKNOWN)"
    fi
}

atm_plugin_run_menu() {
    local id="$1"
    local func=""

    func="${ATM_PLUGIN_MENU_FUNC["$id"]:-}"

    [[ -n "$func" ]] || atm_fail "Plugin has no menu function: $id"
    declare -F "$func" >/dev/null 2>&1 || atm_fail "Plugin menu function not found: $func"

    "$func"
}

atm_plugin_run_install() {
    local id="$1"
    shift || true

    local func=""

    func="${ATM_PLUGIN_INSTALL_FUNC["$id"]:-}"

    [[ -n "$func" ]] || atm_fail "Plugin has no install function: $id"
    declare -F "$func" >/dev/null 2>&1 || atm_fail "Plugin install function not found: $func"

    "$func" "$@"
}

atm_plugin_run_desktop() {
    local id="$1"
    local func=""

    func="${ATM_PLUGIN_DESKTOP_FUNC["$id"]:-}"

    [[ -n "$func" ]] || return 0

    if declare -F "$func" >/dev/null 2>&1; then
        "$func"
    else
        atm_warn "Plugin desktop function not found for $id: $func"
    fi
}

atm_plugin_run_all_desktop() {
    local id=""

    for id in $(atm_plugin_list_sorted_ids); do
        atm_plugin_run_desktop "$id"
    done
}

atm_plugin_exists() {
    local id="$1"
    local item=""

    for item in "${ATM_PLUGIN_IDS[@]}"; do
        [[ "$item" == "$id" ]] && return 0
    done

    return 1
}

atm_plugins_cli() {
    local subcmd="${1:-list}"
    local id=""

    case "$subcmd" in
        list)
            printf '%-18s %-8s %-8s %s\n' "ID" "VERSION" "ORDER" "NAME"

            for id in $(atm_plugin_list_sorted_ids); do
                printf '%-18s %-8s %-8s %s %s\n' \
                    "$id" \
                    "${ATM_PLUGIN_VERSION["$id"]:-0.0.0}" \
                    "${ATM_PLUGIN_ORDER["$id"]:-999}" \
                    "${ATM_PLUGIN_ICON["$id"]:-🔌}" \
                    "${ATM_PLUGIN_NAME["$id"]:-$id}"
            done
            ;;
        *)
            atm_fail "Unknown plugins command: $subcmd"
            ;;
    esac
}
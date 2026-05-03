#!/usr/bin/env bash

atm_os_pretty() {
    if [[ -f /etc/os-release ]]; then
        # shellcheck source=/dev/null
        source /etc/os-release
        printf '%s\n' "${PRETTY_NAME:-unknown}"
    else
        uname -a
    fi
}

atm_doctor_run() {
    local id=""

    printf '%s\n' "ATM Doctor"
    printf '%s\n' "------------------------------------------"
    printf 'ATM_VERSION=%s\n' "$ATM_VERSION"
    printf 'ATM_HOME=%s\n' "$ATM_HOME"
    printf 'ATM_APPS_DIR=%s\n' "$ATM_APPS_DIR"
    printf 'ATM_CACHE_DIR=%s\n' "$ATM_CACHE_DIR"
    printf 'ATM_STATE_DIR=%s\n' "$ATM_STATE_DIR"
    printf 'ATM_CONFIG_FILE=%s\n' "$ATM_CONFIG_FILE"
    printf 'ATM_PLUGINS_CONFIG_FILE=%s\n' "$ATM_PLUGINS_CONFIG_FILE"
    printf 'ATM_LANG=%s\n' "$ATM_LANG"
    printf 'ATM_DRY_RUN=%s\n' "$ATM_DRY_RUN"
    printf 'OS=%s\n' "$(atm_os_pretty)"
    printf '%s\n' "------------------------------------------"
    printf 'Plugins:\n'

    for id in $(atm_plugin_list_sorted_ids); do
        printf '  - %s %s: %s\n' "${ATM_PLUGIN_ICON[$id]}" "$id" "$(atm_plugin_status "$id")"
    done
}
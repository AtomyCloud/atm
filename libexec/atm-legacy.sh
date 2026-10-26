#!/usr/bin/env bash
set -Eeuo pipefail

action="${1:-}"
plugin_id="${2:-}"
[[ "$plugin_id" =~ ^[a-z][a-z0-9_]{0,63}$ ]] || { printf 'Invalid plugin ID\n' >&2; exit 2; }
case "$action" in menu|status|install|remove|uninstall|use) ;; *) exit 2 ;; esac
shift 2
[[ "${ATM_DRY_RUN:-0}" != 1 ]] || { printf 'DRY-RUN: legacy %s %s\n' "$action" "$plugin_id"; exit 0; }
[[ -n "${ATM_HOME:-}" && -f "$ATM_HOME/core-api.json" ]] || exit 2
[[ ! -e "$ATM_HOME/.atm-core-update-pending" && ! -L "$ATM_HOME/.atm-core-update-pending" ]] || {
    printf 'Legacy CORE recovery pending; execution blocked.\n' >&2
    exit 1
}

# Only explicit user actions enter the trusted legacy execution environment.
source "$ATM_HOME/lib/atm_core.sh"
atm_core_source_libs
atm_core_prepare_dirs
atm_logs_init
atm_config_init
atm_i18n_init
atm_plugin_load_all
atm_plugin_exists "$plugin_id" || atm_fail "Plugin missing or disabled: $plugin_id"

case "$action" in
    menu) atm_plugin_run_menu "$plugin_id" ;;
    status) atm_plugin_status "$plugin_id" ;;
    *) ATM_ARGS=("$action" "$plugin_id" "$@"); atm_core_dispatch ;;
esac

#!/usr/bin/env bash

atm_remove_cli() {
    local plugin="${1:-}"
    local version="${2:-}"

    [[ -n "$plugin" && -n "$version" ]] || atm_fail "Usage: atm remove <plugin> <version>"
    atm_plugin_exists "$plugin" || atm_fail "Plugin not found: $plugin"

    local func="${ATM_PLUGIN_REMOVE_FUNC[$plugin]:-}"

    if [[ -n "$func" ]] && declare -F "$func" >/dev/null 2>&1; then
        "$func" "$version" "${@:3}"
    else
        atm_warn "Remove is not implemented for plugin: $plugin"
    fi
}

atm_uninstall_cli() {
    local target="${1:-}"

    [[ -n "$target" ]] || atm_fail "Usage: atm uninstall <plugin|atm>"

    if [[ "$target" == "atm" ]]; then
        atm_uninstall_atm "${@:2}"
        return 0
    fi

    atm_plugin_exists "$target" || atm_fail "Plugin not found: $target"

    local func="${ATM_PLUGIN_UNINSTALL_FUNC[$target]:-}"

    if [[ -n "$func" ]] && declare -F "$func" >/dev/null 2>&1; then
        "$func" "${@:2}"
    else
        atm_warn "Uninstall is not implemented for plugin: $target"
    fi
}

atm_uninstall_atm() {
    local answer=""

    printf 'This will remove ATM command links, not installed tools.\n'
    printf 'ATM_HOME: %s\n' "$ATM_HOME"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES) ;;
        *)
            atm_warn "Cancelled."
            return 0
            ;;
    esac

    if [[ -L "$HOME/.local/bin/atm" ]]; then
        atm_run rm -f "$HOME/.local/bin/atm"
    fi

    if [[ -L "/usr/local/bin/atm" ]]; then
        if [[ -w "/usr/local/bin" ]]; then
            atm_run rm -f "/usr/local/bin/atm"
        else
            atm_run sudo rm -f "/usr/local/bin/atm"
        fi
    fi

    atm_success "ATM command links removed."
}

atm_use_cli() {
    local plugin="${1:-}"
    local version="${2:-}"

    [[ -n "$plugin" && -n "$version" ]] || atm_fail "Usage: atm use <plugin> <version>"
    atm_plugin_exists "$plugin" || atm_fail "Plugin not found: $plugin"

    local func="${ATM_PLUGIN_USE_FUNC[$plugin]:-}"

    if [[ -n "$func" ]] && declare -F "$func" >/dev/null 2>&1; then
        "$func" "$version" "${@:3}"
    else
        atm_warn "Use/switch version is not implemented for plugin: $plugin"
    fi
}
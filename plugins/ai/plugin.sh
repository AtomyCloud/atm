#!/usr/bin/env bash

atm_ai_manifest_file() {
    printf '%s\n' "${ATM_AI_MANIFEST_FILE:-$ATM_MANIFEST_DIR/ai.manifest}"
}

atm_ai_hermes_installed() {
    command -v hermes-agent >/dev/null 2>&1
}

atm_ai_status() {
    if atm_ai_hermes_installed; then
        printf '✅ '
        hermes-agent --version 2>/dev/null || printf '%s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_INSTALLED)"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_AI_STATUS_NOT_INSTALLED)"
    fi
}

atm_ai_write_manifest() {
    local hermes_installed="0"
    local hermes_version=""

    hermes_version="$(hermes-agent --version 2>/dev/null || true)"

    if atm_ai_hermes_installed; then
        hermes_installed="1"
    fi

    atm_manifest_write "ai" \
        "ATM_PLUGIN_NAME=AI Tools" \
        "ATM_PLUGIN_VERSION=0.0.1" \
        "ATM_INSTALLED=1" \
        "ATM_AI_HERMES_INSTALLED=$hermes_installed" \
        "ATM_AI_HERMES_VERSION=$hermes_version"
}

atm_ai_install_hermes() {
    local install_url="${ATM_AI_HERMES_INSTALL_URL:-https://hermes-agent.nousresearch.com/install.sh}"

    printf '%s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_INSTALLING)"

    if atm_ai_hermes_installed; then
        atm_warn "$(atm_t ATM_PLUGIN_AI_HERMES_ALREADY_INSTALLED)"
        return 0
    fi

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands curl bash
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: curl -fsSL %q | bash\n' "$install_url"
    else
        curl -fsSL "$install_url" | bash
    fi

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && ! atm_ai_hermes_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_AI_HERMES_INSTALL_FAILED)"
    fi

    atm_ai_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_AI_HERMES_INSTALLED)"
}

atm_ai_install() {
    atm_ai_install_hermes "$@"
}

atm_ai_use() {
    atm_warn "$(atm_t ATM_PLUGIN_AI_USE_NOT_SUPPORTED)"
}

atm_ai_remove() {
    atm_warn "$(atm_t ATM_PLUGIN_AI_REMOVE_NOT_IMPLEMENTED)"
}

atm_ai_uninstall() {
    atm_warn "$(atm_t ATM_PLUGIN_AI_UNINSTALL_NOT_IMPLEMENTED)"
}

atm_ai_menu() {
    local choice=""
    local current=""

    while true; do
        clear
        current="$(atm_ai_status)"

        printf '%s\n' "=========================================="
        printf '    🧠 %s\n' "$(atm_t ATM_PLUGIN_AI_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_AI_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_AI_INSTALL_HERMES)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_ai_install_hermes
                ;;
            b|B)
                return 0
                ;;
            q|Q)
                exit 0
                ;;
            *)
                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

atm_ai_path_entries() {
    return 0
}

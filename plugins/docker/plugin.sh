#!/usr/bin/env bash

atm_docker_manifest_file() {
    printf '%s\n' "${ATM_DOCKER_MANIFEST_FILE:-$ATM_MANIFEST_DIR/docker.manifest}"
}

atm_docker_target_user() {
    if [[ -n "${ATM_DOCKER_TARGET_USER:-}" ]]; then
        printf '%s\n' "$ATM_DOCKER_TARGET_USER"
    elif [[ -n "${SUDO_USER:-}" && "${SUDO_USER:-}" != "root" ]]; then
        printf '%s\n' "$SUDO_USER"
    elif [[ -n "${USER:-}" && "${USER:-}" != "root" ]]; then
        printf '%s\n' "$USER"
    else
        id -un
    fi
}

atm_docker_run_elevated() {
    local command_text="$1"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: %s\n' "$command_text"
        return 0
    fi

    if [[ "${EUID:-$(id -u)}" == "0" ]]; then
        bash -c "$command_text"
        return 0
    fi

    command -v sudo >/dev/null 2>&1 || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_SUDO_NOT_FOUND)"
    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_SUDO_REQUIRED)"
    sudo --validate || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_SUDO_FAILED)"
    sudo bash -c "$command_text"
}

atm_docker_engine_installed() {
    command -v docker >/dev/null 2>&1
}

atm_docker_status() {
    if atm_docker_engine_installed; then
        printf '✅ '
        docker --version 2>/dev/null || printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_ENGINE_INSTALLED)"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_STATUS_NOT_INSTALLED)"
    fi
}

atm_docker_write_manifest() {
    local target_user=""
    local docker_version=""

    target_user="$(atm_docker_target_user)"
    docker_version="$(docker --version 2>/dev/null || true)"

    atm_manifest_write "docker" \
        "ATM_PLUGIN_NAME=\"Docker\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_DOCKER_ENGINE_INSTALLED=\"1\"" \
        "ATM_DOCKER_VERSION=\"$docker_version\"" \
        "ATM_DOCKER_TARGET_USER=\"$target_user\"" \
        "ATM_DOCKER_CHANNEL=\"${ATM_DOCKER_CHANNEL:-stable}\""
}

atm_docker_install_engine() {
    local script_url="${ATM_DOCKER_INSTALL_SCRIPT_URL:-https://get.docker.com/}"
    local channel="${ATM_DOCKER_CHANNEL:-stable}"
    local target_user=""
    local service_name="${ATM_DOCKER_SERVICE_NAME:-docker}"
    local group_name="${ATM_DOCKER_GROUP:-docker}"

    target_user="$(atm_docker_target_user)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_ENGINE_INSTALLING)"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands curl
    fi

    atm_docker_run_elevated "curl -fsSL '$script_url' | CHANNEL='$channel' sh"
    atm_docker_run_elevated "usermod -aG '$group_name' '$target_user'"

    if command -v systemctl >/dev/null 2>&1 || [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        atm_docker_run_elevated "systemctl enable --now '$service_name'"
    else
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_SYSTEMCTL_NOT_FOUND)"
    fi

    atm_docker_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_DOCKER_ENGINE_INSTALLED)"
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_GROUP_RELOGIN)"
}

atm_docker_install() {
    atm_docker_install_engine "$@"
}

atm_docker_use() {
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_USE_NOT_SUPPORTED)"
}

atm_docker_remove() {
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_REMOVE_NOT_IMPLEMENTED)"
}

atm_docker_uninstall() {
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_UNINSTALL_NOT_IMPLEMENTED)"
}

atm_docker_not_implemented() {
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_NOT_IMPLEMENTED)"
}

atm_docker_menu() {
    local choice=""
    local current=""

    while true; do
        clear
        current="$(atm_docker_status)"

        printf '%s\n' "=========================================="
        printf '    🐳 %s\n' "$(atm_t ATM_PLUGIN_DOCKER_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_DOCKER_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_ENGINE)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_COMPOSE)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_DESKTOP)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_ALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_docker_install_engine
                ;;
            2|3|4)
                atm_docker_not_implemented
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

atm_docker_path_entries() {
    return 0
}

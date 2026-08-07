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

atm_docker_compose_bin() {
    printf '%s\n' "${ATM_DOCKER_COMPOSE_BIN:-/usr/local/bin/docker-compose}"
}

atm_docker_compose_installed() {
    command -v docker-compose >/dev/null 2>&1 || [[ -x "$(atm_docker_compose_bin)" ]]
}

atm_docker_compose_url() {
    local os_name=""
    local arch_name=""
    local base_url="${ATM_DOCKER_COMPOSE_RELEASE_BASE_URL:-https://github.com/docker/compose/releases/latest/download}"

    os_name="$(uname -s)"
    arch_name="$(uname -m)"

    printf '%s/docker-compose-%s-%s\n' "$base_url" "$os_name" "$arch_name"
}

atm_docker_desktop_deb_file() {
    printf '%s\n' "${ATM_DOCKER_DESKTOP_DEB_FILE:-$ATM_DOWNLOAD_DIR/docker-desktop-amd64.deb}"
}

atm_docker_desktop_installed() {
    command -v docker-desktop >/dev/null 2>&1 || [[ -d "${ATM_DOCKER_DESKTOP_INSTALL_DIR:-/opt/docker-desktop}" ]]
}

atm_docker_require_desktop_platform() {
    local arch_name=""

    arch_name="$(uname -m)"

    case "$arch_name" in
        x86_64|amd64)
            ;;
        *)
            atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNSUPPORTED_ARCH)"
            ;;
    esac

    if command -v apt-get >/dev/null 2>&1; then
        return 0
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        return 0
    fi

    atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_APT_REQUIRED)"
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
    local compose_version=""
    local desktop_version=""
    local engine_installed="0"
    local compose_installed="0"
    local desktop_installed="0"

    target_user="$(atm_docker_target_user)"
    docker_version="$(docker --version 2>/dev/null || true)"
    compose_version="$(docker-compose --version 2>/dev/null || true)"
    desktop_version="$(docker-desktop --version 2>/dev/null || true)"

    if atm_docker_engine_installed; then
        engine_installed="1"
    fi

    if atm_docker_compose_installed; then
        compose_installed="1"
    fi

    if atm_docker_desktop_installed; then
        desktop_installed="1"
    fi

    atm_manifest_write "docker" \
        "ATM_PLUGIN_NAME=Docker" \
        "ATM_PLUGIN_VERSION=0.0.1" \
        "ATM_INSTALLED=1" \
        "ATM_DOCKER_ENGINE_INSTALLED=$engine_installed" \
        "ATM_DOCKER_COMPOSE_INSTALLED=$compose_installed" \
        "ATM_DOCKER_DESKTOP_INSTALLED=$desktop_installed" \
        "ATM_DOCKER_VERSION=$docker_version" \
        "ATM_DOCKER_COMPOSE_VERSION=$compose_version" \
        "ATM_DOCKER_DESKTOP_VERSION=$desktop_version" \
        "ATM_DOCKER_TARGET_USER=$target_user" \
        "ATM_DOCKER_CHANNEL=${ATM_DOCKER_CHANNEL:-stable}"
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

atm_docker_install_compose() {
    local compose_bin=""
    local compose_url=""

    compose_bin="$(atm_docker_compose_bin)"
    compose_url="$(atm_docker_compose_url)"

    printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_DOCKER_COMPOSE_DOCS)" "https://docs.docker.com/compose/install/"

    if atm_docker_compose_installed; then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_COMPOSE_ALREADY_INSTALLED)"
        return 0
    fi

    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_COMPOSE_INSTALLING)"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands curl uname
    fi

    atm_docker_run_elevated "curl -L '$compose_url' -o '$compose_bin'"
    atm_docker_run_elevated "chmod +x '$compose_bin'"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && ! atm_docker_compose_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_DOCKER_COMPOSE_INSTALL_FAILED)"
    fi

    atm_docker_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_DOCKER_COMPOSE_INSTALLED)"
}

atm_docker_install_desktop() {
    local deb_url="${ATM_DOCKER_DESKTOP_DEB_URL:-https://desktop.docker.com/linux/main/amd64/docker-desktop-amd64.deb}"
    local deb_file=""

    deb_file="$(atm_docker_desktop_deb_file)"

    printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_DOCS)" "https://docs.docker.com/desktop/setup/install/linux/ubuntu/#install-docker-desktop"

    if atm_docker_desktop_installed; then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_ALREADY_INSTALLED)"
        return 0
    fi

    atm_docker_require_desktop_platform
    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALLING)"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands curl apt-get apt uname
    fi

    atm_run mkdir -p "$(dirname "$deb_file")"
    atm_run curl -L "$deb_url" -o "$deb_file"
    atm_docker_run_elevated "apt-get update"
    atm_docker_run_elevated "apt install -y '$deb_file'"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && ! atm_docker_desktop_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALL_FAILED)"
    fi

    atm_docker_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALLED)"
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_ACCEPT_TERMS)"
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
            2)
                atm_docker_install_compose
                ;;
            3)
                atm_docker_install_desktop
                ;;
            4)
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

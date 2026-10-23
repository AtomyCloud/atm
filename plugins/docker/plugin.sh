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
        return $?
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
    local version="${1:-latest}"
    printf '%s\n' "${ATM_DOCKER_DESKTOP_DEB_FILE:-$ATM_DOWNLOAD_DIR/docker-desktop-${version}-amd64.deb}"
}

atm_docker_desktop_helper() {
    printf '%s/desktop.py\n' "${BASH_SOURCE[0]%/*}"
}

atm_docker_desktop_release_notes_url() {
    printf '%s\n' "${ATM_DOCKER_DESKTOP_RELEASE_NOTES_URL:-https://docs.docker.com/desktop/release-notes/}"
}

atm_docker_desktop_releases() {
    local response=""
    local releases=""
    local version=""
    local build=""

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && command -v curl >/dev/null 2>&1 && command -v python3 >/dev/null 2>&1; then
        if response="$(curl -fsSL --connect-timeout 5 --max-time 20 "$(atm_docker_desktop_release_notes_url)" 2>/dev/null)" \
            && releases="$(python3 "$(atm_docker_desktop_helper)" releases <<< "$response")" \
            && [[ -n "$releases" ]]; then
            printf '%s\n' "$releases"
            return 0
        fi
    fi

    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_RELEASES_FALLBACK)"
    while read -r version build; do
        [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$build" =~ ^[0-9]+$ ]] || continue
        printf '%s\thttps://desktop.docker.com/linux/main/amd64/%s/docker-desktop-amd64.deb\n' "$version" "$build"
    done <<< "${ATM_DOCKER_DESKTOP_FALLBACK_RELEASES:-4.96.3 244472}"
}

atm_docker_desktop_normalize_version() {
    local version="${1#v}"
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
    printf '%s\n' "$version"
}

atm_docker_desktop_release_url() {
    local requested="$1"
    local releases="${ATM_DOCKER_DESKTOP_RELEASES:-}"
    local version=""
    local url=""

    [[ -n "$releases" ]] || releases="$(atm_docker_desktop_releases)"
    while IFS=$'\t' read -r version url; do
        if [[ "$version" == "$requested" ]]; then
            printf '%s\n' "$url"
            return 0
        fi
    done <<< "$releases"
    return 1
}

atm_docker_desktop_version() {
    dpkg-query -W -f='${Version}' docker-desktop 2>/dev/null || true
}

atm_docker_desktop_user_home() {
    local target_user=""
    local entry=""
    local user_home=""

    target_user="$(atm_docker_target_user)"
    if [[ "$target_user" == "$(id -un)" ]]; then
        user_home="$HOME"
    else
        entry="$(getent passwd "$target_user")" || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_HOME_FAILED)"
        IFS=: read -r _ _ _ _ _ user_home _ <<< "$entry"
    fi
    [[ "$user_home" == /* && "$user_home" != / ]] || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_HOME_FAILED)"
    printf '%s\n' "$user_home"
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
    local installed="0"

    target_user="$(atm_docker_target_user)"
    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        docker_version="$(docker --version 2>/dev/null || true)"
        compose_version="$(docker-compose --version 2>/dev/null || true)"
        desktop_version="$(atm_docker_desktop_version)"
    fi

    if atm_docker_engine_installed; then
        engine_installed="1"
    fi

    if atm_docker_compose_installed; then
        compose_installed="1"
    fi

    if atm_docker_desktop_installed; then
        desktop_installed="1"
    fi

    if [[ "$engine_installed$compose_installed$desktop_installed" != "000" ]]; then
        installed="1"
    fi

    atm_manifest_write "docker" \
        "ATM_PLUGIN_NAME=Docker" \
        "ATM_PLUGIN_VERSION=0.0.1" \
        "ATM_INSTALLED=$installed" \
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
    local version="${1:-}"
    local releases=""
    local deb_url=""
    local deb_file=""
    local package_version=""
    local installed_version=""
    local command_text=""

    if [[ -z "$version" ]]; then
        releases="${ATM_DOCKER_DESKTOP_RELEASES:-$(atm_docker_desktop_releases)}"
        IFS=$'\t' read -r version deb_url <<< "$releases"
    fi
    version="$(atm_docker_desktop_normalize_version "$version")" \
        || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INVALID_VERSION)"
    if [[ -z "$deb_url" ]]; then
        deb_url="$(atm_docker_desktop_release_url "$version")" \
            || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_VERSION_UNAVAILABLE) $version"; return 0; }
    fi
    deb_file="$(atm_docker_desktop_deb_file "$version")"

    printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_DOCS)" "https://docs.docker.com/desktop/setup/install/linux/ubuntu/#install-docker-desktop"

    installed_version="$(atm_docker_desktop_version)"
    if atm_docker_desktop_installed && [[ "${installed_version%%-*}" == "$version" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_ALREADY_INSTALLED)"
        return 0
    fi

    atm_docker_require_desktop_platform
    printf '%s %s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALLING)" "$version"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: download %s to %s\n' "$deb_url" "$deb_file"
    else
        atm_require_commands curl apt-get dpkg-deb uname
        atm_download_file "$deb_url" "$deb_file"
        package_version="$(dpkg-deb -f "$deb_file" Version)" \
            || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_PACKAGE_MISMATCH)"
        if [[ "$(dpkg-deb -f "$deb_file" Package)" != docker-desktop \
            || "$(dpkg-deb -f "$deb_file" Architecture)" != amd64 \
            || "${package_version%%-*}" != "$version" ]]; then
            atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_PACKAGE_MISMATCH)"
        fi
    fi

    atm_docker_run_elevated "apt-get update" || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALL_FAILED)"
    printf -v command_text 'apt-get install -y --allow-downgrades %q' "$deb_file"
    atm_docker_run_elevated "$command_text" || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALL_FAILED)"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        installed_version="$(atm_docker_desktop_version)"
        if ! atm_docker_desktop_installed || [[ "${installed_version%%-*}" != "$version" ]]; then
            atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALL_FAILED)"
        fi
    fi

    atm_docker_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INSTALLED)"
    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_ACCEPT_TERMS)"
}

atm_docker_uninstall_desktop() {
    local answer=""
    local user_home=""
    local config_file=""
    local cli_link="${ATM_DOCKER_DESKTOP_CLI_LINK:-/usr/local/bin/com.docker.cli}"
    local command_text=""

    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL_WARNING)"
    printf '%s\n' 'https://docs.docker.com/desktop/uninstall/'
    printf '%s ' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_CONFIRM)"
    read -r answer || return 0
    case "$answer" in
        y|Y|yes|YES) ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_DOCKER_CANCELLED)"; return 0 ;;
    esac

    atm_docker_require_desktop_platform
    user_home="$(atm_docker_desktop_user_home)"
    config_file="${DOCKER_CONFIG:-$user_home/.docker}/config.json"
    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands apt-get
        if [[ -f "$config_file" ]]; then
            atm_require_commands python3
            python3 "$(atm_docker_desktop_helper)" check-config "$config_file" \
                || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_CONFIG_FAILED)"
        fi
    fi

    atm_docker_run_elevated "apt-get purge -y docker-desktop" \
        || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL_FAILED)"
    atm_run rm -rf -- "$user_home/.docker/desktop" \
        || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL_FAILED)"
    printf -v command_text 'rm -f -- %q' "$cli_link"
    atm_docker_run_elevated "$command_text" \
        || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL_FAILED)"

    if [[ -f "$config_file" || "${ATM_DRY_RUN:-0}" == "1" ]]; then
        atm_run python3 "$(atm_docker_desktop_helper)" clean-config "$config_file" \
            || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_CONFIG_FAILED)"
    fi

    atm_docker_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALLED)"
}

atm_docker_desktop_menu() {
    local ATM_DOCKER_DESKTOP_RELEASES=""
    local choice=""
    local version=""
    local row=""
    local idx=1
    local custom_option=""
    local uninstall_option=""
    local versions=()

    ATM_DOCKER_DESKTOP_RELEASES="$(atm_docker_desktop_releases)"
    while IFS=$'\t' read -r version row; do
        [[ -n "$version" ]] && versions+=("$version")
        ((${#versions[@]} < 5)) || break
    done <<< "$ATM_DOCKER_DESKTOP_RELEASES"

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    %s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_MENU_TITLE)"
        printf '%s\n' "$(atm_docker_desktop_release_notes_url)"
        printf '%s\n' "=========================================="
        idx=1
        for version in "${versions[@]}"; do
            printf '%s) Docker Desktop %s\n' "$idx" "$version"
            idx=$((idx + 1))
        done
        custom_option="$idx"
        uninstall_option="$((idx + 1))"
        printf '%s) %s\n' "$custom_option" "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_CUSTOM_VERSION)"
        printf '%s) %s\n' "$uninstall_option" "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0

        case "$choice" in
            "$custom_option")
                printf '%s ' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_ENTER_VERSION)"
                read -r version || return 0
                if ! version="$(atm_docker_desktop_normalize_version "$version")"; then
                    atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_INVALID_VERSION)"
                    continue
                fi
                atm_docker_install_desktop "$version"
                ;;
            "$uninstall_option") atm_docker_uninstall_desktop ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *)
                idx=1
                version=""
                for row in "${versions[@]}"; do
                    if [[ "$choice" == "$idx" ]]; then
                        version="$row"
                        break
                    fi
                    idx=$((idx + 1))
                done
                if [[ -n "$version" ]]; then
                    atm_docker_install_desktop "$version"
                else
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                fi
                ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
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
        printf '%s\n' "$(atm_docker_desktop_release_notes_url)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_DOCKER_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_ENGINE)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_COMPOSE)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_DESKTOP)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_INSTALL_ALL)"
        printf '5) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0

        case "$choice" in
            1)
                atm_docker_install_engine
                ;;
            2)
                atm_docker_install_compose
                ;;
            3)
                atm_docker_desktop_menu
                continue
                ;;
            4)
                atm_docker_not_implemented
                ;;
            5)
                atm_docker_uninstall_desktop
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

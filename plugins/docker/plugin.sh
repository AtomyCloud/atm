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

atm_docker_run_as_user() {
    local target_user=""
    local target_uid=""

    target_user="$(atm_docker_target_user)"
    if [[ "$target_user" == "$(id -un)" ]]; then
        atm_run "$@"
        return $?
    fi

    target_uid="$(id -u "$target_user")" || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_USER_COMMAND_FAILED)"
    if [[ "${EUID:-$(id -u)}" == 0 ]]; then
        atm_run runuser -u "$target_user" -- env \
            "XDG_RUNTIME_DIR=/run/user/$target_uid" \
            "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$target_uid/bus" "$@"
    else
        atm_run sudo -H -u "$target_user" env \
            "XDG_RUNTIME_DIR=/run/user/$target_uid" \
            "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$target_uid/bus" "$@"
    fi
}

atm_docker_user_service_running() {
    local state=""
    state="$(atm_docker_run_as_user systemctl --user show -p ActiveState --value "$1" 2>/dev/null || true)"
    [[ "$state" =~ ^(active|activating|reloading|deactivating)$ ]]
}

atm_docker_system_service_running() {
    local state=""
    state="$(systemctl show -p ActiveState --value "$1" 2>/dev/null || true)"
    [[ "$state" =~ ^(active|activating|reloading|deactivating)$ ]]
}

atm_docker_desktop_backend_running() {
    local target_uid=""
    command -v pgrep >/dev/null 2>&1 || return 1
    target_uid="$(id -u "$(atm_docker_target_user)")" || return 1
    pgrep -u "$target_uid" -f '(^|/)com[.]docker[.]backend([[:space:]]|$)' >/dev/null 2>&1
}

atm_docker_stop_running() {
    local scope="${1:-all}"
    local desktop_service="${ATM_DOCKER_DESKTOP_SERVICE_NAME:-docker-desktop}"
    local engine_service="${ATM_DOCKER_SERVICE_NAME:-docker}"
    local service=""
    local command_text=""
    local active_services=()
    local engine_services=()

    if [[ "$scope" == all ]]; then
        engine_services=("$engine_service" docker.socket)
    fi

    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        if [[ "$scope" == all ]]; then
            printf 'DRY-RUN: %s\n' "$(atm_t ATM_PLUGIN_DOCKER_STOP_PLAN)"
        else
            printf 'DRY-RUN: %s\n' "$(atm_t ATM_PLUGIN_DOCKER_STOP_DESKTOP_PLAN)"
        fi
        return 0
    fi

    if command -v systemctl >/dev/null 2>&1; then
        if atm_docker_user_service_running "$desktop_service"; then
            atm_info "$(atm_t ATM_PLUGIN_DOCKER_STOPPING)"
            atm_docker_run_as_user systemctl --user stop "$desktop_service" \
                || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
        elif atm_docker_desktop_backend_running; then
            atm_info "$(atm_t ATM_PLUGIN_DOCKER_STOPPING)"
            atm_docker_run_as_user docker desktop stop --timeout 30 \
                || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
        fi

        for service in "${engine_services[@]}"; do
            if atm_docker_user_service_running "$service"; then
                atm_docker_run_as_user systemctl --user stop "$service" \
                    || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
                if atm_docker_user_service_running "$service"; then
                    atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
                fi
            fi
            if atm_docker_system_service_running "$service"; then
                active_services+=("$service")
            fi
        done
        if ((${#active_services[@]} > 0)); then
            if atm_docker_system_service_running containerd; then
                active_services+=(containerd)
            fi
            atm_info "$(atm_t ATM_PLUGIN_DOCKER_STOPPING)"
            printf -v command_text '%q ' "${active_services[@]}"
            atm_docker_run_elevated "systemctl stop $command_text" \
                || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
            for service in "${active_services[@]}"; do
                if atm_docker_system_service_running "$service"; then
                    atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
                fi
            done
        fi

        if atm_docker_user_service_running "$desktop_service"; then
            atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
        fi
    fi

    if atm_docker_desktop_backend_running \
        || { [[ "$scope" == all ]] && command -v pgrep >/dev/null 2>&1 && pgrep -x dockerd >/dev/null 2>&1; }; then
        atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
    fi
}

atm_docker_desktop_gpg_keys() {
    local listing=""
    listing="$(atm_docker_run_as_user gpg --with-colons --list-secret-keys --fingerprint)" || return 1
    awk -F: '
        $1 == "sec" { usable = ($2 !~ /^[redi]$/ && $12 ~ /E/ && $12 !~ /D/); next }
        usable && $1 == "fpr" { print $10; usable = 0 }
    ' <<< "$listing"
}

atm_docker_setup_desktop_signin() {
    local user_home=""
    local store_dir=""
    local choice=""
    local key=""
    local listing=""
    local keys=()
    local idx=1

    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_SETUP)"
    printf '%s\n' 'https://docs.docker.com/desktop/setup/install/linux/#signing-in-with-docker-desktop-for-linux'
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: %s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_PLAN)"
        return 0
    fi

    atm_docker_require_desktop_platform
    if ! command -v gpg >/dev/null 2>&1 || ! command -v pass >/dev/null 2>&1; then
        atm_info "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_DEPS)"
        atm_docker_run_elevated 'apt-get update' \
            || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_FAILED)"
        atm_docker_run_elevated 'apt-get install -y gnupg pass' \
            || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_FAILED)"
    fi
    atm_require_commands gpg pass awk
    user_home="$(atm_docker_desktop_user_home)"
    store_dir="${PASSWORD_STORE_DIR:-$user_home/.password-store}"

    if [[ -s "$store_dir/.gpg-id" ]]; then
        atm_success "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_READY)"
        return 0
    fi

    while true; do
        listing="$(atm_docker_desktop_gpg_keys)" \
            || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_FAILED)"
        keys=()
        if [[ -n "$listing" ]]; then
            mapfile -t keys <<< "$listing"
        else
            atm_warn "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_GPG_NONE)"
        fi
        printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_GPG_SELECT)"
        idx=1
        for key in "${keys[@]}"; do
            printf '%s) %s\n' "$idx" "$key"
            idx=$((idx + 1))
        done
        printf 'g) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_GPG_GENERATE)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            b|B) return 0 ;;
            g|G)
                atm_info "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_GPG_PROMPT)"
                atm_docker_run_as_user gpg --generate-key \
                    || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_FAILED)"
                continue
                ;;
        esac

        idx=1
        for key in "${keys[@]}"; do
            if [[ "$choice" == "$idx" ]]; then
                atm_docker_run_as_user pass init "$key" \
                    || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_FAILED)"
                atm_success "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_READY)"
                return 0
            fi
            idx=$((idx + 1))
        done
        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
    done
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

    atm_docker_stop_running || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
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
    atm_docker_stop_running || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
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
    atm_info "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_HINT)"
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

    atm_docker_stop_running desktop || atm_fail "$(atm_t ATM_PLUGIN_DOCKER_STOP_FAILED)"
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
    local signin_option=""
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
        signin_option="$((idx + 2))"
        printf '%s) %s\n' "$custom_option" "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_CUSTOM_VERSION)"
        printf '%s) %s\n' "$uninstall_option" "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_UNINSTALL)"
        printf '%s) %s\n' "$signin_option" "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_SETUP)"
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
            "$signin_option") atm_docker_setup_desktop_signin ;;
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

atm_docker_images_daemon() {
    command -v docker >/dev/null 2>&1 || return 1
    local daemon=""
    daemon="$(docker info --format '{{.ID}}' 2>/dev/null)" || return 1
    [[ -n "$daemon" && "$daemon" != *$'\n'* ]] || return 1
    printf '%s\n' "$daemon"
}

atm_docker_image_rows() {
    docker image ls --all --no-trunc --format '{{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}'
}

atm_docker_images_confirm() {
    local answer=""
    printf '%s ' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_CONFIRM)"
    read -r answer || return 1
    case "$answer" in y|Y|yes|YES) return 0 ;; esac
    return 1
}

atm_docker_images_same_daemon() {
    local daemon=""
    daemon="$(atm_docker_images_daemon)" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_NO_DAEMON)"; return 1; }
    if [[ "$daemon" != "$1" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_DAEMON_CHANGED)"
        return 1
    fi
}

atm_docker_image_default_archive() {
    local scope="${1:-all-images}" slug=""
    slug="$(atm_safe_slug "$scope")"
    printf '%s/docker-%s-%s-%s-%s.tar\n' "$ATM_DOCKER_IMAGE_BACKUP_DIR" "${slug:0:80}" \
        "$(date +%Y%m%d-%H%M%S)" "$RANDOM" "$RANDOM"
}

atm_docker_image_export() {
    local image="${1:-}" output="${2:-}" all="${3:-0}" daemon="" rows="" repository="" tag="" id="" size="" ref="" parent="" temporary=""
    local refs=()
    local -A seen=()
    [[ -n "$output" ]] || output="$(atm_docker_image_default_archive "${image:-all-images}")"
    if [[ "$output" != /*.tar || "$all" != 0 && "$all" != 1 \
        || "$all" == 0 && ( -z "$image" || "$image" == -* || "$image" =~ [[:space:]] ) \
        || "$all" == 1 && -n "$image" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INVALID)"; return 1
    fi
    if [[ -e "$output" || -L "$output" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_EXISTS)"; return 1
    fi
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        if [[ "$all" == 1 ]]; then
            atm_info "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_PLAN_ALL)"
            atm_run docker image save --output "$output" -- '<all-local-tags-and-untagged-IDs>'
        else
            atm_info "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_PLAN_ONE)"
            atm_run docker image save --output "$output" -- "$image"
        fi
        return 0
    fi
    daemon="$(atm_docker_images_daemon)" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_NO_DAEMON)"; return 1; }
    if [[ "$all" == 1 ]]; then
        rows="$(atm_docker_image_rows)" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_LIST_FAILED)"; return 1; }
        while IFS=$'\t' read -r repository tag id size; do
            [[ -n "$id" ]] || continue
            if [[ "$repository" != '<none>' && "$tag" != '<none>' ]]; then ref="$repository:$tag"; else ref="$id"; fi
            [[ -z "${seen[$ref]:-}" ]] || continue
            seen["$ref"]=1
            refs+=("$ref")
        done <<< "$rows"
        if ((${#refs[@]} == 0)); then atm_info "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_NONE)"; return 0; fi
    else
        if ! docker image inspect -- "$image" >/dev/null 2>&1; then
            atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INVALID)"; return 1
        fi
        refs=("$image")
    fi
    printf '%s %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_DAEMON)" "$daemon"
    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_EXPORT_WARNING)" "$output"
    printf '  %s\n' "${refs[@]}"
    if ! atm_docker_images_confirm; then atm_info "$(atm_t ATM_PLUGIN_DOCKER_CANCELLED)"; return 0; fi
    atm_docker_images_same_daemon "$daemon" || return 1
    parent="${output%/*}"
    parent="${parent:-/}"
    # Publish only a complete archive; the existing Brave backup uses this pattern.
    if ! (
        umask 077
        mkdir -p -- "$parent" || exit 1
        temporary="$(mktemp "$parent/.atm-docker-images-XXXXXX.tar.part")" || exit 1
        trap 'rm -f -- "$temporary"' EXIT
        docker image save --output "$temporary" -- "${refs[@]}" || exit 1
        [[ -f "$temporary" && ! -L "$temporary" && -s "$temporary" ]] || exit 1
        chmod 600 -- "$temporary" || exit 1
        mv -n -- "$temporary" "$output" || exit 1
        [[ ! -e "$temporary" ]] || exit 1
    ); then
        atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_FAILED)"; return 1
    fi
    atm_success "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_EXPORTED) $output"
}

atm_docker_image_archive_valid() {
    [[ "$1" == /*.tar && -f "$1" && ! -L "$1" && -r "$1" && -s "$1" ]]
}

atm_docker_image_archives() {
    local directory="$1"
    [[ -d "$directory" ]] || return 0
    find "$directory" -maxdepth 1 -type f -name '*.tar' -print0 | sort -z
}

atm_docker_images_restore() {
    local archive="" daemon=""
    (($# > 0)) || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_NO_ARCHIVES)"; return 0; }
    for archive in "$@"; do
        atm_docker_image_archive_valid "$archive" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INVALID)"; return 1; }
    done
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        for archive in "$@"; do atm_run docker image load --input "$archive"; done
        return 0
    fi
    daemon="$(atm_docker_images_daemon)" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_NO_DAEMON)"; return 1; }
    printf '%s %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_DAEMON)" "$daemon"
    printf '%s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_LOAD_WARNING)"
    printf '  %s\n' "$@"
    if ! atm_docker_images_confirm; then atm_info "$(atm_t ATM_PLUGIN_DOCKER_CANCELLED)"; return 0; fi
    for archive in "$@"; do
        atm_docker_images_same_daemon "$daemon" || return 1
        atm_docker_image_archive_valid "$archive" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INVALID)"; return 1; }
        if ! docker image load --input "$archive"; then
            atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_FAILED)"; return 1
        fi
        atm_success "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_RESTORED) $archive"
    done
}

atm_docker_images_cli() {
    local action="${1:-menu}" image="" output="" input="" directory="$ATM_DOCKER_IMAGE_BACKUP_DIR" all=0
    local archives=()
    if (($# > 0)); then shift; fi
    while (($# > 0)); do
        case "$action:$1" in
            export:--image|export:--output|export:--all|restore:--input|restore-all:--directory) ;;
            *) atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_USAGE)"; return 1 ;;
        esac
        case "$1" in
            --all) all=1; shift ;;
            --image|--output|--input|--directory)
                (($# >= 2)) || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_USAGE)"; return 1; }
                case "$1" in
                    --image) image="$2" ;; --output) output="$2" ;; --input) input="$2" ;; --directory) directory="$2" ;;
                esac
                shift 2 ;;
            *) atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_USAGE)"; return 1 ;;
        esac
    done
    case "$action" in
        menu) atm_docker_images_menu ;;
        export) atm_docker_image_export "$image" "$output" "$all" ;;
        restore) atm_docker_images_restore "$input" ;;
        restore-all)
            [[ "$directory" == /* ]] || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INVALID)"; return 1; }
            mapfile -d '' -t archives < <(atm_docker_image_archives "$directory")
            atm_docker_images_restore "${archives[@]}" ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_USAGE)"; return 1 ;;
    esac
}

atm_docker_image_export_prompt() {
    local all="${1:-0}" image="" output="" default="" rows="" repository="" tag="" id="" size="" choice="" ref="" idx=1
    local refs=()
    if [[ "$all" == 0 ]]; then
        if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
            rows="$(atm_docker_image_rows)" || { atm_warn "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_LIST_FAILED)"; return 1; }
            while IFS=$'\t' read -r repository tag id size; do
                [[ -n "$id" ]] || continue
                if [[ "$repository" != '<none>' && "$tag" != '<none>' ]]; then ref="$repository:$tag"; else ref="$id"; fi
                refs+=("$ref")
                printf '%s) %s [%s]\n' "$idx" "$ref" "$size"
                idx=$((idx + 1))
            done <<< "$rows"
            if ((${#refs[@]} == 0)); then atm_info "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_NONE)"; return 0; fi
        fi
        printf '0) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_EXPORT_ALL)"
        printf 'm) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_MANUAL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf '%s ' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_SELECT)"
        read -r choice || return 0
        case "$choice" in
            b|B|'') return 0 ;;
            0) all=1 ;;
            m|M)
                printf '%s ' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_ENTER)"
                read -r image || return 0
                [[ -n "$image" ]] || return 0 ;;
            *)
                for ((idx=0; idx<${#refs[@]}; idx++)); do
                    if [[ "$choice" == "$((idx + 1))" ]]; then image="${refs[$idx]}"; break; fi
                done
                [[ -n "$image" ]] || { atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; return 1; } ;;
        esac
    fi
    default="$(atm_docker_image_default_archive "${image:-all-images}")"
    printf '%s [%s] ' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_OUTPUT)" "$default"
    read -r output || return 0
    atm_docker_image_export "$image" "${output:-$default}" "$all"
}

atm_docker_images_restore_prompt() {
    local archive="" choice="" idx=1
    local archives=()
    mapfile -d '' -t archives < <(atm_docker_image_archives "$ATM_DOCKER_IMAGE_BACKUP_DIR")
    for archive in "${archives[@]}"; do
        printf '%s) %s\n' "$idx" "${archive##*/}"
        idx=$((idx + 1))
    done
    printf 'm) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INPUT)"
    printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
    printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
    read -r choice || return 0
    archive=""
    case "$choice" in
        b|B|'') return 0 ;;
        m|M)
            printf '%s ' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_INPUT)"
            read -r archive || return 0
            [[ -n "$archive" ]] || return 0 ;;
        *)
            for ((idx=0; idx<${#archives[@]}; idx++)); do
                if [[ "$choice" == "$((idx + 1))" ]]; then archive="${archives[$idx]}"; break; fi
            done
            [[ -n "$archive" ]] || { atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; return 1; } ;;
    esac
    atm_docker_images_restore "$archive"
}

atm_docker_images_menu() {
    local choice="" directory=""
    while true; do
        printf '\n%s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGES)"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_EXPORT_ONE)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_EXPORT_ALL)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_RESTORE)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_RESTORE_ALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_docker_image_export_prompt || true ;;
            2) atm_docker_image_export_prompt 1 || true ;;
            3) atm_docker_images_restore_prompt || true ;;
            4)
                printf '%s [%s] ' "$(atm_t ATM_PLUGIN_DOCKER_IMAGE_FOLDER)" "$ATM_DOCKER_IMAGE_BACKUP_DIR"
                read -r directory || return 0
                atm_docker_images_cli restore-all --directory "${directory:-$ATM_DOCKER_IMAGE_BACKUP_DIR}" || true ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

atm_docker_install() {
    if [[ "${1:-}" == --images ]]; then
        shift
        atm_docker_images_cli "$@"
    else
        atm_docker_install_engine "$@"
    fi
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
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_DESKTOP_SIGNIN_SETUP)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_DOCKER_IMAGES)"
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
            6)
                atm_docker_setup_desktop_signin
                ;;
            7)
                atm_docker_images_menu
                continue
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

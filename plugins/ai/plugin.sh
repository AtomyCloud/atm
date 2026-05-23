#!/usr/bin/env bash

atm_ai_manifest_file() {
    printf '%s\n' "${ATM_AI_MANIFEST_FILE:-$ATM_MANIFEST_DIR/ai.manifest}"
}

atm_ai_run_elevated() {
    local command_text="$1"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: %s\n' "$command_text"
        return 0
    fi

    if [[ "${EUID:-$(id -u)}" == "0" ]]; then
        bash -c "$command_text"
        return 0
    fi

    command -v sudo >/dev/null 2>&1 || atm_fail "$(atm_t ATM_PLUGIN_AI_SUDO_NOT_FOUND)"
    printf '%s\n' "$(atm_t ATM_PLUGIN_AI_SUDO_REQUIRED)"
    sudo --validate || atm_fail "$(atm_t ATM_PLUGIN_AI_SUDO_FAILED)"
    sudo bash -c "$command_text"
}

atm_ai_hermes_installed() {
    command -v hermes-agent >/dev/null 2>&1
}

atm_ai_hermes_desktop_system_installed() {
    command -v hermes-desktop >/dev/null 2>&1 || [[ -d /opt/HermesDesktop || -d /opt/hermes-desktop ]]
}

atm_ai_hermes_desktop_portable_dir() {
    printf '%s\n' "${ATM_AI_HERMES_DESKTOP_PORTABLE_DIR:-$ATM_APPS_DIR/hermes/hermes-desktop}"
}

atm_ai_hermes_desktop_portable_installed() {
    [[ -x "$(atm_ai_hermes_desktop_portable_dir)/current.AppImage" ]]
}

atm_ai_hermes_desktop_installed() {
    atm_ai_hermes_desktop_system_installed || atm_ai_hermes_desktop_portable_installed
}

atm_ai_status() {
    local parts=()

    if atm_ai_hermes_installed; then
        parts+=("Hermes-Agent")
    fi

    if atm_ai_hermes_desktop_installed; then
        parts+=("Hermes-Desktop")
    fi

    if ((${#parts[@]} > 0)); then
        printf '✅ %s\n' "${parts[*]}"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_AI_STATUS_NOT_INSTALLED)"
    fi
}

atm_ai_hermes_desktop_latest_version() {
    local api_url="${ATM_AI_HERMES_DESKTOP_RELEASE_API:-https://api.github.com/repos/fathah/hermes-desktop/releases/latest}"
    local tag=""

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf '%s\n' "${ATM_AI_HERMES_DESKTOP_DEFAULT_VERSION:-0.5.0}"
        return 0
    fi

    atm_require_commands curl sed
    tag="$(curl -fsSL "$api_url" | sed -n 's/.*"tag_name":[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | sed -n '1p')"
    [[ -n "$tag" ]] || atm_fail "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_RELEASE_FAILED)"
    printf '%s\n' "$tag"
}

atm_ai_hermes_desktop_deb_url() {
    local version="$1"
    printf 'https://github.com/fathah/hermes-desktop/releases/download/v%s/hermes-desktop_%s_amd64.deb\n' "$version" "$version"
}

atm_ai_hermes_desktop_appimage_url() {
    local version="$1"
    printf 'https://github.com/fathah/hermes-desktop/releases/download/v%s/hermes-desktop-%s.AppImage\n' "$version" "$version"
}

atm_ai_write_manifest() {
    local hermes_installed="0"
    local desktop_installed="0"
    local desktop_portable_installed="0"
    local hermes_version=""
    local desktop_version=""

    hermes_version="$(hermes-agent --version 2>/dev/null || true)"
    desktop_version="$(hermes-desktop --version 2>/dev/null || true)"

    if atm_ai_hermes_installed; then
        hermes_installed="1"
    fi

    if atm_ai_hermes_desktop_installed; then
        desktop_installed="1"
    fi

    if atm_ai_hermes_desktop_portable_installed; then
        desktop_portable_installed="1"
    fi

    atm_manifest_write "ai" \
        "ATM_PLUGIN_NAME=AI Tools" \
        "ATM_PLUGIN_VERSION=0.0.1" \
        "ATM_INSTALLED=1" \
        "ATM_AI_HERMES_AGENT_INSTALLED=$hermes_installed" \
        "ATM_AI_HERMES_AGENT_VERSION=$hermes_version" \
        "ATM_AI_HERMES_DESKTOP_INSTALLED=$desktop_installed" \
        "ATM_AI_HERMES_DESKTOP_PORTABLE_INSTALLED=$desktop_portable_installed" \
        "ATM_AI_HERMES_DESKTOP_VERSION=$desktop_version"
}

atm_ai_install_hermes_agent() {
    local install_url="${ATM_AI_HERMES_INSTALL_URL:-https://hermes-agent.nousresearch.com/install.sh}"

    printf '%s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_AGENT_INSTALLING)"

    if atm_ai_hermes_installed; then
        atm_warn "$(atm_t ATM_PLUGIN_AI_HERMES_AGENT_ALREADY_INSTALLED)"
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
        atm_fail "$(atm_t ATM_PLUGIN_AI_HERMES_AGENT_INSTALL_FAILED)"
    fi

    atm_ai_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_AI_HERMES_AGENT_INSTALLED)"
}

atm_ai_install_hermes_desktop_system() {
    local version=""
    local deb_url=""
    local deb_file=""
    local deb_dir=""
    local deb_name=""

    version="$(atm_ai_hermes_desktop_latest_version)"
    deb_url="${ATM_AI_HERMES_DESKTOP_DEB_URL:-$(atm_ai_hermes_desktop_deb_url "$version")}"
    deb_file="$ATM_DOWNLOAD_DIR/hermes-desktop_${version}_amd64.deb"
    deb_dir="$(dirname "$deb_file")"
    deb_name="$(basename "$deb_file")"

    printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_RELEASES)" "${ATM_AI_HERMES_DESKTOP_RELEASE_PAGE:-https://github.com/fathah/hermes-desktop/releases/}"
    printf '%s %s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_VERSION)" "$version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands curl apt
    fi

    atm_run mkdir -p "$deb_dir"
    atm_run curl -L "$deb_url" -o "$deb_file"
    atm_ai_run_elevated "cd '$deb_dir' && apt install -y './$deb_name'"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && ! atm_ai_hermes_desktop_system_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_INSTALL_FAILED)"
    fi

    atm_ai_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_INSTALLED)"
}

atm_ai_install_hermes_desktop_portable() {
    local version=""
    local appimage_url=""
    local install_dir=""
    local appimage_file=""
    local current_file=""

    version="$(atm_ai_hermes_desktop_latest_version)"
    appimage_url="${ATM_AI_HERMES_DESKTOP_APPIMAGE_URL:-$(atm_ai_hermes_desktop_appimage_url "$version")}"
    install_dir="$(atm_ai_hermes_desktop_portable_dir)"
    appimage_file="$install_dir/hermes-desktop-$version.AppImage"
    current_file="$install_dir/current.AppImage"

    printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_RELEASES)" "${ATM_AI_HERMES_DESKTOP_RELEASE_PAGE:-https://github.com/fathah/hermes-desktop/releases/}"
    printf '%s %s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_VERSION)" "$version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
        atm_require_commands curl ln chmod
    fi

    atm_run mkdir -p "$install_dir"
    atm_run curl -L "$appimage_url" -o "$appimage_file"
    atm_run chmod +x "$appimage_file"
    atm_run ln -sfn "$appimage_file" "$current_file"

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && ! atm_ai_hermes_desktop_portable_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_INSTALL_FAILED)"
    fi

    atm_ai_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_AI_HERMES_DESKTOP_PORTABLE_INSTALLED)"
}

atm_ai_install() {
    atm_ai_hermes_menu "$@"
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

atm_ai_hermes_menu() {
    local choice=""

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    %s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_AI_INSTALL_HERMES_AGENT)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_AI_INSTALL_HERMES_DESKTOP_SYSTEM)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_AI_INSTALL_HERMES_DESKTOP_PORTABLE)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_ai_install_hermes_agent
                ;;
            2)
                atm_ai_install_hermes_desktop_system
                ;;
            3)
                atm_ai_install_hermes_desktop_portable
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
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_AI_HERMES_MENU_OPTION)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_ai_hermes_menu
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

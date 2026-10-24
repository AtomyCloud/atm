#!/usr/bin/env bash

atm_ai_codex_cli_prefix() {
    printf '%s\n' "${ATM_AI_CODEX_CLI_PREFIX:-$ATM_APPS_DIR/ai/codex/cli}"
}

atm_ai_codex_cli_installed() {
    [[ -x "$(atm_ai_codex_cli_prefix)/bin/codex" \
        && -f "$(atm_ai_codex_cli_prefix)/lib/node_modules/@openai/codex/package.json" ]]
}

atm_ai_codex_cli_version() {
    local package_file="$(atm_ai_codex_cli_prefix)/lib/node_modules/@openai/codex/package.json"
    [[ "${ATM_DRY_RUN:-0}" != 1 && -f "$package_file" ]] || return 0
    node -e 'console.log(require(process.argv[1]).version)' "$package_file" 2>/dev/null || true
}

atm_ai_codex_desktop_manager() {
    if command -v apt-get >/dev/null 2>&1; then
        printf 'apt\n'
    elif command -v dnf >/dev/null 2>&1; then
        printf 'dnf\n'
    elif command -v pacman >/dev/null 2>&1; then
        printf 'pacman\n'
    else
        return 1
    fi
}

atm_ai_codex_desktop_installed() {
    case "${1:-$(atm_ai_codex_desktop_manager || true)}" in
        apt) [[ "$(dpkg-query -W -f='${Status}' chatgpt 2>/dev/null)" == 'install ok installed' ]] ;;
        dnf) rpm -q chatgpt >/dev/null 2>&1 ;;
        pacman) pacman -Q chatgpt >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

atm_ai_codex_desktop_version() {
    [[ "${ATM_DRY_RUN:-0}" != 1 ]] || return 0
    case "$(atm_ai_codex_desktop_manager || true)" in
        apt) dpkg-query -W -f='${Version}' chatgpt 2>/dev/null || true ;;
        dnf) rpm -q --qf '%{VERSION}-%{RELEASE}' chatgpt 2>/dev/null || true ;;
        pacman) pacman -Q chatgpt 2>/dev/null | awk '{print $2}' || true ;;
    esac
}

atm_ai_codex_desktop_arch() {
    [[ "$(uname -s)" == Linux ]] || return 1
    case "$(uname -m)" in
        x86_64|amd64) printf 'amd64\n' ;;
        aarch64|arm64) printf 'arm64\n' ;;
        *) return 1 ;;
    esac
}

atm_ai_codex_desktop_url() {
    local manager="$1"
    local arch="$2"
    local base="${ATM_AI_CODEX_DESKTOP_BASE_URL:-https://persistent.oaistatic.com/codex-app-prod/linux}"
    case "$manager:$arch" in
        apt:amd64|apt:arm64) printf '%s/deb/latest/chatgpt_%s.deb\n' "$base" "$arch" ;;
        dnf:amd64) printf '%s/rpm/latest/chatgpt.x86_64.rpm\n' "$base" ;;
        dnf:arm64) printf '%s/rpm/latest/chatgpt.aarch64.rpm\n' "$base" ;;
        pacman:amd64|pacman:arm64) printf '%s/install-arch.sh\n' "$base" ;;
        *) return 1 ;;
    esac
}

atm_ai_codex_confirm_remove() {
    printf '%s\n' "$1"
    printf '%s ' "$(atm_t ATM_PLUGIN_AI_CODEX_CONFIRM)"
    local answer=""
    read -r answer || return 1
    case "$answer" in
        y|Y|yes|YES) return 0 ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_CANCELLED)"; return 1 ;;
    esac
}

atm_ai_codex_cli_action() {
    local action="$1"
    local prefix="$(atm_ai_codex_cli_prefix)"

    case "$action" in
        install)
            if atm_ai_codex_cli_installed; then
                atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_ALREADY_INSTALLED)"
                return 0
            fi
            ;;
        update|remove)
            if ! atm_ai_codex_cli_installed; then
                atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_CLI_NOT_INSTALLED)"
                return 0
            fi
            ;;
        *) return 1 ;;
    esac
    if [[ "$action" == remove ]]; then
        atm_ai_codex_confirm_remove "$(atm_t ATM_PLUGIN_AI_CODEX_CLI_REMOVE_WARNING)" || return 0
    fi
    if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
        if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
            atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_NODE_REQUIRED)"
        fi
    fi
    if [[ "$action" == remove ]]; then
        atm_run npm uninstall --global --prefix "$prefix" @openai/codex \
            || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        if [[ "${ATM_DRY_RUN:-0}" != 1 ]] && atm_ai_codex_cli_installed; then
            atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        fi
    else
        atm_run npm install --global --prefix "$prefix" @openai/codex@latest \
            || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        if [[ "${ATM_DRY_RUN:-0}" != 1 ]] && ! atm_ai_codex_cli_installed; then
            atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        fi
        printf '%s %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_RUN_CLI)" "$prefix/bin/codex"
        atm_info "$(atm_t ATM_PLUGIN_AI_CODEX_PATH_HINT)"
    fi
    atm_ai_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_DONE)"
}

atm_ai_codex_desktop_action() {
    local action="$1"
    local manager=""
    local arch=""
    local url=""
    local cache_dir="${ATM_AI_CODEX_CACHE_DIR:-$ATM_DOWNLOAD_DIR/ai/codex}"
    local package_file=""
    local command_text=""
    local rpm_arch="x86_64"

    case "$action" in install|update|remove) ;; *) return 1 ;; esac
    manager="$(atm_ai_codex_desktop_manager)" \
        || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_UNSUPPORTED_DESKTOP)"
    arch="$(atm_ai_codex_desktop_arch)" \
        || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_UNSUPPORTED_DESKTOP)"
    [[ "$arch" != arm64 ]] || rpm_arch="aarch64"
    if [[ "$action" == install ]]; then
        if atm_ai_codex_desktop_installed "$manager"; then
            atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_ALREADY_INSTALLED)"
            return 0
        fi
    elif ! atm_ai_codex_desktop_installed "$manager"; then
        atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_DESKTOP_NOT_INSTALLED)"
        return 0
    fi

    if [[ "$action" == remove ]]; then
        atm_ai_codex_confirm_remove "$(atm_t ATM_PLUGIN_AI_CODEX_DESKTOP_REMOVE_WARNING)" || return 0
        case "$manager" in
            apt) command_text='apt-get remove -y chatgpt' ;;
            dnf) command_text='dnf remove -y chatgpt' ;;
            pacman) command_text='pacman -R chatgpt' ;;
        esac
    elif [[ "$action" == update ]]; then
        case "$manager" in
            apt)
                atm_ai_run_elevated 'apt-get update' \
                    || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
                command_text='apt-get install -y --only-upgrade chatgpt'
                ;;
            dnf) command_text='dnf upgrade -y --refresh chatgpt' ;;
            pacman)
                atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_ARCH_UPGRADE)"
                command_text='pacman -Syu'
                ;;
        esac
    else
        url="$(atm_ai_codex_desktop_url "$manager" "$arch")"
        package_file="$cache_dir/${url##*/}"
        if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
            atm_require_commands curl
        fi
        atm_run mkdir -p "$cache_dir" || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        atm_run curl --proto '=https' --tlsv1.2 -fL --retry 3 "$url" -o "$package_file.part" \
            || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        atm_run mv -f -- "$package_file.part" "$package_file" \
            || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        case "$manager" in
            apt)
                if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
                    [[ "$(dpkg-deb -f "$package_file" Package)" == chatgpt \
                        && "$(dpkg-deb -f "$package_file" Architecture)" == "$arch" ]] \
                        || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_PACKAGE_MISMATCH)"
                fi
                printf -v command_text 'apt-get install -y %q' "$package_file"
                ;;
            dnf)
                if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
                    [[ "$(rpm -qp --qf '%{NAME}' "$package_file")" == chatgpt \
                        && "$(rpm -qp --qf '%{ARCH}' "$package_file")" == "$rpm_arch" ]] \
                        || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_PACKAGE_MISMATCH)"
                fi
                printf -v command_text 'dnf install -y %q' "$package_file"
                ;;
            pacman)
                atm_warn "$(atm_t ATM_PLUGIN_AI_CODEX_ARCH_UPGRADE)"
                printf -v command_text 'bash %q' "$package_file"
                ;;
        esac
    fi
    atm_ai_run_elevated "$command_text" || atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
    if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
        if [[ "$action" == remove ]]; then
            if atm_ai_codex_desktop_installed "$manager"; then
                atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
            fi
        elif ! atm_ai_codex_desktop_installed "$manager"; then
            atm_fail "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_FAILED)"
        fi
    fi
    atm_ai_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_AI_CODEX_ACTION_DONE)"
    if [[ "$action" != remove ]]; then
        atm_info "$(atm_t ATM_PLUGIN_AI_CODEX_DESKTOP_SIGNIN)"
    fi
}

atm_ai_codex_menu() {
    local choice=""
    while true; do
        clear
        printf '%s\n' '=========================================='
        printf '    %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_MENU_TITLE)"
        printf '%s\n' 'https://openai.com/codex/'
        printf '%s\n' '=========================================='
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_CLI_INSTALL)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_CLI_UPDATE)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_CLI_REMOVE)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_DESKTOP_INSTALL)"
        printf '5) %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_DESKTOP_UPDATE)"
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_AI_CODEX_DESKTOP_REMOVE)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_ai_codex_cli_action install ;;
            2) atm_ai_codex_cli_action update ;;
            3) atm_ai_codex_cli_action remove ;;
            4) atm_ai_codex_desktop_action install ;;
            5) atm_ai_codex_desktop_action update ;;
            6) atm_ai_codex_desktop_action remove ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

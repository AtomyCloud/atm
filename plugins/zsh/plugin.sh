#!/usr/bin/env bash

atm_zsh_require_user() {
    if [[ "${EUID:-$(id -u)}" == 0 && -n "${SUDO_USER:-}" && "${SUDO_USER:-}" != root ]]; then
        atm_fail "$(atm_t ATM_PLUGIN_ZSH_RUN_AS_USER)"
    fi
}

atm_zsh_require_apt() {
    [[ "$(uname -s)" == Linux ]] && command -v apt-get >/dev/null 2>&1 \
        || atm_fail "$(atm_t ATM_PLUGIN_ZSH_APT_REQUIRED)"
}

atm_zsh_run_elevated() {
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_run "$@"
    elif [[ "${EUID:-$(id -u)}" == 0 ]]; then
        "$@"
    else
        atm_require_commands sudo
        atm_info "$(atm_t ATM_PLUGIN_ZSH_SUDO)"
        sudo --validate || return 1
        sudo -- "$@"
    fi
}

atm_zsh_state() {
    local action="$1"
    shift
    python3 "${BASH_SOURCE[0]%/*}/state.py" "$action" \
        --home "$ATM_ZSH_USER_HOME" --dot-dir "$ATM_ZSH_DOT_DIR" \
        --omz "$ATM_ZSH_OMZ_DIR" --custom "$ATM_ZSH_CUSTOM_DIR" \
        --backups "$ATM_ZSH_BACKUP_DIR" "$@"
}

atm_zsh_omz_installed() {
    [[ -f "$ATM_ZSH_OMZ_DIR/oh-my-zsh.sh" ]]
}

atm_zsh_bin() {
    type -P zsh
}

atm_zsh_status() {
    local parts=()
    if command -v zsh >/dev/null 2>&1; then parts+=(Zsh); fi
    if atm_zsh_omz_installed; then parts+=("Oh My Zsh"); fi
    if ((${#parts[@]} > 0)); then
        printf '✅ %s\n' "${parts[*]}"
    else
        atm_t ATM_PLUGIN_ZSH_NOT_INSTALLED
    fi
}

atm_zsh_write_manifest() {
    local shell_installed=0 omz_installed=0 installed=0
    local version="" revision="" shell_bin=""
    if command -v zsh >/dev/null 2>&1; then shell_installed=1; fi
    shell_bin="$(atm_zsh_bin || true)"
    if atm_zsh_omz_installed; then omz_installed=1; fi
    if [[ "$shell_installed$omz_installed" != 00 ]]; then installed=1; fi
    if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
        if [[ "$shell_installed" == 1 ]]; then version="$(zsh --version 2>/dev/null || true)"; fi
        if [[ "$omz_installed" == 1 ]]; then revision="$(git -C "$ATM_ZSH_OMZ_DIR" rev-parse HEAD 2>/dev/null || true)"; fi
    fi
    atm_manifest_write zsh \
        'ATM_PLUGIN_NAME="Zsh + Oh My Zsh"' 'ATM_PLUGIN_VERSION="0.0.1"' \
        "ATM_INSTALLED=$installed" "ATM_ZSH_INSTALLED=$shell_installed" \
        "ATM_OMZ_INSTALLED=$omz_installed" "ATM_CURRENT_VERSION=$(printf '%q' "$version")" \
        "ATM_CURRENT_PATH=$(printf '%q' "$shell_bin")" "ATM_INSTALLED_VERSIONS=$(printf '%q' "$version")" \
        "ATM_OMZ_REVISION=$(printf '%q' "$revision")" \
        "ATM_INSTALL_ROOT=$(printf '%q' "$ATM_ZSH_OMZ_DIR")" \
        "ATM_ZSH_DOT_DIR=$(printf '%q' "$ATM_ZSH_DOT_DIR")" \
        "ATM_ZSH_BACKUP_DIR=$(printf '%q' "$ATM_ZSH_BACKUP_DIR")"
}

atm_zsh_confirm() {
    local answer=""
    printf '%s\n' "$1"
    printf '%s ' "$(atm_t ATM_PLUGIN_ZSH_CONFIRM)"
    read -r answer || return 1
    case "$answer" in
        y|Y|yes|YES) return 0 ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_ZSH_CANCELLED)"; return 1 ;;
    esac
}

atm_zsh_backup() {
    local archive=""
    atm_zsh_require_user
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: %s (%s)\n' "$(atm_t ATM_PLUGIN_ZSH_BACKUP_PLAN)" "$ATM_ZSH_BACKUP_DIR"
        return 0
    fi
    atm_require_commands python3
    archive="$(atm_zsh_state backup)" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_success "$(atm_t ATM_PLUGIN_ZSH_BACKUP_CREATED) $archive"
}

atm_zsh_list_backups() {
    [[ -d "$ATM_ZSH_BACKUP_DIR" ]] || return 0
    find "$ATM_ZSH_BACKUP_DIR" -maxdepth 1 -type f -name 'zsh-*.tar.gz' -print | sort
}

atm_zsh_install() {
    local installer="$ATM_ZSH_CACHE_DIR/install.sh"
    atm_zsh_require_user
    atm_zsh_require_apt
    atm_zsh_run_elevated apt-get update || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_zsh_run_elevated apt-get install -y zsh git curl python3 || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_zsh_backup
    if ! atm_zsh_omz_installed; then
        [[ ! -e "$ATM_ZSH_OMZ_DIR" ]] || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
        atm_run mkdir -p "$ATM_ZSH_CACHE_DIR" "$ATM_ZSH_DOT_DIR" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
        atm_run curl --proto '=https' --tlsv1.2 -fsSL --retry 3 "$ATM_ZSH_INSTALL_URL" -o "$installer.part" \
            || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
        atm_run mv -f -- "$installer.part" "$installer" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
        atm_run env "ZSH=$ATM_ZSH_OMZ_DIR" "ZDOTDIR=$ATM_ZSH_DOT_DIR" \
            REMOTE=https://github.com/ohmyzsh/ohmyzsh.git BRANCH=master \
            CHSH=no RUNZSH=no KEEP_ZSHRC=yes sh "$installer" --unattended --keep-zshrc \
            || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    fi
    atm_run atm_zsh_state configure || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    if [[ "${ATM_DRY_RUN:-0}" != 1 ]] && ! atm_zsh_omz_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    fi
    atm_zsh_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_ZSH_INSTALLED)"
}

atm_zsh_update() {
    atm_zsh_require_user
    atm_zsh_require_apt
    if ! command -v zsh >/dev/null 2>&1 || ! atm_zsh_omz_installed; then
        atm_warn "$(atm_t ATM_PLUGIN_ZSH_INSTALL_FIRST)"
        return 0
    fi
    atm_zsh_backup
    atm_zsh_run_elevated apt-get update || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_zsh_run_elevated apt-get install -y --only-upgrade zsh || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_run env "ZSH=$ATM_ZSH_OMZ_DIR" zsh -f "$ATM_ZSH_OMZ_DIR/tools/upgrade.sh" \
        || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_zsh_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_ZSH_UPDATED)"
}

atm_zsh_login_shell() {
    local entry=""
    entry="$(getent passwd "$(id -un)")" || return 1
    printf '%s\n' "${entry##*:}"
}

atm_zsh_uninstall() {
    local login_shell=""
    atm_zsh_require_user
    atm_zsh_require_apt
    atm_zsh_confirm "$(atm_t ATM_PLUGIN_ZSH_REMOVE_WARNING)" || return 0
    atm_zsh_backup
    login_shell="$(atm_zsh_login_shell)" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
    [[ "$login_shell" == /* ]] || atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
    if [[ "${login_shell##*/}" == zsh* ]]; then
        [[ -x /bin/bash ]] && grep -Fxq /bin/bash /etc/shells \
            || atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
        atm_run chsh -s /bin/bash || atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
        if [[ "${ATM_DRY_RUN:-0}" != 1 && "$(atm_zsh_login_shell)" != /bin/bash ]]; then
            atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
        fi
    fi
    atm_run atm_zsh_state unconfigure || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_zsh_run_elevated apt-get remove -y zsh || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_run rm -rf -- "$ATM_ZSH_OMZ_DIR" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_run rm -f -- "$ATM_ZSH_MANIFEST_FILE" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_success "$(atm_t ATM_PLUGIN_ZSH_REMOVED)"
}

atm_zsh_remove() { atm_zsh_uninstall; }
atm_zsh_path_entries() { return 0; }
atm_zsh_use() { atm_warn "$(atm_t ATM_PLUGIN_ZSH_USE_UNSUPPORTED)"; }

atm_zsh_set_default() {
    local shell_bin=""
    atm_zsh_require_user
    shell_bin="$(atm_zsh_bin)" || { atm_warn "$(atm_t ATM_PLUGIN_ZSH_INSTALL_FIRST)"; return 0; }
    atm_zsh_confirm "$(atm_t ATM_PLUGIN_ZSH_DEFAULT_WARNING)" || return 0
    grep -Fxq "$shell_bin" /etc/shells || atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
    atm_run chsh -s "$shell_bin" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
    if [[ "${ATM_DRY_RUN:-0}" != 1 && "$(atm_zsh_login_shell)" != "$shell_bin" ]]; then
        atm_fail "$(atm_t ATM_PLUGIN_ZSH_SHELL_FAILED)"
    fi
    atm_success "$(atm_t ATM_PLUGIN_ZSH_DEFAULT_DONE)"
}

atm_zsh_restore() {
    local archive="${1:-}"
    local choice="" item="" idx=1
    local archives=()
    atm_zsh_require_user
    if [[ -z "$archive" ]]; then
        mapfile -t archives < <(atm_zsh_list_backups)
        if ((${#archives[@]} == 0)); then
            atm_warn "$(atm_t ATM_PLUGIN_ZSH_NO_BACKUPS)"
            return 0
        fi
        printf '%s\n' "$(atm_t ATM_PLUGIN_ZSH_SELECT_BACKUP)"
        for item in "${archives[@]}"; do
            printf '%s) %s\n' "$idx" "${item##*/}"
            idx=$((idx + 1))
        done
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        read -r choice || return 0
        case "$choice" in b|B) return 0 ;; esac
        idx=1
        for item in "${archives[@]}"; do
            if [[ "$choice" == "$idx" ]]; then archive="$item"; break; fi
            idx=$((idx + 1))
        done
        [[ -n "$archive" ]] || { atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; return 0; }
    fi
    atm_zsh_confirm "$(atm_t ATM_PLUGIN_ZSH_RESTORE_WARNING)" || return 0
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: %s (%s)\n' "$(atm_t ATM_PLUGIN_ZSH_RESTORE_PLAN)" "$archive"
        return 0
    fi
    atm_require_commands python3
    atm_zsh_state validate --archive "$archive" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_INVALID_BACKUP)"
    atm_zsh_backup
    atm_zsh_state restore --archive "$archive" || atm_fail "$(atm_t ATM_PLUGIN_ZSH_FAILED)"
    atm_zsh_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_ZSH_RESTORED)"
}

atm_zsh_menu() {
    local choice=""
    while true; do
        clear || true
        printf '%s\n' '=========================================='
        printf '    %s\n' "$(atm_t ATM_PLUGIN_ZSH_MENU_TITLE)"
        printf '%s\n' 'https://ohmyz.sh/#install'
        printf '%s\n' '=========================================='
        printf '%s\n' "$(atm_zsh_status)"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_ZSH_INSTALL)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_ZSH_UPDATE)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_ZSH_REMOVE)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_ZSH_BACKUP)"
        printf '5) %s\n' "$(atm_t ATM_PLUGIN_ZSH_RESTORE)"
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_ZSH_LIST_BACKUPS)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_ZSH_DEFAULT_SHELL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_zsh_install ;;
            2) atm_zsh_update ;;
            3) atm_zsh_uninstall ;;
            4) atm_zsh_backup ;;
            5) atm_zsh_restore ;;
            6) atm_zsh_list_backups ;;
            7) atm_zsh_set_default ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

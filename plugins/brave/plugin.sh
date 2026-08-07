#!/usr/bin/env bash

atm_brave_manifest_file() {
    printf '%s\n' "${ATM_BRAVE_MANIFEST_FILE:-$ATM_MANIFEST_DIR/brave.manifest}"
}

atm_brave_package_name() {
    printf '%s\n' "${ATM_BRAVE_PACKAGE_NAME:-brave-browser}"
}

atm_brave_profile_dir() {
    printf '%s\n' "${ATM_BRAVE_PROFILE_DIR:-$HOME/.config/BraveSoftware/Brave-Browser}"
}

atm_brave_backup_dir() {
    printf '%s\n' "${ATM_BRAVE_BACKUP_DIR:-$ATM_APPS_DIR/backups/brave}"
}

atm_brave_package_manager() {
    if command -v apt-get >/dev/null 2>&1; then
        printf '%s\n' "apt-get"
    elif command -v dnf >/dev/null 2>&1; then
        printf '%s\n' "dnf"
    else
        atm_fail "$(atm_t ATM_PLUGIN_BRAVE_UNSUPPORTED_MANAGER)"
    fi
}

atm_brave_run_elevated() {
    local command_text="$1"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: %s\n' "$command_text"
        return 0
    fi

    if [[ "${EUID:-$(id -u)}" == "0" ]]; then
        bash -c "$command_text"
        return 0
    fi

    command -v sudo >/dev/null 2>&1 || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_SUDO_NOT_FOUND)"
    printf '%s\n' "$(atm_t ATM_PLUGIN_BRAVE_SUDO_REQUIRED)"
    sudo --validate || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_SUDO_FAILED)"
    sudo bash -c "$command_text"
}

atm_brave_installed() {
    command -v brave-browser >/dev/null 2>&1
}

atm_brave_browser_running() {
    command -v pgrep >/dev/null 2>&1 || return 1
    pgrep -u "${USER:-$(id -un)}" -f 'brave-browser' >/dev/null 2>&1
}

atm_brave_require_profile_closed() {
    if atm_brave_browser_running; then
        atm_fail "$(atm_t ATM_PLUGIN_BRAVE_CLOSE_BROWSER_FIRST)"
    fi
}

atm_brave_status() {
    if atm_brave_installed; then
        printf '✅ '
        brave-browser --version 2>/dev/null || printf '%s\n' "$(atm_t ATM_PLUGIN_BRAVE_INSTALLED)"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_BRAVE_STATUS_NOT_INSTALLED)"
    fi
}

atm_brave_write_manifest() {
    local installed="${1:-1}"
    local version=""
    local manager=""

    version="$(brave-browser --version 2>/dev/null || true)"
    manager="$(atm_brave_package_manager 2>/dev/null || true)"

    atm_manifest_write "brave" \
        "ATM_PLUGIN_NAME=Brave Browser" \
        "ATM_PLUGIN_VERSION=0.0.1" \
        "ATM_INSTALLED=$installed" \
        "ATM_PACKAGE_MANAGER=$manager" \
        "ATM_BRAVE_VERSION=$version" \
        "ATM_BRAVE_PROFILE_DIR=$(atm_brave_profile_dir)" \
        "ATM_BRAVE_BACKUP_DIR=$(atm_brave_backup_dir)"
}

atm_brave_apt_install_commands() {
    local package_name=""
    local keyring_url="${ATM_BRAVE_APT_KEYRING_URL:-https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg}"
    local keyring_file="${ATM_BRAVE_APT_KEYRING_FILE:-/usr/share/keyrings/brave-browser-archive-keyring.gpg}"
    local sources_url="${ATM_BRAVE_APT_SOURCES_URL:-https://brave-browser-apt-release.s3.brave.com/brave-browser.sources}"
    local sources_file="${ATM_BRAVE_APT_SOURCES_FILE:-/etc/apt/sources.list.d/brave-browser-release.sources}"

    package_name="$(atm_brave_package_name)"

    printf '%s\n' "apt-get update"
    printf '%s\n' "apt-get install -y curl"
    printf '%s\n' "curl -fsSLo '$keyring_file' '$keyring_url'"
    printf '%s\n' "curl -fsSLo '$sources_file' '$sources_url'"
    printf '%s\n' "apt-get update"
    printf '%s\n' "apt-get install -y '$package_name'"
}

atm_brave_dnf_install_commands() {
    local package_name=""
    local repo_url="${ATM_BRAVE_RPM_REPO_URL:-https://brave-browser-rpm-release.s3.brave.com/brave-browser.repo}"

    package_name="$(atm_brave_package_name)"

    printf '%s\n' "dnf install -y dnf-plugins-core"
    printf '%s\n' "dnf config-manager addrepo --from-repofile='$repo_url' || dnf config-manager --add-repo '$repo_url'"
    printf '%s\n' "dnf install -y '$package_name'"
}

atm_brave_install_commands() {
    local package_manager="$1"

    case "$package_manager" in
        apt-get) atm_brave_apt_install_commands ;;
        dnf) atm_brave_dnf_install_commands ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_BRAVE_UNSUPPORTED_MANAGER): $package_manager" ;;
    esac
}

atm_brave_apt_remove_commands() {
    local package_name=""
    local keyring_file="${ATM_BRAVE_APT_KEYRING_FILE:-/usr/share/keyrings/brave-browser-archive-keyring.gpg}"
    local sources_file="${ATM_BRAVE_APT_SOURCES_FILE:-/etc/apt/sources.list.d/brave-browser-release.sources}"

    package_name="$(atm_brave_package_name)"

    printf '%s\n' "apt-get remove -y '$package_name'"
    printf '%s\n' "rm -f '$keyring_file' '$sources_file'"
    printf '%s\n' "apt-get update"
}

atm_brave_dnf_remove_commands() {
    local package_name=""
    local repo_file="${ATM_BRAVE_RPM_REPO_FILE:-/etc/yum.repos.d/brave-browser.repo}"

    package_name="$(atm_brave_package_name)"

    printf '%s\n' "dnf remove -y '$package_name'"
    printf '%s\n' "rm -f '$repo_file'"
    printf '%s\n' "dnf makecache"
}

atm_brave_remove_commands() {
    local package_manager="$1"

    case "$package_manager" in
        apt-get) atm_brave_apt_remove_commands ;;
        dnf) atm_brave_dnf_remove_commands ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_BRAVE_UNSUPPORTED_MANAGER): $package_manager" ;;
    esac
}

atm_brave_install() {
    local package_manager=""
    local command_text=""

    if atm_brave_installed; then
        atm_warn "$(atm_t ATM_PLUGIN_BRAVE_ALREADY_INSTALLED)"
        atm_brave_status
        return 0
    fi

    package_manager="$(atm_brave_package_manager)"
    printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_BRAVE_INSTALL_DOCS)" "https://brave.com/linux/"
    printf '%s\n' "$(atm_t ATM_PLUGIN_BRAVE_INSTALLING)"

    while IFS= read -r command_text || [[ -n "$command_text" ]]; do
        [[ -n "$command_text" ]] || continue
        atm_brave_run_elevated "$command_text"
    done < <(atm_brave_install_commands "$package_manager")

    if [[ "${ATM_DRY_RUN:-0}" != "1" ]] && ! atm_brave_installed; then
        atm_fail "$(atm_t ATM_PLUGIN_BRAVE_INSTALL_FAILED)"
    fi

    atm_brave_write_manifest "1"
    atm_success "$(atm_t ATM_PLUGIN_BRAVE_INSTALLED)"
}

atm_brave_remove() {
    atm_brave_uninstall "$@"
}

atm_brave_uninstall() {
    local package_manager=""
    local command_text=""
    local answer=""

    package_manager="$(atm_brave_package_manager)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_BRAVE_UNINSTALL_WARNING)"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES) ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_BRAVE_CANCELLED)"
            return 0
            ;;
    esac

    while IFS= read -r command_text || [[ -n "$command_text" ]]; do
        [[ -n "$command_text" ]] || continue
        atm_brave_run_elevated "$command_text"
    done < <(atm_brave_remove_commands "$package_manager")

    atm_brave_write_manifest "0"
    atm_success "$(atm_t ATM_PLUGIN_BRAVE_UNINSTALLED)"
}

atm_brave_show_commands() {
    local package_manager=""

    package_manager="$(atm_brave_package_manager)"

    printf '%s\n' "# Install"
    atm_brave_install_commands "$package_manager"
    printf '%s\n' "# Uninstall"
    atm_brave_remove_commands "$package_manager"
}

atm_brave_backup_file() {
    local timestamp=""

    timestamp="$(date +%Y%m%d-%H%M%S)"
    printf '%s/brave-profile-%s.tar.gz\n' "$(atm_brave_backup_dir)" "$timestamp"
}

atm_brave_backup_profile() {
    local profile_dir=""
    local profile_parent=""
    local profile_name=""
    local backup_dir=""
    local backup_file=""

    profile_dir="$(atm_brave_profile_dir)"
    profile_parent="$(dirname "$profile_dir")"
    profile_name="$(basename "$profile_dir")"
    backup_dir="$(atm_brave_backup_dir)"
    backup_file="$(atm_brave_backup_file)"

    [[ -d "$profile_dir" ]] || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_PROFILE_NOT_FOUND): $profile_dir"
    atm_brave_require_profile_closed

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: mkdir -p %s\n' "$backup_dir"
        printf 'DRY-RUN: tar -czf %s -C %s %s\n' "$backup_file" "$profile_parent" "$profile_name"
        return 0
    fi

    atm_require_commands tar date dirname basename
    mkdir -p "$backup_dir" || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_FAILED): $backup_dir"
    tar -czf "$backup_file" -C "$profile_parent" "$profile_name" || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_FAILED): $backup_file"
    atm_success "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_CREATED): $backup_file"
}

atm_brave_list_backups() {
    local backup_dir=""

    backup_dir="$(atm_brave_backup_dir)"

    if [[ ! -d "$backup_dir" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_BRAVE_NO_BACKUPS): $backup_dir"
        return 0
    fi

    find "$backup_dir" -maxdepth 1 -type f -name 'brave-profile-*.tar.gz' | sort
}

atm_brave_latest_backup() {
    atm_brave_list_backups 2>/dev/null | tail -n 1
}

atm_brave_restore_latest_backup() {
    local archive_file=""
    local profile_dir=""
    local profile_parent=""
    local safety_backup=""
    local answer=""

    archive_file="$(atm_brave_latest_backup)"
    [[ -n "$archive_file" && -f "$archive_file" ]] || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_NO_BACKUPS): $(atm_brave_backup_dir)"

    profile_dir="$(atm_brave_profile_dir)"
    profile_parent="$(dirname "$profile_dir")"
    safety_backup="${profile_dir}.before-restore.$(date +%Y%m%d-%H%M%S)"

    atm_brave_require_profile_closed

    printf '%s\n' "$(atm_t ATM_PLUGIN_BRAVE_RESTORE_WARNING)"
    printf '%s: %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_FILE)" "$archive_file"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES) ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_BRAVE_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: mkdir -p %s\n' "$profile_parent"
        [[ -e "$profile_dir" ]] && printf 'DRY-RUN: mv %s %s\n' "$profile_dir" "$safety_backup"
        printf 'DRY-RUN: tar -xzf %s -C %s\n' "$archive_file" "$profile_parent"
        return 0
    fi

    atm_require_commands tar date dirname
    mkdir -p "$profile_parent" || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_RESTORE_FAILED): $profile_parent"

    if [[ -e "$profile_dir" ]]; then
        mv "$profile_dir" "$safety_backup" || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_RESTORE_FAILED): $safety_backup"
        atm_warn "$(atm_t ATM_PLUGIN_BRAVE_EXISTING_PROFILE_BACKED_UP): $safety_backup"
    fi

    tar -xzf "$archive_file" -C "$profile_parent" || atm_fail "$(atm_t ATM_PLUGIN_BRAVE_RESTORE_FAILED): $archive_file"
    atm_success "$(atm_t ATM_PLUGIN_BRAVE_RESTORED): $archive_file"
}

atm_brave_backup_menu() {
    local choice=""

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    🦁 %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_BRAVE_PROFILE_DIR)" "$(atm_brave_profile_dir)"
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_DIR)" "$(atm_brave_backup_dir)"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_CREATE)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_LIST)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_RESTORE_LATEST)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1) atm_brave_backup_profile ;;
            2) atm_brave_list_backups ;;
            3) atm_brave_restore_latest_backup ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

atm_brave_use() {
    atm_warn "$(atm_t ATM_PLUGIN_BRAVE_USE_NOT_SUPPORTED)"
}

atm_brave_path_entries() {
    return 0
}

atm_brave_menu() {
    local choice=""
    local current=""

    while true; do
        clear
        current="$(atm_brave_status)"

        printf '%s\n' "=========================================="
        printf '    🦁 %s\n' "$(atm_t ATM_PLUGIN_BRAVE_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_BRAVE_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_INSTALL)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_UNINSTALL)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_SHOW_COMMANDS)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_BRAVE_BACKUP_MENU)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1) atm_brave_install ;;
            2) atm_brave_uninstall ;;
            3) atm_brave_show_commands ;;
            4) atm_brave_backup_menu ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

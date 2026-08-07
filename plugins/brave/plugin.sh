#!/usr/bin/env bash

atm_brave_manifest_file() {
    printf '%s\n' "${ATM_BRAVE_MANIFEST_FILE:-$ATM_MANIFEST_DIR/brave.manifest}"
}

atm_brave_package_name() {
    printf '%s\n' "${ATM_BRAVE_PACKAGE_NAME:-brave-browser}"
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
        "ATM_BRAVE_VERSION=$version"
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
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1) atm_brave_install ;;
            2) atm_brave_uninstall ;;
            3) atm_brave_show_commands ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

#!/usr/bin/env bash

atm_os_packages_manifest_file() {
    printf '%s\n' "${ATM_OS_PACKAGES_MANIFEST_FILE:-$ATM_MANIFEST_DIR/os_packages.manifest}"
}

atm_os_packages_package_file() {
    printf '%s\n' "${ATM_OS_PACKAGES_PACKAGE_FILE:-$ATM_PLUGIN_DIR/os_packages/packages.txt}"
}

atm_os_packages_detect() {
    local os_id=""
    local os_like=""
    local package_manager=""
    local os_name=""

    if [[ -r /etc/os-release ]]; then
        os_id="$(. /etc/os-release && printf '%s\n' "${ID:-}" | tr '[:upper:]' '[:lower:]')"
        os_like="$(. /etc/os-release && printf '%s\n' "${ID_LIKE:-}" | tr '[:upper:]' '[:lower:]')"
    fi

    case "$os_id" in
        zorin)
            package_manager="apt-get"
            os_name="zorin"
            ;;
        ubuntu)
            package_manager="apt-get"
            os_name="ubuntu"
            ;;
        debian)
            package_manager="apt-get"
            os_name="debian"
            ;;
        centos)
            package_manager="yum"
            os_name="centos"
            ;;
        rocky|rockylinux)
            package_manager="dnf"
            os_name="rockylinux"
            ;;
        almalinux)
            package_manager="dnf"
            os_name="almalinux"
            ;;
        arch|archlinux)
            package_manager="pacman"
            os_name="archlinux"
            ;;
        alpine)
            package_manager="apk"
            os_name="alpine"
            ;;
        *)
            if [[ -f /etc/arch-release ]]; then
                package_manager="pacman"
                os_name="archlinux"
            elif [[ "$os_like" == *"debian"* || "$os_like" == *"ubuntu"* ]]; then
                package_manager="apt-get"
                os_name="${os_id:-debian-like}"
            elif [[ "$os_like" == *"rhel"* || "$os_like" == *"fedora"* ]]; then
                package_manager="dnf"
                os_name="${os_id:-rhel-like}"
            else
                atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNSUPPORTED)"
            fi
            ;;
    esac

    printf '%s\t%s\n' "$os_name" "$package_manager"
}

atm_os_packages_os_name() {
    atm_os_packages_detect | awk -F'\t' '{ print $1 }'
}

atm_os_packages_package_manager() {
    atm_os_packages_detect | awk -F'\t' '{ print $2 }'
}

atm_os_packages_packages_for_manager() {
    local package_manager="$1"
    local package_file=""
    local packages=""

    package_file="$(atm_os_packages_package_file)"
    [[ -f "$package_file" ]] || atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_PACKAGE_FILE_NOT_FOUND): $package_file"

    packages="$(
        awk -v section="$package_manager" '
            function trim(value) {
                gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
                return value
            }

            {
                line = $0
                sub(/[[:space:]]+#.*/, "", line)
                line = trim(line)

                if (line == "" || line ~ /^#/) {
                    next
                }

                if (line ~ /^\[[^]]+\]$/) {
                    active = (line == "[" section "]")
                    next
                }

                if (active) {
                    print line
                }
            }
        ' "$package_file" | tr '\n' ' ' | sed 's/[[:space:]]*$//'
    )"

    printf '%s\n' "$packages"
}

atm_os_packages_update_command() {
    local package_manager="$1"

    case "$package_manager" in
        apt-get) printf '%s\n' "apt-get update" ;;
        dnf) printf '%s\n' "dnf makecache" ;;
        yum) printf '%s\n' "yum makecache" ;;
        pacman) printf '%s\n' "pacman -Sy" ;;
        apk) printf '%s\n' "apk update" ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNSUPPORTED_MANAGER): $package_manager" ;;
    esac
}

atm_os_packages_install_command() {
    local package_manager="$1"
    local packages="$2"

    case "$package_manager" in
        apt-get) printf 'apt-get install -y %s\n' "$packages" ;;
        dnf) printf 'dnf install -y %s\n' "$packages" ;;
        yum) printf 'yum install -y %s\n' "$packages" ;;
        pacman) printf 'pacman -S --needed --noconfirm %s\n' "$packages" ;;
        apk) printf 'apk add %s\n' "$packages" ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNSUPPORTED_MANAGER): $package_manager" ;;
    esac
}

atm_os_packages_remove_command() {
    local package_manager="$1"
    local packages="$2"

    case "$package_manager" in
        apt-get) printf 'apt-get remove -y %s\n' "$packages" ;;
        dnf) printf 'dnf remove -y %s\n' "$packages" ;;
        yum) printf 'yum remove -y %s\n' "$packages" ;;
        pacman) printf 'pacman -R --noconfirm %s\n' "$packages" ;;
        apk) printf 'apk del %s\n' "$packages" ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNSUPPORTED_MANAGER): $package_manager" ;;
    esac
}

atm_os_packages_run_command() {
    local command_text="$1"
    local run_command="$command_text"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: %s\n' "$command_text"
        return 0
    fi

    if [[ "${EUID:-$(id -u)}" != "0" ]]; then
        command -v sudo >/dev/null 2>&1 || atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_SUDO_NOT_FOUND)"

        printf '%s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_SUDO_REQUIRED)"
        sudo --validate || atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_SUDO_FAILED)"
        run_command="sudo $command_text"
    fi

    bash -c "$run_command"
}

atm_os_packages_write_manifest() {
    local installed="${1:-1}"
    local os_name=""
    local package_manager=""
    local packages=""

    os_name="$(atm_os_packages_os_name)"
    package_manager="$(atm_os_packages_package_manager)"
    packages="$(atm_os_packages_packages_for_manager "$package_manager")"

    atm_manifest_write "os_packages" \
        "ATM_PLUGIN_NAME=\"OS Packages\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"$installed\"" \
        "ATM_OS_NAME=\"$os_name\"" \
        "ATM_PACKAGE_MANAGER=\"$package_manager\"" \
        "ATM_PACKAGES=\"$packages\""
}

atm_os_packages_status() {
    local os_name=""
    local package_manager=""

    os_name="$(atm_os_packages_os_name 2>/dev/null || true)"
    package_manager="$(atm_os_packages_package_manager 2>/dev/null || true)"

    if [[ -n "$os_name" && -n "$package_manager" ]]; then
        printf '✅ %s / %s\n' "$os_name" "$package_manager"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_STATUS_NOT_SUPPORTED)"
    fi
}

atm_os_packages_install() {
    local os_name=""
    local package_manager=""
    local packages=""
    local update_command=""
    local install_command=""

    os_name="$(atm_os_packages_os_name)"
    package_manager="$(atm_os_packages_package_manager)"
    packages="$(atm_os_packages_packages_for_manager "$package_manager")"
    update_command="$(atm_os_packages_update_command "$package_manager")"
    install_command="$(atm_os_packages_install_command "$package_manager" "$packages")"

    [[ -n "$packages" ]] || atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_NO_PACKAGES): $package_manager"

    printf '%s %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_INSTALLING)" "$os_name"
    atm_os_packages_run_command "$update_command"
    atm_os_packages_run_command "$install_command"
    atm_os_packages_write_manifest "1"
    atm_success "$(atm_t ATM_PLUGIN_OS_PACKAGES_INSTALLED)"
}

atm_os_packages_use() {
    atm_warn "$(atm_t ATM_PLUGIN_OS_PACKAGES_USE_NOT_SUPPORTED)"
}

atm_os_packages_remove() {
    atm_os_packages_uninstall "$@"
}

atm_os_packages_uninstall() {
    local os_name=""
    local package_manager=""
    local packages=""
    local remove_command=""
    local answer=""

    os_name="$(atm_os_packages_os_name)"
    package_manager="$(atm_os_packages_package_manager)"
    packages="$(atm_os_packages_packages_for_manager "$package_manager")"
    remove_command="$(atm_os_packages_remove_command "$package_manager" "$packages")"

    [[ -n "$packages" ]] || atm_fail "$(atm_t ATM_PLUGIN_OS_PACKAGES_NO_PACKAGES): $package_manager"

    printf '%s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNINSTALL_WARNING)"
    printf '%s: %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_OS)" "$os_name"
    printf '%s: %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_MANAGER)" "$package_manager"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_OS_PACKAGES_CANCELLED)"
            return 0
            ;;
    esac

    printf '%s %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNINSTALLING)" "$os_name"
    atm_os_packages_run_command "$remove_command"
    atm_os_packages_write_manifest "0"
    atm_success "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNINSTALLED)"
}

atm_os_packages_menu() {
    local choice=""
    local current=""
    local os_name=""
    local package_manager=""

    while true; do
        clear
        current="$(atm_os_packages_status)"
        os_name="$(atm_os_packages_os_name 2>/dev/null || true)"
        package_manager="$(atm_os_packages_package_manager 2>/dev/null || true)"

        printf '%s\n' "=========================================="
        printf '    🧰 %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_CURRENT)" "$current"
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_OS)" "${os_name:-unknown}"
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_MANAGER)" "${package_manager:-unknown}"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_INSTALL)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_UNINSTALL)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_OS_PACKAGES_SHOW_COMMANDS)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_os_packages_install
                ;;
            2)
                atm_os_packages_uninstall
                ;;
            3)
                atm_os_packages_show_commands
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

atm_os_packages_show_commands() {
    local package_manager=""
    local packages=""

    package_manager="$(atm_os_packages_package_manager)"
    packages="$(atm_os_packages_packages_for_manager "$package_manager")"

    printf '%s\n' "$(atm_os_packages_update_command "$package_manager")"
    printf '%s\n' "$(atm_os_packages_install_command "$package_manager" "$packages")"
    printf '%s\n' "$(atm_os_packages_remove_command "$package_manager" "$packages")"
}

atm_os_packages_path_entries() {
    return 0
}

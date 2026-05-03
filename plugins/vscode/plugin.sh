#!/usr/bin/env bash

atm_vscode_normalize_version() {
    local version="$1"

    version="${version#v}"

    if [[ "$version" =~ ^[0-9]+\.[0-9]+$ ]]; then
        printf '%s.0\n' "$version"
    elif [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid VS Code version: $version. Expected format: 1.118 or 1.118.0"
    fi
}

atm_vscode_short_version() {
    local version="$1"
    local major=""
    local minor=""

    IFS='.' read -r major minor _ <<< "$version"

    if [[ -n "$major" && -n "$minor" ]]; then
        printf '%s.%s\n' "$major" "$minor"
    else
        printf '%s\n' "$version"
    fi
}

atm_vscode_archive_name() {
    local version="$1"
    local arch="${ATM_VSCODE_ARCH:-linux-x64}"

    printf 'vscode-%s-%s.tar.gz\n' "$version" "$arch"
}

atm_vscode_download_url() {
    local version="$1"
    local arch="${ATM_VSCODE_ARCH:-linux-x64}"
    local quality="${ATM_VSCODE_QUALITY:-stable}"

    printf 'https://update.code.visualstudio.com/%s/%s/%s\n' "$version" "$arch" "$quality"
}

atm_vscode_install_dir() {
    local version="$1"

    printf '%s/code_%s\n' "${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}" "$version"
}

atm_vscode_current_path() {
    printf '%s/current\n' "${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}"
}

atm_vscode_cache_dir() {
    printf '%s\n' "${ATM_VSCODE_CACHE_DIR:-$ATM_DOWNLOAD_DIR/vscode}"
}

atm_vscode_cache_file() {
    local version="$1"

    printf '%s/%s\n' "$(atm_vscode_cache_dir)" "$(atm_vscode_archive_name "$version")"
}

atm_vscode_manifest_file() {
    printf '%s\n' "${ATM_VSCODE_MANIFEST_FILE:-$ATM_MANIFEST_DIR/vscode.manifest}"
}

atm_vscode_version_from_args() {
    local version="${ATM_VSCODE_DEFAULT_VERSION:-1.11}"

    while (($# > 0)); do
        case "$1" in
            --version)
                version="${2:-}"
                [[ -n "$version" ]] || atm_fail "Missing value for --version"
                shift
                ;;
            --version=*)
                version="${1#--version=}"
                ;;
            *)
                ;;
        esac

        shift || true
    done

    atm_vscode_normalize_version "$version"
}

atm_vscode_read_package_version() {
    local vscode_home="$1"
    local package_json="$vscode_home/resources/app/package.json"

    [[ -f "$package_json" ]] || return 1

    sed -nE '/"version"[[:space:]]*:/ { s/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p; q; }' "$package_json"
}

atm_vscode_status() {
    local current=""
    local version=""
    local short_version=""

    current="$(atm_vscode_current_path)"

    if [[ -x "$current/bin/code" ]]; then
        version="$(atm_vscode_read_package_version "$current" 2>/dev/null || true)"

        if [[ -n "$version" ]]; then
            short_version="$(atm_vscode_short_version "$version")"
            printf '✅ v%s\n' "$short_version"
        else
            "$current/bin/code" --version 2>/dev/null | sed -n '1p'
        fi
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_VSCODE_STATUS_NOT_INSTALLED)"
    fi
}

atm_vscode_list_installed_versions() {
    local root="${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}"
    local path=""
    local version=""

    [[ -d "$root" ]] || return 0

    for path in "$root"/code_*; do
        [[ -d "$path" ]] || continue
        [[ -x "$path/bin/code" ]] || continue

        version="${path##*/code_}"
        printf '%s\n' "$version"
    done | sort -V
}

atm_vscode_current_version() {
    local current=""
    local resolved=""

    current="$(atm_vscode_current_path)"

    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1

    basename "$resolved" | sed 's/^code_//'
}

atm_vscode_resolved_install_dir() {
    local version="$1"
    local dest=""

    dest="$(atm_vscode_install_dir "$version")"

    if command -v readlink >/dev/null 2>&1 && [[ -e "$dest" ]]; then
        readlink -f "$dest"
    else
        printf '%s\n' "$dest"
    fi
}

atm_vscode_desktop_file_name() {
    printf 'code.desktop\n'
}

atm_vscode_url_handler_file_name() {
    printf 'code-url-handler.desktop\n'
}

atm_vscode_desktop_template_file() {
    printf '%s/vscode/code.desktop.in\n' "$ATM_PLUGIN_DIR"
}

atm_vscode_url_handler_template_file() {
    printf '%s/vscode/code-url-handler.desktop.in\n' "$ATM_PLUGIN_DIR"
}

atm_vscode_desktop_exec() {
    local vscode_home="$1"

    if [[ -x "$vscode_home/code" ]]; then
        printf '%s/code\n' "$vscode_home"
        return 0
    fi

    if [[ -x "$vscode_home/bin/code" ]]; then
        printf '%s/bin/code\n' "$vscode_home"
        return 0
    fi

    printf '%s/code\n' "$vscode_home"
}

atm_vscode_render_template() {
    local template_file="$1"
    local version="$2"
    local rendered_file=""
    local vscode_home=""
    local vscode_exec=""
    local vscode_icon=""
    local short_version=""

    [[ -f "$template_file" ]] || atm_fail "VS Code desktop template not found: $template_file"

    rendered_file="$(mktemp)"
    vscode_home="$(atm_vscode_resolved_install_dir "$version")"
    vscode_exec="$(atm_vscode_desktop_exec "$vscode_home")"
    vscode_icon="$vscode_home/resources/app/resources/linux/code.png"
    short_version="$(atm_vscode_short_version "$version")"

    sed \
        -e "s|__VSCODE_EXEC__|$vscode_exec|g" \
        -e "s|__VSCODE_ICON__|$vscode_icon|g" \
        -e "s|__VSCODE_SHORT_VERSION__|$short_version|g" \
        -e "s|__VSCODE_FULL_VERSION__|$version|g" \
        "$template_file" > "$rendered_file"

    printf '%s\n' "$rendered_file"
}

atm_vscode_render_desktop_file() {
    local version="$1"

    atm_vscode_render_template "$(atm_vscode_desktop_template_file)" "$version"
}

atm_vscode_render_url_handler_file() {
    local version="$1"

    atm_vscode_render_template "$(atm_vscode_url_handler_template_file)" "$version"
}

atm_vscode_register_url_handler() {
    local handler_file="$1"

    command -v xdg-mime >/dev/null 2>&1 || return 0

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: xdg-mime default %s x-scheme-handler/vscode\n' "$handler_file"
        return 0
    fi

    xdg-mime default "$handler_file" x-scheme-handler/vscode || true
}

atm_vscode_desktop() {
    local version="${1:-}"
    local rendered_desktop=""
    local target_desktop=""
    local rendered_url_handler=""
    local target_url_handler=""

    if [[ -z "$version" ]]; then
        version="$(atm_vscode_current_version 2>/dev/null || true)"
    fi

    if [[ -z "$version" ]]; then
        version="${ATM_VSCODE_DEFAULT_VERSION:-1.118.1}"
        version="$(atm_vscode_normalize_version "$version")"
    fi

    target_desktop="$(atm_vscode_desktop_file_name "$version")"
    rendered_desktop="$(atm_vscode_render_desktop_file "$version")"

    atm_desktop_install_file "$rendered_desktop" "$target_desktop"
    rm -f "$rendered_desktop"

    target_url_handler="$(atm_vscode_url_handler_file_name "$version")"
    rendered_url_handler="$(atm_vscode_render_url_handler_file "$version")"

    atm_desktop_install_file "$rendered_url_handler" "$target_url_handler"
    rm -f "$rendered_url_handler"

    atm_vscode_register_url_handler "$target_url_handler"
}

atm_vscode_create_desktop_entry() {
    local version="${1:-}"

    [[ "${ATM_VSCODE_CREATE_DESKTOP_ENTRY:-1}" == "1" ]] || return 0

    atm_vscode_desktop "$version"
}

atm_vscode_remove_desktop_entry() {
    local version="${1:-}"
    local target_desktop=""
    local target_url_handler=""

    [[ -n "$version" ]] || return 0

    target_desktop="$(atm_vscode_desktop_file_name "$version")"
    target_url_handler="$(atm_vscode_url_handler_file_name "$version")"

    if declare -F atm_desktop_remove_file >/dev/null 2>&1; then
        atm_desktop_remove_file "$target_desktop"
        atm_desktop_remove_file "$target_url_handler"
    fi
}

atm_vscode_write_manifest() {
    local current_version="${1:-}"
    local current_path=""
    local installed_versions=""
    local cli_link=""
    local desktop_file=""
    local url_handler_file=""

    current_path="$(atm_vscode_current_path)"
    installed_versions="$(atm_vscode_list_installed_versions | tr '\n' ' ' | sed 's/[[:space:]]*$//')"
    cli_link="${ATM_VSCODE_CLI_LINK:-$HOME/.local/bin/code}"
    desktop_file="$(atm_vscode_desktop_file_name "$current_version")"
    url_handler_file="$(atm_vscode_url_handler_file_name "$current_version")"

    atm_manifest_write "vscode" \
        "ATM_PLUGIN_NAME=\"VS Code\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_VERSION=\"$current_version\"" \
        "ATM_CURRENT_PATH=\"$current_path\"" \
        "ATM_INSTALL_ROOT=\"${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}\"" \
        "ATM_CLI_LINK=\"$cli_link\"" \
        "ATM_DESKTOP_FILE=\"$desktop_file\"" \
        "ATM_URL_HANDLER_FILE=\"$url_handler_file\"" \
        "ATM_INSTALLED_VERSIONS=\"$installed_versions\""
}

atm_vscode_create_cli_link() {
    local current_path=""
    local cli_link="${ATM_VSCODE_CLI_LINK:-$HOME/.local/bin/code}"

    [[ "${ATM_VSCODE_CREATE_CLI_LINK:-1}" == "1" ]] || return 0

    current_path="$(atm_vscode_current_path)"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: mkdir -p %s\n' "$(dirname "$cli_link")"
        printf 'DRY-RUN: ln -sfn %s/bin/code %s\n' "$current_path" "$cli_link"
        return 0
    fi

    mkdir -p "$(dirname "$cli_link")"
    ln -sfn "$current_path/bin/code" "$cli_link"
}

atm_vscode_extensions_file() {
    if [[ -n "${ATM_VSCODE_EXTENSIONS_FILE:-}" ]]; then
        printf '%s\n' "$ATM_VSCODE_EXTENSIONS_FILE"
    else
        printf '%s/vscode/extensions.txt\n' "$ATM_PLUGIN_DIR"
    fi
}

atm_vscode_install_extensions() {
    local dest="$1"
    local extensions_file=""
    local line=""

    extensions_file="$(atm_vscode_extensions_file)"

    [[ -f "$extensions_file" ]] || return 0

    printf '%s\n' "$(atm_t ATM_PLUGIN_VSCODE_INSTALLING_EXTENSIONS)"

    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "${line// }" ]] && continue

        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: %s/bin/code --install-extension %s\n' "$dest" "$line"
        else
            "$dest/bin/code" \
                --user-data-dir "$dest/data" \
                --extensions-dir "$dest/data/extensions" \
                --install-extension "$line" \
                --force
        fi
    done < "$extensions_file"
}

atm_vscode_install() {
    local version=""
    local short_version=""
    local url=""
    local cache_file=""
    local dest=""

    version="$(atm_vscode_version_from_args "$@")"
    short_version="$(atm_vscode_short_version "$version")"
    url="$(atm_vscode_download_url "$version")"
    cache_file="$(atm_vscode_cache_file "$version")"
    dest="$(atm_vscode_install_dir "$version")"

    printf '%s\n' "$(atm_t ATM_PLUGIN_VSCODE_INSTALLING) v$short_version"

    if [[ -x "$dest/bin/code" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_VSCODE_ALREADY_INSTALLED): $dest"
    else
        mkdir -p "$(atm_vscode_cache_dir)"
        mkdir -p "${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}"

        atm_download_file "$url" "$cache_file"

        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: install VS Code %s into %s\n' "$version" "$dest"
        else
            atm_archive_extract_tar_gz "$cache_file" "$dest" 1
            mkdir -p "$dest/data" "$dest/data/extensions"
        fi
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_vscode_current_path)"
    else
        ln -sfn "$dest" "$(atm_vscode_current_path)"
    fi

    atm_vscode_create_cli_link
    atm_vscode_install_extensions "$dest"
    atm_vscode_write_manifest "$version"

    atm_success "$(atm_t ATM_PLUGIN_VSCODE_INSTALLED): v$short_version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" && -x "$(atm_vscode_current_path)/bin/code" ]]; then
        "$(atm_vscode_current_path)/bin/code" --version | sed -n '1p'
    fi
}

atm_vscode_use() {
    local version="${1:-}"
    local dest=""
    local short_version=""

    [[ -n "$version" ]] || atm_fail "Usage: atm use vscode <version>"

    version="$(atm_vscode_normalize_version "$version")"
    short_version="$(atm_vscode_short_version "$version")"
    dest="$(atm_vscode_install_dir "$version")"

    [[ -x "$dest/bin/code" ]] || atm_fail "VS Code version is not installed: v$short_version"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_vscode_current_path)"
    else
        ln -sfn "$dest" "$(atm_vscode_current_path)"
    fi

    atm_vscode_create_cli_link
    atm_vscode_write_manifest "$version"

    if declare -F atm_vscode_desktop >/dev/null 2>&1; then
        atm_vscode_desktop "$version"
    fi

    atm_success "$(atm_t ATM_PLUGIN_VSCODE_USING): v$short_version"
}

atm_vscode_remove() {
    local version="${1:-}"
    local dest=""
    local current_version=""
    local short_version=""

    [[ -n "$version" ]] || atm_fail "Usage: atm remove vscode <version>"

    version="$(atm_vscode_normalize_version "$version")"
    short_version="$(atm_vscode_short_version "$version")"
    dest="$(atm_vscode_install_dir "$version")"
    current_version="$(atm_vscode_current_version 2>/dev/null || true)"

    [[ -d "$dest" ]] || atm_fail "VS Code version is not installed: v$short_version"

    if [[ "$current_version" == "$version" ]]; then
        atm_fail "Cannot remove current VS Code version: v$short_version. Switch version first with: atm use vscode <other-version>"
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$dest"
    else
        rm -rf "$dest"
    fi

    atm_vscode_write_manifest "$current_version"

    if [[ -n "$current_version" ]] && declare -F atm_vscode_desktop >/dev/null 2>&1; then
        atm_vscode_desktop "$current_version"
    fi

    atm_success "$(atm_t ATM_PLUGIN_VSCODE_REMOVED): v$short_version"
}

atm_vscode_uninstall() {
    local root="${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}"
    local cli_link="${ATM_VSCODE_CLI_LINK:-$HOME/.local/bin/code}"
    local answer=""
    local version=""

    printf '%s\n' "$(atm_t ATM_PLUGIN_VSCODE_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_VSCODE_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$cli_link"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_vscode_manifest_file)"
    else
        for version in $(atm_vscode_list_installed_versions); do
            atm_vscode_remove_desktop_entry "$version"
        done

        rm -rf "$root"

        if [[ -L "$cli_link" ]]; then
            rm -f "$cli_link"
        fi

        rm -f "$(atm_vscode_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_VSCODE_UNINSTALLED)"
}

atm_vscode_use_installed_menu() {
    local choice=""
    local idx=1
    local version=""
    local versions=()

    mapfile -t versions < <(atm_vscode_list_installed_versions)

    if [[ "${#versions[@]}" -eq 0 ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_VSCODE_NONE_INSTALLED)"
        return 0
    fi

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    💻 %s\n' "$(atm_t ATM_PLUGIN_VSCODE_USE_INSTALLED_TITLE)"
        printf '%s\n' "=========================================="

        idx=1

        for version in "${versions[@]}"; do
            printf '%s) VS Code %s\n' "$idx" "$version"
            idx=$((idx + 1))
        done

        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            b|B)
                return 0
                ;;
            q|Q)
                exit 0
                ;;
            ''|*[!0-9]*)
                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
            *)
                if [[ "$choice" -ge 1 && "$choice" -le "${#versions[@]}" ]]; then
                    version="${versions[$((choice - 1))]}"
                    atm_vscode_use "$version"
                    return 0
                fi

                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

atm_vscode_menu() {
    local choice=""
    local version=""
    local idx=1
    local install_count=0
    local versions=()
    local current=""
    local choose_option=0
    local use_option=0
    local list_option=0
    local remove_option=0
    local uninstall_option=0

    while true; do
        clear
        current="$(atm_vscode_status)"

        printf '%s\n' "=========================================="
        printf '    💻 %s\n' "$(atm_t ATM_PLUGIN_VSCODE_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_VSCODE_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"

        idx=1
        versions=()

        for version in ${ATM_VSCODE_VERSION_OPTIONS:-1.118.1 1.118.0 1.117.0 1.116.0 1.115.0 1.114.0 1.113.0}; do
            versions+=("$version")

            if [[ "$idx" == "1" ]]; then
                printf '%s) VS Code %s (%s)\n' "$idx" "$version" "$(atm_t ATM_PLUGIN_VSCODE_LATEST_STABLE)"
            else
                printf '%s) VS Code %s\n' "$idx" "$version"
            fi

            idx=$((idx + 1))
        done

        install_count="${#versions[@]}"

        choose_option="$idx"
        printf '%s) %s\n' "$choose_option" "$(atm_t ATM_PLUGIN_VSCODE_CHOOSE_VERSION)"
        idx=$((idx + 1))

        use_option="$idx"
        printf '%s) %s\n' "$use_option" "$(atm_t ATM_PLUGIN_VSCODE_USE_INSTALLED)"
        idx=$((idx + 1))

        list_option="$idx"
        printf '%s) %s\n' "$list_option" "$(atm_t ATM_PLUGIN_VSCODE_LIST_INSTALLED)"
        idx=$((idx + 1))

        remove_option="$idx"
        printf '%s) %s\n' "$remove_option" "$(atm_t ATM_PLUGIN_VSCODE_REMOVE_VERSION)"
        idx=$((idx + 1))

        uninstall_option="$idx"
        printf '%s) %s\n' "$uninstall_option" "$(atm_t ATM_PLUGIN_VSCODE_UNINSTALL)"

        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            b|B)
                return 0
                ;;
            q|Q)
                exit 0
                ;;
            ''|*[!0-9]*)
                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
            *)
                if [[ "$choice" -ge 1 && "$choice" -le "$install_count" ]]; then
                    version="${versions[$((choice - 1))]}"
                    atm_vscode_install --version "$version"

                elif [[ "$choice" -eq "$choose_option" ]]; then
                    printf '%s ' "$(atm_t ATM_PLUGIN_VSCODE_ENTER_VERSION)"
                    read -r version

                    [[ -n "$version" ]] || {
                        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                        continue
                    }

                    atm_vscode_install --version "$version"

                elif [[ "$choice" -eq "$use_option" ]]; then
                    atm_vscode_use_installed_menu

                elif [[ "$choice" -eq "$list_option" ]]; then
                    atm_vscode_list_installed_versions

                elif [[ "$choice" -eq "$remove_option" ]]; then
                    printf '%s ' "$(atm_t ATM_PLUGIN_VSCODE_ENTER_VERSION)"
                    read -r version

                    [[ -n "$version" ]] || {
                        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                        continue
                    }

                    atm_vscode_remove "$version"

                elif [[ "$choice" -eq "$uninstall_option" ]]; then
                    atm_vscode_uninstall

                else
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                fi
                ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

atm_vscode_path_entries() {
    printf '%s/current/bin\n' "${ATM_VSCODE_INSTALL_ROOT:-$ATM_APPS_DIR/vscode}"
    printf '%s\n' "$(dirname "${ATM_VSCODE_CLI_LINK:-$HOME/.local/bin/code}")"
}

#!/usr/bin/env bash

atm_flutter_archive_name() {
    local version="$1"
    local channel="${ATM_FLUTTER_CHANNEL:-stable}"

    printf 'flutter_linux_%s-%s.tar.xz\n' "$version" "$channel"
}

atm_flutter_download_url() {
    local version="$1"
    local channel="${ATM_FLUTTER_CHANNEL:-stable}"

    printf 'https://storage.googleapis.com/flutter_infra_release/releases/%s/linux/flutter_linux_%s-%s.tar.xz\n' \
        "$channel" \
        "$version" \
        "$channel"
}

atm_flutter_install_dir() {
    local version="$1"

    printf '%s/%s\n' "${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}" "$version"
}

atm_flutter_current_path() {
    printf '%s/current\n' "${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}"
}

atm_flutter_manifest_file() {
    printf '%s\n' "${ATM_FLUTTER_MANIFEST_FILE:-$ATM_MANIFEST_DIR/flutter.manifest}"
}

atm_flutter_cache_dir() {
    printf '%s\n' "${ATM_FLUTTER_CACHE_DIR:-$ATM_DOWNLOAD_DIR/flutter}"
}

atm_flutter_cache_file() {
    local version="$1"

    printf '%s/%s\n' "$(atm_flutter_cache_dir)" "$(atm_flutter_archive_name "$version")"
}

atm_flutter_normalize_version() {
    local version="$1"

    version="${version#flutter}"

    if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9._-]+)?$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid Flutter version: $version. Expected format: 3.41.8"
    fi
}

atm_flutter_version_from_args() {
    local version="${ATM_FLUTTER_DEFAULT_VERSION:-3.41.8}"

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

    atm_flutter_normalize_version "$version"
}

atm_flutter_status() {
    local current=""

    current="$(atm_flutter_current_path)"

    if [[ -x "$current/bin/flutter" ]]; then
    printf '✅ '
    "$current/bin/flutter" --version 2>/dev/null | sed -n '1p'
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_FLUTTER_STATUS_NOT_INSTALLED)"
    fi
}

atm_flutter_list_installed_versions() {
    local root="${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}"
    local path=""

    [[ -d "$root" ]] || return 0

    for path in "$root"/*; do
        [[ -d "$path" ]] || continue
        [[ "$(basename "$path")" == "current" ]] && continue
        [[ -x "$path/bin/flutter" ]] || continue
        basename "$path"
    done | sort -V
}

atm_flutter_current_version() {
    local current=""
    local resolved=""

    current="$(atm_flutter_current_path)"

    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1
    basename "$resolved"
}

atm_flutter_write_manifest() {
    local current_version="${1:-}"
    local current_path=""
    local installed_versions=""

    current_path="$(atm_flutter_current_path)"
    installed_versions="$(atm_flutter_list_installed_versions | tr '\n' ' ' | sed 's/[[:space:]]*$//')"

    atm_manifest_write "flutter" \
        "ATM_PLUGIN_NAME=\"Flutter SDK\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_VERSION=\"$current_version\"" \
        "ATM_CURRENT_PATH=\"$current_path\"" \
        "ATM_INSTALL_ROOT=\"${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}\"" \
        "ATM_CHANNEL=\"${ATM_FLUTTER_CHANNEL:-stable}\"" \
        "ATM_INSTALLED_VERSIONS=\"$installed_versions\""
}

atm_flutter_install() {
    local version=""
    local url=""
    local cache_file=""
    local dest=""
    local tmp_extract=""

    version="$(atm_flutter_version_from_args "$@")"
    url="$(atm_flutter_download_url "$version")"
    cache_file="$(atm_flutter_cache_file "$version")"
    dest="$(atm_flutter_install_dir "$version")"

    printf '%s\n' "$(atm_t ATM_PLUGIN_FLUTTER_INSTALLING) $version"

    if [[ -x "$dest/bin/flutter" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_FLUTTER_ALREADY_INSTALLED): $dest"
    else
        mkdir -p "$(atm_flutter_cache_dir)"
        mkdir -p "${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}"

        atm_download_file "$url" "$cache_file"

        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: install Flutter %s into %s\n' "$version" "$dest"
        else
            tmp_extract="${dest}.tmp.$$"
            rm -rf "$tmp_extract"
            mkdir -p "$tmp_extract"

            tar -xJf "$cache_file" -C "$tmp_extract"

            [[ -d "$tmp_extract/flutter" ]] || atm_fail "Unexpected Flutter archive structure: $cache_file"

            rm -rf "$dest"
            mv "$tmp_extract/flutter" "$dest"
            rm -rf "$tmp_extract"
        fi
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_flutter_current_path)"
    else
        ln -sfn "$dest" "$(atm_flutter_current_path)"
    fi

    atm_flutter_write_manifest "$version"

    atm_success "$(atm_t ATM_PLUGIN_FLUTTER_INSTALLED): $version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" && -x "$(atm_flutter_current_path)/bin/flutter" ]]; then
        "$(atm_flutter_current_path)/bin/flutter" --version
    fi
}

atm_flutter_use() {
    local version="${1:-}"
    local dest=""

    [[ -n "$version" ]] || atm_fail "Usage: atm use flutter <version>"

    version="$(atm_flutter_normalize_version "$version")"
    dest="$(atm_flutter_install_dir "$version")"

    [[ -x "$dest/bin/flutter" ]] || atm_fail "Flutter version is not installed: $version"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_flutter_current_path)"
    else
        ln -sfn "$dest" "$(atm_flutter_current_path)"
    fi

    atm_flutter_write_manifest "$version"

    atm_success "$(atm_t ATM_PLUGIN_FLUTTER_USING): $version"
}

atm_flutter_remove() {
    local version="${1:-}"
    local dest=""
    local current_version=""

    [[ -n "$version" ]] || atm_fail "Usage: atm remove flutter <version>"

    version="$(atm_flutter_normalize_version "$version")"
    dest="$(atm_flutter_install_dir "$version")"
    current_version="$(atm_flutter_current_version 2>/dev/null || true)"

    [[ -d "$dest" ]] || atm_fail "Flutter version is not installed: $version"

    if [[ "$current_version" == "$version" ]]; then
        atm_fail "Cannot remove current Flutter version: $version. Switch version first with: atm use flutter <other-version>"
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$dest"
    else
        rm -rf "$dest"
    fi

    atm_flutter_write_manifest "$current_version"

    atm_success "$(atm_t ATM_PLUGIN_FLUTTER_REMOVED): $version"
}

atm_flutter_uninstall() {
    local root="${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}"
    local answer=""

    printf '%s\n' "$(atm_t ATM_PLUGIN_FLUTTER_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_FLUTTER_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_flutter_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_flutter_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_FLUTTER_UNINSTALLED)"
}

atm_flutter_menu() {
    local choice=""
    local version=""
    local idx=1
    local versions=()
    local current=""

    while true; do
        clear
        current="$(atm_flutter_status)"

        printf '%s\n' "=========================================="
        printf '    🦋 %s\n' "$(atm_t ATM_PLUGIN_FLUTTER_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_FLUTTER_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"

        idx=1
        versions=()

        for version in ${ATM_FLUTTER_VERSION_OPTIONS:-3.41.8 3.41.5 3.41.0 3.38.0 3.35.0}; do
            versions+=("$version")
            if [[ "$idx" == "1" ]]; then
                printf '%s) Flutter %s (%s)\n' "$idx" "$version" "$(atm_t ATM_PLUGIN_FLUTTER_LATEST_STABLE)"
            else
                printf '%s) Flutter %s\n' "$idx" "$version"
            fi
            idx=$((idx + 1))
        done

        printf '6) %s\n' "$(atm_t ATM_PLUGIN_FLUTTER_CHOOSE_VERSION)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_FLUTTER_LIST_INSTALLED)"
        printf '8) %s\n' "$(atm_t ATM_PLUGIN_FLUTTER_REMOVE_VERSION)"
        printf '9) %s\n' "$(atm_t ATM_PLUGIN_FLUTTER_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1|2|3|4|5)
                version="${versions[$((choice - 1))]}"
                atm_flutter_install --version "$version"
                ;;
            6)
                printf '%s ' "$(atm_t ATM_PLUGIN_FLUTTER_ENTER_VERSION)"
                read -r version
                [[ -n "$version" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }
                atm_flutter_install --version "$version"
                ;;
            7)
                atm_flutter_list_installed_versions
                ;;
            8)
                printf '%s ' "$(atm_t ATM_PLUGIN_FLUTTER_ENTER_VERSION)"
                read -r version
                [[ -n "$version" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }
                atm_flutter_remove "$version"
                ;;
            9)
                atm_flutter_uninstall
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

atm_flutter_path_entries() {
    printf '%s/current/bin\n' "${ATM_FLUTTER_INSTALL_ROOT:-$ATM_APPS_DIR/flutter}"
}
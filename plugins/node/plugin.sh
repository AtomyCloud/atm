#!/usr/bin/env bash

atm_node_install_root() {
    printf '%s\n' "${ATM_NODE_INSTALL_ROOT:-$ATM_APPS_DIR/node}"
}

atm_node_cache_dir() {
    printf '%s\n' "${ATM_NODE_CACHE_DIR:-$ATM_DOWNLOAD_DIR/node}"
}

atm_node_manifest_file() {
    printf '%s\n' "${ATM_NODE_MANIFEST_FILE:-$ATM_MANIFEST_DIR/node.manifest}"
}

atm_node_current_path() {
    printf '%s/current\n' "$(atm_node_install_root)"
}

atm_node_platform() {
    local os_name=""
    local arch_name=""

    case "$(uname -s)" in
        Linux) os_name="linux" ;;
        Darwin) os_name="darwin" ;;
        FreeBSD) os_name="freebsd" ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_NODE_UNSUPPORTED_PLATFORM)" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64) arch_name="x64" ;;
        aarch64|arm64) arch_name="arm64" ;;
        armv7l) arch_name="armv7l" ;;
        ppc64le) arch_name="ppc64le" ;;
        s390x) arch_name="s390x" ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_NODE_UNSUPPORTED_PLATFORM)" ;;
    esac

    printf '%s-%s\n' "$os_name" "$arch_name"
}

atm_node_normalize_version() {
    local version="$1"

    version="${version#node-}"
    version="${version#v}"

    if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid Node.js version: $version. Expected format: 26.2.0"
    fi
}

atm_node_major_from_version() {
    local version="$1"
    version="$(atm_node_normalize_version "$version")"
    printf '%s\n' "${version%%.*}"
}

atm_node_archive_name() {
    local version="$1"
    local platform=""

    version="$(atm_node_normalize_version "$version")"
    platform="$(atm_node_platform)"

    printf 'node-v%s-%s.tar.xz\n' "$version" "$platform"
}

atm_node_download_url() {
    local version="$1"
    local archive=""
    local base="${ATM_NODE_DIST_BASE_URL:-https://nodejs.org/dist}"

    version="$(atm_node_normalize_version "$version")"
    archive="$(atm_node_archive_name "$version")"

    printf '%s/v%s/%s\n' "$base" "$version" "$archive"
}

atm_node_install_dir() {
    local version="$1"
    version="$(atm_node_normalize_version "$version")"
    printf '%s/%s\n' "$(atm_node_install_root)" "$version"
}

atm_node_cache_file() {
    local version="$1"
    printf '%s/%s\n' "$(atm_node_cache_dir)" "$(atm_node_archive_name "$version")"
}

atm_node_status() {
    local current=""
    local version_output=""

    current="$(atm_node_current_path)"

    if [[ -x "$current/bin/node" ]]; then
        version_output="$("$current/bin/node" --version 2>/dev/null || true)"
        printf '✅ %s\n' "${version_output#v}"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_NODE_STATUS_NOT_INSTALLED)"
    fi
}

atm_node_list_installed_versions() {
    local root=""
    local path=""

    root="$(atm_node_install_root)"
    [[ -d "$root" ]] || return 0

    for path in "$root"/*; do
        [[ -d "$path" ]] || continue
        [[ "$(basename "$path")" == "current" ]] && continue
        [[ "$(basename "$path")" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
        [[ -x "$path/bin/node" ]] || continue
        basename "$path"
    done | sort -V
}

atm_node_current_version() {
    local current=""
    local resolved=""

    current="$(atm_node_current_path)"
    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1
    basename "$resolved"
}

atm_node_versions_from_index() {
    local major="$1"
    local index_url="${ATM_NODE_INDEX_URL:-https://nodejs.org/dist/index.json}"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        return 1
    fi

    atm_require_commands curl grep sed

    curl -fsSL "$index_url" \
        | grep -o '"version"[[:space:]]*:[[:space:]]*"v'"$major"'\.[^"]*"' \
        | sed 's/.*"v//; s/"$//' \
        | sed -n '1,3p'
}

atm_node_fallback_versions() {
    local major="$1"
    local value=""

    case "$major" in
        26) value="${ATM_NODE_FALLBACK_VERSIONS_26:-26.2.0}" ;;
        25) value="${ATM_NODE_FALLBACK_VERSIONS_25:-25.9.0}" ;;
        24) value="${ATM_NODE_FALLBACK_VERSIONS_24:-24.16.0}" ;;
        *) atm_fail "Unsupported Node.js major: $major" ;;
    esac

    printf '%s\n' $value
}

atm_node_latest_versions() {
    local major="$1"
    local versions=()

    mapfile -t versions < <(atm_node_versions_from_index "$major" || true)

    if ((${#versions[@]} == 0)); then
        mapfile -t versions < <(atm_node_fallback_versions "$major")
    fi

    printf '%s\n' "${versions[@]}"
}

atm_node_write_manifest() {
    local current_version="${1:-}"
    local installed_versions=""

    installed_versions="$(atm_node_list_installed_versions | tr '\n' ' ' | sed 's/[[:space:]]*$//')"

    atm_manifest_write "node" \
        "ATM_PLUGIN_NAME=Node.js" \
        "ATM_PLUGIN_VERSION=0.0.1" \
        "ATM_INSTALLED=1" \
        "ATM_CURRENT_VERSION=$current_version" \
        "ATM_CURRENT_PATH=$(atm_node_current_path)" \
        "ATM_INSTALL_ROOT=$(atm_node_install_root)" \
        "ATM_INSTALLED_VERSIONS=$installed_versions"
}

atm_node_extract_archive() {
    local cache_file="$1"
    local dest="$2"
    local root_dir=""
    local tmp_extract=""

    tmp_extract="${dest}.tmp.$$"

    rm -rf "$tmp_extract"
    mkdir -p "$tmp_extract"

    root_dir="$(tar -tf "$cache_file" | sed -n '1p' | cut -d/ -f1)"
    [[ -n "$root_dir" ]] || atm_fail "Could not detect Node.js archive root: $cache_file"

    tar -xJf "$cache_file" -C "$tmp_extract"

    [[ -d "$tmp_extract/$root_dir" ]] || atm_fail "Unexpected Node.js archive structure: $cache_file"

    rm -rf "$dest"
    mv "$tmp_extract/$root_dir" "$dest"
    rm -rf "$tmp_extract"
}

atm_node_version_from_args() {
    local version="${ATM_NODE_DEFAULT_VERSION:-24.16.0}"

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
            --major)
                local major="${2:-}"
                [[ -n "$major" ]] || atm_fail "Missing value for --major"
                version="$(atm_node_latest_versions "$major" | sed -n '1p')"
                shift
                ;;
            --major=*)
                version="$(atm_node_latest_versions "${1#--major=}" | sed -n '1p')"
                ;;
            *)
                ;;
        esac
        shift || true
    done

    atm_node_normalize_version "$version"
}

atm_node_install() {
    local version=""
    local url=""
    local cache_file=""
    local dest=""

    version="$(atm_node_version_from_args "$@")"
    url="$(atm_node_download_url "$version")"
    cache_file="$(atm_node_cache_file "$version")"
    dest="$(atm_node_install_dir "$version")"

    printf '%s %s\n' "$(atm_t ATM_PLUGIN_NODE_INSTALLING)" "$version"

    if [[ -x "$dest/bin/node" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_NODE_ALREADY_INSTALLED): $dest"
    else
        atm_run mkdir -p "$(atm_node_cache_dir)" "$(atm_node_install_root)"
        atm_download_file "$url" "$cache_file"

        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: install Node.js %s into %s\n' "$version" "$dest"
        else
            atm_require_commands tar xz
            atm_node_extract_archive "$cache_file" "$dest"
        fi
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_node_current_path)"
    else
        ln -sfn "$dest" "$(atm_node_current_path)"
    fi

    atm_node_write_manifest "$version"
    atm_success "$(atm_t ATM_PLUGIN_NODE_INSTALLED): $version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" && -x "$(atm_node_current_path)/bin/node" ]]; then
        "$(atm_node_current_path)/bin/node" --version
        "$(atm_node_current_path)/bin/npm" --version 2>/dev/null || true
    fi
}

atm_node_use() {
    local version="${1:-}"
    local dest=""

    [[ -n "$version" ]] || atm_fail "Usage: atm use node <version>"
    version="$(atm_node_normalize_version "$version")"
    dest="$(atm_node_install_dir "$version")"

    [[ -x "$dest/bin/node" ]] || atm_fail "Node.js version is not installed: $version"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_node_current_path)"
    else
        ln -sfn "$dest" "$(atm_node_current_path)"
    fi

    atm_node_write_manifest "$version"
    atm_success "$(atm_t ATM_PLUGIN_NODE_USING): $version"
}

atm_node_remove() {
    local version="${1:-}"
    local dest=""
    local current_version=""

    [[ -n "$version" ]] || atm_fail "Usage: atm remove node <version>"
    version="$(atm_node_normalize_version "$version")"
    dest="$(atm_node_install_dir "$version")"
    current_version="$(atm_node_current_version 2>/dev/null || true)"

    [[ -d "$dest" ]] || atm_fail "Node.js version is not installed: $version"

    if [[ "$current_version" == "$version" ]]; then
        atm_fail "Cannot remove current Node.js version: $version. Switch version first with: atm use node <other-version>"
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$dest"
    else
        rm -rf "$dest"
    fi

    atm_node_write_manifest "$current_version"
    atm_success "$(atm_t ATM_PLUGIN_NODE_REMOVED): $version"
}

atm_node_uninstall() {
    local root=""
    local answer=""

    root="$(atm_node_install_root)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_NODE_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES) ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_NODE_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_node_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_node_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_NODE_UNINSTALLED)"
}

atm_node_pick_installed_version() {
    local prompt="$1"
    local versions=()
    local idx=1
    local choice=""
    local version=""

    mapfile -t versions < <(atm_node_list_installed_versions)

    if ((${#versions[@]} == 0)); then
        atm_warn "$(atm_t ATM_PLUGIN_NODE_NO_INSTALLED)"
        return 1
    fi

    printf '%s\n' "$prompt"
    for version in "${versions[@]}"; do
        printf '%s) Node.js %s\n' "$idx" "$version"
        idx=$((idx + 1))
    done

    printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
    read -r choice

    if [[ "$choice" =~ ^[0-9]+$ ]] && ((choice >= 1 && choice <= ${#versions[@]})); then
        printf '%s\n' "${versions[$((choice - 1))]}"
        return 0
    fi

    if [[ -n "$choice" ]]; then
        atm_node_normalize_version "$choice"
        return 0
    fi

    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
    return 1
}

atm_node_major_menu() {
    local major="$1"
    local label="$2"
    local choice=""
    local idx=1
    local version=""
    local versions=()

    while true; do
        clear
        mapfile -t versions < <(atm_node_latest_versions "$major")

        printf '%s\n' "=========================================="
        printf '    🟢 %s %s\n' "Node.js" "$label"
        printf '%s\n' "=========================================="
        printf '%s\n' "$(atm_t ATM_PLUGIN_NODE_SOURCE_OFFICIAL)"
        printf '%s\n' "https://nodejs.org/dist/index.json"
        printf '%s\n' "------------------------------------------"

        idx=1
        for version in "${versions[@]}"; do
            printf '%s) Install Node.js %s\n' "$idx" "$version"
            idx=$((idx + 1))
        done

        printf 'c) %s\n' "$(atm_t ATM_PLUGIN_NODE_CHANGE_VERSION)"
        printf 'r) %s\n' "$(atm_t ATM_PLUGIN_NODE_REMOVE_VERSION)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1|2|3)
                if [[ -n "${versions[$((choice - 1))]:-}" ]]; then
                    atm_node_install --version "${versions[$((choice - 1))]}"
                else
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                fi
                ;;
            c|C)
                version="$(atm_node_pick_installed_version "$(atm_t ATM_PLUGIN_NODE_SELECT_VERSION_TO_USE)" || true)"
                [[ -n "$version" ]] && atm_node_use "$version"
                ;;
            r|R)
                version="$(atm_node_pick_installed_version "$(atm_t ATM_PLUGIN_NODE_SELECT_VERSION_TO_REMOVE)" || true)"
                [[ -n "$version" ]] && atm_node_remove "$version"
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

atm_node_menu() {
    local choice=""
    local current=""
    local version=""

    while true; do
        clear
        current="$(atm_node_status)"

        printf '%s\n' "=========================================="
        printf '    🟢 %s\n' "$(atm_t ATM_PLUGIN_NODE_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_NODE_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"
        printf '1) Node.js v26.x\n'
        printf '2) Node.js v25.x\n'
        printf '3) Node.js v24.x (LTS)\n'
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_NODE_LIST_INSTALLED)"
        printf '5) %s\n' "$(atm_t ATM_PLUGIN_NODE_CHANGE_VERSION)"
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_NODE_REMOVE_VERSION)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_NODE_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_node_major_menu "26" "v26.x"
                ;;
            2)
                atm_node_major_menu "25" "v25.x"
                ;;
            3)
                atm_node_major_menu "24" "v24.x (LTS)"
                ;;
            4)
                atm_node_list_installed_versions
                ;;
            5)
                version="$(atm_node_pick_installed_version "$(atm_t ATM_PLUGIN_NODE_SELECT_VERSION_TO_USE)" || true)"
                [[ -n "$version" ]] && atm_node_use "$version"
                ;;
            6)
                version="$(atm_node_pick_installed_version "$(atm_t ATM_PLUGIN_NODE_SELECT_VERSION_TO_REMOVE)" || true)"
                [[ -n "$version" ]] && atm_node_remove "$version"
                ;;
            7)
                atm_node_uninstall
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

atm_node_path_entries() {
    printf '%s/current/bin\n' "$(atm_node_install_root)"
}

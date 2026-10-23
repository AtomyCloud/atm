#!/usr/bin/env bash

atm_go_archive_name() {
    local version="$1"
    printf 'go%s.%s.tar.gz\n' "$version" "${ATM_GO_ARCH:-linux-amd64}"
}

atm_go_download_url() {
    local version="$1"
    printf 'https://go.dev/dl/go%s.%s.tar.gz\n' "$version" "${ATM_GO_ARCH:-linux-amd64}"
}

atm_go_install_dir() {
    local version="$1"
    printf '%s/%s\n' "${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}" "$version"
}

atm_go_current_path() {
    printf '%s/current\n' "${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}"
}

atm_go_workspace() {
    printf '%s\n' "${ATM_GO_WORKSPACE:-$ATM_APPS_DIR/go/workspace}"
}

atm_go_manifest_file() {
    printf '%s\n' "${ATM_GO_MANIFEST_FILE:-$ATM_MANIFEST_DIR/go.manifest}"
}

atm_go_cache_dir() {
    printf '%s\n' "${ATM_GO_CACHE_DIR:-$ATM_DOWNLOAD_DIR/go}"
}

atm_go_cache_file() {
    local version="$1"
    printf '%s/%s\n' "$(atm_go_cache_dir)" "$(atm_go_archive_name "$version")"
}

atm_go_normalize_version() {
    local version="$1"

    version="${version#go}"

    if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid Go version: $version. Expected format: 1.26.2"
    fi
}

atm_go_version_from_args() {
    local version="${ATM_GO_DEFAULT_VERSION:-1.26.2}"

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

    atm_go_normalize_version "$version"
}

atm_go_versions_from_api() {
    local releases_url="${ATM_GO_RELEASES_URL:-https://go.dev/dl/?mode=json&include=all}"
    local response=""

    [[ "${ATM_DRY_RUN:-0}" != "1" ]] || return 1
    command -v curl >/dev/null 2>&1 || return 1
    command -v jq >/dev/null 2>&1 || return 1

    response="$(curl -fsSL --connect-timeout 5 --max-time 15 "$releases_url" 2>/dev/null)" || return 1

    jq -er '
        [.[] | select(.stable == true) | .version
         | select(test("^go[0-9]+\\.[0-9]+\\.[0-9]+$")) | ltrimstr("go")]
        | unique | sort_by(split(".") | map(tonumber)) | reverse | .[:5][]
    ' <<< "$response" 2>/dev/null
}

atm_go_latest_versions() {
    local releases=""
    local version=""
    local fallback=()

    if releases="$(atm_go_versions_from_api)" && [[ -n "$releases" ]]; then
        printf '%s\n' "$releases"
        return 0
    fi

    read -r -a fallback <<< "${ATM_GO_VERSION_OPTIONS:-1.26.2 1.26.1 1.26.0 1.25.9 1.25.5}"
    for version in "${fallback[@]}"; do
        version="${version#go}"
        [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
        printf '%s\n' "$version"
    done | sort -Vru | sed -n '1,5p'
}

atm_go_status() {
    local current=""

    current="$(atm_go_current_path)"

    if [[ -x "$current/bin/go" ]]; then
    printf '✅ '
    "$current/bin/go" version 2>/dev/null | sed 's/^go version //'
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_GO_STATUS_NOT_INSTALLED)"
    fi
}

atm_go_list_installed_versions() {
    local root="${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}"
    local path=""

    [[ -d "$root" ]] || return 0

    for path in "$root"/*; do
        [[ -d "$path" ]] || continue
        [[ "$(basename "$path")" == "current" ]] && continue
        [[ "$(basename "$path")" == "workspace" ]] && continue
        [[ -x "$path/bin/go" ]] || continue

        basename "$path"
    done | sort -V
}

atm_go_current_version() {
    local current=""
    local resolved=""

    current="$(atm_go_current_path)"

    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1

    basename "$resolved"
}

atm_go_write_manifest() {
    local current_version="${1:-}"
    local current_path=""
    local workspace=""
    local installed_versions=""

    current_path="$(atm_go_current_path)"
    workspace="$(atm_go_workspace)"
    installed_versions="$(atm_go_list_installed_versions | tr '\n' ' ' | sed 's/[[:space:]]*$//')"

    atm_manifest_write "go" \
        "ATM_PLUGIN_NAME=\"Go\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_VERSION=\"$current_version\"" \
        "ATM_CURRENT_PATH=\"$current_path\"" \
        "ATM_INSTALL_ROOT=\"${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}\"" \
        "ATM_WORKSPACE=\"$workspace\"" \
        "ATM_INSTALLED_VERSIONS=\"$installed_versions\""
}

atm_go_extract_archive() {
    local cache_file="$1"
    local dest="$2"
    local tmp_extract=""

    tmp_extract="${dest}.tmp.$$"

    rm -rf "$tmp_extract"
    mkdir -p "$tmp_extract"

    tar -xzf "$cache_file" -C "$tmp_extract"

    [[ -d "$tmp_extract/go" ]] || atm_fail "Unexpected Go archive structure: $cache_file"

    rm -rf "$dest"
    mv "$tmp_extract/go" "$dest"
    rm -rf "$tmp_extract"
}

atm_go_install() {
    local version=""
    local url=""
    local cache_file=""
    local dest=""
    local workspace=""

    version="$(atm_go_version_from_args "$@")"
    url="$(atm_go_download_url "$version")"
    cache_file="$(atm_go_cache_file "$version")"
    dest="$(atm_go_install_dir "$version")"
    workspace="$(atm_go_workspace)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_GO_INSTALLING) $version"

    if [[ -x "$dest/bin/go" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_GO_ALREADY_INSTALLED): $dest"
    else
        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: download %s to %s\n' "$url" "$cache_file"
            printf 'DRY-RUN: install Go %s into %s\n' "$version" "$dest"
        else
            mkdir -p "$(atm_go_cache_dir)"
            mkdir -p "${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}"
            atm_download_file "$url" "$cache_file"
            atm_go_extract_archive "$cache_file" "$dest"
        fi
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_go_current_path)"
        printf 'DRY-RUN: mkdir -p %s/bin %s/pkg %s/src\n' "$workspace" "$workspace" "$workspace"
    else
        ln -sfn "$dest" "$(atm_go_current_path)"
        mkdir -p "$workspace/bin" "$workspace/pkg" "$workspace/src"
    fi

    atm_go_write_manifest "$version"

    atm_success "$(atm_t ATM_PLUGIN_GO_INSTALLED): $version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" && -x "$(atm_go_current_path)/bin/go" ]]; then
        "$(atm_go_current_path)/bin/go" version
    fi
}

atm_go_use() {
    local version="${1:-}"
    local dest=""

    [[ -n "$version" ]] || atm_fail "Usage: atm use go <version>"

    version="$(atm_go_normalize_version "$version")"
    dest="$(atm_go_install_dir "$version")"

    [[ -x "$dest/bin/go" ]] || atm_fail "Go version is not installed: $version"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_go_current_path)"
    else
        ln -sfn "$dest" "$(atm_go_current_path)"
    fi

    atm_go_write_manifest "$version"

    atm_success "$(atm_t ATM_PLUGIN_GO_USING): $version"
}

atm_go_remove() {
    local version="${1:-}"
    local dest=""
    local current_version=""

    [[ -n "$version" ]] || atm_fail "Usage: atm remove go <version>"

    version="$(atm_go_normalize_version "$version")"
    dest="$(atm_go_install_dir "$version")"
    current_version="$(atm_go_current_version 2>/dev/null || true)"

    [[ -d "$dest" ]] || atm_fail "Go version is not installed: $version"

    if [[ "$current_version" == "$version" ]]; then
        atm_fail "Cannot remove current Go version: $version. Switch version first with: atm use go <other-version>"
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$dest"
    else
        rm -rf "$dest"
    fi

    atm_go_write_manifest "$current_version"

    atm_success "$(atm_t ATM_PLUGIN_GO_REMOVED): $version"
}

atm_go_uninstall() {
    local root="${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}"
    local answer=""

    printf '%s\n' "$(atm_t ATM_PLUGIN_GO_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_GO_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_go_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_go_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_GO_UNINSTALLED)"
}

atm_go_menu() {
    local choice=""
    local version=""
    local idx=1
    local versions=()
    local current=""
    local choose_option=""
    local list_option=""
    local remove_option=""
    local uninstall_option=""

    mapfile -t versions < <(atm_go_latest_versions)

    while true; do
        clear
        current="$(atm_go_status)"

        printf '%s\n' "=========================================="
        printf '    🐹 %s\n' "$(atm_t ATM_PLUGIN_GO_MENU_TITLE)"
        printf '    https://go.dev/\n'
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_GO_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"

        idx=1

        for version in "${versions[@]}"; do
            if [[ "$idx" == "1" ]]; then
                printf '%s) Go %s (%s)\n' "$idx" "$version" "$(atm_t ATM_PLUGIN_GO_LATEST_STABLE)"
            else
                printf '%s) Go %s\n' "$idx" "$version"
            fi

            idx=$((idx + 1))
        done

        choose_option="$idx"
        list_option="$((idx + 1))"
        remove_option="$((idx + 2))"
        uninstall_option="$((idx + 3))"
        printf '%s) %s\n' "$choose_option" "$(atm_t ATM_PLUGIN_GO_CHOOSE_VERSION)"
        printf '%s) %s\n' "$list_option" "$(atm_t ATM_PLUGIN_GO_LIST_INSTALLED)"
        printf '%s) %s\n' "$remove_option" "$(atm_t ATM_PLUGIN_GO_REMOVE_VERSION)"
        printf '%s) %s\n' "$uninstall_option" "$(atm_t ATM_PLUGIN_GO_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            "$choose_option")
                printf '%s ' "$(atm_t ATM_PLUGIN_GO_ENTER_VERSION)"
                read -r version

                [[ -n "$version" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }

                atm_go_install --version "$version"
                ;;
            "$list_option")
                atm_go_list_installed_versions
                ;;
            "$remove_option")
                printf '%s ' "$(atm_t ATM_PLUGIN_GO_ENTER_VERSION)"
                read -r version

                [[ -n "$version" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }

                atm_go_remove "$version"
                ;;
            "$uninstall_option")
                atm_go_uninstall
                ;;
            b|B)
                return 0
                ;;
            q|Q)
                exit 0
                ;;
            *)
                if [[ "$choice" =~ ^[1-5]$ ]] && ((choice <= ${#versions[@]})); then
                    version="${versions[$((choice - 1))]}"
                    atm_go_install --version "$version"
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

atm_go_path_entries() {
    printf '%s/current/bin\n' "${ATM_GO_INSTALL_ROOT:-$ATM_APPS_DIR/go}"
    printf '%s/bin\n' "${ATM_GO_WORKSPACE:-$ATM_APPS_DIR/go/workspace}"
}

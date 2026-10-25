#!/usr/bin/env bash

# The loader applies a translated locale afterwards when one is available.
source "${BASH_SOURCE[0]%/*}/lang/en-us.lang"

atm_spotatui_normalize_version() {
    local version="${1:-}"
    version="${version#v}"
    [[ "$version" == latest || "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
        || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_INVALID)"; return 1; }
    printf '%s\n' "$version"
}

atm_spotatui_version_from_args() {
    local version="$ATM_SPOTATUI_DEFAULT_VERSION"
    while (($#)); do
        case "$1" in
            --version) (($# >= 2)) || return 1; version="$2"; shift ;;
            --version=*) version="${1#*=}" ;;
            *) atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_INVALID)"; return 1 ;;
        esac
        shift
    done
    atm_spotatui_normalize_version "$version"
}

atm_spotatui_platform() {
    [[ "$(uname -s)" == Linux ]] || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_PLATFORM)"; return 1; }
    case "$(uname -m)" in
        x86_64|amd64) printf 'linux-x86_64\n' ;;
        aarch64|arm64) printf 'linux-aarch64\n' ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_PLATFORM)"; return 1 ;;
    esac
}

atm_spotatui_install_dir() { printf '%s/%s\n' "$ATM_SPOTATUI_INSTALL_ROOT" "$1"; }
atm_spotatui_current_path() { printf '%s/current\n' "$ATM_SPOTATUI_INSTALL_ROOT"; }
atm_spotatui_cache_dir() { printf '%s\n' "$ATM_SPOTATUI_CACHE_DIR"; }
atm_spotatui_manifest_file() { printf '%s\n' "$ATM_SPOTATUI_MANIFEST_FILE"; }
atm_spotatui_archive_name() { printf 'spotatui-%s.tar.gz\n' "$(atm_spotatui_platform)"; }
atm_spotatui_download_url() { printf 'https://github.com/LargeModGames/spotatui/releases/download/v%s/%s\n' "$1" "$(atm_spotatui_archive_name)"; }
atm_spotatui_cache_file() { printf '%s/%s/%s\n' "$ATM_SPOTATUI_CACHE_DIR" "$1" "$(atm_spotatui_archive_name)"; }

atm_spotatui_managed_version() {
    local version="${1:-}" dir=""
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
    dir="$(atm_spotatui_install_dir "$version")"
    [[ -d "$dir" && ! -L "$dir" && -f "$dir/.atm-spotatui-managed" && ! -L "$dir/.atm-spotatui-managed" \
        && -x "$dir/bin/spotatui" && ! -L "$dir/bin/spotatui" && -x "$dir/libexec/spotatui" && ! -L "$dir/libexec/spotatui" ]]
}

atm_spotatui_current_version() {
    local version="" current="$(atm_spotatui_current_path)"
    [[ -L "$current" ]] || return 0
    version="$(readlink "$current")" || return 0
    if atm_spotatui_managed_version "$version"; then printf '%s\n' "$version"; fi
}

atm_spotatui_list_installed_versions() {
    local dir="" version=""
    [[ -d "$ATM_SPOTATUI_INSTALL_ROOT" ]] || return 0
    for dir in "$ATM_SPOTATUI_INSTALL_ROOT"/*; do
        version="${dir##*/}"
        if atm_spotatui_managed_version "$version"; then printf '%s\n' "$version"; fi
    done | sort -Vr
}

atm_spotatui_status() {
    local version="$(atm_spotatui_current_version)"
    if [[ -n "$version" ]]; then printf '✅ %s\n' "$version"; else atm_t ATM_PLUGIN_SPOTATUI_NOT_INSTALLED; fi
}

atm_spotatui_write_manifest() {
    local version="$(atm_spotatui_current_version)" versions="$(atm_spotatui_list_installed_versions | paste -sd ' ' -)" installed=0
    [[ -z "$versions" ]] || installed=1
    atm_manifest_write spotatui 'ATM_PLUGIN_NAME="spotatui"' 'ATM_PLUGIN_VERSION="0.0.1"' "ATM_INSTALLED=$installed" \
        "ATM_CURRENT_VERSION=$(printf '%q' "$version")" "ATM_CURRENT_PATH=$(printf '%q' "$(atm_spotatui_current_path)")" \
        "ATM_INSTALL_ROOT=$(printf '%q' "$ATM_SPOTATUI_INSTALL_ROOT")" "ATM_INSTALLED_VERSIONS=$(printf '%q' "$versions")"
}

atm_spotatui_dependency_help() {
    atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_HELP)"
    printf '%s\n%s\n%s\n%s\n%s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_INSTALL)" \
        "pipx install 'yt-dlp[default]'" "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_UPDATE)" \
        'pipx upgrade yt-dlp' "$(atm_t ATM_PLUGIN_SPOTATUI_NODE_HELP)"
}

atm_spotatui_dependency_info() {
    if ! command -v python3 >/dev/null 2>&1; then
        atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_FAILED)"; return 1
    fi
    python3 -B "${BASH_SOURCE[0]%/*}/release.py" dependencies
}

atm_spotatui_check_dependencies() {
    local info=""
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_PLAN)"; return 0; fi
    if ! info="$(atm_spotatui_dependency_info)"; then
        atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_FAILED)"
        atm_spotatui_dependency_help
        return 1
    fi
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_READY)"
    printf '%s\n' "$info"
}

atm_spotatui_release() {
    local requested="$1" platform="$2" endpoint="" response=""
    endpoint='https://api.github.com/repos/LargeModGames/spotatui/releases/latest'
    [[ "$requested" == latest ]] || endpoint="https://api.github.com/repos/LargeModGames/spotatui/releases/tags/v$requested"
    response="$(curl --proto '=https' --tlsv1.2 -fsSL --retry 2 --connect-timeout 10 --max-time 30 "$endpoint")" || return 1
    python3 -B "${BASH_SOURCE[0]%/*}/release.py" resolve "$platform" <<< "$response"
}

atm_spotatui_activate() {
    local version="$1" current="$(atm_spotatui_current_path)"
    atm_spotatui_managed_version "$version" || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_NOT_FOUND)"; return 1; }
    [[ ! -e "$current" || -L "$current" ]] || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_FAILED)"; return 1; }
    atm_run ln -sfn -- "$version" "$current" || return 1
    atm_spotatui_write_manifest || return 1
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_USE_DONE)"
}

atm_spotatui_use() {
    local version="" deps=""
    version="$(atm_spotatui_normalize_version "${1:-}")" || return 1
    atm_spotatui_managed_version "$version" || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_NOT_FOUND)"; return 1; }
    if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
        deps="$(atm_spotatui_dependency_info)" || { atm_spotatui_dependency_help; return 1; }
        python3 -B "${BASH_SOURCE[0]%/*}/release.py" configure "$(atm_spotatui_install_dir "$version")" <<< "$deps" || return 1
    fi
    atm_spotatui_activate "$version"
}

atm_spotatui_install() {
    local requested="" platform="" deps="" release="" version="" url="" checksum_url="" archive="" checksum=""
    requested="$(atm_spotatui_version_from_args "$@")" || return 1
    platform="$(atm_spotatui_platform)" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_PLAN)"
        printf '%s/%s (%s)\n' "$ATM_SPOTATUI_INSTALL_ROOT" "$requested" "$platform"
        return 0
    fi
    if ! deps="$(atm_spotatui_dependency_info)"; then
        atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_FAILED)"
        atm_spotatui_dependency_help
        return 1
    fi
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_DEP_READY)"
    release="$(atm_spotatui_release "$requested" "$platform")" \
        || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_RELEASE_FAILED)"; return 1; }
    IFS=$'\t' read -r version url checksum_url <<< "$release"
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && ( "$requested" == latest || "$requested" == "$version" ) ]] || return 1
    if ! atm_spotatui_managed_version "$version"; then
        archive="$ATM_SPOTATUI_CACHE_DIR/$version/${url##*/}"
        checksum="$archive.sha256"
        atm_download_file "$url" "$archive" && atm_download_file "$checksum_url" "$checksum" \
            && python3 -B "${BASH_SOURCE[0]%/*}/release.py" install "$archive" "$checksum" "$(atm_spotatui_install_dir "$version")" <<< "$deps" \
            || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_FAILED)"; return 1; }
    else
        python3 -B "${BASH_SOURCE[0]%/*}/release.py" configure "$(atm_spotatui_install_dir "$version")" <<< "$deps" || return 1
    fi
    atm_spotatui_activate "$version" || return 1
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_DONE)"
    atm_spotatui_usage
}

atm_spotatui_path_entries() {
    if [[ -n "$(atm_spotatui_current_version)" ]]; then printf '%s/bin\n' "$(atm_spotatui_current_path)"; fi
}

atm_spotatui_remove() {
    local version=""
    version="$(atm_spotatui_normalize_version "${1:-}")" || return 1
    atm_spotatui_managed_version "$version" || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_NOT_FOUND)"; return 1; }
    [[ "$version" != "$(atm_spotatui_current_version)" ]] || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_PROTECTED)"; return 1; }
    atm_run rm -rf -- "$(atm_spotatui_install_dir "$version")" || return 1
    atm_spotatui_write_manifest || return 1
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_REMOVED)"
}

atm_spotatui_uninstall() {
    local answer="" version="" current="$(atm_spotatui_current_version)"
    printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_CONFIRM)"
    read -r answer || return 0
    case "$answer" in y|Y|yes|YES) ;; *) atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_CANCELLED)"; return 0 ;; esac
    while IFS= read -r version; do
        [[ -n "$version" ]] || continue
        atm_run rm -rf -- "$(atm_spotatui_install_dir "$version")" || return 1
    done < <(atm_spotatui_list_installed_versions)
    if [[ -n "$current" ]]; then atm_run rm -f -- "$(atm_spotatui_current_path)" || return 1; fi
    atm_run rm -f -- "$ATM_SPOTATUI_MANIFEST_FILE" || return 1
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_REMOVED)"
    atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_RETAINED)"
}

atm_spotatui_usage() {
    atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_PATH)"
    printf '%s\n' "$(atm_spotatui_current_path)/bin/spotatui" 'spotatui' 'https://spotatui.com/'
}

atm_spotatui_menu() {
    local choice="" version="" key="" idx=1
    local keys=(LATEST CUSTOM DEPS USE LIST REMOVE UNINSTALL USAGE)
    while true; do
        clear || true
        printf '%s\n%s\n%s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_TITLE)" 'https://spotatui.com/' "$(atm_spotatui_status)"
        idx=1
        for key in "${keys[@]}"; do printf '%s) %s\n' "$idx" "$(atm_t "ATM_PLUGIN_SPOTATUI_$key")"; idx=$((idx + 1)); done
        printf 'b) %s\nq) %s\n%s ' "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_EXIT)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_spotatui_install || true ;;
            2) printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_VERSION)"; read -r version || return 0; atm_spotatui_install --version "$version" || true ;;
            3) atm_spotatui_check_dependencies || true ;;
            4|6)
                atm_spotatui_list_installed_versions
                printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_VERSION)"
                read -r version || return 0
                if [[ "$choice" == 4 ]]; then atm_spotatui_use "$version" || true; else atm_spotatui_remove "$version" || true; fi ;;
            5) atm_spotatui_list_installed_versions ;;
            7) atm_spotatui_uninstall || true ;;
            8) atm_spotatui_usage ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

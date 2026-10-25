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
    local version="$(atm_spotatui_current_version)" other="" parts=()
    [[ -z "$version" ]] || parts+=("ATM: $version")
    if [[ -e "$ATM_SPOTATUI_LOCAL_ENTRY" || -L "$ATM_SPOTATUI_LOCAL_ENTRY" ]]; then
        other="$(atm_spotatui_local_version)"
        parts+=("$(atm_t ATM_PLUGIN_SPOTATUI_LOCAL): $other")
    fi
    if ((${#parts[@]})); then printf '✅ %s\n' "${parts[*]}"; else atm_t ATM_PLUGIN_SPOTATUI_NOT_INSTALLED; fi
}

atm_spotatui_local_entry_owned() {
    [[ -L "$ATM_SPOTATUI_LOCAL_ENTRY" && "$(readlink "$ATM_SPOTATUI_LOCAL_ENTRY")" == "$ATM_SPOTATUI_LOCAL_ROOT/current/bin/spotatui" ]]
}

atm_spotatui_local_version() {
    local version=""
    if atm_spotatui_local_entry_owned; then
        version="$(ATM_SPOTATUI_INSTALL_ROOT="$ATM_SPOTATUI_LOCAL_ROOT" atm_spotatui_current_version)"
    elif [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_t ATM_PLUGIN_SPOTATUI_NOT_PROBED; return 0
    elif command -v python3 >/dev/null 2>&1; then
        version="$(python3 -B "${BASH_SOURCE[0]%/*}/release.py" installed-version "$ATM_SPOTATUI_LOCAL_ENTRY")" || true
    fi
    printf '%s\n' "${version:-$(atm_t ATM_PLUGIN_SPOTATUI_UNKNOWN)}"
}

atm_spotatui_show_installations() {
    printf '%s\n' "$(atm_spotatui_status)"
    printf '%s: %s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_ATM)" "$ATM_SPOTATUI_INSTALL_ROOT"
    atm_spotatui_list_installed_versions
    printf '%s: %s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_LOCAL)" "$ATM_SPOTATUI_LOCAL_ENTRY"
    ATM_SPOTATUI_INSTALL_ROOT="$ATM_SPOTATUI_LOCAL_ROOT" atm_spotatui_list_installed_versions
    printf '%s %s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_SELECTED_COMMAND)" "$(command -v spotatui || true)"
}

atm_spotatui_choose_location() {
    local choice=""
    printf '%s\n1) %s (%s): %s\n2) %s: %s\n%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_LOCATION)" \
        "$(atm_t ATM_PLUGIN_SPOTATUI_ATM)" "$(atm_t ATM_PLUGIN_SPOTATUI_DEFAULT)" "$ATM_SPOTATUI_INSTALL_ROOT" \
        "$(atm_t ATM_PLUGIN_SPOTATUI_LOCAL)" "$ATM_SPOTATUI_LOCAL_ENTRY" "$(atm_t ATM_PLUGIN_SPOTATUI_LOCATION_PROMPT)" >&2
    read -r choice || return 1
    case "$choice" in
        ''|1) printf 'atm\n' ;; 2) printf 'local\n' ;; b|B) return 1 ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_LOCATION_INVALID)"; return 1 ;;
    esac
}

atm_spotatui_location_call() {
    local action="$1" location=atm explicit=0 choice=""
    local args=()
    shift
    while (($#)); do
        case "$1" in
            --location) (($# >= 2)) || return 1; location="$2"; explicit=1; shift ;;
            --location=*) location="${1#*=}"; explicit=1 ;;
            *) args+=("$1") ;;
        esac
        shift
    done
    case "$location" in atm|local) ;; *) atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_LOCATION_INVALID)"; return 1 ;; esac
    case "$action" in
        use|remove) ((${#args[@]} == 1)) || return 1 ;;
        uninstall|list) ((${#args[@]} == 0)) || return 1 ;;
    esac
    if [[ "$action" == install && "$explicit" == 0 && -t 0 ]]; then
        location="$(atm_spotatui_choose_location)" || return 0
    fi
    local ATM_SPOTATUI_LOCATION="$location" ATM_SPOTATUI_LOCAL_EXPECTED=""
    local ATM_SPOTATUI_INSTALL_ROOT="$ATM_SPOTATUI_INSTALL_ROOT"
    local ATM_SPOTATUI_MANIFEST_ID=spotatui ATM_SPOTATUI_MANIFEST_FILE="$ATM_SPOTATUI_MANIFEST_FILE"
    if [[ "$location" == local ]]; then
        [[ "$ATM_SPOTATUI_LOCAL_ROOT" == /* && "$ATM_SPOTATUI_LOCAL_ENTRY" == /* \
            && "$ATM_SPOTATUI_LOCAL_ROOT" != "$ATM_SPOTATUI_INSTALL_ROOT" ]] || return 1
        ATM_SPOTATUI_INSTALL_ROOT="$ATM_SPOTATUI_LOCAL_ROOT"
        ATM_SPOTATUI_MANIFEST_ID=spotatui_local
        ATM_SPOTATUI_MANIFEST_FILE="$ATM_MANIFEST_DIR/spotatui_local.manifest"
        if [[ ( "$action" == install || "$action" == use ) && "${ATM_DRY_RUN:-0}" != 1 ]]; then
            command -v python3 >/dev/null 2>&1 || { atm_spotatui_dependency_help; return 1; }
            ATM_SPOTATUI_LOCAL_EXPECTED="$(python3 -B "${BASH_SOURCE[0]%/*}/release.py" fingerprint "$ATM_SPOTATUI_LOCAL_ENTRY")" || return 1
            if [[ "$ATM_SPOTATUI_LOCAL_EXPECTED" != missing ]] && ! atm_spotatui_local_entry_owned; then
                printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_REPLACE)"
                read -r choice || return 0
                case "$choice" in y|Y|yes|YES) ;; *) atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_CANCELLED)"; return 0 ;; esac
            fi
        fi
    fi
    "atm_spotatui_${action}_at" "${args[@]}"
}

atm_spotatui_install() { atm_spotatui_location_call install "$@"; }
atm_spotatui_use() { atm_spotatui_location_call use "$@"; }
atm_spotatui_remove() { atm_spotatui_location_call remove "$@"; }
atm_spotatui_uninstall() { atm_spotatui_location_call uninstall "$@"; }
atm_spotatui_list_at() { atm_spotatui_list_installed_versions; }

atm_spotatui_write_manifest() {
    local version="$(atm_spotatui_current_version)" versions="$(atm_spotatui_list_installed_versions | paste -sd ' ' -)" installed=0
    [[ -z "$versions" ]] || installed=1
    atm_manifest_write "$ATM_SPOTATUI_MANIFEST_ID" 'ATM_PLUGIN_NAME="spotatui"' 'ATM_PLUGIN_VERSION="0.0.1"' "ATM_INSTALLED=$installed" \
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
    if [[ "${ATM_SPOTATUI_LOCATION:-atm}" == local && "${ATM_DRY_RUN:-0}" != 1 ]]; then
        local backup=""
        backup="$(python3 -B "${BASH_SOURCE[0]%/*}/release.py" publish-entry "$ATM_SPOTATUI_LOCAL_ENTRY" \
            "$ATM_SPOTATUI_LOCAL_ROOT/current/bin/spotatui" "$ATM_SPOTATUI_LOCAL_EXPECTED")" \
            || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_ENTRY_FAILED)"; return 1; }
        [[ -z "$backup" ]] || printf '%s %s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_BACKUP_KEPT)" "$backup"
    fi
    atm_spotatui_write_manifest || return 1
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_USE_DONE)"
}

atm_spotatui_use_at() {
    local version="" deps=""
    version="$(atm_spotatui_normalize_version "${1:-}")" || return 1
    atm_spotatui_managed_version "$version" || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_NOT_FOUND)"; return 1; }
    if [[ "${ATM_DRY_RUN:-0}" != 1 ]]; then
        deps="$(atm_spotatui_dependency_info)" || { atm_spotatui_dependency_help; return 1; }
        python3 -B "${BASH_SOURCE[0]%/*}/release.py" configure "$(atm_spotatui_install_dir "$version")" <<< "$deps" || return 1
    fi
    atm_spotatui_activate "$version"
}

atm_spotatui_install_at() {
    local requested="" platform="" deps="" release="" version="" url="" checksum_url="" archive="" checksum=""
    requested="$(atm_spotatui_version_from_args "$@")" || return 1
    platform="$(atm_spotatui_platform)" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_PLAN)"
        printf '%s/%s (%s)\n' "$ATM_SPOTATUI_INSTALL_ROOT" "$requested" "$platform"
        if [[ "${ATM_SPOTATUI_LOCATION:-atm}" == local ]]; then
            atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_ENTRY_PLAN)"
            printf '%s\n' "$ATM_SPOTATUI_LOCAL_ENTRY"
        fi
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
    if atm_spotatui_local_entry_owned && [[ -n "$(ATM_SPOTATUI_INSTALL_ROOT="$ATM_SPOTATUI_LOCAL_ROOT" atm_spotatui_current_version)" ]]; then
        printf '%s\n' "${ATM_SPOTATUI_LOCAL_ENTRY%/*}"
    fi
}

atm_spotatui_remove_at() {
    local version=""
    version="$(atm_spotatui_normalize_version "${1:-}")" || return 1
    atm_spotatui_managed_version "$version" || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_NOT_FOUND)"; return 1; }
    [[ "$version" != "$(atm_spotatui_current_version)" ]] || { atm_warn "$(atm_t ATM_PLUGIN_SPOTATUI_PROTECTED)"; return 1; }
    atm_run rm -rf -- "$(atm_spotatui_install_dir "$version")" || return 1
    atm_spotatui_write_manifest || return 1
    atm_success "$(atm_t ATM_PLUGIN_SPOTATUI_REMOVED)"
}

atm_spotatui_uninstall_at() {
    local answer="" version="" current="$(atm_spotatui_current_version)"
    printf '%s\n' "$ATM_SPOTATUI_INSTALL_ROOT"
    printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_CONFIRM)"
    read -r answer || return 0
    case "$answer" in y|Y|yes|YES) ;; *) atm_info "$(atm_t ATM_PLUGIN_SPOTATUI_CANCELLED)"; return 0 ;; esac
    if [[ "${ATM_SPOTATUI_LOCATION:-atm}" == local ]] && atm_spotatui_local_entry_owned; then
        atm_run rm -f -- "$ATM_SPOTATUI_LOCAL_ENTRY" || return 1
    fi
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
    local choice="" version="" key="" idx=1 location=""
    local keys=(LATEST CUSTOM DEPS USE LIST REMOVE UNINSTALL USAGE)
    while true; do
        clear || true
        printf '%s\n%s\n%s\n' "$(atm_t ATM_PLUGIN_SPOTATUI_TITLE)" 'https://spotatui.com/' "$(atm_spotatui_status)"
        printf 'ATM: %s\n%s: %s\n' "$ATM_SPOTATUI_INSTALL_ROOT" "$(atm_t ATM_PLUGIN_SPOTATUI_LOCAL)" "$ATM_SPOTATUI_LOCAL_ENTRY"
        idx=1
        for key in "${keys[@]}"; do printf '%s) %s\n' "$idx" "$(atm_t "ATM_PLUGIN_SPOTATUI_$key")"; idx=$((idx + 1)); done
        printf 'b) %s\nq) %s\n%s ' "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_EXIT)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1|2)
                version=latest
                if [[ "$choice" == 2 ]]; then printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_VERSION)"; read -r version || return 0; fi
                location="$(atm_spotatui_choose_location)" || continue
                atm_spotatui_install --version "$version" --location "$location" || true ;;
            3) atm_spotatui_check_dependencies || true ;;
            4|6)
                location="$(atm_spotatui_choose_location)" || continue
                atm_spotatui_location_call list --location "$location"
                printf '%s ' "$(atm_t ATM_PLUGIN_SPOTATUI_VERSION)"
                read -r version || return 0
                if [[ "$choice" == 4 ]]; then atm_spotatui_use "$version" --location "$location" || true; else atm_spotatui_remove "$version" --location "$location" || true; fi ;;
            5) atm_spotatui_show_installations ;;
            7)
                location="$(atm_spotatui_choose_location)" || continue
                atm_spotatui_uninstall --location "$location" || true ;;
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

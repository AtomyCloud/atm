#!/usr/bin/env bash

atm_trivy_normalize_version() {
    local version="${1:-}"
    version="${version#v}"
    [[ "$version" == latest || "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
        || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_INVALID_VERSION)"
    printf '%s\n' "$version"
}

atm_trivy_platform() {
    [[ "$(uname -s)" == Linux ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_PLATFORM)"
    case "$(uname -m)" in
        x86_64|amd64) printf 'Linux-64bit\n' ;;
        aarch64|arm64) printf 'Linux-ARM64\n' ;;
        armv7l) printf 'Linux-ARM\n' ;;
        i386|i486|i586|i686) printf 'Linux-32bit\n' ;;
        ppc64le) printf 'Linux-PPC64LE\n' ;;
        s390x) printf 'Linux-s390x\n' ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_TRIVY_PLATFORM)" ;;
    esac
}

atm_trivy_install_dir() { printf '%s/%s\n' "$ATM_TRIVY_INSTALL_ROOT" "$1"; }
atm_trivy_current_path() { printf '%s/current\n' "$ATM_TRIVY_INSTALL_ROOT"; }
atm_trivy_cache_dir() { printf '%s\n' "$ATM_TRIVY_CACHE_DIR"; }
atm_trivy_manifest_file() { printf '%s\n' "$ATM_TRIVY_MANIFEST_FILE"; }
atm_trivy_archive_name() { printf 'trivy_%s_%s.tar.gz\n' "$1" "$(atm_trivy_platform)"; }
atm_trivy_download_url() { printf 'https://github.com/aquasecurity/trivy/releases/download/v%s/%s\n' "$1" "$(atm_trivy_archive_name "$1")"; }
atm_trivy_cache_file() { printf '%s/%s\n' "$ATM_TRIVY_CACHE_DIR" "$(atm_trivy_archive_name "$1")"; }

atm_trivy_managed_version() {
    local version="${1:-}" dir=""
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
    dir="$(atm_trivy_install_dir "$version")"
    [[ -d "$dir" && ! -L "$dir" && -f "$dir/.atm-trivy-managed" && ! -L "$dir/.atm-trivy-managed" \
        && -x "$dir/bin/trivy" && ! -L "$dir/bin/trivy" ]]
}

atm_trivy_current_version() {
    local version=""
    [[ -L "$(atm_trivy_current_path)" ]] || return 0
    version="$(readlink "$(atm_trivy_current_path)")" || return 0
    if atm_trivy_managed_version "$version"; then printf '%s\n' "$version"; fi
}

atm_trivy_list_installed_versions() {
    local dir="" version=""
    [[ -d "$ATM_TRIVY_INSTALL_ROOT" ]] || return 0
    for dir in "$ATM_TRIVY_INSTALL_ROOT"/*; do
        version="${dir##*/}"
        if atm_trivy_managed_version "$version"; then printf '%s\n' "$version"; fi
    done | sort -Vr
}

atm_trivy_docker_version() {
    local version=""
    [[ -f "$ATM_TRIVY_DOCKER_STATE" ]] || return 0
    IFS= read -r version < "$ATM_TRIVY_DOCKER_STATE" || true
    if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then printf '%s\n' "$version"; fi
}

atm_trivy_status() {
    local binary="$(atm_trivy_current_version)" image="$(atm_trivy_docker_version)" parts=()
    if [[ -n "$binary" ]]; then parts+=("$(atm_t ATM_PLUGIN_TRIVY_BINARY_STATUS) $binary"); fi
    if [[ -n "$image" ]]; then parts+=("$(atm_t ATM_PLUGIN_TRIVY_DOCKER_STATUS) $image"); fi
    if ((${#parts[@]})); then printf '✅ %s\n' "${parts[*]}"; else atm_t ATM_PLUGIN_TRIVY_NOT_INSTALLED; fi
}

atm_trivy_write_manifest() {
    local binary="$(atm_trivy_current_version)" image="$(atm_trivy_docker_version)"
    local versions="$(atm_trivy_list_installed_versions | paste -sd ' ' -)" installed=0 path=""
    if [[ -n "$versions$image" ]]; then installed=1; fi
    if [[ -n "$binary" ]]; then path="$(atm_trivy_current_path)"; fi
    atm_manifest_write trivy 'ATM_PLUGIN_NAME="Trivy"' 'ATM_PLUGIN_VERSION="0.0.1"' \
        "ATM_INSTALLED=$installed" "ATM_CURRENT_VERSION=$(printf '%q' "${binary:-$image}")" \
        "ATM_CURRENT_PATH=$(printf '%q' "$path")" "ATM_INSTALL_ROOT=$(printf '%q' "$ATM_TRIVY_INSTALL_ROOT")" \
        "ATM_INSTALLED_VERSIONS=$(printf '%q' "$versions")" "ATM_TRIVY_DOCKER_VERSION=$(printf '%q' "$image")"
}

atm_trivy_release() {
    local version="$1" platform="$2" endpoint="" response=""
    atm_require_commands curl python3
    endpoint="https://api.github.com/repos/aquasecurity/trivy/releases/latest"
    if [[ "$version" != latest ]]; then endpoint="https://api.github.com/repos/aquasecurity/trivy/releases/tags/v$version"; fi
    response="$(curl --proto '=https' --tlsv1.2 -fsSL --retry 2 --connect-timeout 10 --max-time 30 "$endpoint")" || return 1
    python3 -B "${BASH_SOURCE[0]%/*}/release.py" resolve "$platform" <<< "$response"
}

atm_trivy_use() {
    local version="" current="$(atm_trivy_current_path)"
    version="$(atm_trivy_normalize_version "${1:-}")" || return 1
    atm_trivy_managed_version "$version" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_NOT_FOUND)"
    [[ ! -e "$current" || -L "$current" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    atm_run ln -sfn -- "$version" "$current" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    atm_trivy_write_manifest
}

atm_trivy_install_binary() {
    local requested="" platform="" release="" version="" url="" checksum_url="" archive="" checksums=""
    requested="$(atm_trivy_normalize_version "${1:-$ATM_TRIVY_DEFAULT_VERSION}")" || return 1
    platform="$(atm_trivy_platform)" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: %s %s/%s (%s)\n' "$(atm_t ATM_PLUGIN_TRIVY_BINARY_PLAN)" "$ATM_TRIVY_INSTALL_ROOT" "$requested" "$platform"
        return 0
    fi
    if atm_trivy_managed_version "$requested"; then
        atm_trivy_use "$requested"
        atm_success "$(atm_t ATM_PLUGIN_TRIVY_BINARY_DONE)"
        return 0
    fi
    release="$(atm_trivy_release "$requested" "$platform")" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_RELEASE_FAILED)"
    IFS=$'\t' read -r version url checksum_url <<< "$release"
    [[ "$requested" == latest || "$version" == "$requested" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_RELEASE_FAILED)"
    if ! atm_trivy_managed_version "$version"; then
        archive="$ATM_TRIVY_CACHE_DIR/${url##*/}"
        checksums="$ATM_TRIVY_CACHE_DIR/${checksum_url##*/}"
        atm_download_file "$url" "$archive" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
        atm_download_file "$checksum_url" "$checksums" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
        python3 -B "${BASH_SOURCE[0]%/*}/release.py" install "$archive" "$checksums" "$(atm_trivy_install_dir "$version")" \
            || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    fi
    atm_trivy_use "$version"
    atm_success "$(atm_t ATM_PLUGIN_TRIVY_BINARY_DONE)"
}

atm_trivy_require_docker() {
    command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 \
        || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_DOCKER_REQUIRED)"
}

atm_trivy_install_docker() {
    local requested="" version="" temporary=""
    requested="$(atm_trivy_normalize_version "${1:-$ATM_TRIVY_DEFAULT_VERSION}")" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: %s (%s)\n' "$(atm_t ATM_PLUGIN_TRIVY_DOCKER_PLAN)" "$requested"
        return 0
    fi
    atm_trivy_require_docker
    version="$(atm_trivy_release "$requested" docker)" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_RELEASE_FAILED)"
    [[ "$requested" == latest || "$version" == "$requested" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_RELEASE_FAILED)"
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_RELEASE_FAILED)"
    atm_run docker pull "aquasec/trivy:$version" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    docker image inspect "aquasec/trivy:$version" >/dev/null || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    mkdir -p "${ATM_TRIVY_DOCKER_STATE%/*}" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    temporary="$(mktemp "${ATM_TRIVY_DOCKER_STATE}.XXXXXX")" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    if ! { printf '%s\n' "$version" > "$temporary" && mv -f -- "$temporary" "$ATM_TRIVY_DOCKER_STATE"; }; then
        rm -f -- "$temporary"
        atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    fi
    atm_trivy_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_TRIVY_DOCKER_DONE) aquasec/trivy:$version"
    atm_trivy_usage
}

atm_trivy_install() {
    local method="" version="$ATM_TRIVY_DEFAULT_VERSION" choice=""
    while (($#)); do
        case "$1" in
            --method) method="${2:-}"; [[ "$#" -ge 2 && -n "$method" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_INVALID_ARGS)"; shift ;;
            --method=*) method="${1#*=}"; [[ -n "$method" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_INVALID_ARGS)" ;;
            --version) version="${2:-}"; [[ "$#" -ge 2 && -n "$version" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_INVALID_ARGS)"; shift ;;
            --version=*) version="${1#*=}" ;;
            *) atm_fail "$(atm_t ATM_PLUGIN_TRIVY_INVALID_ARGS)" ;;
        esac
        shift
    done
    version="$(atm_trivy_normalize_version "$version")" || return 1
    if [[ -z "$method" ]]; then
        printf '%s\n1) %s\n2) %s\nb) %s\nq) %s\n' "$(atm_t ATM_PLUGIN_TRIVY_METHOD)" \
            "$(atm_t ATM_PLUGIN_TRIVY_METHOD_BINARY)" "$(atm_t ATM_PLUGIN_TRIVY_METHOD_DOCKER)" "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_EXIT)"
        read -r choice || return 0
        case "$choice" in 1) method=binary ;; 2) method=docker ;; b|B) return 0 ;; q|Q) exit 0 ;; *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; return 0 ;; esac
    fi
    case "$method" in
        binary) atm_trivy_install_binary "$version" ;;
        docker) atm_trivy_install_docker "$version" ;;
        *) atm_fail "$(atm_t ATM_PLUGIN_TRIVY_INVALID_ARGS)" ;;
    esac
}

atm_trivy_path_entries() {
    if [[ -n "$(atm_trivy_current_version)" ]]; then printf '%s/bin\n' "$(atm_trivy_current_path)"; fi
}

atm_trivy_remove() {
    local version=""
    version="$(atm_trivy_normalize_version "${1:-}")" || return 1
    atm_trivy_managed_version "$version" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_NOT_FOUND)"
    [[ "$version" != "$(atm_trivy_current_version)" ]] || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_CURRENT_PROTECTED)"
    atm_run rm -rf -- "$(atm_trivy_install_dir "$version")" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    atm_trivy_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_TRIVY_REMOVED)"
}

atm_trivy_confirm() {
    local answer=""
    printf '%s\n%s ' "$1" "$(atm_t ATM_PLUGIN_TRIVY_CONFIRM)"
    read -r answer || return 1
    case "$answer" in y|Y|yes|YES) return 0 ;; *) atm_warn "$(atm_t ATM_PLUGIN_TRIVY_CANCELLED)"; return 1 ;; esac
}

atm_trivy_remove_image() {
    local version="$1" images="" reference=""
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_run docker image rm "aquasec/trivy:$version"; return 0; fi
    atm_trivy_require_docker
    images="$(docker image ls --format '{{.Repository}}:{{.Tag}}' --filter "reference=aquasec/trivy:$version")" \
        || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    while IFS= read -r reference; do
        if [[ "$reference" == "aquasec/trivy:$version" ]]; then
            atm_run docker image rm "aquasec/trivy:$version" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
            break
        fi
    done <<< "$images"
}

atm_trivy_remove_docker() {
    local version="$(atm_trivy_docker_version)"
    [[ -n "$version" ]] || { atm_warn "$(atm_t ATM_PLUGIN_TRIVY_NO_DOCKER)"; return 0; }
    atm_trivy_confirm "$(atm_t ATM_PLUGIN_TRIVY_REMOVE_DOCKER_WARNING) aquasec/trivy:$version" || return 0
    atm_trivy_remove_image "$version"
    atm_run rm -f -- "$ATM_TRIVY_DOCKER_STATE" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    atm_trivy_write_manifest
    atm_success "$(atm_t ATM_PLUGIN_TRIVY_REMOVED)"
}

atm_trivy_uninstall() {
    local image="$(atm_trivy_docker_version)" current="$(atm_trivy_current_version)" version=""
    atm_trivy_confirm "$(atm_t ATM_PLUGIN_TRIVY_UNINSTALL_WARNING)" || return 0
    if [[ -n "$image" ]]; then atm_trivy_remove_image "$image"; fi
    while IFS= read -r version; do
        [[ -n "$version" ]] || continue
        atm_run rm -rf -- "$(atm_trivy_install_dir "$version")" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    done < <(atm_trivy_list_installed_versions)
    if [[ -n "$current" ]]; then atm_run rm -f -- "$(atm_trivy_current_path)" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"; fi
    atm_run rm -f -- "$ATM_TRIVY_DOCKER_STATE" "$ATM_TRIVY_MANIFEST_FILE" || atm_fail "$(atm_t ATM_PLUGIN_TRIVY_FAILED)"
    atm_success "$(atm_t ATM_PLUGIN_TRIVY_REMOVED)"
    atm_info "$(atm_t ATM_PLUGIN_TRIVY_CACHE_KEPT)"
}

atm_trivy_usage() {
    local version="$(atm_trivy_docker_version)"
    printf '\n%s\n' 'https://trivy.dev/docs/latest/getting-started/'
    printf '%s\n' 'trivy --version' 'trivy image alpine:latest' 'trivy fs .'
    printf 'docker run --rm aquasec/trivy:%s --version\n' "${version:-latest}"
    printf 'docker run --rm aquasec/trivy:%s image alpine:latest\n' "${version:-latest}"
    printf 'docker run --rm -v "$PWD:/work:ro" aquasec/trivy:%s fs /work\n' "${version:-latest}"
}

atm_trivy_menu() {
    local choice="" version="" key="" idx=1
    local keys=(BINARY DOCKER CUSTOM USE LIST REMOVE REMOVE_DOCKER UNINSTALL USAGE)
    while true; do
        clear || true
        printf '%s\n%s\n%s\n' "$(atm_t ATM_PLUGIN_TRIVY_TITLE)" 'https://trivy.dev/docs/latest/getting-started/' "$(atm_trivy_status)"
        idx=1
        for key in "${keys[@]}"; do printf '%s) %s\n' "$idx" "$(atm_t "ATM_PLUGIN_TRIVY_$key")"; idx=$((idx + 1)); done
        printf 'b) %s\nq) %s\n%s ' "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_EXIT)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_trivy_install_binary latest ;;
            2) atm_trivy_install_docker latest ;;
            3) printf '%s ' "$(atm_t ATM_PLUGIN_TRIVY_VERSION_PROMPT)"; read -r version || return 0; atm_trivy_install --version "$version" ;;
            4|6)
                atm_trivy_list_installed_versions
                printf '%s ' "$(atm_t ATM_PLUGIN_TRIVY_INSTALLED_PROMPT)"
                read -r version || return 0
                if [[ "$choice" == 4 ]]; then atm_trivy_use "$version"; else atm_trivy_remove "$version"; fi
                ;;
            5) atm_trivy_list_installed_versions ;;
            7) atm_trivy_remove_docker ;;
            8) atm_trivy_uninstall ;;
            9) atm_trivy_usage ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

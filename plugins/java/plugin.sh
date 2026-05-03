#!/usr/bin/env bash

atm_java_vendor_slug() {
    local vendor="$1"

    vendor="$(printf '%s' "$vendor" | tr '[:upper:]' '[:lower:]')"

    case "$vendor" in
        openjdk|jdk)
            printf 'openjdk\n'
            ;;
        corretto|amazon|amazon-corretto|amazon_corretto)
            printf 'corretto\n'
            ;;
        temurin|eclipse|eclipse-temurin|eclipse_temurin)
            printf 'temurin\n'
            ;;
        microsoft|microsoft-openjdk|microsoft_openjdk|ms)
            printf 'microsoft\n'
            ;;
        graalvm|graal)
            printf 'graalvm\n'
            ;;
        custom)
            printf 'custom\n'
            ;;
        *)
            atm_fail "Invalid Java vendor: $vendor"
            ;;
    esac
}

atm_java_vendor_display() {
    local vendor="$1"

    case "$vendor" in
        openjdk) printf 'OpenJDK\n' ;;
        corretto) printf 'Amazon Corretto\n' ;;
        temurin) printf 'Eclipse Temurin\n' ;;
        microsoft) printf 'Microsoft OpenJDK\n' ;;
        graalvm) printf 'GraalVM JDK\n' ;;
        custom) printf 'Custom JDK\n' ;;
        *) printf '%s\n' "$vendor" ;;
    esac
}

atm_java_install_root() {
    printf '%s\n' "${ATM_JAVA_INSTALL_ROOT:-$ATM_APPS_DIR/Java}"
}

atm_java_current_path() {
    printf '%s/current\n' "$(atm_java_install_root)"
}

atm_java_cache_dir() {
    printf '%s\n' "${ATM_JAVA_CACHE_DIR:-$ATM_DOWNLOAD_DIR/java}"
}

atm_java_manifest_file() {
    printf '%s\n' "${ATM_JAVA_MANIFEST_FILE:-$ATM_MANIFEST_DIR/java.manifest}"
}

atm_java_normalize_version() {
    local version="$1"

    version="${version#jdk-}"
    version="${version#jdk}"
    version="${version#java-}"
    version="${version#java}"

    if [[ "$version" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid Java version: $version"
    fi
}

atm_java_archive_name_from_url() {
    local url="$1"
    local fallback="$2"
    local clean_url=""

    clean_url="${url%%\?*}"
    clean_url="$(basename "$clean_url")"

    if [[ "$clean_url" == *.tar.gz || "$clean_url" == *.tgz ]]; then
        printf '%s\n' "$clean_url"
    else
        printf '%s.tar.gz\n' "$fallback"
    fi
}

atm_java_cache_file() {
    local vendor="$1"
    local version="$2"
    local url="$3"
    local fallback=""

    fallback="java-${vendor}-${version}"
    printf '%s/%s\n' "$(atm_java_cache_dir)" "$(atm_java_archive_name_from_url "$url" "$fallback")"
}

atm_java_install_dir() {
    local vendor="$1"
    local version="$2"

    printf '%s/%s/%s\n' "$(atm_java_install_root)" "$vendor" "$version"
}

atm_java_resolve_url() {
    local vendor="$1"
    local version="$2"

    case "$vendor:$version" in
        openjdk:26.0.1|openjdk:26)
            printf '%s\n' "$ATM_JAVA_OPENJDK_26_0_1_URL"
            ;;
        corretto:26)
            printf '%s\n' "$ATM_JAVA_CORRETTO_26_URL"
            ;;
        temurin:26)
            printf '%s\n' "$ATM_JAVA_TEMURIN_26_URL"
            ;;
        microsoft:25)
            printf '%s\n' "$ATM_JAVA_MICROSOFT_25_URL"
            ;;
        graalvm:25)
            printf '%s\n' "$ATM_JAVA_GRAALVM_25_URL"
            ;;
        *)
            return 1
            ;;
    esac
}

atm_java_root_from_tar() {
    local file="$1"
    local list_file=""
    local first_entry=""
    local root_dir=""

    list_file="$(mktemp)"
    tar -tzf "$file" > "$list_file"

    IFS= read -r first_entry < "$list_file" || true
    rm -f "$list_file"

    [[ -n "${first_entry:-}" ]] || atm_fail "Could not read Java archive: $file"

    root_dir="${first_entry%%/*}"

    [[ -n "$root_dir" ]] || atm_fail "Could not detect Java archive root directory: $file"

    printf '%s\n' "$root_dir"
}

atm_java_version_from_args() {
    local vendor="${ATM_JAVA_DEFAULT_VENDOR:-openjdk}"
    local version="${ATM_JAVA_DEFAULT_VERSION:-26.0.1}"
    local url=""

    while (($# > 0)); do
        case "$1" in
            --vendor)
                vendor="${2:-}"
                [[ -n "$vendor" ]] || atm_fail "Missing value for --vendor"
                shift
                ;;
            --vendor=*)
                vendor="${1#--vendor=}"
                ;;
            --version)
                version="${2:-}"
                [[ -n "$version" ]] || atm_fail "Missing value for --version"
                shift
                ;;
            --version=*)
                version="${1#--version=}"
                ;;
            --url)
                url="${2:-}"
                [[ -n "$url" ]] || atm_fail "Missing value for --url"
                shift
                ;;
            --url=*)
                url="${1#--url=}"
                ;;
            *)
                ;;
        esac

        shift || true
    done

    vendor="$(atm_java_vendor_slug "$vendor")"

    if [[ "$vendor" != "custom" ]]; then
        version="$(atm_java_normalize_version "$version")"
    fi

    printf '%s|%s|%s\n' "$vendor" "$version" "$url"
}

atm_java_status() {
    local current=""
    local version_output=""

    current="$(atm_java_current_path)"

    if [[ -x "$current/bin/java" ]]; then
        version_output="$("$current/bin/java" -version 2>&1 | sed -n '1p')"
        printf '✅ %s\n' "$version_output"
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_JAVA_STATUS_NOT_INSTALLED)"
    fi
}

atm_java_current_version() {
    local current=""
    local resolved=""

    current="$(atm_java_current_path)"

    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1

    printf '%s\n' "${resolved#$(atm_java_install_root)/}"
}

atm_java_list_installed_versions() {
    local root=""
    local vendor_dir=""
    local version_dir=""

    root="$(atm_java_install_root)"

    [[ -d "$root" ]] || return 0

    for vendor_dir in "$root"/*; do
        [[ -d "$vendor_dir" ]] || continue
        [[ "$(basename "$vendor_dir")" == "current" ]] && continue

        for version_dir in "$vendor_dir"/*; do
            [[ -d "$version_dir" ]] || continue
            [[ -x "$version_dir/bin/java" ]] || continue
            printf '%s/%s\n' "$(basename "$vendor_dir")" "$(basename "$version_dir")"
        done
    done | sort -V
}

atm_java_write_manifest() {
    local current_version="${1:-}"
    local current_path=""
    local installed_versions=""

    current_path="$(atm_java_current_path)"
    installed_versions="$(atm_java_list_installed_versions | tr '\n' ' ' | sed 's/[[:space:]]*$//')"

    atm_manifest_write "java" \
        "ATM_PLUGIN_NAME=\"Java / JDK\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_VERSION=\"$current_version\"" \
        "ATM_CURRENT_PATH=\"$current_path\"" \
        "ATM_INSTALL_ROOT=\"$(atm_java_install_root)\"" \
        "ATM_INSTALLED_VERSIONS=\"$installed_versions\""
}

atm_java_extract_to_dest() {
    local cache_file="$1"
    local dest="$2"
    local archive_root=""
    local tmp_extract=""

    archive_root="$(atm_java_root_from_tar "$cache_file")"

    tmp_extract="${dest}.tmp.$$"
    rm -rf "$tmp_extract"
    mkdir -p "$tmp_extract"

    tar -xzf "$cache_file" -C "$tmp_extract"

    [[ -d "$tmp_extract/$archive_root" ]] || atm_fail "Unexpected Java archive structure: $cache_file"

    rm -rf "$dest"
    mv "$tmp_extract/$archive_root" "$dest"
    rm -rf "$tmp_extract"
}

atm_java_install() {
    local spec=""
    local vendor=""
    local version=""
    local url=""
    local vendor_display=""
    local cache_file=""
    local dest=""
    local current_label=""

    spec="$(atm_java_version_from_args "$@")"
    vendor="${spec%%|*}"
    spec="${spec#*|}"
    version="${spec%%|*}"
    url="${spec#*|}"

    vendor_display="$(atm_java_vendor_display "$vendor")"

    if [[ -z "$url" ]]; then
        url="$(atm_java_resolve_url "$vendor" "$version" || true)"
    fi

    [[ -n "$url" ]] || atm_fail "No URL configured for $vendor_display $version. Use --url <tar.gz-url>."

    cache_file="$(atm_java_cache_file "$vendor" "$version" "$url")"
    dest="$(atm_java_install_dir "$vendor" "$version")"
    current_label="$vendor/$version"

    printf '%s\n' "$(atm_t ATM_PLUGIN_JAVA_INSTALLING) $vendor_display $version"

    if [[ -x "$dest/bin/java" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_JAVA_ALREADY_INSTALLED): $dest"
    else
        mkdir -p "$(atm_java_cache_dir)"
        mkdir -p "$(dirname "$dest")"

        atm_download_file "$url" "$cache_file"

        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: install Java %s %s into %s\n' "$vendor_display" "$version" "$dest"
        else
            atm_java_extract_to_dest "$cache_file" "$dest"
        fi
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_java_current_path)"
    else
        ln -sfn "$dest" "$(atm_java_current_path)"
    fi

    atm_java_write_manifest "$current_label"

    atm_success "$(atm_t ATM_PLUGIN_JAVA_INSTALLED): $vendor_display $version"

    if [[ "${ATM_DRY_RUN:-0}" != "1" && -x "$(atm_java_current_path)/bin/java" ]]; then
        "$(atm_java_current_path)/bin/java" -version
    fi
}

atm_java_use() {
    local target="${1:-}"
    local dest=""

    [[ -n "$target" ]] || atm_fail "Usage: atm use java <vendor/version>"

    if [[ "$target" != */* ]]; then
        atm_fail "Use format: atm use java <vendor/version>, example: atm use java openjdk/26.0.1"
    fi

    dest="$(atm_java_install_root)/$target"

    [[ -x "$dest/bin/java" ]] || atm_fail "Java version is not installed: $target"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_java_current_path)"
    else
        ln -sfn "$dest" "$(atm_java_current_path)"
    fi

    atm_java_write_manifest "$target"

    atm_success "$(atm_t ATM_PLUGIN_JAVA_USING): $target"
}

atm_java_remove() {
    local target="${1:-}"
    local dest=""
    local current_version=""

    [[ -n "$target" ]] || atm_fail "Usage: atm remove java <vendor/version>"

    if [[ "$target" != */* ]]; then
        atm_fail "Use format: atm remove java <vendor/version>, example: atm remove java openjdk/26.0.1"
    fi

    dest="$(atm_java_install_root)/$target"
    current_version="$(atm_java_current_version 2>/dev/null || true)"

    [[ -d "$dest" ]] || atm_fail "Java version is not installed: $target"

    if [[ "$current_version" == "$target" ]]; then
        atm_fail "Cannot remove current Java version: $target. Switch version first with: atm use java <other-version>"
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$dest"
    else
        rm -rf "$dest"
    fi

    atm_java_write_manifest "$current_version"

    atm_success "$(atm_t ATM_PLUGIN_JAVA_REMOVED): $target"
}

atm_java_uninstall() {
    local root=""
    local answer=""

    root="$(atm_java_install_root)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_JAVA_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_JAVA_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_java_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_java_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_JAVA_UNINSTALLED)"
}

atm_java_menu_install_custom_url() {
    local label=""
    local version=""
    local url=""

    printf '%s ' "$(atm_t ATM_PLUGIN_JAVA_ENTER_CUSTOM_LABEL)"
    read -r label
    label="${label:-custom}"

    printf '%s ' "$(atm_t ATM_PLUGIN_JAVA_ENTER_VERSION)"
    read -r version
    [[ -n "$version" ]] || version="$label"

    printf '%s ' "$(atm_t ATM_PLUGIN_JAVA_ENTER_URL)"
    read -r url
    [[ -n "$url" ]] || atm_fail "URL is required."

    atm_java_install --vendor custom --version "$version" --url "$url"
}

atm_java_menu() {
    local choice=""
    local current=""

    while true; do
        clear
        current="$(atm_java_status)"

        printf '%s\n' "=========================================="
        printf '    ☕ %s\n' "$(atm_t ATM_PLUGIN_JAVA_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_JAVA_CURRENT)" "$current"
        printf '%s\n' "------------------------------------------"
        printf '1) OpenJDK 26.0.1 (%s)\n' "$(atm_t ATM_PLUGIN_JAVA_DEFAULT)"
        printf '2) Amazon Corretto 26\n'
        printf '3) Eclipse Temurin 26\n'
        printf '4) Microsoft OpenJDK 25\n'
        printf '5) GraalVM JDK 25\n'
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_JAVA_CUSTOM_URL)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_JAVA_LIST_INSTALLED)"
        printf '8) %s\n' "$(atm_t ATM_PLUGIN_JAVA_REMOVE_VERSION)"
        printf '9) %s\n' "$(atm_t ATM_PLUGIN_JAVA_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_java_install --vendor openjdk --version 26.0.1
                ;;
            2)
                atm_java_install --vendor corretto --version 26
                ;;
            3)
                atm_java_install --vendor temurin --version 26
                ;;
            4)
                atm_java_install --vendor microsoft --version 25
                ;;
            5)
                atm_java_install --vendor graalvm --version 25
                ;;
            6)
                atm_java_menu_install_custom_url
                ;;
            7)
                atm_java_list_installed_versions
                ;;
            8)
                printf '%s ' "$(atm_t ATM_PLUGIN_JAVA_ENTER_TARGET)"
                read -r target
                [[ -n "$target" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }
                atm_java_remove "$target"
                ;;
            9)
                atm_java_uninstall
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

atm_java_path_entries() {
    printf '%s/current/bin\n' "$(atm_java_install_root)"
}
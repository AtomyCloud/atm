#!/usr/bin/env bash

atm_android_studio_upper_id() {
    local id="$1"
    printf '%s\n' "$id" | tr '[:lower:]-' '[:upper:]_'
}

atm_android_studio_slug() {
    local value="$1"

    value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
    value="$(printf '%s' "$value" | sed -E 's/[^a-z0-9._-]+/_/g; s/^_+//; s/_+$//')"

    [[ -n "$value" ]] || value="android_studio"
    printf '%s\n' "$value"
}

atm_android_studio_install_root() {
    printf '%s\n' "${ATM_ANDROID_STUDIO_INSTALL_ROOT:-$ATM_APPS_DIR/AndroidStudio}"
}

atm_android_studio_current_path() {
    printf '%s/current\n' "$(atm_android_studio_install_root)"
}

atm_android_studio_cache_dir() {
    printf '%s\n' "${ATM_ANDROID_STUDIO_CACHE_DIR:-$ATM_DOWNLOAD_DIR/android-studio}"
}

atm_android_studio_manifest_file() {
    printf '%s\n' "${ATM_ANDROID_STUDIO_MANIFEST_FILE:-$ATM_MANIFEST_DIR/android_studio.manifest}"
}

atm_android_studio_desktop_dir() {
    printf '%s\n' "${ATM_ANDROID_STUDIO_DESKTOP_DIR:-$HOME/.local/share/applications}"
}

atm_android_studio_option_value() {
    local id="$1"
    local field="$2"
    local upper=""
    local var=""

    upper="$(atm_android_studio_upper_id "$id")"
    var="ATM_ANDROID_STUDIO_${upper}_${field}"

    printf '%s\n' "${!var:-}"
}

atm_android_studio_option_name() {
    atm_android_studio_option_value "$1" "NAME"
}

atm_android_studio_option_version() {
    atm_android_studio_option_value "$1" "VERSION"
}

atm_android_studio_option_package() {
    atm_android_studio_option_value "$1" "PACKAGE"
}

atm_android_studio_option_url() {
    atm_android_studio_option_value "$1" "URL"
}

atm_android_studio_default_id() {
    printf '%s\n' "${ATM_ANDROID_STUDIO_DEFAULT_ID:-panda4}"
}

atm_android_studio_normalize_version() {
    local version="$1"

    if [[ "$version" =~ ^[0-9]{4}\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid Android Studio version: $version. Expected format: 2025.3.4.6"
    fi
}

atm_android_studio_cache_file() {
    local package="$1"
    printf '%s/%s\n' "$(atm_android_studio_cache_dir)" "$package"
}

atm_android_studio_install_dir() {
    local name="$1"
    local version="$2"
    local slug=""

    slug="$(atm_android_studio_slug "$name")"

    printf '%s/studio_%s_%s\n' "$(atm_android_studio_install_root)" "$slug" "$version"
}

atm_android_studio_desktop_file_name() {
    local name="$1"
    local version="$2"
    local slug=""

    slug="$(atm_android_studio_slug "${name}_${version}")"

    printf 'atm-android-studio-%s.desktop\n' "$slug"
}

atm_android_studio_version_from_args() {
    local id="$(atm_android_studio_default_id)"
    local name=""
    local version=""
    local package=""
    local url=""

    while (($# > 0)); do
        case "$1" in
            --id)
                id="${2:-}"
                [[ -n "$id" ]] || atm_fail "Missing value for --id"
                shift
                ;;
            --id=*)
                id="${1#--id=}"
                ;;
            --name)
                name="${2:-}"
                [[ -n "$name" ]] || atm_fail "Missing value for --name"
                shift
                ;;
            --name=*)
                name="${1#--name=}"
                ;;
            --version)
                version="${2:-}"
                [[ -n "$version" ]] || atm_fail "Missing value for --version"
                shift
                ;;
            --version=*)
                version="${1#--version=}"
                ;;
            --package)
                package="${2:-}"
                [[ -n "$package" ]] || atm_fail "Missing value for --package"
                shift
                ;;
            --package=*)
                package="${1#--package=}"
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

    if [[ -z "$name" ]]; then
        name="$(atm_android_studio_option_name "$id")"
    fi

    if [[ -z "$version" ]]; then
        version="$(atm_android_studio_option_version "$id")"
    fi

    if [[ -z "$package" ]]; then
        package="$(atm_android_studio_option_package "$id")"
    fi

    if [[ -z "$url" ]]; then
        url="$(atm_android_studio_option_url "$id")"
    fi

    [[ -n "$name" ]] || atm_fail "Android Studio name is required."
    [[ -n "$version" ]] || atm_fail "Android Studio version is required."
    [[ -n "$package" ]] || atm_fail "Android Studio package filename is required."

    version="$(atm_android_studio_normalize_version "$version")"

    printf '%s|%s|%s|%s|%s\n' "$id" "$name" "$version" "$package" "$url"
}

atm_android_studio_infer_version_from_dir() {
    local dir="$1"
    local base=""

    base="$(basename "$dir")"
    base="${base#studio_}"

    if [[ "$base" =~ ([0-9]{4}\.[0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
        printf '%s\n' "${BASH_REMATCH[1]}"
        return 0
    fi

    return 1
}

atm_android_studio_infer_name_from_dir() {
    local dir="$1"
    local base=""
    local version=""
    local slug=""

    base="$(basename "$dir")"
    base="${base#studio_}"

    version="$(atm_android_studio_infer_version_from_dir "$dir" 2>/dev/null || true)"

    if [[ -n "$version" ]]; then
        slug="${base%_$version}"
    else
        slug="$base"
    fi

    case "$slug" in
        android_studio_panda_4|panda_4|panda4)
            printf 'Android Studio Panda 4\n'
            ;;
        android_studio_panda_4_rc_1|panda_4_rc_1|panda4_rc1)
            printf 'Android Studio Panda 4 RC 1\n'
            ;;
        android_studio_panda_3_patch_1|panda_3_patch_1|panda3_patch1)
            printf 'Android Studio Panda 3 Patch 1\n'
            ;;
        android_studio_quail_1_canary_2|quail_1_canary_2|quail1_canary2)
            printf 'Android Studio Quail 1 Canary 2\n'
            ;;
        android_studio_quail_1_canary_1|quail_1_canary_1|quail1_canary1)
            printf 'Android Studio Quail 1 Canary 1\n'
            ;;
        *)
            printf '%s\n' "$slug" | sed -E 's/_/ /g; s/\bandroid\b/Android/g; s/\bstudio\b/Studio/g; s/\bpanda\b/Panda/g; s/\bquail\b/Quail/g; s/\bcanary\b/Canary/g; s/\bpatch\b/Patch/g'
            ;;
    esac
}

atm_android_studio_resolved_current_path() {
    local current=""
    local resolved=""

    current="$(atm_android_studio_current_path)"

    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1
    printf '%s\n' "$resolved"
}

atm_android_studio_status() {
    local current=""
    local resolved=""
    local meta=""
    local inferred_name=""
    local inferred_version=""

    current="$(atm_android_studio_current_path)"

    if [[ ! -x "$current/bin/studio.sh" ]]; then
        printf '%s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_STATUS_NOT_INSTALLED)"
        return 0
    fi

    resolved="$(atm_android_studio_resolved_current_path 2>/dev/null || true)"
    meta="$resolved/.atm-meta"

    if [[ -n "$resolved" && -f "$meta" ]]; then
        # shellcheck source=/dev/null
        source "$meta"

        if [[ -n "${ATM_ANDROID_STUDIO_DISPLAY_NAME:-}" && -n "${ATM_ANDROID_STUDIO_VERSION:-}" ]]; then
            printf '✅ %s | %s\n' "$ATM_ANDROID_STUDIO_DISPLAY_NAME" "$ATM_ANDROID_STUDIO_VERSION"
            return 0
        fi

        if [[ -n "${ATM_ANDROID_STUDIO_DISPLAY_NAME:-}" ]]; then
            printf '✅ %s\n' "$ATM_ANDROID_STUDIO_DISPLAY_NAME"
            return 0
        fi
    fi

    if [[ -n "$resolved" ]]; then
        inferred_name="$(atm_android_studio_infer_name_from_dir "$resolved" 2>/dev/null || true)"
        inferred_version="$(atm_android_studio_infer_version_from_dir "$resolved" 2>/dev/null || true)"

        if [[ -n "$inferred_name" && -n "$inferred_version" ]]; then
            printf '✅ %s | %s\n' "$inferred_name" "$inferred_version"
            return 0
        fi

        if [[ -n "$inferred_name" ]]; then
            printf '✅ %s\n' "$inferred_name"
            return 0
        fi
    fi

    printf '✅ Android Studio\n'
}

atm_android_studio_current_version() {
    local resolved=""
    local meta=""
    local inferred_version=""

    resolved="$(atm_android_studio_resolved_current_path 2>/dev/null || true)"
    [[ -n "$resolved" ]] || return 1

    meta="$resolved/.atm-meta"

    if [[ -f "$meta" ]]; then
        # shellcheck source=/dev/null
        source "$meta"

        if [[ -n "${ATM_ANDROID_STUDIO_VERSION:-}" ]]; then
            printf '%s\n' "$ATM_ANDROID_STUDIO_VERSION"
            return 0
        fi
    fi

    inferred_version="$(atm_android_studio_infer_version_from_dir "$resolved" 2>/dev/null || true)"

    if [[ -n "$inferred_version" ]]; then
        printf '%s\n' "$inferred_version"
        return 0
    fi

    basename "$resolved"
}

atm_android_studio_current_label() {
    local resolved=""
    local meta=""
    local inferred_name=""
    local inferred_version=""
    local inferred_id=""

    resolved="$(atm_android_studio_resolved_current_path 2>/dev/null || true)"
    [[ -n "$resolved" ]] || return 1

    meta="$resolved/.atm-meta"

    if [[ -f "$meta" ]]; then
        # shellcheck source=/dev/null
        source "$meta"

        if [[ -n "${ATM_ANDROID_STUDIO_ID:-}" && -n "${ATM_ANDROID_STUDIO_VERSION:-}" ]]; then
            printf '%s/%s\n' "$ATM_ANDROID_STUDIO_ID" "$ATM_ANDROID_STUDIO_VERSION"
            return 0
        fi
    fi

    inferred_name="$(atm_android_studio_infer_name_from_dir "$resolved" 2>/dev/null || true)"
    inferred_version="$(atm_android_studio_infer_version_from_dir "$resolved" 2>/dev/null || true)"

    if [[ -n "$inferred_name" && -n "$inferred_version" ]]; then
        inferred_id="$(atm_android_studio_slug "$inferred_name")"
        inferred_id="${inferred_id#android_studio_}"

        case "$inferred_id" in
            panda_4) inferred_id="panda4" ;;
            panda_4_rc_1) inferred_id="panda4_rc1" ;;
            panda_3_patch_1) inferred_id="panda3_patch1" ;;
            quail_1_canary_2) inferred_id="quail1_canary2" ;;
            quail_1_canary_1) inferred_id="quail1_canary1" ;;
        esac

        printf '%s/%s\n' "$inferred_id" "$inferred_version"
        return 0
    fi

    basename "$resolved"
}

atm_android_studio_list_installed_versions() {
    local root=""
    local path=""
    local meta=""
    local label=""

    root="$(atm_android_studio_install_root)"

    [[ -d "$root" ]] || return 0

    for path in "$root"/studio_*; do
        [[ -d "$path" ]] || continue
        [[ -x "$path/bin/studio.sh" ]] || continue

        meta="$path/.atm-meta"

        if [[ -f "$meta" ]]; then
            # shellcheck source=/dev/null
            source "$meta"
            label="${ATM_ANDROID_STUDIO_ID:-unknown}/${ATM_ANDROID_STUDIO_VERSION:-unknown} - ${ATM_ANDROID_STUDIO_DISPLAY_NAME:-Android Studio}"
        else
            label="$(basename "$path")"
        fi

        printf '%s\n' "$label"
    done | sort -V
}

atm_android_studio_write_meta() {
    local dest="$1"
    local id="$2"
    local name="$3"
    local version="$4"
    local package="$5"
    local url="$6"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: write meta %s/.atm-meta\n' "$dest"
        return 0
    fi

    cat > "$dest/.atm-meta" <<EOF_META
ATM_ANDROID_STUDIO_ID="$id"
ATM_ANDROID_STUDIO_DISPLAY_NAME="$name"
ATM_ANDROID_STUDIO_VERSION="$version"
ATM_ANDROID_STUDIO_PACKAGE="$package"
ATM_ANDROID_STUDIO_URL="$url"
EOF_META
}

atm_android_studio_write_manifest() {
    local current_label="${1:-}"
    local current_path=""
    local installed_versions=""

    current_path="$(atm_android_studio_current_path)"
    installed_versions="$(atm_android_studio_list_installed_versions | tr '\n' '|' | sed 's/|$//')"

    atm_manifest_write "android_studio" \
        "ATM_PLUGIN_NAME=\"Android Studio\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_VERSION=\"$current_label\"" \
        "ATM_CURRENT_PATH=\"$current_path\"" \
        "ATM_INSTALL_ROOT=\"$(atm_android_studio_install_root)\"" \
        "ATM_INSTALLED_VERSIONS=\"$installed_versions\""
}

atm_android_studio_create_desktop_entry() {
    local name="$1"
    local version="$2"
    local current_path=""
    local desktop_dir=""
    local desktop_file=""

    [[ "${ATM_ANDROID_STUDIO_CREATE_DESKTOP_ENTRY:-1}" == "1" ]] || return 0

    current_path="$(atm_android_studio_current_path)"
    desktop_dir="$(atm_android_studio_desktop_dir)"
    desktop_file="$desktop_dir/$(atm_android_studio_desktop_file_name "$name" "$version")"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: create desktop file %s\n' "$desktop_file"
        return 0
    fi

    mkdir -p "$desktop_dir"

    cat > "$desktop_file" <<EOF_DESKTOP
[Desktop Entry]
Type=Application
Name=$name
Comment=Android Studio installed by ATM
Exec=$current_path/bin/studio.sh
Icon=$current_path/bin/studio.png
Terminal=false
Categories=Development;IDE;
EOF_DESKTOP

    chmod 644 "$desktop_file"

    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database "$desktop_dir" >/dev/null 2>&1 || true
    fi
}

atm_android_studio_install() {
    local spec=""
    local id=""
    local name=""
    local version=""
    local package=""
    local url=""
    local cache_file=""
    local dest=""
    local current_label=""

    spec="$(atm_android_studio_version_from_args "$@")"

    id="${spec%%|*}"
    spec="${spec#*|}"
    name="${spec%%|*}"
    spec="${spec#*|}"
    version="${spec%%|*}"
    spec="${spec#*|}"
    package="${spec%%|*}"
    url="${spec#*|}"

    [[ -n "$url" ]] || atm_fail "No URL configured for $name $version. Use --url <android-studio-linux.tar.gz-url>."

    cache_file="$(atm_android_studio_cache_file "$package")"
    dest="$(atm_android_studio_install_dir "$name" "$version")"
    current_label="$id/$version"

    printf '%s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_INSTALLING) $name"

    if [[ -x "$dest/bin/studio.sh" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_ALREADY_INSTALLED): $dest"
    else
        mkdir -p "$(atm_android_studio_cache_dir)"
        mkdir -p "$(atm_android_studio_install_root)"

        atm_download_file "$url" "$cache_file"

        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: install %s into %s\n' "$name" "$dest"
        else
            atm_archive_extract_tar_gz "$cache_file" "$dest" 1
        fi
    fi

    atm_android_studio_write_meta "$dest" "$id" "$name" "$version" "$package" "$url"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_android_studio_current_path)"
    else
        ln -sfn "$dest" "$(atm_android_studio_current_path)"
    fi

    atm_android_studio_create_desktop_entry "$name" "$version"
    atm_android_studio_write_manifest "$current_label"

    atm_success "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_INSTALLED): $name"
}

atm_android_studio_use() {
    local target="${1:-}"
    local root=""
    local path=""
    local meta=""
    local id=""
    local version=""
    local name=""
    local current_label=""

    [[ -n "$target" ]] || atm_fail "Usage: atm use android_studio <id/version>"

    if [[ "$target" != */* ]]; then
        atm_fail "Use format: atm use android_studio <id/version>, example: atm use android_studio panda4/2025.3.4.6"
    fi

    id="${target%%/*}"
    version="${target#*/}"
    root="$(atm_android_studio_install_root)"

    for path in "$root"/studio_*; do
        [[ -d "$path" ]] || continue
        [[ -x "$path/bin/studio.sh" ]] || continue

        meta="$path/.atm-meta"
        [[ -f "$meta" ]] || continue

        # shellcheck source=/dev/null
        source "$meta"

        if [[ "${ATM_ANDROID_STUDIO_ID:-}" == "$id" && "${ATM_ANDROID_STUDIO_VERSION:-}" == "$version" ]]; then
            name="${ATM_ANDROID_STUDIO_DISPLAY_NAME:-Android Studio}"
            current_label="$id/$version"

            if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
                printf 'DRY-RUN: ln -sfn %s %s\n' "$path" "$(atm_android_studio_current_path)"
            else
                ln -sfn "$path" "$(atm_android_studio_current_path)"
            fi

            atm_android_studio_create_desktop_entry "$name" "$version"
            atm_android_studio_write_manifest "$current_label"

            atm_success "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_USING): $name"
            return 0
        fi
    done

    atm_fail "Android Studio version is not installed: $target"
}

atm_android_studio_remove() {
    local target="${1:-}"
    local root=""
    local path=""
    local meta=""
    local id=""
    local version=""
    local current_label=""

    [[ -n "$target" ]] || atm_fail "Usage: atm remove android_studio <id/version>"

    if [[ "$target" != */* ]]; then
        atm_fail "Use format: atm remove android_studio <id/version>, example: atm remove android_studio panda4/2025.3.4.6"
    fi

    id="${target%%/*}"
    version="${target#*/}"
    current_label="$(atm_android_studio_current_label 2>/dev/null || true)"
    root="$(atm_android_studio_install_root)"

    if [[ "$current_label" == "$target" ]]; then
        atm_fail "Cannot remove current Android Studio version: $target. Switch version first with: atm use android_studio <other-version>"
    fi

    for path in "$root"/studio_*; do
        [[ -d "$path" ]] || continue
        meta="$path/.atm-meta"
        [[ -f "$meta" ]] || continue

        # shellcheck source=/dev/null
        source "$meta"

        if [[ "${ATM_ANDROID_STUDIO_ID:-}" == "$id" && "${ATM_ANDROID_STUDIO_VERSION:-}" == "$version" ]]; then
            if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
                printf 'DRY-RUN: rm -rf %s\n' "$path"
            else
                rm -rf "$path"
            fi

            atm_android_studio_write_manifest "$current_label"
            atm_success "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_REMOVED): $target"
            return 0
        fi
    done

    atm_fail "Android Studio version is not installed: $target"
}

atm_android_studio_uninstall() {
    local root=""
    local answer=""

    root="$(atm_android_studio_install_root)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_android_studio_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_android_studio_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_UNINSTALLED)"
}

atm_android_studio_menu_install_custom_url() {
    local name=""
    local version=""
    local package=""
    local url=""

    printf '%s ' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_ENTER_NAME)"
    read -r name
    [[ -n "$name" ]] || atm_fail "Name is required."

    printf '%s ' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_ENTER_VERSION)"
    read -r version
    [[ -n "$version" ]] || atm_fail "Version is required."

    printf '%s ' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_ENTER_PACKAGE)"
    read -r package
    [[ -n "$package" ]] || package="$(atm_android_studio_slug "$name")-$version-linux.tar.gz"

    printf '%s ' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_ENTER_URL)"
    read -r url
    [[ -n "$url" ]] || atm_fail "URL is required."

    atm_android_studio_install \
        --id custom \
        --name "$name" \
        --version "$version" \
        --package "$package" \
        --url "$url"
}

atm_android_studio_menu() {
    local choice=""
    local id=""
    local idx=1
    local option_ids=()
    local name=""
    local version=""
    local url=""
    local target=""

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    🤖 %s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_CURRENT)" "$(atm_android_studio_status)"
        printf '%s: %s\n' "Install root" "$(atm_android_studio_install_root)"
        printf '%s\n' "------------------------------------------"

        idx=1
        option_ids=()

        for id in ${ATM_ANDROID_STUDIO_OPTION_IDS:-panda4 quail1_canary2 quail1_canary1 panda4_rc1 panda3_patch1}; do
            option_ids+=("$id")
            name="$(atm_android_studio_option_name "$id")"
            version="$(atm_android_studio_option_version "$id")"
            url="$(atm_android_studio_option_url "$id")"

            if [[ -n "$url" ]]; then
                printf '%s) %s | %s\n' "$idx" "$name" "$version"
            else
                printf '%s) %s | %s (%s)\n' "$idx" "$name" "$version" "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_URL_REQUIRED)"
            fi

            idx=$((idx + 1))
        done

        printf '6) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_CUSTOM_URL)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_LIST_INSTALLED)"
        printf '8) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_REMOVE_VERSION)"
        printf '9) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1|2|3|4|5)
                id="${option_ids[$((choice - 1))]}"
                atm_android_studio_install --id "$id"
                ;;
            6)
                atm_android_studio_menu_install_custom_url
                ;;
            7)
                atm_android_studio_list_installed_versions
                ;;
            8)
                printf '%s ' "$(atm_t ATM_PLUGIN_ANDROID_STUDIO_ENTER_TARGET)"
                read -r target
                [[ -n "$target" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }
                atm_android_studio_remove "$target"
                ;;
            9)
                atm_android_studio_uninstall
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

atm_android_studio_path_entries() {
    printf '%s/current/bin\n' "$(atm_android_studio_install_root)"
}
#!/usr/bin/env bash

atm_android_sdk_root() {
    printf '%s\n' "${ATM_ANDROID_SDK_ROOT:-$ATM_APPS_DIR/AndroidStudio/SDK}"
}

atm_android_sdk_cache_dir() {
    printf '%s\n' "${ATM_ANDROID_SDK_CACHE_DIR:-$ATM_DOWNLOAD_DIR/android-sdk}"
}

atm_android_sdk_manifest_file() {
    printf '%s\n' "${ATM_ANDROID_SDK_MANIFEST_FILE:-$ATM_MANIFEST_DIR/android_sdk.manifest}"
}

atm_android_sdk_cmdline_tools_dir() {
    printf '%s/cmdline-tools/%s\n' "$(atm_android_sdk_root)" "${ATM_ANDROID_CMDLINE_TOOLS_VERSION:-14742923}"
}

atm_android_sdk_cmdline_tools_latest_dir() {
    printf '%s/cmdline-tools/latest\n' "$(atm_android_sdk_root)"
}

atm_android_sdk_sdkmanager() {
    printf '%s/bin/sdkmanager\n' "$(atm_android_sdk_cmdline_tools_latest_dir)"
}

atm_android_sdk_package_file() {
    printf '%s/%s\n' "$(atm_android_sdk_cache_dir)" "${ATM_ANDROID_CMDLINE_TOOLS_PACKAGE:-commandlinetools-linux-14742923_latest.zip}"
}

atm_android_sdk_package_installed() {
    local package="$1"
    local sdk_root=""

    sdk_root="$(atm_android_sdk_root)"

    case "$package" in
        platform-tools)
            [[ -x "$sdk_root/platform-tools/adb" ]]
            ;;
        emulator)
            [[ -x "$sdk_root/emulator/emulator" ]]
            ;;
        platforms\;android-*)
            local api="${package#platforms;}"
            [[ -d "$sdk_root/platforms/$api" ]]
            ;;
        build-tools\;*)
            local version="${package#build-tools;}"
            [[ -d "$sdk_root/build-tools/$version" ]]
            ;;
        cmake\;*)
            local version="${package#cmake;}"
            [[ -x "$sdk_root/cmake/$version/bin/cmake" ]]
            ;;
        ndk\;*)
            local version="${package#ndk;}"
            [[ -d "$sdk_root/ndk/$version" ]]
            ;;
        *)
            return 1
            ;;
    esac
}

atm_android_sdk_ensure_java() {
    if [[ -x "$ATM_APPS_DIR/Java/current/bin/java" ]]; then
        export JAVA_HOME="$ATM_APPS_DIR/Java/current"
        export PATH="$JAVA_HOME/bin:$PATH"
        return 0
    fi

    if command -v java >/dev/null 2>&1; then
        return 0
    fi

    atm_fail "Java is required for Android sdkmanager. Install Java first: atm install java"
}

atm_android_sdk_ensure_cmdline_tools() {
    local sdk_root=""
    local cmd_dir=""
    local latest_dir=""
    local zip_file=""
    local tmp_dir=""

    sdk_root="$(atm_android_sdk_root)"
    cmd_dir="$(atm_android_sdk_cmdline_tools_dir)"
    latest_dir="$(atm_android_sdk_cmdline_tools_latest_dir)"
    zip_file="$(atm_android_sdk_package_file)"

    if [[ -x "$latest_dir/bin/sdkmanager" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_ANDROID_SDK_CMDLINE_TOOLS_ALREADY_INSTALLED): $latest_dir"
        return 0
    fi

    mkdir -p "$(atm_android_sdk_cache_dir)"
    mkdir -p "$sdk_root/cmdline-tools"

    atm_download_file "${ATM_ANDROID_CMDLINE_TOOLS_URL:-https://dl.google.com/android/repository/commandlinetools-linux-14742923_latest.zip}" "$zip_file"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: unzip %s into %s\n' "$zip_file" "$cmd_dir"
        printf 'DRY-RUN: ln -sfn %s %s\n' "$cmd_dir" "$latest_dir"
        return 0
    fi

    atm_require_commands unzip

    tmp_dir="$sdk_root/cmdline-tools/.tmp-${ATM_ANDROID_CMDLINE_TOOLS_VERSION:-14742923}-$$"
    rm -rf "$tmp_dir"
    mkdir -p "$tmp_dir"

    unzip -q "$zip_file" -d "$tmp_dir"

    [[ -d "$tmp_dir/cmdline-tools" ]] || atm_fail "Unexpected Android cmdline-tools archive structure: $zip_file"

    rm -rf "$cmd_dir"
    mv "$tmp_dir/cmdline-tools" "$cmd_dir"
    rm -rf "$tmp_dir"

    ln -sfn "$cmd_dir" "$latest_dir"
}

atm_android_sdk_accept_licenses() {
    local sdkmanager=""

    sdkmanager="$(atm_android_sdk_sdkmanager)"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: yes | %s --sdk_root=%s --licenses\n' "$sdkmanager" "$(atm_android_sdk_root)"
        return 0
    fi

    [[ -x "$sdkmanager" ]] || atm_fail "sdkmanager not found: $sdkmanager"

    set +o pipefail
    yes | "$sdkmanager" --sdk_root="$(atm_android_sdk_root)" --licenses >/dev/null
    local sdkmanager_status="${PIPESTATUS[1]}"
    set -o pipefail

    [[ "$sdkmanager_status" -eq 0 ]] || atm_fail "Failed to accept Android SDK licenses."
}

atm_android_sdk_install_packages() {
    local packages=("$@")
    local sdkmanager=""
    local package=""
    local pending=()

    sdkmanager="$(atm_android_sdk_sdkmanager)"

    [[ "${#packages[@]}" -gt 0 ]] || return 0

    for package in "${packages[@]}"; do
        if atm_android_sdk_package_installed "$package"; then
            atm_warn "$(atm_t ATM_PLUGIN_ANDROID_SDK_PACKAGE_ALREADY_INSTALLED): $package"
        else
            pending+=("$package")
        fi
    done

    if [[ "${#pending[@]}" -eq 0 ]]; then
        atm_success "$(atm_t ATM_PLUGIN_ANDROID_SDK_ALL_PACKAGES_PRESENT)"
        return 0
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: %s --sdk_root=%s --channel=%s' \
            "$sdkmanager" \
            "$(atm_android_sdk_root)" \
            "${ATM_ANDROID_SDK_CHANNEL:-0}"

        for package in "${pending[@]}"; do
            printf ' %q' "$package"
        done

        printf '\n'
        return 0
    fi

    [[ -x "$sdkmanager" ]] || atm_fail "sdkmanager not found: $sdkmanager"

    "$sdkmanager" \
        --sdk_root="$(atm_android_sdk_root)" \
        --channel="${ATM_ANDROID_SDK_CHANNEL:-0}" \
        "${pending[@]}"
}

atm_android_sdk_stack_packages() {
    local stack="${1:-latest}"

    case "$stack" in
        latest)
            printf '%s\n' \
                "platform-tools" \
                "emulator" \
                "platforms;${ATM_ANDROID_DEFAULT_PLATFORM:-android-36}" \
                "build-tools;${ATM_ANDROID_DEFAULT_BUILD_TOOLS:-36.0.0}" \
                "cmake;${ATM_ANDROID_DEFAULT_CMAKE:-3.31.6}" \
                "ndk;${ATM_ANDROID_DEFAULT_NDK:-29.0.14206865}"
            ;;
        api36)
            printf '%s\n' \
                "platform-tools" \
                "emulator" \
                "platforms;android-36" \
                "build-tools;36.0.0" \
                "cmake;3.31.6" \
                "ndk;29.0.14206865"
            ;;
        api35)
            printf '%s\n' \
                "platform-tools" \
                "emulator" \
                "platforms;android-35" \
                "build-tools;35.0.1" \
                "cmake;3.31.6" \
                "ndk;29.0.14206865"
            ;;
        api35_legacy)
            printf '%s\n' \
                "platform-tools" \
                "emulator" \
                "platforms;android-35" \
                "build-tools;35.0.0" \
                "cmake;3.22.1" \
                "ndk;28.2.13676358"
            ;;
        api34)
            printf '%s\n' \
                "platform-tools" \
                "emulator" \
                "platforms;android-34" \
                "build-tools;34.0.0" \
                "cmake;3.22.1" \
                "ndk;27.3.13750724"
            ;;
        custom)
            return 0
            ;;
        *)
            atm_fail "Unknown Android SDK stack: $stack"
            ;;
    esac
}

atm_android_sdk_version_from_args() {
    local stack="latest"
    local platform=""
    local build_tools=""
    local cmake=""
    local ndk=""

    while (($# > 0)); do
        case "$1" in
            --stack)
                stack="${2:-}"
                [[ -n "$stack" ]] || atm_fail "Missing value for --stack"
                shift
                ;;
            --stack=*)
                stack="${1#--stack=}"
                ;;
            --platform)
                platform="${2:-}"
                [[ -n "$platform" ]] || atm_fail "Missing value for --platform"
                shift
                ;;
            --platform=*)
                platform="${1#--platform=}"
                ;;
            --build-tools)
                build_tools="${2:-}"
                [[ -n "$build_tools" ]] || atm_fail "Missing value for --build-tools"
                shift
                ;;
            --build-tools=*)
                build_tools="${1#--build-tools=}"
                ;;
            --cmake)
                cmake="${2:-}"
                [[ -n "$cmake" ]] || atm_fail "Missing value for --cmake"
                shift
                ;;
            --cmake=*)
                cmake="${1#--cmake=}"
                ;;
            --ndk)
                ndk="${2:-}"
                [[ -n "$ndk" ]] || atm_fail "Missing value for --ndk"
                shift
                ;;
            --ndk=*)
                ndk="${1#--ndk=}"
                ;;
            *)
                ;;
        esac

        shift || true
    done

    if [[ -n "$platform" || -n "$build_tools" || -n "$cmake" || -n "$ndk" ]]; then
        stack="custom"
    fi

    printf '%s|%s|%s|%s|%s\n' "$stack" "$platform" "$build_tools" "$cmake" "$ndk"
}

atm_android_sdk_status() {
    local sdk_root=""
    local platform=""
    local ndk=""
    local build_tools=""
    local configured_ndk="${ATM_ANDROID_DEFAULT_NDK:-}"

    sdk_root="$(atm_android_sdk_root)"

    if [[ ! -x "$(atm_android_sdk_sdkmanager)" ]]; then
        printf '%s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_STATUS_NOT_INSTALLED)"
        return 0
    fi

    platform="$(
        find "$sdk_root/platforms" -maxdepth 1 -mindepth 1 -type d 2>/dev/null \
            | sed 's|.*/||' \
            | grep -E '^android-[0-9]+$' \
            | sort -V \
            | tail -n 1 || true
    )"

    if [[ -z "$platform" ]]; then
        platform="$(
            find "$sdk_root/platforms" -maxdepth 1 -mindepth 1 -type d -name 'android-*' 2>/dev/null \
                | sed 's|.*/||' \
                | sort -V \
                | tail -n 1 || true
        )"
    fi

    build_tools="$(
        find "$sdk_root/build-tools" -maxdepth 1 -mindepth 1 -type d 2>/dev/null \
            | sed 's|.*/||' \
            | grep -Ev '(^|[-.])rc[0-9]*$' \
            | sort -V \
            | tail -n 1 || true
    )"

    if [[ -z "$build_tools" ]]; then
        build_tools="$(
            find "$sdk_root/build-tools" -maxdepth 1 -mindepth 1 -type d 2>/dev/null \
                | sed 's|.*/||' \
                | sort -V \
                | tail -n 1 || true
        )"
    fi

    if [[ -n "$configured_ndk" && -d "$sdk_root/ndk/$configured_ndk" ]]; then
        ndk="$configured_ndk"
    else
        ndk="$(
            find "$sdk_root/ndk" -maxdepth 1 -mindepth 1 -type d 2>/dev/null \
                | sed 's|.*/||' \
                | grep -Ev '(^|[-.])rc[0-9]*$' \
                | sort -V \
                | tail -n 1 || true
        )"
    fi

    if [[ -z "$ndk" ]]; then
        ndk="$(
            find "$sdk_root/ndk" -maxdepth 1 -mindepth 1 -type d 2>/dev/null \
                | sed 's|.*/||' \
                | sort -V \
                | tail -n 1 || true
        )"
    fi

    if [[ -n "$platform" || -n "$ndk" || -n "$build_tools" ]]; then
        printf '✅ API %s / Build Tools %s / NDK %s\n' \
            "${platform#android-}" \
            "${build_tools:-none}" \
            "${ndk:-none}"
    else
        printf '✅ cmdline-tools installed\n'
    fi
}

atm_android_sdk_list_installed_packages() {
    local sdk_root=""

    sdk_root="$(atm_android_sdk_root)"

    printf 'SDK root: %s\n' "$sdk_root"
    printf 'cmdline-tools: %s\n' "$(atm_android_sdk_cmdline_tools_latest_dir)"

    printf '\nPlatforms:\n'
    find "$sdk_root/platforms" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | sed 's|.*/|- |' | sort -V || true

    printf '\nBuild tools:\n'
    find "$sdk_root/build-tools" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | sed 's|.*/|- |' | sort -V || true

    printf '\nCMake:\n'
    find "$sdk_root/cmake" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | sed 's|.*/|- |' | sort -V || true

    printf '\nNDK:\n'
    find "$sdk_root/ndk" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | sed 's|.*/|- |' | sort -V || true
}

atm_android_sdk_write_manifest() {
    local stack="${1:-latest}"
    local packages="${2:-}"
    local sdk_root=""

    sdk_root="$(atm_android_sdk_root)"

    atm_manifest_write "android_sdk" \
        "ATM_PLUGIN_NAME=\"Android SDK\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_STACK=\"$stack\"" \
        "ATM_SDK_ROOT=\"$sdk_root\"" \
        "ATM_ANDROID_HOME=\"$sdk_root\"" \
        "ATM_ANDROID_SDK_ROOT=\"$sdk_root\"" \
        "ATM_PACKAGES=\"$packages\""
}

atm_android_sdk_install() {
    local spec=""
    local stack=""
    local platform=""
    local build_tools=""
    local cmake=""
    local ndk=""
    local packages=()
    local packages_text=""

    spec="$(atm_android_sdk_version_from_args "$@")"
    stack="${spec%%|*}"
    spec="${spec#*|}"
    platform="${spec%%|*}"
    spec="${spec#*|}"
    build_tools="${spec%%|*}"
    spec="${spec#*|}"
    cmake="${spec%%|*}"
    ndk="${spec#*|}"

    printf '%s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_INSTALLING) $stack"

    atm_android_sdk_ensure_java
    atm_android_sdk_ensure_cmdline_tools
    atm_android_sdk_accept_licenses

    if [[ "$stack" == "custom" ]]; then
        packages=("platform-tools" "emulator")

        [[ -n "$platform" ]] && packages+=("platforms;$platform")
        [[ -n "$build_tools" ]] && packages+=("build-tools;$build_tools")
        [[ -n "$cmake" ]] && packages+=("cmake;$cmake")
        [[ -n "$ndk" ]] && packages+=("ndk;$ndk")
    else
        mapfile -t packages < <(atm_android_sdk_stack_packages "$stack")
    fi

    atm_android_sdk_install_packages "${packages[@]}"

    packages_text="$(printf '%s ' "${packages[@]}" | sed 's/[[:space:]]*$//')"
    atm_android_sdk_write_manifest "$stack" "$packages_text"

    atm_success "$(atm_t ATM_PLUGIN_ANDROID_SDK_INSTALLED): $stack"
}

atm_android_sdk_remove() {
    local package="${1:-}"
    local sdkmanager=""

    [[ -n "$package" ]] || atm_fail "Usage: atm remove android_sdk <sdk-package>"

    sdkmanager="$(atm_android_sdk_sdkmanager)"
    [[ -x "$sdkmanager" ]] || atm_fail "sdkmanager not found: $sdkmanager"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: %s --sdk_root=%s --uninstall %q\n' "$sdkmanager" "$(atm_android_sdk_root)" "$package"
    else
        "$sdkmanager" --sdk_root="$(atm_android_sdk_root)" --uninstall "$package"
    fi

    atm_success "$(atm_t ATM_PLUGIN_ANDROID_SDK_REMOVED): $package"
}

atm_android_sdk_uninstall() {
    local root=""
    local answer=""

    root="$(atm_android_sdk_root)"

    printf '%s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES)
            ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_ANDROID_SDK_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_android_sdk_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_android_sdk_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_ANDROID_SDK_UNINSTALLED)"
}

atm_android_sdk_use() {
    atm_warn "Android SDK does not use a current-version symlink. Use install/remove package operations instead."
}

atm_android_sdk_menu() {
    local choice=""
    local platform=""
    local build_tools=""
    local cmake=""
    local ndk=""
    local package=""

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    📦 %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_CURRENT)" "$(atm_android_sdk_status)"
        printf '%s: %s\n' "SDK root" "$(atm_android_sdk_root)"
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_STACK_LATEST)"
        printf '2) Android API 36 + Build Tools 36.0.0 + CMake 3.31.6 + NDK r29\n'
        printf '3) Android API 35 + Build Tools 35.0.1 + CMake 3.31.6 + NDK r29\n'
        printf '4) Android API 35 + Build Tools 35.0.0 + CMake 3.22.1 + NDK r28\n'
        printf '5) Android API 34 + Build Tools 34.0.0 + CMake 3.22.1 + NDK r27\n'
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_CUSTOM_STACK)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_LIST_INSTALLED)"
        printf '8) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_REMOVE_PACKAGE)"
        printf '9) %s\n' "$(atm_t ATM_PLUGIN_ANDROID_SDK_UNINSTALL)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_android_sdk_install --stack latest
                ;;
            2)
                atm_android_sdk_install --stack api36
                ;;
            3)
                atm_android_sdk_install --stack api35
                ;;
            4)
                atm_android_sdk_install --stack api35_legacy
                ;;
            5)
                atm_android_sdk_install --stack api34
                ;;
            6)
                printf 'Platform [android-36]: '
                read -r platform
                platform="${platform:-android-36}"

                printf 'Build tools [36.0.0]: '
                read -r build_tools
                build_tools="${build_tools:-36.0.0}"

                printf 'CMake [3.31.6]: '
                read -r cmake
                cmake="${cmake:-3.31.6}"

                printf 'NDK [29.0.14206865]: '
                read -r ndk
                ndk="${ndk:-29.0.14206865}"

                atm_android_sdk_install \
                    --platform "$platform" \
                    --build-tools "$build_tools" \
                    --cmake "$cmake" \
                    --ndk "$ndk"
                ;;
            7)
                atm_android_sdk_list_installed_packages
                ;;
            8)
                printf '%s ' "$(atm_t ATM_PLUGIN_ANDROID_SDK_ENTER_PACKAGE)"
                read -r package
                [[ -n "$package" ]] || {
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    continue
                }
                atm_android_sdk_remove "$package"
                ;;
            9)
                atm_android_sdk_uninstall
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

atm_android_sdk_path_entries() {
    local sdk_root=""

    sdk_root="$(atm_android_sdk_root)"

    printf '%s/cmdline-tools/latest/bin\n' "$sdk_root"
    printf '%s/platform-tools\n' "$sdk_root"
    printf '%s/emulator\n' "$sdk_root"

    if [[ -d "$sdk_root/cmake" ]]; then
        find "$sdk_root/cmake" -maxdepth 2 -type d -name bin 2>/dev/null | sort -V | tail -n 1
    fi
}

#!/usr/bin/env bash

# ==========================================================
# ATM Desktop Launcher Core
#
# Simple user-space .desktop installer.
#
# Rules:
#   - No sudo.
#   - No system/global desktop entries.
#   - No icon theme installation.
#   - Desktop files live inside each plugin.
#   - Plugins copy their own .desktop files to:
#       ~/.local/share/applications
# ==========================================================

atm_desktop_dir() {
    printf '%s\n' "${ATM_DESKTOP_DIR:-$HOME/.local/share/applications}"
}

atm_desktop_update_database() {
    local dir=""

    dir="$(atm_desktop_dir)"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: update-desktop-database %s\n' "$dir"
        return 0
    fi

    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database "$dir" >/dev/null 2>&1 || true
    fi
}

atm_desktop_install_file() {
    local source_file="$1"
    local target_name="${2:-}"
    local dir=""
    local target=""

    [[ -f "$source_file" ]] || atm_fail "Desktop source file not found: $source_file"

    if [[ -z "$target_name" ]]; then
        target_name="$(basename "$source_file")"
    fi

    [[ "$target_name" == *.desktop ]] || atm_fail "Desktop target must end with .desktop: $target_name"

    dir="$(atm_desktop_dir)"
    target="$dir/$target_name"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: mkdir -p %s\n' "$dir"
        printf 'DRY-RUN: install -m 644 %s %s\n' "$source_file" "$target"
        printf 'DRY-RUN: update-desktop-database %s\n' "$dir"
        return 0
    fi

    mkdir -p "$dir"
    install -m 644 "$source_file" "$target"
    atm_desktop_update_database

    atm_success "Desktop launcher installed: $target"
}

atm_desktop_remove_file() {
    local target_name="$1"
    local dir=""
    local target=""

    [[ -n "$target_name" ]] || atm_fail "Desktop target name is required."
    [[ "$target_name" == *.desktop ]] || atm_fail "Desktop target must end with .desktop: $target_name"

    dir="$(atm_desktop_dir)"
    target="$dir/$target_name"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -f %s\n' "$target"
        printf 'DRY-RUN: update-desktop-database %s\n' "$dir"
        return 0
    fi

    rm -f "$target"
    atm_desktop_update_database

    atm_success "Desktop launcher removed: $target"
}

atm_desktop_file_exists() {
    local target_name="$1"
    local target=""

    target="$(atm_desktop_dir)/$target_name"

    [[ -f "$target" ]]
}
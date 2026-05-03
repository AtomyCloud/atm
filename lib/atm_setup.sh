#!/usr/bin/env bash

ATM_PORTABLE_HOME="${ATM_PORTABLE_HOME:-$HOME/Apps/atm}"

atm_setup_current_home() {
    printf '%s\n' "$ATM_HOME"
}

atm_setup_portable_home() {
    printf '%s\n' "$ATM_PORTABLE_HOME"
}

atm_setup_portable_command() {
    printf '%s/bin/atm\n' "$(atm_setup_portable_home)"
}

atm_setup_current_command() {
    printf '%s/bin/atm\n' "$(atm_setup_current_home)"
}

atm_setup_system_target() {
    printf '/usr/local/bin/atm\n'
}

atm_setup_detect_command_mode() {
    local portable_home=""
    local portable_command=""
    local system_target=""
    local resolved_system=""
    local resolved_portable=""

    portable_home="$(atm_setup_portable_home)"
    portable_command="$(atm_setup_portable_command)"
    system_target="$(atm_setup_system_target)"

    if command -v readlink >/dev/null 2>&1; then
        resolved_portable="$(readlink -f "$portable_command" 2>/dev/null || true)"
        resolved_system="$(readlink -f "$system_target" 2>/dev/null || true)"
    fi

    if [[ -n "$resolved_system" && -n "$resolved_portable" && "$resolved_system" == "$resolved_portable" ]]; then
        printf 'system\n'
        return 0
    fi

    if echo "$PATH" | tr ':' '\n' | grep -qx "$portable_home/bin"; then
        printf 'portable\n'
        return 0
    fi

    if [[ "$ATM_HOME" == "$portable_home" ]]; then
        printf 'portable-source\n'
        return 0
    fi

    printf 'custom\n'
}

atm_setup_validate_current_source() {
    local current_command=""

    current_command="$(atm_setup_current_command)"

    [[ -f "$current_command" ]] || atm_fail "ATM command source not found: $current_command"
    [[ -x "$current_command" ]] || atm_fail "ATM command source is not executable: $current_command"
    [[ -d "$ATM_HOME/lib" ]] || atm_fail "ATM lib directory not found: $ATM_HOME/lib"
    [[ -d "$ATM_HOME/plugins" ]] || atm_fail "ATM plugins directory not found: $ATM_HOME/plugins"
    [[ -d "$ATM_HOME/lang" ]] || atm_fail "ATM lang directory not found: $ATM_HOME/lang"
}

atm_setup_install_project_to_portable_home() {
    local source_home=""
    local target_home=""
    local tmp_home=""

    source_home="$(atm_setup_current_home)"
    target_home="$(atm_setup_portable_home)"

    atm_setup_validate_current_source

    if [[ "$source_home" == "$target_home" ]]; then
        atm_debug "ATM already running from portable home: $target_home"
        return 0
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: install ATM project from %s to %s\n' "$source_home" "$target_home"
        return 0
    fi

    mkdir -p "$(dirname "$target_home")"

    tmp_home="${target_home}.tmp.$$"
    rm -rf "$tmp_home"
    mkdir -p "$tmp_home"

    if command -v rsync >/dev/null 2>&1; then
        rsync -a --delete \
            --exclude='.git' \
            --exclude='*.tmp.*' \
            "$source_home"/ "$tmp_home"/
    else
        cp -a "$source_home"/. "$tmp_home"/
        rm -rf "$tmp_home/.git"
    fi

    chmod +x "$tmp_home/bin/atm"

    rm -rf "$target_home"
    mv "$tmp_home" "$target_home"
}

atm_setup_path_block_begin="# >>> ATM command >>>"
atm_setup_path_block_end="# <<< ATM command <<<"

atm_setup_generate_portable_path_block() {
    local portable_home=""

    portable_home="$(atm_setup_portable_home)"

    cat <<EOF
$atm_setup_path_block_begin
export PATH="$portable_home/bin:\$PATH"
$atm_setup_path_block_end
EOF
}

atm_setup_upsert_shell_file() {
    local file="$1"
    local block="$2"
    local tmp=""

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: update ATM command PATH block in %s\n' "$file"
        printf '%s\n' "$block"
        return 0
    fi

    mkdir -p "$(dirname "$file")"
    tmp="$(mktemp)"

    if [[ -f "$file" ]]; then
        awk -v begin="$atm_setup_path_block_begin" -v end="$atm_setup_path_block_end" '
            $0 == begin { skip=1; next }
            $0 == end { skip=0; next }
            skip != 1 { print }
        ' "$file" > "$tmp"
    fi

    {
        cat "$tmp"
        printf '\n%s\n' "$block"
    } > "${tmp}.new"

    mv "${tmp}.new" "$file"
    rm -f "$tmp"
}

atm_setup_install_portable() {
    local block=""
    local portable_command=""

    atm_setup_install_project_to_portable_home

    block="$(atm_setup_generate_portable_path_block)"
    portable_command="$(atm_setup_portable_command)"

    atm_setup_upsert_shell_file "$HOME/.bashrc" "$block"

    if [[ -f "$HOME/.zshrc" ]]; then
        atm_setup_upsert_shell_file "$HOME/.zshrc" "$block"
    fi

    atm_success "$(atm_t ATM_MSG_SETUP_PORTABLE_DONE)"
    printf 'ATM home:     %s\n' "$(atm_setup_portable_home)"
    printf 'Command path: %s\n' "$portable_command"
    printf 'Added to:     %s\n' "$HOME/.bashrc"

    if [[ -f "$HOME/.zshrc" ]]; then
        printf 'Added to:     %s\n' "$HOME/.zshrc"
    fi

    printf '\nRun now:\n'
    printf '  source ~/.bashrc\n'
    printf '  atm --version\n'
}

atm_setup_install_system() {
    local target=""
    local portable_command=""

    atm_setup_install_project_to_portable_home

    target="$(atm_setup_system_target)"
    portable_command="$(atm_setup_portable_command)"

    [[ -f "$portable_command" ]] || atm_fail "Portable ATM command not found after install: $portable_command"
    [[ -x "$portable_command" ]] || atm_fail "Portable ATM command is not executable: $portable_command"

    if [[ -e "$target" && ! -L "$target" ]]; then
        atm_fail "Target exists and is not a symlink: $target"
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$portable_command" "$target"
        return 0
    fi

    if [[ -w "$(dirname "$target")" ]]; then
        ln -sfn "$portable_command" "$target"
    elif command -v sudo >/dev/null 2>&1; then
        sudo ln -sfn "$portable_command" "$target"
    else
        atm_fail "No write permission for $(dirname "$target") and sudo is not available."
    fi

    atm_success "$(atm_t ATM_MSG_SETUP_SYSTEM_DONE): $target"
    printf 'ATM home: %s\n' "$(atm_setup_portable_home)"
    printf 'Source:   %s\n' "$portable_command"
    printf 'Target:   %s\n' "$target"
}

atm_setup_show_status() {
    local current_home=""
    local portable_home=""
    local portable_command=""
    local system_target=""

    current_home="$(atm_setup_current_home)"
    portable_home="$(atm_setup_portable_home)"
    portable_command="$(atm_setup_portable_command)"
    system_target="$(atm_setup_system_target)"

    printf 'Current mode: %s\n' "$(atm_setup_detect_command_mode)"
    printf 'Running from: %s\n' "$current_home"
    printf 'Portable:     %s\n' "$portable_command"
    printf 'Portable PATH:%s\n' "$portable_home/bin"
    printf 'System:       %s\n' "$system_target"

    if command -v atm >/dev/null 2>&1; then
        printf 'Command:      %s\n' "$(command -v atm)"
    else
        printf 'Command:      not found in PATH\n'
    fi
}

atm_setup_menu() {
    local choice=""

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    ⚙️  %s\n' "$(atm_t ATM_MENU_SETUP_ATM)"
        printf '%s\n' "=========================================="
        atm_setup_show_status
        printf '%s\n' "------------------------------------------"
        printf '1) %s\n' "$(atm_t ATM_MENU_SETUP_PORTABLE)"
        printf '2) %s\n' "$(atm_t ATM_MENU_SETUP_SYSTEM)"
        printf '3) %s\n' "$(atm_t ATM_MENU_SETUP_STATUS)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            1)
                atm_setup_install_portable
                ;;
            2)
                atm_setup_install_system
                ;;
            3)
                atm_setup_show_status
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

atm_setup_cli() {
    local mode="${1:-menu}"

    case "$mode" in
        menu)
            atm_setup_menu
            ;;
        portable)
            atm_setup_install_portable
            ;;
        system)
            atm_setup_install_system
            ;;
        status)
            atm_setup_show_status
            ;;
        *)
            atm_fail "Usage: atm setup [portable|system|status]"
            ;;
    esac
}
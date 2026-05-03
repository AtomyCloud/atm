#!/usr/bin/env bash

atm_path_block_begin="# >>> ATM - Atomy Tools Modules >>>"
atm_path_block_end="# <<< ATM - Atomy Tools Modules <<<"

atm_path_system_profile_file() {
    printf '%s\n' "${ATM_SYSTEM_PROFILE_FILE:-/etc/profile.d/atm.sh}"
}

atm_path_user_profile_file() {
    printf '%s\n' "$HOME/.profile"
}

atm_path_user_bashrc_file() {
    printf '%s\n' "$HOME/.bashrc"
}

atm_path_user_zshrc_file() {
    printf '%s\n' "$HOME/.zshrc"
}

atm_path_collect_entries() {
    local id=""
    local func=""

    printf '%s\n' "$HOME/.local/bin"
    printf '%s\n' "$ATM_HOME/bin"

    for id in $(atm_plugin_list_sorted_ids); do
        func="${ATM_PLUGIN_PATH_FUNC[$id]:-}"

        if [[ -n "$func" ]] && declare -F "$func" >/dev/null 2>&1; then
            "$func"
        fi
    done
}

atm_path_generate_block() {
    local entries=()
    local entry=""
    local path_line=""

    mapfile -t entries < <(atm_path_collect_entries | awk 'NF && !seen[$0]++')

    for entry in "${entries[@]}"; do
        path_line="${path_line:+$path_line:}$entry"
    done

    cat <<EOF
$atm_path_block_begin
export ATM_HOME="$ATM_HOME"
export ATM_APPS_DIR="$ATM_APPS_DIR"
export ATM_CACHE_DIR="$ATM_CACHE_DIR"
export ATM_STATE_DIR="$ATM_STATE_DIR"
export ATM_CONFIG_DIR="$ATM_CONFIG_DIR"

export JAVA_HOME="$ATM_APPS_DIR/Java/current"
export ANDROID_HOME="$ATM_APPS_DIR/AndroidStudio/SDK"
export ANDROID_SDK_ROOT="$ATM_APPS_DIR/AndroidStudio/SDK"
export ANDROID_STUDIO_HOME="$ATM_APPS_DIR/AndroidStudio/current"
export VSCODE_HOME="$ATM_APPS_DIR/vscode/current"
export FLUTTER_HOME="$ATM_APPS_DIR/flutter/current"
export GOROOT="$ATM_APPS_DIR/go/current"
export GOPATH="$ATM_APPS_DIR/go/workspace"

export PATH="$path_line:\$PATH"
$atm_path_block_end
EOF
}

atm_path_upsert_file() {
    local file="$1"
    local block="$2"
    local tmp=""

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: update PATH block in %s\n' "$file"
        return 0
    fi

    mkdir -p "$(dirname "$file")"
    tmp="$(mktemp)"

    if [[ -f "$file" ]]; then
        awk -v begin="$atm_path_block_begin" -v end="$atm_path_block_end" '
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

atm_path_write_system_profile() {
    local block="$1"
    local file=""

    if [[ "${ATM_PATH_WRITE_SYSTEM_PROFILE:-0}" != "1" ]]; then
        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: skip system profile; set ATM_PATH_WRITE_SYSTEM_PROFILE=1 to enable\n'
        fi
        return 0
    fi

    file="$(atm_path_system_profile_file)"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: write system profile %s\n' "$file"
        printf 'DRY-RUN: sudo tee %s >/dev/null\n' "$file"
        return 0
    fi

    if [[ -w "$(dirname "$file")" ]]; then
        printf '%s\n' "$block" > "$file"
        atm_success "System PATH configured: $file"
        return 0
    fi

    if command -v sudo >/dev/null 2>&1; then
        printf '%s\n' "$block" | sudo tee "$file" >/dev/null
        atm_success "System PATH configured: $file"
        return 0
    fi

    atm_warn "sudo not found; cannot write system profile: $file"
}

atm_path_apply_user_profiles() {
    local block="$1"
    local file=""

    file="$(atm_path_user_profile_file)"
    atm_path_upsert_file "$file" "$block"
    atm_success "User PATH configured: $file"

    file="$(atm_path_user_bashrc_file)"
    atm_path_upsert_file "$file" "$block"
    atm_success "User PATH configured: $file"

    file="$(atm_path_user_zshrc_file)"
    if [[ -f "$file" ]]; then
        atm_path_upsert_file "$file" "$block"
        atm_success "User PATH configured: $file"
    fi
}

atm_path_ensure_vscode_cli() {
    local vscode_code=""
    local code_link=""

    vscode_code="$ATM_APPS_DIR/vscode/current/bin/code"
    code_link="$HOME/.local/bin/code"

    if [[ ! -x "$vscode_code" ]]; then
        atm_warn "VS Code CLI source not found, skipping code symlink: $vscode_code"
        return 0
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: mkdir -p %s\n' "$HOME/.local/bin"
        printf 'DRY-RUN: ln -sfn %s %s\n' "$vscode_code" "$code_link"
        return 0
    fi

    mkdir -p "$HOME/.local/bin"
    ln -sfn "$vscode_code" "$code_link"
    chmod 755 "$HOME/.local/bin"

    atm_success "VS Code CLI configured: $code_link -> $vscode_code"
}

atm_path_apply_plugin_desktops() {
    local old_scope="${ATM_DESKTOP_SCOPE:-}"

    # Opção 9 deve criar user + system launchers.
    # Não rode `sudo atm`; o próprio core chama sudo para o escopo system.
    export ATM_DESKTOP_SCOPE="all"

    if declare -F atm_plugin_run_all_desktop >/dev/null 2>&1; then
        atm_plugin_run_all_desktop
    else
        atm_warn "Desktop plugin runner not available."
    fi

    if [[ -n "$old_scope" ]]; then
        export ATM_DESKTOP_SCOPE="$old_scope"
    else
        unset ATM_DESKTOP_SCOPE
    fi
}

atm_path_apply() {
    local block=""

    block="$(atm_path_generate_block)"

    atm_path_apply_user_profiles "$block"
    atm_path_write_system_profile "$block"
    atm_path_ensure_vscode_cli
    atm_path_apply_plugin_desktops

    atm_success "$(atm_t ATM_MSG_PATH_APPLIED)"
    printf '\nRun now:\n'
    printf '  source ~/.bashrc\n'
    printf '  hash -r\n'
    printf '  atm --version\n'
    printf '  code --version\n'
}

atm_path_cli() {
    local subcmd="${1:-apply}"

    case "$subcmd" in
        apply)
            atm_path_apply
            ;;
        print)
            atm_path_generate_block
            ;;
        *)
            atm_fail "Unknown path command: $subcmd"
            ;;
    esac
}

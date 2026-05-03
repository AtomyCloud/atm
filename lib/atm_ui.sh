#!/usr/bin/env bash

atm_menu_detect_mode() {
    if declare -F atm_setup_detect_command_mode >/dev/null 2>&1; then
        atm_setup_detect_command_mode
        return 0
    fi

    if [[ "$ATM_HOME" == "$HOME/Apps/atm" ]]; then
        printf 'portable-source\n'
    elif [[ -L "/usr/local/bin/atm" ]]; then
        printf 'system-command\n'
    else
        printf 'custom\n'
    fi
}

atm_menu_header() {
    clear
    printf '%s\n' "=========================================="
    printf '    🚀 %s v%s\n' "$(atm_t ATM_MENU_APP_TITLE)" "$ATM_VERSION"
    printf '%s\n' "=========================================="
    printf '%-9s %s\n' "$(atm_t ATM_MENU_MODE):" "$(atm_menu_detect_mode)"
    printf '%-9s %s\n' "$(atm_t ATM_MENU_BASE_DIR):" "$ATM_APPS_DIR"
    printf '%-9s %s\n' "$(atm_t ATM_MENU_CACHE_DIR):" "$ATM_CACHE_DIR"
    printf '%-9s %s\n' "$(atm_t ATM_MENU_PLUGINS_DIR):" "$ATM_PLUGIN_DIR"
    printf '%-9s %s\n' "$(atm_t ATM_MENU_LANG):" "$ATM_LANG"
    printf '%s\n' "------------------------------------------"
}

atm_prompt_continue() {
    printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
    read -r -n 1 _ || true
    printf '\n'
}

atm_menu_main() {
    local choice=""
    local idx=1
    local id=""
    local ids=()
    local map_ids=()

    while true; do
        idx=1
        atm_menu_header

        ids=()
        mapfile -t ids < <(atm_plugin_list_sorted_ids)
        map_ids=()

        for id in "${ids[@]}"; do
            printf '%s) %s %-32s %s\n' \
                "$idx" \
                "${ATM_PLUGIN_ICON[$id]}" \
                "${ATM_PLUGIN_NAME[$id]}" \
                "$(atm_plugin_status "$id")"

            map_ids[$idx]="$id"
            idx=$((idx + 1))
        done

        printf '%s\n' "------------------------------------------"
        printf '8) ⚡ %s\n' "$(atm_t ATM_MENU_FULL_SETUP)"
        printf '9) 🛠️  %s\n' "$(atm_t ATM_MENU_PATH)"
        printf 's) ⚙️  %s\n' "$(atm_t ATM_MENU_SETUP_ATM)"
        printf 'p) 🔌 %s\n' "$(atm_t ATM_MENU_PLUGINS)"
        printf 'd) 🩺 %s\n' "$(atm_t ATM_MENU_DOCTOR)"
        printf 'u) ♻️  %s\n' "$(atm_t ATM_MENU_SELF_UPDATE)"
        printf 'q) ❌ %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s\n' "------------------------------------------"

        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            q|Q)
                atm_success "$(atm_t ATM_MSG_BYE)"
                exit 0
                ;;
            8)
                atm_full_setup
                atm_prompt_continue
                ;;
            9)
                atm_path_apply
                atm_prompt_continue
                ;;
            s|S)
                atm_setup_menu
                ;;
            p|P)
                atm_plugins_cli list
                atm_prompt_continue
                ;;
            d|D)
                atm_doctor_run
                atm_prompt_continue
                ;;
            u|U)
                atm_self_update_cli --check
                atm_prompt_continue
                ;;
            ''|*[!0-9]*)
                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                sleep 1
                ;;
            *)
                if [[ -n "${map_ids[$choice]:-}" ]]; then
                    atm_plugin_run_menu "${map_ids[$choice]}"
                    atm_prompt_continue
                else
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                    sleep 1
                fi
                ;;
        esac
    done
}

atm_install_cli() {
    local plugin="${1:-}"

    if [[ -z "$plugin" ]]; then
        atm_install_menu
        return 0
    fi

    shift || true

    atm_plugin_exists "$plugin" || atm_fail "Plugin not found: $plugin"
    atm_plugin_run_install "$plugin" "$@"
}

atm_install_menu() {
    local idx=1
    local id=""
    local ids=()
    local map_ids=()
    local choice=""

    while true; do
        idx=1
        atm_menu_header
        printf '%s\n' "$(atm_t ATM_MENU_INSTALLABLE_PLUGINS)"
        printf '%s\n' "------------------------------------------"

        mapfile -t ids < <(atm_plugin_list_sorted_ids)

        for id in "${ids[@]}"; do
            printf '%s) %s %s\n' "$idx" "${ATM_PLUGIN_ICON[$id]}" "${ATM_PLUGIN_NAME[$id]}"
            map_ids[$idx]="$id"
            idx=$((idx + 1))
        done

        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            b|B)
                return 0
                ;;
            q|Q)
                exit 0
                ;;
            ''|*[!0-9]*)
                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
            *)
                if [[ -n "${map_ids[$choice]:-}" ]]; then
                    atm_plugin_run_install "${map_ids[$choice]}"
                    return 0
                fi

                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
        esac
    done
}

atm_full_setup() {
    local id=""

    for id in $(atm_plugin_list_sorted_ids); do
        [[ "${ATM_PLUGIN_FULL_SETUP[$id]:-0}" == "1" ]] || continue
        atm_info "▶ ${ATM_PLUGIN_NAME[$id]}"
        atm_plugin_run_install "$id"
    done
}
#!/usr/bin/env bash

atm_backup_restore_state() {
    atm_require_commands python3
    PYTHONDONTWRITEBYTECODE=1 python3 "${BASH_SOURCE[0]%/*}/state.py" "$@" \
        --home "$ATM_BACKUP_RESTORE_HOME" --excludes "$ATM_BACKUP_RESTORE_EXCLUDES"
}

atm_backup_restore_status() {
    if command -v python3 >/dev/null 2>&1; then
        atm_t ATM_PLUGIN_BACKUP_RESTORE_READY
    else
        atm_t ATM_PLUGIN_BACKUP_RESTORE_DEPENDENCY
    fi
}

atm_backup_restore_confirm() {
    local answer=""
    printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CONFIRM)"
    read -r answer || return 1
    case "$answer" in y|Y|yes|YES) return 0 ;; esac
    return 1
}

atm_backup_restore_run() {
    local action="$1" dry_args=()
    shift
    if [[ -n "${SUDO_USER:-}" && "${SUDO_USER:-}" != root ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_USER_ONLY)"
        return 1
    fi
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then dry_args=(--dry-run); fi
    case "$action" in
        backup|restore)
            printf '%s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_WARNING)"
            if [[ "$action" == restore ]]; then
                printf '%s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_RESTORE_WARNING)"
            fi
            # Validate and show paths before asking, without creating files or logs.
            if ! atm_backup_restore_state "$action" "$@" --dry-run; then return 1; fi
            if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then return 0; fi
            if ! atm_backup_restore_confirm; then
                atm_info "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CANCELLED)"
                return 0
            fi
            ;;
        list|verify) ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_ARGS)"; return 1 ;;
    esac
    if ! atm_backup_restore_state "$action" "$@" "${dry_args[@]}"; then
        atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_FAILED)"
        return 1
    fi
    if [[ "$action" == backup || "$action" == restore ]]; then
        atm_success "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_DONE)"
    fi
}

atm_backup_restore_install() {
    local action="" kind="safe" destination="$ATM_BACKUP_RESTORE_DIR"
    local args=()
    if (($# == 0)); then atm_backup_restore_menu; return; fi
    while (($# > 0)); do
        case "$1" in
            --action|--type|--destination|--path|--source|--target|--user)
                if (($# < 2)); then atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_ARGS)"; return 1; fi
                case "$1" in
                    --action) action="$2" ;;
                    --type) kind="$2" ;;
                    --destination) destination="$2" ;;
                    *) args+=("$1" "$2") ;;
                esac
                shift 2 ;;
            *) atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_ARGS)"; return 1 ;;
        esac
    done
    atm_backup_restore_run "$action" --destination "$destination" --type "$kind" "${args[@]}"
}

atm_backup_restore_prompt_backup() {
    local kind="$1" destination="" path="" args=()
    printf '%s [%s] ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_DESTINATION)" "$ATM_BACKUP_RESTORE_DIR"
    read -r destination || return 0
    destination="${destination:-$ATM_BACKUP_RESTORE_DIR}"
    if [[ "$kind" == custom ]]; then
        while true; do
            printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_PATH)"
            read -r path || return 0
            [[ -n "$path" ]] || break
            args+=(--path "$path")
        done
    fi
    atm_backup_restore_run backup --destination "$destination" --type "$kind" "${args[@]}"
}

atm_backup_restore_prompt_source() {
    local action="$1" source="" target="" user="" args=()
    printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_SOURCE)"
    read -r source || return 0
    [[ -n "$source" ]] || return 0
    printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_USER)"
    read -r user || return 0
    if [[ -n "$user" ]]; then args+=(--user "$user"); fi
    if [[ "$action" == restore ]]; then
        printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_TARGET)"
        read -r target || return 0
        [[ -n "$target" ]] || return 0
        args+=(--target "$target")
    fi
    atm_backup_restore_run "$action" --destination "$ATM_BACKUP_RESTORE_DIR" --source "$source" "${args[@]}"
}

atm_backup_restore_list() {
    local output=""
    output="$(atm_backup_restore_state list --destination "$ATM_BACKUP_RESTORE_DIR")" || return 1
    if [[ -n "$output" ]]; then printf '%s\n' "$output"; else atm_info "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_NO_BACKUPS)"; fi
}

atm_backup_restore_path_entries() { return 0; }
atm_backup_restore_remove() { atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_UNSUPPORTED)"; }
atm_backup_restore_uninstall() { atm_backup_restore_remove; }
atm_backup_restore_use() { atm_backup_restore_remove; }

atm_backup_restore_menu() {
    local choice=""
    while true; do
        printf '\n%s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_TITLE)"
        printf '%s\n' "$(atm_backup_restore_status)"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_SAFE)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_FULL)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_MAIN)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CUSTOM)"
        printf '5) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_RESTORE)"
        printf '6) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_LIST)"
        printf '7) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_VERIFY)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_backup_restore_prompt_backup safe || true ;;
            2) atm_backup_restore_prompt_backup full || true ;;
            3) atm_backup_restore_prompt_backup main || true ;;
            4) atm_backup_restore_prompt_backup custom || true ;;
            5) atm_backup_restore_prompt_source restore || true ;;
            6) atm_backup_restore_list || true ;;
            7) atm_backup_restore_prompt_source verify || true ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

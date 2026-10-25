#!/usr/bin/env bash

atm_backup_restore_state() {
    atm_require_commands python3
    PYTHONDONTWRITEBYTECODE=1 python3 "${BASH_SOURCE[0]%/*}/state.py" "$@" \
        --home "$ATM_BACKUP_RESTORE_HOME" --excludes "$ATM_BACKUP_RESTORE_EXCLUDES" \
        --home-base "$ATM_BACKUP_RESTORE_HOME_BASE" --media-root "$ATM_BACKUP_RESTORE_MEDIA_ROOT" \
        --mountinfo "$ATM_BACKUP_RESTORE_MOUNTINFO"
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
            --all-users) args+=(--all-users); shift ;;
            --action|--type|--destination|--path|--source|--target|--user|--mount-id)
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

atm_backup_restore_select_users() {
    local action="$1" source="${2:-}" rows="" name="" path="" choice="" idx=1
    local names=()
    ATM_BACKUP_RESTORE_SELECTION_ARGS=()
    if [[ "$action" == backup ]]; then
        rows="$(atm_backup_restore_state users)" || return 1
    else
        rows="$(atm_backup_restore_state backup-users --source "$source")" || return 1
    fi
    while IFS=$'\t' read -r name path; do
        [[ -n "$name" ]] && names+=("$name")
    done <<< "$rows"
    while true; do
        printf '\n%s\n==============\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_USER_SELECTION)"
        printf '0) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_ALL_USERS)"
        idx=1
        for name in "${names[@]}"; do printf '%s) %s\n' "$idx" "$name"; idx=$((idx + 1)); done
        printf 'm) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_MANUAL_USER)"
        printf 'q) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CANCEL)"
        printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_USER_CHOICE)"
        read -r choice || return 1
        case "$choice" in
            ''|q|Q) return 1 ;;
            0)
                if ((${#names[@]} == 0)); then
                    atm_warn "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_NO_USERS)"; continue
                fi
                ATM_BACKUP_RESTORE_SELECTION_ARGS=(--all-users)
                return 0 ;;
            m|M)
                printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_USERNAME)"
                read -r name || return 1
                [[ -n "$name" ]] || continue
                for path in "${names[@]}"; do
                    if [[ "$path" == "$name" ]]; then
                        ATM_BACKUP_RESTORE_SELECTION_ARGS=(--user "$name"); return 0
                    fi
                done ;;
            *)
                idx=1
                for name in "${names[@]}"; do
                    if [[ "$choice" == "$idx" ]]; then
                        ATM_BACKUP_RESTORE_SELECTION_ARGS=(--user "$name"); return 0
                    fi
                    idx=$((idx + 1))
                done ;;
        esac
        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
    done
}

atm_backup_restore_select_type() {
    local choice=""
    while true; do
        printf '\n%s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_BACKUP_TYPE)"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_FULL)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_MAIN)"
        printf '3) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_SAFE)"
        printf '4) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CUSTOM)"
        printf 'q) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CANCEL)"
        printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_TYPE_CHOICE)"
        read -r choice || return 1
        case "$choice" in
            1) ATM_BACKUP_RESTORE_SELECTED_TYPE=full ;;
            2) ATM_BACKUP_RESTORE_SELECTED_TYPE=main ;;
            ''|3) ATM_BACKUP_RESTORE_SELECTED_TYPE=safe ;;
            4) ATM_BACKUP_RESTORE_SELECTED_TYPE=custom ;;
            q|Q) return 1 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        return 0
    done
}

atm_backup_restore_select_destination() {
    local rows="" path="" source="" fstype="" free="" mount_id="" writable="" choice="" idx=1 marker=""
    local paths=() mount_ids=() writable_flags=()
    ATM_BACKUP_RESTORE_DESTINATION_ARGS=()
    rows="$(atm_backup_restore_state devices)" || return 1
    while IFS=$'\t' read -r path source fstype free mount_id writable; do
        [[ -n "$path" ]] || continue
        paths+=("$path"); mount_ids+=("$mount_id"); writable_flags+=("$writable")
    done <<< "$rows"
    while true; do
        printf '\n%s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_DESTINATION_SELECTION)"
        printf '0) %s: %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_ATM_PATH)" "$ATM_BACKUP_RESTORE_DIR"
        idx=1
        while IFS=$'\t' read -r path source fstype free mount_id writable; do
            [[ -n "$path" ]] || continue
            marker=""
            if [[ "$writable" != 1 ]]; then marker=" $(atm_t ATM_PLUGIN_BACKUP_RESTORE_READ_ONLY)"; fi
            printf '%s) %s [%s, %s, %s]%s\n' "$idx" "$path" "$source" "$fstype" "$free" "$marker"
            idx=$((idx + 1))
        done <<< "$rows"
        printf 'm) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_MANUAL_PATH)"
        printf 'q) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CANCEL)"
        printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_DESTINATION_CHOICE)"
        read -r choice || return 1
        case "$choice" in
            ''|0) ATM_BACKUP_RESTORE_DESTINATION_ARGS=(--destination "$ATM_BACKUP_RESTORE_DIR"); return 0 ;;
            q|Q) return 1 ;;
            m|M)
                printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_DESTINATION)"
                read -r path || return 1
                [[ -n "$path" ]] || continue
                ATM_BACKUP_RESTORE_DESTINATION_ARGS=(--destination "$path"); return 0 ;;
        esac
        for ((idx=0; idx<${#paths[@]}; idx++)); do
            if [[ "$choice" == "$((idx + 1))" ]]; then
                if [[ "${writable_flags[$idx]}" == 1 ]]; then
                    ATM_BACKUP_RESTORE_DESTINATION_ARGS=(--destination "${paths[$idx]}" --mount-id "${mount_ids[$idx]}")
                    return 0
                fi
                break
            fi
        done
        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
    done
}

atm_backup_restore_prompt_backup() {
    local kind="" path="" args=()
    atm_backup_restore_select_users backup || return 1
    args=("${ATM_BACKUP_RESTORE_SELECTION_ARGS[@]}")
    atm_backup_restore_select_type || return 1
    kind="$ATM_BACKUP_RESTORE_SELECTED_TYPE"
    atm_backup_restore_select_destination || return 1
    args+=("${ATM_BACKUP_RESTORE_DESTINATION_ARGS[@]}")
    if [[ "$kind" == custom ]]; then
        while true; do
            printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_PATH)"
            read -r path || return 0
            [[ -n "$path" ]] || break
            args+=(--path "$path")
        done
    fi
    atm_backup_restore_run backup --type "$kind" "${args[@]}"
}

atm_backup_restore_prompt_source() {
    local action="$1" source="" target="" args=()
    printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_SOURCE)"
    read -r source || return 0
    [[ -n "$source" ]] || return 0
    atm_backup_restore_select_users restore "$source" || return 1
    args=("${ATM_BACKUP_RESTORE_SELECTION_ARGS[@]}")
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
        printf '%s 0.0.1\n\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_VERSION)"
        printf '%s\n=========\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_MAIN_MENU)"
        printf '1) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_CREATE)"
        printf '2) %s\n' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_RESTORE)"
        printf '0) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_PLUGIN_BACKUP_RESTORE_MAIN_CHOICE)"
        read -r choice || return 0
        case "$choice" in
            1) atm_backup_restore_prompt_backup || continue ;;
            2) atm_backup_restore_prompt_source restore || continue ;;
            ''|0|b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

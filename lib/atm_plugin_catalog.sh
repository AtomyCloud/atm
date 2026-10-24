#!/usr/bin/env bash

atm_plugins_official_source() {
    printf '%s\n' 'https://github.com/AtomyCloud/atm/tree/main/plugins'
}

atm_plugins_catalog_rows() {
    atm_require_commands python3
    python3 -B "$ATM_HOME/lib/plugin_catalog.py" search -- "${1:-}"
}

atm_plugins_local_present() {
    [[ -e "$ATM_PLUGIN_DIR/$1" || -L "$ATM_PLUGIN_DIR/$1" ]] || atm_plugin_exists "$1"
}

atm_plugins_print_results() {
    local rows="$1" numbered="${2:-0}" idx=1 id="" name="" version="" description="" revision="" state=""
    printf '%s\n' "$(atm_plugins_official_source)"
    while IFS=$'\t' read -r id name version description revision; do
        [[ -n "$id" ]] || continue
        state="$(atm_t ATM_PLUGINS_AVAILABLE)"
        if atm_plugins_local_present "$id"; then state="$(atm_t ATM_PLUGINS_LOCAL)"; fi
        if [[ "$numbered" == 1 ]]; then printf '%s) ' "$idx"; fi
        printf '%s | %s | %s | %s\n  %s\n' "$id" "$name" "$version" "$state" "$description"
        idx=$((idx + 1))
    done <<< "$rows"
}

atm_plugins_remote_search() {
    local rows=""
    if ! rows="$(atm_plugins_catalog_rows "${1:-}")"; then
        atm_warn "$(atm_t ATM_PLUGINS_SEARCH_FAILED)"
        return 1
    fi
    if [[ -z "$rows" ]]; then atm_info "$(atm_t ATM_PLUGINS_NO_MATCHES)"; return 0; fi
    atm_plugins_print_results "$rows"
}

atm_plugins_remote_download() {
    local id="${1:-}" revision="${2:-}" answer="" destination=""
    local args=()
    [[ "$id" =~ ^[a-z][a-z0-9_-]{0,63}$ ]] || { atm_warn "$(atm_t ATM_PLUGINS_INVALID_ID)"; return 1; }
    if atm_plugins_local_present "$id"; then
        atm_warn "$(atm_t ATM_PLUGINS_EXISTS): $id"
        return 0
    fi
    printf '%s\n%s\n' "$(atm_t ATM_PLUGINS_SOURCE)" "$(atm_plugins_official_source)"
    printf '%s %s\n' "$(atm_t ATM_PLUGINS_TARGET)" "$ATM_PLUGIN_DIR/$id"
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: %s %s\n' "$(atm_t ATM_PLUGINS_DOWNLOAD_PLAN)" "$id"
        return 0
    fi
    printf '%s\n' "$(atm_t ATM_PLUGINS_TRUST_WARNING)"
    printf '%s ' "$(atm_t ATM_PLUGINS_CONFIRM)"
    read -r answer || return 0
    case "$answer" in
        y|Y|yes|YES) ;;
        *) atm_warn "$(atm_t ATM_PLUGINS_CANCELLED)"; return 0 ;;
    esac
    atm_require_commands python3 bash
    if [[ -n "$revision" ]]; then args+=(--revision "$revision"); fi
    if ! destination="$(python3 -B "$ATM_HOME/lib/plugin_catalog.py" download "$id" --root "$ATM_PLUGIN_DIR" "${args[@]}")"; then
        atm_warn "$(atm_t ATM_PLUGINS_DOWNLOAD_FAILED)"
        return 1
    fi
    atm_success "$(atm_t ATM_PLUGINS_DOWNLOADED): $destination"
    atm_info "$(atm_t ATM_PLUGINS_RESTART)"
}

atm_plugins_search_menu() {
    local query="" rows="" choice="" id="" name="" version="" description="" revision="" idx=1
    local ids=() revisions=()
    printf '%s\n%s\n' "$(atm_t ATM_PLUGINS_SEARCH_TITLE)" "$(atm_plugins_official_source)"
    printf '%s ' "$(atm_t ATM_PLUGINS_QUERY)"
    read -r query || return 0
    case "$query" in b|B) return 0 ;; esac
    if ! rows="$(atm_plugins_catalog_rows "$query")"; then
        atm_warn "$(atm_t ATM_PLUGINS_SEARCH_FAILED)"
        return 0
    fi
    if [[ -z "$rows" ]]; then atm_info "$(atm_t ATM_PLUGINS_NO_MATCHES)"; return 0; fi
    while IFS=$'\t' read -r id name version description revision; do
        [[ -n "$id" ]] || continue
        ids+=("$id")
        revisions+=("$revision")
    done <<< "$rows"
    atm_plugins_print_results "$rows" 1
    printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
    printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
    printf '%s ' "$(atm_t ATM_PLUGINS_SELECT_DOWNLOAD)"
    read -r choice || return 0
    case "$choice" in b|B) return 0 ;; q|Q) exit 0 ;; esac
    for ((idx=0; idx<${#ids[@]}; idx++)); do
        if [[ "$choice" == "$((idx + 1))" ]]; then
            atm_plugins_remote_download "${ids[$idx]}" "${revisions[$idx]}" || true
            return 0
        fi
    done
    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
}

atm_plugins_menu() {
    local choice=""
    while true; do
        clear || true
        printf '%s\n' "$(atm_t ATM_MENU_PLUGINS)"
        printf '1) %s\n' "$(atm_t ATM_PLUGINS_LIST_LOCAL)"
        printf '2) %s\n' "$(atm_t ATM_PLUGINS_SEARCH_TITLE)"
        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_plugins_cli list ;;
            2) atm_plugins_search_menu ;;
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; continue ;;
        esac
        atm_prompt_continue
    done
}

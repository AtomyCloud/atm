#!/usr/bin/env bash

atm_updates_t() {
    if [[ -n "${!1:-}" ]]; then atm_t "$1"; else ( source "$ATM_LANG_DIR/en-us.lang"; atm_t "$1"; ); fi
}

atm_updates_engine() {
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: component updates'; printf ' %q' "$@"; printf '; no network, probes or writes\n'
        return 0
    fi
    command -v python3 >/dev/null 2>&1 || { atm_warn "$(atm_updates_t ATM_UPDATES_UNKNOWN)"; return 1; }
    local run=()
    if [[ "${1:-}" == check ]] && command -v timeout >/dev/null 2>&1; then
        run=(timeout --kill-after=1 "${ATM_UPDATES_CHECK_TIMEOUT:-90}")
    fi
    ATM_DRY_RUN="${ATM_DRY_RUN:-0}" "${run[@]}" python3 -B "$ATM_HOME/lib/component_updates.py" \
        --home "$ATM_HOME" --plugins "$ATM_PLUGIN_DIR" --state "$ATM_STATE_DIR" --cache "$ATM_CACHE_DIR" "$@"
}

atm_updates_refresh() {
    local output="" key="$ATM_HOME|$ATM_PLUGIN_DIR" args=(check --ttl 300)
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then ATM_UPDATES_ROWS=""; return 0; fi
    if [[ "${1:-}" != --force && "${ATM_UPDATES_KEY:-}" == "$key" && "${ATM_UPDATES_AT:-}" =~ ^[0-9]+$ ]] \
        && ((SECONDS >= ATM_UPDATES_AT && SECONDS - ATM_UPDATES_AT < 60)); then return 0; fi
    ATM_UPDATES_KEY="$key" ATM_UPDATES_AT="$SECONDS" ATM_UPDATES_ROWS=""
    command -v python3 >/dev/null 2>&1 || return 0
    if [[ "${1:-}" == --force ]]; then
        args=(check)
    elif [[ "${ATM_UPDATES_AUTO_CHECK:-1}" != 1 ]] || ! command -v timeout >/dev/null 2>&1; then
        args=(cached)
    fi
    local ATM_UPDATES_CHECK_TIMEOUT="${ATM_UPDATES_CHECK_TIMEOUT:-8}"
    [[ "${1:-}" != --force ]] || ATM_UPDATES_CHECK_TIMEOUT=90
    if output="$(atm_updates_engine "${args[@]}" 2>/dev/null)"; then ATM_UPDATES_ROWS="$output"; fi
    return 0
}

atm_updates_state() {
    local component="" status="" revision="" detail=""
    while IFS=$'\t' read -r component status revision detail; do
        if [[ "$component" == "$1" ]]; then printf '%s\n' "$status"; return 0; fi
    done <<< "${ATM_UPDATES_ROWS:-}"
    printf 'unknown\n'
}

atm_updates_blue() {
    if [[ -t 1 && "${TERM:-dumb}" != dumb && -z "${NO_COLOR:-}" ]]; then
        printf '\033[34m%s\033[0m' "$1"
    else printf '%s' "$1"; fi
}

atm_updates_badge() {
    case "$(atm_updates_state "plugin:$1")" in available|blocked) printf ' '; atm_updates_blue "$(atm_updates_t ATM_UPDATES_BADGE)" ;; esac
}

atm_updates_menu_label() {
    local component="" status="" revision="" detail="" count=0 core=0
    printf '%s' "$(atm_updates_t ATM_UPDATES_TITLE)"
    while IFS=$'\t' read -r component status revision detail; do
        case "$status" in available|blocked)
            if [[ "$component" == core ]]; then core=1; else count=$((count + 1)); fi ;;
        esac
    done <<< "${ATM_UPDATES_ROWS:-}"
    if ((core || count)); then
        printf ' '
        atm_updates_blue "$(atm_updates_t ATM_UPDATES_BADGE)"
        printf ' [CORE: %s; Plugins: %s]' "$core" "$count"
    fi
    printf '\n'
}

atm_updates_show() {
    local component="" status="" revision="" detail=""
    printf '%s\n%s\n' "$(atm_updates_t ATM_UPDATES_TITLE)" 'https://github.com/AtomyCloud/atm/tree/main'
    if [[ -z "${ATM_UPDATES_ROWS:-}" ]]; then atm_info "$(atm_updates_t ATM_UPDATES_UNKNOWN)"; return 0; fi
    while IFS=$'\t' read -r component status revision detail; do
        [[ -n "$component" ]] || continue
        printf '%-24s %-22s %s\n' "$component" "$(atm_updates_t "ATM_UPDATES_STATE_${status^^}")" "$detail"
    done <<< "$ATM_UPDATES_ROWS"
}

atm_updates_apply() {
    local target="$1" answer="" revision="" component="" status="" detail="" output="" result=0
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: check/confirm/stage/validate/back up/update %s; preserve local edits and application data; no network or writes\n' "$target"
        return 0
    fi
    atm_updates_refresh --force
    atm_updates_show
    while IFS=$'\t' read -r component status revision detail; do [[ "$revision" == - ]] || break; done <<< "${ATM_UPDATES_ROWS:-}"
    [[ "$revision" =~ ^[a-f0-9]{40}$ ]] || { atm_warn "$(atm_updates_t ATM_UPDATES_UNKNOWN)"; return 1; }
    printf '%s\n%s\n%s %s\n%s ' "$(atm_updates_t ATM_UPDATES_TRUST)" "$(atm_updates_t ATM_UPDATES_SCOPE)" \
        "$(atm_updates_t ATM_UPDATES_TARGET)" "$target@$revision" "$(atm_updates_t ATM_UPDATES_CONFIRM)"
    read -r answer || return 0
    case "$answer" in y|Y|yes|YES) ;; *) atm_info "$(atm_updates_t ATM_UPDATES_CANCELLED)"; return 0 ;; esac
    if output="$(atm_updates_engine apply "$target" --revision "$revision")"; then :; else result=$?; fi
    while IFS=$'\t' read -r component status detail; do
        [[ -n "$component" ]] || continue
        printf '%s: %s %s\n' "$component" "$(atm_updates_t "ATM_UPDATES_RESULT_${status^^}")" "$detail"
        if [[ "$status" == updated ]]; then ATM_UPDATES_RESTART_REQUIRED=1; fi
    done <<< "$output"
    ATM_UPDATES_KEY="" ATM_UPDATES_ROWS=""
    if [[ -e "$ATM_HOME/.atm-core-update-pending" || -L "$ATM_HOME/.atm-core-update-pending" ]]; then
        ATM_UPDATES_RESTART_REQUIRED=1
        atm_warn "$(atm_updates_t ATM_UPDATES_PENDING)"
    elif [[ "${ATM_UPDATES_RESTART_REQUIRED:-0}" == 1 ]]; then atm_warn "$(atm_updates_t ATM_UPDATES_RESTART)"; fi
    return "$result"
}

atm_updates_pick_plugin() {
    local component="" status="" revision="" detail="" choice="" idx=1 ids=()
    while IFS=$'\t' read -r component status revision detail; do
        [[ "$component" == plugin:* ]] || continue
        ids+=("$component")
        printf '%s) %s [%s]\n' "$idx" "${component#plugin:}" "$(atm_updates_t "ATM_UPDATES_STATE_${status^^}")" >&2
        idx=$((idx + 1))
    done <<< "${ATM_UPDATES_ROWS:-}"
    printf 'b) %s\n%s ' "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_SELECT_OPTION)" >&2
    read -r choice || return 0
    case "$choice" in ''|b|B) return 0 ;; esac
    for ((idx=0; idx<${#ids[@]}; idx++)); do
        if [[ "$choice" == "$((idx + 1))" ]]; then atm_updates_apply "${ids[$idx]}"; return; fi
    done
    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
}

atm_updates_details() {
    atm_info "$(atm_updates_t ATM_UPDATES_CHANGES)"
    atm_updates_engine changes || return 1
    atm_info "$(atm_updates_t ATM_UPDATES_BACKUPS)"
    atm_updates_engine backups
}

atm_updates_menu() {
    local choice=""
    atm_updates_refresh --force
    while true; do
        atm_updates_show
        printf '\n1) %s\n2) %s\n3) %s\n4) %s\n5) %s\nb) %s\n%s ' \
            "$(atm_updates_t ATM_UPDATES_CHECK)" "$(atm_updates_t ATM_UPDATES_CORE)" "$(atm_updates_t ATM_UPDATES_PLUGIN)" \
            "$(atm_updates_t ATM_UPDATES_ALL)" "$(atm_updates_t ATM_UPDATES_DETAILS)" \
            "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) atm_updates_refresh --force ;;
            2) atm_updates_apply core || true ;;
            3) atm_updates_pick_plugin || true ;;
            4) atm_updates_apply all-plugins || true ;;
            5) atm_updates_details || true ;;
            b|B) return 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac
        [[ "${ATM_UPDATES_RESTART_REQUIRED:-0}" != 1 ]] || return 0
    done
}

atm_updates_cli() {
    local action="${1:-menu}" answer=""
    (($# == 0)) || shift
    case "$action" in
        menu) (($# == 0)) || return 1; atm_updates_menu ;;
        check) (($# == 0)) || return 1
            if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_updates_engine check; else atm_updates_refresh --force; atm_updates_show; fi ;;
        core|all) (($# == 0)) || return 1; [[ "$action" != all ]] || action=all-plugins; atm_updates_apply "$action" ;;
        plugin) (($# == 1)) && [[ "$1" =~ ^[a-z][a-z0-9_-]{0,63}$ ]] || return 1; atm_updates_apply "plugin:$1" ;;
        changes|backups) (($# == 0)) || return 1; atm_updates_engine "$action" ;;
        restore)
            (($# == 1)) && [[ "$1" =~ ^[a-f0-9]{32}$ ]] || return 1
            if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_updates_engine recover "$1"; return; fi
            printf '%s\n%s ' "$(atm_updates_t ATM_UPDATES_RESTORE)" "$(atm_updates_t ATM_UPDATES_CONFIRM)"
            read -r answer || return 0
            case "$answer" in y|Y|yes|YES) atm_updates_engine recover "$1" ;; *) atm_info "$(atm_updates_t ATM_UPDATES_CANCELLED)" ;; esac
            ;;
        verify)
            (($# == 2)) && [[ "$2" =~ ^[a-f0-9]{40}$ ]] || return 1
            local target="$1"
            [[ "$target" == core || "$target" == plugin:* ]] || target="plugin:$target"
            if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_updates_engine verify "$target" --revision "$2"; return; fi
            printf '%s\n%s ' "$(atm_updates_t ATM_UPDATES_VERIFY)" "$(atm_updates_t ATM_UPDATES_CONFIRM)"
            read -r answer || return 0
            case "$answer" in y|Y|yes|YES) atm_updates_engine verify "$target" --revision "$2" ;; *) atm_info "$(atm_updates_t ATM_UPDATES_CANCELLED)" ;; esac
            ;;
        *) atm_warn "$(atm_updates_t ATM_UPDATES_USAGE)"; return 1 ;;
    esac
}

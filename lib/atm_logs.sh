#!/usr/bin/env bash

atm_logs_init() {
    ATM_LOG_FILE="${ATM_LOG_FILE:-$ATM_LOG_DIR/$(date '+%Y-%m-%d_%H-%M-%S').log}"
    export ATM_LOG_FILE

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        atm_debug "DRY-RUN log file: $ATM_LOG_FILE"
        return 0
    fi

    mkdir -p "$ATM_LOG_DIR"
    : > "$ATM_LOG_FILE"

    atm_debug "Log file: $ATM_LOG_FILE"
}

atm_log() {
    local level="$1"
    shift || true

    [[ -n "${ATM_LOG_FILE:-}" ]] || return 0

    printf '[%s] [%s] %s\n' "$(date -Iseconds)" "$level" "$*" >> "$ATM_LOG_FILE"
}
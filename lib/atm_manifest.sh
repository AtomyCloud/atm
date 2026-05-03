#!/usr/bin/env bash

atm_manifest_file() {
    local plugin_id="$1"
    printf '%s/%s.manifest\n' "$ATM_MANIFEST_DIR" "$plugin_id"
}

atm_manifest_write() {
    local plugin_id="$1"
    shift

    local file=""
    file="$(atm_manifest_file "$plugin_id")"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: write manifest %s\n' "$file"
        printf '%s\n' "$@"
        return 0
    fi

    mkdir -p "$ATM_MANIFEST_DIR"

    {
        printf 'ATM_PLUGIN_ID=%q\n' "$plugin_id"
        printf 'ATM_UPDATED_AT=%q\n' "$(date -Iseconds)"
        printf '%s\n' "$@"
    } > "$file"
}
#!/usr/bin/env bash

atm_download_file() {
    local url="$1"
    local output="$2"
    local partial="${output}.part"

    atm_require_commands curl

    if [[ -s "$output" ]]; then
        atm_warn "Cache hit: $output"
        return 0
    fi

    mkdir -p "$(dirname "$output")"

    atm_run curl \
        --fail \
        --location \
        --show-error \
        --progress-bar \
        --retry 3 \
        --retry-delay 2 \
        "$url" \
        -o "$partial"

    [[ "${ATM_DRY_RUN:-0}" == "1" ]] && return 0

    [[ -s "$partial" ]] || atm_fail "Download produced empty file: $url"

    mv -f "$partial" "$output"
}
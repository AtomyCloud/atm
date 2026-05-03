#!/usr/bin/env bash

atm_archive_extract_tar_gz() {
    local file="$1"
    local dest="$2"
    local strip_components="${3:-1}"
    local tmp_dest="${dest}.tmp.$$"

    atm_require_commands tar

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: extract %s to %s\n' "$file" "$dest"
        return 0
    fi

    rm -rf "$tmp_dest"
    mkdir -p "$tmp_dest"

    tar -xzf "$file" -C "$tmp_dest" --strip-components="$strip_components"

    rm -rf "$dest"
    mv "$tmp_dest" "$dest"
}
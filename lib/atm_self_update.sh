#!/usr/bin/env bash

atm_self_update_cli() {
    local mode="check"

    if (($# > 0)); then
        case "$1" in
            --git) mode="git" ;;
            --tarball) mode="tarball" ;;
            --check) mode="check" ;;
            *) atm_fail "Unknown self-update option: $1" ;;
        esac
    fi

    case "$mode" in
        check)
            printf 'Self-update repository: %s\n' "$ATM_REPO_URL"
            printf 'Current version: %s\n' "$ATM_VERSION"
            printf 'Modes supported by design: git, tarball release\n'
            ;;
        git)
            if [[ ! -d "$ATM_HOME/.git" ]]; then
                atm_fail "Git self-update requires ATM_HOME to be a git clone: $ATM_HOME"
            fi

            atm_run git -C "$ATM_HOME" pull --ff-only
            ;;
        tarball)
            atm_warn "Tarball self-update is reserved for a later patch."
            ;;
    esac
}
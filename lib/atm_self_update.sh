#!/usr/bin/env bash

atm_self_update_t() {
    local key="$1"
    if [[ -n "${!key:-}" ]]; then
        atm_t "$key"
    else
        # New development labels fall back without replacing the active locale.
        (
            source "${ATM_LANG_DIR:-$ATM_HOME/lang}/en-us.lang"
            atm_t "$key"
        )
    fi
}

atm_self_update_refresh() {
    local force="${1:-}" current="" remote="" ref="" extra="" output="" key=""
    local branch="${ATM_SELF_UPDATE_BRANCH:-main}"
    local -x GIT_OPTIONAL_LOCKS=0 GIT_NO_LAZY_FETCH=1

    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        ATM_SELF_UPDATE_STATUS=dry_run
        ATM_SELF_UPDATE_CURRENT_COMMIT=""
        ATM_SELF_UPDATE_REMOTE_COMMIT=""
        ATM_SELF_UPDATE_CHECK_KEY=""
        return 0
    fi
    if ! command -v git >/dev/null 2>&1 \
        || [[ ! -d "$ATM_HOME/.git" && ! -f "$ATM_HOME/.git" ]]; then
        ATM_SELF_UPDATE_STATUS=not_git
        ATM_SELF_UPDATE_CURRENT_COMMIT=""
        ATM_SELF_UPDATE_REMOTE_COMMIT=""
        ATM_SELF_UPDATE_CHECK_KEY=""
        return 0
    fi
    current="$(git -C "$ATM_HOME" rev-parse --verify HEAD 2>/dev/null)" || current=""
    key="$ATM_HOME|${ATM_REPO_URL:-}|$branch|$current"
    if [[ "$force" != --force && "${ATM_SELF_UPDATE_CHECK_KEY:-}" == "$key" \
        && "${ATM_SELF_UPDATE_CHECK_AT:-}" =~ ^[0-9]+$ ]] \
        && (( SECONDS >= ATM_SELF_UPDATE_CHECK_AT && SECONDS - ATM_SELF_UPDATE_CHECK_AT < 60 )); then
        return 0
    fi
    ATM_SELF_UPDATE_CHECK_KEY="$key"
    ATM_SELF_UPDATE_CHECK_AT="$SECONDS"
    ATM_SELF_UPDATE_CURRENT_COMMIT="$current"
    ATM_SELF_UPDATE_REMOTE_COMMIT=""
    ATM_SELF_UPDATE_STATUS=unknown
    [[ "$current" =~ ^([a-f0-9]{40}|[a-f0-9]{64})$ && -n "${ATM_REPO_URL:-}" ]] || return 0
    git check-ref-format "refs/heads/$branch" >/dev/null 2>&1 || return 0
    command -v timeout >/dev/null 2>&1 || return 0

    # ls-remote reads the advertised tip without fetching objects or changing refs.
    if ! output="$(timeout --kill-after=1s 5s env GIT_TERMINAL_PROMPT=0 \
        GIT_SSH_COMMAND='ssh -oBatchMode=yes -oStrictHostKeyChecking=yes -oConnectTimeout=3' \
        git -c credential.interactive=false -c credential.helper= -C "$ATM_HOME" \
        ls-remote --exit-code --refs -- "$ATM_REPO_URL" "refs/heads/$branch" 2>/dev/null)"; then
        return 0
    fi
    [[ "$output" != *$'\n'* ]] || return 0
    read -r remote ref extra <<< "$output"
    [[ "$remote" =~ ^([a-f0-9]{40}|[a-f0-9]{64})$ && "$ref" == "refs/heads/$branch" && -z "$extra" ]] || return 0
    ATM_SELF_UPDATE_REMOTE_COMMIT="$remote"
    if [[ "$remote" == "$current" ]]; then
        ATM_SELF_UPDATE_STATUS=current
        return 0
    fi
    ATM_SELF_UPDATE_STATUS=available
    # Avoid graph walks that could lazily fetch objects in partial clones.
    if git -C "$ATM_HOME" config --get-regexp '^remote\..*\.promisor$' >/dev/null 2>&1 \
        || git -C "$ATM_HOME" config --get extensions.partialClone >/dev/null 2>&1; then
        return 0
    fi
    if git -C "$ATM_HOME" cat-file -e "$remote^{commit}" 2>/dev/null; then
        if git -C "$ATM_HOME" merge-base --is-ancestor "$remote" "$current" 2>/dev/null; then
            ATM_SELF_UPDATE_STATUS=ahead
        elif ! git -C "$ATM_HOME" merge-base --is-ancestor "$current" "$remote" 2>/dev/null; then
            ATM_SELF_UPDATE_STATUS=diverged
        fi
    fi
    return 0
}

atm_self_update_color_enabled() {
    [[ -t 1 && "${TERM:-dumb}" != dumb && -z "${NO_COLOR:-}" ]]
}

atm_self_update_menu_label() {
    printf '%s' "$(atm_t ATM_MENU_SELF_UPDATE)"
    case "${ATM_SELF_UPDATE_STATUS:-unknown}" in
        available|diverged)
            if atm_self_update_color_enabled; then
                printf ' \033[34m%s\033[0m' "${ATM_SELF_UPDATE_BADGE:-Update}"
            else
                printf ' %s' "${ATM_SELF_UPDATE_BADGE:-Update}"
            fi
            ;;
    esac
    printf '\n'
}

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
            atm_self_update_refresh --force
            printf '%s %s\n' "$(atm_self_update_t ATM_SELF_UPDATE_REPOSITORY)" "$ATM_REPO_URL"
            printf '%s %s\n' "$(atm_self_update_t ATM_SELF_UPDATE_BRANCH_LABEL)" "${ATM_SELF_UPDATE_BRANCH:-main}"
            printf '%s %s\n' "$(atm_self_update_t ATM_SELF_UPDATE_VERSION)" "$ATM_VERSION"
            if [[ -n "${ATM_SELF_UPDATE_CURRENT_COMMIT:-}" ]]; then
                printf '%s %s\n' "$(atm_self_update_t ATM_SELF_UPDATE_LOCAL_COMMIT)" "$ATM_SELF_UPDATE_CURRENT_COMMIT"
            fi
            if [[ -n "${ATM_SELF_UPDATE_REMOTE_COMMIT:-}" ]]; then
                printf '%s %s\n' "$(atm_self_update_t ATM_SELF_UPDATE_LATEST_COMMIT)" "$ATM_SELF_UPDATE_REMOTE_COMMIT"
            fi
            atm_self_update_t "ATM_SELF_UPDATE_STATUS_${ATM_SELF_UPDATE_STATUS^^}"
            case "$ATM_SELF_UPDATE_STATUS" in
                available|diverged) printf '\n  atm self-update --git\n' ;;
            esac
            ;;
        git)
            if [[ ! -d "$ATM_HOME/.git" && ! -f "$ATM_HOME/.git" ]]; then
                atm_fail "Git self-update requires ATM_HOME to be a git clone: $ATM_HOME"
            fi

            if [[ "$(git -C "$ATM_HOME" symbolic-ref --quiet --short HEAD 2>/dev/null || true)" != "${ATM_SELF_UPDATE_BRANCH:-main}" ]]; then
                atm_warn "$(atm_self_update_t ATM_SELF_UPDATE_WRONG_BRANCH)"
                return 1
            fi
            atm_run git -C "$ATM_HOME" pull --ff-only -- "$ATM_REPO_URL" "${ATM_SELF_UPDATE_BRANCH:-main}" || return 1
            ATM_SELF_UPDATE_CHECK_KEY=""
            ;;
        tarball)
            atm_warn "Tarball self-update is reserved for a later patch."
            ;;
    esac
}

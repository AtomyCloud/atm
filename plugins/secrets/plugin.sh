#!/usr/bin/env bash

atm_secrets_flatpak() {
    local action="$1"
    shift
    FLATPAK_USER_DIR="$ATM_SECRETS_FLATPAK_DIR" command flatpak "$action" --user "$@"
}

atm_secrets_version() {
    [[ -d "$ATM_SECRETS_FLATPAK_DIR" ]] || return 1
    local rows="" app="" branch="" version=""
    rows="$(atm_secrets_flatpak list --app --columns=application,branch,version)" || return 2
    while IFS=$'\t' read -r app branch version; do
        if [[ "$app" == "$ATM_SECRETS_APP_ID" && "$branch" == stable ]]; then
            printf '%s\n' "${version:-$(atm_t ATM_PLUGIN_SECRETS_UNKNOWN_VERSION)}"
            return 0
        fi
    done <<< "$rows"
    return 1
}

atm_secrets_status() {
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_t ATM_PLUGIN_SECRETS_DRY_RUN
    elif ! command -v flatpak >/dev/null 2>&1; then
        atm_t ATM_PLUGIN_SECRETS_DEPENDENCY
    else
        local version=""
        if version="$(atm_secrets_version 2>/dev/null)"; then
            printf 'Secrets %s (ATM)\n' "$version"
        elif [[ "$?" == 1 ]]; then
            atm_t ATM_PLUGIN_SECRETS_NOT_INSTALLED
        else
            atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED
        fi
    fi
}

atm_secrets_launcher_text() {
    printf '%s\n' '#!/usr/bin/env bash' '# ATM Secrets launcher v1' 'set -e' \
        'root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"' \
        'export FLATPAK_USER_DIR="$root/flatpak"' \
        'exec flatpak run --user --branch=stable org.gnome.World.Secrets "$@"'
}

atm_secrets_paths() {
    local path="" launcher="$ATM_SECRETS_ROOT/bin/secrets"
    for path in "$ATM_SECRETS_ROOT" "$ATM_SECRETS_ROOT/bin" "$ATM_SECRETS_FLATPAK_DIR"; do
        if [[ -L "$path" || ( -e "$path" && ! -d "$path" ) ]]; then
            atm_warn "$(atm_t ATM_PLUGIN_SECRETS_PATH_CONFLICT): $path"; return 1
        fi
    done
    if [[ -L "$launcher" || ( -e "$launcher" && ! -f "$launcher" ) ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_SECRETS_PATH_CONFLICT): $launcher"; return 1
    fi
    if [[ -f "$launcher" ]] && ! cmp -s "$launcher" <(atm_secrets_launcher_text); then
        atm_warn "$(atm_t ATM_PLUGIN_SECRETS_PATH_CONFLICT): $launcher"; return 1
    fi
}

atm_secrets_require() {
    [[ "$(uname -s)" == Linux ]] || { atm_warn "$(atm_t ATM_PLUGIN_SECRETS_LINUX_ONLY)"; return 1; }
    command -v flatpak >/dev/null 2>&1 || { atm_warn "$(atm_t ATM_PLUGIN_SECRETS_DEPENDENCY)"; return 1; }
    atm_secrets_paths
}

atm_secrets_confirm() {
    local answer=""
    printf '%s\n%s: %s\n%s: %s\n%s ' "$(atm_t ATM_PLUGIN_SECRETS_NOTICE)" \
        "$(atm_t ATM_PLUGIN_SECRETS_LOCATION)" "$ATM_SECRETS_ROOT" \
        "$(atm_t ATM_PLUGIN_SECRETS_DATA)" "$HOME/.var/app/$ATM_SECRETS_APP_ID" \
        "$(atm_t ATM_PLUGIN_SECRETS_CONFIRM)"
    read -r answer || return 1
    case "$answer" in y|Y|yes|YES) return 0 ;; esac
    atm_info "$(atm_t ATM_PLUGIN_SECRETS_CANCELLED)"
    return 1
}

atm_secrets_remote() {
    local rows="" name="" url="" options=""
    rows="$(atm_secrets_flatpak remotes --show-disabled --columns=name,url,options)" || return 1
    while IFS=$'\t' read -r name url options; do
        [[ "$name" == "$ATM_SECRETS_REMOTE" ]] || continue
        if [[ "$url" != https://dl.flathub.org/repo/ || "$options" == *no-gpg-verify* || "$options" == *disabled* ]]; then
            atm_warn "$(atm_t ATM_PLUGIN_SECRETS_REMOTE_INVALID)"; return 1
        fi
        return 0
    done <<< "$rows"
    atm_secrets_flatpak remote-add --from "$ATM_SECRETS_REMOTE" "$ATM_SECRETS_REPO_URL"
}

atm_secrets_origin() {
    local origin=""
    origin="$(atm_secrets_flatpak info --show-origin "$ATM_SECRETS_APP_ID" stable)" || return 1
    [[ "$origin" == "$ATM_SECRETS_REMOTE" ]] || { atm_warn "$(atm_t ATM_PLUGIN_SECRETS_REMOTE_INVALID)"; return 1; }
}

atm_secrets_record() {
    local installed="$1" version="${2:-}"
    atm_manifest_write secrets "ATM_PLUGIN_VERSION=0.0.1" "ATM_INSTALLED=$installed" \
        "ATM_SECRETS_VERSION=$(printf '%q' "$version")" \
        "ATM_SECRETS_FLATPAK_DIR=$(printf '%q' "$ATM_SECRETS_FLATPAK_DIR")"
}

atm_secrets_install() {
    local action=install version="" temporary="" status=0
    case "$#:${1:-}" in 0:) ;; 1:--update) action=update ;; *) atm_warn "$(atm_t ATM_PLUGIN_SECRETS_ARGS)"; return 1 ;; esac
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: FLATPAK_USER_DIR=%q flatpak %s --user %s %s\n' "$ATM_SECRETS_FLATPAK_DIR" "$action" \
            "$([[ "$action" == install ]] && printf '%s' "$ATM_SECRETS_REMOTE")" "$ATM_SECRETS_APP_ID//stable"
        return 0
    fi
    atm_secrets_require || return 1
    if atm_secrets_version >/dev/null 2>&1; then
        atm_secrets_origin || return 1
    else
        status=$?
        if ((status != 1)); then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"; return 1; fi
        if [[ "$action" == update ]]; then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_NOT_INSTALLED)"; return 1; fi
    fi
    atm_secrets_confirm || return 0
    atm_secrets_remote || return 1
    if [[ "$action" == install ]]; then
        atm_secrets_flatpak install "$ATM_SECRETS_REMOTE" "$ATM_SECRETS_APP_ID//stable" || return 1
    else
        atm_secrets_flatpak update "$ATM_SECRETS_APP_ID//stable" || return 1
    fi
    version="$(atm_secrets_version)" || return 1
    atm_secrets_origin || return 1
    atm_secrets_paths || return 1
    mkdir -p "$ATM_SECRETS_ROOT/bin" || return 1
    temporary="$(mktemp "$ATM_SECRETS_ROOT/bin/.secrets.XXXXXX")" || return 1
    if ! atm_secrets_launcher_text > "$temporary" || ! chmod 755 "$temporary" || ! mv -f -- "$temporary" "$ATM_SECRETS_ROOT/bin/secrets"; then
        rm -f -- "$temporary"; return 1
    fi
    atm_secrets_record 1 "$version" || return 1
    atm_success "$(atm_t ATM_PLUGIN_SECRETS_DONE): $version"
    printf '%s: %s\n' "$(atm_t ATM_PLUGIN_SECRETS_LAUNCHER)" "$ATM_SECRETS_ROOT/bin/secrets"
}

atm_secrets_remove() {
    local status=0
    case "$#:${1:-}" in 0:|1:stable) ;; *) atm_warn "$(atm_t ATM_PLUGIN_SECRETS_ARGS)"; return 1 ;; esac
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: FLATPAK_USER_DIR=%q flatpak uninstall --user %s\n' "$ATM_SECRETS_FLATPAK_DIR" "$ATM_SECRETS_APP_ID//stable"
        return 0
    fi
    atm_secrets_require || return 1
    if atm_secrets_version >/dev/null 2>&1; then :; else
        status=$?
        if ((status != 1)); then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"; return 1; fi
        atm_info "$(atm_t ATM_PLUGIN_SECRETS_NOT_INSTALLED)"; return 0
    fi
    atm_secrets_confirm || return 0
    atm_secrets_flatpak uninstall "$ATM_SECRETS_APP_ID//stable" || return 1
    if atm_secrets_version >/dev/null 2>&1; then return 1; else
        status=$?
        if ((status != 1)); then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"; return 1; fi
    fi
    if [[ -f "$ATM_SECRETS_ROOT/bin/secrets" ]] && cmp -s "$ATM_SECRETS_ROOT/bin/secrets" <(atm_secrets_launcher_text); then
        rm -f -- "$ATM_SECRETS_ROOT/bin/secrets" || return 1
    fi
    atm_secrets_record 0 || return 1
    atm_success "$(atm_t ATM_PLUGIN_SECRETS_PRESERVED)"
}

atm_secrets_open() {
    (($# == 0)) || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: FLATPAK_USER_DIR=%q flatpak run --user --branch=stable %s\n' "$ATM_SECRETS_FLATPAK_DIR" "$ATM_SECRETS_APP_ID"
        return 0
    fi
    atm_secrets_require || return 1
    atm_secrets_version >/dev/null 2>&1 || { atm_warn "$(atm_t ATM_PLUGIN_SECRETS_NOT_INSTALLED)"; return 1; }
    atm_secrets_flatpak run --branch=stable "$ATM_SECRETS_APP_ID"
}

atm_secrets_path_entries() {
    if [[ -x "$ATM_SECRETS_ROOT/bin/secrets" ]]; then printf '%s\n' "$ATM_SECRETS_ROOT/bin"; fi
}

atm_secrets_menu() {
    local option=""
    while true; do
        printf '\n%s\nhttps://apps.gnome.org/Secrets/\n' "$(atm_t ATM_PLUGIN_SECRETS_MENU_TITLE)"
        atm_secrets_status; printf '\n'
        printf '1) %s\n2) %s\n3) %s\n4) %s\nb) %s\nq) %s\n%s ' \
            "$(atm_t ATM_PLUGIN_SECRETS_INSTALL)" "$(atm_t ATM_PLUGIN_SECRETS_UPDATE)" \
            "$(atm_t ATM_PLUGIN_SECRETS_OPEN)" "$(atm_t ATM_PLUGIN_SECRETS_REMOVE)" \
            "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_EXIT)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r option || return 0
        case "$option" in
            1) atm_secrets_install || true ;; 2) atm_secrets_install --update || true ;;
            3) atm_secrets_open || true ;; 4) atm_secrets_remove || true ;;
            b|B|q|Q|0) return 0 ;; *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac
        atm_prompt_continue
    done
}

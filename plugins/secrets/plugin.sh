#!/usr/bin/env bash

atm_secrets_flatpak() {
    local action="$1"
    shift
    if [[ "${ATM_SECRETS_MODE:-atm}" == default ]]; then
        env -u FLATPAK_USER_DIR -u FLATPAK_SYSTEM_DIR flatpak "$action" --system "$@"
    else
        FLATPAK_USER_DIR="$ATM_SECRETS_FLATPAK_DIR" command flatpak "$action" --user "$@"
    fi
}

atm_secrets_options() {
    local operation="$1" mode_seen=0 update_seen=0 stable_seen=0
    shift
    while (($#)); do
        case "$1" in
            --mode)
                (($# >= 2 && mode_seen == 0)) || break
                case "$2" in atm|default) ATM_SECRETS_MODE="$2" ;; *) break ;; esac
                mode_seen=1; shift 2 ;;
            --update)
                [[ "$operation" == install && "$update_seen" == 0 ]] || break
                ATM_SECRETS_UPDATE=1; update_seen=1; shift ;;
            stable)
                [[ "$operation" == remove && "$stable_seen" == 0 ]] || break
                stable_seen=1; shift ;;
            *) break ;;
        esac
    done
    if (($#)); then
        atm_warn "$(atm_t ATM_PLUGIN_SECRETS_ARGS) $(atm_t ATM_PLUGIN_SECRETS_MODE_USAGE)"; return 1
    fi
    if [[ "$ATM_SECRETS_MODE" == default ]]; then ATM_SECRETS_REMOTE=flathub; fi
}

atm_secrets_not_installed() {
    if [[ "${ATM_SECRETS_MODE:-atm}" == default ]]; then atm_t ATM_PLUGIN_SECRETS_NOT_INSTALLED_DEFAULT
    else atm_t ATM_PLUGIN_SECRETS_NOT_INSTALLED; fi
}

atm_secrets_dry_run() {
    if [[ "${ATM_SECRETS_MODE:-atm}" == default ]]; then
        printf 'DRY-RUN: env -u FLATPAK_USER_DIR -u FLATPAK_SYSTEM_DIR flatpak %q --system' "$1"
    else
        printf 'DRY-RUN: FLATPAK_USER_DIR=%q flatpak %q --user' "$ATM_SECRETS_FLATPAK_DIR" "$1"
    fi
    shift
    printf ' %q' "$@"; printf '\n'
}

atm_secrets_version() {
    if [[ "${ATM_SECRETS_MODE:-atm}" == atm && ! -d "$ATM_SECRETS_FLATPAK_DIR" ]]; then return 1; fi
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
        local version="" ATM_SECRETS_MODE=atm label=""
        for ATM_SECRETS_MODE in atm default; do
            label=ATM
            [[ "$ATM_SECRETS_MODE" != default ]] || label="$(atm_t ATM_PLUGIN_SECRETS_MODE_DEFAULT)"
            if version="$(atm_secrets_version 2>/dev/null)"; then
                printf 'Secrets %s (%s)\n' "$version" "$label"
            elif [[ "$?" == 1 ]]; then
                atm_secrets_not_installed; printf '\n'
            else
                printf '%s: %s\n' "$label" "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"
            fi
        done
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
    if [[ "${ATM_SECRETS_MODE:-atm}" == atm ]]; then atm_secrets_paths; fi
}

atm_secrets_confirm() {
    local answer=""
    if [[ "${ATM_SECRETS_MODE:-atm}" == default ]]; then
        printf '%s\n%s\n' "$(atm_t ATM_PLUGIN_SECRETS_MODE_DEFAULT)" "$(atm_t ATM_PLUGIN_SECRETS_DEFAULT_NOTICE)"
    else
        printf '%s\n%s: %s\n' "$(atm_t ATM_PLUGIN_SECRETS_NOTICE)" "$(atm_t ATM_PLUGIN_SECRETS_LOCATION)" "$ATM_SECRETS_ROOT"
    fi
    printf '%s: %s\n%s ' \
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
    local installed="$1" version="${2:-}" manifest=secrets location="$ATM_SECRETS_FLATPAK_DIR"
    if [[ "${ATM_SECRETS_MODE:-atm}" == default ]]; then manifest=secrets-default; location=system; fi
    atm_manifest_write "$manifest" "ATM_PLUGIN_VERSION=0.0.1" "ATM_INSTALLED=$installed" \
        "ATM_SECRETS_MODE=${ATM_SECRETS_MODE:-atm}" \
        "ATM_SECRETS_VERSION=$(printf '%q' "$version")" \
        "ATM_SECRETS_FLATPAK_DIR=$(printf '%q' "$location")"
}

atm_secrets_install() {
    local action=install version="" temporary="" status=0
    local ATM_SECRETS_MODE=atm ATM_SECRETS_UPDATE=0 ATM_SECRETS_REMOTE="$ATM_SECRETS_REMOTE"
    atm_secrets_options install "$@" || return 1
    [[ "$ATM_SECRETS_UPDATE" == 0 ]] || action=update
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        if [[ "$action" == install ]]; then atm_secrets_dry_run install "$ATM_SECRETS_REMOTE" "$ATM_SECRETS_APP_ID//stable"
        else atm_secrets_dry_run update "$ATM_SECRETS_APP_ID//stable"; fi
        return 0
    fi
    atm_secrets_require || return 1
    if atm_secrets_version >/dev/null 2>&1; then
        atm_secrets_origin || return 1
    else
        status=$?
        if ((status != 1)); then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"; return 1; fi
        if [[ "$action" == update ]]; then atm_warn "$(atm_secrets_not_installed)"; return 1; fi
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
    if [[ "$ATM_SECRETS_MODE" == default ]]; then
        atm_secrets_record 1 "$version" || return 1
        atm_success "$(atm_t ATM_PLUGIN_SECRETS_DONE): $version"
        return 0
    fi
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
    local ATM_SECRETS_MODE=atm ATM_SECRETS_REMOTE="$ATM_SECRETS_REMOTE"
    atm_secrets_options remove "$@" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_secrets_dry_run uninstall "$ATM_SECRETS_APP_ID//stable"
        return 0
    fi
    atm_secrets_require || return 1
    if atm_secrets_version >/dev/null 2>&1; then :; else
        status=$?
        if ((status != 1)); then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"; return 1; fi
        atm_info "$(atm_secrets_not_installed)"; return 0
    fi
    atm_secrets_confirm || return 0
    atm_secrets_flatpak uninstall "$ATM_SECRETS_APP_ID//stable" || return 1
    if atm_secrets_version >/dev/null 2>&1; then return 1; else
        status=$?
        if ((status != 1)); then atm_warn "$(atm_t ATM_PLUGIN_SECRETS_QUERY_FAILED)"; return 1; fi
    fi
    if [[ "$ATM_SECRETS_MODE" == atm && -f "$ATM_SECRETS_ROOT/bin/secrets" ]] && cmp -s "$ATM_SECRETS_ROOT/bin/secrets" <(atm_secrets_launcher_text); then
        rm -f -- "$ATM_SECRETS_ROOT/bin/secrets" || return 1
    fi
    atm_secrets_record 0 || return 1
    atm_success "$(atm_t ATM_PLUGIN_SECRETS_PRESERVED)"
}

atm_secrets_open() {
    local ATM_SECRETS_MODE=atm ATM_SECRETS_REMOTE="$ATM_SECRETS_REMOTE"
    atm_secrets_options open "$@" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        atm_secrets_dry_run run --branch=stable "$ATM_SECRETS_APP_ID"
        return 0
    fi
    atm_secrets_require || return 1
    atm_secrets_version >/dev/null 2>&1 || { atm_warn "$(atm_secrets_not_installed)"; return 1; }
    atm_secrets_flatpak run --branch=stable "$ATM_SECRETS_APP_ID"
}

atm_secrets_path_entries() {
    if [[ -x "$ATM_SECRETS_ROOT/bin/secrets" ]]; then printf '%s\n' "$ATM_SECRETS_ROOT/bin"; fi
}

atm_secrets_choose_mode() {
    local choice=""
    printf '1) %s\n2) %s\nb) %s\n%s ' "$(atm_t ATM_PLUGIN_SECRETS_MODE_ATM)" \
        "$(atm_t ATM_PLUGIN_SECRETS_MODE_DEFAULT)" "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_PLUGIN_SECRETS_SELECT_MODE)" >&2
    read -r choice || return 1
    case "$choice" in ''|1) printf 'atm\n' ;; 2) printf 'default\n' ;; b|B|q|Q) return 1 ;;
        *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"; return 1 ;; esac
}

atm_secrets_menu() {
    local option="" mode=""
    while true; do
        printf '\n%s\nhttps://apps.gnome.org/Secrets/\n' "$(atm_t ATM_PLUGIN_SECRETS_MENU_TITLE)"
        atm_secrets_status; printf '\n'
        printf '1) %s: %s\n2) %s: %s\n3) %s\n4) %s\n5) %s\nb) %s\nq) %s\n%s ' \
            "$(atm_t ATM_PLUGIN_SECRETS_INSTALL)" "$(atm_t ATM_PLUGIN_SECRETS_MODE_ATM)" \
            "$(atm_t ATM_PLUGIN_SECRETS_INSTALL)" "$(atm_t ATM_PLUGIN_SECRETS_MODE_DEFAULT)" "$(atm_t ATM_PLUGIN_SECRETS_UPDATE)" \
            "$(atm_t ATM_PLUGIN_SECRETS_OPEN)" "$(atm_t ATM_PLUGIN_SECRETS_REMOVE)" \
            "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_MENU_EXIT)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r option || return 0
        case "$option" in
            1) atm_secrets_install --mode atm || true ;; 2) atm_secrets_install --mode default || true ;;
            3|4|5)
                if mode="$(atm_secrets_choose_mode)"; then
                    case "$option" in
                        3) atm_secrets_install --mode "$mode" --update || true ;;
                        4) atm_secrets_open --mode "$mode" || true ;;
                        5) atm_secrets_remove --mode "$mode" || true ;;
                    esac
                fi ;;
            b|B|q|Q|0) return 0 ;; *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac
        atm_prompt_continue
    done
}

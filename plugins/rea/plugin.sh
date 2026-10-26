#!/usr/bin/env bash

source "${BASH_SOURCE[0]%/*}/lang/en-us.lang"

atm_rea_manage() {
    command -v python3 >/dev/null 2>&1 || { atm_warn "$(atm_t ATM_PLUGIN_REA_PYTHON)"; return 1; }
    ATM_APPS_DIR="$ATM_APPS_DIR" ATM_HOME="$ATM_HOME" python3 -B "${BASH_SOURCE[0]%/*}/manage.py" "$@"
}

atm_rea_root() {
    if [[ -f "$ATM_REA_STATE_FILE" || -L "$ATM_REA_STATE_FILE" ]]; then
        atm_rea_manage root "$ATM_REA_STATE_FILE" "$ATM_REA_INSTALL_ROOT"
    else
        printf '%s\n' "$ATM_REA_INSTALL_ROOT"
    fi
}

atm_rea_path_entries() {
    local root=""
    root="$(atm_rea_root)" || return 0
    atm_rea_valid_path "$root" || return 0
    printf '%s/bin\n' "$root"
}

atm_rea_valid_path() {
    [[ "$1" == /* && "$1" != *:* && "$1" != *[$'\001'-$'\037'$'\177']* \
        && "$1" != *\"* && "$1" != *'$'* && "$1" != *'`'* && "$1" != *\\* ]]
}

atm_rea_status() {
    local root="" version=""
    root="$(atm_rea_root 2>/dev/null)" || { atm_t ATM_PLUGIN_REA_FAILED; return 0; }
    if [[ -f "$root/.atm-rea-root" ]] && command -v python3 >/dev/null 2>&1; then
        version="$(atm_rea_manage version "$root" 2>/dev/null)" || { atm_t ATM_PLUGIN_REA_FAILED; return 0; }
    fi
    if [[ -n "$version" ]]; then printf 'REA %s (%s)\n' "$version" "$root"; else atm_t ATM_PLUGIN_REA_NOT_INSTALLED; fi
}

atm_rea_choose_path() {
    local choice="" root=""
    printf '1) %s: %s\n2) %s\nb) %s\n%s ' "$(atm_t ATM_PLUGIN_REA_DEFAULT_PATH)" "$ATM_REA_INSTALL_ROOT" \
        "$(atm_t ATM_PLUGIN_REA_CUSTOM_PATH)" "$(atm_t ATM_MENU_BACK)" "$(atm_t ATM_PLUGIN_REA_PATH_PROMPT)" >&2
    read -r choice || return 1
    case "$choice" in
        ''|1) printf '%s\n' "$ATM_REA_INSTALL_ROOT" ;;
        2)
            printf '%s ' "$(atm_t ATM_PLUGIN_REA_CUSTOM_PROMPT)" >&2
            read -r root || return 1
            [[ -n "$root" ]] || return 1
            printf '%s\n' "$root"
            ;;
        b|B) return 1 ;;
        *) atm_warn "$(atm_t ATM_PLUGIN_REA_INVALID)"; return 1 ;;
    esac
}

atm_rea_node_supported() {
    local value="${1#v}" major="" minor="" patch=""
    [[ "$value" =~ ^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$ ]] || return 1
    IFS=. read -r major minor patch <<< "$value"
    major=$((10#$major)); minor=$((10#$minor)); patch=$((10#$patch))
    ((major == 22 && (minor > 23 || minor == 23 && patch >= 4) ||
      major == 24 && (minor > 22 || minor == 22 && patch >= 1) ||
      major == 26 && (minor > 12 || minor == 12 && patch >= 1) || major > 26))
}

atm_rea_select_runtime() {
    local candidate="" node="" npm="" observed=""
    ATM_REA_NODE_PATH="" ATM_REA_NPM_PATH=""
    command -v timeout >/dev/null 2>&1 || return 1
    for candidate in "$(command -v node || true)" "$ATM_APPS_DIR/node/current/bin/node"; do
        [[ -x "$candidate" ]] || continue
        node="$(readlink -f -- "$candidate")" || continue
        observed="$(timeout --kill-after=1 4 "$node" --version </dev/null 2>/dev/null)" || continue
        atm_rea_node_supported "$observed" || continue
        npm="${node%/*}/npm"
        [[ -x "$npm" ]] || npm="$(command -v npm || true)"
        [[ -x "$npm" ]] || continue
        PATH="${node%/*}:$PATH" timeout --kill-after=1 6 "$npm" --version </dev/null >/dev/null 2>&1 || continue
        ATM_REA_NODE_PATH="$node" ATM_REA_NPM_PATH="$npm"
        return 0
    done
    return 1
}

atm_rea_ps_ready() {
    command -v timeout >/dev/null 2>&1 && command -v ps >/dev/null 2>&1 \
        && timeout --kill-after=1 4 ps -p "$$" -o pid= -o ppid= -o stat= -o args= >/dev/null 2>&1
}

atm_rea_is_apt() {
    local ID="" ID_LIKE=""
    command -v apt-get >/dev/null 2>&1 || return 1
    [[ ! -f /etc/os-release ]] || source /etc/os-release
    case " $ID $ID_LIKE " in *' ubuntu '*|*' debian '*|*' zorin '*) return 0 ;; *) return 1 ;; esac
}

atm_rea_dependencies() {
    local answer="" packages=() runtime_missing=0
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_info "$(atm_t ATM_PLUGIN_REA_PLAN)"; return 0; fi
    [[ "$(uname -s)" == Linux ]] || { atm_warn "$(atm_t ATM_PLUGIN_REA_PLATFORM)"; return 1; }
    command -v python3 >/dev/null 2>&1 || packages+=(python3)
    command -v timeout >/dev/null 2>&1 || packages+=(coreutils)
    atm_rea_ps_ready || packages+=(procps)
    atm_rea_select_runtime || runtime_missing=1
    if ((${#packages[@]} || runtime_missing)); then
        atm_warn "$(atm_t ATM_PLUGIN_REA_MISSING)"
        ((${#packages[@]} == 0)) || printf '  %s\n' "${packages[@]}"
        ((runtime_missing == 0)) || atm_info "$(atm_t ATM_PLUGIN_REA_NODE)"
        atm_info "$(atm_t ATM_PLUGIN_REA_DEP_PLAN)"
        printf '%s ' "$(atm_t ATM_PLUGIN_REA_DEP_CONFIRM)"
        read -r answer || return 1
        case "$answer" in y|Y|yes|YES) ;; *) atm_info "$(atm_t ATM_PLUGIN_REA_CANCELLED)"; return 1 ;; esac
        if ((${#packages[@]})); then
            atm_rea_is_apt || { atm_warn "$(atm_t ATM_PLUGIN_REA_APT_ONLY)"; return 1; }
            local elevated=()
            if ((EUID != 0)); then
                command -v sudo >/dev/null 2>&1 && sudo --validate || return 1
                elevated=(sudo --)
            fi
            "${elevated[@]}" apt-get update || return 1
            "${elevated[@]}" apt-get --no-remove install "${packages[@]}" || return 1
        fi
        if ! atm_rea_select_runtime; then
            declare -F atm_node_install >/dev/null 2>&1 || { atm_warn "$(atm_t ATM_PLUGIN_REA_NODE_HELP)"; return 1; }
            atm_node_install --location atm --major 26 || return 1
        fi
    fi
    command -v python3 >/dev/null 2>&1 && atm_rea_ps_ready && atm_rea_select_runtime \
        || { atm_warn "$(atm_t ATM_PLUGIN_REA_MISSING)"; return 1; }
    atm_success "$(atm_t ATM_PLUGIN_REA_READY)"
    printf 'Node.js: %s\nnpm: %s\n' "$ATM_REA_NODE_PATH" "$ATM_REA_NPM_PATH"
    atm_info "$(atm_t ATM_PLUGIN_REA_OPTIONAL)"
}

atm_rea_write_manifest() {
    local root="$1" version="$2" installed=0
    [[ -z "$version" ]] || installed=1
    atm_manifest_write rea 'ATM_PLUGIN_NAME="REA: Reverse Engineer Anything"' 'ATM_PLUGIN_VERSION="0.0.1"' \
        "ATM_INSTALLED=$installed" "ATM_CURRENT_VERSION=$(printf '%q' "$version")" \
        "ATM_INSTALL_ROOT=$(printf '%q' "$root")" "ATM_CURRENT_PATH=$(printf '%q' "$root/current")"
}

atm_rea_usage() {
    local root="$1"
    atm_info "$(atm_t ATM_PLUGIN_REA_PATH_HINT)"
    printf '  %q --help\n' "$root/bin/rea"
    atm_info "$(atm_t ATM_PLUGIN_REA_SETUP_HINT)"
    printf '  %q setup\n' "$root/bin/rea"
}

atm_rea_publish_install() (
    local root="$1" requested="$2" version="" stage=""
    version="$(PATH="${ATM_REA_NODE_PATH%/*}:$PATH" npm_config_cache="$ATM_REA_CACHE_DIR/npm" \
        timeout --kill-after=2 60 "$ATM_REA_NPM_PATH" view --registry=https://registry.npmjs.org \
        --json "rea-agents@$requested" version | atm_rea_manage release)" || return 1
    stage="$(atm_rea_manage begin "$root")" || return 1
    trap 'atm_rea_manage finish "$root"' EXIT
    if [[ -e "$root/versions/$version" || -L "$root/versions/$version" ]]; then
        atm_rea_manage activate "$root" "$ATM_REA_STATE_FILE" "$version" "$ATM_REA_NODE_PATH" || return 1
    else
        PATH="${ATM_REA_NODE_PATH%/*}:$PATH" npm_config_cache="$ATM_REA_CACHE_DIR/npm" \
            "$ATM_REA_NPM_PATH" install --global --prefix "$stage" --registry=https://registry.npmjs.org \
            --engine-strict --ignore-scripts --no-audit --no-fund "rea-agents@$version" || return 1
        atm_rea_manage publish "$root" "$ATM_REA_STATE_FILE" "$version" "$ATM_REA_NODE_PATH" || return 1
    fi
    atm_rea_write_manifest "$root" "$version" || return 1
    atm_rea_manage finish "$root" || return 1
    trap - EXIT
    atm_success "$(atm_t ATM_PLUGIN_REA_INSTALLED) $version"
)

atm_rea_install() {
    local action=install requested="$ATM_REA_DEFAULT_VERSION" root="" explicit=0 old_root="" current=""
    while (($#)); do
        case "$1" in
            --version) (($# >= 2)) || return 1; requested="$2"; shift ;;
            --version=*) requested="${1#*=}" ;;
            --path) (($# >= 2)) || return 1; root="$2"; explicit=1; shift ;;
            --path=*) root="${1#*=}"; explicit=1 ;;
            --update|--dependencies|--status)
                [[ "$action" == install ]] || return 1
                action="${1#--}"
                ;;
            *) atm_warn "$(atm_t ATM_PLUGIN_REA_INVALID)"; return 1 ;;
        esac
        shift
    done
    [[ "$requested" == latest || "$requested" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
        || { atm_warn "$(atm_t ATM_PLUGIN_REA_INVALID)"; return 1; }
    if ((explicit)); then
        atm_rea_valid_path "$root" || { atm_warn "$(atm_t ATM_PLUGIN_REA_INVALID)"; return 1; }
    fi
    case "$action" in
        dependencies) atm_rea_dependencies; return ;;
        status) atm_rea_status; return ;;
    esac
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        [[ -n "$root" ]] || root="$(atm_rea_root)" || return 1
        atm_rea_dependencies
        printf 'DRY-RUN: %s rea-agents@%s in %q (staged npm global prefix, engine-strict, ignore-scripts); update current and receipt/manifest\n' \
            "$action" "$requested" "$root"
        return 0
    fi
    atm_rea_dependencies || return 1
    old_root="$(atm_rea_root)" || return 1
    if [[ "$action" == install && "$explicit" == 0 && -t 0 ]]; then
        root="$(atm_rea_choose_path)" || return 0
    fi
    [[ -n "$root" ]] || root="$old_root"
    root="$(atm_rea_manage validate "$root")" || return 1
    current="$(atm_rea_manage version "$old_root")" || return 1
    if [[ -n "$current" && "$root" != "$old_root" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_REA_DESTINATION_LOCKED)"; return 1
    fi
    if [[ "$action" == update && -z "$current" ]]; then atm_warn "$(atm_t ATM_PLUGIN_REA_NOT_INSTALLED)"; return 1; fi
    atm_rea_publish_install "$root" "$requested" || { atm_warn "$(atm_t ATM_PLUGIN_REA_FAILED)"; return 1; }
    atm_rea_usage "$root"
}

atm_rea_uninstall() {
    (($# == 0)) || { atm_warn "$(atm_t ATM_PLUGIN_REA_INVALID)"; return 1; }
    local root="" version="" answer=""
    root="$(atm_rea_root)" || return 1
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then
        printf 'DRY-RUN: confirm removal of ATM-owned REA versions/launchers in %q; retain external programs, dependencies, settings and data\n' "$root"
        return 0
    fi
    version="$(atm_rea_manage version "$root")" || return 1
    [[ -n "$version" ]] || { atm_warn "$(atm_t ATM_PLUGIN_REA_NOT_INSTALLED)"; return 1; }
    atm_info "$(atm_t ATM_PLUGIN_REA_REMOVE_WARNING)"
    printf '%s\n%s ' "$root" "$(atm_t ATM_PLUGIN_REA_CONFIRM)"
    read -r answer || return 0
    case "$answer" in y|Y|yes|YES) ;; *) atm_info "$(atm_t ATM_PLUGIN_REA_CANCELLED)"; return 0 ;; esac
    atm_rea_manage remove "$root" "$ATM_REA_STATE_FILE" || { atm_warn "$(atm_t ATM_PLUGIN_REA_FAILED)"; return 1; }
    atm_rea_write_manifest "$root" '' || return 1
    atm_success "$(atm_t ATM_PLUGIN_REA_REMOVED)"
}

atm_rea_menu() {
    local choice="" root=""
    while true; do
        clear
        printf '%s\nhttps://github.com/morluto/rea\nhttps://rea.tools/\n' "$(atm_t ATM_PLUGIN_REA_TITLE)"
        atm_rea_status
        printf '\n1) %s\n2) %s\n3) %s\n4) %s\n5) %s\nb) %s\nq) %s\n%s ' \
            "$(atm_t ATM_PLUGIN_REA_INSTALL)" "$(atm_t ATM_PLUGIN_REA_UPDATE)" "$(atm_t ATM_PLUGIN_REA_REMOVE)" \
            "$(atm_t ATM_PLUGIN_REA_DEPS)" "$(atm_t ATM_PLUGIN_REA_DETAILS)" "$(atm_t ATM_MENU_BACK)" \
            "$(atm_t ATM_MENU_EXIT)" "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice || return 0
        case "$choice" in
            1) if root="$(atm_rea_choose_path)"; then atm_rea_install --path "$root" || atm_warn "$(atm_t ATM_PLUGIN_REA_FAILED)"; fi ;;
            2) atm_rea_install --update || atm_warn "$(atm_t ATM_PLUGIN_REA_FAILED)" ;;
            3) atm_rea_uninstall || atm_warn "$(atm_t ATM_PLUGIN_REA_FAILED)" ;;
            4) atm_rea_dependencies || atm_warn "$(atm_t ATM_PLUGIN_REA_MISSING)" ;;
            5) root="$(atm_rea_root)" || return 1; atm_rea_usage "$root"; atm_info "$(atm_t ATM_PLUGIN_REA_OPTIONAL)" ;;
            b|B) return 0 ;; q|Q) exit 0 ;;
            *) atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)" ;;
        esac
        atm_prompt_continue
    done
}

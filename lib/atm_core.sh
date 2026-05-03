#!/usr/bin/env bash

ATM_VERSION="0.0.1"
ATM_REPO_URL="${ATM_REPO_URL:-https://github.com/atomycloud/atm}"

ATM_APPS_DIR="${ATM_APPS_DIR:-$HOME/Apps}"
ATM_CACHE_DIR="${ATM_CACHE_DIR:-$HOME/.cache/atm}"
ATM_STATE_DIR="${ATM_STATE_DIR:-$HOME/.local/share/atm}"
ATM_CONFIG_DIR="${ATM_CONFIG_DIR:-$HOME/.config/atm}"

ATM_CONFIG_FILE="${ATM_CONFIG_FILE:-$ATM_CONFIG_DIR/atm.conf}"
ATM_PLUGINS_CONFIG_FILE="${ATM_PLUGINS_CONFIG_FILE:-$ATM_CONFIG_DIR/plugins.conf}"

ATM_MANIFEST_DIR="${ATM_MANIFEST_DIR:-$ATM_STATE_DIR/manifests}"
ATM_HISTORY_DIR="${ATM_HISTORY_DIR:-$ATM_STATE_DIR/history}"
ATM_ROLLBACK_DIR="${ATM_ROLLBACK_DIR:-$ATM_STATE_DIR/rollback}"

ATM_LOG_DIR="${ATM_LOG_DIR:-$ATM_CACHE_DIR/logs}"
ATM_DOWNLOAD_DIR="${ATM_DOWNLOAD_DIR:-$ATM_CACHE_DIR/downloads}"
ATM_TMP_DIR="${ATM_TMP_DIR:-$ATM_CACHE_DIR/tmp}"

ATM_LANG_DIR="${ATM_LANG_DIR:-$ATM_HOME/lang}"
ATM_PLUGIN_DIR="${ATM_PLUGIN_DIR:-$ATM_HOME/plugins}"

ATM_DEBUG="${ATM_DEBUG:-0}"
ATM_DRY_RUN="${ATM_DRY_RUN:-0}"

declare -a ATM_ARGS=()

atm_debug() {
    [[ "${ATM_DEBUG:-0}" == "1" ]] || return 0
    printf '[DEBUG] %s\n' "$*" >&2
}

atm_info() {
    printf '%s\n' "$*"
}

atm_warn() {
    printf '⚠️ %s\n' "$*" >&2
}

atm_success() {
    printf '✅ %s\n' "$*"
}

atm_fail() {
    printf '❌ %s\n' "$*" >&2
    exit 1
}

atm_on_error() {
    local exit_code=$?
    printf '❌ ATM internal error: line=%s command=%q exit=%s\n' "${BASH_LINENO[0]:-unknown}" "${BASH_COMMAND:-unknown}" "$exit_code" >&2
    exit "$exit_code"
}

trap atm_on_error ERR

atm_core_source_libs() {
    local lib=""

    for lib in "$ATM_HOME"/lib/atm_*.sh; do
        [[ -f "$lib" ]] || continue
        [[ "$(basename "$lib")" == "atm_core.sh" ]] && continue

        # shellcheck source=/dev/null
        source "$lib"
    done
}

atm_core_parse_global_args() {
    ATM_ARGS=()

    while (($# > 0)); do
        case "$1" in
            --debug)
                ATM_DEBUG="1"
                export ATM_DEBUG
                shift
                ;;
            --dry-run)
                ATM_DRY_RUN="1"
                export ATM_DRY_RUN
                shift
                ;;
            --version|-v)
                printf 'atm %s\n' "$ATM_VERSION"
                exit 0
                ;;
            --help|-h)
                atm_cli_help
                exit 0
                ;;
            --)
                shift
                ATM_ARGS+=("$@")
                break
                ;;
            *)
                # First non-global argument found.
                # From here onward, all arguments belong to the command/plugin.
                ATM_ARGS+=("$@")
                break
                ;;
        esac
    done
}

atm_core_prepare_dirs() {
    local dir=""

    for dir in \
        "$ATM_APPS_DIR" \
        "$ATM_CACHE_DIR" \
        "$ATM_STATE_DIR" \
        "$ATM_CONFIG_DIR" \
        "$ATM_MANIFEST_DIR" \
        "$ATM_HISTORY_DIR" \
        "$ATM_ROLLBACK_DIR" \
        "$ATM_LOG_DIR" \
        "$ATM_DOWNLOAD_DIR" \
        "$ATM_TMP_DIR"
    do
        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            atm_debug "DRY-RUN mkdir -p $dir"
        else
            mkdir -p "$dir"
        fi
    done
}

atm_run() {
    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN:'
        printf ' %q' "$@"
        printf '\n'
        return 0
    fi

    "$@"
}

atm_require_commands() {
    local missing=()
    local cmd=""

    for cmd in "$@"; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            missing+=("$cmd")
        fi
    done

    if ((${#missing[@]} > 0)); then
        atm_fail "Missing required commands: ${missing[*]}"
    fi
}

atm_safe_slug() {
    local value="$1"

    value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
    value="$(printf '%s' "$value" | sed -E 's/[^a-z0-9._-]+/_/g; s/^_+//; s/_+$//')"

    [[ -n "$value" ]] || value="item"

    printf '%s\n' "$value"
}

atm_cli_help() {
    cat <<'EOF_HELP'
atm - Atomy Tools Modules

Usage:
  atm
  atm menu
  atm doctor
  atm plugins list
  atm install [plugin] [--version VERSION]
  atm remove <plugin> <version>
  atm uninstall <plugin|atm>
  atm use <plugin> <version>
  atm path apply
  atm setup [portable|system|status]
  atm self-update [--git|--tarball|--check]
  atm lang list
  atm lang set <locale>

Global options:
  --debug
  --dry-run
  --version
  --help
EOF_HELP
}

atm_core_dispatch() {
    local cmd="${ATM_ARGS[0]:-menu}"

    case "$cmd" in
        menu)
            atm_menu_main
            ;;
        doctor)
            atm_doctor_run
            ;;
        plugins)
            atm_plugins_cli "${ATM_ARGS[@]:1}"
            ;;
        install)
            atm_install_cli "${ATM_ARGS[@]:1}"
            ;;
        path)
            atm_path_cli "${ATM_ARGS[@]:1}"
            ;;
        setup)
            atm_setup_cli "${ATM_ARGS[@]:1}"
            ;;
        self-update)
            atm_self_update_cli "${ATM_ARGS[@]:1}"
            ;;
        remove)
            atm_remove_cli "${ATM_ARGS[@]:1}"
            ;;
        uninstall)
            atm_uninstall_cli "${ATM_ARGS[@]:1}"
            ;;
        use)
            atm_use_cli "${ATM_ARGS[@]:1}"
            ;;
        lang)
            atm_i18n_cli "${ATM_ARGS[@]:1}"
            ;;
        *)
            atm_fail "Unknown command: $cmd"
            ;;
    esac
}

atm_core_main() {
    atm_core_parse_global_args "$@"
    atm_core_source_libs

    atm_core_prepare_dirs
    atm_logs_init
    atm_config_init
    atm_i18n_init
    atm_plugin_load_all

    atm_debug "ATM_HOME=$ATM_HOME"
    atm_debug "ATM_APPS_DIR=$ATM_APPS_DIR"
    atm_debug "ATM_CACHE_DIR=$ATM_CACHE_DIR"
    atm_debug "ATM_CONFIG_FILE=$ATM_CONFIG_FILE"
    atm_debug "ATM_LANG=${ATM_LANG:-unset}"
    atm_debug "ATM_DRY_RUN=$ATM_DRY_RUN"

    atm_core_dispatch
}
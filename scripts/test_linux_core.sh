#!/usr/bin/env bash
# Human QA harness only. CORE services are implemented in Go, not this script.
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
    printf 'Run this script with bash; do not source it.\n' >&2
    return 1
fi
set -Eeuo pipefail

ATM_QA_HOME=''
ATM_QA_BINARY=''
ATM_QA_TUI=1

atm_qa_help() {
    printf '%s\n' \
        'Usage: bash scripts/test_linux_core.sh --binary FILE --home DIRECTORY [--no-tui]' \
        'Use only a locally built or independently trusted Linux ATM executable/runtime.' \
        'Runs offline CLI plans and optionally the dry-run TUI in temporary user directories.' \
        'No signing, network discovery, application installation or personal profile edits.'
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --binary|--home)
            [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || { atm_qa_help >&2; exit 2; }
            case "$1" in --binary) ATM_QA_BINARY="$2" ;; --home) ATM_QA_HOME="$2" ;; esac
            shift 2 ;;
        --no-tui) ATM_QA_TUI=0; shift ;;
        -h|--help) atm_qa_help; exit 0 ;;
        *) atm_qa_help >&2; exit 2 ;;
    esac
done

[[ "$(uname -s)" == Linux ]] || { printf 'Linux host required.\n' >&2; exit 2; }
[[ "$ATM_QA_BINARY" == /* && -f "$ATM_QA_BINARY" && -x "$ATM_QA_BINARY" && ! -L "$ATM_QA_BINARY" ]] || {
    printf 'Provide an absolute, trusted, regular executable with --binary.\n' >&2; exit 2;
}
[[ "$ATM_QA_HOME" == /* && -f "$ATM_QA_HOME/core-api.json" && -d "$ATM_QA_HOME/plugins" && -d "$ATM_QA_HOME/lang" ]] || {
    printf 'Provide an absolute ATM runtime directory with --home.\n' >&2; exit 2;
}
if [[ "$ATM_QA_TUI" == 1 && ( ! -t 0 || ! -t 1 ) ]]; then
    printf 'Use a real terminal for the TUI, or pass --no-tui.\n' >&2
    exit 2
fi

ATM_QA_ENV_BIN="$(command -v env)"
ATM_QA_ROOT="$(mktemp -d /tmp/atm-linux-core.XXXXXX)"
chmod 0700 "$ATM_QA_ROOT"
mkdir -p "$ATM_QA_ROOT"/{user,config,state,cache,apps,tmp,run}
printf 'Isolated QA directory: %s\n' "$ATM_QA_ROOT"
printf 'The source runtime is inspected read-only; every ATM command uses --dry-run.\n'

atm_qa_run() {
    "$ATM_QA_ENV_BIN" -i \
        HOME="$ATM_QA_ROOT/user" USERPROFILE="$ATM_QA_ROOT/user" \
        USER=atm-qa LOGNAME=atm-qa SHELL=/bin/bash \
        XDG_CONFIG_HOME="$ATM_QA_ROOT/config" APPDATA="$ATM_QA_ROOT/config" \
        XDG_DATA_HOME="$ATM_QA_ROOT/state" XDG_STATE_HOME="$ATM_QA_ROOT/state" \
        XDG_CACHE_HOME="$ATM_QA_ROOT/cache" XDG_RUNTIME_DIR="$ATM_QA_ROOT/run" \
        ATM_CONFIG_DIR="$ATM_QA_ROOT/config" ATM_CONFIG_FILE="$ATM_QA_ROOT/config/atm.conf" \
        ATM_PLUGINS_CONFIG_FILE="$ATM_QA_ROOT/config/plugins.conf" \
        ATM_STATE_DIR="$ATM_QA_ROOT/state" ATM_APPS_DIR="$ATM_QA_ROOT/apps" \
        ATM_LANG=en-us TMPDIR="$ATM_QA_ROOT/tmp" TMP="$ATM_QA_ROOT/tmp" TEMP="$ATM_QA_ROOT/tmp" \
        TERM="${TERM:-xterm-256color}" COLORTERM=truecolor LANG=C.UTF-8 PATH=/nonexistent \
        "$ATM_QA_BINARY" --home "$ATM_QA_HOME" --dry-run "$@"
}

atm_qa_check() {
    local label="$1"
    shift
    printf 'Checking %s... ' "$label"
    if atm_qa_run "$@" >> "$ATM_QA_ROOT/cli.log" 2>&1; then
        printf 'PASS\n'
    else
        printf 'FAIL; see %s/cli.log\n' "$ATM_QA_ROOT" >&2
        return 1
    fi
}

atm_qa_run --version
atm_qa_check doctor --json doctor
atm_qa_check plugins --json plugins list
atm_qa_check trust --json updates trust
atm_qa_check update-check-plan --json updates check
atm_qa_check core-update-plan --json updates core
atm_qa_check path-plan --json path apply
atm_qa_check plugin-integration-plan --json path apply --plugins
atm_qa_check portable-setup-plan --json setup portable
atm_qa_check full-setup-plan --json install
if [[ "$ATM_QA_TUI" == 1 ]]; then
    atm_qa_run --no-animation tui
fi
if [[ -n "$(find "$ATM_QA_ROOT" -mindepth 2 -print -quit)" ]]; then
    printf 'FAIL: an ATM dry-run operation wrote inside the isolated user directories: %s\n' "$ATM_QA_ROOT" >&2
    exit 1
fi
printf 'PASS: CLI dry-run checks completed without user-state writes.\n'
printf 'Human acceptance is not automatic. Record the checklist results separately.\n'
printf 'QA directory retained for inspection: %s\n' "$ATM_QA_ROOT"

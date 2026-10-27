#!/usr/bin/env bash
set -Eeuo pipefail

atm_release_test_main() {
    local repo arch release verifier lab runtime manifest signature archive no_tui=0
    repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
    case "$(uname -s)/$(uname -m)" in
        Linux/x86_64) arch=amd64 ;;
        Linux/aarch64|Linux/arm64) arch=arm64 ;;
        *) printf 'This test supports Linux amd64 or arm64 only.\n' >&2; return 2 ;;
    esac
    if [[ "${1:-}" == --no-tui ]]; then no_tui=1; shift; fi
    if (( $# > 2 )); then
        printf 'Usage: bash scripts/test_linux_release.sh [--no-tui] [RELEASE_DIR [TRUSTED_VERIFIER]]\n' >&2
        return 2
    fi
    release="${1:-$repo/binary/v0.0.2-6c8b9be}"
    verifier="${2:-$release/qa/atm-verifier-linux-$arch}"
    [[ -d "$release" && -x "$verifier" ]] || {
        printf 'Missing release directory or trusted native verifier: %s\n' "$verifier" >&2
        return 1
    }
    release="$(cd -- "$release" && pwd -P)"
    verifier="$(realpath -- "$verifier")"
    manifest="$release/signatures-linux-$arch/atm-0.0.2-linux-$arch.manifest.json"
    signature="$release/signatures-linux-$arch/atm-0.0.2-linux-$arch.manifest.sig"
    archive="$release/atm-0.0.2-linux-$arch.tar.gz"
    lab="$(mktemp -d "${TMPDIR:-/tmp}/atm-linux-human-test.XXXXXXXX")"
    printf 'Isolated test directory (retained): %s\n' "$lab"

    # Never import personal ATM preferences or use the real application paths.
    unset ATM_HOME ATM_CONFIG_FILE ATM_PLUGINS_CONFIG_FILE ATM_PLUGIN_DIR
    unset ATM_LANG ATM_DRY_RUN ATM_PLUGIN_SORT_ORDER
    export HOME="$lab/home"
    export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share"
    export XDG_CACHE_HOME="$HOME/.cache" XDG_STATE_HOME="$HOME/.local/state"
    export ATM_CONFIG_DIR="$XDG_CONFIG_HOME/atm" ATM_STATE_DIR="$XDG_DATA_HOME/atm"
    export ATM_APPS_DIR="$HOME/Apps"
    mkdir -p -- "$HOME" "$ATM_APPS_DIR"

    printf '\nVerify the signature and complete archive before extraction:\n'
    "$verifier" --home "$repo" --json updates verify-package "$manifest" "$signature" "$archive"
    # A modified manifest must fail authentication. Original release files stay intact.
    cp -- "$manifest" "$lab/modified.manifest.json"
    printf '\n' >> "$lab/modified.manifest.json"
    if "$verifier" --home "$repo" --json updates verify-package "$lab/modified.manifest.json" "$signature" "$archive" > "$lab/rejected.json" 2> "$lab/rejected.stderr"; then
        printf 'FAIL: modified manifest was accepted.\n' >&2
        return 1
    fi
    grep -q 'invalid signed package manifest' "$lab/rejected.stderr" || {
        printf 'FAIL: negative test failed for an unexpected reason. See %s\n' "$lab/rejected.stderr" >&2
        return 1
    }
    printf 'PASS: modified manifest rejected.\n'

    tar --extract --gzip --file "$archive" --directory "$lab" --no-same-owner
    runtime="$lab/atm-0.0.2-linux-$arch"
    "$runtime/atm" --version
    "$runtime/atm" --json updates trust
    "$runtime/atm" --json updates verify-package "$manifest" "$signature" "$archive"
    "$runtime/atm" --json doctor
    "$runtime/atm" plugins list
    "$runtime/atm" --dry-run --json updates core
    "$runtime/atm" --dry-run --json path apply --plugins > "$lab/integration-plan.json"
    printf 'Plugin integration dry-run plan: %s\n' "$lab/integration-plan.json"
    "$runtime/atm" --dry-run --json recover-core --root "$lab/missing-runtime"
    printf '\nPASS: offline checks. No production mutation was requested.\n'
    if (( ! no_tui )); then
        if [[ -t 0 && -t 1 ]]; then
            printf 'Opening the TUI in dry-run mode; no application installers will run.\n'
            "$runtime/atm" --dry-run --no-animation tui
        else
            printf 'No interactive terminal; TUI skipped. Run this script in your terminal.\n'
        fi
    fi
    printf '\nReview the test directory: %s\n' "$lab"
}

atm_release_test_main "$@"

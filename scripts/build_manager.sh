#!/usr/bin/env bash
# Local build orchestration; no installation, signing, upload or release.
set -Eeuo pipefail

ATM_BUILD_ROOT="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
ATM_BUILD_OUT="$ATM_BUILD_ROOT/binary"
ATM_BUILD_FORMAT=binaries
ATM_BUILD_TARGET=''
ATM_BUILD_MENU=0
ATM_BUILD_GO_PATH=''
ATM_BUILD_HASH=''
ATM_BUILD_TARGETS=(windows-amd64 linux-amd64 linux-arm64 darwin-amd64 darwin-arm64)

atm_build_help() {
    printf '%s\n' \
        '🏭 ATM Build Manager' \
        'Usage: bash scripts/build_manager.sh [options]' \
        '' \
        'No arguments: interactive menu (EOF exits without building).' \
        '  --target TARGET   windows-amd64, linux-amd64, linux-arm64,' \
        '                    darwin-amd64, darwin-arm64, or all' \
        '  --format FORMAT   binaries (default) or packages' \
        '  --out DIR         Output parent (default: <repository>/binary)' \
        '  --menu            Open menu, optionally with --format/--out' \
        '  -h, --help        Show help without probing Go or creating files' \
        '' \
        'ATM_BUILD_GO selects a Go executable; otherwise use go from PATH.' \
        'Requires the Go version declared in go.mod. No automatic tool installation.' \
        'Binaries need --home <ATM runtime directory>; packages include that runtime.' \
        'macOS builds are Terminal executables, not Finder .app bundles.'
}

atm_build_error() { printf '❌ ERROR: %s\n' "$*" >&2; }

atm_build_valid_target() {
    case "$1" in
        windows-amd64|linux-amd64|linux-arm64|darwin-amd64|darwin-arm64|all) return 0 ;;
        *) atm_build_error "Unsupported target: $1"; return 1 ;;
    esac
}

atm_build_go_host() {
    env GOOS= GOARCH= CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
        "$ATM_BUILD_GO_PATH" "$@"
}

atm_build_dependencies() {
    local go_dir required
    [[ -f "$ATM_BUILD_ROOT/go.mod" && -f "$ATM_BUILD_ROOT/cmd/atm/main.go" ]] || {
        atm_build_error 'ATM sources not found beside scripts/.'; return 1;
    }
    ATM_BUILD_GO_PATH="$(command -v "${ATM_BUILD_GO:-go}")" || {
        atm_build_error 'Go not found. Install Go or set ATM_BUILD_GO to its executable.'; return 1;
    }
    [[ -f "$ATM_BUILD_GO_PATH" && -x "$ATM_BUILD_GO_PATH" ]] || {
        atm_build_error 'ATM_BUILD_GO must resolve to an executable file.'; return 1;
    }
    go_dir="$(CDPATH='' cd -- "$(dirname -- "$ATM_BUILD_GO_PATH")" && pwd -P)" || return 1
    ATM_BUILD_GO_PATH="$go_dir/$(basename -- "$ATM_BUILD_GO_PATH")"
    # The packager starts `go` itself; keep its compiler consistent with this script.
    [[ "$(basename -- "$ATM_BUILD_GO_PATH")" == go || "$(basename -- "$ATM_BUILD_GO_PATH")" == go.exe ]] || {
        atm_build_error 'The selected compiler must be named go or go.exe.'; return 1;
    }
    export PATH="$go_dir:$PATH"
    required="$(atm_build_go_host list -m -f '{{.GoVersion}}')" || {
        atm_build_error 'Go cannot load this module. Check the required version in go.mod.'; return 1;
    }
    printf '🔧 Required Go: %s\n' "$required"
    atm_build_go_host version || return 1
    if [[ "$ATM_BUILD_FORMAT" == binaries ]]; then
        if command -v sha256sum >/dev/null 2>&1; then ATM_BUILD_HASH=sha256sum
        elif command -v shasum >/dev/null 2>&1; then ATM_BUILD_HASH=shasum
        else atm_build_error 'Binary builds require sha256sum or shasum.'; return 1
        fi
    fi
}

atm_build_commit() {
    local commit status
    commit="$(git -C "$ATM_BUILD_ROOT" rev-parse --verify HEAD 2>/dev/null)" || commit=development
    if [[ "$commit" =~ ^[0-9a-f]{40,64}$ ]]; then
        if status="$(git -C "$ATM_BUILD_ROOT" status --porcelain 2>/dev/null)"; then
            [[ -z "$status" ]] || commit="$commit-dirty"
        else commit="$commit-dirty"
        fi
    else commit=development
    fi
    printf '%s\n' "$commit"
}

atm_build_binary() {
    local target="$1" run_dir="$2" commit="$3" os arch name dir
    os="${target%-*}"; arch="${target##*-}"
    name=atm; [[ "$os" != windows ]] || name=atm.exe
    dir="$run_dir/$target"
    mkdir -- "$dir" || return 1
    if ! env GOOS="$os" GOARCH="$arch" GOAMD64=v1 GOARM64=v8.0 CGO_ENABLED=0 \
        GOTOOLCHAIN=local GOWORK=off "$ATM_BUILD_GO_PATH" build \
        -trimpath -buildvcs=false \
        -ldflags "-s -w -buildid= -X github.com/AtomyCloud/atm/internal/version.Commit=$commit" \
        -o "$dir/.building" ./cmd/atm; then
        rm -f -- "$dir/.building"
        return 1
    fi
    [[ -f "$dir/.building" && -s "$dir/.building" && ! -L "$dir/.building" ]] || {
        atm_build_error "Compiler did not produce a regular binary for $target"; return 1;
    }
    mv -- "$dir/.building" "$dir/$name" || return 1
    (
        cd -- "$dir" || exit 1
        if [[ "$ATM_BUILD_HASH" == sha256sum ]]; then sha256sum "$name" > "$name.sha256"
        else shasum -a 256 "$name" > "$name.sha256"
        fi
    ) || return 1
    printf '✅ Built: %s\n' "$dir/$name"
}

atm_build_run() (
    local selection="$1" run_dir target commit
    local targets=()
    atm_build_valid_target "$selection" || return 1
    if [[ "$selection" == all ]]; then targets=("${ATM_BUILD_TARGETS[@]}")
    else targets=("$selection")
    fi
    # Resolve the compiler before changing cwd, including relative ATM_BUILD_GO paths.
    ATM_BUILD_GO_PATH="$(command -v "${ATM_BUILD_GO:-go}")" || {
        atm_build_error 'Go not found. Set ATM_BUILD_GO or add Go to PATH.'; return 1;
    }
    case "$ATM_BUILD_GO_PATH" in /*) ;; *) ATM_BUILD_GO_PATH="$PWD/$ATM_BUILD_GO_PATH" ;; esac
    export ATM_BUILD_GO="$ATM_BUILD_GO_PATH"
    cd -- "$ATM_BUILD_ROOT" || return 1
    atm_build_dependencies || return 1
    mkdir -p -- "$ATM_BUILD_OUT" || return 1
    run_dir="$(mktemp -d "$ATM_BUILD_OUT/atm-build-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")" || return 1
    # This freshly allocated run owns these files; existing outputs are never touched.
    printf '*\n' > "$run_dir/.gitignore" || return 1
    printf 'This build did not complete. Earlier successful targets may be present.\n' > "$run_dir/BUILD-INCOMPLETE" || return 1
    commit="$(atm_build_commit)" || return 1
    printf 'format=%s\ncommit=%s\ntarget=%s\n' "$ATM_BUILD_FORMAT" "$commit" "$selection" > "$run_dir/build-info.txt" || return 1
    printf '\n📂 Output: %s\n' "$run_dir"
    for target in "${targets[@]}"; do
        printf '\n🔨 Building %s (%s)...\n' "$target" "$ATM_BUILD_FORMAT"
        if [[ "$ATM_BUILD_FORMAT" == packages ]]; then
            if ! atm_build_go_host run ./tools/package -os "${target%-*}" -arch "${target##*-}" -out "$run_dir"; then
                atm_build_error "Build failed: $target. Incomplete output retained at $run_dir"; return 1
            fi
        elif ! atm_build_binary "$target" "$run_dir" "$commit"; then
            atm_build_error "Build failed: $target. Incomplete output retained at $run_dir"; return 1
        fi
    done
    rm -- "$run_dir/BUILD-INCOMPLETE" || return 1
    printf '\n✅ Build completed: %s\n' "$run_dir"
    if [[ "$ATM_BUILD_FORMAT" == binaries ]]; then
        printf 'ℹ️  Binary-only output: run with --home pointing to a matching ATM runtime.\n'
        printf '📦 For standalone distributions with plugins/locales, use --format packages.\n'
    fi
)

atm_build_menu() {
    local choice target
    while true; do
        printf '\n🏭 ATM Build Manager\n===================\n📂 Output: %s\n📦 Format: %s\n' "$ATM_BUILD_OUT" "$ATM_BUILD_FORMAT"
        printf '%s\n' \
            '1) 🪟 Windows (amd64)' '2) 🐧 Linux (amd64)' '3) 🐧 Linux (arm64)' \
            '4) 🍎 macOS (Intel)' '5) 🍎 macOS (Apple Silicon)' '6) 🚀 Build ALL platforms' \
            '7) 🔄 Switch format: binaries / portable packages' '0) 🚪 Exit'
        printf '👉 Choose option [0]: '
        IFS= read -r choice || return 0
        case "$choice" in
            0|'') return 0 ;;
            1) target=windows-amd64 ;; 2) target=linux-amd64 ;; 3) target=linux-arm64 ;;
            4) target=darwin-amd64 ;; 5) target=darwin-arm64 ;; 6) target=all ;;
            7) if [[ "$ATM_BUILD_FORMAT" == binaries ]]; then ATM_BUILD_FORMAT=packages; else ATM_BUILD_FORMAT=binaries; fi; continue ;;
            *) atm_build_error 'Invalid option.'; continue ;;
        esac
        if ! atm_build_run "$target"; then atm_build_error 'Build stopped; no installation or upload was performed.'; fi
        printf '\n↩️  Press Enter to return to the menu: '
        IFS= read -r choice || return 0
    done
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --target|--format|--out)
            [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || { atm_build_error "Missing value for $1"; exit 2; }
            case "$1" in --target) ATM_BUILD_TARGET="$2" ;; --format) ATM_BUILD_FORMAT="$2" ;; --out) ATM_BUILD_OUT="$2" ;; esac
            shift 2 ;;
        --menu) ATM_BUILD_MENU=1; shift ;;
        -h|--help) atm_build_help; exit 0 ;;
        *) atm_build_error "Unknown argument: $1"; atm_build_help >&2; exit 2 ;;
    esac
done
case "$ATM_BUILD_FORMAT" in binaries|packages) ;; *) atm_build_error 'Format must be binaries or packages.'; exit 2 ;; esac
[[ "$ATM_BUILD_MENU" == 0 || -z "$ATM_BUILD_TARGET" ]] || { atm_build_error '--menu and --target cannot be combined.'; exit 2; }
case "$ATM_BUILD_OUT" in /*) ;; *) ATM_BUILD_OUT="$PWD/$ATM_BUILD_OUT" ;; esac
if [[ -n "$ATM_BUILD_TARGET" ]]; then atm_build_run "$ATM_BUILD_TARGET"
else atm_build_menu
fi

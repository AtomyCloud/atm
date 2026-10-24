#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-docker-v14.4.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_HOME="$PWD"
ATM_APPS_DIR="$task_root/apps"
ATM_DOWNLOAD_DIR="$task_root/downloads"
ATM_MANIFEST_DIR="$task_root/manifests"
source lib/atm_core.sh
source lib/atm_i18n.sh
source lib/atm_manifest.sh
source lang/en-us.lang
source plugins/docker/lang/en-us.lang
source plugins/docker/plugin.conf
source plugins/docker/plugin.sh
clear() { :; }
atm_docker_desktop_user_home() { printf '%s\n' "$task_root/user"; }
atm_docker_require_desktop_platform() { :; }
log_file="$task_root/commands"
touch "$log_file"

systemctl() {
    local unit="${@: -1}"
    printf 'system %s\n' "$*" >> "$log_file"
    if [[ -e "$task_root/system.$unit" ]]; then printf 'active\n'; else printf 'inactive\n'; fi
}
pgrep() {
    if [[ "$1" == -x ]]; then [[ -e "$task_root/dockerd" ]]; else [[ -e "$task_root/backend" ]]; fi
}
gpg() { printf 'UNEXPECTED_REAL_GPG\n' >&2; return 99; }
pass() { printf 'UNEXPECTED_REAL_PASS\n' >&2; return 99; }
atm_docker_run_as_user() {
    local unit="${@: -1}"
    printf 'user %s\n' "$*" >> "$log_file"
    if [[ "$1" == systemctl ]]; then
        if [[ "$3" == show ]]; then
            if [[ -e "$task_root/user.$unit" ]]; then printf 'active\n'; else printf 'inactive\n'; fi
        elif [[ "$3" == stop ]]; then
            [[ "${STOP_FAIL:-0}" == 0 ]] || return 23
            rm -f "$task_root/user.$unit"
            [[ "$unit" != docker-desktop ]] || rm -f "$task_root/backend"
        fi
    elif [[ "$1 $2" == 'docker desktop' ]]; then
        [[ "${STOP_FAIL:-0}" == 0 ]] || return 23
        rm -f "$task_root/backend"
    elif [[ "$1" == gpg ]]; then
        if [[ "$2" == --generate-key ]]; then
            touch "$task_root/generated"
        else
            [[ "${GPG_FAIL:-0}" == 0 ]] || return 24
            if [[ -e "$task_root/generated" || "${NO_KEYS:-0}" == 0 ]]; then
                printf 'sec:u:3072:1:123:0:0:::::scESC:\nfpr:::::::::ABCDEF1234567890ABCDEF1234567890ABCDEF12:\n'
            fi
            printf 'sec:r:3072:1:456:0:0:::::E:\nfpr:::::::::REVOKED:\n'
            printf 'sec:u:3072:1:789:0:0:::::scESCD:\nfpr:::::::::DISABLED:\n'
            printf 'sec:e:3072:1:888:0:0:::::E:\nfpr:::::::::EXPIRED:\n'
            printf 'sec:u:3072:1:999:0:0:::::scSC:\nfpr:::::::::SIGNONLY:\n'
        fi
    elif [[ "$1" == pass ]]; then
        mkdir -p "$task_root/user/.password-store"
        printf '%s\n' "$3" > "$task_root/user/.password-store/.gpg-id"
    fi
}
atm_docker_run_elevated() {
    printf 'elevated %s\n' "$1" >> "$log_file"
    [[ "${STOP_FAIL:-0}" == 0 ]] || return 23
    if [[ "$1" == 'systemctl stop '* ]]; then
        local unit=""
        for unit in ${1#systemctl stop }; do rm -f "$task_root/system.$unit"; done
        rm -f "$task_root/dockerd"
    fi
}

touch "$task_root/user.docker-desktop" "$task_root/backend" "$task_root/system.docker" "$task_root/system.docker.socket" "$task_root/system.containerd" "$task_root/dockerd"
atm_docker_stop_running
[[ ! -e "$task_root/backend" && ! -e "$task_root/dockerd" && ! -e "$task_root/system.docker.socket" ]]
grep -q 'user systemctl --user stop docker-desktop' "$log_file"
grep -q 'elevated systemctl stop docker docker.socket containerd' "$log_file"
printf 'PASS active Desktop, Engine, socket and associated containerd are stopped\n'

touch "$task_root/user.docker-desktop" "$task_root/backend" "$task_root/system.docker" "$task_root/dockerd"
atm_docker_stop_running desktop
[[ ! -e "$task_root/backend" && -e "$task_root/system.docker" && -e "$task_root/dockerd" ]]
rm "$task_root/system.docker" "$task_root/dockerd"
printf 'PASS Desktop removal shutdown leaves the independent Engine running\n'

touch "$task_root/system.containerd"
atm_docker_stop_running
[[ -e "$task_root/system.containerd" ]]
printf 'PASS unrelated containerd is preserved when Docker is not active\n'

touch "$task_root/user.docker" "$task_root/user.docker.socket"
atm_docker_stop_running
[[ ! -e "$task_root/user.docker" && ! -e "$task_root/user.docker.socket" ]]
printf 'PASS rootless Docker services are stopped\n'

touch "$task_root/backend"
atm_docker_stop_running
[[ ! -e "$task_root/backend" ]]
grep -q 'user docker desktop stop --timeout 30' "$log_file"
printf 'PASS unmanaged Desktop backend uses the synchronous Desktop CLI\n'

touch "$task_root/user.docker-desktop" "$task_root/backend"
if output="$(STOP_FAIL=1 atm_docker_stop_running 2>&1)"; then exit 1; fi
[[ -e "$task_root/backend" && "$output" == *'could not be stopped'* ]]
before="$(cksum "$log_file")"
ATM_DRY_RUN=1 atm_docker_stop_running
[[ -e "$task_root/backend" && "$(cksum "$log_file")" == "$before" ]]
rm "$task_root/user.docker-desktop" "$task_root/backend"
printf 'PASS stop failure aborts and dry-run never changes services\n'

[[ "$(atm_docker_desktop_gpg_keys)" == ABCDEF1234567890ABCDEF1234567890ABCDEF12 ]]
output="$(atm_docker_setup_desktop_signin <<< 1)"
[[ "$(<"$task_root/user/.password-store/.gpg-id")" == ABCDEF1234567890ABCDEF1234567890ABCDEF12 ]]
[[ "$output" == *'pass is initialized'* ]]
before="$(cksum "$log_file")"
output="$(atm_docker_setup_desktop_signin </dev/null)"
[[ "$(cksum "$log_file")" == "$before" ]]
printf 'PASS usable secret key initializes pass once and preserves an existing store\n'

rm "$task_root/user/.password-store/.gpg-id"
output="$(NO_KEYS=1 atm_docker_setup_desktop_signin <<< $'g\n1')"
[[ -e "$task_root/generated" && -s "$task_root/user/.password-store/.gpg-id" ]]
printf 'PASS interactive GPG generation is followed by key selection and pass initialization\n'

rm "$task_root/user/.password-store/.gpg-id"
before="$(cksum "$log_file")"
ATM_DRY_RUN=1 atm_docker_setup_desktop_signin
[[ ! -e "$task_root/user/.password-store/.gpg-id" && "$(cksum "$log_file")" == "$before" ]]
printf 'PASS credential dry-run executes no GPG/pass/package commands\n'

output="$(atm_docker_setup_desktop_signin <<< b)"
[[ ! -e "$task_root/user/.password-store/.gpg-id" ]]
printf 'PASS credential setup cancellation preserves the uninitialized store\n'

if output="$(GPG_FAIL=1 atm_docker_setup_desktop_signin <<< 1 2>&1)"; then exit 1; fi
[[ ! -e "$task_root/user/.password-store/.gpg-id" && "$output" == *'credential setup failed'* ]]
printf 'PASS GPG listing errors abort without initializing pass\n'

(
    command() {
        if [[ "$1" == -v && "${2:-}" == pass && ! -e "$task_root/deps-installed" ]]; then return 1; fi
        builtin command "$@"
    }
    atm_docker_run_elevated() {
        printf 'deps %s\n' "$1" >> "$log_file"
        [[ "$1" != 'apt-get install -y gnupg pass' ]] || touch "$task_root/deps-installed"
        return 0
    }
    atm_docker_setup_desktop_signin <<< b >/dev/null
    [[ -e "$task_root/deps-installed" ]]
)
printf 'PASS missing credential dependencies are installed before setup\n'

atm_docker_status() { printf 'test\n'; }
output="$(ATM_DRY_RUN=1 atm_docker_menu <<< $'6\nxb')"
[[ "$output" == *'6) Set up Docker Desktop sign-in'* && "$output" == *'DRY-RUN: Install missing GPG/pass'* && "$output" != *'[missing:'* ]]
output="$(ATM_DRY_RUN=1 atm_docker_desktop_menu <<< $'8\nxb' 2>&1)"
[[ "$output" == *'8) Set up Docker Desktop sign-in'* && "$output" == *'DRY-RUN: Install missing GPG/pass'* ]]
printf 'PASS main and Desktop menus route to sign-in setup\n'

(
    source plugins/docker/plugin.sh
    ATM_DRY_RUN=1
    atm_docker_stop_running() { printf 'STOP_HOOK\n'; }
    atm_docker_desktop_installed() { return 0; }
    atm_docker_desktop_version() { printf '0.0.0\n'; }
    atm_docker_require_desktop_platform() { :; }
    atm_docker_write_manifest() { :; }
    output="$(atm_docker_install_desktop 4.96.3 2>&1)"
    [[ "$output" == *STOP_HOOK*'apt-get install -y --allow-downgrades'* ]]
    output="$(atm_docker_install_engine 2>&1)"
    [[ "$output" == *STOP_HOOK*'curl -fsSL'* ]]
    output="$(atm_docker_uninstall_desktop <<< y 2>&1)"
    [[ "$output" == *STOP_HOOK*'apt-get purge -y docker-desktop'* ]]
    atm_docker_stop_running() { return 23; }
    if output="$(atm_docker_install_desktop 4.96.3 2>&1)"; then exit 1; fi
    [[ "$output" != *'apt-get install -y --allow-downgrades'* ]]
)
printf 'PASS stop hook runs before Engine/Desktop installation and removal and blocks failed shutdown\n'

(
    source plugins/docker/plugin.sh
    atm_run() { printf '%s\n' "$@"; }
    atm_docker_target_user() { command id -un; }
    output="$(atm_docker_run_as_user pass init TEST_KEY)"
    [[ "$output" == $'pass\ninit\nTEST_KEY' ]]
    atm_docker_target_user() { printf '__atm_test_user__\n'; }
    id() {
        if [[ "$*" == '-u __atm_test_user__' ]]; then printf '12345\n'; else command id "$@"; fi
    }
    output="$(atm_docker_run_as_user pass init TEST_KEY)"
    [[ "$output" == *'__atm_test_user__'* && "$output" == *'XDG_RUNTIME_DIR=/run/user/12345'* && "$output" == *$'pass\ninit\nTEST_KEY' ]]
)
printf 'PASS target-user wrapper preserves identity and user session paths without executing credentials\n'

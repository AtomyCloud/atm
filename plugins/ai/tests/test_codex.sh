#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-ai-codex.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_HOME="$PWD"
ATM_APPS_DIR="$task_root/apps with spaces"
ATM_DOWNLOAD_DIR="$task_root/downloads with spaces"
ATM_MANIFEST_DIR="$task_root/manifests"
source lib/atm_core.sh
source lib/atm_i18n.sh
source lib/atm_manifest.sh
source lang/en-us.lang
source plugins/ai/lang/en-us.lang
source plugins/ai/plugin.conf
source plugins/ai/plugin.sh
clear() { :; }
log_file="$task_root/commands"
touch "$log_file"

atm_ai_hermes_installed() { return 1; }
atm_ai_hermes_desktop_installed() { return 1; }
atm_ai_hermes_desktop_portable_installed() { return 1; }
atm_ai_ollama_installed() { return 1; }
atm_ai_picoclaw_installed() { return 1; }
atm_ai_codex_desktop_manager() { printf '%s\n' "${TEST_MANAGER:-apt}"; }
atm_ai_codex_desktop_installed() { [[ -e "$task_root/desktop-installed" ]]; }
atm_ai_codex_desktop_version() { printf '26.1000\n'; }
atm_ai_codex_cli_version() { printf '0.143.0\n'; }
npm() {
    printf 'npm %s\n' "$*" >> "$log_file"
    [[ "${TEST_NPM_FAIL:-0}" == 0 ]] || return 22
    [[ "$3" == --prefix && "$4" == "$ATM_AI_CODEX_CLI_PREFIX" ]]
    if [[ "$1" == uninstall ]]; then
        rm "$ATM_AI_CODEX_CLI_PREFIX/bin/codex" "$ATM_AI_CODEX_CLI_PREFIX/lib/node_modules/@openai/codex/package.json"
    else
        mkdir -p "$ATM_AI_CODEX_CLI_PREFIX/bin" "$ATM_AI_CODEX_CLI_PREFIX/lib/node_modules/@openai/codex"
        ln -sfn /usr/bin/true "$ATM_AI_CODEX_CLI_PREFIX/bin/codex"
        printf '{"version":"0.143.0"}\n' > "$ATM_AI_CODEX_CLI_PREFIX/lib/node_modules/@openai/codex/package.json"
    fi
}
node() { printf 'UNEXPECTED_NODE_EXECUTION\n' >&2; return 99; }
hermes-agent() { :; }
hermes-desktop() { :; }
ollama() { :; }
picoclaw() { :; }
curl() {
    printf 'curl %s\n' "$*" >> "$log_file"
    [[ "${TEST_DOWNLOAD_FAIL:-0}" == 0 ]] || return 22
    touch "${@: -1}"
}
dpkg-deb() {
    case "$3" in
        Package) printf '%s\n' "${TEST_PACKAGE:-chatgpt}" ;;
        Architecture) printf '%s\n' "${TEST_ARCH:-amd64}" ;;
    esac
}
rpm() {
    if [[ "$3" == '%{ARCH}' ]]; then
        printf '%s\n' "${TEST_RPM_ARCH:-x86_64}"
    else
        printf '%s\n' "${TEST_PACKAGE:-chatgpt}"
    fi
}
atm_ai_run_elevated() {
    printf 'elevated %s\n' "$1" >> "$log_file"
    [[ "${TEST_ELEVATED_FAIL:-0}" == 0 ]] || return 23
    if [[ "$1" == *remove* || "$1" == 'pacman -R '* ]]; then
        rm -f "$task_root/desktop-installed"
    else
        touch "$task_root/desktop-installed"
    fi
}
uname() {
    case "$1" in
        -s) printf '%s\n' "${TEST_OS:-Linux}" ;;
        -m) printf '%s\n' "${TEST_CPU:-x86_64}" ;;
    esac
}

output="$(atm_ai_codex_cli_action install)"
atm_ai_codex_cli_installed
[[ "$(atm_ai_path_entries)" == "$ATM_AI_CODEX_CLI_PREFIX/bin" ]]
grep -q 'ATM_AI_CODEX_CLI_INSTALLED=1' "$ATM_AI_MANIFEST_FILE"
(
    source "$ATM_AI_MANIFEST_FILE"
    [[ "$ATM_PLUGIN_NAME" == 'AI Tools' && "$ATM_AI_CODEX_CLI_PREFIX" == "$ATM_APPS_DIR/ai/codex/cli" ]]
)
grep -q 'npm install --global --prefix' "$log_file"
output="$(atm_ai_codex_cli_action update)"
[[ "$output" == *'Codex operation completed.'* ]]
before="$(cksum "$log_file")"
output="$(atm_ai_codex_cli_action remove <<< n 2>&1)"
[[ "$(cksum "$log_file")" == "$before" ]]
atm_ai_codex_cli_installed
printf 'PASS isolated CLI install/update, manifest/PATH registration, and removal cancellation\n'

output="$(atm_ai_codex_cli_action remove <<< y)"
if atm_ai_codex_cli_installed; then exit 1; fi
grep -q 'npm uninstall --global --prefix' "$log_file"
before="$(cksum "$log_file")"
output="$(atm_ai_codex_cli_action update 2>&1)"
[[ "$output" == *'No ATM-managed Codex CLI installation'* && "$(cksum "$log_file")" == "$before" ]]
printf 'PASS CLI removal only targets the managed prefix and leaves external installations alone\n'

if output="$(TEST_NPM_FAIL=1 atm_ai_codex_cli_action install 2>&1)"; then exit 1; fi
[[ "$output" == *'Codex installation, update, or removal failed.'* ]]
printf 'PASS npm failure does not report success\n'

output="$(atm_ai_codex_desktop_action install)"
[[ -e "$task_root/desktop-installed" ]]
grep -q 'chatgpt_amd64.deb' "$log_file"
grep -q 'apt-get install -y' "$log_file"
grep -q 'ATM_AI_CODEX_DESKTOP_INSTALLED=1' "$ATM_AI_MANIFEST_FILE"
output="$(atm_ai_codex_desktop_action update)"
grep -q 'apt-get install -y --only-upgrade chatgpt' "$log_file"
output="$(atm_ai_codex_desktop_action remove <<< y)"
[[ ! -e "$task_root/desktop-installed" ]]
printf 'PASS APT desktop install/update/remove and manifest state\n'

if output="$(TEST_PACKAGE=wrong-package atm_ai_codex_desktop_action install 2>&1)"; then exit 1; fi
[[ ! -e "$task_root/desktop-installed" && "$output" == *'not the expected ChatGPT'* ]]
if output="$(TEST_DOWNLOAD_FAIL=1 atm_ai_codex_desktop_action install 2>&1)"; then exit 1; fi
[[ ! -e "$task_root/desktop-installed" ]]
if output="$(TEST_ELEVATED_FAIL=1 atm_ai_codex_desktop_action install 2>&1)"; then exit 1; fi
[[ ! -e "$task_root/desktop-installed" ]]
printf 'PASS package mismatch, download failure, and package-manager failure abort desktop installation\n'

[[ "$(TEST_CPU=aarch64 atm_ai_codex_desktop_arch)" == arm64 ]]
[[ "$(atm_ai_codex_desktop_url apt arm64)" == */chatgpt_arm64.deb ]]
[[ "$(atm_ai_codex_desktop_url dnf amd64)" == */chatgpt.x86_64.rpm ]]
[[ "$(atm_ai_codex_desktop_url dnf arm64)" == */chatgpt.aarch64.rpm ]]
[[ "$(atm_ai_codex_desktop_url pacman arm64)" == */install-arch.sh ]]
if TEST_CPU=riscv64 atm_ai_codex_desktop_arch; then exit 1; fi
if TEST_OS=Darwin atm_ai_codex_desktop_arch; then exit 1; fi
TEST_MANAGER=dnf atm_ai_codex_desktop_action install >/dev/null
TEST_MANAGER=dnf atm_ai_codex_desktop_action update >/dev/null
TEST_MANAGER=dnf atm_ai_codex_desktop_action remove <<< y >/dev/null
if output="$(TEST_MANAGER=dnf TEST_RPM_ARCH=aarch64 atm_ai_codex_desktop_action install 2>&1)"; then exit 1; fi
[[ ! -e "$task_root/desktop-installed" ]]
TEST_MANAGER=pacman atm_ai_codex_desktop_action install >/dev/null
TEST_MANAGER=pacman atm_ai_codex_desktop_action update >/dev/null
TEST_MANAGER=pacman atm_ai_codex_desktop_action remove <<< y >/dev/null
grep -q 'dnf upgrade -y --refresh chatgpt' "$log_file"
grep -q 'pacman -Syu' "$log_file"
printf 'PASS desktop architecture selection and DNF/Arch lifecycle command generation\n'

source plugins/ai/plugin.sh
atm_ai_codex_desktop_installed() { return 1; }
atm_ai_codex_desktop_manager() { printf 'apt\n'; }
before="$(cksum "$log_file")"
manifest_before="$(cksum "$ATM_AI_MANIFEST_FILE")"
output="$(ATM_DRY_RUN=1 atm_ai_codex_cli_action install)"
[[ "$output" == *'DRY-RUN: npm install'* && ! -x "$ATM_AI_CODEX_CLI_PREFIX/bin/codex" ]]
output="$(ATM_DRY_RUN=1 atm_ai_codex_desktop_action install)"
[[ "$output" == *'DRY-RUN: curl'* && "$output" == *'DRY-RUN: apt-get install'* ]]
[[ "$(cksum "$log_file")" == "$before" && "$(cksum "$ATM_AI_MANIFEST_FILE")" == "$manifest_before" ]]
printf 'PASS dry-run executes no package/download/version commands and changes no manifest\n'

atm_ai_status() { printf 'test\n'; }
output="$(ATM_DRY_RUN=1 atm_ai_menu <<< $'4\n1\nxb\nb')"
[[ "$output" == *'4) Codex'* && "$output" == *'6) Remove Codex Desktop (ChatGPT app)'* && "$output" == *'DRY-RUN: npm install'* && "$output" != *'[missing:'* ]]
printf 'PASS AI to Codex routing and all six actions use English locale strings\n'

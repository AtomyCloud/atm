#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$repo_root"
source lib/atm_i18n.sh
source plugins/os_packages/plugin.sh
ATM_PLUGIN_DIR="$repo_root/plugins"
ATM_DRY_RUN=1
source plugins/os_packages/lang/en-us.lang
baseline_detect="$(atm_os_packages_detect)"
baseline_commands="$(atm_os_packages_show_commands)"

for locale in en-us pt-br pt-pt es it fr de ru ja zh-cn ko; do
    source "plugins/os_packages/lang/$locale.lang"
    [[ "$(atm_os_packages_detect)" == "$baseline_detect" ]]
    [[ "$(atm_os_packages_show_commands)" == "$baseline_commands" ]]
    output="$(atm_os_packages_run_command 'exit 99')"
    [[ "$output" == "$ATM_PLUGIN_OS_PACKAGES_DRY_RUN: exit 99" ]]
    [[ "$ATM_PLUGIN_OS_PACKAGES_CONFIRM" == *'[y/N]:' ]]
    (
        atm_warn() { printf '%s\n' "$1"; }
        atm_success() { printf '%s\n' "$1"; }
        atm_os_packages_packages_for_manager() { printf 'fixture-package\n'; }
        atm_os_packages_write_manifest() { printf 'fixture-manifest:%s\n' "$1"; }
        output="$(atm_os_packages_uninstall <<< n)"
        [[ "$output" == *"$ATM_PLUGIN_OS_PACKAGES_CONFIRM"* ]]
        [[ "$output" == *"$ATM_PLUGIN_OS_PACKAGES_CANCELLED"* ]]
        [[ "$output" != *fixture-manifest:* ]]
        output="$(atm_os_packages_uninstall <<< y)"
        [[ "$output" == *"$ATM_PLUGIN_OS_PACKAGES_DRY_RUN:"* ]]
        [[ "$output" == *fixture-manifest:0* ]]
        [[ "$output" == *"$ATM_PLUGIN_OS_PACKAGES_UNINSTALLED"* ]]
    )
    printf 'PASS %s: command/TSV invariants, no-execution dry-run, y/N confirmation\n' "$locale"
done

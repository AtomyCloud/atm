#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-zsh-lifecycle.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_APPS_DIR="$task_root/apps with spaces"
ATM_DOWNLOAD_DIR="$task_root/downloads"
ATM_MANIFEST_DIR="$task_root/manifests"
ATM_ZSH_USER_HOME="$task_root/user home"
ATM_ZSH_DOT_DIR="$ATM_ZSH_USER_HOME/dot files"
ATM_ZSH_OMZ_DIR="$ATM_ZSH_USER_HOME/framework"
ATM_ZSH_CUSTOM_DIR="$ATM_ZSH_OMZ_DIR/custom"
ATM_ZSH_BACKUP_DIR="$ATM_APPS_DIR/Backups"
source lib/atm_core.sh
source lib/atm_i18n.sh
source lib/atm_manifest.sh
source lang/en-us.lang
source plugins/zsh/lang/en-us.lang
source plugins/zsh/plugin.conf
source plugins/zsh/plugin.sh
export TEST_LOG="$task_root/commands"
mkdir -p "$ATM_ZSH_DOT_DIR" "$task_root/bin"
touch "$TEST_LOG"
printf 'export MY_CONFIG=preserved\n' > "$ATM_ZSH_DOT_DIR/.zshrc"
printf 'my shell history\n' > "$ATM_ZSH_DOT_DIR/.zsh_history"
printf '/bin/bash\n' > "$task_root/login-shell"
export PATH="$task_root/bin:$PATH"

# Executables invoked through env must also be isolated from the real system.
cat > "$task_root/bin/zsh" <<'MOCK'
#!/bin/sh
printf 'zsh %s\n' "$*" >> "$TEST_LOG"
if [ "$1" = --version ]; then printf 'zsh 5.9\n'; fi
MOCK
chmod +x "$task_root/bin/zsh"
clear() { :; }
git() { printf 'mock-revision\n'; }
atm_zsh_login_shell() { cat "$task_root/login-shell"; }
chsh() {
    printf 'chsh %s\n' "$*" >> "$TEST_LOG"
    [[ "${TEST_CHSH_FAIL:-0}" == 0 ]] || return 2
    [[ "${TEST_CHSH_NO_CHANGE:-0}" == 0 ]] || return 0
    printf '%s\n' "$2" > "$task_root/login-shell"
}
atm_zsh_run_elevated() {
    if [[ "${ATM_DRY_RUN:-0}" == 1 ]]; then atm_run "$@"; return; fi
    printf 'elevated %s\n' "$*" >> "$TEST_LOG"
    [[ "${TEST_APT_FAIL:-0}" == 0 ]]
}
curl() {
    printf 'curl %s\n' "$*" >> "$TEST_LOG"
    [[ "${TEST_CURL_FAIL:-0}" == 0 ]] || return 22
    cat > "${@: -1}" <<'MOCK'
#!/bin/sh
set -eu
[ "$CHSH" = no ] && [ "$RUNZSH" = no ] && [ "$KEEP_ZSHRC" = yes ]
[ "$REMOTE" = https://github.com/ohmyzsh/ohmyzsh.git ]
[ "$1" = --unattended ] && [ "$2" = --keep-zshrc ]
printf 'installer %s\n' "$*" >> "$TEST_LOG"
mkdir -p "$ZSH/tools" "$ZSH/custom"
printf '# framework\n' > "$ZSH/oh-my-zsh.sh"
printf '# updater\n' > "$ZSH/tools/upgrade.sh"
MOCK
}

output="$(atm_zsh_install)"
[[ -f "$ATM_ZSH_OMZ_DIR/oh-my-zsh.sh" ]]
grep -q 'apt-get install -y zsh git curl python3' "$TEST_LOG"
grep -q 'installer --unattended --keep-zshrc' "$TEST_LOG"
grep -q 'MY_CONFIG=preserved' "$ATM_ZSH_DOT_DIR/.zshrc"
grep -q 'ATM_PLUGIN_VERSION="0.0.1"' "$ATM_ZSH_MANIFEST_FILE"
[[ "$(atm_zsh_login_shell)" == /bin/bash ]]
count_before="$(atm_zsh_list_backups | wc -l)"
[[ "$count_before" == 1 ]]
printf 'PASS install uses official unattended flags, preserves settings, writes manifest, and leaves login shell unchanged\n'

output="$(atm_zsh_update)"
grep -q 'apt-get install -y --only-upgrade zsh' "$TEST_LOG"
grep -q 'zsh -f .*tools/upgrade.sh' "$TEST_LOG"
[[ "$(atm_zsh_list_backups | wc -l)" == 2 ]]
printf 'PASS update creates a backup and uses the official framework updater\n'

before="$(cksum "$TEST_LOG")"
output="$(atm_zsh_uninstall <<< n)"
[[ "$(cksum "$TEST_LOG")" == "$before" && -d "$ATM_ZSH_OMZ_DIR" ]]
printf '/usr/bin/zsh\n' > "$task_root/login-shell"
if output="$(TEST_CHSH_FAIL=1 atm_zsh_uninstall <<< y 2>&1)"; then exit 1; fi
[[ -d "$ATM_ZSH_OMZ_DIR" ]]
if grep -q 'apt-get remove' "$TEST_LOG"; then exit 1; fi
printf 'PASS cancellation and failed login-shell changes cannot remove Zsh\n'

output="$(atm_zsh_uninstall <<< y)"
[[ ! -e "$ATM_ZSH_OMZ_DIR" && ! -e "$ATM_ZSH_MANIFEST_FILE" ]]
[[ "$(atm_zsh_login_shell)" == /bin/bash ]]
grep -q 'MY_CONFIG=preserved' "$ATM_ZSH_DOT_DIR/.zshrc"
if grep -q '# >>> ATM Oh My Zsh >>>' "$ATM_ZSH_DOT_DIR/.zshrc"; then exit 1; fi
[[ -d "$ATM_ZSH_BACKUP_DIR" ]]
printf 'PASS removal switches login shell safely, retains settings and backups, and removes framework/manifest\n'

archive="$(atm_zsh_list_backups | tail -n 1)"
printf 'changed config\n' > "$ATM_ZSH_DOT_DIR/.zshrc"
printf 'changed history\n' > "$ATM_ZSH_DOT_DIR/.zsh_history"
before_count="$(atm_zsh_list_backups | wc -l)"
output="$(atm_zsh_restore "$archive" <<< y)"
[[ -f "$ATM_ZSH_OMZ_DIR/oh-my-zsh.sh" ]]
grep -q 'MY_CONFIG=preserved' "$ATM_ZSH_DOT_DIR/.zshrc"
grep -q 'my shell history' "$ATM_ZSH_DOT_DIR/.zsh_history"
[[ "$(atm_zsh_list_backups | wc -l)" == $((before_count + 1)) ]]
printf 'PASS restore validates snapshots, saves current files first, and restores framework/configuration\n'

printf 'invalid' > "$task_root/invalid.tar.gz"
before_count="$(atm_zsh_list_backups | wc -l)"
if output="$(atm_zsh_restore "$task_root/invalid.tar.gz" <<< y 2>&1)"; then exit 1; fi
[[ "$(atm_zsh_list_backups | wc -l)" == "$before_count" ]]
[[ -f "$ATM_ZSH_OMZ_DIR/oh-my-zsh.sh" ]]
printf 'PASS invalid archives fail before backup or replacement\n'

before="$(cksum "$TEST_LOG")"
manifest_before="$(cksum "$ATM_ZSH_MANIFEST_FILE")"
before_count="$(atm_zsh_list_backups | wc -l)"
output="$(ATM_DRY_RUN=1 atm_zsh_install)"
output+="$(ATM_DRY_RUN=1 atm_zsh_update)"
output+="$(ATM_DRY_RUN=1 atm_zsh_backup)"
output+="$(ATM_DRY_RUN=1 atm_zsh_restore "$archive" <<< y)"
output+="$(ATM_DRY_RUN=1 atm_zsh_uninstall <<< y)"
[[ "$output" == *DRY-RUN* ]]
[[ "$(cksum "$TEST_LOG")" == "$before" && "$(cksum "$ATM_ZSH_MANIFEST_FILE")" == "$manifest_before" ]]
[[ "$(atm_zsh_list_backups | wc -l)" == "$before_count" && -d "$ATM_ZSH_OMZ_DIR" ]]
output="$(ATM_DRY_RUN=1 atm_zsh_menu <<< $'4\nxb')"
[[ "$output" == *'5) Restore backup'* && "$output" != *'[missing:'* ]]
printf 'PASS all lifecycle dry-runs and menu routing are read-only\n'

if output="$(TEST_APT_FAIL=1 atm_zsh_update 2>&1)"; then exit 1; fi
[[ "$output" != *'updated. Restart'* ]]
rm -rf "$ATM_ZSH_OMZ_DIR"
if output="$(TEST_CURL_FAIL=1 atm_zsh_install 2>&1)"; then exit 1; fi
[[ ! -d "$ATM_ZSH_OMZ_DIR" && "$output" != *'installed. Run zsh'* ]]
printf 'PASS package-manager and download failures do not report success\n'

(
    printf '# registered shells\n/bin/bash\n/bin/sh\n/bin/bash\n/missing/shell\nrelative-shell\n' > "$task_root/shells"
    [[ "$(atm_zsh_available_shells "$task_root/shells")" == $'/bin/bash\n/bin/sh' ]]
    [[ -z "$(atm_zsh_available_shells "$task_root/missing-shells")" ]]
    atm_zsh_available_shells() { printf '/bin/bash\n/bin/sh\n'; }
    output="$(atm_zsh_set_default <<< $'2\ny')"
    [[ "$output" == *'/bin/bash (current)'* && "$(atm_zsh_login_shell)" == /bin/sh ]]
    [[ "$output" == *'You must log out and log back in for the new default shell to take effect.'* ]]
    before="$(cksum "$TEST_LOG")"
    output="$(atm_zsh_set_default <<< 2)"
    [[ "$output" == *'already your default shell'* && "$(cksum "$TEST_LOG")" == "$before" ]]
    output="$(atm_zsh_set_default <<< $'1\nn')"
    output="$(atm_zsh_set_default <<< b)"
    output="$(atm_zsh_set_default <<< '1+1' 2>&1)"
    output="$(atm_zsh_set_default < /dev/null)"
    [[ "$(cksum "$TEST_LOG")" == "$before" ]]
    output="$(ATM_DRY_RUN=1 atm_zsh_set_default <<< $'1\ny')"
    [[ "$output" == *'DRY-RUN: chsh -s /bin/bash'* && "$output" != *'You must log out'* ]]
    [[ "$(cksum "$TEST_LOG")" == "$before" && "$(atm_zsh_login_shell)" == /bin/sh ]]
    if output="$(TEST_CHSH_FAIL=1 atm_zsh_set_default <<< $'1\ny' 2>&1)"; then exit 1; fi
    [[ "$output" != *'You must log out'* ]]
    if output="$(TEST_CHSH_NO_CHANGE=1 atm_zsh_set_default <<< $'1\ny' 2>&1)"; then exit 1; fi
    [[ "$output" != *'You must log out'* ]]
    output="$(ATM_DRY_RUN=1 atm_zsh_menu <<< $'7\n1\ny\nxb')"
    [[ "$output" == *'7) Choose default shell'* && "$output" == *'DRY-RUN: chsh -s /bin/bash'* ]]
    [[ "$output" != *'[missing:'* ]]
    atm_zsh_available_shells() { :; }
    output="$(atm_zsh_set_default 2>&1)"
    [[ "$output" == *'No installed login shells'* ]]
)
printf 'PASS shell picker filters/deduplicates paths, marks current shell, verifies changes, and handles cancellation/failure/dry-run\n'

(
    unset ATM_ZSH_BACKUP_DIR
    source plugins/zsh/plugin.conf
    [[ "$ATM_ZSH_BACKUP_DIR" == "$ATM_APPS_DIR/Backups" ]]
    mkdir -p "$ATM_APPS_DIR/backups/zsh"
    legacy_archive="$ATM_APPS_DIR/backups/zsh/zsh-legacy.tar.gz"
    cp "$archive" "$legacy_archive"
    output="$(atm_zsh_list_backups)"
    [[ "$output" == *"$legacy_archive"* && "$output" == *"$ATM_ZSH_BACKUP_DIR"* ]]
    ATM_ZSH_BACKUP_DIR="$task_root/custom backups"
    source plugins/zsh/plugin.conf
    [[ "$ATM_ZSH_BACKUP_DIR" == "$task_root/custom backups" && -z "$(atm_zsh_list_backups)" ]]
)
printf 'PASS default backups use Apps/Backups, legacy archives remain listed, and custom backup paths are respected\n'

(
    source plugins/zsh/plugin.sh
    sudo() { printf 'sudo %s\n' "$*" >> "$TEST_LOG"; }
    apt-get() { printf 'apt-get %s\n' "$*" >> "$TEST_LOG"; }
    atm_zsh_run_elevated apt-get update >/dev/null
    if [[ "$EUID" != 0 ]]; then
        grep -q 'sudo --validate' "$TEST_LOG"
        grep -q 'sudo -- apt-get update' "$TEST_LOG"
        sudo() { return 1; }
        if atm_zsh_run_elevated apt-get update >/dev/null; then exit 1; fi
    fi
)
printf 'PASS privileged runner uses sudo validation for normal users and propagates failures\n'

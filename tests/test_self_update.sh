#!/usr/bin/env bash
set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp="$(mktemp -d /tmp/atm-self-update.XXXXXX)"
trap 'rm -rf -- "$temp"' EXIT
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME=ATM-Test GIT_AUTHOR_EMAIL=atm@example.invalid
export GIT_COMMITTER_NAME=ATM-Test GIT_COMMITTER_EMAIL=atm@example.invalid
export PYTHONDONTWRITEBYTECODE=1
ATM_HOME="$temp/installed"
ATM_LANG_DIR="$repo/lang"
ATM_REPO_URL="$temp/remote.git"
ATM_SELF_UPDATE_BRANCH=main
SECONDS=120
source "$repo/lib/atm_core.sh"
source "$repo/lib/atm_i18n.sh"
source "$repo/lib/atm_self_update.sh"
source "$repo/lib/atm_ui.sh"
source "$repo/lang/en-us.lang"
git init --bare --initial-branch=main "$ATM_REPO_URL" >/dev/null
git init --initial-branch=main "$temp/author" >/dev/null
git -C "$temp/author" commit --allow-empty -qm first
git -C "$temp/author" remote add origin "$ATM_REPO_URL"
git -C "$temp/author" push -q origin main
git clone -q "$ATM_REPO_URL" "$ATM_HOME"
log="$temp/lookups"
touch "$log"
timeout() {
    printf 'lookup\n' >> "$log"
    command timeout "$@"
}
snapshot() {
    python3 -B -c 'import hashlib, pathlib, sys
root=pathlib.Path(sys.argv[1])
for p in sorted(root.rglob("*")):
    st=p.lstat()
    content=hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else "directory"
    print(p.relative_to(root), st.st_mode, st.st_mtime_ns, content)
' "$ATM_HOME"
}

before="$(snapshot)"
atm_self_update_refresh
[[ "$ATM_SELF_UPDATE_STATUS" == current && "$(snapshot)" == "$before" ]]
[[ "$(wc -l < "$log")" == 1 ]]
atm_self_update_refresh
[[ "$(wc -l < "$log")" == 1 ]]
ATM_SELF_UPDATE_CHECK_AT=$((SECONDS - 61))
atm_self_update_refresh
[[ "$(wc -l < "$log")" == 2 ]]
printf 'PASS identical commit, in-memory cache/expiry, and unchanged Git/worktree files\n'

git -C "$temp/author" commit --allow-empty -qm second
git -C "$temp/author" push -q origin main
remote="$(git -C "$temp/author" rev-parse HEAD)"
before="$(snapshot)"
atm_self_update_refresh --force
[[ "$ATM_SELF_UPDATE_STATUS" == available && "$ATM_SELF_UPDATE_REMOTE_COMMIT" == "$remote" ]]
[[ "$(snapshot)" == "$before" ]]
[[ "$(atm_self_update_menu_label)" == 'Self-update Update' ]]
(
    atm_self_update_color_enabled() { return 0; }
    [[ "$(atm_self_update_menu_label)" == $'Self-update \033[34mUpdate\033[0m' ]]
)
if NO_COLOR=1 atm_self_update_color_enabled; then exit 1; fi
output="$(atm_self_update_cli --check)"
[[ "$output" == *"Latest remote commit: $remote"* && "$output" == *'Update available.'* ]]
[[ "$output" == *'atm self-update --git'* && "$(snapshot)" == "$before" ]]
printf 'PASS latest remote commit without fetch/pull, blue ANSI/reset, plain output, and explicit check details\n'

# Fixture-only fetch/merge makes the remote commit locally available for graph cases.
git -C "$ATM_HOME" fetch -q origin
atm_self_update_refresh --force
[[ "$ATM_SELF_UPDATE_STATUS" == available ]]
git -C "$ATM_HOME" merge -q --ff-only origin/main
git -C "$ATM_HOME" commit --allow-empty -qm local
atm_self_update_refresh
[[ "$ATM_SELF_UPDATE_STATUS" == ahead && "$(atm_self_update_menu_label)" == Self-update ]]
git -C "$temp/author" commit --allow-empty -qm divergent
git -C "$temp/author" push -q origin main
git -C "$ATM_HOME" fetch -q origin
before="$(snapshot)"
atm_self_update_refresh --force
[[ "$ATM_SELF_UPDATE_STATUS" == diverged && "$(snapshot)" == "$before" ]]
[[ "$(atm_self_update_menu_label)" == 'Self-update Update' ]]
output="$(atm_self_update_cli --check)"
[[ "$output" == *'have diverged'* ]]
if atm_self_update_cli --git > "$temp/pull-result" 2>&1; then exit 1; fi
[[ "$(git -C "$ATM_HOME" rev-parse HEAD)" == "$ATM_SELF_UPDATE_CURRENT_COMMIT" ]]
printf 'PASS behind/ahead/diverged detection and fast-forward refusal preserves local commits\n'

(
    timeout() { return 124; }
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == unknown && -z "$ATM_SELF_UPDATE_REMOTE_COMMIT" ]]
    [[ "$(atm_self_update_menu_label)" == Self-update ]]
    output="$(atm_self_update_cli --check)"
    [[ "$output" == *'Could not check'* && "$output" != *'up to date'* ]]
)
(
    timeout() { printf 'not-a-commit\trefs/heads/main\n'; }
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == unknown ]]
)
(
    timeout() { printf '%s\trefs/heads/wrong\n' "$remote"; }
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == unknown ]]
)
(
    ATM_SELF_UPDATE_BRANCH=missing
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == unknown ]]
    ATM_SELF_UPDATE_BRANCH='../invalid'
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == unknown ]]
)
printf 'PASS timeout/offline, malformed/foreign refs, missing/invalid branch, and stale badge clearing\n'

(
    timeout() { printf 'UNEXPECTED_NETWORK\n' >&2; return 99; }
    before="$(snapshot)"
    ATM_DRY_RUN=1 atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == dry_run && -z "$ATM_SELF_UPDATE_REMOTE_COMMIT" ]]
    output="$(ATM_DRY_RUN=1 atm_self_update_cli --check 2>&1)"
    [[ "$output" == *'Network lookup skipped'* && "$output" != *UNEXPECTED_NETWORK* ]]
    output="$(ATM_DRY_RUN=1 atm_self_update_cli --git)"
    [[ "$output" == *'DRY-RUN: git'* && "$output" == *'pull --ff-only'* ]]
    [[ "$(snapshot)" == "$before" ]]
)
(
    ATM_HOME="$temp/not-a-clone"
    mkdir -p "$ATM_HOME"
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == not_git && "$(atm_self_update_menu_label)" == Self-update ]]
)
(
    git -C "$temp/author" worktree add -qb feature "$temp/worktree"
    ATM_HOME="$temp/worktree"
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == current ]]
    if atm_self_update_cli --git > "$temp/branch-result" 2>&1; then exit 1; fi
    grep -q 'configured self-update branch' "$temp/branch-result"
    git -C "$ATM_HOME" checkout -q --detach
    if atm_self_update_cli --git > "$temp/detached-result" 2>&1; then exit 1; fi
)
printf 'PASS network-free/no-write dry-run, non-clone, linked worktree, and wrong/detached branch protection\n'

(
    ATM_HOME="$temp/updatable"
    git clone -q "$ATM_REPO_URL" "$ATM_HOME"
    printf 'user notes\n' > "$ATM_HOME/local-notes"
    git -C "$temp/author" commit --allow-empty -qm next
    git -C "$temp/author" push -q origin main
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == available ]]
    atm_self_update_cli --git > "$temp/update-result" 2>&1
    [[ -z "$ATM_SELF_UPDATE_CHECK_KEY" ]]
    [[ "$(git -C "$ATM_HOME" rev-parse HEAD)" == "$(git -C "$temp/author" rev-parse HEAD)" ]]
    [[ "$(cat "$ATM_HOME/local-notes")" == 'user notes' ]]
    atm_self_update_refresh
    [[ "$ATM_SELF_UPDATE_STATUS" == current ]]
)
printf 'PASS explicit fast-forward update, preserved user files, cache invalidation, and cleared indicator\n'

(
    git -C "$ATM_HOME" config remote.origin.promisor true
    git() {
        case " $* " in *' cat-file '*|*' merge-base '*) return 99 ;; esac
        command git "$@"
    }
    atm_self_update_refresh --force
    [[ "$ATM_SELF_UPDATE_STATUS" == available ]]
    command git -C "$ATM_HOME" config --unset remote.origin.promisor
)
(
    unset ATM_SELF_UPDATE_STATUS_CURRENT ATM_SELF_UPDATE_REPOSITORY
    ATM_MENU_SELF_UPDATE=Localized
    [[ "$(atm_self_update_t ATM_SELF_UPDATE_STATUS_CURRENT)" == 'ATM is up to date with the remote branch.' ]]
    [[ "$ATM_MENU_SELF_UPDATE" == Localized ]]
)
printf 'PASS partial-clone graph skipping and English fallback preserves active locale\n'

(
    atm_menu_header() { :; }
    atm_plugin_list_sorted_ids() { :; }
    atm_prompt_continue() { :; }
    output="$(atm_menu_main <<< $'u\nq')"
    [[ "$output" == *'u) ♻️  Self-update Update'* && "$output" == *'Latest remote commit:'* ]]
)
printf 'PASS actual main-menu Update indicator and u status action\n'

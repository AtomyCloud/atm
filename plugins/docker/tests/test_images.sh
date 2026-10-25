#!/usr/bin/env bash
set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
temp="$(mktemp -d /tmp/atm-docker-images.XXXXXX)"
trap 'rm -rf -- "$temp"' EXIT
ATM_HOME="$repo"
ATM_APPS_DIR="$temp/apps"
ATM_MANIFEST_DIR="$temp/manifests"
ATM_DOWNLOAD_DIR="$temp/downloads"
unset ATM_DOCKER_IMAGE_BACKUP_DIR
source "$repo/lib/atm_core.sh"
source "$repo/lib/atm_i18n.sh"
source "$repo/lang/en-us.lang"
source "$repo/plugins/docker/plugin.conf"
source "$repo/plugins/docker/plugin.sh"
source "$repo/plugins/docker/lang/en-us.lang"
clear() { :; }
log="$temp/docker.log"
loaded="$temp/loaded"
touch "$log" "$loaded"
mkdir -p "$temp/fixture"
printf '[]\n' > "$temp/fixture/manifest.json"
id_a="sha256:$(printf '%064d' 1)"
id_b="sha256:$(printf '%064d' 2)"

docker() {
    printf '%q ' "$@" >> "$log"
    printf '\n' >> "$log"
    local count=0
    case "$1 ${2:-}" in
        'info --format')
            [[ "${TEST_FAIL:-}" != info ]] || return 33
            if [[ -f "$temp/info-count" ]]; then count="$(<"$temp/info-count")"; fi
            count=$((count + 1))
            printf '%s\n' "$count" > "$temp/info-count"
            if [[ -n "${TEST_CHANGE_AT:-}" ]] && ((count >= TEST_CHANGE_AT)); then
                printf 'daemon-B\n'
            else
                printf 'daemon-A\n'
            fi ;;
        'image ls')
            [[ "${TEST_FAIL:-}" != ls ]] || return 34
            [[ "${TEST_EMPTY:-0}" != 1 ]] || return 0
            printf 'registry.local:5000/team/app\tv1\t%s\t12MB\n' "$id_a"
            printf 'registry.local:5000/team/app\tstable\t%s\t12MB\n' "$id_a"
            printf '<none>\t<none>\t%s\t4MB\n' "$id_b"
            printf '<none>\t<none>\t%s\t4MB\n' "$id_b" ;;
        'image inspect')
            [[ "$3" == -- && "${TEST_FAIL:-}" != inspect && "$4" != missing ]] ;;
        'image save')
            [[ "$3" == --output && "$5" == -- ]]
            printf '%s\0' "${@:6}" > "$temp/saved-refs"
            if [[ "${TEST_FAIL:-}" == save ]]; then printf partial > "$4"; return 35; fi
            [[ "${TEST_FAIL:-}" != empty-save ]] || return 0
            tar -cf "$4" -C "$temp/fixture" manifest.json
            if [[ -n "${TEST_RACE_TARGET:-}" ]]; then printf 'concurrent archive\n' > "$TEST_RACE_TARGET"; fi ;;
        'image load')
            [[ "$3" == --input && "$#" == 4 ]]
            [[ "${TEST_FAIL:-}" != load && "$4" != *fail.tar ]] || return 36
            printf '%s\n' "$4" >> "$loaded"
            printf 'Loaded image: fixture:latest\n' ;;
        *) printf 'UNEXPECTED_DOCKER_COMMAND\n' >&2; return 99 ;;
    esac
}

[[ "$ATM_DOCKER_IMAGE_BACKUP_DIR" == "$ATM_APPS_DIR/Backups/docker" ]]
(
    ATM_DOCKER_IMAGE_BACKUP_DIR="$temp/custom images"
    source "$repo/plugins/docker/plugin.conf"
    [[ "$ATM_DOCKER_IMAGE_BACKUP_DIR" == "$temp/custom images" ]]
)
one="$temp/output with spaces/selected.tar"
output="$(atm_docker_image_export registry.local:5000/team/app:v1 "$one" <<< y)"
[[ "$output" == *'Docker image archive created:'* && -s "$one" ]]
[[ "$(stat -c %a "$one")" == 600 ]]
[[ "$(tar -tf "$one")" == manifest.json ]]
mapfile -d '' -t refs < "$temp/saved-refs"
[[ "${refs[*]}" == registry.local:5000/team/app:v1 ]]
all="$temp/all.tar"
atm_docker_images_cli export --all --output "$all" <<< y > "$temp/all-result"
mapfile -d '' -t refs < "$temp/saved-refs"
[[ "${#refs[@]}" == 3 && "${refs[0]}" == registry.local:5000/team/app:v1 ]]
[[ "${refs[1]}" == registry.local:5000/team/app:stable && "${refs[2]}" == "$id_b" ]]
printf 'PASS selected/all image export, tags/registry ports, untagged ID deduplication, private permissions, and spaces\n'

before="$(sha256sum "$one")"
if atm_docker_image_export app:v1 "$one" <<< y > "$temp/error" 2>&1; then exit 1; fi
[[ "$(sha256sum "$one")" == "$before" ]]
ln -s "$one" "$temp/symlink.tar"
if atm_docker_image_export app:v1 "$temp/symlink.tar" <<< y > "$temp/error" 2>&1; then exit 1; fi
for bad in relative.tar "$temp/no-extension"; do
    if atm_docker_image_export app:v1 "$bad" > "$temp/error" 2>&1; then exit 1; fi
done
if atm_docker_image_export --bad "$temp/no.tar" > "$temp/error" 2>&1; then exit 1; fi
if atm_docker_image_export app:v1 "$temp/no.tar" 1 > "$temp/error" 2>&1; then exit 1; fi
for failure in info ls inspect save empty-save; do
    method=0
    image=app:v1
    if [[ "$failure" == ls ]]; then method=1; image=""; fi
    if TEST_FAIL="$failure" atm_docker_image_export "$image" "$temp/$failure/new.tar" "$method" <<< y > "$temp/error" 2>&1; then exit 1; fi
    [[ ! -e "$temp/$failure/new.tar" ]]
    [[ -z "$(find "$temp/$failure" -name '*.part' -print 2>/dev/null || true)" ]]
done
output="$(TEST_EMPTY=1 atm_docker_image_export '' "$temp/empty/new.tar" 1 <<< y)"
[[ "$output" == *'No images found'* && ! -e "$temp/empty" ]]
atm_docker_image_export app:v1 "$temp/cancel/new.tar" <<< n > "$temp/cancel-output"
atm_docker_image_export app:v1 "$temp/cancel/new.tar" < /dev/null > "$temp/cancel-output"
[[ ! -e "$temp/cancel" ]]
race="$temp/race.tar"
if TEST_RACE_TARGET="$race" atm_docker_image_export app:v1 "$race" <<< y > "$temp/error" 2>&1; then exit 1; fi
[[ "$(<"$race")" == 'concurrent archive' ]]
[[ -z "$(find "$temp" -name '*.part' -print)" ]]
printf 'PASS collision/symlink protection, validation, no images, cancellation/EOF, and failed/empty saves clean partial files\n'

rm -f "$temp/info-count"
if TEST_CHANGE_AT=2 atm_docker_image_export app:v1 "$temp/changed/new.tar" <<< y > "$temp/error" 2>&1; then exit 1; fi
[[ ! -e "$temp/changed" ]]
grep -q 'daemon changed' "$temp/error"
before="$(sha256sum "$one")"
output="$(atm_docker_images_restore "$one" <<< y)"
[[ "$output" == *'Existing tag references can change'* && "$output" == *'Docker image archive loaded:'* ]]
[[ "$(<"$loaded")" == "$one" && "$(sha256sum "$one")" == "$before" ]]
loaded_before="$(cksum "$loaded")"
atm_docker_images_restore "$one" <<< n > "$temp/cancel-load"
atm_docker_images_restore "$one" < /dev/null > "$temp/cancel-load"
[[ "$(cksum "$loaded")" == "$loaded_before" ]]
touch "$temp/empty.tar"
for bad in "$temp/missing.tar" "$temp/empty.tar" "$temp/symlink.tar"; do
    if atm_docker_images_restore "$bad" <<< y > "$temp/error" 2>&1; then exit 1; fi
done
if TEST_FAIL=load atm_docker_images_restore "$one" <<< y > "$temp/error" 2>&1; then exit 1; fi
[[ "$(cksum "$loaded")" == "$loaded_before" ]]
printf 'PASS restore confirmation/tag warning, source preservation, invalid files, cancellation, and load failure\n'

mkdir -p "$temp/batch"
cp "$one" "$temp/batch/a.tar"
cp "$one" "$temp/batch/b fail.tar"
cp "$one" "$temp/batch/c.tar"
if atm_docker_images_cli restore-all --directory "$temp/batch" <<< y > "$temp/batch-output" 2>&1; then exit 1; fi
[[ "$(tail -n 1 "$loaded")" == "$temp/batch/a.tar" ]]
if grep -q '/batch/c.tar' "$loaded"; then exit 1; fi
rm "$temp/batch/b fail.tar"
atm_docker_images_cli restore-all --directory "$temp/batch" <<< y > "$temp/batch-output"
[[ "$(tail -n 1 "$loaded")" == "$temp/batch/c.tar" ]]
mkdir -p "$temp/no-archives"
output="$(atm_docker_images_cli restore-all --directory "$temp/no-archives" 2>&1)"
[[ "$output" == *'No .tar archives'* ]]
rm -f "$temp/info-count"
loaded_before="$(cksum "$loaded")"
if TEST_CHANGE_AT=2 atm_docker_images_restore "$one" <<< y > "$temp/error" 2>&1; then exit 1; fi
[[ "$(cksum "$loaded")" == "$loaded_before" ]]
printf 'PASS batch load success/stop-on-failure, empty folder, and daemon change protection\n'

before="$(cksum "$log")"
loaded_before="$(cksum "$loaded")"
output="$(ATM_DRY_RUN=1 atm_docker_images_cli export --image app:v1 --output "$temp/dry/new.tar")"
[[ "$output" == *'DRY-RUN:'* && ! -e "$temp/dry" ]]
ATM_DRY_RUN=1 atm_docker_images_cli export --all --output "$temp/dry/all.tar" > "$temp/dry-plan"
ATM_DRY_RUN=1 atm_docker_images_cli restore --input "$one" > "$temp/dry-plan"
ATM_DRY_RUN=1 atm_docker_images_cli restore-all --directory "$temp/batch" > "$temp/dry-plan"
[[ "$(cksum "$log")" == "$before" && "$(cksum "$loaded")" == "$loaded_before" && ! -e "$temp/dry" ]]
for args in 'export --input bad' 'restore --all' 'export --image' 'unknown' 'menu --all'; do
    if atm_docker_images_cli $args > "$temp/error" 2>&1; then exit 1; fi
done
printf 'PASS all action dry-runs execute no Docker commands/writes and strict CLI dispatch rejects invalid flags\n'

atm_docker_status() { printf 'fixture\n'; }
output="$(atm_docker_menu <<< $'7\nb\nb')"
[[ "$output" == *'7) Export / Restore images'* && "$output" == *'4) Restore ALL .tar archives'* ]]
input="$(printf '1\n1\n%s\ny\nxb\n' "$temp/menu-selected.tar")"
output="$(atm_docker_images_menu <<< "$input")"
[[ -s "$temp/menu-selected.tar" && "$output" == *'Docker image archive created:'* ]]
output="$(atm_docker_images_restore_prompt <<< $'m\n'"$one"$'\ny')"
[[ "$output" == *'Docker image archive loaded:'* ]]
before="$(cksum "$log")"
output="$(ATM_DRY_RUN=1 atm_docker_images_menu <<< $'1\nm\napp:v1\n\nxb')"
[[ "$output" == *'DRY-RUN:'* && "$(cksum "$log")" == "$before" ]]
atm_docker_install_engine() { printf 'UNEXPECTED_INSTALL\n'; return 99; }
output="$(ATM_DRY_RUN=1 atm_docker_install --images export --all)"
[[ "$output" == *'DRY-RUN:'* && "$output" != *UNEXPECTED_INSTALL* ]]
printf 'PASS main/submenu routing, selected image/manual restore, no-Docker menu dry-run, and install callback isolation\n'

#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$repo_root"
task_root="$(mktemp -d /tmp/atm-trivy-lifecycle.XXXXXX)"
trap 'rm -rf "$task_root"' EXIT
ATM_HOME="$repo_root"
ATM_APPS_DIR="$task_root/apps with spaces"
ATM_STATE_DIR="$task_root/state"
ATM_DOWNLOAD_DIR="$task_root/downloads"
ATM_MANIFEST_DIR="$task_root/manifests"
ATM_TRIVY_INSTALL_ROOT="$ATM_APPS_DIR/trivy"
ATM_TRIVY_CACHE_DIR="$ATM_DOWNLOAD_DIR/trivy"
ATM_TRIVY_DOCKER_STATE="$ATM_STATE_DIR/trivy/docker-version"
source lib/atm_core.sh
source lib/atm_manifest.sh
source lib/atm_i18n.sh
source lang/en-us.lang
source plugins/trivy/plugin.conf
source plugins/trivy/plugin.sh
source plugins/trivy/lang/en-us.lang
clear() { :; }
fixture_dir="$task_root/fixtures"
log="$task_root/commands"
mkdir -p "$fixture_dir"
touch "$log"

python3 -B -c 'import hashlib, io, json, pathlib, sys, tarfile
root=pathlib.Path(sys.argv[1])
for version in ("0.76.1", "0.75.0"):
    name=f"trivy_{version}_Linux-64bit.tar.gz"
    payload=b"#!/usr/bin/env bash\necho TRIVY_BINARY_EXECUTED >&2\nexit 99\n"
    with tarfile.open(root/name, "w:gz") as output:
        member=tarfile.TarInfo("trivy"); member.size=len(payload); member.mode=0o755
        output.addfile(member, io.BytesIO(payload))
    checksum_name=f"trivy_{version}_checksums.txt"
    (root/checksum_name).write_text(hashlib.sha256((root/name).read_bytes()).hexdigest()+"  "+name+"\n")
    assets=[{"name": item, "browser_download_url": f"https://github.com/aquasecurity/trivy/releases/download/v{version}/{item}"} for item in (name, checksum_name)]
    (root/f"{version}.json").write_text(json.dumps({"tag_name": "v"+version, "draft": False, "prerelease": False, "assets": assets}))
' "$fixture_dir"

uname() { case "$1" in -s) printf '%s\n' "${TEST_OS:-Linux}" ;; -m) printf '%s\n' "${TEST_CPU:-x86_64}" ;; esac; }
curl() {
    printf 'curl %s\n' "$*" >> "$log"
    [[ "${TEST_RELEASE_FAIL:-0}" == 0 ]] || return 22
    case "${@: -1}" in
        */latest|*/tags/v0.76.1) cat "$fixture_dir/0.76.1.json" ;;
        */tags/v0.75.0) cat "$fixture_dir/0.75.0.json" ;;
        *) return 22 ;;
    esac
}
atm_download_file() {
    printf 'download %s\n' "$1" >> "$log"
    mkdir -p "${2%/*}"
    cp "$fixture_dir/${1##*/}" "$2"
    if [[ "${TEST_BAD_ARCHIVE:-0}" == 1 && "$2" == *.tar.gz ]]; then printf bad > "$2"; fi
}
docker() {
    printf 'docker %s\n' "$*" >> "$log"
    case "$1 ${2:-}" in
        'info ')
            [[ "${TEST_DOCKER_FAIL:-}" != info ]] ;;
        'pull '*)
            [[ "${TEST_DOCKER_FAIL:-}" != pull ]] || return 2
            printf '%s\n' "$2" > "$task_root/docker-tag" ;;
        'image inspect')
            [[ "${TEST_DOCKER_FAIL:-}" != inspect && -f "$task_root/docker-tag" ]] ;;
        'image ls')
            if [[ -f "$task_root/docker-tag" ]]; then cat "$task_root/docker-tag"; fi ;;
        'image rm')
            [[ "${TEST_DOCKER_FAIL:-}" != rm ]] || return 3
            [[ "$3" == aquasec/trivy:* && "$#" == 3 ]]
            rm -f "$task_root/docker-tag" ;;
        *) printf 'Unexpected Docker action\n' >&2; return 99 ;;
    esac
}

[[ "$(TEST_CPU=aarch64 atm_trivy_platform)" == Linux-ARM64 ]]
[[ "$(TEST_CPU=armv7l atm_trivy_platform)" == Linux-ARM ]]
[[ "$(TEST_CPU=i686 atm_trivy_platform)" == Linux-32bit ]]
[[ "$(TEST_CPU=ppc64le atm_trivy_platform)" == Linux-PPC64LE ]]
[[ "$(TEST_CPU=s390x atm_trivy_platform)" == Linux-s390x ]]
if output="$(TEST_CPU=riscv64 ATM_DRY_RUN=1 atm_trivy_install_binary 2>&1)"; then exit 1; fi
if output="$(TEST_OS=Darwin ATM_DRY_RUN=1 atm_trivy_install_binary 2>&1)"; then exit 1; fi
if output="$(ATM_DRY_RUN=1 atm_trivy_install --method=binary --version=../bad 2>&1)"; then exit 1; fi
if output="$(atm_trivy_install --method invalid 2>&1)"; then exit 1; fi
if output="$(atm_trivy_install --method 2>&1)"; then exit 1; fi
[[ ! -e "$ATM_TRIVY_INSTALL_ROOT" ]]
if output="$(TEST_RELEASE_FAIL=1 atm_trivy_install_binary 2>&1)"; then exit 1; fi
[[ ! -e "$ATM_TRIVY_INSTALL_ROOT" && "$output" == *'Could not resolve'* ]]
printf 'PASS platform mapping and strict version/method validation\n'

output="$(atm_trivy_install --method binary)"
[[ "$(atm_trivy_current_version)" == 0.76.1 ]]
[[ "$(atm_trivy_path_entries)" == "$ATM_TRIVY_INSTALL_ROOT/current/bin" ]]
[[ "$output" == *'Trivy binary installed/activated'* ]]
(
    source "$ATM_TRIVY_MANIFEST_FILE"
    [[ "$ATM_PLUGIN_VERSION" == 0.0.1 && "$ATM_INSTALL_ROOT" == "$ATM_TRIVY_INSTALL_ROOT" ]]
)
before="$(cksum "$log")"
output="$(atm_trivy_install_binary 0.76.1)"
[[ "$(cksum "$log")" == "$before" ]]
if output="$(TEST_BAD_ARCHIVE=1 atm_trivy_install_binary 0.75.0 2>&1)"; then exit 1; fi
[[ "$(atm_trivy_current_version)" == 0.76.1 && ! -e "$ATM_TRIVY_INSTALL_ROOT/0.75.0" ]]
output="$(atm_trivy_install --method=binary --version=v0.75.0)"
[[ "$(atm_trivy_current_version)" == 0.75.0 ]]
output="$(atm_trivy_use 0.76.1)"
if output="$(atm_trivy_remove 0.76.1 2>&1)"; then exit 1; fi
output="$(atm_trivy_remove 0.75.0)"
[[ ! -e "$ATM_TRIVY_INSTALL_ROOT/0.75.0" && "$(atm_trivy_current_version)" == 0.76.1 ]]
printf 'PASS verified portable install, existing-version reuse, PATH/manifest, checksum rejection, switching, and protected removal\n'

before="$(cksum "$log")"
output="$(atm_trivy_status)"
[[ "$output" == *'Binary 0.76.1'* && "$(cksum "$log")" == "$before" ]]
output="$(atm_trivy_install --method docker)"
[[ "$(atm_trivy_docker_version)" == 0.76.1 && "$(atm_trivy_current_version)" == 0.76.1 ]]
[[ "$output" == *'docker run --rm aquasec/trivy:0.76.1'* ]]
if grep -q '^docker run' "$log"; then exit 1; fi
if output="$(TEST_DOCKER_FAIL=info atm_trivy_install_docker 2>&1)"; then exit 1; fi
if output="$(TEST_DOCKER_FAIL=pull atm_trivy_install_docker 0.75.0 2>&1)"; then exit 1; fi
[[ "$(atm_trivy_docker_version)" == 0.76.1 ]]
if output="$(TEST_DOCKER_FAIL=inspect atm_trivy_install_docker 0.75.0 2>&1)"; then exit 1; fi
[[ "$(atm_trivy_docker_version)" == 0.76.1 ]]
output="$(atm_trivy_install_docker 0.76.1)"
before="$(cksum "$log")"
output="$(atm_trivy_remove_docker <<< n 2>&1)"
[[ "$(cksum "$log")" == "$before" && "$(atm_trivy_docker_version)" == 0.76.1 ]]
if output="$(TEST_DOCKER_FAIL=rm atm_trivy_uninstall <<< y 2>&1)"; then exit 1; fi
[[ -x "$ATM_TRIVY_INSTALL_ROOT/0.76.1/bin/trivy" && -f "$ATM_TRIVY_DOCKER_STATE" ]]
output="$(atm_trivy_remove_docker <<< y)"
[[ -z "$(atm_trivy_docker_version)" && -x "$ATM_TRIVY_INSTALL_ROOT/0.76.1/bin/trivy" ]]
printf 'PASS Docker pull/pinned state/usage, no automatic containers, daemon/pull/inspect/removal failures, and cancellation\n'

output="$(atm_trivy_install_binary 0.75.0)"
output="$(atm_trivy_use 0.76.1)"
output="$(atm_trivy_install_docker)"
before="$(cksum "$log")"
manifest_before="$(cksum "$ATM_TRIVY_MANIFEST_FILE")"
output="$(ATM_DRY_RUN=1 atm_trivy_install --method binary)"
output+="$(ATM_DRY_RUN=1 atm_trivy_install --method=docker --version=0.76.1)"
output+="$(ATM_DRY_RUN=1 atm_trivy_use 0.75.0)"
output+="$(ATM_DRY_RUN=1 atm_trivy_remove 0.75.0)"
output+="$(ATM_DRY_RUN=1 atm_trivy_remove_docker <<< y)"
output+="$(ATM_DRY_RUN=1 atm_trivy_uninstall <<< y)"
output+="$(ATM_DRY_RUN=1 atm_trivy_install <<< 2)"
[[ "$output" == *DRY-RUN* && "$(cksum "$log")" == "$before" ]]
[[ "$(cksum "$ATM_TRIVY_MANIFEST_FILE")" == "$manifest_before" && "$(atm_trivy_current_version)" == 0.76.1 ]]
output="$(ATM_DRY_RUN=1 atm_trivy_menu <<< $'2\nxb')"
[[ "$output" == *'2) Install / update Docker image'* && "$output" != *'[missing:'* ]]
printf 'PASS no-write/no-network/no-Docker dry-run for every lifecycle action and menu method selection\n'

mkdir -p "$ATM_TRIVY_INSTALL_ROOT/personal-data" "$ATM_TRIVY_INSTALL_ROOT/1.2.3/bin"
printf keep > "$ATM_TRIVY_INSTALL_ROOT/personal-data/report"
printf external > "$ATM_TRIVY_INSTALL_ROOT/1.2.3/bin/trivy"
output="$(atm_trivy_uninstall <<< y)"
[[ -z "$(atm_trivy_current_version)" && -z "$(atm_trivy_docker_version)" ]]
[[ ! -e "$ATM_TRIVY_INSTALL_ROOT/0.76.1" && ! -e "$ATM_TRIVY_INSTALL_ROOT/0.75.0" ]]
[[ ! -e "$ATM_TRIVY_MANIFEST_FILE" && -d "$ATM_TRIVY_CACHE_DIR" ]]
[[ "$(cat "$ATM_TRIVY_INSTALL_ROOT/personal-data/report")" == keep && -f "$ATM_TRIVY_INSTALL_ROOT/1.2.3/bin/trivy" ]]
if grep -q 'TRIVY_BINARY_EXECUTED\|^docker run\|--force' "$log"; then exit 1; fi
printf 'PASS confirmed uninstall preserves unowned files, download caches, reports, and unrelated installations\n'

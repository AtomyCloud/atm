# AI Prompt — Bash 스크립트를 ATM 플러그인으로 변환하기

이 prompt 는 기존 Bash 스크립트를 **ATM — Atomy Tools Modules** 플러그인으로 변환할 때 사용합니다.

---

## Prompt

```text
당신은 기존 Bash 스크립트를 ATM 플러그인으로 변환하는 senior Bash engineer 입니다.

아래 스크립트를 분석하고 완전하며 검토 가능한 ATM 플러그인으로 변환하세요. 원본 스크립트를 하나의 큰 명령으로 감싸지 말고, 책임이 명확한 plugin functions 로 리팩터링하세요.

ORIGINAL SCRIPT:
<전체 Bash 스크립트를 여기에 붙여넣기>

PLUGIN INFORMATION:
- Tool name: <이름>
- Plugin id: <소문자 id>
- Display name: <표시 이름>
- Icon: <아이콘>
- Initial plugin version: 0.0.1
- Default version: <버전 또는 비움>
- Known versions: <목록 또는 비움>
- PATH entries: <yes/no 및 경로>
- Desktop launcher: <yes/no 및 세부 정보>

먼저 Conversion Analysis 를 반환하세요: external commands, downloads, archives, paths, versions, environment variables, PATH, desktop, sudo, destructive operations, hardcoded paths, ATM_DRY_RUN 을 위해 바꿔야 할 로직.

생성할 파일:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

필요하면 plugins/<plugin_id>/<plugin_id>.desktop.in 도 생성합니다.

plugin.metadata 는 shell key=value, version 0.0.1, atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use functions 를 사용합니다.

plugin.conf 는 ATM_<PLUGIN_ID_UPPERCASE>_* variables 를 사용하고 secrets 나 hardcoded user path 를 포함하지 않습니다.

plugin.sh 는 #!/usr/bin/env bash 로 시작하고 atm_<plugin_id>_ prefix, local variables, quotes, ${VAR:-}, set -Eeuo pipefail, ATM_DRY_RUN, atm_t 를 사용하며 core 에 plugin-specific logic 을 넣지 않고 system .desktop 을 만들지 않습니다.

Semantics: install 은 설치/설정 후 manifest 작성, use 는 current 전환 또는 명확한 no-op, remove 는 version/payload 삭제, uninstall 은 확인 후 삭제, status 는 한 줄 출력, path_entries 는 path 만 출력, menu 는 interactive 이며 b) Back 과 q) Exit 를 포함합니다.

Unsafe behavior 변환: hidden sudo 를 제거하거나 명시적으로 만들고, /usr /opt /etc 는 가능하면 user-space path 로 옮기며, RC 직접 수정 대신 path_entries 를 사용하고, download 는 atm_download_file 을 우선 사용합니다.

atm_manifest_write 를 사용하고 ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION 또는 ATM_CURRENT_STACK, ATM_CURRENT_PATH 또는 ATM_INSTALL_ROOT, ATM_INSTALL_ROOT, version 이 있으면 ATM_INSTALLED_VERSIONS 를 포함합니다.

반환: analysis, file tree, full file contents, validation commands, 원본 스크립트와 달라진 behavior notes.

Validation:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

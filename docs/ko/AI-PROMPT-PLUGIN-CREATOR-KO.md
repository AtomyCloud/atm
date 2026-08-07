# AI 프롬프트 — ATM 플러그인 생성기

이 프롬프트는 AI에게 **ATM — Atomy Tools Modules**용 플러그인을 만들도록 요청할 때 사용합니다.

```text
당신은 Bash 시니어 엔지니어이며 ATM — Atomy Tools Modules용 플러그인을 만들어야 합니다.

배경:
ATM은 Linux에서 개발 도구를 설치하고 관리하는 모듈형 Bash 도구입니다. 각 플러그인은 plugins/<plugin_id>/에 위치하며 도구별 설치, 버전, 메뉴, 상태, 버전 전환, 제거, 전체 제거, manifest, PATH entries, 필요한 경우 desktop launcher, 언어 파일을 담당합니다.

도구:
<도구 설명>

Plugin ID:
<예: nodejs, rust, deno, bun>

표시 이름:
<예: Node.js>

아이콘:
<예: 🟩>

플러그인 초기 버전:
0.0.1

메뉴 버전:
<버전 목록>

기본 버전:
<버전>

다운로드 패턴:
<공식 URL 또는 패턴. 확실하지 않으면 검토 지점으로 명확히 표시하세요.>

다음 파일을 생성하세요:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

필수 규칙:

- set -Eeuo pipefail과 호환되는 Bash.
- 모든 공개 함수는 atm_<plugin_id>_ 접두사를 사용.
- 함수 안에서 local 사용.
- 변수는 따옴표로 감싸기.
- ATM_DRY_RUN 준수.
- sudo 사용 금지.
- 명확한 이유 없이 ATM 모델 밖에 쓰지 않기.
- 플러그인 전용 로직을 lib/에 넣지 않기.
- desktop launcher는 ~/.local/share/applications에만 설치.
- UX 문자열은 atm_t와 ATM_PLUGIN_<ID>_* 키 사용.

plugin.metadata에는 ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT 및 menu/status/install/path/desktop/remove/uninstall/use 함수 참조가 포함되어야 합니다.

구현 함수:

atm_<plugin_id>_archive_name
atm_<plugin_id>_download_url
atm_<plugin_id>_install_dir
atm_<plugin_id>_current_path
atm_<plugin_id>_cache_dir
atm_<plugin_id>_cache_file
atm_<plugin_id>_manifest_file
atm_<plugin_id>_normalize_version
atm_<plugin_id>_version_from_args
atm_<plugin_id>_status
atm_<plugin_id>_list_installed_versions
atm_<plugin_id>_current_version
atm_<plugin_id>_write_manifest
atm_<plugin_id>_install
atm_<plugin_id>_use
atm_<plugin_id>_remove
atm_<plugin_id>_uninstall
atm_<plugin_id>_menu
atm_<plugin_id>_path_entries

의미 규칙:

- install은 --version <version> 및 --version=<version>을 받습니다.
- install은 다운로드 전에 이미 설치된 버전을 감지합니다.
- install은 current를 갱신하고 manifest를 씁니다.
- use는 설치된 버전으로만 전환합니다.
- remove는 current 버전을 삭제할 수 없습니다.
- uninstall은 확인을 요청합니다.
- status는 짧은 한 줄을 출력하며 설치된 경우 "✅ "로 시작합니다.
- path_entries는 경로만 한 줄에 하나씩 출력합니다.
- menu는 중복 번호가 없어야 하며 b) Back과 q) Exit를 포함해야 합니다.

Manifest:

atm_manifest_write를 사용하고 다음을 포함하세요:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

마지막에 검증 명령을 제공하세요:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

각 파일의 전체 내용을 별도 블록으로 출력하세요.
```


# ATM 플러그인 개발자 매뉴얼

이 매뉴얼은 **ATM — Atomy Tools Modules** 용 플러그인을 만드는 방법을 설명합니다.

ATM 플러그인은 로컬에서 신뢰되는 Bash 모듈입니다. 설치, 버전, PATH, desktop launcher, manifest, 메뉴, 번역 등 도구별 로직을 관리합니다.

## 1. 플러그인 계약

각 플러그인은 다음 위치에 둡니다.

```text
plugins/<plugin_id>/
```

최소 파일:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. 이름 규칙

안정적인 `plugin_id` 를 사용하세요.

```text
example_tool
```

```text
- 소문자, 숫자, 밑줄만 사용합니다.
- 플러그인 ID 는 고유해야 합니다.
- 공개 함수는 atm_<plugin_id>_ 접두사를 사용합니다.
- 플러그인 전용 로직을 lib/ 에 넣지 마세요.
```

## 3. 초기 구조

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

ID, 이름, 아이콘, 버전 `0.0.1`, 순서, 설명, entry point, 그리고 menu/status/install/path/desktop/remove/uninstall/use 함수 참조를 포함합니다.

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

`ATM_PLUGIN_<ID>_*` 와 `atm_t` 를 사용하세요. 최종 UX 문구를 하드코딩하지 마세요.

## 7. 필수 함수

download, install_dir, current_path, cache, manifest, status, 버전 목록, 현재 버전, install, use, remove, uninstall, menu, path_entries 를 위한 `atm_<plugin_id>_` 함수를 구현합니다.

## 8. 의미

```text
install   버전을 설치하고 current 를 갱신하며 manifest 를 작성합니다.
use       current 를 설치된 버전으로 전환합니다.
remove    특정 버전을 삭제하지만 current 는 절대 삭제하지 않습니다.
uninstall 확인 후 플러그인이 관리하는 모든 항목을 삭제합니다.
status    짧은 상태 줄을 출력합니다.
menu      중복 번호가 없는 대화형 메뉴를 표시합니다.
path_entries 경로만 출력합니다.
```

항상 `ATM_DRY_RUN` 을 준수하세요.

## 9. Manifest

`ATM_PLUGIN_VERSION="0.0.1"` 과 현재 버전, 현재 경로, install root, 설치된 버전을 사용해 `atm_manifest_write` 를 호출합니다.

## 10. Desktop

Desktop launchers 는 다음 위치에만 생성합니다.

```text
~/.local/share/applications
```

sudo 를 사용하지 마세요.

## 11. 검증

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 12. 릴리스

```text
- 초기 플러그인 버전: 0.0.1.
- sudo 사용 금지.
- 최종 UX 문구 하드코딩 금지.
- 중복 ID 금지.
- dry-run 은 디스크를 변경하지 않습니다.
```


# AI Prompt — Bash スクリプトを ATM プラグインへ変換する

この prompt は、既存の Bash スクリプトを **ATM — Atomy Tools Modules** 用プラグインへ変換するために使います。

---

## Prompt

```text
あなたは既存の Bash スクリプトを ATM プラグインへ変換する senior Bash engineer です。

下のスクリプトを分析し、完全でレビュー可能な ATM プラグインへ変換してください。元スクリプトを巨大な 1 コマンドとして包むのではなく、責務の明確な plugin functions にリファクタリングしてください。

ORIGINAL SCRIPT:
<完全な Bash スクリプトをここに貼る>

PLUGIN INFORMATION:
- Tool name: <名前>
- Plugin id: <小文字 id>
- Display name: <表示名>
- Icon: <アイコン>
- Initial plugin version: 0.0.1
- Default version: <version または空>
- Known versions: <list または空>
- PATH entries: <yes/no と path>
- Desktop launcher: <yes/no と詳細>

最初に Conversion Analysis を返してください: 外部コマンド、download、archive、path、version、environment variables、PATH、desktop、sudo、破壊的操作、hardcoded path、ATM_DRY_RUN のための変更点。

作成する files:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

必要なら plugins/<plugin_id>/<plugin_id>.desktop.in も作成します。

plugin.metadata は shell key=value、version 0.0.1、atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use functions を使います。

plugin.conf は ATM_<PLUGIN_ID_UPPERCASE>_* variables を使い、secrets や hardcoded user path を含めません。

plugin.sh は #!/usr/bin/env bash で始め、atm_<plugin_id>_ prefix、local variables、quotes、${VAR:-}、set -Eeuo pipefail、ATM_DRY_RUN、atm_t、core に plugin-specific logic を入れないこと、system .desktop を作らないことを守ります。

Semantics: install は install/configure と manifest write、use は current switch または明確な no-op、remove は version/payload 削除、uninstall は確認付き削除、status は 1 行、path_entries は path のみ、menu は interactive で b) Back と q) Exit を含めます。

Unsafe behavior: hidden sudo を削除または明示化し、/usr /opt /etc は可能なら user-space path へ移し、RC 直接編集ではなく path_entries を使い、download は atm_download_file を優先します。

atm_manifest_write を使い、ATM_PLUGIN_NAME、ATM_PLUGIN_VERSION="0.0.1"、ATM_INSTALLED、ATM_CURRENT_VERSION または ATM_CURRENT_STACK、ATM_CURRENT_PATH または ATM_INSTALL_ROOT、ATM_INSTALL_ROOT、version がある場合 ATM_INSTALLED_VERSIONS を含めます。

返すもの: analysis、file tree、full file contents、validation commands、元スクリプトから変更した behavior notes。

Validation:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

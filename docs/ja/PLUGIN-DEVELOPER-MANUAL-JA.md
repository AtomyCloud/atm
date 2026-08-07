# ATM プラグイン開発者マニュアル

このマニュアルでは、**ATM — Atomy Tools Modules** 用プラグインの作成方法を説明します。

ATM プラグインは、ローカルで信頼される Bash モジュールです。インストール、バージョン、PATH、desktop launcher、マニフェスト、メニュー、翻訳など、ツール固有のロジックを管理します。

## 1. プラグイン契約

各プラグインは次の場所に配置します。

```text
plugins/<plugin_id>/
```

最小ファイル構成:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. 命名規則

安定した `plugin_id` を使用してください。

```text
example_tool
```

```text
- 小文字、数字、アンダースコアのみを使用します。
- プラグイン ID は一意である必要があります。
- 公開関数は atm_<plugin_id>_ プレフィックスを使用します。
- プラグイン固有のロジックを lib/ に入れないでください。
```

## 3. 初期構造

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

ID、名前、アイコン、バージョン `0.0.1`、順序、説明、entry point、menu/status/install/path/desktop/remove/uninstall/use 用の関数参照を含めます。

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

`ATM_PLUGIN_<ID>_*` と `atm_t` を使用します。最終的な UX テキストをハードコードしないでください。

## 7. 必須関数

download、install_dir、current_path、cache、manifest、status、バージョン一覧、現在のバージョン、install、use、remove、uninstall、menu、path_entries 用に `atm_<plugin_id>_` 関数を実装します。

## 8. セマンティクス

```text
install   バージョンをインストールし、current を更新し、マニフェストを書き込みます。
use       current をインストール済みバージョンに切り替えます。
remove    指定バージョンを削除します。current は削除しません。
uninstall 確認後、プラグインが管理するすべての内容を削除します。
status    短い状態行を出力します。
menu      重複番号のない対話型メニューを表示します。
path_entries パスだけを出力します。
```

常に `ATM_DRY_RUN` を尊重してください。

## 9. マニフェスト

`ATM_PLUGIN_VERSION="0.0.1"` と、現在のバージョン、現在のパス、install root、インストール済みバージョンを使って `atm_manifest_write` を呼び出します。

## 10. Desktop

Desktop launchers は次の場所だけに作成します。

```text
~/.local/share/applications
```

sudo は使用しないでください。

## 11. 検証

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 12. リリース

```text
- 初期プラグインバージョン: 0.0.1。
- sudo は使いません。
- 最終的な UX テキストをハードコードしません。
- ID を重複させません。
- dry-run はディスクを変更しません。
```


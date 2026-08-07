# AI プロンプト — ATM Plugin Creator

このプロンプトは **ATM — Atomy Tools Modules** 用のプラグインを AI に作成させるためのものです。

```text
あなたは Bash に詳しいシニアエンジニアです。ATM — Atomy Tools Modules のプラグインを作成してください。

背景:
ATM は Linux の開発ツールをインストール、管理するためのモジュール式 Bash ツールです。各プラグインは plugins/<plugin_id>/ に配置され、対象ツール固有の処理を担当します。担当範囲には、インストール、バージョン管理、メニュー、ステータス、バージョン切り替え、削除、アンインストール、manifest、PATH entries、必要な場合の desktop launcher、言語ファイルが含まれます。

ツール:
<ツールを説明>

Plugin ID:
<例: nodejs, rust, deno, bun>

表示名:
<例: Node.js>

アイコン:
<例: 🟩>

プラグイン初期バージョン:
0.0.1

メニューに表示するバージョン:
<バージョン一覧>

デフォルトバージョン:
<バージョン>

ダウンロード方式:
<公式 URL またはパターン。不明な場合はレビュー対象として明記する。>

作成するファイル:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

必須ルール:

- set -Eeuo pipefail と互換性のある Bash。
- すべての公開関数は atm_<plugin_id>_ prefix を使う。
- 関数内では local を使う。
- 変数は引用符で囲む。
- ATM_DRY_RUN を尊重する。
- sudo を使わない。
- 明確な理由なく ATM モデル外に書き込まない。
- プラグイン固有ロジックを lib/ に置かない。
- desktop launcher は ~/.local/share/applications のみ。
- UX 文字列は atm_t と ATM_PLUGIN_<ID>_* keys を使う。

plugin.metadata には ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT と menu/status/install/path/desktop/remove/uninstall/use の関数参照を含める。

実装する関数:

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

セマンティクス:

- install は --version <version> と --version=<version> を受け付ける。
- install はダウンロード前に既存インストールを検出する。
- install は current を更新し manifest を書く。
- use はインストール済みバージョンにのみ切り替える。
- remove は current バージョンを削除してはいけない。
- uninstall は確認を求める。
- status は短い 1 行を出力し、インストール済みなら "✅ " で始める。
- path_entries はパスだけを 1 行ずつ出力する。
- menu は重複番号を持たず、b) Back と q) Exit を含める。

Manifest:

atm_manifest_write を使い、以下を含める:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

最後に検証コマンドを出力:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

各ファイルの完全な内容を別々のブロックで出力してください。
```


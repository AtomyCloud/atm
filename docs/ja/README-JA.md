# ATM - Atomy Tools Modules

<div align="center">

#### _他の言語の README。_
<kbd>[<img title="English Britain" alt="English Britain" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/gb.svg" width="44">](../en-gb/README-EN-GB.md)</kbd>
<kbd>[<img title="English USA" alt="English USA" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/us.svg" width="44">](../en-us/README-EN-US.md)</kbd>
<kbd>[<img title="Português" alt="Português" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/pt.svg" width="44">](../pt/README-PT-PT.md)</kbd>
<kbd>[<img title="Português Brasileiro" alt="Português Brasileiro" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/br.svg" width="44">](../br/README-PT-BR.md)</kbd>
<kbd>[<img title="Español" alt="Español" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/es.svg" width="44">](../es/README-ES.md)</kbd>
<kbd>[<img title="Français" alt="Français" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/fr.svg" width="44">](../fr/README-FR.md)</kbd>
<kbd>[<img title="Italiano" alt="Italiano" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/it.svg" width="44">](../it/README-IT.md)</kbd>
<kbd>[<img title="Deutsch" alt="Deutsch" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/de.svg" width="44">](../de/README-DE.md)</kbd>
<kbd>[<img title="日本語" alt="日本語" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/jp.svg" width="44">](../ja/README-JA.md)</kbd>
<kbd>[<img title="Русский" alt="Русский" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/ru.svg" width="44">](../ru/README-RU.md)</kbd>
<kbd>[<img title="中文" alt="中文" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/cn.svg" width="44">](../zh-cn/README-ZH-CN.md)</kbd>
<kbd>[<img title="한국어" alt="한국어" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/kr.svg" width="44">](../ko/README-KO.md)</kbd>

</div>

ATM は Bash で書かれたモジュール式の Linux ツールマネージャーです。開発ツールをユーザー空間にインストールして管理し、複数バージョンを並べて保持し、`current` シンボリックリンクで有効バージョンを切り替え、シェルの PATH を設定し、GUI ツール用の desktop launcher を作成します。

ATM は portable-first のワークフロー向けに設計されています:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## 対応プラグイン

| プラグイン | ID | バージョン | 目的 |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | 対応 vendor またはカスタム URL から Java/JDK バージョンをインストールして切り替えます。 |
| Flutter SDK | `flutter` | `0.0.1` | Flutter SDK バージョンをインストール、切り替え、削除、アンインストールします。 |
| Go | `go` | `0.0.1` | Go バージョンをインストール、切り替え、削除、アンインストールし、Go workspace を管理します。 |
| VS Code | `vscode` | `0.0.1` | VS Code バージョンをインストールし、インストール済みバージョンを切り替え、CLI と desktop launcher を設定します。 |
| Android Studio | `android_studio` | `0.0.1` | Android Studio バージョンをインストールして切り替え、desktop launcher を作成します。 |
| Android SDK | `android_sdk` | `0.0.1` | Android SDK command-line tools、platforms、build-tools、CMake、NDK、emulator、platform-tools をインストールします。 |
| OS Packages | `os_packages` | `0.0.1` | 検出された Linux package manager を使って共通 OS パッケージをインストール/削除します。 |
| Docker | `docker` | `0.0.1` | 公式ソースから Docker Engine、Docker Compose、Docker Desktop をインストールします。 |
| AI Tools | `ai` | `0.0.1` | Install Hermes-Agent and Hermes-Desktop tools. |

## 機能

- モジュール式プラグインアーキテクチャ。
- `~/Apps/atm` 配下の portable install mode。
- `/usr/local/bin/atm` への任意の system command link。
- `~/Apps` 配下のバージョン別ツールインストール。
- `atm use <plugin> <version>` で現在のバージョンを切り替えます。
- ユーザー空間の desktop launcher。`.desktop` ファイルに `sudo` は不要です。
- VS Code 公式 desktop files: `code.desktop` と `code-url-handler.desktop`。
- `en-us`、`pt-br`、`pt-pt`、`es`、`it`、`fr`、`de`、`ru`、`ja`、`zh-cn`、`ko` の locales。
- 変更前に安全に検証するための dry-run mode。

## 要件

ATM は Ubuntu、Zorin、および類似の Linux デスクトップ環境を対象にしています。

必要な共通コマンド:

```bash
bash
curl
tar
unzip
sed
awk
find
sort
readlink
```

特定のワークフローで使う任意コマンド:

```bash
xdg-mime
update-desktop-database
sudo
```

desktop launcher には `sudo` を使いません。必要なのは任意の system-level command setup と、システムパッケージをインストールするプラグインだけです。

## クイックスタート

リポジトリを clone してメニューを実行します:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

portable mode を設定します:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

利用可能なプラグインを一覧表示します:

```bash
ATM_LANG=en-us atm plugins list
```

PATH、CLI リンク、desktop launcher を適用します:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## よく使う CLI

ツールのバージョンをインストール:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

インストール済みバージョンへ切り替え:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

インストール済みの 1 バージョンを削除:

```bash
atm remove go 1.25.9
```

1 つのプラグインが管理するものをすべてアンインストール:

```bash
atm uninstall go
```

dry-run を実行:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## メインメニュー

対話メニューを実行します:

```bash
ATM_LANG=en-us atm
```

メインメニューには環境情報と、現在の状態付きのプラグイン一覧が表示されます:

```text
==========================================
    🚀 ATM - Atomy Tools Modules v0.0.1
==========================================
Mode:     portable-source
Base dir: ~/Apps
Cache:    ~/.cache/atm
Plugins:  ~/Apps/atm/plugins
Language: en-us
------------------------------------------
1) ☕ Java / JDK                       <status>
2) 🦋 Flutter SDK                      <status>
3) 🐹 Go                               <status>
4) 💻 VS Code                          <status>
5) 🤖 Android Studio                   <status>
6) 📦 Android SDK                      <status>
7) 🧰 OS Packages                      <status>
8) 🐳 Docker                           <status>
9) 🧠 AI Tools                         <status>
------------------------------------------
10) ⚡ Install Stack / Full Setup
11) 🛠️  Configure PATH, CLI & Desktop
s) ⚙️  Setup ATM Command
p) 🔌 Plugins
d) 🩺 Doctor
u) ♻️  Self-update
q) ❌ Exit
------------------------------------------
Choose an option:
```

メインメニューの操作:

| オプション | 操作 |
|---|---|
| `1` to `9` | 選択したプラグインのサブメニューを開きます。 |
| `9` | stack installation が有効なプラグインに対して Full Setup を実行します。 |
| `10` | shell PATH、CLI links、desktop launchers を設定します。 |
| `s` | ATM command setup を開きます。 |
| `p` | 読み込まれたプラグインを一覧表示します。 |
| `d` | doctor checks を実行します。 |
| `u` | self-update status を確認します。 |
| `q` | 終了します。 |

## プラグインサブメニュー

各プラグインは独自のサブメニューを持ちます。メニューには検出されたバージョン/状態と、ツール固有のインストール・保守操作が表示されます。

### Java / JDK

```text
☕ Java / JDK Installer
Current: <status>
------------------------------------------
1) OpenJDK 26.0.1 (Default)
2) Amazon Corretto 26
3) Eclipse Temurin 26
4) Microsoft OpenJDK 25
5) GraalVM 25
6) Install from custom URL
7) List installed versions
8) Remove specific version
9) Uninstall Java completely
b) Back
q) Exit
```

このサブメニューでは、対応 JDK ディストリビューションのインストール、カスタム URL からの JDK tarball インストール、インストール済みバージョン一覧、1 バージョンの削除、ATM 管理下の Java/JDK 全削除ができます。

### Flutter SDK

```text
🦋 Flutter SDK Installer
Current: <status>
------------------------------------------
1) Flutter 3.41.8 (Latest Stable)
2) Flutter 3.41.5
3) Flutter 3.41.0
4) Flutter 3.38.0
5) Flutter 3.35.0
6) Choose specific version
7) List installed versions
8) Remove specific version
9) Uninstall Flutter completely
b) Back
q) Exit
```

このサブメニューでは、事前定義された Flutter バージョンのインストール、特定バージョンの選択、インストール済み一覧、1 バージョンの削除、Flutter の完全アンインストールができます。

### Go

```text
🐹 Go Installer
Current: <status>
------------------------------------------
1) Go 1.26.2 (Latest Stable)
2) Go 1.26.1
3) Go 1.26.0
4) Go 1.25.9
5) Go 1.25.5
6) Choose specific version
7) List installed versions
8) Remove specific version
9) Uninstall Go completely
b) Back
q) Exit
```

このサブメニューでは、Go バージョンのインストール、CLI による現在の Go の切り替え、インストール済み一覧、1 バージョンの削除、ATM 管理下の Go ファイル全削除ができます。

### VS Code

```text
💻 VS Code Installer
Current: <status>
------------------------------------------
1) VS Code 1.118.1 (Latest Stable)
2) VS Code 1.118.0
3) VS Code 1.117.0
4) VS Code 1.116.0
5) VS Code 1.115.0
6) VS Code 1.114.0
7) VS Code 1.113.0
8) Choose specific version
9) Use installed version
10) List installed versions
11) Remove specific version
12) Uninstall VS Code completely
b) Back
q) Exit
```

このサブメニューでは、VS Code バージョンのインストール、既にインストール済みのバージョンへの切り替え、一覧表示、1 バージョンの削除、ATM 管理下の VS Code 全削除ができます。

バージョン切り替え時、ATM は次を再生成します:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

また次を登録します:

```text
x-scheme-handler/vscode -> code-url-handler.desktop
```

### Android Studio

```text
🤖 Android Studio Installer
Current: <status>
Install root: ~/Apps/AndroidStudio
------------------------------------------
1) Android Studio Panda 4 | 2025.3.4.6
2) Android Studio Quail 1 Canary 2 | 2026.1.1.2 (URL required)
3) Android Studio Quail 1 Canary 1 | 2026.1.1.1 (URL required)
4) Android Studio Panda 4 RC 1 | 2025.3.4.5 (URL required)
5) Android Studio Panda 3 Patch 1 | 2025.3.3.7 (URL required)
6) Install from custom URL
7) List installed versions
8) Remove specific version
9) Uninstall Android Studio completely
b) Back
q) Exit
```

このサブメニューでは、Android Studio のインストール、カスタム URL からのインストール、インストール済み一覧、1 バージョンの削除、ATM 管理下の Android Studio 全削除ができます。

### Android SDK

```text
📦 Android SDK Installer
Current: <status>
SDK root: ~/Apps/AndroidStudio/SDK
------------------------------------------
1) Latest Stable SDK Stack
2) SDK Stack: api37
3) SDK Stack: api36
4) SDK Stack: api35
5) SDK Stack: api35_legacy
6) Choose specific SDK versions
7) List installed SDK packages
8) Remove SDK package
9) Uninstall Android SDK completely
b) Back
q) Exit
```

このサブメニューでは、Android SDK command-line tools と package stack のインストール、カスタム SDK バージョン選択、インストール済みパッケージ一覧、パッケージ削除、ATM 管理下の SDK アンインストールができます。

ATM の status は、codename/preview よりも安定した数値 Android API を優先します。

### OS Packages

```text
🧰 OS Packages
Current: <status>
OS: <detected distribution>
Package manager: <detected package manager>
------------------------------------------
1) Install OS packages
2) Uninstall OS packages
3) Show package manager commands
b) Back
q) Exit
```

このサブメニューでは、検出された Linux ディストリビューション向けに `plugins/os_packages/packages.txt` のパッケージセットをインストールまたは削除できます。実際のパッケージ操作では、ATM が root で実行されていない場合 `sudo` を要求します。`--dry-run` は sudo を要求せず、安全に package manager コマンドを表示します。

### Docker

```text
🐳 Docker Installer
Current: <status>
------------------------------------------
1) Install Docker Engine
2) Install Docker Compose
3) Install Docker Desktop
4) Install ALL
b) Back
q) Exit
```

このフェーズでは `Install Docker Engine`、`Install Docker Compose`、`Install Docker Desktop` が実装済みです。Docker Engine は提供された Ansible フローに従い、公式 Docker install script を実行し、対象ユーザーを `docker` グループに追加し、Docker service を enable/start します。Docker Compose は GitHub から最新の公式 standalone binary を `/usr/local/bin/docker-compose` にダウンロードし、実行可能にします。Docker Desktop は Ubuntu 公式 DEB フローに従い、`docker-desktop-amd64.deb` をダウンロードし、`apt-get update` を実行して `apt` でローカルパッケージをインストールします。Install ALL は後続 patch 用の placeholder です。

## セットアップメニュー

セットアップメニューはメインメニューの `s` から開けます:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

portable mode が推奨デフォルトです。system mode は command link のみを作成し、プロジェクトファイルは `~/Apps/atm` に残ります。

### AI Tools

```text
🧠 AI Tools Installer
Current: <status>
------------------------------------------
1) Hermes
b) Back
q) Exit
```

The Hermes submenu groups Hermes tools:

```text
Hermes
------------------------------------------
1) Install Hermes-Agent
2) Install Hermes-Desktop (System DEB)
3) Install Hermes-Desktop (Portable AppImage)
b) Back
q) Exit
```

`Install Hermes-Agent` runs the official installer: `curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash`. `Install Hermes-Desktop (System DEB)` downloads the latest GitHub release DEB and installs it with `apt install`. `Install Hermes-Desktop (Portable AppImage)` downloads the latest AppImage into `~/Apps/hermes/hermes-desktop` and updates `current.AppImage`. `--dry-run` prints commands without downloading or installing.



## PATH、CLI、Desktop

`atm path apply` はユーザー shell files とプラグインの desktop integration を更新します。

デフォルトでは ATM は `/etc/profile.d/atm.sh` に書き込みません。

この任意の system profile 書き込みを明示的に有効化するには:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

desktop launcher は次にインストールされます:

```text
~/.local/share/applications
```

## Locales

言語を選ぶには `ATM_LANG` を使います:

```bash
ATM_LANG=pt-br atm
ATM_LANG=pt-pt atm
ATM_LANG=es atm
ATM_LANG=it atm
ATM_LANG=fr atm
ATM_LANG=de atm
ATM_LANG=ru atm
ATM_LANG=ja atm
ATM_LANG=zh-cn atm
ATM_LANG=ko atm
```

対応 locales:

```text
en-us
pt-br
pt-pt
es
it
fr
de
ru
ja
zh-cn
ko
```

## 公開ドキュメント

公開ドキュメントは次にあります:

```text
docs/
```

利用可能な公開ドキュメント:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## プロジェクト構成

```text
bin/atm                         メイン実行ファイル
lib/                            Core Bash ライブラリ
lang/                           Core locale ファイル
plugins/<plugin_id>/            プラグインディレクトリ
plugins/<plugin_id>/plugin.*    プラグインの metadata、config、implementation
plugins/<plugin_id>/lang/       プラグイン locale ファイル
docs/                           公開ドキュメント
```

## 検証

推奨チェック:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## ライセンス

[LICENSE](../../LICENSE) を参照してください。

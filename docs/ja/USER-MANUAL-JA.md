# USER-MANUAL — ATM ユーザーマニュアル

## 1. ATM の役割

ATM は、ユーザー領域に開発ツールをインストールして管理します。

主な対応ツール:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

既定の場所:

```text
~/Apps
```

## 2. ATM を起動する

プロジェクトフォルダーから:

```bash
cd ~/Apps/atm
ATM_LANG=ja bin/atm
```

設定済みの場合:

```bash
atm
```

## 3. atm コマンドを設定する

ポータブルモード:

```bash
cd ~/Apps/atm
ATM_LANG=ja bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

システムコマンドモード:

```bash
cd ~/Apps/atm
ATM_LANG=ja bin/atm setup system
atm --version
```

このモードでは sudo を求められる場合があります。

## 4. メインメニュー

```text
1) Go
2) Java / JDK
3) VS Code
4) Android Studio
5) Android SDK
6) Flutter SDK
8) Install Stack / Full Setup
9) Configure PATH, CLI & Desktop
s) Setup ATM Command
p) Plugins
d) Doctor
u) Self-update
q) Exit
```

プラグインの順序は `plugin.metadata` により変わることがあります。

## 5. PATH、CLI、desktop

```bash
ATM_LANG=ja atm path apply
```

PATH、`code`、desktop launchers、`vscode://` handler を設定します。

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. ツールをインストールする

```bash
atm install go --version 1.26.2
atm use go 1.25.9
atm remove go 1.25.9
atm uninstall go
```

```bash
atm install vscode --version 1.118.0
atm use vscode 1.117.0
atm path apply
```

```bash
atm install java --vendor openjdk --version 26.0.1
```

Android Studio は `atm` を起動し、Android Studio を選択してバージョンを選びます。

Android SDK:

```text
~/Apps/AndroidStudio/SDK
```

Flutter:

```bash
atm install flutter --version 3.41.8
atm use flutter 3.41.8
```

## 7. プラグイン

```bash
atm plugins list
```

## 8. Dry run

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. トラブルシューティング

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=ja atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=ja atm path apply
```

プラグイン ID の重複確認:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. ATM をアンインストールする

```bash
atm uninstall atm
```

手動削除:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

`~/.bashrc`、`~/.zshrc`、`~/.profile` から ATM の PATH ブロックも削除してください。


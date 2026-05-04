# USER-MANUAL — ATM 用户手册

## 1. ATM 的作用

ATM 在用户空间中安装和管理开发工具。

主要工具:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

默认位置:

```text
~/Apps
```

## 2. 启动 ATM

从项目目录运行:

```bash
cd ~/Apps/atm
ATM_LANG=zh-cn bin/atm
```

如果已经配置:

```bash
atm
```

## 3. 配置 atm 命令

便携模式:

```bash
cd ~/Apps/atm
ATM_LANG=zh-cn bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

系统命令模式:

```bash
cd ~/Apps/atm
ATM_LANG=zh-cn bin/atm setup system
atm --version
```

此模式可能会请求 sudo。

## 4. 主菜单

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

插件顺序可能根据 `plugin.metadata` 改变。

## 5. PATH、CLI 和 desktop

```bash
ATM_LANG=zh-cn atm path apply
```

配置 PATH、`code`、desktop launchers 和 `vscode://` handler。

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. 安装工具

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

Android Studio: 运行 `atm`，选择 Android Studio，然后选择版本。

Android SDK:

```text
~/Apps/AndroidStudio/SDK
```

Flutter:

```bash
atm install flutter --version 3.41.8
atm use flutter 3.41.8
```

## 7. 插件

```bash
atm plugins list
```

## 8. Dry run

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. 故障排查

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=zh-cn atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=zh-cn atm path apply
```

检查重复插件 ID:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. 卸载 ATM

```bash
atm uninstall atm
```

手动清理:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

同时从 `~/.bashrc`、`~/.zshrc` 或 `~/.profile` 删除 ATM PATH 块。


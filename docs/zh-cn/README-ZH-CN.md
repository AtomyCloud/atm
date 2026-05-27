# ATM - Atomy Tools Modules

<div align="center">

#### _其他语言的 README。_
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

ATM 是一个用 Bash 编写的模块化 Linux 工具管理器。它在用户空间安装和管理开发工具，让多个版本并存，通过 `current` 符号链接切换当前版本，配置 shell PATH，并为 GUI 工具创建桌面启动器。

ATM 面向 portable-first 工作流设计：

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## 支持的插件

| 插件 | ID | 版本 | 用途 |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | 从受支持的供应商或自定义 URL 安装并切换 Java/JDK 版本。 |
| Flutter SDK | `flutter` | `0.0.1` | 安装、切换、移除和卸载 Flutter SDK 版本。 |
| Go | `go` | `0.0.1` | 安装、切换、移除和卸载 Go 版本，并管理 Go workspace。 |
| VS Code | `vscode` | `0.0.1` | 安装 VS Code 版本、切换已安装版本、配置 CLI 和桌面启动器。 |
| Android Studio | `android_studio` | `0.0.1` | 安装和切换 Android Studio 版本，并创建桌面启动器。 |
| Android SDK | `android_sdk` | `0.0.1` | 安装 Android SDK command-line tools、platforms、build-tools、CMake、NDK、emulator 和 platform-tools。 |
| OS Packages | `os_packages` | `0.0.1` | 使用检测到的 Linux 包管理器安装和移除常用操作系统软件包。 |
| Docker | `docker` | `0.0.1` | 从官方来源安装 Docker Engine、Docker Compose 和 Docker Desktop。 |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## 功能

- 模块化插件架构。
- `~/Apps/atm` 下的 portable install mode。
- `/usr/local/bin/atm` 下的可选系统命令链接。
- `~/Apps` 下的版本化工具安装。
- 使用 `atm use <plugin> <version>` 切换当前版本。
- 用户空间桌面启动器，`.desktop` 文件不需要 `sudo`。
- VS Code 官方 desktop files：`code.desktop` 和 `code-url-handler.desktop`。
- 支持 `en-us`、`pt-br`、`pt-pt`、`es`、`it`、`fr`、`de`、`ru`、`ja`、`zh-cn` 和 `ko` locales。
- dry-run 模式，用于在更改前进行更安全的验证。

## 要求

ATM 面向 Ubuntu、Zorin 以及类似的 Linux 桌面发行版。

必需的通用命令：

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

特定流程使用的可选命令：

```bash
xdg-mime
update-desktop-database
sudo
```

桌面启动器不会使用 `sudo`。只有可选的系统级命令设置和安装系统软件包的插件才需要它。

## 快速开始

克隆仓库并运行菜单：

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

配置 portable mode：

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

列出可用插件：

```bash
ATM_LANG=en-us atm plugins list
```

应用 PATH、CLI 链接和桌面启动器：

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## 常用 CLI

安装工具版本：

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

切换到已安装版本：

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

移除一个已安装版本：

```bash
atm remove go 1.25.9
```

卸载某个插件管理的全部内容：

```bash
atm uninstall go
```

运行 dry-run：

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## 主菜单

使用以下命令运行交互式菜单：

```bash
ATM_LANG=en-us atm
```

主菜单显示环境信息以及带当前状态的插件列表：

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
f) ⚡ Install Stack / Full Setup
c) 🛠️  Configure PATH, CLI & Desktop
s) ⚙️  Setup ATM Command
p) 🔌 Plugins
l) 🌐 Change Language
d) 🩺 Doctor
u) ♻️  Self-update
q) ❌ Exit
------------------------------------------
Choose an option:
```

主菜单操作：

| 选项 | 操作 |
|---|---|
| `1` to `9` | 打开所选插件子菜单。 |
| `f` | 对启用 stack installation 的插件运行 Full Setup。 |
| `c` | 配置 shell PATH、CLI links 和 desktop launchers。 |
| `s` | 打开 ATM command setup。 |
| `p` | 列出已加载插件。 |
| `d` | 运行 doctor checks。 |
| `u` | 检查 self-update status。 |
| `q` | 退出。 |

## 插件子菜单

每个插件都有自己的子菜单。菜单会显示检测到的版本/状态，然后显示该工具的安装和维护操作。

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

使用此子菜单安装受支持的 JDK 发行版、从自定义 URL 安装 JDK tarball、列出已安装版本、移除一个版本，或卸载 ATM 管理的所有 Java/JDK。

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

使用此子菜单安装预定义 Flutter 版本、选择指定版本、列出已安装版本、移除一个版本，或完全卸载 Flutter。

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

使用此子菜单安装 Go 版本、通过 CLI 切换当前 Go、列出已安装版本、移除一个版本，或卸载 ATM 管理的所有 Go 文件。

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

使用此子菜单安装 VS Code 版本、切换到已安装版本、列出已安装版本、移除一个版本，或卸载 ATM 管理的所有 VS Code 版本。

切换版本时，ATM 会重新生成：

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

它还会注册：

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

使用此子菜单安装 Android Studio、从自定义 URL 安装、列出已安装版本、移除一个版本，或卸载 ATM 管理的所有 Android Studio。

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

使用此子菜单安装 Android SDK command-line tools 和包栈、选择自定义 SDK 版本、列出已安装包、移除包，或卸载 ATM 管理的 SDK。

当稳定数字 Android API 和 codename/preview 同时存在时，ATM 状态优先显示稳定数字 API。

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

使用此子菜单为检测到的 Linux 发行版安装或移除 `plugins/os_packages/packages.txt` 中的软件包集合。执行真实软件包操作时，如果 ATM 不是以 root 运行，插件会请求 `sudo`。`--dry-run` 会安全打印包管理器命令，不请求 sudo。

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

在此阶段，`Install Docker Engine`、`Install Docker Compose` 和 `Install Docker Desktop` 已实现。Docker Engine 遵循提供的 Ansible 流程：运行官方 Docker 安装脚本、把目标用户加入 `docker` 组，并启用/启动 Docker 服务。Docker Compose 从 GitHub 下载最新官方 standalone binary 到 `/usr/local/bin/docker-compose` 并设为可执行。Docker Desktop 遵循官方 Ubuntu DEB 流程：下载 `docker-desktop-amd64.deb`，运行 `apt-get update`，并用 `apt` 安装本地包。Install ALL 仍是后续 patch 的 placeholder。

## 设置菜单

设置菜单可在主菜单中通过 `s` 打开：

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

portable mode 是推荐默认值。system mode 只创建命令链接；项目文件仍位于 `~/Apps/atm`。

### AI Tools

```text
🧠 AI Tools Installer
Current: <status>
------------------------------------------
1) Hermes
2) Ollama
3) PicoClaw
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

The Ollama submenu installs Ollama:

```text
Ollama
------------------------------------------
1) Install Ollama
b) Back
q) Exit
```

`Install Hermes-Agent` runs the official installer: `curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash`. `Install Hermes-Desktop (System DEB)` downloads the latest GitHub release DEB and installs it with `apt install`. `Install Hermes-Desktop (Portable AppImage)` downloads the latest AppImage into `~/Apps/hermes/hermes-desktop` and updates `current.AppImage`. `Install Ollama` runs `curl -fsSL https://ollama.com/install.sh | sh` and prints run/pull usage examples after installation. `PicoClaw` downloads the official latest `.tar.gz` release for the detected OS/architecture and extracts it into `~/Apps/ai/picoclaw`. `--dry-run` prints commands without downloading or installing.



## PATH、CLI 和 Desktop

`atm path apply` 会更新用户 shell 文件和插件桌面集成。

默认情况下，ATM 不写入 `/etc/profile.d/atm.sh`。

要显式启用这个可选的系统 profile 写入：

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

桌面启动器安装在：

```text
~/.local/share/applications
```

## Locales

Use the CLI to persistently change the ATM language:

```bash
atm lang list
atm lang current
atm lang set pt-br
atm lang select
```

Use `ATM_LANG` for a one-command temporary language override:

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

支持的 locales：

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

## 公开文档

公开文档位于：

```text
docs/
```

可用公开文档：

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## 项目结构

```text
bin/atm                         主可执行文件
lib/                            核心 Bash 库
lang/                           核心 locale 文件
plugins/<plugin_id>/            插件目录
plugins/<plugin_id>/plugin.*    插件 metadata、config 和 implementation
plugins/<plugin_id>/lang/       插件 locale 文件
docs/                           公开文档
```

## 验证

推荐检查：

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 许可证

见 [LICENSE](../../LICENSE)。

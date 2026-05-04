# ATM - Atomy Tools Modules

<div align="center">

#### _Read this in [other languages](translations/Translations.md)._
<kbd>[<img title="English Britain" alt="English" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/gb.svg" width="44">](README.md)</kbd>
<kbd>[<img title="English USA" alt="English" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/us.svg" width="44">](README.md)</kbd>
<kbd>[<img title="Português" alt="Português" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/pt.svg" width="44">](docs/pt/README-PT.md)</kbd>
<kbd>[<img title="Português Brasileiro" alt="Português Brasileiro   " src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/br.svg" width="44">](docs/br/README-PT-BR.md)</kbd>
<kbd>[<img title="Español" alt="Español" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/es.svg" width="44">](docs/es/README-ES.md)</kbd>
<kbd>[<img title="Français" alt="Français" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/fr.svg" width="44">](docs/fr/README-FR.md)</kbd>
<kbd>[<img title="Italiano" alt="Italiano" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/it.svg" width="44">](docs/it/README-IT.md)</kbd>
<kbd>[<img title="Deutsch" alt="Deutsch" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/de.svg" width="44">](docs/de/README-DE.md)</kbd>
<kbd>[<img title="日本語" alt="日本語" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/jp.svg" width="44">](docs/jp/README-JP.md)</kbd>
<kbd>[<img title="Русский" alt="Русский" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/ru.svg" width="44">](docs/ru/README-RU.md)</kbd>
<kbd>[<img title="中文" alt="中文" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/cn.svg" width="44">](docs/cn/README-CN.md)</kbd>
<kbd>[<img title="한국어" alt="한국어" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/kr.svg" width="44">](docs/kr/README-KR.md)</kbd>

</div>

<div align="center">
  
![GitHub commit activity](https://img.shields.io/github/commit-activity/t/atomycloud/atomycloud?style=for-the-badge&logo=github&logoSize=auto&labelColor=%238000ff&color=%23bf00ff)
![GitHub followers](https://img.shields.io/github/followers/atomycloud?style=for-the-badge&labelColor=%2300bfff&color=%23bf00ff)
![GitHub forks](https://img.shields.io/github/forks/atomycloud/atomycloud?style=for-the-badge&labelColor=%2300bfff&color=%23bf00ff)
![GitHub Repo stars](https://img.shields.io/github/stars/atomycloud/atomycloud?style=for-the-badge&labelColor=%23bf00ff)
![GitHub watchers](https://img.shields.io/github/watchers/atomycloud/atomycloud?style=for-the-badge&labelColor=%23bf00ff)
  
</div>

ATM is a modular Linux tool manager written in Bash. It installs and manages developer tools in user space, keeps multiple versions side by side, switches the active version with `current` symlinks, configures shell PATH entries, and creates desktop launchers for GUI tools.

ATM is designed for a portable-first workflow:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Supported Plugins

| Plugin | ID | Version | Purpose |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Install and switch Java/JDK versions from supported vendors or custom URLs. |
| Flutter SDK | `flutter` | `0.0.1` | Install, switch, remove, and uninstall Flutter SDK versions. |
| Go | `go` | `0.0.1` | Install, switch, remove, and uninstall Go versions and manage a Go workspace. |
| VS Code | `vscode` | `0.0.1` | Install VS Code versions, switch installed versions, configure CLI and desktop launchers. |
| Android Studio | `android_studio` | `0.0.1` | Install and switch Android Studio versions and create desktop launchers. |
| Android SDK | `android_sdk` | `0.0.1` | Install Android SDK command-line tools, platforms, build-tools, CMake, NDK, emulator, and platform-tools. |

## Features

- Modular plugin architecture.
- Portable install mode under `~/Apps/atm`.
- Optional system command link at `/usr/local/bin/atm`.
- Versioned tool installs under `~/Apps`.
- `atm use <plugin> <version>` to switch the current version.
- User-space desktop launchers, no `sudo` for `.desktop` files.
- VS Code official desktop files: `code.desktop` and `code-url-handler.desktop`.
- Locales for `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn`, and `ko`.
- Dry-run mode for safer validation before changes.

## Requirements

ATM targets Linux desktop systems such as Ubuntu, Zorin, and similar distributions.

Required common commands:

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

Optional commands used by specific workflows:

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` is not used for desktop launchers. It is only needed for optional system-level command setup.

## Quick Start

Clone the repository and run the menu:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Configure portable mode:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

List available plugins:

```bash
ATM_LANG=en-us atm plugins list
```

Apply PATH, CLI links, and desktop launchers:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Common CLI Usage

Install a tool version:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Switch to an installed version:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Remove one installed version:

```bash
atm remove go 1.25.9
```

Uninstall everything managed by one plugin:

```bash
atm uninstall go
```

Run a dry-run:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Main Menu

Run the interactive menu with:

```bash
ATM_LANG=en-us atm
```

The main menu shows environment information and the plugin list with current status:

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
------------------------------------------
8) ⚡ Install Stack / Full Setup
9) 🛠️  Configure PATH, CLI & Desktop
s) ⚙️  Setup ATM Command
p) 🔌 Plugins
d) 🩺 Doctor
u) ♻️  Self-update
q) ❌ Exit
------------------------------------------
Choose an option:
```

Main menu actions:

| Option | Action |
|---|---|
| `1` to `6` | Open the selected plugin submenu. |
| `8` | Run Full Setup for plugins enabled for stack installation. |
| `9` | Configure shell PATH, CLI links, and desktop launchers. |
| `s` | Open ATM command setup. |
| `p` | List loaded plugins. |
| `d` | Run doctor checks. |
| `u` | Check self-update status. |
| `q` | Exit. |

## Plugin Submenus

Each plugin owns its submenu. Menus show the current detected version/status, then tool-specific install and maintenance actions.

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

Use this submenu to install supported JDK distributions, install a custom JDK tarball URL, list installed versions, remove one version, or uninstall all Java/JDK versions managed by ATM.

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

Use this submenu to install one of the predefined Flutter versions, choose a specific version, list installed versions, remove one version, or uninstall Flutter completely.

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

Use this submenu to install Go versions, switch current Go via CLI, list installed versions, remove one version, or uninstall all Go files managed by ATM.

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

Use this submenu to install VS Code versions, switch to an already installed version, list installed versions, remove one version, or uninstall all VS Code versions managed by ATM.

When switching versions, ATM regenerates:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

It also registers:

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

Use this submenu to install Android Studio, install from a custom URL, list installed versions, remove one version, or uninstall all Android Studio versions managed by ATM.

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

Use this submenu to install Android SDK command-line tools and package stacks, choose custom SDK versions, list installed packages, remove a package, or uninstall the SDK managed by ATM.

ATM status prefers stable numeric Android APIs over codenames/previews when both exist.

## Setup Menu

The setup menu is available from `s` in the Main Menu:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

Portable mode is the recommended default. System mode only creates a command link; project files still live under `~/Apps/atm`.

## PATH, CLI, And Desktop

`atm path apply` updates user shell files and plugin desktop integration.

By default, ATM does not write `/etc/profile.d/atm.sh`.

To explicitly enable that optional system profile write:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

Desktop launchers are installed in:

```text
~/.local/share/applications
```

## Locales

Use `ATM_LANG` to select a language:

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

Supported locales:

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

## Public Documentation

Public documentation lives in:

```text
docs/
```

Available public docs:

```text
docs/USER-MANUAL.md
docs/USER-MANUAL-PT-BR.md
docs/USER-MANUAL-PT-PT.md
docs/USER-MANUAL-ES.md
docs/USER-MANUAL-IT.md
docs/USER-MANUAL-FR.md
docs/USER-MANUAL-DE.md
docs/USER-MANUAL-RU.md
docs/USER-MANUAL-JA.md
docs/USER-MANUAL-ZH-CN.md
docs/USER-MANUAL-KO.md
docs/PLUGIN-DEVELOPER-MANUAL.md
docs/PLUGIN-DEVELOPER-MANUAL-PT-BR.md
docs/PLUGIN-DEVELOPER-MANUAL-PT-PT.md
docs/PLUGIN-DEVELOPER-MANUAL-ES.md
docs/PLUGIN-DEVELOPER-MANUAL-IT.md
docs/PLUGIN-DEVELOPER-MANUAL-FR.md
docs/PLUGIN-DEVELOPER-MANUAL-DE.md
docs/PLUGIN-DEVELOPER-MANUAL-RU.md
docs/PLUGIN-DEVELOPER-MANUAL-JA.md
docs/PLUGIN-DEVELOPER-MANUAL-ZH-CN.md
docs/PLUGIN-DEVELOPER-MANUAL-KO.md
docs/AI-PROMPT-PLUGIN-CREATOR-PT-BR.md
docs/AI-PROMPT-PLUGIN-CREATOR-PT-PT.md
docs/AI-PROMPT-PLUGIN-CREATOR-ES.md
docs/AI-PROMPT-PLUGIN-CREATOR-IT.md
docs/AI-PROMPT-PLUGIN-CREATOR-FR.md
docs/AI-PROMPT-PLUGIN-CREATOR-DE.md
docs/AI-PROMPT-PLUGIN-CREATOR-RU.md
docs/AI-PROMPT-PLUGIN-CREATOR-JA.md
docs/AI-PROMPT-PLUGIN-CREATOR-ZH-CN.md
docs/AI-PROMPT-PLUGIN-CREATOR-KO.md
```

## Project Layout

```text
bin/atm                         Main executable
lib/                            Core Bash libraries
lang/                           Core locale files
plugins/<plugin_id>/            Plugin directories
plugins/<plugin_id>/plugin.*    Plugin metadata, config, and implementation
plugins/<plugin_id>/lang/       Plugin locale files
docs/                           Public documentation
```

## Validation

Recommended checks:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## License

See [LICENSE](LICENSE).

# ATM - Atomy Tools Modules

<div align="center">

#### _README in other languages._
<kbd>[<img title="English Britain" alt="English Britain" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/gb.svg" width="44">](docs/en-gb/README-EN-GB.md)</kbd>
<kbd>[<img title="English USA" alt="English USA" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/us.svg" width="44">](docs/en-us/README-EN-US.md)</kbd>
<kbd>[<img title="Português" alt="Português" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/pt.svg" width="44">](docs/pt/README-PT-PT.md)</kbd>
<kbd>[<img title="Português Brasileiro" alt="Português Brasileiro" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/br.svg" width="44">](docs/br/README-PT-BR.md)</kbd>
<kbd>[<img title="Español" alt="Español" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/es.svg" width="44">](docs/es/README-ES.md)</kbd>
<kbd>[<img title="Français" alt="Français" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/fr.svg" width="44">](docs/fr/README-FR.md)</kbd>
<kbd>[<img title="Italiano" alt="Italiano" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/it.svg" width="44">](docs/it/README-IT.md)</kbd>
<kbd>[<img title="Deutsch" alt="Deutsch" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/de.svg" width="44">](docs/de/README-DE.md)</kbd>
<kbd>[<img title="日本語" alt="日本語" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/jp.svg" width="44">](docs/ja/README-JA.md)</kbd>
<kbd>[<img title="Русский" alt="Русский" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/ru.svg" width="44">](docs/ru/README-RU.md)</kbd>
<kbd>[<img title="中文" alt="中文" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/cn.svg" width="44">](docs/zh-cn/README-ZH-CN.md)</kbd>
<kbd>[<img title="한국어" alt="한국어" src="https://cdn.jsdelivr.net/gh/hjnilsson/country-flags@master/svg/kr.svg" width="44">](docs/ko/README-KO.md)</kbd>

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
| OS Packages | `os_packages` | `0.0.1` | Install and remove common operating system packages using the detected Linux package manager. |
| [Docker](plugins/docker/README.md) | `docker` | `0.0.1` | Install Docker Engine, Compose, and Desktop; export and restore image archives. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |
| [Node.js](plugins/node/README.md) | `node` | `0.0.1` | Manage portable Node.js releases or install the system APT package; identify ATM, user-local and system versions. |
| [Trivy](plugins/trivy/README.md) | `trivy` | `0.0.1` | Choose a verified portable binary or the official Docker image, with dynamic stable-release lookup. |
| [ATM - Backup & Restore](plugins/backup_restore/README.md) | `backup_restore` | `0.0.1` | Selected/all-user local backups, mounted-device selection, verification, and staged restore with recovery copies. |
| [spotatui](plugins/spotatui/README.md) | `spotatui` | `0.0.1` | Portable music player with yt-dlp/EJS, FFmpeg and JavaScript runtime checks before installation. |

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
python3
```

`python3` is required for the official plugin catalog (Python standard library only).

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

Search and download new plugins from the fixed official source,
[AtomyCloud/atm, main/plugins](https://github.com/AtomyCloud/atm/tree/main/plugins):

```bash
ATM_LANG=en-us atm plugins search brave
ATM_LANG=en-us atm plugins download brave
ATM_LANG=en-us atm plugins menu
```

The same workflow is in **Main Menu > p) Plugins > 2) Search and download official plugins**.
Search is case-insensitive and matches plugin IDs, display names, descriptions,
and file paths. An empty search lists the catalog; it does not search file bodies.
Results show which plugins are already local. Existing plugins are never replaced.

Downloads require confirmation because plugins contain Bash code that ATM sources
on startup. Files are pinned to one `main` commit, verified against Git blob hashes,
and checked with `bash -n` before publication under ATM's local `plugins` directory.
Search/download does not execute the downloaded code or install its application.
Restart ATM to load the new plugin, then use its regular menu. No sudo is used for
catalog operations; the local plugin directory must be writable by your account.

The source is fixed; custom repository URLs and third-party plugins are not
supported by this downloader. Public access needs no token. If GitHub limits
requests or the repository requires authentication, `GITHUB_TOKEN` can be supplied
through the environment; ATM does not save it. Metadata searches use read-only
network requests even with `--dry-run`; download dry-run prints the plan without
network requests, files, or confirmation prompts.

Change plugin ordering under **p) Plugins > 3) Sort plugins**:

```bash
atm plugins sort az
atm plugins sort za
atm plugins sort default
atm plugins sort
```

A-Z and Z-A sort by display name, ignoring ASCII letter case and using the
plugin ID to break ties. The preference is saved as `ATM_PLUGIN_SORT_ORDER`
in ATM's configuration and applies to the main/install menus, local lists,
and official catalog results. `default` restores metadata order; existing
installations keep that original order until a preference is selected.
The metadata files and their order values are never rewritten. Full Setup,
PATH precedence, and desktop processing retain metadata order independently.
Use `ATM_PLUGIN_SORT_ORDER=az atm plugins list` for a temporary override.
`--dry-run plugins sort za` previews the setting without saving or applying it.

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
7) 🧰 OS Packages                      <status>
8) 🐳 Docker                           <status>
9) 🧠 AI Tools                         <status>
10) 🟢 Node.js                          <status>
11) 🦁 Brave Browser                    <status>
12) 🐚 Zsh + Oh My Zsh                   <status>
13) 🔍 Trivy                            <status>
14) 💾 ATM - Backup & Restore            <status>
15) 🎵 spotatui                         <status>
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

Main menu actions:

| Option | Action |
|---|---|
| `1` to `15` | Open the selected plugin submenu; numbering follows enabled plugins and the configured sort order. |
| `f` | Run Full Setup for plugins enabled for stack installation. |
| `c` | Configure shell PATH, CLI links, and desktop launchers. |
| `s` | Open ATM command setup. |
| `p` | Open plugin management: list local plugins or search/download official plugins. |
| `l` | Change the configured ATM language. |
| `d` | Run doctor checks. |
| `u` | Check self-update status. |
| `q` | Exit. |

### Self-update Indicator

ATM automatically compares its installed Git commit with the latest commit on
`ATM_REPO_URL` / `ATM_SELF_UPDATE_BRANCH` (default: the official repository's
`main` branch). A remote tip not contained locally adds **Update** beside
`u) Self-update`, in blue on color-capable terminals. A local checkout that
already contains the remote commit does not get an update label.

The check uses `git ls-remote`, never fetches/pulls or changes checkout files,
and caches the result in memory for 60 seconds. Requests have a five-second
timeout with a one-second forced-stop grace period. Git and GNU `timeout`
(normally supplied by coreutils on Linux) are required. Offline/unavailable
checks and non-Git copies show no label; they do not claim to be up to date.
Shallow/partial clones may report a different remote tip without being able to
prove ancestry. Diverged history is flagged and explained under `u`.

Selecting `u`, or running `atm self-update --check`, forces a fresh check and
shows both commit IDs. Updating is explicit:

```bash
atm self-update --git
```

This uses the same configured repository/branch and fast-forward only. The
checkout must already be on that branch; detached HEAD/other branches are not
switched automatically. No reset, force update, or local-file cleanup is done.
Restart ATM after updating to load the new code. Tarball updates remain pending.
Configure the source/branch in `~/.config/atm/atm.conf`.
`--dry-run` skips the network check and writes nothing. `NO_COLOR=1`, a `dumb`
terminal, and redirected output use a plain-text label instead of ANSI color.

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

Use this submenu to install or remove the package set from `plugins/os_packages/packages.txt` for the detected Linux distribution. For real package operations, this plugin asks for `sudo` when ATM is not already running as root. `--dry-run` prints the package manager commands safely without requesting sudo.

### Docker

```text
🐳 Docker Installer
Current: <status>
------------------------------------------
1) Install Docker Engine
2) Install Docker Compose
3) Install Docker Desktop
4) Install ALL
5) Uninstall Docker Desktop completely
6) Set up Docker Desktop sign-in
7) Export / Restore images
b) Back
q) Exit
```

Docker Engine uses the official installer. Compose installs the latest official standalone binary into `/usr/local/bin/docker-compose`. Desktop offers the five latest Linux DEB releases or a custom version, complete removal, and sign-in prerequisites; browser authentication remains interactive. Install/update checks stop running local Docker services before replacing packages. Install ALL remains a placeholder.

Option **7** manages images in the active Docker daemon without stopping it:

```text
1) Export a selected image to .tar
2) Export ALL images to one .tar
3) Restore images from a .tar archive
4) Restore ALL .tar archives in a folder
b) Back
q) Exit
```

Archives default to `~/Apps/Backups/docker`; custom absolute paths are supported. Exports preserve existing files, and imports require confirmation because tags can be replaced. Archives contain images, not containers or volumes. Only restore trusted archives. See the [Docker plugin guide](plugins/docker/README.md) for CLI examples and limitations.

### Node.js

```text
🟢 Node.js Installer
Current: <status>
------------------------------------------
1) Node.js v26.x
2) Node.js v25.x
3) Node.js v24.x (LTS)
4) List installed versions
5) Change active version
6) Remove installed version
7) Uninstall Node.js completely
8) Install Node.js (ATM / system package)
b) Back
q) Exit
```

Each major-version submenu fetches the latest three available versions from the official Node.js distribution index at `https://nodejs.org/dist/index.json`. Installations use official `node-v<version>-<platform>.tar.xz` archives, extract into `~/Apps/node/<version>`, and update `~/Apps/node/current`.

`Change active version` lists installed versions. Choose a number or enter an
installed version (optionally prefixed with `v`); `b` or Enter cancels. The same
selector is used for removal and the major-version submenu shortcuts. Switching
updates `current` and the manifest without downloading another Node.js version.
Portable activation also refreshes ATM's user-shell PATH blocks so `current/bin`
precedes user-local Node installations, without replacing their executables.
For an already-open terminal, run the PATH refresh/cache-reset commands printed
by ATM; new terminals load the saved selection automatically.

Option 4 also reports installed origins/paths and the command selected by PATH.
Option 8 offers ATM portable installation (default) or the system package via APT
on Debian/Ubuntu/Zorin. The latter installs `nodejs` and `npm` from configured
repositories after confirmation, requesting sudo when necessary. Its version is
chosen by APT, not the portable v24/v25/v26 menu. User-local binaries and ATM's
active link are not overwritten. See the [Node plugin guide](plugins/node/README.md).

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

### spotatui

```text
spotatui Installer
https://spotatui.com/
<status>
1) Install / Update latest stable
2) Install a specific version
3) Check playback dependencies
4) Change active version
5) Installed versions / locations
6) Remove an inactive version
7) Uninstall ATM-managed spotatui
8) Show usage
b) Back
q) Exit
```

Shows installed versions from ATM and `~/.local/bin/spotatui`. Installation offers
`~/Apps/spotatui` (default) or the user-local entry, with versioned support files
under `~/.local/share/atm/spotatui`. Replacing an existing local executable requires
confirmation and preserves a sibling backup. CLI: `atm install spotatui --location atm|local`.
Downloads use official checksum-verified Linux binaries.
Before any download, checks `curl`, Python, FFmpeg/ffprobe, modern `yt-dlp` with
EJS, and Deno >=2.3 or Node >=22. Missing dependencies block installation and
show correction commands; no automatic sudo/package installation occurs.
The managed launcher enables the chosen runtime without editing global settings.
Existing spotatui installations and profiles are preserved. See the
[plugin guide](plugins/spotatui/README.md) for setup and limitations.

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
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
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

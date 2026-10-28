# ATM - Atomy Tools Modules

> **Native Go CORE v0.0.2 (Linux amd64 milestone complete):** an opt-in Bubble Tea interface and
> platform-specific build tooling are available, along with a native plugin
> executor supporting progress and cancellation. Node.js, Go, Java/JDK, Flutter,
> Android Studio, Android SDK, VS Code, Trivy, spotatui, REA, OS Packages, Brave and Zsh have native Linux
> backends; remaining plugins use an explicit compatibility backend while being
> migrated individually. Native Windows application installation remains
> pending. Compiled CORE updates require signed packages and explicit consent. The original `bin/atm` is
> unchanged. See the [migration guide](docs/en-us/MIGRATION-GO-0.0.2.md).
> macOS Terminal packages are also built for Intel and Apple Silicon. Native
> CORE installation services are implemented; remaining application plugin ports are in progress.
> Official catalog search and plugin code downloads now run natively in Go on
> Linux, macOS and Windows (`atm plugins search|download` and the TUI catalog).
> Downloads require confirmation and never replace existing plugins. Component
> checks, guarded plugin code updates/recovery, portable setup, ATM user PATH
> integration and Full Setup orchestration now have native Go routes. The release
> signing key is approved; authenticated discovery, signed package verification,
> staged full-package replacement and independent recovery services are implemented.
> Signed CORE enrollment/update/recovery routes are enabled after maintainer approval;
> source checkouts and modified owned files are never automatically replaced.
> The plugin integration contract is native; application backends and real-platform
> acceptance remain pending. See [native administration](docs/en-us/NATIVE-CORE-ADMINISTRATION.md).
> Linux amd64 signed upgrade/recovery passed automated tests; the maintainer confirmed
> the final binary test passed on 2026-10-27. The [isolated acceptance harness](docs/en-us/LINUX-CORE-ACCEPTANCE.md)
> remains available for regression checks. Release publication/deployment is separate.
> Windows, macOS and Linux ARM64 runtime acceptance remains **Pending**.

## Legacy v0.0.1 (Linux / Bash)

To use ATM before the Go migration, check out commit
[`05673e6`](https://github.com/AtomyCloud/atm/commit/05673e6d9c370ca0b7972011dd0be1f435c8754a),
the last commit before migration to `v0.0.2`. This version requires Git and the
Linux/Bash dependencies listed below, but does not require Go.

For a fresh installation (`~/Apps/atm` must not already contain an installation):

```bash
mkdir -p "$HOME/Apps" &&
git clone https://github.com/AtomyCloud/atm.git "$HOME/Apps/atm" &&
git -C "$HOME/Apps/atm" checkout --detach 05673e6d9c370ca0b7972011dd0be1f435c8754a &&
ATM_LANG=en-us "$HOME/Apps/atm/bin/atm"
```

**New versions:** [Download ATM releases](https://github.com/AtomyCloud/atm/releases).

The legacy command pins a historical snapshot in detached HEAD mode, not a
`v0.0.1` tag. Do not pull `main` or apply CORE/plugin code updates if you want
to keep this exact snapshot. Preserve any existing installation before switching.

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

ATM is a modular, portable-first tool manager. The v0.0.2 CORE and Linux Node.js,
Go toolchain, Java/JDK, Flutter, Android Studio, Android SDK, VS Code, Trivy,
spotatui, REA, OS Packages, Brave and Zsh backends are written in Go.
The v0.0.1 legacy entrypoint and other application
plugins retain their explicit Linux Bash compatibility backends.
Application plugins install and manage developer tools in user space, keep multiple
versions side by side, switch active versions, configure PATH and create desktop
launchers. The detailed application menus and `bin/atm` examples below describe
the retained Linux legacy workflow; native CORE commands are documented above.

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
| [Java / JDK](plugins/java/README.md) | `java` | `0.0.1` | Install and switch Java/JDK versions from supported vendors or checksummed custom URLs. |
| [Flutter SDK](plugins/flutter/README.md) | `flutter` | `0.0.1` | Latest-five/custom official releases, channel-aware updates, version switching and guarded removal. |
| [Go](plugins/go/README.md) | `go` | `0.0.1` | Install, switch, remove, and uninstall Go versions while preserving the Go workspace. |
| [VS Code](plugins/vscode/README.md) | `vscode` | `0.0.1` | Verified latest-seven/custom releases, portable data migration, explicit extensions and user integration. |
| [Android Studio](plugins/android_studio/README.md) | `android_studio` | `0.0.1` | Verified pinned/custom IDE archives, version switching and owned user desktop integration. |
| [Android SDK](plugins/android_sdk/README.md) | `android_sdk` | `0.0.1` | Native package management, dependency resolution, explicit license review and ownership-preserving updates/removal. |
| [OS Packages](plugins/os_packages/README.md) | `os_packages` | `0.0.1` | Explicitly reviewed native system-package operations with detected Linux managers and guarded attempt journals. |
| [Docker](plugins/docker/README.md) | `docker` | `0.0.1` | Install Docker Engine, Compose, and Desktop; export and restore image archives. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |
| [Node.js](plugins/node/README.md) | `node` | `0.0.1` | Manage portable Node.js releases or install the system APT package; identify ATM, user-local and system versions. |
| [Brave Browser](plugins/brave/README.md) | `brave` | `0.0.1` | Install/remove the system browser, back up complete profiles or all profile bookmarks, and restore profile archives. |
| [Zsh + Oh My Zsh](plugins/zsh/README.md) | `zsh` | `0.0.1` | Manage the system shell and framework, private configuration backups, guarded restore and explicit default-shell selection. |
| [Trivy](plugins/trivy/README.md) | `trivy` | `0.0.1` | Choose a verified portable binary or the official Docker image, with dynamic stable-release lookup. |
| [ATM - Backup & Restore](plugins/backup_restore/README.md) | `backup_restore` | `0.0.1` | Selected/all-user local backups, mounted-device selection, verification, and staged restore with recovery copies. |
| [spotatui](plugins/spotatui/README.md) | `spotatui` | `0.0.1` | Native portable lifecycle, ATM/local destinations, explicit dependency diagnostics and native launchers. |
| [REA: Reverse Engineer Anything](plugins/rea/README.md) | `rea` | `0.0.1` | Native ATM/custom lifecycle, offline manual-install detection, explicit dependency diagnostics and guarded PATH integration. |

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

`python3` is required for the legacy Bash plugin catalog (Python standard library
only). Native Go catalog search/download does not require Python or Bash.

`sudo` is not used for desktop launchers. It is only needed for optional system-level command setup.

## Quick Start

After cloning the legacy snapshot above, run the menu:

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
16) 🔬 REA: Reverse Engineer Anything   <status>
------------------------------------------
f) ⚡ Install Stack / Full Setup
c) 🛠️  Configure PATH, CLI & Desktop
s) ⚙️  Setup ATM Command
p) 🔌 Plugins
l) 🌐 Change Language
d) 🩺 Doctor
u) ♻️  Updates
q) ❌ Exit
------------------------------------------
Choose an option:
```

Main menu actions:

| Option | Action |
|---|---|
| Plugin numbers | Open the selected plugin submenu; numbering follows enabled plugins and the configured sort order. |
| `f` | Run Full Setup for plugins enabled for stack installation. |
| `c` | Configure shell PATH, CLI links, and desktop launchers. |
| `s` | Open ATM command setup. |
| `p` | List, search/download, sort or update official plugins. |
| `l` | Change the configured ATM language. |
| `d` | Run doctor checks. |
| `u` | Check/update CORE and individual/all installed plugins independently. |
| `q` | Exit. |

### CORE And Plugin Updates

`u) Updates` detects CORE and plugin updates separately by official file hashes,
even while plugin versions remain `0.0.1`. Changed plugins show a blue `Update`
badge; plugin-only commits do not flag unchanged CORE files. Checks use a pinned
official `AtomyCloud/atm/main` snapshot and a five-minute cache. Offline or
unverified installations are reported explicitly, not assumed current.

```bash
atm updates check
atm updates core
atm updates plugin brave
atm updates all
atm updates changes
atm updates backups
atm updates restore <backup-id>
```

Updates require confirmation, stage/validate downloaded code, retain recovery
backups, block local edits and preserve application settings/data. The `all` action updates
installed plugins only (including disabled ones); it does not download new
plugins or update CORE implicitly. Restart ATM after successful changes.

Selective updates refuse Git checkouts. `atm self-update --git` remains an explicit
whole-repository fast-forward action, not a CORE-only update. Non-Git runtimes
use verified installation receipts; older copies can verify a known official
baseline. `atm self-update --check` aliases the independent component check.
See the [update and recovery guide](docs/en-us/COMPONENT-UPDATES-EN-US.md) for
compatibility, local-edit handling, interrupted CORE recovery and limitations.

## Plugin Submenus

Each plugin owns its submenu. Menus show the current detected version/status, then tool-specific install and maintenance actions.

### Java / JDK

Compiled ATM uses a native Go Linux backend for OpenJDK, Corretto, Temurin,
Microsoft OpenJDK, GraalVM and custom HTTPS archives with an explicit SHA-256.
It preserves unowned installations, manages JAVA_HOME/PATH, and refuses silent
major-version upgrades or replacement of a different artifact at an existing
version. See the [Java plugin guide](plugins/java/README.md) for supported selectors,
custom download inputs and ownership rules. The menu below is **legacy `bin/atm`**:

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

Compiled ATM uses the native Go backend. It discovers the latest five compatible
stable releases and exact custom versions from official metadata, verifies SHA-256,
extracts XZ natively and retains the active channel during updates. SDK selection
tolerates regenerable caches; removal refuses modified files. PATH integration is
explicit. See the [Flutter guide](plugins/flutter/README.md) for commands, ownership
rules and architecture availability. The menu below belongs to legacy `bin/atm`.

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

Compiled ATM uses a native Go backend on Linux amd64/arm64. It offers the latest
five compatible stable official releases, exact versions, updates, installed-version
selection and owned-version removal. The workspace and unowned installations are
preserved. GOROOT, GOPATH and PATH integration uses a separate managed profile block.
See the [Go plugin guide](plugins/go/README.md) for native commands and safeguards.
The menu below describes the retained **legacy `bin/atm`** workflow:

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

The native Linux backend keeps versions in `~/Apps/vscode/code_<version>` and
selects one through `current`. Open `atm plugin vscode` for the native menu;
the release submenu fetches the latest seven stable versions, with custom version
entry. Installations verify Microsoft's SHA-256 and build identity. Root menu and
status are offline and never launch the editor.

PATH, the user CLI link, desktop entries and extension installation are explicit
actions. Portable `data` migration requires both editors closed, copies the source
and retains the old destination in `preserved-data`. Removal preserves personal
data and refuses changed or unowned application files. See the
[VS Code guide](plugins/vscode/README.md) for native commands and limitations.

The retained Bash menu below is the legacy workflow:

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

In that legacy workflow, switching versions regenerates:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

It also registers:

```text
x-scheme-handler/vscode -> code-url-handler.desktop
```

### Android Studio

Compiled ATM uses a native Go backend on Linux amd64. It retains the five pinned
selectors below, with official download URLs and SHA-256 hashes for each, and
requires an explicit checksum for custom archives. These selectors are not a
dynamic latest-release list. Status inspects IDE metadata without launching Java
or the IDE. PATH and owned desktop integration are explicit; modified installs,
user settings and projects are preserved. See the
[Android Studio guide](plugins/android_studio/README.md) for native commands and
limits. The menu below describes the unchanged legacy `bin/atm` workflow.

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

The native Linux amd64 backend resolves official Google package metadata and
manages command-line tools, platforms, build-tools, CMake, NDK, emulator and
platform-tools directly in Go. It does not execute Java or `sdkmanager` during
installation. Java detection is read-only; running SDK tools still needs a compatible
JDK. Root menu/status are offline.

Open `atm plugin android_sdk`, select a stack and review its plan and licenses.
**View full output** exposes the complete retained review text in the TUI.
Installation requires explicit acceptance tied to the exact plan and license
contents; changes require another review. The native plugin is excluded from
automatic Full Setup because it cannot accept those terms implicitly.

The SDK root remains `~/Apps/AndroidStudio/SDK`. Existing unmanaged SDKs are
detected and preserved, not adopted. Native updates/removal protect changed or
unknown files and preserve licenses, AVD/user data and retained package history.
See the [Android SDK guide](plugins/android_sdk/README.md) for commands, checksum
trust, compatibility and partial-operation limits.

The retained Bash menu below describes the legacy workflow:

```text
📦 Android SDK Installer
Current: <status>
SDK root: ~/Apps/AndroidStudio/SDK
------------------------------------------
1) Latest Stable SDK Stack
2) SDK Stack: api36
3) SDK Stack: api35
4) SDK Stack: api35_legacy
5) SDK Stack: api34
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

The compiled Linux ATM uses a native Go backend for APT, DNF, YUM, Pacman and APK.
Open `atm plugin os_packages` to review the package list and commands before
confirming installation or removal. `packages.txt` remains editable data. These
are system packages, not portable apps; removal can affect tools installed before ATM.

Configured repositories are preserved. Ubuntu/Zorin optionally use a temporary
official source list for reviewed `jammy`/`noble` bases, without editing `/etc/apt`.
Native sudo is noninteractive: authenticate separately with `sudo -v` if needed.
Cancellation cannot guarantee privileged processes have stopped; incomplete
attempts require inspection, not an automatic retry. The plugin is excluded from
Full Setup. Real system-package and ARM64 runtime acceptance remain pending.
See the [OS Packages guide](plugins/os_packages/README.md) for consent flags,
package-name restrictions, privilege boundaries and recovery instructions.

Legacy Bash submenu (`bin/atm`):

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

The compiled ATM uses a native Go Node.js backend on Linux amd64/arm64. Its
structured menus offer the latest three official releases for each of majors
24, 25 and 26, an exact custom version, installation location, installed versions,
activation, removal and PATH integration. Downloads use official `.tar.gz`
archives with SHA-256 verification, without invoking Bash, curl, tar or Python.
Only unchanged native-owned versions are eligible for deletion; legacy versions,
external installations and user-added files are preserved.

Use a complete Linux ATM package, or build the plugin in a source checkout:

```bash
bash scripts/build_manager.sh --node-local --target linux-amd64
go build -o /tmp/atm ./cmd/atm
/tmp/atm --home "$PWD" plugin node
```

Native APT installation requires explicit system-package consent and previously
authorized sudo; it does not read a password through the JSONL protocol. See the
[Node plugin guide](plugins/node/README.md) for commands, ownership rules and tests.
Other Node platforms remain pending. The menu below describes **legacy `bin/atm`**:

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

### Brave Browser

The native Linux backend manages the stable system browser using APT or DNF.
It also creates full-profile or all-profile bookmark archives in
`~/Apps/backups/brave` by default. Status does not launch the browser.

```bash
atm plugin brave
atm --yes plugin brave status
atm --yes plugin brave menu backups
atm --yes plugin brave menu backup-profile
atm --yes plugin brave menu backup-bookmarks
```

Close Brave before backup or restore. Restore accepts a reviewed archive name
and SHA-256, stages the replacement and retains the previous profile as a
recovery copy. Bookmark-only archives are exports, not full-profile restore
inputs. Archives contain sensitive personal data and are not encrypted.

Installation/removal require separate system-operation acknowledgement and
sudo authentication outside ATM. Existing repository files are preserved;
unsupported or untrusted repository settings cause a refusal, not an automatic
rewrite. No portable Brave binary or automatic package rollback is provided.
See the [Brave guide](plugins/brave/README.md) for commands, trust boundaries,
archive limits and platform acceptance status. The legacy Bash entrypoint is
retained separately.

### Zsh + Oh My Zsh

Open `atm plugin zsh` for framework/package installation, updates, removal,
backup/restore and default-shell selection. Zsh remains an APT-managed system
package; Oh My Zsh is user configuration, not a portable shell executable.

```bash
atm status zsh
atm plugin zsh menu backup
atm --yes plugin zsh menu backups
atm plugin zsh menu shells
```

Backups default to `~/Apps/Backups` and contain private, unencrypted shell history
and configuration. Native snapshots use a versioned `.json.gz` format; legacy
`.tar.gz` backups remain preserved but require the legacy restore workflow.
Close other Zsh sessions before changing or restoring files.
Manual installations and modified framework files are not silently adopted or
overwritten. Changing the default shell is a separate confirmed action:
**You must log out and log back in for the new default shell to take effect.**

See the [Zsh guide](plugins/zsh/README.md) for native commands, trust and ownership
rules, recovery behavior and differences from the retained Bash implementation.

### AI Tools

AI migration is in progress. The normal menu still uses the existing legacy
backend. A separate native preview covers portable PicoClaw, Hermes Desktop
AppImage and Codex CLI, plus explicitly authorized Hermes Desktop system DEB
installation on Linux amd64. It is not selected automatically or included as a
production native backend. The DEB route changes the actual system and executes
package maintainer scripts through APT; an isolated preview HOME is not a system
sandbox. Custom APT preferences are refused in this preview. Hermes Agent,
Ollama and Codex Desktop remain Pending. See the [AI plugin guide](plugins/ai/README.md) for preview
scope, isolated testing and ownership boundaries.

The native preview also reports external executable candidates from known legacy
ATM locations, the user's `.local/bin` and bounded absolute PATH entries.
Discovery only inspects filesystem metadata: it does not execute programs, read
their configuration or establish their version. Found candidates are unverified,
not adopted or made eligible for native removal. Missing candidates do not prove
that an application is absent from the system. Native PATH registration and the
remaining installer routes are still pending.

```text
🧠 AI Tools Installer
Current: <status>
------------------------------------------
1) Hermes
2) Ollama
3) PicoClaw
4) Codex
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

The native Linux backend opens with ATM and user-local installation choices,
installed versions, playback dependencies and usage. Each destination offers
latest/custom installation, update, version selection, removal and explicit PATH
integration. It uses Go for release metadata, SHA-256 verification, archives and
launchers; no Bash/Python installer helper is run.

```bash
atm plugin spotatui
atm status spotatui
atm plugin spotatui menu dependencies
atm install spotatui --location atm --trust-dependencies
atm update spotatui --location local --trust-dependencies
atm use spotatui 0.46.0 --location atm --trust-dependencies
atm plugin spotatui integrate --location atm
```

Install/update/use require explicit consent to execute external dependency checks.
The initial dependency menu only locates tools; its confirmation action runs the
checks. No dependency is installed automatically, no media URL is supplied and
local EJS detection is not a playback guarantee. Normal playback preserves the
user's configuration; its runtime shim does not edit global yt-dlp settings.

Native payloads are isolated beneath `~/Apps/spotatui/native-v1` or
`~/.local/share/atm/spotatui/native-v1`. Native local installation refuses a foreign
`~/.local/bin/spotatui` instead of replacing it. Legacy installations and private
settings are preserved; status never executes an external player to read its
version. See the [plugin guide](plugins/spotatui/README.md) for ownership and limits.
The maintainer has tested and accepted the Linux version. Other-platform acceptance
remains pending; separate audio-provider coverage was not reported.

**Legacy Bash menu:**

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

### REA: Reverse Engineer Anything

#### Native Go Backend (Linux)

The native backend manages the official `rea-agents` npm package under
`~/Apps/rea/native-v1`, or `native-v1` beneath a dedicated custom base. It remembers
that destination for updates/removal and detects manual installations even when
`~/Apps/rea/bin/rea` is absent from PATH. Detection does not execute REA or grant
ownership of an external installation.

```bash
atm plugin rea
atm status rea
atm plugin rea menu dependencies
atm install rea --trust-dependencies
atm install rea --path /absolute/dedicated/rea --version 6.8.0 --trust-dependencies
atm update rea --trust-dependencies
atm plugin rea integrate
atm uninstall rea
```

Installation requires compatible Node/npm and Linux `ps`. Dependency execution
requires explicit trust; missing prerequisites are diagnosed with separate
installation guidance. There is no automatic system-package or Node repair in
this native milestone. npm lifecycle scripts are disabled, which can leave
optional native providers unavailable. Agent/MCP setup, skills, analysis engines
and private captures remain outside ATM installation/removal.

See the [REA plugin guide](plugins/rea/README.md) for supported runtimes, ownership,
PATH integration and validation status.

#### Legacy Bash Menu

```text
1) Install REA (choose destination)
2) Update REA
3) Remove ATM-managed REA
4) Check / install missing dependencies
5) Installation details / usage
b) Back
q) Exit
```

Choose `~/Apps/rea` or a dedicated custom directory. The destination is remembered
for updates/removal. Required Node.js/npm, Python, coreutils and Linux procps
checks run before installation; missing dependencies require confirmation.
REA is installed from the official `rea-agents` npm package. Agent/MCP setup and
optional analysis engines are separate; existing settings/data are preserved.
CLI: `atm install rea`, `atm install rea --update`, `atm uninstall rea`.
See the [REA plugin guide](plugins/rea/README.md) for runtime requirements,
dependency repair, PATH activation and scoped removal.

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

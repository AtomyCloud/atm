# ATM - Atomy Tools Modules

<div align="center">

#### _다른 언어의 README._
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

ATM은 Bash로 작성된 모듈식 Linux 도구 관리자입니다. 개발 도구를 사용자 공간에 설치하고 관리하며, 여러 버전을 나란히 유지하고, `current` symlink로 활성 버전을 전환하고, shell PATH 항목을 구성하며, GUI 도구용 desktop launcher를 생성합니다.

ATM은 portable-first 워크플로를 위해 설계되었습니다:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## 지원 플러그인

| 플러그인 | ID | 버전 | 목적 |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | 지원 vendor 또는 사용자 지정 URL에서 Java/JDK 버전을 설치하고 전환합니다. |
| Flutter SDK | `flutter` | `0.0.1` | Flutter SDK 버전을 설치, 전환, 제거, 삭제합니다. |
| Go | `go` | `0.0.1` | Go 버전을 설치, 전환, 제거, 삭제하고 Go workspace를 관리합니다. |
| VS Code | `vscode` | `0.0.1` | VS Code 버전을 설치하고, 설치된 버전을 전환하며, CLI와 desktop launcher를 구성합니다. |
| Android Studio | `android_studio` | `0.0.1` | Android Studio 버전을 설치하고 전환하며 desktop launcher를 생성합니다. |
| Android SDK | `android_sdk` | `0.0.1` | Android SDK command-line tools, platforms, build-tools, CMake, NDK, emulator, platform-tools를 설치합니다. |
| OS Packages | `os_packages` | `0.0.1` | 감지된 Linux package manager를 사용해 일반 OS 패키지를 설치하고 제거합니다. |
| Docker | `docker` | `0.0.1` | 공식 소스에서 Docker Engine, Docker Compose, Docker Desktop을 설치합니다. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## 기능

- 모듈식 플러그인 아키텍처.
- `~/Apps/atm` 아래의 portable install mode.
- `/usr/local/bin/atm`의 선택적 system command link.
- `~/Apps` 아래의 버전별 도구 설치.
- `atm use <plugin> <version>`으로 현재 버전을 전환합니다.
- 사용자 공간 desktop launcher, `.desktop` 파일에는 `sudo`를 사용하지 않습니다.
- VS Code 공식 desktop files: `code.desktop`, `code-url-handler.desktop`.
- `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn`, `ko` locales.
- 변경 전 더 안전한 검증을 위한 dry-run mode.

## 요구 사항

ATM은 Ubuntu, Zorin 및 유사한 Linux 데스크톱 배포판을 대상으로 합니다.

필수 공통 명령:

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

특정 워크플로에서 사용하는 선택 명령:

```bash
xdg-mime
update-desktop-database
sudo
```

desktop launcher에는 `sudo`를 사용하지 않습니다. 선택적 system-level command setup과 시스템 패키지를 설치하는 플러그인에만 필요합니다.

## 빠른 시작

저장소를 clone하고 메뉴를 실행합니다:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

portable mode를 구성합니다:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

사용 가능한 플러그인을 나열합니다:

```bash
ATM_LANG=en-us atm plugins list
```

PATH, CLI 링크, desktop launcher를 적용합니다:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## 일반 CLI 사용

도구 버전 설치:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

설치된 버전으로 전환:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

설치된 버전 하나 제거:

```bash
atm remove go 1.25.9
```

플러그인 하나가 관리하는 모든 항목 제거:

```bash
atm uninstall go
```

dry-run 실행:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## 메인 메뉴

대화형 메뉴 실행:

```bash
ATM_LANG=en-us atm
```

메인 메뉴는 환경 정보와 현재 상태가 포함된 플러그인 목록을 보여줍니다:

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
d) 🩺 Doctor
u) ♻️  Self-update
q) ❌ Exit
------------------------------------------
Choose an option:
```

메인 메뉴 작업:

| 옵션 | 작업 |
|---|---|
| `1` to `9` | 선택한 플러그인 하위 메뉴를 엽니다. |
| `f` | stack installation이 활성화된 플러그인에 대해 Full Setup을 실행합니다. |
| `c` | shell PATH, CLI links, desktop launchers를 구성합니다. |
| `s` | ATM command setup을 엽니다. |
| `p` | 로드된 플러그인을 나열합니다. |
| `d` | doctor checks를 실행합니다. |
| `u` | self-update status를 확인합니다. |
| `q` | 종료합니다. |

## 플러그인 하위 메뉴

각 플러그인은 자체 하위 메뉴를 가집니다. 메뉴는 감지된 버전/상태를 표시한 다음 도구별 설치 및 유지관리 작업을 표시합니다.

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

이 하위 메뉴에서 지원 JDK 배포판 설치, 사용자 지정 URL의 JDK tarball 설치, 설치된 버전 목록, 버전 하나 제거, ATM이 관리하는 모든 Java/JDK 제거를 수행할 수 있습니다.

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

이 하위 메뉴에서 사전 정의된 Flutter 버전 설치, 특정 버전 선택, 설치된 버전 목록, 버전 하나 제거, Flutter 전체 제거를 수행할 수 있습니다.

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

이 하위 메뉴에서 Go 버전 설치, CLI로 현재 Go 전환, 설치된 버전 목록, 버전 하나 제거, ATM이 관리하는 모든 Go 파일 제거를 수행할 수 있습니다.

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

이 하위 메뉴에서 VS Code 버전 설치, 이미 설치된 버전으로 전환, 설치된 버전 목록, 버전 하나 제거, ATM이 관리하는 모든 VS Code 버전 제거를 수행할 수 있습니다.

버전을 전환하면 ATM은 다음을 재생성합니다:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

또한 다음을 등록합니다:

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

이 하위 메뉴에서 Android Studio 설치, 사용자 지정 URL 설치, 설치된 버전 목록, 버전 하나 제거, ATM이 관리하는 모든 Android Studio 제거를 수행할 수 있습니다.

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

이 하위 메뉴에서 Android SDK command-line tools와 package stack 설치, 사용자 지정 SDK 버전 선택, 설치된 패키지 목록, 패키지 제거, ATM이 관리하는 SDK 제거를 수행할 수 있습니다.

ATM 상태는 codename/preview보다 안정적인 숫자 Android API를 우선합니다.

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

이 하위 메뉴에서 감지된 Linux 배포판에 대해 `plugins/os_packages/packages.txt`의 패키지 세트를 설치하거나 제거할 수 있습니다. 실제 패키지 작업에서는 ATM이 root로 실행 중이 아니면 `sudo`를 요청합니다. `--dry-run`은 sudo 요청 없이 package manager 명령을 안전하게 출력합니다.

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

이 단계에서는 `Install Docker Engine`, `Install Docker Compose`, `Install Docker Desktop`이 구현되어 있습니다. Docker Engine은 제공된 Ansible 흐름을 따릅니다: 공식 Docker install script 실행, 대상 사용자를 `docker` 그룹에 추가, Docker service enable/start. Docker Compose는 GitHub에서 최신 공식 standalone binary를 `/usr/local/bin/docker-compose`로 다운로드하고 실행 가능하게 만듭니다. Docker Desktop은 공식 Ubuntu DEB 흐름을 따릅니다: `docker-desktop-amd64.deb` 다운로드, `apt-get update` 실행, `apt`로 로컬 패키지 설치. Install ALL은 이후 patch를 위한 placeholder입니다.

## 설정 메뉴

설정 메뉴는 메인 메뉴의 `s`에서 사용할 수 있습니다:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

portable mode가 권장 기본값입니다. system mode는 command link만 생성하며 프로젝트 파일은 `~/Apps/atm`에 남아 있습니다.

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



## PATH, CLI, Desktop

`atm path apply`는 사용자 shell 파일과 플러그인의 desktop integration을 업데이트합니다.

기본적으로 ATM은 `/etc/profile.d/atm.sh`에 쓰지 않습니다.

이 선택적 system profile 쓰기를 명시적으로 활성화하려면:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

desktop launcher는 다음 위치에 설치됩니다:

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

지원 locales:

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

## 공개 문서

공개 문서는 다음에 있습니다:

```text
docs/
```

사용 가능한 공개 문서:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## 프로젝트 구조

```text
bin/atm                         메인 실행 파일
lib/                            Core Bash 라이브러리
lang/                           Core locale 파일
plugins/<plugin_id>/            플러그인 디렉터리
plugins/<plugin_id>/plugin.*    플러그인 metadata, config, implementation
plugins/<plugin_id>/lang/       플러그인 locale 파일
docs/                           공개 문서
```

## 검증

권장 검사:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 라이선스

[LICENSE](../../LICENSE)를 참고하세요.

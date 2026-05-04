# USER-MANUAL — ATM 사용자 매뉴얼

## 1. ATM 이 하는 일

ATM 은 사용자 공간에 개발 도구를 설치하고 관리합니다.

주요 도구:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

기본 위치:

```text
~/Apps
```

## 2. ATM 시작

프로젝트 폴더에서:

```bash
cd ~/Apps/atm
ATM_LANG=ko bin/atm
```

이미 설정된 경우:

```bash
atm
```

## 3. atm 명령 설정

포터블 모드:

```bash
cd ~/Apps/atm
ATM_LANG=ko bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

시스템 명령 모드:

```bash
cd ~/Apps/atm
ATM_LANG=ko bin/atm setup system
atm --version
```

이 모드는 sudo 를 요청할 수 있습니다.

## 4. 메인 메뉴

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

플러그인 순서는 `plugin.metadata` 에 따라 달라질 수 있습니다.

## 5. PATH, CLI, desktop

```bash
ATM_LANG=ko atm path apply
```

PATH, `code`, desktop launchers, `vscode://` handler 를 설정합니다.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. 도구 설치

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

Android Studio 는 `atm` 을 실행하고 Android Studio 를 선택한 뒤 버전을 고릅니다.

Android SDK:

```text
~/Apps/AndroidStudio/SDK
```

Flutter:

```bash
atm install flutter --version 3.41.8
atm use flutter 3.41.8
```

## 7. 플러그인

```bash
atm plugins list
```

## 8. Dry run

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. 문제 해결

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=ko atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=ko atm path apply
```

중복 플러그인 ID 확인:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. ATM 제거

```bash
atm uninstall atm
```

수동 정리:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

`~/.bashrc`, `~/.zshrc`, `~/.profile` 에서 ATM PATH 블록도 제거하세요.


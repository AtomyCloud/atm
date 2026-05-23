# ATM - Atomy Tools Modules

<div align="center">

#### _README на других языках._
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

ATM — модульный менеджер Linux-инструментов, написанный на Bash. Он устанавливает и управляет инструментами разработки в пользовательском пространстве, хранит несколько версий рядом, переключает активную версию через symlink `current`, настраивает PATH оболочки и создаёт desktop-launchers для GUI-инструментов.

ATM рассчитан на portable-first рабочий процесс:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Поддерживаемые Плагины

| Плагин | ID | Версия | Назначение |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Устанавливает и переключает версии Java/JDK от поддерживаемых поставщиков или пользовательских URL. |
| Flutter SDK | `flutter` | `0.0.1` | Устанавливает, переключает, удаляет и деинсталлирует версии Flutter SDK. |
| Go | `go` | `0.0.1` | Устанавливает, переключает, удаляет и деинсталлирует версии Go, а также управляет Go workspace. |
| VS Code | `vscode` | `0.0.1` | Устанавливает версии VS Code, переключает установленные версии, настраивает CLI и desktop launchers. |
| Android Studio | `android_studio` | `0.0.1` | Устанавливает и переключает версии Android Studio и создаёт desktop launchers. |
| Android SDK | `android_sdk` | `0.0.1` | Устанавливает Android SDK command-line tools, platforms, build-tools, CMake, NDK, emulator и platform-tools. |
| OS Packages | `os_packages` | `0.0.1` | Устанавливает и удаляет общие пакеты ОС через обнаруженный пакетный менеджер. |
| Docker | `docker` | `0.0.1` | Устанавливает Docker Engine, Docker Compose и Docker Desktop из официальных источников. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## Возможности

- Модульная архитектура плагинов.
- Portable install mode в `~/Apps/atm`.
- Опциональная системная ссылка команды в `/usr/local/bin/atm`.
- Версионированные установки инструментов в `~/Apps`.
- `atm use <plugin> <version>` для переключения текущей версии.
- Desktop launchers в пользовательском пространстве, без `sudo` для `.desktop` файлов.
- Официальные desktop-файлы VS Code: `code.desktop` и `code-url-handler.desktop`.
- Locales для `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn` и `ko`.
- Dry-run режим для более безопасной проверки перед изменениями.

## Требования

ATM ориентирован на Linux desktop-системы, такие как Ubuntu, Zorin и похожие дистрибутивы.

Обязательные общие команды:

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

Опциональные команды для отдельных сценариев:

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` не используется для desktop-launchers. Он нужен только для опциональной системной настройки команды и плагинов, устанавливающих системные пакеты.

## Быстрый Старт

Клонируйте репозиторий и запустите меню:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Настройте portable mode:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Показать доступные плагины:

```bash
ATM_LANG=en-us atm plugins list
```

Применить PATH, CLI-ссылки и desktop-launchers:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Типовое Использование CLI

Установить версию инструмента:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Переключиться на установленную версию:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Удалить одну установленную версию:

```bash
atm remove go 1.25.9
```

Удалить всё, чем управляет один плагин:

```bash
atm uninstall go
```

Запустить dry-run:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Главное Меню

Запустите интерактивное меню:

```bash
ATM_LANG=en-us atm
```

Главное меню показывает информацию окружения и список плагинов с текущим состоянием:

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

Действия главного меню:

| Опция | Действие |
|---|---|
| `1` to `9` | Открывает подменю выбранного плагина. |
| `f` | Запускает Full Setup для плагинов, включённых для установки stack. |
| `c` | Настраивает shell PATH, CLI links и desktop launchers. |
| `s` | Открывает настройку команды ATM. |
| `p` | Показывает загруженные плагины. |
| `d` | Запускает doctor checks. |
| `u` | Проверяет статус self-update. |
| `q` | Выход. |

## Подменю Плагинов

У каждого плагина своё подменю. Меню показывают найденную версию/статус, затем действия установки и обслуживания.

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

Используйте это подменю для установки поддерживаемых JDK-дистрибутивов, установки JDK tarball по пользовательскому URL, списка установленных версий, удаления версии или удаления всех Java/JDK, управляемых ATM.

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

Используйте это подменю для установки предопределённых версий Flutter, выбора конкретной версии, списка установленных версий, удаления версии или полного удаления Flutter.

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

Используйте это подменю для установки версий Go, переключения текущего Go через CLI, списка установленных версий, удаления версии или удаления всех файлов Go, управляемых ATM.

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

Используйте это подменю для установки версий VS Code, переключения на уже установленную версию, списка установленных версий, удаления версии или удаления всех версий VS Code, управляемых ATM.

При переключении версий ATM пересоздаёт:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

Также регистрирует:

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

Используйте это подменю для установки Android Studio, установки из пользовательского URL, списка установленных версий, удаления версии или удаления всех Android Studio, управляемых ATM.

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

Используйте это подменю для установки command-line tools и стеков Android SDK, выбора пользовательских SDK-версий, списка установленных пакетов, удаления пакета или удаления SDK, управляемого ATM.

Статус ATM предпочитает стабильные числовые Android API вместо codenames/previews, если доступны оба варианта.

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

Используйте это подменю для установки или удаления набора пакетов из `plugins/os_packages/packages.txt` для найденного Linux-дистрибутива. Для реальных операций плагин запрашивает `sudo`, если ATM не запущен как root. `--dry-run` безопасно печатает команды пакетного менеджера без запроса sudo.

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

На этой фазе реализованы `Install Docker Engine`, `Install Docker Compose` и `Install Docker Desktop`. Docker Engine следует предоставленному Ansible-процессу: запуск официального install script Docker, добавление целевого пользователя в группу `docker`, включение и запуск Docker service. Docker Compose скачивает последний официальный standalone binary с GitHub в `/usr/local/bin/docker-compose` и делает его исполняемым. Docker Desktop следует официальному Ubuntu DEB-процессу: скачать `docker-desktop-amd64.deb`, выполнить `apt-get update` и установить локальный пакет через `apt`. Install ALL остаётся placeholder для следующего patch.

## Меню Настройки

Меню настройки доступно через `s` в Главном Меню:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

Portable mode — рекомендуемый вариант по умолчанию. System mode создаёт только command link; файлы проекта остаются в `~/Apps/atm`.

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



## PATH, CLI и Desktop

`atm path apply` обновляет пользовательские shell-файлы и desktop-интеграцию плагинов.

По умолчанию ATM не пишет в `/etc/profile.d/atm.sh`.

Чтобы явно включить эту опциональную запись system profile:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

Desktop-launchers устанавливаются в:

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

Поддерживаемые locales:

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

## Публичная Документация

Публичная документация находится в:

```text
docs/
```

Доступные публичные документы:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## Структура Проекта

```text
bin/atm                         Основной исполняемый файл
lib/                            Основные Bash-библиотеки
lang/                           Основные locale-файлы
plugins/<plugin_id>/            Каталоги плагинов
plugins/<plugin_id>/plugin.*    Метаданные, конфигурация и реализация плагина
plugins/<plugin_id>/lang/       Locale-файлы плагина
docs/                           Публичная документация
```

## Валидация

Рекомендуемые проверки:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## Лицензия

См. [LICENSE](../../LICENSE).

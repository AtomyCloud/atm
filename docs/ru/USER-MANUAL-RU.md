# USER-MANUAL — Руководство пользователя ATM

## 1. Что делает ATM

ATM устанавливает и управляет инструментами разработки в пользовательском пространстве.

Основные инструменты:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Путь по умолчанию:

```text
~/Apps
```

## 2. Запуск ATM

Из папки проекта:

```bash
cd ~/Apps/atm
ATM_LANG=ru bin/atm
```

Если уже настроено:

```bash
atm
```

## 3. Настройка команды atm

Портативный режим:

```bash
cd ~/Apps/atm
ATM_LANG=ru bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Системный режим:

```bash
cd ~/Apps/atm
ATM_LANG=ru bin/atm setup system
atm --version
```

Этот режим может запросить sudo.

## 4. Главное меню

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

Порядок плагинов может меняться по `plugin.metadata`.

## 5. PATH, CLI и desktop

```bash
ATM_LANG=ru atm path apply
```

Настраивает PATH, `code`, desktop launchers и handler `vscode://`.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Установка инструментов

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

Для Android Studio запустите `atm`, выберите Android Studio и версию.

Android SDK:

```text
~/Apps/AndroidStudio/SDK
```

Flutter:

```bash
atm install flutter --version 3.41.8
atm use flutter 3.41.8
```

## 7. Плагины

```bash
atm plugins list
```

## 8. Dry run

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. Устранение проблем

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=ru atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=ru atm path apply
```

Дублирующиеся ID плагинов:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. Удаление ATM

```bash
atm uninstall atm
```

Ручная очистка:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Удалите PATH-блок ATM из `~/.bashrc`, `~/.zshrc` или `~/.profile`.


# USER-MANUAL — ATM Benutzerhandbuch

## 1. Was ATM macht

ATM installiert und verwaltet Entwicklerwerkzeuge im Benutzerbereich.

Hauptwerkzeuge:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Standardort:

```text
~/Apps
```

## 2. ATM starten

Aus dem Projektordner:

```bash
cd ~/Apps/atm
ATM_LANG=de bin/atm
```

Wenn bereits konfiguriert:

```bash
atm
```

## 3. Befehl atm einrichten

Portabler Modus:

```bash
cd ~/Apps/atm
ATM_LANG=de bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Systemmodus:

```bash
cd ~/Apps/atm
ATM_LANG=de bin/atm setup system
atm --version
```

Dieser Modus kann sudo anfordern.

## 4. Hauptmenue

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

Die Plugin-Reihenfolge kann durch `plugin.metadata` variieren.

## 5. PATH, CLI und Desktop

```bash
ATM_LANG=de atm path apply
```

Konfiguriert PATH, `code`, Desktop-Launcher und den Handler `vscode://`.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Werkzeuge installieren

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

Fuer Android Studio `atm` starten, Android Studio waehlen und eine Version auswaehlen.

Android SDK:

```text
~/Apps/AndroidStudio/SDK
```

Flutter:

```bash
atm install flutter --version 3.41.8
atm use flutter 3.41.8
```

## 7. Plugins

```bash
atm plugins list
```

## 8. Dry run

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. Fehlerbehebung

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=de atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=de atm path apply
```

Doppelte Plugin-IDs:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. ATM deinstallieren

```bash
atm uninstall atm
```

Manuelle Bereinigung:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Entfernen Sie den ATM-PATH-Block aus `~/.bashrc`, `~/.zshrc` oder `~/.profile`.


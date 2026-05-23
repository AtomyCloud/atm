# ATM - Atomy Tools Modules

<div align="center">

#### _README in anderen Sprachen._
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

ATM ist ein modularer Linux-Tool-Manager in Bash. Er installiert und verwaltet Entwicklerwerkzeuge im Benutzerbereich, hält mehrere Versionen parallel vor, wechselt die aktive Version über `current`-Symlinks, konfiguriert Shell-PATH-Einträge und erstellt Desktop-Starter für grafische Werkzeuge.

ATM ist für einen portable-first Workflow ausgelegt:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Unterstützte Plugins

| Plugin | ID | Version | Zweck |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Installiert und wechselt Java/JDK-Versionen von unterstützten Anbietern oder benutzerdefinierten URLs. |
| Flutter SDK | `flutter` | `0.0.1` | Installiert, wechselt, entfernt und deinstalliert Flutter-SDK-Versionen. |
| Go | `go` | `0.0.1` | Installiert, wechselt, entfernt und deinstalliert Go-Versionen und verwaltet einen Go-Workspace. |
| VS Code | `vscode` | `0.0.1` | Installiert VS-Code-Versionen, wechselt installierte Versionen, konfiguriert CLI und Desktop-Starter. |
| Android Studio | `android_studio` | `0.0.1` | Installiert und wechselt Android-Studio-Versionen und erstellt Desktop-Starter. |
| Android SDK | `android_sdk` | `0.0.1` | Installiert Android-SDK command-line tools, platforms, build-tools, CMake, NDK, emulator und platform-tools. |
| OS Packages | `os_packages` | `0.0.1` | Installiert und entfernt gängige Betriebssystempakete mit dem erkannten Paketmanager. |
| Docker | `docker` | `0.0.1` | Installiert Docker Engine, Docker Compose und Docker Desktop aus offiziellen Quellen. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## Funktionen

- Modulare Plugin-Architektur.
- Portabler Installationsmodus unter `~/Apps/atm`.
- Optionaler Systembefehlslink unter `/usr/local/bin/atm`.
- Versionierte Tool-Installationen unter `~/Apps`.
- `atm use <plugin> <version>` zum Wechseln der aktuellen Version.
- Desktop-Starter im Benutzerbereich, kein `sudo` für `.desktop`-Dateien.
- Offizielle VS-Code-Desktop-Dateien: `code.desktop` und `code-url-handler.desktop`.
- Locales für `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn` und `ko`.
- Dry-run-Modus für sicherere Validierung vor Änderungen.

## Anforderungen

ATM richtet sich an Linux-Desktop-Systeme wie Ubuntu, Zorin und ähnliche Distributionen.

Erforderliche allgemeine Befehle:

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

Optionale Befehle für bestimmte Abläufe:

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` wird nicht für Desktop-Starter verwendet. Es wird nur für optionale systemweite Befehlseinrichtung und Plugins benötigt, die Systempakete installieren.

## Schnellstart

Repository klonen und Menü starten:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Portablen Modus konfigurieren:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Verfügbare Plugins auflisten:

```bash
ATM_LANG=en-us atm plugins list
```

PATH, CLI-Links und Desktop-Starter anwenden:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Häufige CLI-Nutzung

Eine Tool-Version installieren:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Zu einer installierten Version wechseln:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Eine installierte Version entfernen:

```bash
atm remove go 1.25.9
```

Alles deinstallieren, was von einem Plugin verwaltet wird:

```bash
atm uninstall go
```

Dry-run ausführen:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Hauptmenü

Interaktives Menü starten mit:

```bash
ATM_LANG=en-us atm
```

Das Hauptmenü zeigt Umgebungsinformationen und die Plugin-Liste mit aktuellem Status:

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

Aktionen im Hauptmenü:

| Option | Aktion |
|---|---|
| `1` to `9` | Öffnet das Untermenü des ausgewählten Plugins. |
| `f` | Führt Full Setup für Plugins aus, die für Stack-Installation aktiviert sind. |
| `c` | Konfiguriert Shell-PATH, CLI-Links und Desktop-Starter. |
| `s` | Öffnet die ATM-Befehlseinrichtung. |
| `p` | Listet geladene Plugins auf. |
| `d` | Führt Doctor-Prüfungen aus. |
| `u` | Prüft den Self-update-Status. |
| `q` | Beendet. |

## Plugin-Untermenüs

Jedes Plugin besitzt ein eigenes Untermenü. Menüs zeigen die erkannte Version/den Status und danach tool-spezifische Installations- und Wartungsaktionen.

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

Dieses Untermenü installiert unterstützte JDK-Distributionen, installiert ein JDK-Tarball über eine benutzerdefinierte URL, listet installierte Versionen, entfernt eine Version oder deinstalliert alle von ATM verwalteten Java/JDK-Versionen.

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

Dieses Untermenü installiert vordefinierte Flutter-Versionen, wählt eine bestimmte Version, listet installierte Versionen, entfernt eine Version oder deinstalliert Flutter vollständig.

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

Dieses Untermenü installiert Go-Versionen, wechselt das aktuelle Go per CLI, listet installierte Versionen, entfernt eine Version oder deinstalliert alle von ATM verwalteten Go-Dateien.

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

Dieses Untermenü installiert VS-Code-Versionen, wechselt zu einer bereits installierten Version, listet installierte Versionen, entfernt eine Version oder deinstalliert alle von ATM verwalteten VS-Code-Versionen.

Beim Versionswechsel erzeugt ATM neu:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

Außerdem registriert es:

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

Dieses Untermenü installiert Android Studio, installiert aus einer benutzerdefinierten URL, listet installierte Versionen, entfernt eine Version oder deinstalliert alle von ATM verwalteten Android-Studio-Versionen.

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

Dieses Untermenü installiert Android-SDK-Command-line-Tools und Paketstacks, wählt benutzerdefinierte SDK-Versionen, listet installierte Pakete, entfernt ein Paket oder deinstalliert das von ATM verwaltete SDK.

Der ATM-Status bevorzugt stabile numerische Android-APIs gegenüber Codenames/Previews, wenn beide vorhanden sind.

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

Dieses Untermenü installiert oder entfernt den Paketsatz aus `plugins/os_packages/packages.txt` für die erkannte Linux-Distribution. Bei echten Paketoperationen fragt dieses Plugin nach `sudo`, wenn ATM nicht bereits als root läuft. `--dry-run` gibt die Paketmanager-Befehle sicher aus, ohne sudo anzufordern.

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

In dieser Phase sind `Install Docker Engine`, `Install Docker Compose` und `Install Docker Desktop` implementiert. Docker Engine folgt dem bereitgestellten Ansible-Ablauf: offizielles Docker-Installationsskript ausführen, Zielbenutzer zur Gruppe `docker` hinzufügen und den Docker-Dienst aktivieren/starten. Docker Compose lädt das neueste offizielle standalone Binary von GitHub nach `/usr/local/bin/docker-compose` und macht es ausführbar. Docker Desktop folgt dem offiziellen Ubuntu-DEB-Ablauf: `docker-desktop-amd64.deb` herunterladen, `apt-get update` ausführen und das lokale Paket mit `apt` installieren. Install ALL bleibt ein Platzhalter für einen späteren Patch.

## Setup-Menü

Das Setup-Menü ist im Hauptmenü über `s` verfügbar:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

Der portable Modus ist der empfohlene Standard. Der Systemmodus erstellt nur einen Befehlslink; Projektdateien bleiben unter `~/Apps/atm`.

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



## PATH, CLI und Desktop

`atm path apply` aktualisiert Benutzer-Shell-Dateien und die Desktop-Integration der Plugins.

Standardmäßig schreibt ATM nicht nach `/etc/profile.d/atm.sh`.

Um dieses optionale systemweite Profil-Schreiben explizit zu aktivieren:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

Desktop-Starter werden installiert in:

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

Unterstützte Locales:

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

## Öffentliche Dokumentation

Öffentliche Dokumentation befindet sich in:

```text
docs/
```

Verfügbare öffentliche Dokumente:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## Projektstruktur

```text
bin/atm                         Hauptausführbare Datei
lib/                            Zentrale Bash-Bibliotheken
lang/                           Zentrale Locale-Dateien
plugins/<plugin_id>/            Plugin-Verzeichnisse
plugins/<plugin_id>/plugin.*    Plugin-Metadaten, Konfiguration und Implementierung
plugins/<plugin_id>/lang/       Plugin-Locale-Dateien
docs/                           Öffentliche Dokumentation
```

## Validierung

Empfohlene Prüfungen:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## Lizenz

Siehe [LICENSE](../../LICENSE).

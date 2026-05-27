# ATM - Atomy Tools Modules

<div align="center">

#### _README in altre lingue._
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

ATM è un gestore modulare di strumenti Linux scritto in Bash. Installa e gestisce strumenti di sviluppo nello spazio utente, mantiene più versioni affiancate, cambia la versione attiva con symlink `current`, configura le voci PATH della shell e crea launcher desktop per strumenti grafici.

ATM è progettato per un flusso di lavoro prima di tutto portatile:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Plugin Supportati

| Plugin | ID | Versione | Scopo |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Installa e cambia versioni Java/JDK da vendor supportati o URL personalizzati. |
| Flutter SDK | `flutter` | `0.0.1` | Installa, cambia, rimuove e disinstalla versioni del Flutter SDK. |
| Go | `go` | `0.0.1` | Installa, cambia, rimuove e disinstalla versioni di Go e gestisce un workspace Go. |
| VS Code | `vscode` | `0.0.1` | Installa versioni di VS Code, cambia versioni installate, configura CLI e launcher desktop. |
| Android Studio | `android_studio` | `0.0.1` | Installa e cambia versioni di Android Studio e crea launcher desktop. |
| Android SDK | `android_sdk` | `0.0.1` | Installa command-line tools, platforms, build-tools, CMake, NDK, emulator e platform-tools dell’Android SDK. |
| OS Packages | `os_packages` | `0.0.1` | Installa e rimuove pacchetti comuni del sistema usando il package manager rilevato. |
| Docker | `docker` | `0.0.1` | Installa Docker Engine, Docker Compose e Docker Desktop da fonti ufficiali. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## Funzionalità

- Architettura modulare a plugin.
- Modalità di installazione portatile in `~/Apps/atm`.
- Link opzionale del comando di sistema in `/usr/local/bin/atm`.
- Installazioni versionate degli strumenti in `~/Apps`.
- `atm use <plugin> <version>` per cambiare la versione corrente.
- Launcher desktop nello spazio utente, senza `sudo` per file `.desktop`.
- File desktop ufficiali di VS Code: `code.desktop` e `code-url-handler.desktop`.
- Locales per `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn` e `ko`.
- Modalità dry-run per validare in modo più sicuro prima delle modifiche.

## Requisiti

ATM è pensato per sistemi Linux desktop come Ubuntu, Zorin e distribuzioni simili.

Comandi comuni richiesti:

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

Comandi opzionali usati da flussi specifici:

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` non viene usato per i launcher desktop. È necessario solo per la configurazione opzionale del comando di sistema e per plugin che installano pacchetti di sistema.

## Avvio Rapido

Clona il repository ed esegui il menu:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Configura la modalità portatile:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Elenca i plugin disponibili:

```bash
ATM_LANG=en-us atm plugins list
```

Applica PATH, link CLI e launcher desktop:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Uso Comune della CLI

Installare una versione di uno strumento:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Passare a una versione installata:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Rimuovere una versione installata:

```bash
atm remove go 1.25.9
```

Disinstallare tutto ciò che è gestito da un plugin:

```bash
atm uninstall go
```

Eseguire un dry-run:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Menu Principale

Esegui il menu interattivo con:

```bash
ATM_LANG=en-us atm
```

Il menu principale mostra le informazioni dell’ambiente e la lista dei plugin con lo stato corrente:

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

Azioni del menu principale:

| Opzione | Azione |
|---|---|
| `1` to `9` | Apre il sottomenu del plugin selezionato. |
| `f` | Esegue Full Setup per i plugin abilitati all’installazione dello stack. |
| `c` | Configura PATH della shell, link CLI e launcher desktop. |
| `s` | Apre la configurazione del comando ATM. |
| `p` | Elenca i plugin caricati. |
| `d` | Esegue controlli doctor. |
| `u` | Controlla lo stato di self-update. |
| `q` | Esce. |

## Sottomenu dei Plugin

Ogni plugin possiede il proprio sottomenu. I menu mostrano la versione/stato rilevato e poi le azioni specifiche di installazione e manutenzione.

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

Usa questo sottomenu per installare distribuzioni JDK supportate, installare un tarball JDK da URL personalizzato, elencare versioni installate, rimuovere una versione o disinstallare tutti i Java/JDK gestiti da ATM.

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

Usa questo sottomenu per installare versioni Flutter predefinite, scegliere una versione specifica, elencare versioni installate, rimuovere una versione o disinstallare Flutter completamente.

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

Usa questo sottomenu per installare versioni Go, cambiare il Go corrente tramite CLI, elencare versioni installate, rimuovere una versione o disinstallare tutti i file Go gestiti da ATM.

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

Usa questo sottomenu per installare versioni di VS Code, passare a una versione già installata, elencare versioni installate, rimuovere una versione o disinstallare tutte le versioni di VS Code gestite da ATM.

Quando si cambia versione, ATM rigenera:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

Registra anche:

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

Usa questo sottomenu per installare Android Studio, installare da URL personalizzato, elencare versioni installate, rimuovere una versione o disinstallare tutti gli Android Studio gestiti da ATM.

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

Usa questo sottomenu per installare command-line tools e stack Android SDK, scegliere versioni SDK personalizzate, elencare pacchetti installati, rimuovere un pacchetto o disinstallare l’SDK gestito da ATM.

Lo stato di ATM preferisce API Android numeriche stabili rispetto a codename/preview quando esistono entrambe.

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

Usa questo sottomenu per installare o rimuovere il set di pacchetti da `plugins/os_packages/packages.txt` per la distribuzione Linux rilevata. Per operazioni reali, questo plugin chiede `sudo` quando ATM non è già eseguito come root. `--dry-run` stampa in sicurezza i comandi del package manager senza chiedere sudo.

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

In questa fase, `Install Docker Engine`, `Install Docker Compose` e `Install Docker Desktop` sono implementati. Docker Engine segue il flusso Ansible fornito: esegue lo script ufficiale di installazione Docker, aggiunge l’utente target al gruppo `docker` e abilita/avvia il servizio Docker. Docker Compose scarica il binario standalone ufficiale più recente da GitHub in `/usr/local/bin/docker-compose` e lo rende eseguibile. Docker Desktop segue il flusso DEB ufficiale Ubuntu: scarica `docker-desktop-amd64.deb`, esegue `apt-get update` e installa il pacchetto locale con `apt`. Install ALL resta un placeholder per una patch successiva.

## Menu di Configurazione

Il menu di configurazione è disponibile da `s` nel Menu Principale:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

La modalità portatile è il default consigliato. La modalità sistema crea solo un link di comando; i file del progetto restano sotto `~/Apps/atm`.

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



## PATH, CLI e Desktop

`atm path apply` aggiorna i file shell utente e l’integrazione desktop dei plugin.

Per impostazione predefinita, ATM non scrive `/etc/profile.d/atm.sh`.

Per abilitare esplicitamente questa scrittura opzionale del profilo di sistema:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

I launcher desktop vengono installati in:

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

Locales supportati:

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

## Documentazione Pubblica

La documentazione pubblica si trova in:

```text
docs/
```

Documenti pubblici disponibili:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## Struttura del Progetto

```text
bin/atm                         Eseguibile principale
lib/                            Librerie Bash core
lang/                           File locale core
plugins/<plugin_id>/            Directory dei plugin
plugins/<plugin_id>/plugin.*    Metadati, configurazione e implementazione del plugin
plugins/<plugin_id>/lang/       File locale del plugin
docs/                           Documentazione pubblica
```

## Validazione

Controlli consigliati:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## Licenza

Vedi [LICENSE](../../LICENSE).

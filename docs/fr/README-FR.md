# ATM - Atomy Tools Modules

<div align="center">

#### _README dans d’autres langues._
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

ATM est un gestionnaire modulaire d’outils Linux écrit en Bash. Il installe et gère les outils de développement dans l’espace utilisateur, garde plusieurs versions côte à côte, bascule la version active avec des liens symboliques `current`, configure les entrées PATH du shell et crée des lanceurs desktop pour les outils graphiques.

ATM est conçu pour un flux de travail portable par défaut :

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Plugins Pris en Charge

| Plugin | ID | Version | Objectif |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Installe et bascule les versions Java/JDK depuis des fournisseurs pris en charge ou des URL personnalisées. |
| Flutter SDK | `flutter` | `0.0.1` | Installe, bascule, supprime et désinstalle les versions du Flutter SDK. |
| Go | `go` | `0.0.1` | Installe, bascule, supprime et désinstalle les versions de Go et gère un workspace Go. |
| VS Code | `vscode` | `0.0.1` | Installe des versions de VS Code, bascule les versions installées, configure la CLI et les lanceurs desktop. |
| Android Studio | `android_studio` | `0.0.1` | Installe et bascule les versions d’Android Studio et crée des lanceurs desktop. |
| Android SDK | `android_sdk` | `0.0.1` | Installe les command-line tools, platforms, build-tools, CMake, NDK, emulator et platform-tools de l’Android SDK. |
| OS Packages | `os_packages` | `0.0.1` | Installe et supprime les paquets système communs avec le gestionnaire de paquets détecté. |
| Docker | `docker` | `0.0.1` | Installe Docker Engine, Docker Compose et Docker Desktop depuis les sources officielles. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |
| Node.js | `node` | `0.0.1` | Install, switch, remove, and uninstall Node.js versions from official releases. |

## Fonctionnalités

- Architecture de plugins modulaire.
- Mode d’installation portable sous `~/Apps/atm`.
- Lien optionnel de commande système dans `/usr/local/bin/atm`.
- Installations versionnées des outils sous `~/Apps`.
- `atm use <plugin> <version>` pour basculer la version courante.
- Lanceurs desktop en espace utilisateur, sans `sudo` pour les fichiers `.desktop`.
- Fichiers desktop officiels de VS Code : `code.desktop` et `code-url-handler.desktop`.
- Locales pour `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn` et `ko`.
- Mode dry-run pour valider plus sûrement avant les changements.

## Prérequis

ATM cible les systèmes Linux desktop comme Ubuntu, Zorin et les distributions similaires.

Commandes communes requises :

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

Commandes optionnelles utilisées par certains flux :

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` n’est pas utilisé pour les lanceurs desktop. Il est seulement nécessaire pour la configuration optionnelle de commande système et les plugins qui installent des paquets système.

## Démarrage Rapide

Clonez le dépôt et lancez le menu :

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Configurez le mode portable :

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Listez les plugins disponibles :

```bash
ATM_LANG=en-us atm plugins list
```

Appliquez PATH, liens CLI et lanceurs desktop :

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Utilisation Courante de la CLI

Installer une version d’outil :

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Basculer vers une version installée :

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Supprimer une version installée :

```bash
atm remove go 1.25.9
```

Désinstaller tout ce qui est géré par un plugin :

```bash
atm uninstall go
```

Exécuter un dry-run :

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Menu Principal

Lancez le menu interactif avec :

```bash
ATM_LANG=en-us atm
```

Le menu principal affiche les informations d’environnement et la liste des plugins avec leur état actuel :

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

Actions du menu principal :

| Option | Action |
|---|---|
| `1` to `10` | Ouvre le sous-menu du plugin sélectionné. |
| `f` | Exécute Full Setup pour les plugins activés pour l’installation de stack. |
| `c` | Configure le PATH du shell, les liens CLI et les lanceurs desktop. |
| `s` | Ouvre la configuration de la commande ATM. |
| `p` | Liste les plugins chargés. |
| `l` | Change la langue configurée d’ATM. |
| `d` | Exécute les contrôles doctor. |
| `u` | Vérifie l’état de self-update. |
| `q` | Quitte. |

## Sous-menus des Plugins

Chaque plugin possède son sous-menu. Les menus affichent la version/l’état détecté, puis les actions d’installation et de maintenance propres à l’outil.

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

Utilisez ce sous-menu pour installer des distributions JDK prises en charge, installer un tarball JDK depuis une URL personnalisée, lister les versions installées, supprimer une version ou désinstaller tous les Java/JDK gérés par ATM.

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

Utilisez ce sous-menu pour installer des versions Flutter prédéfinies, choisir une version spécifique, lister les versions installées, supprimer une version ou désinstaller complètement Flutter.

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

Utilisez ce sous-menu pour installer des versions de Go, changer le Go courant via CLI, lister les versions installées, supprimer une version ou désinstaller tous les fichiers Go gérés par ATM.

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

Utilisez ce sous-menu pour installer des versions de VS Code, basculer vers une version déjà installée, lister les versions installées, supprimer une version ou désinstaller toutes les versions de VS Code gérées par ATM.

Lors d’un changement de version, ATM régénère :

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

Il enregistre aussi :

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

Utilisez ce sous-menu pour installer Android Studio, installer depuis une URL personnalisée, lister les versions installées, supprimer une version ou désinstaller tous les Android Studio gérés par ATM.

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

Utilisez ce sous-menu pour installer les command-line tools et stacks Android SDK, choisir des versions SDK personnalisées, lister les paquets installés, supprimer un paquet ou désinstaller le SDK géré par ATM.

L’état ATM préfère les API Android numériques stables aux codenames/previews lorsque les deux existent.

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

Utilisez ce sous-menu pour installer ou supprimer l’ensemble de paquets de `plugins/os_packages/packages.txt` pour la distribution Linux détectée. Pour les opérations réelles, ce plugin demande `sudo` quand ATM ne tourne pas déjà en root. `--dry-run` imprime les commandes du gestionnaire de paquets en sécurité sans demander sudo.

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

Dans cette phase, `Install Docker Engine`, `Install Docker Compose` et `Install Docker Desktop` sont implémentés. Docker Engine suit le flux Ansible fourni : exécuter le script officiel d’installation Docker, ajouter l’utilisateur cible au groupe `docker`, puis activer/démarrer le service Docker. Docker Compose télécharge le dernier binaire standalone officiel depuis GitHub vers `/usr/local/bin/docker-compose` et le rend exécutable. Docker Desktop suit le flux DEB officiel Ubuntu : télécharger `docker-desktop-amd64.deb`, exécuter `apt-get update` et installer le paquet local avec `apt`. Install ALL reste un placeholder pour un patch ultérieur.

## Menu de Configuration

Le menu de configuration est disponible avec `s` dans le Menu Principal :

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

Le mode portable est le défaut recommandé. Le mode système crée seulement un lien de commande ; les fichiers du projet restent sous `~/Apps/atm`.

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
b) Back
q) Exit
```

Each major-version submenu fetches the latest three available versions from the official Node.js distribution index at `https://nodejs.org/dist/index.json`. Installations use official `node-v<version>-<platform>.tar.xz` archives, extract into `~/Apps/node/<version>`, and update `~/Apps/node/current`.

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



## PATH, CLI et Desktop

`atm path apply` met à jour les fichiers shell utilisateur et l’intégration desktop des plugins.

Par défaut, ATM n’écrit pas dans `/etc/profile.d/atm.sh`.

Pour activer explicitement cette écriture optionnelle de profil système :

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

Les lanceurs desktop sont installés dans :

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

Locales prises en charge :

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

## Documentation Publique

La documentation publique se trouve dans :

```text
docs/
```

Documents publics disponibles :

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## Structure du Projet

```text
bin/atm                         Exécutable principal
lib/                            Bibliothèques Bash centrales
lang/                           Fichiers de locale centraux
plugins/<plugin_id>/            Répertoires de plugins
plugins/<plugin_id>/plugin.*    Métadonnées, configuration et implémentation du plugin
plugins/<plugin_id>/lang/       Fichiers de locale du plugin
docs/                           Documentation publique
```

## Validation

Vérifications recommandées :

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## Licence

Voir [LICENSE](../../LICENSE).

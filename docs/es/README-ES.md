# ATM - Atomy Tools Modules

<div align="center">

#### _README en otros idiomas._
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

ATM es un gestor modular de herramientas Linux escrito en Bash. Instala y gestiona herramientas de desarrollo en el espacio del usuario, mantiene varias versiones en paralelo, cambia la versión activa con enlaces simbólicos `current`, configura entradas PATH de la shell y crea lanzadores de escritorio para herramientas gráficas.

ATM está diseñado para un flujo de trabajo portátil primero:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Plugins Soportados

| Plugin | ID | Versión | Propósito |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Instala y cambia versiones Java/JDK de proveedores soportados o URLs personalizadas. |
| Flutter SDK | `flutter` | `0.0.1` | Instala, cambia, elimina y desinstala versiones del Flutter SDK. |
| Go | `go` | `0.0.1` | Instala, cambia, elimina y desinstala versiones de Go y gestiona un workspace Go. |
| VS Code | `vscode` | `0.0.1` | Instala versiones de VS Code, cambia versiones instaladas, configura CLI y lanzadores de escritorio. |
| Android Studio | `android_studio` | `0.0.1` | Instala y cambia versiones de Android Studio y crea lanzadores de escritorio. |
| Android SDK | `android_sdk` | `0.0.1` | Instala command-line tools, platforms, build-tools, CMake, NDK, emulator y platform-tools del Android SDK. |
| OS Packages | `os_packages` | `0.0.1` | Instala y elimina paquetes comunes del sistema usando el gestor de paquetes detectado. |
| Docker | `docker` | `0.0.1` | Instala Docker Engine, Docker Compose y Docker Desktop desde fuentes oficiales. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## Funciones

- Arquitectura modular de plugins.
- Modo de instalación portátil en `~/Apps/atm`.
- Enlace opcional de comando del sistema en `/usr/local/bin/atm`.
- Instalaciones versionadas de herramientas en `~/Apps`.
- `atm use <plugin> <version>` para cambiar la versión actual.
- Lanzadores de escritorio en espacio de usuario, sin `sudo` para archivos `.desktop`.
- Archivos desktop oficiales de VS Code: `code.desktop` y `code-url-handler.desktop`.
- Locales para `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn` y `ko`.
- Modo dry-run para validar con más seguridad antes de cambios.

## Requisitos

ATM está orientado a sistemas Linux de escritorio como Ubuntu, Zorin y distribuciones similares.

Comandos comunes requeridos:

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

Comandos opcionales usados por flujos específicos:

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` no se usa para lanzadores de escritorio. Solo se necesita para la configuración opcional del comando del sistema y para plugins que instalan paquetes del sistema.

## Inicio Rápido

Clona el repositorio y ejecuta el menú:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Configura el modo portátil:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Lista los plugins disponibles:

```bash
ATM_LANG=en-us atm plugins list
```

Aplica PATH, enlaces CLI y lanzadores de escritorio:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Uso Común de CLI

Instalar una versión de herramienta:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Cambiar a una versión instalada:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Eliminar una versión instalada:

```bash
atm remove go 1.25.9
```

Desinstalar todo lo gestionado por un plugin:

```bash
atm uninstall go
```

Ejecutar dry-run:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Menú Principal

Ejecuta el menú interactivo con:

```bash
ATM_LANG=en-us atm
```

El menú principal muestra información del entorno y la lista de plugins con el estado actual:

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

Acciones del menú principal:

| Opción | Acción |
|---|---|
| `1` to `9` | Abre el submenú del plugin seleccionado. |
| `f` | Ejecuta Full Setup para plugins habilitados para instalación de stack. |
| `c` | Configura PATH de shell, enlaces CLI y lanzadores de escritorio. |
| `s` | Abre la configuración del comando ATM. |
| `p` | Lista plugins cargados. |
| `d` | Ejecuta comprobaciones doctor. |
| `u` | Comprueba el estado de self-update. |
| `q` | Sale. |

## Submenús de Plugins

Cada plugin tiene su propio submenú. Los menús muestran la versión/estado detectado y luego acciones específicas de instalación y mantenimiento.

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

Usa este submenú para instalar distribuciones JDK soportadas, instalar un tarball JDK desde una URL personalizada, listar versiones instaladas, eliminar una versión o desinstalar todos los Java/JDK gestionados por ATM.

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

Usa este submenú para instalar versiones Flutter predefinidas, elegir una versión específica, listar versiones instaladas, eliminar una versión o desinstalar Flutter por completo.

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

Usa este submenú para instalar versiones de Go, cambiar el Go actual por CLI, listar versiones instaladas, eliminar una versión o desinstalar todos los archivos Go gestionados por ATM.

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

Usa este submenú para instalar versiones de VS Code, cambiar a una versión ya instalada, listar versiones instaladas, eliminar una versión o desinstalar todas las versiones de VS Code gestionadas por ATM.

Al cambiar versiones, ATM regenera:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

También registra:

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

Usa este submenú para instalar Android Studio, instalar desde una URL personalizada, listar versiones instaladas, eliminar una versión o desinstalar todos los Android Studio gestionados por ATM.

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

Usa este submenú para instalar command-line tools y stacks del Android SDK, elegir versiones SDK personalizadas, listar paquetes instalados, eliminar un paquete o desinstalar el SDK gestionado por ATM.

El estado de ATM prefiere APIs Android numéricas estables sobre codenames/previews cuando ambas existen.

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

Usa este submenú para instalar o eliminar el conjunto de paquetes de `plugins/os_packages/packages.txt` para la distribución Linux detectada. Para operaciones reales, este plugin pide `sudo` cuando ATM no se ejecuta como root. `--dry-run` imprime los comandos del gestor de paquetes de forma segura sin pedir sudo.

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

En esta fase, `Install Docker Engine`, `Install Docker Compose` e `Install Docker Desktop` están implementados. Docker Engine sigue el flujo Ansible proporcionado: ejecutar el script oficial de instalación de Docker, añadir el usuario objetivo al grupo `docker` y habilitar/iniciar el servicio Docker. Docker Compose descarga el binario standalone oficial más reciente desde GitHub en `/usr/local/bin/docker-compose` y lo marca como ejecutable. Docker Desktop sigue el flujo DEB oficial de Ubuntu: descargar `docker-desktop-amd64.deb`, ejecutar `apt-get update` e instalar el paquete local con `apt`. Install ALL sigue como placeholder para un patch posterior.

## Menú de Configuración

El menú de configuración está disponible desde `s` en el Menú Principal:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

El modo portátil es el valor recomendado. El modo sistema solo crea un enlace de comando; los archivos del proyecto siguen en `~/Apps/atm`.

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



## PATH, CLI y Escritorio

`atm path apply` actualiza archivos de shell del usuario y la integración de escritorio de los plugins.

Por defecto, ATM no escribe `/etc/profile.d/atm.sh`.

Para habilitar explícitamente esa escritura opcional de perfil del sistema:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

Los lanzadores de escritorio se instalan en:

```text
~/.local/share/applications
```

## Locales

Usa `ATM_LANG` para seleccionar un idioma:

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

Locales soportados:

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

## Documentación Pública

La documentación pública vive en:

```text
docs/
```

Documentos públicos disponibles:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## Estructura del Proyecto

```text
bin/atm                         Ejecutable principal
lib/                            Bibliotecas Bash centrales
lang/                           Archivos de locale centrales
plugins/<plugin_id>/            Directorios de plugins
plugins/<plugin_id>/plugin.*    Metadatos, configuración e implementación del plugin
plugins/<plugin_id>/lang/       Archivos de locale del plugin
docs/                           Documentación pública
```

## Validación

Comprobaciones recomendadas:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## Licencia

Consulta [LICENSE](../../LICENSE).

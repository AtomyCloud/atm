# ATM - Atomy Tools Modules

<div align="center">

#### _README noutros idiomas._
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

ATM é um gestor modular de ferramentas Linux escrito em Bash. Instala e gere ferramentas de desenvolvimento no espaço do utilizador, mantém várias versões lado a lado, alterna a versão ativa com symlinks `current`, configura entradas PATH da shell e cria lançadores desktop para ferramentas gráficas.

ATM foi concebido para um fluxo primeiro portátil:

```text
ATM home:     ~/Apps/atm
Apps root:    ~/Apps
Cache:        ~/.cache/atm
State:        ~/.local/share/atm
Config:       ~/.config/atm
Desktop apps: ~/.local/share/applications
```

## Plugins Suportados

| Plugin | ID | Versão | Objetivo |
|---|---|---:|---|
| Java / JDK | `java` | `0.0.1` | Instala e alterna versões Java/JDK de fornecedores suportados ou URLs personalizadas. |
| Flutter SDK | `flutter` | `0.0.1` | Instala, alterna, remove e desinstala versões do Flutter SDK. |
| Go | `go` | `0.0.1` | Instala, alterna, remove e desinstala versões do Go e gere um workspace Go. |
| VS Code | `vscode` | `0.0.1` | Instala versões do VS Code, alterna versões instaladas, configura CLI e lançadores desktop. |
| Android Studio | `android_studio` | `0.0.1` | Instala e alterna versões do Android Studio e cria lançadores desktop. |
| Android SDK | `android_sdk` | `0.0.1` | Instala command-line tools, platforms, build-tools, CMake, NDK, emulator e platform-tools do Android SDK. |
| OS Packages | `os_packages` | `0.0.1` | Instala e remove pacotes comuns do sistema operativo usando o gestor de pacotes detetado. |
| Docker | `docker` | `0.0.1` | Instala Docker Engine, Docker Compose e Docker Desktop a partir de fontes oficiais. |
| AI Tools | `ai` | `0.0.1` | Install Hermes, Ollama, and PicoClaw AI tools. |

## Funcionalidades

- Arquitetura modular de plugins.
- Modo de instalação portátil em `~/Apps/atm`.
- Link opcional de comando do sistema em `/usr/local/bin/atm`.
- Instalações versionadas de ferramentas em `~/Apps`.
- `atm use <plugin> <version>` para alternar a versão atual.
- Lançadores desktop no espaço do utilizador, sem `sudo` para ficheiros `.desktop`.
- Ficheiros desktop oficiais do VS Code: `code.desktop` e `code-url-handler.desktop`.
- Locales para `en-us`, `pt-br`, `pt-pt`, `es`, `it`, `fr`, `de`, `ru`, `ja`, `zh-cn` e `ko`.
- Modo dry-run para validação mais segura antes de alterações.

## Requisitos

ATM destina-se a sistemas Linux desktop como Ubuntu, Zorin e distribuições semelhantes.

Comandos comuns necessários:

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

Comandos opcionais usados por fluxos específicos:

```bash
xdg-mime
update-desktop-database
sudo
```

`sudo` não é usado para lançadores desktop. Só é necessário para a configuração opcional de comando ao nível do sistema e para plugins que instalam pacotes do sistema.

## Início Rápido

Clone o repositório e execute o menu:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

Configure o modo portátil:

```bash
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Liste os plugins disponíveis:

```bash
ATM_LANG=en-us atm plugins list
```

Aplique PATH, links CLI e lançadores desktop:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
```

## Uso Comum da CLI

Instalar uma versão de ferramenta:

```bash
atm install go --version 1.26.2
atm install vscode --version 1.118.1
```

Alternar para uma versão instalada:

```bash
atm use go 1.26.2
atm use vscode 1.117.0
```

Remover uma versão instalada:

```bash
atm remove go 1.25.9
```

Desinstalar tudo o que é gerido por um plugin:

```bash
atm uninstall go
```

Executar em dry-run:

```bash
ATM_LANG=en-us atm --dry-run path apply
ATM_LANG=en-us atm --dry-run install vscode --version 1.118.1
```

## Menu Principal

Execute o menu interativo com:

```bash
ATM_LANG=en-us atm
```

O menu principal mostra informações do ambiente e a lista de plugins com o estado atual:

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

Ações do menu principal:

| Opção | Ação |
|---|---|
| `1` to `9` | Abre o submenu do plugin selecionado. |
| `f` | Executa Full Setup para plugins habilitados para instalação da stack. |
| `c` | Configura PATH da shell, links CLI e lançadores desktop. |
| `s` | Abre a configuração do comando ATM. |
| `p` | Lista plugins carregados. |
| `d` | Executa verificações doctor. |
| `u` | Verifica o estado de self-update. |
| `q` | Sai. |

## Submenus dos Plugins

Cada plugin possui o seu submenu. Os menus mostram a versão/estado detetado e depois ações específicas de instalação e manutenção.

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

Use este submenu para instalar distribuições JDK suportadas, instalar um tarball JDK por URL personalizado, listar versões instaladas, remover uma versão ou desinstalar todos os Java/JDK geridos pelo ATM.

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

Use este submenu para instalar versões Flutter predefinidas, escolher uma versão específica, listar versões instaladas, remover uma versão ou desinstalar completamente o Flutter.

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

Use este submenu para instalar versões Go, alternar o Go atual pela CLI, listar versões instaladas, remover uma versão ou desinstalar todos os ficheiros Go geridos pelo ATM.

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

Use este submenu para instalar versões do VS Code, alternar para uma versão já instalada, listar versões instaladas, remover uma versão ou desinstalar todos os VS Code geridos pelo ATM.

Ao alternar versões, o ATM regenera:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

Também regista:

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

Use este submenu para instalar o Android Studio, instalar a partir de um URL personalizado, listar versões instaladas, remover uma versão ou desinstalar todos os Android Studio geridos pelo ATM.

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

Use este submenu para instalar command-line tools e stacks do Android SDK, escolher versões SDK personalizadas, listar pacotes instalados, remover um pacote ou desinstalar o SDK gerido pelo ATM.

O estado do ATM prefere APIs Android numéricas estáveis em vez de codenames/previews quando ambos existem.

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

Use este submenu para instalar ou remover o conjunto de pacotes de `plugins/os_packages/packages.txt` para a distribuição Linux detetada. Para operações reais, o plugin pede `sudo` quando o ATM não está a correr como root. `--dry-run` imprime os comandos do gestor de pacotes em segurança sem pedir sudo.

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

Nesta fase, `Install Docker Engine`, `Install Docker Compose` e `Install Docker Desktop` estão implementados. O Docker Engine segue o fluxo Ansible fornecido: executar o script oficial de instalação Docker, adicionar o utilizador alvo ao grupo `docker` e ativar/iniciar o serviço Docker. O Docker Compose descarrega o binário standalone oficial mais recente do GitHub para `/usr/local/bin/docker-compose` e marca-o como executável. O Docker Desktop segue o fluxo DEB oficial do Ubuntu: descarregar `docker-desktop-amd64.deb`, executar `apt-get update` e instalar o pacote local com `apt`. Install ALL continua como placeholder para um patch posterior.

## Menu de Configuração

O menu de configuração está disponível em `s` no Menu Principal:

```text
⚙️  Setup ATM Command
------------------------------------------
1) Install Portable Mode: add ~/Apps/atm/bin to shell PATH
2) Install System Mode: create /usr/local/bin/atm
3) Show ATM command setup status
b) Back
q) Exit
```

O modo portátil é o padrão recomendado. O modo sistema só cria um link de comando; os ficheiros do projeto continuam em `~/Apps/atm`.

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

`atm path apply` atualiza ficheiros de shell do utilizador e a integração desktop dos plugins.

Por padrão, o ATM não escreve em `/etc/profile.d/atm.sh`.

Para ativar explicitamente essa escrita opcional de perfil do sistema:

```bash
ATM_PATH_WRITE_SYSTEM_PROFILE=1 atm path apply
```

Os lançadores desktop são instalados em:

```text
~/.local/share/applications
```

## Locales

Use `ATM_LANG` para selecionar um idioma:

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

Locales suportados:

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

## Documentação Pública

A documentação pública está em:

```text
docs/
```

Documentos públicos disponíveis:

```text
docs/<locale>/README-<LANG>.md
docs/<locale>/USER-MANUAL-<LANG>.md
docs/<locale>/PLUGIN-DEVELOPER-MANUAL-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CREATOR-<LANG>.md
docs/<locale>/AI-PROMPT-PLUGIN-CONVERT-<LANG>.md
```

## Estrutura do Projeto

```text
bin/atm                         Executável principal
lib/                            Bibliotecas Bash centrais
lang/                           Ficheiros de locale centrais
plugins/<plugin_id>/            Diretórios de plugins
plugins/<plugin_id>/plugin.*    Metadados, configuração e implementação do plugin
plugins/<plugin_id>/lang/       Ficheiros de locale do plugin
docs/                           Documentação pública
```

## Validação

Verificações recomendadas:

```bash
bash -n bin/atm
bash -n lib/*.sh
find plugins -name '*.sh' -print -exec bash -n {} \;
find lang -name '*.lang' -print -exec bash -n {} \;
find plugins -path '*/lang/*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run path apply
```

## Licença

Veja [LICENSE](../../LICENSE).

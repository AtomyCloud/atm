# USER-MANUAL — Manual do Usuario ATM

## 1. O que o ATM faz

O ATM instala e gerencia ferramentas de desenvolvimento no espaco do usuario.

Ferramentas principais:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Local padrao:

```text
~/Apps
```

## 2. Iniciar o ATM

Pela pasta do projeto:

```bash
cd ~/Apps/atm
ATM_LANG=pt-br bin/atm
```

Se ja estiver configurado:

```bash
atm
```

## 3. Configurar o comando atm

Modo portatil, apenas para o seu usuario:

```bash
cd ~/Apps/atm
ATM_LANG=pt-br bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Modo sistema, criando `/usr/local/bin/atm`:

```bash
cd ~/Apps/atm
ATM_LANG=pt-br bin/atm setup system
atm --version
```

Este modo pode pedir sudo.

## 4. Menu principal

O menu principal mostra modo, diretorios, idioma, plugins e acoes:

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

A ordem dos plugins pode mudar conforme `plugin.metadata`.

## 5. PATH, CLI e desktop

```bash
ATM_LANG=pt-br atm path apply
```

Configura PATH, o comando `code`, desktop launchers e o handler `vscode://`.

Depois execute:

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Instalar ferramentas

Go:

```bash
atm install go --version 1.26.2
atm use go 1.25.9
atm remove go 1.25.9
atm uninstall go
```

VS Code:

```bash
atm install vscode --version 1.118.0
atm use vscode 1.117.0
atm path apply
```

Java / JDK:

```bash
atm install java --vendor openjdk --version 26.0.1
```

Android Studio:

```bash
atm
```

Escolha Android Studio no menu e selecione uma versao.

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

Preve a acao sem alterar arquivos:

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. Solucao de problemas

`atm` nao encontrado:

```bash
source ~/.bashrc
hash -r
which atm
```

`code .` nao encontrado:

```bash
ATM_LANG=pt-br atm path apply
source ~/.bashrc
hash -r
which code
```

VS Code com icone generico:

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=pt-br atm path apply
```

Android Studio duplicado no menu:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

Cada `ATM_PLUGIN_ID` deve ser unico.

## 10. Desinstalar o ATM

```bash
atm uninstall atm
```

Se ainda nao estiver implementado:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Remova tambem o bloco PATH do ATM em `~/.bashrc`, `~/.zshrc` ou `~/.profile`.


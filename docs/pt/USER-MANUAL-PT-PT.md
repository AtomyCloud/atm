# USER-MANUAL — Manual de Utilizador ATM

## 1. O que o ATM faz

O ATM instala e gere ferramentas de desenvolvimento no espaco do utilizador.

Ferramentas principais:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Localizacao predefinida:

```text
~/Apps
```

## 2. Iniciar o ATM

Na pasta do projecto:

```bash
cd ~/Apps/atm
ATM_LANG=pt-pt bin/atm
```

Se ja estiver configurado:

```bash
atm
```

## 3. Configurar o comando atm

Modo portatil, apenas para o seu utilizador:

```bash
cd ~/Apps/atm
ATM_LANG=pt-pt bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Modo sistema, criando `/usr/local/bin/atm`:

```bash
cd ~/Apps/atm
ATM_LANG=pt-pt bin/atm setup system
atm --version
```

Este modo pode pedir sudo.

## 4. Menu principal

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
ATM_LANG=pt-pt atm path apply
```

Configura PATH, o comando `code`, lancadores desktop e o handler `vscode://`.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Instalar ferramentas

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

Para Android Studio, abra `atm`, escolha Android Studio e seleccione uma versao.

Android SDK usa:

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

## 9. Resolucao de problemas

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=pt-pt atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=pt-pt atm path apply
```

IDs duplicados de plugin:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. Desinstalar o ATM

```bash
atm uninstall atm
```

Limpeza manual:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Remova o bloco PATH do ATM em `~/.bashrc`, `~/.zshrc` ou `~/.profile`.


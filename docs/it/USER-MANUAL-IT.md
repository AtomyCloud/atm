# USER-MANUAL — Manuale Utente ATM

## 1. Cosa fa ATM

ATM installa e gestisce strumenti di sviluppo nello spazio dell'utente.

Strumenti principali:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Percorso predefinito:

```text
~/Apps
```

## 2. Avviare ATM

Dalla cartella del progetto:

```bash
cd ~/Apps/atm
ATM_LANG=it bin/atm
```

Se e gia configurato:

```bash
atm
```

## 3. Configurare il comando atm

Modalita portatile:

```bash
cd ~/Apps/atm
ATM_LANG=it bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Modalita sistema:

```bash
cd ~/Apps/atm
ATM_LANG=it bin/atm setup system
atm --version
```

Puo richiedere sudo.

## 4. Menu principale

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

L'ordine dei plugin puo cambiare in base a `plugin.metadata`.

## 5. PATH, CLI e desktop

```bash
ATM_LANG=it atm path apply
```

Configura PATH, `code`, launcher desktop e handler `vscode://`.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Installare strumenti

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

Per Android Studio, eseguire `atm`, scegliere Android Studio e selezionare una versione.

Android SDK:

```text
~/Apps/AndroidStudio/SDK
```

Flutter:

```bash
atm install flutter --version 3.41.8
atm use flutter 3.41.8
```

## 7. Plugin

```bash
atm plugins list
```

## 8. Dry run

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. Risoluzione problemi

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=it atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=it atm path apply
```

ID plugin duplicati:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. Disinstallare ATM

```bash
atm uninstall atm
```

Pulizia manuale:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Rimuovere il blocco PATH di ATM da `~/.bashrc`, `~/.zshrc` o `~/.profile`.


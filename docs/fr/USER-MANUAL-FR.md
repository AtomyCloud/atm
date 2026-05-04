# USER-MANUAL — Manuel Utilisateur ATM

## 1. Ce que fait ATM

ATM installe et gere des outils de developpement dans l'espace utilisateur.

Outils principaux:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Emplacement par defaut:

```text
~/Apps
```

## 2. Demarrer ATM

Depuis le dossier du projet:

```bash
cd ~/Apps/atm
ATM_LANG=fr bin/atm
```

Si la commande est deja configuree:

```bash
atm
```

## 3. Configurer la commande atm

Mode portable:

```bash
cd ~/Apps/atm
ATM_LANG=fr bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Mode systeme:

```bash
cd ~/Apps/atm
ATM_LANG=fr bin/atm setup system
atm --version
```

Ce mode peut demander sudo.

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

L'ordre des plugins peut changer selon `plugin.metadata`.

## 5. PATH, CLI et desktop

```bash
ATM_LANG=fr atm path apply
```

Configure PATH, `code`, les lanceurs desktop et le handler `vscode://`.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Installer des outils

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

Pour Android Studio, lancez `atm`, choisissez Android Studio et selectionnez une version.

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

## 9. Depannage

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=fr atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=fr atm path apply
```

IDs de plugin en double:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. Desinstaller ATM

```bash
atm uninstall atm
```

Nettoyage manuel:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Supprimez le bloc PATH d'ATM dans `~/.bashrc`, `~/.zshrc` ou `~/.profile`.


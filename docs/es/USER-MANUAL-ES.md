# USER-MANUAL — Manual de Usuario de ATM

## 1. Que hace ATM

ATM instala y gestiona herramientas de desarrollo en el espacio del usuario.

Herramientas principales:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Ubicacion predeterminada:

```text
~/Apps
```

## 2. Iniciar ATM

Desde la carpeta del proyecto:

```bash
cd ~/Apps/atm
ATM_LANG=es bin/atm
```

Si ya esta configurado:

```bash
atm
```

## 3. Configurar el comando atm

Modo portable:

```bash
cd ~/Apps/atm
ATM_LANG=es bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Modo sistema:

```bash
cd ~/Apps/atm
ATM_LANG=es bin/atm setup system
atm --version
```

Puede pedir sudo.

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

El orden depende de `plugin.metadata`.

## 5. PATH, CLI y desktop

```bash
ATM_LANG=es atm path apply
```

Configura PATH, `code`, lanzadores desktop y el handler `vscode://`.

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Instalar herramientas

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

Android Studio: ejecute `atm`, elija Android Studio y seleccione una version.

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

## 9. Solucion de problemas

```bash
source ~/.bashrc
hash -r
which atm
```

```bash
ATM_LANG=es atm path apply
source ~/.bashrc
hash -r
which code
```

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=es atm path apply
```

IDs duplicados:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

## 10. Desinstalar ATM

```bash
atm uninstall atm
```

Limpieza manual:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Quite el bloque PATH de ATM de `~/.bashrc`, `~/.zshrc` o `~/.profile`.


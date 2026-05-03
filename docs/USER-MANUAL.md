# USER-MANUAL — ATM User Manual

## 1. What ATM does

ATM installs and manages developer tools in your user space.

Main tools currently supported:

```text
Go
Java / JDK
VS Code
Android Studio
Android SDK
Flutter SDK
```

Default app location:

```text
~/Apps
```

## 2. Start ATM

If running from project folder:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm
```

If already configured:

```bash
atm
```

## 3. Setup ATM command

### Portable mode

Use this when you want ATM in your user account only:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
atm --version
```

Portable mode adds this to your shell PATH:

```text
~/Apps/atm/bin
```

### System command mode

Use this when you want `atm` available as `/usr/local/bin/atm`:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm setup system
atm --version
```

This creates:

```text
/usr/local/bin/atm -> ~/Apps/atm/bin/atm
```

It may ask for sudo.

## 4. Main menu

Current main menu pattern:

```text
==========================================
    🚀 ATM - Atomy Tools Modules v0.0.1
==========================================
Mode:     portable/custom/system
Base dir: /home/<user>/Apps
Cache:    /home/<user>/.cache/atm
Plugins:  /home/<user>/Apps/atm/plugins
Language: en-us
------------------------------------------
1) 🐹 Go
2) ☕ Java / JDK
3) 💻 VS Code
4) 🤖 Android Studio
5) 📦 Android SDK
6) 🦋 Flutter SDK
------------------------------------------
8) ⚡ Install Stack / Full Setup
9) 🛠️  Configure PATH, CLI & Desktop
s) ⚙️  Setup ATM Command
p) 🔌 Plugins
d) 🩺 Doctor
u) ♻️  Self-update
q) ❌ Exit
```

Plugin order may change based on plugin metadata.

## 5. Configure PATH, CLI and desktop launchers

Run:

```bash
ATM_LANG=en-us atm path apply
```

This configures:

```text
PATH entries for installed tools
~/.local/bin/code -> VS Code current CLI
Desktop launchers in ~/.local/share/applications
VS Code URL handler for vscode:// links
```

After running:

```bash
source ~/.bashrc
hash -r
which code
code --version
```

## 6. Install tools

### Go

```bash
atm install go --version 1.26.2
```

Switch version:

```bash
atm use go 1.25.9
```

Remove version:

```bash
atm remove go 1.25.9
```

Uninstall all Go versions managed by ATM:

```bash
atm uninstall go
```

### VS Code

```bash
atm install vscode --version 1.118.0
```

Switch version:

```bash
atm use vscode 1.117.0
```

Then regenerate CLI/desktop:

```bash
atm path apply
```

VS Code desktop launchers:

```text
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

### Java / JDK

Default OpenJDK:

```bash
atm install java --vendor openjdk --version 26.0.1
```

Other vendors can be selected through the menu:

```text
OpenJDK
Amazon Corretto
Eclipse Temurin
Microsoft OpenJDK
GraalVM JDK
Custom URL
```

### Android Studio

Open the menu:

```bash
atm
```

Select Android Studio, then choose one of the available versions.

Install root:

```text
~/Apps/AndroidStudio/studio_<name>_<version>
~/Apps/AndroidStudio/current
```

Desktop launcher example:

```text
~/.local/share/applications/atm-android-studio-panda_4_2025.3.4.6.desktop
```

### Android SDK

Android SDK installs into:

```text
~/Apps/AndroidStudio/SDK
```

It manages:

```text
platform-tools
emulator
platforms
build-tools
cmake
ndk
```

### Flutter

```bash
atm install flutter --version 3.41.8
```

Switch version:

```bash
atm use flutter 3.41.8
```

## 7. List plugins

```bash
atm plugins list
```

## 8. Dry run

Preview what will happen without changing files:

```bash
atm --dry-run install go --version 1.26.2
atm --dry-run path apply
```

## 9. Troubleshooting

### `atm` not found

Run:

```bash
source ~/.bashrc
hash -r
which atm
```

If still missing:

```bash
cd ~/Apps/atm
ATM_LANG=en-us bin/atm setup portable
source ~/.bashrc
hash -r
```

### `code .` not found

Run:

```bash
ATM_LANG=en-us atm path apply
source ~/.bashrc
hash -r
which code
code --version
```

Expected:

```text
/home/<user>/.local/bin/code
```

### VS Code opens with generic gear icon

Current fix:

```text
Use official filenames:
~/.local/share/applications/code.desktop
~/.local/share/applications/code-url-handler.desktop
```

Regenerate:

```bash
rm -f ~/.local/share/applications/atm-vscode*.desktop
rm -f ~/.local/share/applications/dev-vscode*.desktop
ATM_LANG=en-us atm path apply
```

Then log out/in if Zorin/GNOME still caches the old launcher.

### Android Studio appears duplicated in menu

This means duplicate plugin IDs exist.

Run:

```bash
find ~/Apps/atm/plugins -name plugin.metadata -print -exec grep -H 'ATM_PLUGIN_ID' {} \;
```

Each `ATM_PLUGIN_ID` must be unique.

## 10. Uninstall ATM

Current semantic target:

```bash
atm uninstall atm
```

If not implemented yet, manual cleanup:

```bash
rm -rf ~/Apps/atm
rm -f /usr/local/bin/atm
```

Then remove ATM PATH block from:

```text
~/.bashrc
~/.zshrc
~/.profile
```


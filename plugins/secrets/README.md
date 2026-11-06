# Secrets

ATM legacy plugin `secrets`, version `0.0.1`. Installs the official GNOME Secrets
Flatpak from Flathub into an isolated ATM user installation. Full Setup is off.
No changes to CORE are needed for discovery.

## Portability Assessment

The official [GNOME app page](https://apps.gnome.org/Secrets/) distributes Secrets
through [Flathub](https://flathub.org/en/apps/org.gnome.World.Secrets). The
[Flathub manifest](https://github.com/flathub/org.gnome.World.Secrets) builds the
source archive with Meson inside the GNOME runtime; that archive is not a ready
standalone binary. No official standalone AppImage was identified in these sources.

ATM uses Flatpak's documented `FLATPAK_USER_DIR` for an isolated installation:

```text
~/Apps/secrets/
  flatpak/     application, runtimes and repository
  bin/secrets  ATM launcher
```

`ATM_APPS_DIR` changes the base directory. Every Flatpak command uses `--user`
and the explicit installation directory, ignoring an inherited FLATPAK_USER_DIR.
Flatpak chooses the host architecture and installs the stable app/runtime refs;
ATM does not hardcode a Secrets version. Flathub lists x86_64 and aarch64 builds.

**This is not a fully self-contained portable application.** Linux must already
provide Flatpak and a working graphical session, session D-Bus and desktop
portals. Installing Flatpak itself may require administrator access through your
distribution; the plugin diagnoses its absence and does not install system packages.
The runtime download can be substantially larger than the app download alone.

Settings and application data remain under `~/.var/app/org.gnome.World.Secrets`.
KeePass vaults remain wherever the user saves them. The default and ATM Flatpak
installations use the same app ID, so application data can be shared between them.
This is installation isolation, **not separate vault/profile isolation**. Do not
assume that copying `~/Apps/secrets` also copies databases, keys, portal grants or
all application settings. Relocation to another host has not been validated.

Reference: [Flatpak command/environment documentation](https://docs.flatpak.org/en/latest/flatpak-command-reference.html).

## Menu and CLI

The Secrets menu provides Install, Update, Open, and Remove (keep data).

```bash
atm --dry-run install secrets
atm install secrets
atm install secrets --update
atm remove secrets stable
# Equivalent removal:
atm uninstall secrets

# After installation, or use Open in the Secrets menu:
"$HOME/Apps/secrets/bin/secrets"
```

Install/update/removal require ATM confirmation. Flatpak keeps its own interactive
permission and transaction review; no assume-yes flag is passed. Custom versions
and arbitrary remotes are not supported. The plugin checks only its ATM location,
not `command -v secrets`, and does not adopt an existing standard user/system app.

The launcher sets the private Flatpak location on each invocation and forwards
arguments without shell evaluation. ATM's Configure PATH action can add its bin
directory. No desktop shortcut, shell profile modification or system-wide launcher
is automatically created by this plugin. An existing unrelated launcher or linked
installation directory blocks writes rather than being overwritten.

## Removal and Data

Only the stable Secrets app in the ATM Flatpak location is uninstalled. No
`--delete-data`, `--unused`, `--all`, `--force-remove` or recursive directory removal
is used. The owned launcher is removed after uninstall verification; modified
launchers block the action. Runtimes, repository, settings, keyfiles and vaults are
retained. Neither the ATM code nor tests inspect any real password database.

The remote `atm-flathub` is added from the official Flathub `.flatpakrepo`, with
signature verification enabled by default. An existing remote with an unexpected
URL, disabled state or disabled GPG checking is refused. Updates also check the
installed origin. Flatpak remains responsible for signed deployment verification.

## Verification

```bash
bash -n plugins/secrets/plugin.sh
python3 -B -m unittest discover -s plugins/secrets/tests -p 'test_*.py'
python3 -B -m unittest discover -s tests -p 'test_plugin_i18n.py'
```

Lifecycle tests use a mock Flatpak and disposable homes. They cover scoped
install/update/open/remove, failure/cancellation, launcher protection, CLI dry-run,
manifest handling and preservation of fixture vaults and other installations.
All 11 runtime locales are included. Real GUI/install acceptance and cross-host
portability remain Pending; tests do not install Secrets or open real vaults.

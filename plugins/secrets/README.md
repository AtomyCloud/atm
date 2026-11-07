# Secrets

ATM legacy plugin `secrets`, version `0.0.1`. Installs the official GNOME Secrets
Flatpak from Flathub in ATM or Default (system) mode. Full Setup is off.
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

`ATM_APPS_DIR` changes the base directory. In ATM mode, every Flatpak command uses `--user`
and the explicit installation directory, ignoring an inherited FLATPAK_USER_DIR.
Flatpak chooses the host architecture and installs the stable app/runtime refs;
ATM does not hardcode a Secrets version. Flathub lists x86_64 and aarch64 builds.

**Default mode** uses the standard system Flatpak installation and the `flathub`
remote, equivalent to `flatpak install flathub org.gnome.World.Secrets` with
`--system` explicitly selected. Inherited `FLATPAK_USER_DIR` and
`FLATPAK_SYSTEM_DIR` overrides are removed for these commands. Flatpak may request
administrator authorization through Polkit. The plugin does not invoke sudo or
silently fall back to ATM mode if authorization fails. System changes can affect
other users of that installation. Default mode creates no ATM launcher or private
Flatpak directory; Flatpak manages its standard application exports.

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

The Secrets menu provides:

1. Install Secrets: ATM.
2. Install Secrets: Default (system Flatpak).
3. Update Secrets.
4. Open Secrets.
5. Remove Secrets (keep data).

Update, Open and Remove ask which mode to manage. Status displays both
installations independently. CLI commands without `--mode` retain ATM mode for
backward compatibility; an existing system installation is never chosen implicitly.

```bash
atm --dry-run install secrets
atm install secrets
atm install secrets --mode atm
atm install secrets --mode default
atm install secrets --update
atm install secrets --mode default --update
atm remove secrets stable
# Equivalent removal:
atm uninstall secrets
atm uninstall secrets --mode default

# After ATM-mode installation, or use Open in the Secrets menu:
"$HOME/Apps/secrets/bin/secrets"
```

Install/update/removal require ATM confirmation. Flatpak keeps its own interactive
permission and transaction review; no assume-yes flag is passed. Custom versions
and arbitrary remotes are not supported. The plugin queries the explicitly selected
Flatpak installation, not `command -v secrets`. Default mode can manage an existing
system app; installation/update requires its origin to be `flathub`. A standard
per-user installation in `~/.local/share/flatpak` is not a third managed mode.

The ATM-mode launcher sets the private Flatpak location on each invocation and forwards
arguments without shell evaluation. ATM's Configure PATH action can add its bin
directory. No desktop shortcut, shell profile modification or system-wide launcher
is automatically created by this plugin. An existing unrelated launcher or linked
installation directory blocks writes rather than being overwritten.

## Removal and Data

Only the stable Secrets app in the selected Flatpak installation is uninstalled. No
`--delete-data`, `--unused`, `--all`, `--force-remove` or recursive directory removal
is used. In ATM mode, the owned launcher is removed after uninstall verification;
modified launchers block the action. Default-mode removal does not touch that
launcher or the private installation. Runtimes, repository, settings, keyfiles and vaults are
retained. Neither the ATM code nor tests inspect any real password database.

The remote `atm-flathub` (ATM) or `flathub` (Default) is added to the selected
installation from the official Flathub `.flatpakrepo`, with
signature verification enabled by default. An existing remote with an unexpected
URL, disabled state or disabled GPG checking is refused. Updates also check the
installed origin. Flatpak remains responsible for signed deployment verification.

ATM records separate receipts, `secrets.manifest` and `secrets-default.manifest`,
so updating or removing one mode does not overwrite the other mode's receipt.
These are two scopes of the same plugin, not two separate plugins.

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

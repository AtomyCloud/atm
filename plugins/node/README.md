# Node.js Plugin

Plugin `node`, version `0.0.1`.

## Installed Origins

The status and Node menu distinguish:

- **ATM**: the active portable version under `~/Apps/node/current`.
- **User local**: `~/.local/bin/node`, including an external version-manager link.
- **System**: `/usr/bin/node`, the usual APT executable path.

Option 4 lists managed versions, both external paths and the command selected by
the current process's PATH. External executables are trusted local programs queried
with `--version`, closed stdin, and a three-second timeout (plus one-second kill
grace). Failed/nonexecutable/unexpected output is unavailable. Without the `timeout`
utility, probes are skipped. Dry-run never executes these programs; managed
versions use directory metadata. Detection does not adopt external installations,
certify package ownership, or edit their links. Interactive aliases/functions in
the parent shell can differ from the command selected in ATM's own process.

`ATM_NODE_LOCAL_BINARY`, `ATM_NODE_SYSTEM_BINARY`, and `ATM_NODE_SYSTEM_NPM` may
override probe/verification paths. These do not tell APT where to put packages.

## Install Location

Main option **8) Install Node.js (ATM / system package)** offers:

```text
1) ATM portable (default): ~/Apps/node
2) System package (APT)
```

Enter chooses ATM, `b` or EOF cancels. CLI without a location prompts only when
interactive and no version/major was specified; noninteractive use defaults to ATM.
An exact official version or major selects portable installation unless explicitly
overridden. System mode rejects `--version` and `--major` rather than ignoring them.

```bash
atm install node --location atm --version 26.2.0
atm install node --location system
atm --dry-run install node --location system
```

Portable mode preserves the existing official release archives, major-version
menus and default version. It writes only its Apps/current/cache/manifest paths.
Version switching/removal and plugin uninstall continue to manage **ATM portable
versions only**, not distribution packages or user-local files.

## System Package

Currently supported on Linux Debian/Ubuntu/Zorin with `apt-get`. Unsupported
systems fail explicitly; DNF/pacman/APK and third-party repository setup are not
implemented. The [Ubuntu package file list](https://packages.ubuntu.com/noble/armhf/nodejs/filelist)
shows the system executable path. The package name is `nodejs`, not `node`.

After an explicit confirmation (default No), ATM requests sudo authorization if
not root and runs argument arrays equivalent to:

```bash
sudo apt-get update
sudo apt-get --no-remove install nodejs npm
```

APT retains its normal transaction confirmation; ATM does not pass `-y`.
`--no-remove` refuses transactions requiring package removals. Dependencies may be
installed/upgraded, and an installation failure is not transactional rollback.
ATM does not run full-system upgrade, purge, autoremove, or add/repair repositories.
APT uses the configured repositories, which can already include third-party ones.
See the [APT manual](https://manpages.ubuntu.com/manpages/noble/man8/apt-get.8.html).

The repository determines the version; selecting system installation does **not**
promise a portable v24/v25/v26 release. After success, ATM verifies the system Node
version and npm executable presence and records a separate `node_system.manifest`.
It leaves `node.manifest`, `~/Apps/node/current`, user-local binaries, npm global
packages and shell settings untouched. This receipt does not give ATM ownership of
APT packages; use the system package manager separately for removal.

Installing a package is not switching the selected shell command. Another Node
can still precede `/usr/bin/node` on PATH. Inspect option 4 or use an absolute
path, such as `/usr/bin/node --version`; ATM never silently replaces a user-local
link or changes the parent's PATH. Global npm packages can differ across runtimes.

Dry-run prints planned commands without sudo prompts, APT/index operations,
version probes, manifests, caches or directory creation. A fresh portable dry-run
also avoids the shared downloader's directory creation.

## Validation

```bash
bash -n plugins/node/plugin.sh plugins/node/plugin.conf plugins/node/lang/en-us.lang
python3 -B -m unittest discover -s plugins/node/tests -p 'test_*.py' -v
```

Fixtures use temporary HOME/Apps/state and fake Node/npm/sudo/APT commands.
They cover selector PTY visibility, switching/removal, origins/probe failures,
location defaults/cancel/dispatch, rejected version flags, confirmed system
installation, privilege/package/verification failures, separate manifests,
preserved external files/current, and actual CLI no-write dry-run in both modes.
No real package installation or user runtime switch is performed. New labels
use en-us fallback until translations stabilize; plugin version stays `0.0.1`.

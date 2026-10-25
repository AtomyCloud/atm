# spotatui

ATM plugin `spotatui`, version `0.0.1`, metadata order `15`, Full Setup disabled.
Official website: [spotatui.com](https://spotatui.com/).

## Menu

```text
1) Install / Update latest stable
2) Install a specific version
3) Check playback dependencies
4) Change active version
5) List installed versions
6) Remove an inactive version
7) Uninstall ATM-managed spotatui
8) Show usage
b) Back
q) Exit
```

## Dependency Gate

Before downloading any release, ATM requires:

- `curl` and Python 3.9+ for metadata, downloads, verification and staging.
- `ffmpeg` and `ffprobe` on PATH for YouTube audio processing.
- `yt-dlp` on PATH with version >=2025.11.12 and detected EJS solver libraries.
  This is a compatibility floor, not a promise that every newer extractor still
  handles current YouTube changes. Keep it updated.
- Deno >=2.3.0 (preferred when available) or Node >=22.0.0, validated by version
  and yt-dlp's runtime diagnostics. Bun and QuickJS are not checked in this plugin.

Checks initialize yt-dlp without a URL, ignore its configuration/plugins, disable
its cache and Python bytecode writes, and inspect only the local diagnostics.
No music, browser cookies or account credentials are accessed. Raw debug output
is not printed or stored. The checks do not test YouTube reachability, actual
audio devices, or account/video availability.

On failure ATM stops before downloading or creating the application directory.
It prints remediation instructions and does not install system packages, invoke
sudo, overwrite dependencies, or change your shell/profile. `pipx` is not a runtime
requirement; it is one suggested way to install a current extractor:

```bash
# First pipx installation (install pipx first if absent):
pipx install 'yt-dlp[default]'
hash -r
command -v yt-dlp
yt-dlp --version

# Subsequent upgrades of that pipx installation:
pipx upgrade yt-dlp
```

Install missing FFmpeg tools through your distribution's package manager, and
Node through ATM's Node.js plugin or a supported external installation. Ensure
the intended executables are on the PATH inherited by ATM. An old APT `yt-dlp`
can coexist with a newer user-space installation, but should not take precedence.

The [yt-dlp EJS guide](https://github.com/yt-dlp/yt-dlp/wiki/EJS) documents solver
libraries and runtime requirements. The [runtime announcement](https://github.com/yt-dlp/yt-dlp/issues/15012)
establishes the 2025.11.12 compatibility floor. A passing local dependency check
cannot guarantee streaming; video restrictions/network changes can still fail.

## Installation and Ownership

Linux x86_64 and aarch64 are supported. ATM resolves the latest stable or requested
numeric version from `LargeModGames/spotatui` GitHub releases, checks the exact
official asset/checksum URLs, downloads into a versioned cache, requires SHA-256
verification, and stages only the root executable. Missing checksums, archive
links/special files/unsafe paths, duplicate binaries and existing destination
directories are refused. No remote installer is executed. These assets match the
[official installer's release naming](https://github.com/LargeModGames/spotatui/blob/main/install.sh).
Checksums are not a separate publisher-signature verification.

```text
~/Apps/spotatui/<version>/libexec/spotatui
~/Apps/spotatui/<version>/bin/spotatui
~/Apps/spotatui/<version>/shims/yt-dlp
~/Apps/spotatui/<version>/.atm-spotatui-managed
~/Apps/spotatui/current -> <version>
```

The launcher gives only its child process a scoped yt-dlp shim. That shim invokes
the detected external yt-dlp with `--js-runtimes node:/path` or
`--js-runtimes deno:/path`; the FFmpeg directory is also placed on that child's
PATH. Shell arguments and paths with spaces are preserved. Your global yt-dlp
configuration and `~/.config/spotatui` are not edited. A custom spotatui
`behavior.ytdlp_path` bypasses this shim; that explicit override remains your
responsibility.

Install/update and version switching recheck dependencies. Reusing a managed
version refreshes its launchers for the current dependency paths without
redownloading the executable. If dependencies move later, repeat install or
switch to refresh paths. `atm path apply` registers `current/bin` with the generic
ATM PATH workflow. Until then, use the absolute managed launcher; a pre-existing
`~/.local/bin/spotatui` can still be the command your shell selects.

Existing external binaries (including the website installer's `~/.local/bin/spotatui`)
are neither adopted nor removed. ATM status reports only managed versions and
does not execute the player. It writes the standard `spotatui.manifest`. Do not
use the player's self-updater to replace an ATM-managed version; use ATM's update
option instead. The upstream `behavior.disable_auto_update: true` setting can
disable self-updates; ATM does not change it for you.

Inactive-version removal protects the current version. Confirmed uninstall removes
only marked versions, the managed active link and manifest. Unmarked directories,
external installations, dependencies, caches, settings, credentials and listening
data remain. No process termination, desktop integration or automatic playback.

## CLI

```bash
atm install spotatui
atm install spotatui --version 0.46.0
atm use spotatui 0.46.0
atm remove spotatui 0.45.0
atm uninstall spotatui
atm --dry-run install spotatui
atm path apply
```

`--version=X.Y.Z` and a leading `v` are also accepted. Dependency check is menu
option 3. Install/update's dry-run skips even dependency version commands and
network requests, printing the planned checks/downloads instead. It creates no
files, directories, manifests, or caches. Removal and switching use the same
no-write dry-run contract for existing managed versions.

## Tests

```bash
bash -n plugins/spotatui/plugin.sh plugins/spotatui/plugin.conf plugins/spotatui/plugin.metadata plugins/spotatui/lang/en-us.lang
python3 -B -m unittest discover -s plugins/spotatui/tests -p 'test_*.py' -v
```

Fixtures cover dependency versions/missing EJS/runtime failures, official release
resolution, SHA-256 and unsafe archives, scoped launchers, launcher refresh/failure
cleanup, lifecycle and external-data preservation, menus, and actual CLI dry-run
with tool-call blockers and filesystem snapshots. No real application install,
dependency modification or playback occurs. New strings are en-us with local
fallback for other ATM languages; translations await stabilization.

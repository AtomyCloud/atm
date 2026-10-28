# OS Packages

ATM plugin `os_packages`, version `0.0.1`, order `7`, excluded from Full Setup.

## Native Go Backend

ATM 0.0.2 provides a schema-2/jsonl-v1 backend for Linux amd64 and arm64.
It supports `menu`, `status`, `install`, `remove` and `uninstall`. This is system
package management, not a portable application installation. There is no version
switching, full-system upgrade, automatic repository repair or Bash fallback.

Build from a checkout or use a complete Linux ATM package:

```bash
bash scripts/build_manager.sh --plugins-local --target linux-amd64
atm plugin os_packages
atm status os_packages
atm --yes plugin os_packages menu commands
atm --dry-run install os_packages
```

The menu reviews installation, removal and exact command arguments. On Ubuntu or
Zorin it also offers installation using temporary official Ubuntu sources.
Reviews bind the action to the detected OS/architecture, package-file bytes and
source mode and generated temporary-source contents. Changed inputs require a fresh review. This fingerprint
does not authenticate repositories or freeze dependency resolution.
It does not bind administrator-controlled repository files or package-manager policy.

Direct noninteractive mutations require explicit acknowledgement in addition to
CORE confirmation:

```bash
atm --yes install os_packages --accept-system-packages
atm --yes install os_packages --accept-system-packages --ubuntu-sources
atm --yes uninstall os_packages --accept-system-packages --accept-removal
```

**Removal can affect packages that predate ATM**, including `sudo`, Python, Git
and other tools in the selected list. Packages and dependencies remain owned by
the system package manager. ATM does not determine which packages it originally
introduced. Explicit target names can resolve groups or affect dependency graphs;
the review is not a package-manager simulation of every resulting change.

## Package Data And Managers

`packages.txt` remains editable data. It contains the sections `[apt-get]`, `[dnf]`,
`[yum]`, `[pacman]` and `[apk]`, with one package per line. Empty lines, full-line
comments and whitespace-delimited trailing comments are supported. Selection is
bounded to 256 entries per section and 128 bytes per name, with stable deduplication.
Unknown/duplicate sections, options, glob selectors, URLs, file packages and shell
expressions are rejected. APT trailing action overrides and dotted names are also
rejected: missing dotted names can trigger apt-get's regex fallback. This includes
otherwise valid names such as `python3.11`; install those separately for now.
See the [APT package-selection documentation](https://manpages.ubuntu.com/manpages/noble/man8/apt-get.8.html).

`ATM_OS_PACKAGES_PACKAGE_FILE` can select a clean absolute regular file; symlink
files/ancestors are refused. OS metadata is parsed as literal data, never sourced.
Detection uses exact IDs and `ID_LIKE` tokens, not substring matching. Status
reports detection and the last ATM request, not an installed-package inventory.
Status/review menus do not execute sudo or a package manager and make no network
requests. CORE dry-run does not launch the backend or write files.

APT, DNF, YUM and APK refresh indexes before installation and stop on failure.
APT installation requires version 2.2+ for strict update-error handling, prevents
removal during install and retains existing configuration through dpkg options.
Removal requests no purge or automatic unused-dependency cleanup for APT, DNF/YUM.
Pacman uses `-S --needed` without synchronizing the database or upgrading the whole
system; perform an ordinary full system upgrade separately when required. It
never uses `-Sy` or `-Syu`. Pacman removal does not request recursive removal.
APK applies its normal dependency behavior. Package availability still depends
on the distribution and configured repositories; missing packages are not skipped.

## Ubuntu And Zorin Sources

The default uses existing system repositories unchanged. `--ubuntu-sources` is
an explicit alternate mode for reviewed Ubuntu bases `jammy` and `noble` only.
Unknown/newer suites must use their configured repositories until reviewed.
Zorin's Ubuntu base codename is used; no release name is guessed.

The plugin creates a private temporary source list with official HTTPS endpoints,
`main restricted universe multiverse`, base/updates/backports/security suites,
and the existing `/usr/share/keyrings/ubuntu-archive-keyring.gpg` trust file.
amd64 uses archive/security.ubuntu.com; these reviewed arm64 suites use
ports.ubuntu.com. Architecture mappings for later suites are not assumed.

The same absolute source list is passed to both APT update and install, with
`sourceparts=/dev/null` and list cleanup disabled. Existing `/etc/apt` files and
unrelated cached indexes are not removed. ATM adds no keys or verification-disabling
options. APT's normal authentication, date, TLS and sandbox defaults apply, subject
to administrator-controlled APT configuration. Under those defaults, missing or
unreadable system keyrings cause authentication failure. Update failures stop
installation. This mode still changes
APT indexes and packages; it is not a fully isolated package database.

Unlike the legacy backend, native installation does not write an
`atm-ubuntu-<codename>.list` under `/etc/apt`. Legacy mirror overrides and shell
configuration are not imported; configure system repositories for custom mirrors.

## Privileges And Recovery

Executables are resolved only from fixed system directories, with root ownership
and non-writable executable/ancestor checks. ATM passes literal arguments and a
minimal environment; it does not run shell command strings or parse executable
configuration. Trusted package managers and their hooks can themselves run scripts.

Nonroot execution uses noninteractive `sudo -n`. Authenticate separately with
`sudo -v` when needed; timestamp and sudoers policies may still deny access.
Only the explicit `DEBIAN_FRONTEND` environment preservation is requested. Denial
does not trigger a more permissive retry. ATM never reads a password from protocol
stdin. Do not grant broad new sudo permissions simply to suppress a refusal.

The private journal `ATM_STATE_DIR/os_packages/native-operation.json` honors
CORE's effective state directory and records attempts, phases and observed results.
It is not an ownership receipt or rollback mechanism. A lock prevents concurrent
ATM operations; malformed/foreign journals and symlink paths are preserved/refused.
The journal must belong to the current user with mode 0600, in a private mode-0700
directory. Writable or foreign-owned ancestors are refused, except root-owned
sticky shared ancestors such as `/tmp`. Existing permissions are never repaired
automatically. Same-user malicious races are outside this protection boundary.
Output collection is bounded and drained so ordinary verbose output does not
interrupt a package transaction merely by filling the JSONL stream.

**Cancellation does not guarantee privileged work has stopped.** CORE stops its
backend, but elevated/detached package processes and partial changes may remain.
Cancellation or an abrupt interruption can leave a `running` journal, lock and temporary source
directory. Inspect active processes and repair the distribution's package state
before retrying. Never remove package-manager locks blindly. Preserve/archive an
unresolved ATM journal and inspect its `.native-lock` manually only after recovery;
ATM will not infer completion, erase locks or retry automatically.

## Validation And Legacy

Tests use temporary state and compiled synthetic package managers. They exercise
all manager plans, explicit consent, stale reviews, source handling, denial/failure,
journal preservation, output flooding and actual CORE cancellation. Synthetic
same-user process cleanup does not establish privileged-process cleanup guarantees.
Real package transactions, ARM64 runtime and Windows/macOS backend ports remain
Pending. No real system packages are changed by the automated suite.

The separate legacy `bin/atm` entrypoint retains `plugin.sh`/`plugin.conf`, its
sudo prompt and persistent Ubuntu source behavior. Native code does not invoke it.

```bash
go test ./plugins/os_packages/native
go test -race ./plugins/os_packages/native
bash -n plugins/os_packages/plugin.sh
```

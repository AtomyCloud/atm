# Brave Browser

Patch v29.11 supplies a native Go Linux amd64/arm64 backend using schema 2 and
JSONL-v1. CORE remains `0.0.2`, this plugin remains `0.0.1`, and Full Setup is off.
The separate legacy Bash plugin and its tests are unchanged. Native operations
never delegate to Bash/Python, shell command strings, or the legacy plugin.
Package managers still run their normal package scripts and dependencies.

## Commands And Menus

Build the declared native executable with the repository's normal local-plugin
build workflow. Open `atm plugin brave`, or use the explicit routes below.
Global CORE flags precede commands; mutations require CORE confirmation or `--yes`.

```text
atm status brave
atm plugin brave menu install
atm plugin brave menu remove
atm plugin brave menu commands
atm plugin brave menu backups
atm --yes plugin brave menu backup-profile
atm --yes plugin brave menu backup-bookmarks
atm --yes plugin brave menu restore ARCHIVE_BASENAME --sha256 HEX --accept-restore
atm --yes install brave --accept-system-packages [--plan HEX]
atm --yes remove brave --accept-system-packages --accept-removal [--plan HEX]
atm --yes uninstall brave --accept-system-packages --accept-removal [--plan HEX]
```

`install`, `remove`, and `commands` above are read-only review submenus; `backups`
lists archives and provides exact fingerprint-bound restore choices. The root
choice IDs are `install`, `remove`, `commands`, `backups`, `backup-bookmarks`.
Backup and restore choices use operation `menu` with the corresponding literal
arguments. `uninstall` aliases package removal. `use` explicitly reports unsupported:
this is a system-managed stable-channel package, not portable version switching.
No bookmarks-only restore or separate update operation is declared.

Root menu, status, review menus and archive listing never execute commands,
connect to the network or write files. Status checks standard binary paths only;
it reports unknown versions and does not infer ownership. In particular it NEVER
runs `brave-browser --version`: that wrapper can modify desktop/MIME settings.
CORE dry-run launches no backend; standalone backend dry-run also avoids writes,
network and execution. No real profile is inspected by root menu/status.

## System Packages And Trust

Native installation supports trusted `apt-get` (2.2+) or `dnf`, found only in
system directories, with root-owned non-writable executable ancestry. Commands
use literal argv, a minimal environment, no input stream and `sudo -n` when needed.
ATM never runs a `sudo -v` preflight or reads passwords. Arrange authorization
separately. Command-specific sudo policies must permit each reviewed helper.

Repository configuration is temporary, not written into shared `/etc` files.
Privileged `mktemp` creates a root-owned `/run/atm-brave-*` tree. Trusted `install`
copies public keys/configuration as `0644` and dependency sources/preferences as
`0600`; directories are `0755` so `_apt` can traverse to the public key. The user
process verifies metadata; private contents are verified via trusted elevated
`sha256sum`, never reopened unprivileged. Input copies in the user's temporary
directory are private. Normal completion cleans only these generated staging trees.

APT snapshots bounded, root-owned non-writable configured sources and preferences.
Administrator preference bytes/order are preserved, including restrictions on
Brave; ATM adds no overriding pins. Source and preference bytes,
manager, architecture, action and key pin are included in the menu plan digest and
rechecked under the operation lock. Empty files contribute no configuration.
Existing Brave source declarations are refused for separate manual review rather
than adopted or rewritten. Linked, writable, unreadable or oversized nonempty
configuration is refused; ATM never repairs it automatically.

APT refresh uses strict error handling and authenticated repository metadata with
a scoped `Signed-By` keyring. A native parser of bounded C-locale `apt-cache policy`
output verifies an official stable candidate and rejects same-version foreign
instances, unknown formats and missing candidates; installation selects that exact
version. Prohibited, foreign or ambiguous candidates stop before install, as do
refresh/signature failures. Installation
uses `--no-remove`; install/remove disable automatic removal and purge. Root APT
configuration and package scripts remain system policy, not an ATM sandbox.
See [Brave's APT source](https://brave-browser-apt-release.s3.brave.com/brave-browser.sources)
and [APT instance-selection rules](https://manpages.debian.org/trixie/apt/apt_preferences.5.en.html).

DNF uses an invocation-only `--repofrompath=atm-brave-native,...` repository,
`gpgcheck=1`, HTTPS verification, and the pinned local public key. Other repositories
receive a `brave-browser` exclusion, then that exclusion is cleared on the temporary
repository, which includes only `brave-browser`. DNF documents appended option
values and empty-value clearing for both [DNF4](https://dnf.readthedocs.io/en/latest/command_ref.html)
and [DNF5](https://dnf5.readthedocs.io/en/latest/dnf5.8.html). Repository-ID collisions,
unsupported options or any failed transaction stop; there is no DNF5-to-DNF4 retry.
`repo_gpgcheck=0` matches the [official RPM repository](https://brave-browser-rpm-release.s3.brave.com/brave-browser.repo):
RPM package signatures are required, but repository metadata has HTTPS transport
protection, not an asserted OpenPGP signature. Existing RPM trust still applies;
DNF may import the public key into its system trust database.

Removal targets only `brave-browser`, never deletes repository/key files itself,
never refreshes indexes, never purges, and never removes profiles/backups. DNF uses
cache-only removal with dependency autoremove disabled. System packages remain
system-owned, including pre-existing installations explicitly selected for removal.
Package scripts, dependencies and caches can change; no transactional rollback or
guarantee about third-party/root package scripts is claimed.

### Signing Key Review: 2026-10-28

Key acquisition uses fixed HTTPS URLs, a 1 MiB bound, a 30-second timeout, no
redirects and exact reviewed SHA-256 byte pins. Changed keys require maintainer
review; ATM does not silently accept rotation. The published release keys were
also compared with [Brave's signing-keys page](https://brave.com/signing-keys/).
This is HTTPS-origin bootstrap with a pinned reviewed snapshot, not an independent
cryptographic trust anchor or automated freshness/revocation service.

- [APT binary keyring](https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg):
  `c85e85aa3d1783ffaa649ee8dbbc22af7f87192d304602d37e3018226b394788`
- [RPM armored keys](https://brave-browser-rpm-release.s3.brave.com/brave-core.asc):
  `bf978bd03e49f6006568d99f620763bf43a6455f6209dff791f52ebc6239f738`

Both contain the same three primary release-key fingerprints published on that page:

```text
DBF1A116C220B8C7164F98230686B78420038257
47D32A74E9A9E013A4B4926C68D513D36A73CD96
B2A3DCA350E67256740DF904DE4EC67BE4B0DCA0
```

## Profile Data

The default source is `~/.config/BraveSoftware/Brave-Browser`. The default archive
directory is the existing `AppsDir/backups/brave` (normally `~/Apps/backups/brave`).
`ATM_BRAVE_PROFILE_DIR` and `ATM_BRAVE_BACKUP_DIR` override these with clean absolute
paths. Profile, backup, state, ATM runtime and AppsDir roots must not overlap in a
way that could archive or replace operational data. Linked/writable ancestry is
refused. Native code does not honor legacy package-name/repository/key overrides.

Close Brave before backup or restore. Native `/proc` inspection is UID-aware and
checks actual executable, command line, profile arguments and visibility. Any
Singleton lock/socket/cookie, hidden process visibility, unknown denied executable
or ambiguous process metadata refuses the operation; ATM never kills processes or
removes locks. Only matching `ssh-agent`/`gpg-agent` identities have a narrow
EACCES/EPERM fallback. This is not protection against hostile same-user process
identity spoofing or a substitute for keeping the browser closed. Checks repeat
before publication; an unrelated nondumpable process can still require manual
resolution. There is no production guard bypass.

Full backups include the complete regular-file/directory tree under a canonical
`Brave-Browser/` archive root. Bookmarks backups include only
`Default/Bookmarks` and `Profile [0-9]+/Bookmarks`, including Profile 0 and Profile 12.
Nonmatching profiles and missing Bookmarks are skipped. Empty bookmark selections
create no archive; full profiles without regular files are refused.
Files are native Brave JSON, not HTML exports. Both archive types are unencrypted,
private `0600` tar/gzip files with distinct `brave-profile-` / `brave-bookmarks-`
names, timestamp and unique suffix. Publication never overwrites an older archive.
The source inventory, identities and copied-file hashes are rechecked before
publication. Symlinks and special files are refused, including transient profile
locks: unlike legacy bookmark backup, native code never dereferences symlinks.

Restore requires an explicitly selected full-profile basename and its SHA-256.
It never auto-selects the newest archive or selects a bookmarks archive. The whole
stream is validated before staging, then extracted into a private sibling staging
tree and fingerprint-checked again. Traversal, duplicate/conflicting entries,
links, specials, extended attributes, unsupported PAX metadata, invalid gzip CRC,
truncation and trailing/concatenated streams are refused. Safe PAX paths/timestamps
support Unicode and long names. Legacy full archives are accepted only after the
same checks, with one top-level `Brave-Browser` or the configured profile basename;
legacy archives containing symlinks/locks/special files are not compatible.

The existing current-user-owned profile is moved to a unique sibling
`.before-restore.*` recovery path, never recursively overwritten. Replacement uses
no-overwrite renames and identity checks. Safe failures roll the old tree back;
conflicts preserve staging/recovery for inspection. Successful recovery copies are
never automatically deleted. Restored files/directories use private `0600`/`0700`
modes, not original ACLs, ownership or extended attributes. Browser encrypted
passwords/cookies can depend on external Secret Service/keyrings; ATM does not
export keyrings, decrypt secrets or promise portability.

## Bounds And Interrupted Work

Archive limits: 1 GiB compressed, 4 GiB regular-file payload, 512 MiB per file,
100,000 logical headers including the root, 64 path separators and 1024-byte entry
names. Decompression also has a separate overhead bound; GNU tar zero padding is
limited to 1 MiB. Listing allows 512 directory entries, 48 matching archives and
2 GiB total compressed input. Oversized collections require separate manual
archival, not an implicit limit override. Source/preference snapshots each allow
128 fragments, 1 MiB per file and 4 MiB aggregate.

The private attempt journal is `StateDir/brave/native-operation.json`, with a
`.native-lock` directory. It records attempts, not installation ownership. A
running journal or retained lock blocks retries. On interruption, inspect the
journal's target, staging, input and recovery paths, active processes and package
state before manual recovery. Privileged descendants may outlive ATM; cancellation
retains their inputs/staging and does not prove termination or rollback. A crash
between profile renames may leave the old tree only at its recovery path. Preserve
it and resolve conflicts manually; never blindly delete a lock or retry. No
hostile-same-user, hostile-root or power-loss atomicity guarantee is made.

## Validation And Acceptance

```bash
go test ./plugins/brave/native -count=1
go test -race ./plugins/brave/native -count=1
go vet ./plugins/brave/native
bash -n plugins/brave/plugin.sh
bash plugins/brave/tests/test_bookmarks.sh
```

Tests use synthetic private profiles and mocked privileged commands; no real
browser/package installation, profile, system repository or signing secret is
changed. Independent CLI checks cover offline/no-exec/no-write routes and consent.
An unknown nondumpable same-UID process on the development host blocked successful
CLI archive lifecycle testing; successful lifecycle and failure cases are covered
by injected Go fixtures instead. Real APT/DNF transactions, browser profiles,
Linux ARM64 runtime and other-host implementations/acceptance remain Pending.
Cross-compilation is not runtime acceptance. New native labels are en-us only.

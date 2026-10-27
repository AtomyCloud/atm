# Signed Linux Release Test

Local release candidate: **ATM 0.0.2**, built from clean commit
`6c8b9be3502be651b0dbfe446429d02c9734d04f` on 2026-10-27.
Linux amd64 and arm64 packages were signed through the maintainer's existing
ssh-agent. No private key was opened/exported, and nothing was published to GitHub.
The packages still have production enrollment/update/recovery disabled.

Checkpoint: the maintainer accepted this test and subsequently authorized enabling
the routes in source (Patch v27.9). These existing signed artifacts were NOT rebuilt
or overwritten. This guide intentionally continues to expect their original false
capability. New source builds report true; checking trust is still read-only.

Signature fingerprint:

```text
SHA256:jNhv8Ehni4ENGvQaar4/l8vQQPHcVZDnCmcmTvkFXVM
```

The signature is raw Ed25519 over the domain-prefixed exact manifest bytes, not
an SSHSIG envelope. Both signatures and all 253 indexed files per package passed
the Go verifier. ARM64 was built and inspected, not executed on this amd64 host.

## Local Artifacts

The ignored directory `binary/v0.0.2-6c8b9be/` contains:

```text
atm-0.0.2-linux-amd64.tar.gz
atm-0.0.2-linux-amd64.tar.gz.sha256
atm-0.0.2-linux-arm64.tar.gz
atm-0.0.2-linux-arm64.tar.gz.sha256
signatures-linux-amd64/atm-0.0.2-linux-amd64.manifest.json
signatures-linux-amd64/atm-0.0.2-linux-amd64.manifest.sig
signatures-linux-arm64/atm-0.0.2-linux-arm64.manifest.json
signatures-linux-arm64/atm-0.0.2-linux-arm64.manifest.sig
qa/atm-verifier-linux-amd64
```

Archive SHA-256 values:

```text
41224e74abcf9ec33cd4f89cc37e1400cbbe2f07c512a2ff6fc78c5336870089  atm-0.0.2-linux-amd64.tar.gz
5262ca1d5128b3e602accd7ba1cd46cc0b589879581de1e45cb17fa05a765a2f  atm-0.0.2-linux-arm64.tar.gz
```

`qa/atm-verifier-linux-amd64` is a separately built, locally trusted verifier
from the same reviewed source, not a signed publication asset. Do not trust a
verifier merely because it arrived alongside a remote archive. For another host,
build a native verifier from an independently trusted source checkout, or use a
previously trusted compatible ATM binary. Pass its absolute path as argument 2.

## Human Test

From the repository root, in a real terminal:

```bash
bash scripts/test_linux_release.sh
```

For automated checks without opening the TUI:

```bash
bash scripts/test_linux_release.sh --no-tui
```

For a different release directory or a native verifier on ARM64:

```bash
bash scripts/test_linux_release.sh /absolute/release/directory /absolute/trusted/atm
```

The script detects Linux architecture, verifies the signature **before extraction**,
rejects a modified copy of the manifest, extracts into a new private temporary
directory and tests the packaged executable. HOME, XDG and ATM settings are isolated
inside that test directory. The script is a child process: your terminal's HOME
and PATH do not change. No sudo, shell profile edits, app installation, network
discovery, production update or automatic deletion is performed. The directory is
retained and printed for inspection. Do not `source` this script.

Expected checks:

- Version is `atm 0.0.2`; commit is `6c8b9be3502be651b0dbfe446429d02c9734d04f`.
- Signing identity matches above, and `replacement_enabled` is **false**.
- Package inspection reports 253 verified files; modified manifest is rejected.
- Doctor shows temporary paths and production update blocked.
- Plugins are listed; application backends can remain legacy or unavailable.
- CORE update, plugin integration and missing-root recovery produce dry-run plans.
- In the TUI, exercise navigation, mouse/keyboard, Plugins/CORE menus and Cancel.
  All operations remain dry-run. Exit with `q`.

This original script does not test an update/rollback between versions. Its
candidate is 0.0.2; updates require a strictly newer stable version. Source route
activation was separately approved in v27.9. GitHub publication and real installed
copy updates remain separate authorizations.

## Two-Version Acceptance

Patch v27.10 adds opt-in automated acceptance of a private, officially signed
Linux candidate: **0.0.2 -> 0.0.3 -> 0.0.2**. The maintainer explicitly authorized
signing this local QA candidate. It is not a published release or a repository
version bump. The production key was used through ssh-agent, without opening or
exporting a private-key file. A separate temporary source repository records the
exact version-only QA build; the original source/history remains unchanged.

On Linux amd64, the external compiled CLI enrolled the baseline, applied the full
candidate, executed each installed binary to check its version/commit, and recovered
the exact enrolled baseline. The test checks signature/consent/version refusals,
preserves unrelated data/configuration, retains post-upgrade edits in quarantine,
and runs the candidate's doctor, plugin listing and dry-run CORE route without a
shell in PATH. Four update interruption points and two recovery interruption points
also passed with real signatures and subprocess exits. External recovery works
even when the active installation root is absent.

These are controlled process interruptions, not power-loss durability guarantees.
Automated Linux amd64 acceptance is not human TUI acceptance, all-platform approval
or permission to deploy. Linux ARM64, Windows and macOS runtime tests remain Pending.

To repeat with the reviewed private artifacts, build a trusted updater from the
approved source and provide absolute paths. Never download an untrusted verifier
alongside an archive and assume it is safe. Candidate directory layout:

```text
atm-0.0.3-linux-amd64.tar.gz
signatures/atm-0.0.3-linux-amd64.manifest.json
signatures/atm-0.0.3-linux-amd64.manifest.sig
```

From the repository root:

```bash
export ATM_TEST_SIGNED_RELEASE_DIR="$PWD/binary/v0.0.2-6c8b9be"
export ATM_TEST_SIGNED_UPGRADE_DIR="/absolute/private/candidate"
export ATM_TEST_UPDATER_BIN="/absolute/trusted/atm"
go test ./internal/cli ./internal/coreupdate \
  -run '^TestSigned(ReleaseCLIUpgrade|UpgradeCrashRecovery)$' -count=1 -v
```

The tests skip when the candidate variable is absent; a skip is not acceptance.
They verify inputs before extraction, operate only in fresh temporary roots, check
that original artifacts remain unchanged, and delete their temporary installations
on completion. They do not sign, publish, install apps or change personal profiles.
The supplied artifacts/updater are not deleted. Keep the QA candidate private:
its signature is production-valid, not a fixture key or a sandbox restriction.

## Reproducing Signatures

`scripts/sign_release.py` is optional maintainer tooling using Python's installed
Paramiko and cryptography libraries. These are not dependencies of the native CORE.
It invokes a trusted build of `tools/coremanifest` before signing, selects only
the embedded approved public key from ssh-agent, verifies the returned signature
and refuses existing output directories. It never loads a private key file.

Build packages from the reviewed clean revision first, then:

```bash
go build -o /tmp/atm-coremanifest ./tools/coremanifest
python3 scripts/sign_release.py --yes \
  --preparer /tmp/atm-coremanifest \
  --archive /absolute/path/atm-0.0.2-linux-amd64.tar.gz \
  --version 0.0.2 --commit 6c8b9be3502be651b0dbfe446429d02c9734d04f \
  --arch amd64 --out /absolute/path/new-signatures-linux-amd64
```

Do not replace the signing identity with a new fixture key, and do not send a
private key or passphrase through chat. Signing is a maintainer action, never part
of ordinary installation or CI unless separately authorized.

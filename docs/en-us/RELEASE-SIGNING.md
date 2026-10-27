# ATM Release Signing

Status: signed CORE enrollment/update/recovery are enabled in source (Patch v27.9)
after the maintainer validated the signed Linux test and explicitly authorized code
activation on 2026-10-27. Every mutation still requires confirmation and a valid
signature/baseline. No update runs merely by checking, verifying or starting ATM.
The original signed 0.0.2 artifacts from commit 6c8b9be remain unchanged and retain
their disabled gate. No new release was published or real installation updated.
No signing secrets or CI signing jobs were provisioned.

## Approved Identity

```text
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE19PySbE0RCjKYuFUllXU4meRvy08V0uPaf/rReN6qM atomy@atomy.cloud
SHA256:jNhv8Ehni4ENGvQaar4/l8vQQPHcVZDnCmcmTvkFXVM
```

The public key is safe to distribute. Its SSH comment is descriptive, not a
security check. ATM parses the SSH wire structure, extracts the 32-byte Ed25519
public key, and checks its embedded fingerprint. It never fetches a replacement
trust key from the release being verified. A future key rotation needs an explicit
authenticated transition/recovery policy, not an environment-variable override.

## Full-Package Format

`compiled-core-package-v1` binds schema, stable version, clean source commit,
OS/architecture, official asset URL, archive SHA-256/size/mode and the SHA-256 of
the exact inner `release.json`. The latter indexes every payload file. Only the
five existing Linux, Windows and macOS package targets are supported.

The Ed25519 signature covers these exact bytes:

```text
UTF-8("ATM compiled CORE package manifest v1") || 0x00 || exact manifest.json bytes
```

The detached `manifest.sig` is exactly 64 raw signature bytes. It is NOT an
OpenSSH SSHSIG, OpenPGP, base64 or hex file. `ssh-keygen -Y sign` produces a
different envelope and cannot be used directly with this verifier. The SSH
public-key representation does not determine the signature file format.
Do not copy private keys into ATM, this repository, documentation or chat.

## Offline Preparation

Build and review the normal runtime package from a clean source revision first.
The preparation tool does not build, sign, publish, upload or install anything.
Example placeholders below must be replaced with the actual release values:

```bash
go run ./tools/coremanifest \
  --archive /path/to/atm-VERSION-linux-amd64.tar.gz \
  --version VERSION --commit COMMIT_SHA --os linux --arch amd64 \
  --out /path/to/new-signing-output
```

It validates the complete archive before emitting a manifest and domain-prefixed
signing input. Existing output must not be overwritten. A separately approved
signer must produce the raw signature using the corresponding private key; the
tool intentionally does not load private keys. Preparing a manifest is not signing
it and does not prove that the archive was built from the claimed commit.

```bash
atm updates trust
atm updates verify-package /path/to/manifest.json /path/to/manifest.sig /path/to/archive.tar.gz
```

Verification is read-only and host-specific. Fixture signatures from tests are
not accepted by the production CLI. The local Linux amd64/arm64 candidates were
signed through the approved key already loaded in the maintainer's ssh-agent,
without opening/exporting private-key files. The Go verifier authenticated both
signatures and complete payloads. These candidates have not been published to
GitHub or installed over a real ATM. See the [Linux human test](LINUX-SIGNED-RELEASE-TEST.md).

## Discovery And Installed Identity

Discovery uses only the latest stable release endpoint of `AtomyCloud/atm`.
For each target, publish these separately reviewed assets when authorized:

```text
atm-VERSION-OS-ARCH.manifest.json
atm-VERSION-OS-ARCH.manifest.sig
atm-VERSION-OS-ARCH.tar.gz  (Windows: .zip)
```

The metadata and raw signature bind the exact archive; metadata from a release
listing alone is not trusted. Network reads are bounded, HTTPS redirects are
restricted, and no authentication token or cookies are sent. A valid signature
does not prove freshness: the check reports freshness as unknown. Automatic
replacement requires a strictly greater stable version and refuses same-version
identity conflicts and downgrades. Key rotation/revocation is not automated.

Native baseline/enrollment and replacement require a signed package matching the
entire indexed installation. An unregistered installation is not adopted from a
version string or its own `release.json`. Source checkouts, changed owned files
(including bundled plugins/configuration), unsafe links/hardlinks, unowned-path
collisions and pending component transactions refuse replacement. Unknown regular
files/directories are preserved. The signed installed index authenticates bytes;
it does not record each historical file mode. Supported modes are validated and
the prepared tree is rechecked before mutation. File contents, ordinary permissions
and empty directories are preserved; ACLs, extended attributes and timestamps are
not copied. Pre-publication crash reservations may remain for manual inspection.

Staging occurs beside the installation, followed by two directory renames with
a sibling journal. This is recoverable, not a single globally atomic operation
or a power-loss guarantee. Recovery verifies the original snapshot and retains
the displaced candidate, including edits, instead of deleting it. Completed
transaction history is retained. Do not manually remove journals or locks.
Stop other ATM sessions before updating; this is not a hostile-same-user sandbox.

On Windows, apply/recovery requires a trusted executable outside the target
installation. Automatic external-helper handoff is not implemented. Open-file
rename failures must be resolved and recovered, not bypassed. On every platform,
keep a trusted external copy: interruption between renames can leave the root
temporarily absent. Independent `recover-core` does not load runtime/config/plugins.

Independent plugin update receipts are preserved, not silently rebased after a
full-package change. A changed plugin may consequently be reported as modified
by plugin-only checks. Review it and use explicit `updates verify ID COMMIT`
against the corresponding official commit to establish a new plugin baseline.
Existing independently updated/edited owned plugins block full-package updates;
no automatic merge, force overwrite or rollback of those edits is provided.

## Remaining Acceptance

- The local signed Linux dry-run test was accepted; CI signing is not configured.
- New enabled release build/publication and real installed-copy operation are separate actions.
- Actual signed upgrade between distinct stable versions and recovery acceptance.
- Independent freshness/key rotation/revocation policy.
- Actual target-platform and Windows file-in-use/external-updater acceptance.
- User validation of conflict handling and independent plugin receipt rebaselining.

Checksums and GitHub commit signatures do not replace package signatures. A valid
package signature authenticates these bytes, not their safety, freshness or
source-to-binary reproducibility. Release publication and real installation remain
separate authorizations.

# ATM Release Signing

Status: approved public identity and offline full-package verification. Automatic
installation/replacement is not enabled. No signing secrets or CI signing jobs
are provisioned by this change. Migration version remains 0.0.2.

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
not accepted by the production CLI. No authentic release has been signed or
verified with the maintainer's private key during this implementation.

## Remaining Production Gates

- An approved signing process and an actual signed release.
- Authenticated release discovery, freshness and anti-rollback policy.
- Trusted installed baseline and ownership receipts for complete packages.
- Local-data/configuration preservation and conflicts with independent plugin updates.
- Recoverable full-package replacement and Windows in-use handling.
- Independent recovery, key rotation/revocation and real-platform acceptance.

Checksums and GitHub commit signatures do not replace package signatures. A valid
package signature authenticates these bytes, not their safety, freshness or
source-to-binary reproducibility. Release publication and real installation remain
separate authorizations.

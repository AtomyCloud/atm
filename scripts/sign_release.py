#!/usr/bin/env python3
"""Sign a validated ATM Linux package through ssh-agent, never key files.

Maintainer tooling only; this is not a runtime CORE dependency.
Requires locally installed paramiko and cryptography.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile

import paramiko
from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.serialization import load_ssh_public_key

PUBLIC_KEY = b"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE19PySbE0RCjKYuFUllXU4meRvy08V0uPaf/rReN6qM"
FINGERPRINT = "SHA256:jNhv8Ehni4ENGvQaar4/l8vQQPHcVZDnCmcmTvkFXVM"
DOMAIN = b"ATM compiled CORE package manifest v1\x00"


def signed_bytes(raw, agent):
    blob = base64.b64decode(PUBLIC_KEY.split()[1], validate=True)
    fingerprint = "SHA256:" + base64.b64encode(hashlib.sha256(blob).digest()).decode().rstrip("=")
    if fingerprint != FINGERPRINT:
        raise ValueError("embedded signing identity mismatch")
    matches = [key for key in agent.get_keys() if key.asbytes() == blob]
    if len(matches) != 1:
        raise ValueError("approved Ed25519 key must be loaded exactly once in ssh-agent")
    payload = DOMAIN + raw
    # SSH agent signs these exact bytes. ssh-keygen -Y sign instead signs an
    # SSHSIG envelope, which is deliberately not ATM's raw signature format.
    response = matches[0].sign_ssh_data(payload, algorithm="ssh-ed25519")
    message = paramiko.Message(bytes(response))
    algorithm, signature = message.get_text(), message.get_binary()
    if algorithm != "ssh-ed25519" or len(signature) != 64 or message.get_remainder():
        raise ValueError("invalid Ed25519 agent signature envelope")
    load_ssh_public_key(PUBLIC_KEY).verify(signature, payload)
    return signature


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def sign(args):
    if not args.yes:
        raise ValueError("signing requires explicit --yes")
    archive = Path(args.archive).absolute()
    output = Path(args.out).absolute()
    if archive.is_symlink() or not archive.is_file():
        raise ValueError("archive must be a regular file")
    if output.exists() or output.is_symlink():
        raise ValueError("output must not already exist")
    if output.parent.resolve(strict=True) != output.parent:
        raise ValueError("symlinked output parent refused")
    # The trusted Go preparer validates archive structure, identity, complete
    # payload hashes, binary format, scope and fixed official source before signing.
    with tempfile.TemporaryDirectory(prefix="atm-release-sign-") as temporary:
        prepared = Path(temporary).resolve() / "prepared"
        subprocess.run([
            str(Path(args.preparer).resolve(strict=True)),
            "--archive", str(archive), "--version", args.version,
            "--commit", args.commit, "--os", "linux", "--arch", args.arch,
            "--out", str(prepared),
        ], check=True, timeout=180)
        raw = (prepared / "manifest.json").read_bytes()
        if not 0 < len(raw) <= 16384:
            raise ValueError("invalid manifest size")
        manifest = json.loads(raw)
        if (prepared / "manifest.signing-input").read_bytes() != DOMAIN + raw:
            raise ValueError("preparer signing input mismatch")
        if manifest["archive"]["sha256"] != digest(archive):
            raise ValueError("archive changed after preparation")
        previous_timeout = socket.getdefaulttimeout()
        socket.setdefaulttimeout(30)
        try:
            agent = paramiko.Agent()
            try:
                signature = signed_bytes(raw, agent)
            finally:
                agent.close()
        finally:
            socket.setdefaulttimeout(previous_timeout)
        if manifest["archive"]["sha256"] != digest(archive):
            raise ValueError("archive changed during signing; no signature published")
        stem = f"atm-{args.version}-linux-{args.arch}"
        output.mkdir(mode=0o700)
        # Exclusive creation never overwrites an existing signature or manifest.
        for name, data in ((stem + ".manifest.json", raw), (stem + ".manifest.sig", signature)):
            with (output / name).open("xb") as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
        print(json.dumps({"directory": str(output), "fingerprint": FINGERPRINT,
                          "archive_sha256": manifest["archive"]["sha256"],
                          "commit": args.commit, "version": args.version,
                          "platform": "linux/" + args.arch, "signature_bytes": len(signature)}, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("preparer", "archive", "version", "commit", "out"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--yes", action="store_true")
    try:
        sign(parser.parse_args())
    except (OSError, ValueError, subprocess.SubprocessError, paramiko.SSHException, InvalidSignature) as error:
        parser.exit(1, f"Signing failed: {error}\n")


if __name__ == "__main__":
    main()

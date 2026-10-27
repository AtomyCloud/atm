"""Offline signer tests; no real ssh-agent or maintainer private key is used."""
import argparse
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

try:
    import paramiko
    from cryptography.exceptions import InvalidSignature
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
except ImportError as error:
    raise unittest.SkipTest("optional signing-tool tests require paramiko and cryptography") from error

SPEC = importlib.util.spec_from_file_location("sign_release", Path(__file__).resolve().parents[1] / "scripts/sign_release.py")
signer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(signer)


class AgentKey:
    def __init__(self, key, algorithm="ssh-ed25519", corrupt=False, extra=b""):
        self.key, self.algorithm, self.corrupt, self.extra = key, algorithm, corrupt, extra
        self.payload = None

    def asbytes(self):
        return signer.base64.b64decode(self.key.public_key().public_bytes(Encoding.OpenSSH, PublicFormat.OpenSSH).split()[1])

    def sign_ssh_data(self, payload, algorithm):
        self.payload = payload
        signature = self.key.sign(payload)
        if self.corrupt:
            signature = b"x" * 64
        message = paramiko.Message()
        message.add_string(self.algorithm)
        message.add_string(signature)
        return message.asbytes() + self.extra


class Agent:
    def __init__(self, keys):
        self.keys = keys

    def get_keys(self):
        return self.keys


class SigningTests(unittest.TestCase):
    def fixture(self, **kwargs):
        key = Ed25519PrivateKey.generate()
        public = key.public_key().public_bytes(Encoding.OpenSSH, PublicFormat.OpenSSH)
        blob = signer.base64.b64decode(public.split()[1])
        fingerprint = "SHA256:" + signer.base64.b64encode(signer.hashlib.sha256(blob).digest()).decode().rstrip("=")
        self.enterContext(patch.object(signer, "PUBLIC_KEY", public))
        self.enterContext(patch.object(signer, "FINGERPRINT", fingerprint))
        return AgentKey(key, **kwargs)

    def test_exact_domain_and_signature(self):
        key = self.fixture()
        raw = b'{"fixture":true}\n'
        signature = signer.signed_bytes(raw, Agent([key]))
        self.assertEqual(key.payload, signer.DOMAIN + raw)
        self.assertEqual(len(signature), 64)
        key.key.public_key().verify(signature, signer.DOMAIN + raw)
        with self.assertRaises(InvalidSignature):
            key.key.public_key().verify(signature, raw)

    def test_no_key_and_wrong_key(self):
        self.fixture()
        for keys in ([], [AgentKey(Ed25519PrivateKey.generate())]):
            with self.assertRaisesRegex(ValueError, "approved Ed25519 key"):
                signer.signed_bytes(b"fixture", Agent(keys))

    def test_duplicate_key(self):
        key = self.fixture()
        with self.assertRaisesRegex(ValueError, "exactly once"):
            signer.signed_bytes(b"fixture", Agent([key, key]))

    def test_bad_algorithm(self):
        key = self.fixture(algorithm="ssh-rsa")
        with self.assertRaisesRegex(ValueError, "envelope"):
            signer.signed_bytes(b"fixture", Agent([key]))

    def test_trailing_envelope(self):
        key = self.fixture(extra=b"trailing")
        with self.assertRaisesRegex(ValueError, "envelope"):
            signer.signed_bytes(b"fixture", Agent([key]))

    def test_bad_signature(self):
        key = self.fixture(corrupt=True)
        with self.assertRaises(InvalidSignature):
            signer.signed_bytes(b"fixture", Agent([key]))

    def test_missing_consent(self):
        with self.assertRaisesRegex(ValueError, "explicit --yes"):
            signer.sign(argparse.Namespace(yes=False))

    def test_existing_output_refused_before_preparer_or_agent(self):
        with tempfile.TemporaryDirectory() as root:
            archive = Path(root) / "archive"
            archive.write_bytes(b"fixture")
            with patch.object(signer.subprocess, "run") as run:
                with self.assertRaisesRegex(ValueError, "already exist"):
                    signer.sign(argparse.Namespace(yes=True, archive=str(archive), out=root))
                run.assert_not_called()


if __name__ == "__main__":
    unittest.main()

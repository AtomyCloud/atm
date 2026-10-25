import io
import os
from pathlib import Path
import shlex
import subprocess
import tarfile
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[3]


class ImageCliTests(unittest.TestCase):
    def test_actual_cli_dry_runs_do_not_call_docker_or_write(self):
        with tempfile.TemporaryDirectory(prefix="atm-docker-image-cli-") as temp:
            root = Path(temp)
            home = root / "home"
            home.mkdir()
            tools = root / "tools"
            tools.mkdir()
            docker = tools / "docker"
            docker.write_text("#!/usr/bin/env bash\nprintf 'UNEXPECTED_DOCKER_CALL\\n' >&2\nexit 99\n")
            docker.chmod(0o700)
            archive = root / "sample archive.tar"
            with tarfile.open(archive, "w") as output:
                member = tarfile.TarInfo("manifest.json")
                member.size = 2
                output.addfile(member, io.BytesIO(b"[]"))
            env = {k: v for k, v in os.environ.items() if not k.startswith("ATM_")}
            env.update(HOME=str(home), ATM_APPS_DIR=str(home / "Apps"),
                       ATM_CONFIG_DIR=str(root / "config"), ATM_CACHE_DIR=str(root / "cache"),
                       ATM_STATE_DIR=str(root / "state"), ATM_LANG="en-us", TERM="xterm",
                       PATH=str(tools) + os.pathsep + env.get("PATH", os.defpath))

            def snapshot():
                return {str(p.relative_to(root)): p.read_bytes() if p.is_file() else None
                        for p in root.rglob("*")}

            before = snapshot()
            commands = [
                (["export", "--image", "busybox:latest"], None),
                (["export", "--all"], None),
                (["restore", "--input", str(archive)], None),
                (["restore-all", "--directory", str(root)], None),
                (["menu"], "2\n\nxb\n"),
            ]
            for args, input in commands:
                with self.subTest(args=args):
                    result = subprocess.run(["bash", str(REPO / "bin/atm"), "--dry-run", "install",
                                             "docker", "--images", *args],
                                            input=input, env=env, cwd=REPO, capture_output=True,
                                            text=True, timeout=30)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn("DRY-RUN:", result.stdout)
                    self.assertNotIn("UNEXPECTED_DOCKER_CALL", result.stdout + result.stderr)
                    if args[0] in ("export", "menu"):
                        self.assertIn(str(home / "Apps/Backups/docker"), result.stdout)
                    else:
                        command = next(line.removeprefix("DRY-RUN: ") for line in result.stdout.splitlines()
                                       if line.startswith("DRY-RUN: docker image load "))
                        self.assertIn(str(archive), shlex.split(command))
                    self.assertEqual(before, snapshot())


if __name__ == "__main__":
    unittest.main()

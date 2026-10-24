# Trivy

ATM plugin ID: `trivy`. Plugin version: `0.0.1`. Choose a portable Linux binary
or the official `aquasec/trivy` Docker image. Both can coexist; Docker installation
does not replace the binary or create a Docker-backed `trivy` wrapper.

Official references:

- [Getting started](https://trivy.dev/docs/latest/getting-started/)
- [Installation](https://trivy.dev/docs/latest/getting-started/installation/)
- [Latest release](https://github.com/aquasecurity/trivy/releases/latest/)

## Menu

Select **Trivy** from ATM's main menu:

1. Install / update binary (latest stable)
2. Install / update Docker image (latest stable)
3. Install a specific version (then choose binary or Docker)
4. Use installed binary version
5. List installed binary versions
6. Remove binary version
7. Remove recorded Docker image tag
8. Uninstall ATM-managed Trivy
9. Show usage commands

The installation command also prompts for binary or Docker when no method is
provided. Full Setup is disabled for this plugin.

## CLI

```bash
atm install trivy
atm install trivy --method binary
atm install trivy --method docker
atm install trivy --method binary --version 0.76.1
atm install trivy --method=docker --version=0.76.1
atm use trivy 0.76.1
atm remove trivy 0.75.0
atm uninstall trivy
```

`latest` is resolved dynamically using the official GitHub release API, rather
than a bundled version list. Specific versions must be stable `X.Y.Z` releases;
the optional `v` prefix is accepted. Preview/draft releases and missing assets
are rejected. A failed lookup never silently installs an older release.

## Binary Installation

Requires `curl` and Python 3.9 or newer (standard library only). Supported Linux
release mappings: x86_64/amd64, aarch64/arm64, armv7l, i386-i686, ppc64le, s390x.
Unsupported platforms or absent upstream assets fail explicitly.

```text
~/Apps/trivy/<version>/bin/trivy
~/Apps/trivy/current -> <version>
~/.cache/atm/downloads/trivy/
```

The archive and checksum list come from the same official release. ATM verifies
SHA-256 before extraction, rejects unsafe paths/links/special files, and extracts
only the root `trivy` executable into a staging directory. Existing versions are
not overwritten. Published checksums are checked, but Sigstore signatures are
not verified by this initial implementation.

ATM marks its own binary directories for later switching/removal. It does not
adopt a system package or another `trivy` on your PATH. The current version is
protected from individual removal: activate another installed version first.

After installing the binary, apply ATM's PATH configuration and open a new shell:

```bash
atm path apply
trivy --version
trivy image alpine:latest
trivy fs .
```

You can also run `~/Apps/trivy/current/bin/trivy` directly. Installation/status
does not execute the downloaded binary, scan files, or download scan databases.

## Docker Installation

Docker must already be installed with its daemon accessible to your account.
ATM does not automatically install Docker, request sudo, alter group membership,
or grant access to the daemon in this plugin.

The plugin resolves the release and pulls `aquasec/trivy:<version>` rather than
relying on a moving `latest` tag. It verifies that the local image is inspectable,
records the last successfully pulled version, and prints usage commands:

```bash
docker run --rm aquasec/trivy:0.76.1 --version
docker run --rm aquasec/trivy:0.76.1 image alpine:latest
docker run --rm -v "$PWD:/work:ro" aquasec/trivy:0.76.1 fs /work
```

These are examples, not automatically executed commands. No container, privileged
mode, host Docker socket mount, or automatic scan is started by installation.
Run scans only against resources you are authorized to inspect. Scans can fetch
databases and use network resources when you explicitly run them.

Menu status shows a **saved Docker reference**, not a live daemon/image check.
Only the most recently recorded tag is selected by the Docker removal action.
Earlier tags and unrelated images remain untouched. Removal requires confirmation
and never uses force or deletes containers; Docker can refuse removal when a tag
is in use. If this tag existed before ATM pulled it, confirmed removal still
removes that named tag.

## Uninstall And Configuration

Confirmed uninstall first removes the recorded Docker tag, if any; a Docker
failure stops before deleting local binaries. It then removes only marked binary
directories, their managed current link, the saved image reference, and manifest.
Unmarked files, download caches, scan caches/databases, reports, and external
installations remain intact. Other Docker tags are not automatically tracked or
deleted. Individual Docker removal leaves the binary installation unchanged.

| Variable | Default |
| --- | --- |
| `ATM_TRIVY_DEFAULT_VERSION` | `latest` |
| `ATM_TRIVY_INSTALL_ROOT` | `$ATM_APPS_DIR/trivy` |
| `ATM_TRIVY_CACHE_DIR` | `$ATM_DOWNLOAD_DIR/trivy` |
| `ATM_TRIVY_DOCKER_STATE` | `$ATM_STATE_DIR/trivy/docker-version` |

`--dry-run` performs no downloads, Docker operations, version execution, or file
writes. Latest resolution is described symbolically rather than requiring the
network. Invalid versions/platforms are still rejected. Labels are `en-us` only
during active development.

## Validation

```bash
bash -n plugins/trivy/plugin.sh
bash plugins/trivy/tests/test_lifecycle.sh
python3 -B -m unittest discover -s plugins/trivy/tests -p 'test_*.py' -v
ATM_LANG=en-us bin/atm --dry-run install trivy --method binary
ATM_LANG=en-us bin/atm --dry-run install trivy --method docker
```

Automated tests use inert temporary archives and mocked Docker/network commands.
Actual binary execution, Docker pulls, scans, and real removal require manual
validation; none are performed by these tests.

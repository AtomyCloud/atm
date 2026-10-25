# Docker Plugin

Plugin ID: `docker`. Version: `0.0.1`.

The Docker menu installs Engine, standalone Compose, and Desktop. Desktop supports
release selection, removal, and sign-in prerequisites. Option 7 opens image archive
management; Install ALL remains a placeholder.

## Image Archives

```text
7) Export / Restore images
   1) Export a selected image to .tar
   2) Export ALL images to one .tar
   3) Restore images from a .tar archive
   4) Restore ALL .tar archives in a folder
```

Selected export lists local image references and sizes. Choose a number, enter an
existing local `name:tag` or image ID manually, or select All. No image is pulled.
All export includes every local repository tag and untagged image ID, deduplicating
references, in one archive. Only locally available platform variants are saved.

Exports use [Docker image save](https://docs.docker.com/reference/cli/docker/image/save/).
Restore uses [Docker image load](https://docs.docker.com/reference/cli/docker/image/load/),
loading **all images and tags contained in the chosen archive**. These operations
do not back up or restore containers, volumes, running processes, or daemon settings.

### Storage

The default directory is `$ATM_APPS_DIR/Backups/docker`, normally
`~/Apps/Backups/docker`. Override it with `ATM_DOCKER_IMAGE_BACKUP_DIR` before
starting ATM. Export prompts allow a different absolute `.tar` path. Enter an
expanded path such as `/home/user/Backups/app.tar`; interactive input does not
expand `~` or shell variables.

Generated filenames contain the image slug, timestamp, and random suffixes.
An export creates a private temporary file beside the destination, then publishes
it only after Docker succeeds and the file is nonempty. Archives have mode `0600`.
Existing files and symlinks are never overwritten. Failed exports remove their
temporary file; newly created parent directories can remain.

Restore lists `.tar` files from the default directory and also accepts a manual
absolute path. Batch restore loads regular `.tar` files directly inside the chosen
folder, sorted by filename, without recursion. Empty folders are a no-op. Symlink,
empty, unreadable, and missing input archives are refused. Docker validates the
archive format; ATM never extracts archive members into the host filesystem.

### Daemon and Data Safety

- Docker must already be installed and its active daemon accessible. ATM respects
  the current Docker context/environment, including rootless configurations.
- No automatic sudo, service startup/shutdown, context switching, image deletion,
  or pruning occurs during image operations.
- ATM displays the daemon ID and requires confirmation, defaulting to No. It
  checks that ID again before save and before each load; a changed/unavailable
  daemon aborts the operation. Do not change contexts concurrently.
- Archives are **unencrypted** and image layers may contain credentials or other
  private data. Store and share them accordingly.
- Only load trusted archives. Imports can move existing tags to different images.
  A multi-image archive cannot be selectively restored through this menu.
- Batch restore stops on the first failure. Earlier imports remain; there is no
  transaction or automatic rollback of daemon changes.

## CLI Examples

```bash
atm install docker --images menu
atm install docker --images export --image nginx:stable
atm install docker --images export --all
atm install docker --images export --image nginx:stable --output /home/user/Backups/nginx.tar
atm install docker --images restore --input /home/user/Backups/nginx.tar
atm install docker --images restore-all --directory /home/user/Backups
atm --dry-run install docker --images export --all
```

The `--images` prefix routes only to image management; it does not install Engine
or invoke the install/update shutdown workflow. Without that prefix, the existing
`atm install docker` behavior is unchanged.

In dry-run mode image operations print planned commands without invoking Docker,
requesting confirmation, or creating directories/files. All-export therefore uses
a placeholder for the not-yet-queried references. Restore still checks input paths
read-only, so its archive must already exist. Filenames with spaces are supported.

## Validation

```bash
bash -n plugins/docker/plugin.sh plugins/docker/plugin.conf plugins/docker/tests/test_images.sh
bash plugins/docker/tests/test_images.sh
bash plugins/docker/tests/test_runtime.sh
python3 -B -m unittest discover -s plugins/docker/tests -p 'test_*.py' -v
```

Image tests use disposable files and a fake Docker executable/function. They cover
tag preservation, untagged images, daemon/access failures, cancellation, collisions,
partial-file cleanup, batch failures, menu dispatch, and actual CLI no-write dry-run.
Validation with a real Docker daemon remains manual; no real images are modified
by these tests. New image-management strings currently use the en-us fallback.

# AI Prompt — Convert a Bash Script into an ATM Plugin

Use this prompt to ask an AI assistant to convert an existing Bash script into a plugin for **ATM — Atomy Tools Modules**.

The goal is to preserve the useful behaviour of the original script while adapting it to the ATM plugin architecture.

---

## Prompt

```text
You are a senior Bash engineer converting an existing Bash script into an ATM — Atomy Tools Modules plugin.

Analyse the script below and convert it into a complete, reviewable ATM plugin. Do not wrap the original script as one large command. Refactor it into plugin functions with clear responsibilities.

ORIGINAL SCRIPT:
<paste the full Bash script here>

TARGET PLUGIN INFORMATION:
- Tool name: <tool name>
- Plugin id: <lowercase id, for example: nodejs, docker_cli, custom_tool>
- Display name: <display name>
- Icon: <icon>
- Initial plugin version: 0.0.1
- Default version: <version or blank>
- Known versions: <versions or blank>
- PATH entries needed: <yes/no and paths>
- Desktop launcher needed: <yes/no and details>

First return a Conversion Analysis explaining:
- What the script installs or configures.
- External commands, downloads, archives, paths and versions.
- Environment variables, PATH changes and desktop logic.
- sudo usage, destructive operations and hardcoded user paths.
- What must change to respect ATM_DRY_RUN.

Create these files:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

If needed, also create:
plugins/<plugin_id>/<plugin_id>.desktop.in

plugin.metadata must use shell key=value syntax and plugin version 0.0.1. Export the standard function names:
atm_<plugin_id>_menu, atm_<plugin_id>_status, atm_<plugin_id>_install, atm_<plugin_id>_path_entries, atm_<plugin_id>_remove, atm_<plugin_id>_uninstall and atm_<plugin_id>_use.

plugin.conf must contain defaults using ATM_<PLUGIN_ID_IN_UPPERCASE>_* variables. Do not store secrets and do not hardcode the current user's home directory.

plugin.sh requirements:
- Start with #!/usr/bin/env bash.
- Use atm_<plugin_id>_ prefixes for public functions.
- Use local variables and quote expansions.
- Be compatible with set -Eeuo pipefail.
- Use ${VAR:-} for optional variables.
- Respect ATM_DRY_RUN.
- Do not put plugin-specific behaviour in core lib files.
- Do not create system/global .desktop files.
- Use locale keys for final user-facing text.

Implement standard semantics:
install installs or configures the tool, writes the manifest and supports --version when versions exist.
use switches current version when versions exist, or returns a clear no-op.
remove removes one version or one payload when applicable.
uninstall asks for confirmation and removes only plugin-managed files.
status prints one short menu status line.
path_entries prints only paths, one per line.
menu is interactive, avoids duplicate numbering, and includes b) Back and q) Exit.

Unsafe behaviour conversion:
- If sudo is present, remove hidden elevation or make it explicit and explain it.
- Convert /usr, /opt, /etc and system desktop writes to ATM user-space paths when possible.
- Prefer path_entries plus atm path apply instead of direct shell RC edits.
- Use atm_download_file for downloads when possible.

Use atm_manifest_write and include at least:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION or ATM_CURRENT_STACK
ATM_CURRENT_PATH or ATM_INSTALL_ROOT
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS when versions exist

Create lang/en-us.lang with all visible menu/status text and use atm_t.

Return:
1. Conversion Analysis
2. File tree
3. Full content of each generated file
4. Validation commands
5. Notes about behaviour changed from the original script

Validation commands must include:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

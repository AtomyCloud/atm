# AI Prompt — ATM Plugin Creator

Use this prompt to ask an AI assistant to create a plugin for **ATM — Atomy Tools Modules**.

The goal is to generate a complete, reviewable plugin that follows the current ATM architecture.

---

## Prompt

```text
You are a senior Bash engineer creating a plugin for ATM — Atomy Tools Modules.

ATM is a modular Bash tool for installing and managing developer tools on Linux. It uses trusted local plugins. Each plugin lives in plugins/<plugin_id>/ and owns the tool-specific logic: install paths, downloads, versions, status, menu, version switching, removal, uninstall, manifests, PATH entries, desktop launchers when applicable, and locales.

Create a complete plugin for this tool:

TOOL:
<describe the tool, for example: Node.js, Rust, Docker CLI, Deno, Bun, Python, etc.>

PLUGIN ID:
<example: nodejs, rust, deno, bun>

DISPLAY NAME:
<example: Node.js>

ICON:
<example: 🟩>

INITIAL PLUGIN VERSION:
0.0.1

TOOL VERSIONS TO SHOW IN THE MENU:
<example: 22.11.0 22.10.0 20.18.1 20.17.0 18.20.5>

DEFAULT VERSION:
<example: 22.11.0>

TARGET ARCHITECTURE:
linux-x64 or linux-amd64, depending on the tool.

DOWNLOAD URL PATTERN:
<explain the official URL pattern. If unknown, leave a clearly marked download function for review.>

Requirements:

1. The plugin must follow the ATM architecture.

Required files:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Recommended locale files for release:

plugins/<plugin_id>/lang/pt-br.lang
plugins/<plugin_id>/lang/pt-pt.lang
plugins/<plugin_id>/lang/es.lang
plugins/<plugin_id>/lang/it.lang
plugins/<plugin_id>/lang/fr.lang
plugins/<plugin_id>/lang/de.lang
plugins/<plugin_id>/lang/ru.lang
plugins/<plugin_id>/lang/ja.lang
plugins/<plugin_id>/lang/zh-cn.lang
plugins/<plugin_id>/lang/ko.lang

2. plugin.metadata must use shell key=value syntax.

Use this format, adjusting names and functions:

ATM_PLUGIN_ID="<plugin_id>"
ATM_PLUGIN_NAME_VALUE="<Display name>"
ATM_PLUGIN_ICON_VALUE="<icon>"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="<suggested order number>"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage <Display name> versions"
ATM_PLUGIN_ENTRYPOINT="plugin.sh"
ATM_PLUGIN_FULL_SETUP_VALUE="1"
ATM_PLUGIN_DEPENDS_VALUE=""
ATM_PLUGIN_OPTIONAL_DEPENDS_VALUE=""
ATM_PLUGIN_MENU_FUNC_VALUE="atm_<plugin_id>_menu"
ATM_PLUGIN_STATUS_FUNC_VALUE="atm_<plugin_id>_status"
ATM_PLUGIN_INSTALL_FUNC_VALUE="atm_<plugin_id>_install"
ATM_PLUGIN_PATH_FUNC_VALUE="atm_<plugin_id>_path_entries"
ATM_PLUGIN_DESKTOP_FUNC_VALUE=""
ATM_PLUGIN_REMOVE_FUNC_VALUE="atm_<plugin_id>_remove"
ATM_PLUGIN_UNINSTALL_FUNC_VALUE="atm_<plugin_id>_uninstall"
ATM_PLUGIN_USE_FUNC_VALUE="atm_<plugin_id>_use"

If the tool has a GUI and needs a desktop launcher, also implement:

ATM_PLUGIN_DESKTOP_FUNC_VALUE="atm_<plugin_id>_desktop"

3. plugin.conf must contain default settings.

Use variables prefixed with ATM_<PLUGIN_ID_IN_UPPERCASE>_.

Example:

ATM_<PLUGIN>_DEFAULT_VERSION="<version>"
ATM_<PLUGIN>_ARCH="linux-x64"
ATM_<PLUGIN>_INSTALL_ROOT="$ATM_APPS_DIR/<plugin_id>"
ATM_<PLUGIN>_CACHE_DIR="$ATM_DOWNLOAD_DIR/<plugin_id>"
ATM_<PLUGIN>_MANIFEST_FILE="$ATM_MANIFEST_DIR/<plugin_id>.manifest"
ATM_<PLUGIN>_VERSION_OPTIONS="<version list>"

4. plugin.sh must be compatible Bash and follow these rules:

- Start with #!/usr/bin/env bash.
- All public functions must use the atm_<plugin_id>_ prefix.
- Use local variables inside functions.
- Quote variables.
- Be compatible with set -Eeuo pipefail.
- Avoid optional variables without fallbacks. Use ${VAR:-}.
- Respect ATM_DRY_RUN.
- Do not write outside the ATM path model without a clear reason.
- Do not use sudo.
- Do not put plugin-specific behaviour in the core.
- Do not create global/system .desktop files.
- Do not install icons into native system themes.

5. The plugin must implement these functions:

atm_<plugin_id>_archive_name
atm_<plugin_id>_download_url
atm_<plugin_id>_install_dir
atm_<plugin_id>_current_path
atm_<plugin_id>_cache_dir
atm_<plugin_id>_cache_file
atm_<plugin_id>_manifest_file
atm_<plugin_id>_normalize_version
atm_<plugin_id>_version_from_args
atm_<plugin_id>_status
atm_<plugin_id>_list_installed_versions
atm_<plugin_id>_current_version
atm_<plugin_id>_write_manifest
atm_<plugin_id>_install
atm_<plugin_id>_use
atm_<plugin_id>_remove
atm_<plugin_id>_uninstall
atm_<plugin_id>_menu
atm_<plugin_id>_path_entries

If it is a GUI tool:

atm_<plugin_id>_desktop

6. Required semantics:

install:
- Accept --version <version> and --version=<version>.
- Use the default version when none is provided.
- Normalise and validate the version.
- Detect whether the version is already installed before downloading.
- Download into "$ATM_DOWNLOAD_DIR/<plugin_id>".
- Extract/install into "$ATM_APPS_DIR/<plugin_id>/<version>" or the equivalent plugin path.
- Update the current symlink.
- Write the manifest.
- Do not run version commands during dry-run.

use:
- Accept atm use <plugin_id> <version>.
- Check that the version is installed.
- Update the current symlink.
- Write the manifest.
- Regenerate the desktop launcher if applicable.

remove:
- Remove a specific version.
- Do not allow removing the current version.
- Update the manifest afterwards.

uninstall:
- Ask for interactive confirmation.
- Remove everything managed by the plugin.
- Remove the manifest.
- Remove the desktop launcher if applicable.

status:
- Print one short line for the main menu.
- If installed, start with "✅ ".
- If not installed, use locale key ATM_PLUGIN_<ID>_STATUS_NOT_INSTALLED.

path_entries:
- Print only paths, one per line.
- Do not print human messages.

menu:
- Must be interactive.
- Must avoid duplicate numbering.
- Must use dynamic options when the version list changes.
- Must always include:
  b) Back
  q) Exit
- After an action, wait for "Press any key to continue...".

7. Manifest:

Use atm_manifest_write.

The manifest must include at least:

ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

8. Download and extraction:

Use core helpers where possible:

atm_download_file "$url" "$cache_file"
atm_archive_extract_tar_gz "$cache_file" "$dest" 1

If the format is zip, use the existing ATM helper if available, or provide a small reviewed extraction function.

9. Locales:

Create lang/en-us.lang with all visible menu and status text.

Use keys named like:

ATM_PLUGIN_<ID>_MENU_TITLE
ATM_PLUGIN_<ID>_CURRENT
ATM_PLUGIN_<ID>_LATEST_STABLE
ATM_PLUGIN_<ID>_CHOOSE_VERSION
ATM_PLUGIN_<ID>_LIST_INSTALLED
ATM_PLUGIN_<ID>_REMOVE_VERSION
ATM_PLUGIN_<ID>_UNINSTALL
ATM_PLUGIN_<ID>_STATUS_NOT_INSTALLED
ATM_PLUGIN_<ID>_INSTALLING
ATM_PLUGIN_<ID>_INSTALLED
ATM_PLUGIN_<ID>_USING
ATM_PLUGIN_<ID>_REMOVED
ATM_PLUGIN_<ID>_UNINSTALLED
ATM_PLUGIN_<ID>_CANCELLED

Use atm_t for user-facing text.

10. Validation:

At the end, provide these commands:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <default_version>
ATM_LANG=en-us bin/atm --dry-run path apply

Return the full file contents for each required file and a short implementation note explaining any assumptions.
```


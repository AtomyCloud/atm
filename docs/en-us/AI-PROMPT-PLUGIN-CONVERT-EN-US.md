# AI Prompt — Convert a Bash Script into an ATM Plugin

Use this prompt to ask an AI assistant to convert an existing Bash script into a plugin for **ATM — Atomy Tools Modules**.

The goal is to preserve the useful behavior from the original script while adapting it to the ATM plugin architecture: metadata, configuration, install/use/remove/uninstall semantics, manifests, PATH entries, menus, dry-run, and locales.

---

## Prompt

```text
You are a senior Bash engineer converting an existing Bash script into a plugin for ATM — Atomy Tools Modules.

ATM is a modular Bash tool for installing and managing developer tools on Linux. It uses trusted local plugins. Each plugin lives in plugins/<plugin_id>/ and owns the tool-specific logic: install paths, downloads, versions, status, menu, version switching, removal, uninstall, manifests, PATH entries, desktop launchers when applicable, and locales.

Your task is to analyse the Bash script below and convert it into a complete ATM plugin.

Do not simply wrap the original script as one large command. Refactor it into ATM plugin functions with clear responsibilities.

ORIGINAL SCRIPT:

```bash
<paste the full Bash script here>
```

TARGET PLUGIN INFORMATION:

Tool name:
<example: Node.js, Rust, Docker CLI, Deno, Bun, Python, Custom Tool>

Desired plugin id:
<example: nodejs, rust, docker_cli, deno, bun, python, custom_tool>

Display name:
<example: Node.js>

Icon:
<example: 🟩>

Initial plugin version:
0.0.1

Default tool version:
<example: 22.11.0, or leave blank if the script does not manage versions>

Known installable versions:
<example: 22.11.0 22.10.0 20.18.1, or leave blank if unknown>

Target architecture:
<example: linux-x64, linux-amd64, noarch, or unknown>

Does the tool need PATH entries?
<yes/no; explain expected binary paths>

Does the tool need a desktop launcher?
<yes/no; if yes, provide app name, executable, icon, categories, and URL schemes if any>

Expected install root:
<default: "$ATM_APPS_DIR/<plugin_id>">

Expected cache directory:
<default: "$ATM_DOWNLOAD_DIR/<plugin_id>">

Requirements:

1. First analyse the original script.

Identify:

- What the script installs or configures.
- Required external commands.
- Download URLs or package sources.
- Archive formats.
- Install paths.
- Version handling, if any.
- Environment variables.
- PATH changes.
- Desktop launcher logic, if any.
- Destructive operations.
- sudo usage.
- Any hardcoded user paths.
- Any interactive prompts.
- Any logic that must be changed to respect ATM_DRY_RUN.

Return a short "Conversion Analysis" section before the files.

2. Convert the script to the ATM plugin structure.

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

If a desktop launcher is needed, also create:

plugins/<plugin_id>/<plugin_id>.desktop.in

3. plugin.metadata must use shell key=value syntax.

Use this format, adjusting names and functions:

ATM_PLUGIN_ID="<plugin_id>"
ATM_PLUGIN_NAME_VALUE="<Display name>"
ATM_PLUGIN_ICON_VALUE="<icon>"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="<suggested order number>"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage <Display name>"
ATM_PLUGIN_ENTRYPOINT="plugin.sh"
ATM_PLUGIN_FULL_SETUP_VALUE="0"
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

If the original script creates or launches a GUI app, use:

ATM_PLUGIN_DESKTOP_FUNC_VALUE="atm_<plugin_id>_desktop"

Set ATM_PLUGIN_FULL_SETUP_VALUE="1" only if the plugin is safe and expected to run during full setup.

4. plugin.conf must contain plugin defaults.

Use variables prefixed with ATM_<PLUGIN_ID_IN_UPPERCASE>_.

Include values such as:

ATM_<PLUGIN>_DEFAULT_VERSION="<version>"
ATM_<PLUGIN>_VERSION_OPTIONS="<version list>"
ATM_<PLUGIN>_ARCH="<architecture>"
ATM_<PLUGIN>_INSTALL_ROOT="$ATM_APPS_DIR/<plugin_id>"
ATM_<PLUGIN>_CACHE_DIR="$ATM_DOWNLOAD_DIR/<plugin_id>"
ATM_<PLUGIN>_MANIFEST_FILE="$ATM_MANIFEST_DIR/<plugin_id>.manifest"
ATM_<PLUGIN>_DOWNLOAD_BASE_URL="<base URL if known>"

Rules:

- Let environment variables override defaults when useful.
- Do not store secrets.
- Do not hardcode the current user's home path.
- Use ATM paths instead of raw script paths whenever possible.

5. plugin.sh must be refactored Bash, not a raw script wrapper.

Rules:

- Start with #!/usr/bin/env bash.
- All public functions must use the atm_<plugin_id>_ prefix.
- Use local variables inside functions.
- Quote variables.
- Be compatible with set -Eeuo pipefail.
- Use ${VAR:-} for optional variables.
- Respect ATM_DRY_RUN.
- Do not use sudo.
- Do not write outside the ATM path model without a clear reason.
- Do not put plugin-specific behavior in core lib files.
- Do not create global/system .desktop files.
- Do not install icons into native system icon themes.
- Replace hardcoded final UX strings with locale keys when practical.

6. Implement the standard plugin functions.

Implement these functions when the tool manages versions or downloadable archives:

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

If the script does not manage versions, still provide compatible install/use/remove/uninstall/status/menu/path_entries functions. In that case:

- install should set up the tool.
- use may print a clear no-op or validate the installed state.
- remove may remove the installed payload if there is only one payload.
- uninstall must remove everything managed by the plugin after confirmation.

If it is a GUI tool, also implement:

atm_<plugin_id>_desktop

7. Preserve behavior, but adapt semantics.

install:

- Accept --version <version> and --version=<version> when versions exist.
- Use the default version when none is provided.
- Validate the version when possible.
- Detect whether the selected version is already installed.
- Download into "$ATM_DOWNLOAD_DIR/<plugin_id>".
- Extract/install into "$ATM_APPS_DIR/<plugin_id>/<version>" or a documented plugin path.
- Update the current symlink when versions exist.
- Write the manifest.
- Regenerate desktop launcher if applicable.
- Do not execute newly installed binaries during dry-run.

use:

- Accept atm use <plugin_id> <version> when versions exist.
- Check that the version is installed.
- Update the current symlink.
- Write the manifest.
- Regenerate desktop launcher if applicable.

remove:

- Remove one specific version when versions exist.
- Do not allow removing the current version.
- Update the manifest after removal.

uninstall:

- Ask for interactive confirmation.
- Remove all plugin-managed files.
- Remove the manifest.
- Remove desktop launchers if applicable.
- Do not remove unrelated user data.

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
- Must include useful actions from the original script.
- Must always include:
  b) Back
  q) Exit
- After an action, wait for "Press any key to continue...".

8. Convert unsafe script behavior.

If the original script uses sudo:

- Remove sudo from plugin operations.
- Explain what was changed.
- Use user-space paths instead.
- If system-level setup is unavoidable, mark it as unsupported or manual.

If the original script writes to /usr, /opt, /etc, or system desktop directories:

- Convert writes to ATM user-space paths.
- Use ~/.local/share/applications for desktop files.
- Use ~/.local/bin only for CLI symlinks when appropriate.

If the original script modifies shell files:

- Prefer plugin path_entries and ATM path apply.
- Do not directly edit shell RC files from the plugin unless there is a strong ATM-compatible reason.

If the original script downloads remote files:

- Use atm_download_file where possible.
- Keep URLs explicit and reviewable.
- Add checksum verification only if official checksums are known.

9. Manifest:

Use atm_manifest_write.

The manifest must include at least:

ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION or ATM_CURRENT_STACK
ATM_CURRENT_PATH or ATM_INSTALL_ROOT
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS when versions exist

10. Locales:

Create lang/en-us.lang with all visible menu and status text.

Use keys named like:

ATM_PLUGIN_<ID>_MENU_TITLE
ATM_PLUGIN_<ID>_CURRENT
ATM_PLUGIN_<ID>_INSTALL
ATM_PLUGIN_<ID>_USE_VERSION
ATM_PLUGIN_<ID>_LIST_INSTALLED
ATM_PLUGIN_<ID>_REMOVE_VERSION
ATM_PLUGIN_<ID>_UNINSTALL
ATM_PLUGIN_<ID>_STATUS_NOT_INSTALLED
ATM_PLUGIN_<ID>_INSTALLING
ATM_PLUGIN_<ID>_INSTALLED
ATM_PLUGIN_<ID>_USING
ATM_PLUGIN_<ID>_REMOVED
ATM_PLUGIN_<ID>_UNINSTALL_WARNING
ATM_PLUGIN_<ID>_UNINSTALLED
ATM_PLUGIN_<ID>_CANCELLED

Use atm_t for user-facing text.

11. Return the converted plugin as full files.

Return:

1. Conversion Analysis
2. File tree
3. Full content of plugins/<plugin_id>/plugin.metadata
4. Full content of plugins/<plugin_id>/plugin.conf
5. Full content of plugins/<plugin_id>/plugin.sh
6. Full content of plugins/<plugin_id>/lang/en-us.lang
7. Full content of desktop template if needed
8. Validation commands
9. Notes about behavior changed from the original script

12. Validation commands:

At the end, provide commands like:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <default_version>
ATM_LANG=en-us bin/atm --dry-run path apply

If the plugin has no version support, use:

ATM_LANG=en-us bin/atm --dry-run install <plugin_id>

Important:

- Keep the implementation reviewable.
- Do not hide risky behavior.
- Do not invent official URLs if the original script does not provide them.
- Mark uncertain download or install logic with a clear TODO comment.
- Prefer ATM helpers over custom code when helpers exist.
- Keep the plugin version at 0.0.1 for initial release.
```


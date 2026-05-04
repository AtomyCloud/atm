# ATM Plugin Developer Manual — From Scratch

This manual explains how to create a new ATM plugin from zero.

ATM plugins are trusted local Bash modules. A plugin owns tool-specific logic: install paths, version detection, PATH entries, desktop templates, manifests, menus, and translations.

## 1. Plugin Contract

Every plugin lives in:

```text
plugins/<plugin_id>/
```

Minimum files:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

Recommended translated locale files:

```text
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
```

## 2. Naming Rules

Use one stable plugin id:

```text
example_tool
```

Rules:

```text
- Use lowercase letters, numbers, and underscores.
- Keep the plugin id unique.
- Prefix all plugin functions with atm_<plugin_id>_.
- Do not put tool-specific behavior in core lib files.
```

Example function prefix:

```bash
atm_example_tool_status
atm_example_tool_install
atm_example_tool_use
```

## 3. Create The Directory

```bash
mkdir -p plugins/example_tool/lang
```

## 4. Create plugin.metadata

`plugin.metadata` is shell `key=value`. It is sourced by ATM, so keep it simple and trusted.

```bash
ATM_PLUGIN_ID="example_tool"
ATM_PLUGIN_NAME_VALUE="Example Tool"
ATM_PLUGIN_ICON_VALUE="🧩"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="90"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage Example Tool versions"
ATM_PLUGIN_ENTRYPOINT="plugin.sh"
ATM_PLUGIN_FULL_SETUP_VALUE="0"
ATM_PLUGIN_DEPENDS_VALUE=""
ATM_PLUGIN_OPTIONAL_DEPENDS_VALUE=""
ATM_PLUGIN_MENU_FUNC_VALUE="atm_example_tool_menu"
ATM_PLUGIN_STATUS_FUNC_VALUE="atm_example_tool_status"
ATM_PLUGIN_INSTALL_FUNC_VALUE="atm_example_tool_install"
ATM_PLUGIN_PATH_FUNC_VALUE="atm_example_tool_path_entries"
ATM_PLUGIN_DESKTOP_FUNC_VALUE=""
ATM_PLUGIN_REMOVE_FUNC_VALUE="atm_example_tool_remove"
ATM_PLUGIN_UNINSTALL_FUNC_VALUE="atm_example_tool_uninstall"
ATM_PLUGIN_USE_FUNC_VALUE="atm_example_tool_use"
```

Field meanings:

```text
ATM_PLUGIN_ID                  Unique plugin id.
ATM_PLUGIN_NAME_VALUE          Human-readable name.
ATM_PLUGIN_ICON_VALUE          Menu icon.
ATM_PLUGIN_VERSION_VALUE       Plugin implementation version.
ATM_PLUGIN_ORDER_VALUE         Main menu sort order.
ATM_PLUGIN_DESCRIPTION_VALUE   Short description.
ATM_PLUGIN_ENTRYPOINT          Shell file sourced after plugin.conf.
ATM_PLUGIN_FULL_SETUP_VALUE    1 to include in Full Setup, 0 to skip.
ATM_PLUGIN_*_FUNC_VALUE        Function names exported by plugin.sh.
```

## 5. Create plugin.conf

`plugin.conf` stores default paths, versions, URLs, and plugin-specific settings.

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"

ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

Rules:

```text
- Use ATM_<PLUGIN_ID_UPPER>_* variables.
- Let environment variables override defaults when useful.
- Keep secrets out of plugin.conf.
```

## 6. Create lang/en-us.lang

All user-facing menu text should be localized.

```bash
ATM_PLUGIN_EXAMPLE_TOOL_MENU_TITLE="Example Tool Installer"
ATM_PLUGIN_EXAMPLE_TOOL_CURRENT="Current"
ATM_PLUGIN_EXAMPLE_TOOL_LATEST_STABLE="Latest Stable"
ATM_PLUGIN_EXAMPLE_TOOL_CHOOSE_VERSION="Choose specific version"
ATM_PLUGIN_EXAMPLE_TOOL_LIST_INSTALLED="List installed versions"
ATM_PLUGIN_EXAMPLE_TOOL_REMOVE_VERSION="Remove specific version"
ATM_PLUGIN_EXAMPLE_TOOL_UNINSTALL="Uninstall Example Tool completely"
ATM_PLUGIN_EXAMPLE_TOOL_ENTER_VERSION="Enter Example Tool version:"
ATM_PLUGIN_EXAMPLE_TOOL_STATUS_NOT_INSTALLED="not installed"
ATM_PLUGIN_EXAMPLE_TOOL_INSTALLING="Installing Example Tool"
ATM_PLUGIN_EXAMPLE_TOOL_ALREADY_INSTALLED="Example Tool already installed"
ATM_PLUGIN_EXAMPLE_TOOL_INSTALLED="Example Tool installed"
ATM_PLUGIN_EXAMPLE_TOOL_USING="Using Example Tool"
ATM_PLUGIN_EXAMPLE_TOOL_REMOVED="Example Tool version removed"
ATM_PLUGIN_EXAMPLE_TOOL_UNINSTALL_WARNING="This will remove all Example Tool versions managed by ATM."
ATM_PLUGIN_EXAMPLE_TOOL_UNINSTALLED="Example Tool uninstalled"
ATM_PLUGIN_EXAMPLE_TOOL_CANCELLED="Cancelled."
```

Use locale keys in Bash:

```bash
printf '%s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_INSTALLING)"
```

## 7. Create plugin.sh Skeleton

Start with this complete skeleton:

```bash
#!/usr/bin/env bash

atm_example_tool_install_dir() {
    local version="$1"
    printf '%s/%s\n' "${ATM_EXAMPLE_TOOL_INSTALL_ROOT:-$ATM_APPS_DIR/example-tool}" "$version"
}

atm_example_tool_current_path() {
    printf '%s/current\n' "${ATM_EXAMPLE_TOOL_INSTALL_ROOT:-$ATM_APPS_DIR/example-tool}"
}

atm_example_tool_cache_dir() {
    printf '%s\n' "${ATM_EXAMPLE_TOOL_CACHE_DIR:-$ATM_DOWNLOAD_DIR/example-tool}"
}

atm_example_tool_manifest_file() {
    printf '%s\n' "${ATM_EXAMPLE_TOOL_MANIFEST_FILE:-$ATM_MANIFEST_DIR/example_tool.manifest}"
}

atm_example_tool_normalize_version() {
    local version="$1"

    if [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s\n' "$version"
    else
        atm_fail "Invalid Example Tool version: $version. Expected format: 1.0.0"
    fi
}

atm_example_tool_version_from_args() {
    local version="${ATM_EXAMPLE_TOOL_DEFAULT_VERSION:-1.0.0}"

    while (($# > 0)); do
        case "$1" in
            --version)
                version="${2:-}"
                [[ -n "$version" ]] || atm_fail "Missing value for --version"
                shift
                ;;
            --version=*)
                version="${1#--version=}"
                ;;
            *)
                ;;
        esac

        shift || true
    done

    atm_example_tool_normalize_version "$version"
}

atm_example_tool_status() {
    local current=""

    current="$(atm_example_tool_current_path)"

    if [[ -x "$current/bin/example-tool" ]]; then
        printf '✅ '
        "$current/bin/example-tool" --version 2>/dev/null | sed -n '1p'
    else
        printf '%s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_STATUS_NOT_INSTALLED)"
    fi
}

atm_example_tool_list_installed_versions() {
    local root="${ATM_EXAMPLE_TOOL_INSTALL_ROOT:-$ATM_APPS_DIR/example-tool}"
    local path=""

    [[ -d "$root" ]] || return 0

    for path in "$root"/*; do
        [[ -d "$path" ]] || continue
        [[ "$(basename "$path")" == "current" ]] && continue
        [[ -x "$path/bin/example-tool" ]] || continue
        basename "$path"
    done | sort -V
}

atm_example_tool_current_version() {
    local current=""
    local resolved=""

    current="$(atm_example_tool_current_path)"
    [[ -e "$current" ]] || return 1

    if command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "$current" 2>/dev/null || true)"
    else
        resolved="$current"
    fi

    [[ -n "$resolved" ]] || return 1
    basename "$resolved"
}

atm_example_tool_write_manifest() {
    local current_version="${1:-}"
    local installed_versions=""

    installed_versions="$(atm_example_tool_list_installed_versions | tr '\n' ' ' | sed 's/[[:space:]]*$//')"

    atm_manifest_write "example_tool" \
        "ATM_PLUGIN_NAME=\"Example Tool\"" \
        "ATM_PLUGIN_VERSION=\"0.0.1\"" \
        "ATM_INSTALLED=\"1\"" \
        "ATM_CURRENT_VERSION=\"$current_version\"" \
        "ATM_CURRENT_PATH=\"$(atm_example_tool_current_path)\"" \
        "ATM_INSTALL_ROOT=\"${ATM_EXAMPLE_TOOL_INSTALL_ROOT:-$ATM_APPS_DIR/example-tool}\"" \
        "ATM_INSTALLED_VERSIONS=\"$installed_versions\""
}

atm_example_tool_install() {
    local version=""
    local dest=""

    version="$(atm_example_tool_version_from_args "$@")"
    dest="$(atm_example_tool_install_dir "$version")"

    printf '%s %s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_INSTALLING)" "$version"

    if [[ -x "$dest/bin/example-tool" ]]; then
        atm_warn "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_ALREADY_INSTALLED): $dest"
    else
        if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
            printf 'DRY-RUN: install Example Tool %s into %s\n' "$version" "$dest"
        else
            mkdir -p "$dest/bin"
            # Replace this placeholder with download/extract/install logic.
            atm_fail "Example Tool install logic is not implemented yet."
        fi
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_example_tool_current_path)"
    else
        ln -sfn "$dest" "$(atm_example_tool_current_path)"
    fi

    atm_example_tool_write_manifest "$version"
    atm_success "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_INSTALLED): $version"
}

atm_example_tool_use() {
    local version="${1:-}"
    local dest=""

    [[ -n "$version" ]] || atm_fail "Usage: atm use example_tool <version>"

    version="$(atm_example_tool_normalize_version "$version")"
    dest="$(atm_example_tool_install_dir "$version")"

    [[ -x "$dest/bin/example-tool" ]] || atm_fail "Example Tool version is not installed: $version"

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: ln -sfn %s %s\n' "$dest" "$(atm_example_tool_current_path)"
    else
        ln -sfn "$dest" "$(atm_example_tool_current_path)"
    fi

    atm_example_tool_write_manifest "$version"
    atm_success "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_USING): $version"
}

atm_example_tool_remove() {
    local version="${1:-}"
    local dest=""
    local current_version=""

    [[ -n "$version" ]] || atm_fail "Usage: atm remove example_tool <version>"

    version="$(atm_example_tool_normalize_version "$version")"
    dest="$(atm_example_tool_install_dir "$version")"
    current_version="$(atm_example_tool_current_version 2>/dev/null || true)"

    [[ -d "$dest" ]] || atm_fail "Example Tool version is not installed: $version"

    if [[ "$current_version" == "$version" ]]; then
        atm_fail "Cannot remove current Example Tool version: $version. Switch version first."
    fi

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$dest"
    else
        rm -rf "$dest"
    fi

    atm_example_tool_write_manifest "$current_version"
    atm_success "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_REMOVED): $version"
}

atm_example_tool_uninstall() {
    local root="${ATM_EXAMPLE_TOOL_INSTALL_ROOT:-$ATM_APPS_DIR/example-tool}"
    local answer=""

    printf '%s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_UNINSTALL_WARNING)"
    printf 'Target: %s\n' "$root"
    printf 'Continue? [y/N]: '
    read -r answer

    case "$answer" in
        y|Y|yes|YES) ;;
        *)
            atm_warn "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_CANCELLED)"
            return 0
            ;;
    esac

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: rm -rf %s\n' "$root"
        printf 'DRY-RUN: rm -f %s\n' "$(atm_example_tool_manifest_file)"
    else
        rm -rf "$root"
        rm -f "$(atm_example_tool_manifest_file)"
    fi

    atm_success "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_UNINSTALLED)"
}

atm_example_tool_menu() {
    local choice=""
    local version=""
    local idx=1
    local install_count=0
    local versions=()
    local choose_option=0
    local list_option=0
    local remove_option=0
    local uninstall_option=0

    while true; do
        clear
        printf '%s\n' "=========================================="
        printf '    🧩 %s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_MENU_TITLE)"
        printf '%s\n' "=========================================="
        printf '%s: %s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_CURRENT)" "$(atm_example_tool_status)"
        printf '%s\n' "------------------------------------------"

        idx=1
        versions=()

        for version in ${ATM_EXAMPLE_TOOL_VERSION_OPTIONS:-1.0.0 0.9.0 0.8.0}; do
            versions+=("$version")
            if [[ "$idx" == "1" ]]; then
                printf '%s) Example Tool %s (%s)\n' "$idx" "$version" "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_LATEST_STABLE)"
            else
                printf '%s) Example Tool %s\n' "$idx" "$version"
            fi
            idx=$((idx + 1))
        done

        install_count="${#versions[@]}"

        choose_option="$idx"
        printf '%s) %s\n' "$choose_option" "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_CHOOSE_VERSION)"
        idx=$((idx + 1))

        list_option="$idx"
        printf '%s) %s\n' "$list_option" "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_LIST_INSTALLED)"
        idx=$((idx + 1))

        remove_option="$idx"
        printf '%s) %s\n' "$remove_option" "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_REMOVE_VERSION)"
        idx=$((idx + 1))

        uninstall_option="$idx"
        printf '%s) %s\n' "$uninstall_option" "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_UNINSTALL)"

        printf 'b) %s\n' "$(atm_t ATM_MENU_BACK)"
        printf 'q) %s\n' "$(atm_t ATM_MENU_EXIT)"
        printf '%s ' "$(atm_t ATM_MENU_SELECT_OPTION)"
        read -r choice

        case "$choice" in
            b|B) return 0 ;;
            q|Q) exit 0 ;;
            ''|*[!0-9]*)
                atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                ;;
            *)
                if [[ "$choice" -ge 1 && "$choice" -le "$install_count" ]]; then
                    version="${versions[$((choice - 1))]}"
                    atm_example_tool_install --version "$version"
                elif [[ "$choice" -eq "$choose_option" ]]; then
                    printf '%s ' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_ENTER_VERSION)"
                    read -r version
                    [[ -n "$version" ]] || {
                        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                        continue
                    }
                    atm_example_tool_install --version "$version"
                elif [[ "$choice" -eq "$list_option" ]]; then
                    atm_example_tool_list_installed_versions
                elif [[ "$choice" -eq "$remove_option" ]]; then
                    printf '%s ' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_ENTER_VERSION)"
                    read -r version
                    [[ -n "$version" ]] || {
                        atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                        continue
                    }
                    atm_example_tool_remove "$version"
                elif [[ "$choice" -eq "$uninstall_option" ]]; then
                    atm_example_tool_uninstall
                else
                    atm_warn "$(atm_t ATM_ERR_INVALID_OPTION)"
                fi
                ;;
        esac

        printf '\n%s' "$(atm_t ATM_MENU_PRESS_ANY_KEY)"
        read -r -n 1 _ || true
        printf '\n'
    done
}

atm_example_tool_path_entries() {
    printf '%s/current/bin\n' "${ATM_EXAMPLE_TOOL_INSTALL_ROOT:-$ATM_APPS_DIR/example-tool}"
}
```

## 8. Implement Real Install Logic

A real plugin usually needs:

```bash
atm_example_tool_archive_name() {
    local version="$1"
    printf 'example-tool-%s-linux-x64.tar.gz\n' "$version"
}

atm_example_tool_download_url() {
    local version="$1"
    printf 'https://example.com/releases/%s/%s\n' "$version" "$(atm_example_tool_archive_name "$version")"
}

atm_example_tool_cache_file() {
    local version="$1"
    printf '%s/%s\n' "$(atm_example_tool_cache_dir)" "$(atm_example_tool_archive_name "$version")"
}
```

Then inside install:

```bash
mkdir -p "$(atm_example_tool_cache_dir)"
atm_download_file "$url" "$cache_file"
atm_archive_extract_tar_gz "$cache_file" "$dest" 1
```

Respect dry-run:

```bash
if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
    printf 'DRY-RUN: install Example Tool %s into %s\n' "$version" "$dest"
else
    atm_archive_extract_tar_gz "$cache_file" "$dest" 1
fi
```

## 9. Desktop Plugins

Only GUI plugins need desktop integration.

Add to metadata:

```bash
ATM_PLUGIN_DESKTOP_FUNC_VALUE="atm_example_tool_desktop"
```

Create a template:

```text
plugins/example_tool/example-tool.desktop.in
```

Example:

```ini
[Desktop Entry]
Name=Example Tool
Comment=Example Tool installed by ATM
Exec=__EXAMPLE_TOOL_EXEC__ %F
Icon=__EXAMPLE_TOOL_ICON__
Type=Application
StartupNotify=false
Categories=Development;
```

Render and install:

```bash
atm_example_tool_desktop() {
    local version=""
    local current=""
    local rendered=""

    version="$(atm_example_tool_current_version 2>/dev/null || true)"
    [[ -n "$version" ]] || return 0

    current="$(atm_example_tool_current_path)"
    rendered="$(mktemp)"

    sed \
        -e "s|__EXAMPLE_TOOL_EXEC__|$current/bin/example-tool|g" \
        -e "s|__EXAMPLE_TOOL_ICON__|$current/share/example-tool/icon.png|g" \
        "$ATM_PLUGIN_DIR/example_tool/example-tool.desktop.in" > "$rendered"

    atm_desktop_install_file "$rendered" "example-tool.desktop"
    rm -f "$rendered"
}
```

Rules:

```text
- Desktop files go to ~/.local/share/applications.
- Do not use sudo for desktop files.
- Do not install icons into system icon themes.
- Plugin owns its templates and render rules.
```

## 10. Locales For A Plugin

The active locale is loaded from:

```text
plugins/<plugin_id>/lang/<ATM_LANG>.lang
```

If the file does not exist, plugin-specific keys will be missing. For release, create every supported locale.

Supported release locales:

```text
en-us
pt-br
pt-pt
es
it
fr
de
ru
ja
zh-cn
ko
```

Validate locale files:

```bash
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
```

## 11. Validation Checklist

Syntax:

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
```

Loader:

```bash
ATM_LANG=en-us bin/atm plugins list
```

Dry-run:

```bash
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

Menu:

```bash
ATM_LANG=en-us bin/atm
```

Locale smoke tests:

```bash
for lang in en-us pt-br pt-pt es it fr de ru ja zh-cn ko; do
  ATM_LANG="$lang" bin/atm plugins list
done
```

## 12. Release Rules

Before opening a pull request or packaging a release:

```text
- Keep plugin version at 0.0.1 unless a release decision says otherwise.
- No hardcoded final UX strings in plugin menus.
- No duplicate plugin IDs.
- No core changes for plugin-specific behavior.
- Dry-run must not modify disk.
- remove deletes one installed version.
- uninstall deletes everything managed by the plugin.
- path_entries prints paths only, one per line.
- status prints one short line for the main menu.
```

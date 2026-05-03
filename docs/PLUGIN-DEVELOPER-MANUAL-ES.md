# Manual del Desarrollador de Plugins ATM

Este manual explica como crear un plugin para **ATM — Atomy Tools Modules**.

Un plugin ATM es un modulo Bash local y confiable. El plugin gestiona la logica especifica de una herramienta: instalacion, versiones, PATH, launchers de escritorio, manifiestos, menus y traducciones.

## 1. Contrato del Plugin

Todo plugin vive en:

```text
plugins/<plugin_id>/
```

Archivos minimos:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

Locales recomendados:

```text
pt-br pt-pt es it fr de ru ja zh-cn ko
```

## 2. Reglas de Nombre

Use un `plugin_id` estable:

```text
example_tool
```

```text
- Use minusculas, numeros y underscores.
- El plugin id debe ser unico.
- Todas las funciones publicas usan atm_<plugin_id>_.
- No coloque logica especifica del plugin en lib/.
```

## 3. Estructura Inicial

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

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

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Use claves `ATM_PLUGIN_<ID>_*` y `atm_t`.

```bash
ATM_PLUGIN_EXAMPLE_TOOL_MENU_TITLE="Example Tool Installer"
ATM_PLUGIN_EXAMPLE_TOOL_CURRENT="Current"
ATM_PLUGIN_EXAMPLE_TOOL_STATUS_NOT_INSTALLED="not installed"
```

## 7. Funciones Obligatorias

Implemente funciones con prefijo `atm_<plugin_id>_` para archive_name, download_url, install_dir, current_path, cache, manifest, status, list_installed_versions, current_version, write_manifest, install, use, remove, uninstall, menu y path_entries.

## 8. Semantica

```text
install   Instala una version, actualiza current y escribe manifiesto.
use       Cambia current a una version instalada.
remove    Elimina una version especifica, nunca current.
uninstall Elimina todo lo gestionado por el plugin, con confirmacion.
status    Imprime una linea corta.
menu      Menu interactivo sin numeros duplicados.
path_entries Imprime solo rutas.
```

Respete siempre `ATM_DRY_RUN`.

## 9. Manifiesto

Use `atm_manifest_write` e incluya `ATM_PLUGIN_VERSION="0.0.1"`, version actual, ruta actual, install root y versiones instaladas.

## 10. Desktop

Launchers de escritorio solo en:

```text
~/.local/share/applications
```

No use sudo.

## 11. Validacion

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 12. Release

```text
- Version inicial del plugin: 0.0.1.
- Sin sudo.
- Sin strings finales hardcoded.
- Sin IDs duplicados.
- dry-run no modifica disco.
```


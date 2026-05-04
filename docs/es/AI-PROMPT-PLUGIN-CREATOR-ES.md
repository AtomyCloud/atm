# Prompt de IA — Creador de Plugins ATM

Usa este prompt para pedir a una IA que cree un plugin para **ATM — Atomy Tools Modules**.

```text
Eres un ingeniero senior de Bash y vas a crear un plugin para ATM — Atomy Tools Modules.

Contexto:
ATM es una herramienta modular en Bash para instalar y gestionar herramientas de desarrollo en Linux. Cada plugin vive en plugins/<plugin_id>/ y es responsable de la logica especifica de la herramienta: instalacion, versiones, menu, estado, cambio de version, eliminacion, desinstalacion, manifiestos, entradas PATH, launchers de escritorio cuando aplique y archivos de idioma.

Herramienta:
<describe la herramienta>

Plugin ID:
<ejemplo: nodejs, rust, deno, bun>

Nombre visible:
<ejemplo: Node.js>

Icono:
<ejemplo: 🟩>

Version inicial del plugin:
0.0.1

Versiones del menu:
<lista de versiones>

Version predeterminada:
<version>

Patron de descarga:
<URL o patron oficial. Si no estas seguro, dejalo marcado claramente como punto a revisar.>

Crea estos archivos:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Reglas obligatorias:

- Bash compatible con set -Eeuo pipefail.
- Todas las funciones publicas usan prefijo atm_<plugin_id>_.
- Usar local dentro de funciones.
- Citar variables con comillas.
- Respetar ATM_DRY_RUN.
- No usar sudo.
- No escribir fuera del modelo ATM sin motivo claro.
- No poner logica especifica del plugin en lib/.
- Launchers de escritorio solo en ~/.local/share/applications.
- Strings finales de UX via atm_t y claves ATM_PLUGIN_<ID>_*.

plugin.metadata debe incluir:

ATM_PLUGIN_ID="<plugin_id>"
ATM_PLUGIN_NAME_VALUE="<Nombre>"
ATM_PLUGIN_ICON_VALUE="<icono>"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="<orden>"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage <Nombre> versions"
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

Implementa:

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

Semantica:

- install acepta --version <version> y --version=<version>.
- install detecta version ya instalada antes de descargar.
- install actualiza current y escribe manifiesto.
- use solo cambia a versiones instaladas.
- remove no permite eliminar la version current.
- uninstall pide confirmacion.
- status imprime una linea corta y empieza con "✅ " cuando esta instalado.
- path_entries imprime solo rutas, una por linea.
- menu no puede tener numeros duplicados y debe incluir b) Back y q) Exit.

Manifiesto:

Usa atm_manifest_write e incluye:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

Al final proporciona comandos de validacion:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Entrega el contenido completo de cada archivo, en bloques separados.
```


# Prompt de IA — Convertir un Script Bash en Plugin ATM

Usa este prompt para pedir a una IA que convierta un script Bash existente en un plugin para **ATM — Atomy Tools Modules**.

---

## Prompt

```text
Eres un ingeniero Bash senior que convierte un script Bash existente en un plugin de ATM — Atomy Tools Modules.

Analiza el script y conviértelo en un plugin ATM completo y revisable. No envuelvas el script original como un único comando; refactoriza la lógica en funciones de plugin.

SCRIPT ORIGINAL:
<pega aquí el script Bash completo>

DATOS DEL PLUGIN:
- Herramienta: <nombre>
- Plugin id: <id en minúsculas>
- Nombre visible: <nombre>
- Icono: <icono>
- Versión inicial del plugin: 0.0.1
- Versión por defecto: <versión o vacío>
- Versiones conocidas: <lista o vacío>
- Entradas PATH: <sí/no y rutas>
- Desktop launcher: <sí/no y detalles>

Primero devuelve un Análisis de Conversión con comandos externos, descargas, rutas, versiones, variables, cambios PATH, lógica desktop, sudo, operaciones destructivas, rutas hardcoded y cambios necesarios para ATM_DRY_RUN.

Crea:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Si aplica, crea plugins/<plugin_id>/<plugin_id>.desktop.in.

plugin.metadata debe usar shell key=value, versión 0.0.1 y funciones atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use.

plugin.conf debe usar variables ATM_<PLUGIN_ID_EN_MAYÚSCULAS>_* sin secretos ni rutas hardcoded del usuario.

plugin.sh debe empezar con #!/usr/bin/env bash, usar prefijo atm_<plugin_id>_, variables locales, comillas, ${VAR:-}, compatibilidad con set -Eeuo pipefail, ATM_DRY_RUN, locales con atm_t, sin lógica específica en core y sin .desktop globales.

Semántica: install instala/configura y escribe manifest; use cambia current o no-op claro; remove elimina versión/payload; uninstall pide confirmación; status imprime una línea; path_entries imprime solo rutas; menu es interactivo e incluye b) Back y q) Exit.

Convierte comportamiento inseguro: elimina sudo oculto o hazlo explícito, mueve /usr /opt /etc a rutas de usuario cuando sea posible, usa path_entries en lugar de editar RC y usa atm_download_file para descargas.

Usa atm_manifest_write con ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION o ATM_CURRENT_STACK, ATM_CURRENT_PATH o ATM_INSTALL_ROOT, ATM_INSTALL_ROOT y ATM_INSTALLED_VERSIONS si existen versiones.

Devuelve análisis, árbol, contenido completo de archivos, comandos de validación y notas sobre cambios respecto al script original.

Validación:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

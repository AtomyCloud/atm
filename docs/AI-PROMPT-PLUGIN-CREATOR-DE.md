# KI-Prompt — ATM Plugin Creator

Verwende diesen Prompt, um eine KI ein Plugin für **ATM — Atomy Tools Modules** erstellen zu lassen.

```text
Du bist ein senior Bash-Engineer und erstellst ein Plugin fuer ATM — Atomy Tools Modules.

Kontext:
ATM ist ein modulares Bash-Werkzeug zum Installieren und Verwalten von Entwicklerwerkzeugen unter Linux. Jedes Plugin liegt in plugins/<plugin_id>/ und verwaltet die werkzeugspezifische Logik: Installation, Versionen, Menue, Status, Versionswechsel, Entfernen, Deinstallation, Manifeste, PATH-Eintraege, Desktop-Launcher falls noetig und Sprachdateien.

Werkzeug:
<beschreibe das Werkzeug>

Plugin ID:
<Beispiel: nodejs, rust, deno, bun>

Anzeigename:
<Beispiel: Node.js>

Icon:
<Beispiel: 🟩>

Initiale Plugin-Version:
0.0.1

Versionen im Menue:
<Versionsliste>

Standardversion:
<Version>

Download-Schema:
<offizielle URL oder offizielles Schema. Wenn unsicher, deutlich als Pruefpunkt markieren.>

Erstelle diese Dateien:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Pflichtregeln:

- Bash kompatibel mit set -Eeuo pipefail.
- Alle oeffentlichen Funktionen nutzen den Prefix atm_<plugin_id>_.
- local in Funktionen verwenden.
- Variablen quoten.
- ATM_DRY_RUN respektieren.
- Kein sudo verwenden.
- Nicht ohne klaren Grund ausserhalb des ATM-Modells schreiben.
- Keine pluginspezifische Logik in lib/.
- Desktop-Launcher nur in ~/.local/share/applications.
- UX-Texte ueber atm_t und Keys ATM_PLUGIN_<ID>_*.

plugin.metadata muss ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT und alle Funktionsreferenzen fuer menu/status/install/path/desktop/remove/uninstall/use enthalten.

Implementiere:

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

Semantik:

- install akzeptiert --version <version> und --version=<version>.
- install erkennt bereits installierte Versionen vor dem Download.
- install aktualisiert current und schreibt das Manifest.
- use wechselt nur zu installierten Versionen.
- remove darf die current-Version nicht entfernen.
- uninstall fragt nach Bestaetigung.
- status gibt eine kurze Zeile aus und beginnt bei Installation mit "✅ ".
- path_entries gibt nur Pfade aus, einen pro Zeile.
- menu darf keine doppelten Nummern haben und muss b) Back und q) Exit enthalten.

Manifest:

Nutze atm_manifest_write und enthalte:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

Am Ende Validierungsbefehle ausgeben:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Gib den vollstaendigen Inhalt jeder Datei in separaten Bloecken aus.
```


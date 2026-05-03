# ATM Plugin-Entwicklerhandbuch

Dieses Handbuch erklaert, wie ein Plugin fuer **ATM — Atomy Tools Modules** erstellt wird.

Ein ATM-Plugin ist ein lokales, vertrauenswuerdiges Bash-Modul. Es verwaltet werkzeugspezifische Logik: Installation, Versionen, PATH, Desktop-Launcher, Manifeste, Menues und Uebersetzungen.

## 1. Plugin-Vertrag

Jedes Plugin liegt in:

```text
plugins/<plugin_id>/
```

Minimale Dateien:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. Namensregeln

Verwende eine stabile `plugin_id`:

```text
example_tool
```

```text
- Kleinbuchstaben, Zahlen und Underscores.
- Plugin id muss eindeutig sein.
- Oeffentliche Funktionen nutzen atm_<plugin_id>_.
- Keine pluginspezifische Logik in lib/.
```

## 3. Anfangsstruktur

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

Enthaelt ID, Name, Icon, Version `0.0.1`, Reihenfolge, Beschreibung, Entry Point und Funktionsreferenzen fuer menu/status/install/path/desktop/remove/uninstall/use.

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Nutze `ATM_PLUGIN_<ID>_*` und `atm_t`. Keine finalen UX-Texte hardcoden.

## 7. Pflichtfunktionen

Implementiere `atm_<plugin_id>_` Funktionen fuer download, install_dir, current_path, cache, manifest, status, Versionsliste, aktuelle Version, install, use, remove, uninstall, menu und path_entries.

## 8. Semantik

```text
install   Installiert eine Version, aktualisiert current und schreibt Manifest.
use       Wechselt current zu einer installierten Version.
remove    Entfernt eine bestimmte Version, niemals current.
uninstall Entfernt alles vom Plugin Verwaltete, mit Bestaetigung.
status    Gibt eine kurze Zeile aus.
menu      Interaktives Menue ohne doppelte Nummern.
path_entries Gibt nur Pfade aus.
```

Immer `ATM_DRY_RUN` respektieren.

## 9. Manifest

Nutze `atm_manifest_write` mit `ATM_PLUGIN_VERSION="0.0.1"` und aktueller Version, aktuellem Pfad, Install Root und installierten Versionen.

## 10. Desktop

Desktop-Launcher nur in:

```text
~/.local/share/applications
```

Kein sudo verwenden.

## 11. Validierung

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
- Initiale Plugin-Version: 0.0.1.
- Kein sudo.
- Keine hardcodierten finalen UX-Texte.
- Keine doppelten IDs.
- dry-run veraendert die Festplatte nicht.
```


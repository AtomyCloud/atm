# KI-Prompt — Bash-Skript in ein ATM-Plugin umwandeln

Nutze diesen Prompt, um ein vorhandenes Bash-Skript in ein Plugin für **ATM — Atomy Tools Modules** umzuwandeln.

---

## Prompt

```text
Du bist ein Senior-Bash-Engineer und wandelst ein vorhandenes Bash-Skript in ein ATM-Plugin um.

Analysiere das Skript und erstelle ein vollständiges, prüfbares ATM-Plugin. Verpacke das Originalskript nicht als einen großen Befehl; refaktorisiere die Logik in Plugin-Funktionen.

ORIGINALSCRIPT:
<hier das komplette Bash-Skript einfügen>

PLUGIN-INFORMATIONEN:
- Tool-Name: <Name>
- Plugin id: <kleine id>
- Anzeigename: <Name>
- Icon: <Icon>
- Initiale Plugin-Version: 0.0.1
- Standardversion: <Version oder leer>
- Bekannte Versionen: <Liste oder leer>
- PATH-Einträge: <ja/nein und Pfade>
- Desktop-Launcher: <ja/nein und Details>

Gib zuerst eine Conversion Analysis zurück: externe Befehle, Downloads, Archive, Pfade, Versionen, Variablen, PATH, Desktop, sudo, destruktive Operationen, hardcodierte Pfade und Änderungen für ATM_DRY_RUN.

Erstelle:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Falls nötig: plugins/<plugin_id>/<plugin_id>.desktop.in.

plugin.metadata nutzt shell key=value, Version 0.0.1 und Funktionen atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use.

plugin.conf nutzt ATM_<PLUGIN_ID_IN_GROSSBUCHSTABEN>_*-Variablen ohne Secrets und ohne hardcodierte Benutzerpfade.

plugin.sh startet mit #!/usr/bin/env bash, nutzt atm_<plugin_id>_, lokale Variablen, Quotes, ${VAR:-}, set -Eeuo pipefail, ATM_DRY_RUN, atm_t für Texte, keine plugin-spezifische Core-Logik und keine globalen .desktop-Dateien.

Semantik: install installiert/konfiguriert und schreibt Manifest; use wechselt current oder klarer no-op; remove entfernt Version/Payload; uninstall fragt nach Bestätigung; status druckt eine Zeile; path_entries druckt nur Pfade; menu ist interaktiv und enthält b) Back und q) Exit.

Unsicheres Verhalten umwandeln: verstecktes sudo entfernen oder explizit machen, /usr /opt /etc wenn möglich in Benutzerpfade verschieben, path_entries statt RC-Änderungen nutzen, atm_download_file für Downloads verwenden.

Nutze atm_manifest_write mit ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION oder ATM_CURRENT_STACK, ATM_CURRENT_PATH oder ATM_INSTALL_ROOT, ATM_INSTALL_ROOT und ATM_INSTALLED_VERSIONS falls vorhanden.

Gib Analyse, Dateibaum, vollständige Inhalte, Validierungsbefehle und Änderungsnotizen zurück.

Validierung:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

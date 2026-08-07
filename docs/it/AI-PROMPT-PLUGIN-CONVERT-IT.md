# Prompt AI — Convertire uno Script Bash in un Plugin ATM

Usa questo prompt per chiedere a un'IA di convertire uno script Bash esistente in un plugin per **ATM — Atomy Tools Modules**.

---

## Prompt

```text
Sei un ingegnere Bash senior che converte uno script Bash esistente in un plugin ATM.

Analizza lo script e convertilo in un plugin completo e revisionabile. Non avvolgere lo script originale in un solo comando; rifattorizza la logica in funzioni del plugin.

SCRIPT ORIGINALE:
<incolla qui lo script Bash completo>

DATI DEL PLUGIN:
- Nome strumento: <nome>
- Plugin id: <id minuscolo>
- Nome visualizzato: <nome>
- Icona: <icona>
- Versione iniziale plugin: 0.0.1
- Versione predefinita: <versione o vuoto>
- Versioni note: <lista o vuoto>
- PATH entries: <sì/no e percorsi>
- Desktop launcher: <sì/no e dettagli>

Prima restituisci una Conversion Analysis con comandi esterni, download, archivi, percorsi, versioni, variabili, PATH, desktop, sudo, operazioni distruttive, percorsi hardcoded e modifiche necessarie per ATM_DRY_RUN.

Crea:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Se serve, crea plugins/<plugin_id>/<plugin_id>.desktop.in.

plugin.metadata usa shell key=value, versione 0.0.1 e funzioni atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use.

plugin.conf usa variabili ATM_<PLUGIN_ID_UPPERCASE>_* senza segreti e senza percorsi utente hardcoded.

plugin.sh deve iniziare con #!/usr/bin/env bash, usare prefisso atm_<plugin_id>_, variabili locali, quote, ${VAR:-}, compatibilità con set -Eeuo pipefail, ATM_DRY_RUN, atm_t per testi utente, nessuna logica specifica nel core e nessun .desktop globale.

Semantica: install installa/configura e scrive manifest; use cambia current o no-op chiaro; remove elimina versione/payload; uninstall chiede conferma; status stampa una riga; path_entries stampa solo percorsi; menu è interattivo e include b) Back e q) Exit.

Converti comportamenti non sicuri: rimuovi sudo nascosto o rendilo esplicito, sposta /usr /opt /etc verso percorsi utente quando possibile, usa path_entries invece di modificare RC, usa atm_download_file per download.

Usa atm_manifest_write con ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION o ATM_CURRENT_STACK, ATM_CURRENT_PATH o ATM_INSTALL_ROOT, ATM_INSTALL_ROOT e ATM_INSTALLED_VERSIONS se esistono versioni.

Restituisci analisi, albero file, contenuto completo, validazione e note sui cambiamenti.

Validazione:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

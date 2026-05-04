# Prompt AI — Creatore di Plugin ATM

Usa questo prompt per chiedere a una AI di creare un plugin per **ATM — Atomy Tools Modules**.

```text
Sei un ingegnere senior Bash e devi creare un plugin per ATM — Atomy Tools Modules.

Contesto:
ATM e uno strumento modulare Bash per installare e gestire strumenti di sviluppo su Linux. Ogni plugin vive in plugins/<plugin_id>/ ed e responsabile della logica specifica dello strumento: installazione, versioni, menu, stato, cambio versione, rimozione, disinstallazione, manifest, PATH entries, desktop launcher quando applicabile e file di lingua.

Strumento:
<descrivi lo strumento>

Plugin ID:
<esempio: nodejs, rust, deno, bun>

Nome visualizzato:
<esempio: Node.js>

Icona:
<esempio: 🟩>

Versione iniziale del plugin:
0.0.1

Versioni nel menu:
<lista versioni>

Versione predefinita:
<versione>

Schema di download:
<URL o schema ufficiale. Se non sei sicuro, segnalo chiaramente come punto da rivedere.>

Crea questi file:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Regole obbligatorie:

- Bash compatibile con set -Eeuo pipefail.
- Tutte le funzioni pubbliche usano il prefisso atm_<plugin_id>_.
- Usa local dentro le funzioni.
- Cita le variabili con virgolette.
- Rispetta ATM_DRY_RUN.
- Non usare sudo.
- Non scrivere fuori dal modello ATM senza motivo chiaro.
- Non inserire logica specifica del plugin in lib/.
- Desktop launcher solo in ~/.local/share/applications.
- Stringhe UX finali tramite atm_t e chiavi ATM_PLUGIN_<ID>_*.

plugin.metadata deve includere i campi ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT e tutti i riferimenti alle funzioni menu/status/install/path/desktop/remove/uninstall/use.

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

- install accetta --version <versione> e --version=<versione>.
- install rileva versioni gia installate prima del download.
- install aggiorna current e scrive il manifest.
- use cambia solo verso versioni installate.
- remove non permette di rimuovere la versione current.
- uninstall chiede conferma.
- status stampa una riga breve e inizia con "✅ " quando installato.
- path_entries stampa solo percorsi, uno per riga.
- menu non deve avere numeri duplicati e deve includere b) Back e q) Exit.

Manifest:

Usa atm_manifest_write e includi:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

Alla fine fornisci i comandi di validazione:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Fornisci il contenuto completo di ogni file in blocchi separati.
```


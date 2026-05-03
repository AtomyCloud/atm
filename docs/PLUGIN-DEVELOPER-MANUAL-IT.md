# Manuale Sviluppatore Plugin ATM

Questo manuale spiega come creare un plugin per **ATM — Atomy Tools Modules**.

Un plugin ATM e un modulo Bash locale e affidabile. Il plugin gestisce la logica specifica dello strumento: installazione, versioni, PATH, desktop launcher, manifest, menu e traduzioni.

## 1. Contratto del Plugin

Ogni plugin vive in:

```text
plugins/<plugin_id>/
```

File minimi:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. Regole di Nome

Usa un `plugin_id` stabile, ad esempio:

```text
example_tool
```

```text
- Usa minuscole, numeri e underscore.
- Il plugin id deve essere unico.
- Tutte le funzioni pubbliche usano atm_<plugin_id>_.
- Non mettere logica specifica del plugin in lib/.
```

## 3. Struttura Iniziale

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

Deve contenere ID, nome, icona, versione `0.0.1`, ordine, descrizione, entrypoint e riferimenti alle funzioni menu/status/install/path/desktop/remove/uninstall/use.

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Usa chiavi `ATM_PLUGIN_<ID>_*` e `atm_t`. Non hardcodare stringhe finali nei menu.

## 7. Funzioni Obbligatorie

Implementa funzioni `atm_<plugin_id>_` per download, install_dir, current_path, cache, manifest, status, version list, current version, install, use, remove, uninstall, menu e path_entries.

## 8. Semantica

```text
install   Installa una versione, aggiorna current e scrive il manifest.
use       Cambia current verso una versione installata.
remove    Rimuove una versione specifica, mai current.
uninstall Rimuove tutto cio che il plugin gestisce, con conferma.
status    Stampa una riga breve.
menu      Menu interattivo senza numeri duplicati.
path_entries Stampa solo percorsi.
```

Rispetta sempre `ATM_DRY_RUN`.

## 9. Manifest

Usa `atm_manifest_write` con `ATM_PLUGIN_VERSION="0.0.1"` e includi versione corrente, path corrente, install root e versioni installate.

## 10. Desktop

I desktop launcher devono essere installati solo in:

```text
~/.local/share/applications
```

Non usare sudo.

## 11. Validazione

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
- Versione iniziale plugin: 0.0.1.
- Nessun sudo.
- Nessuna stringa UX finale hardcoded.
- Nessun ID duplicato.
- dry-run non modifica il disco.
```


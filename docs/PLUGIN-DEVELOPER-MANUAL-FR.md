# Manuel Developpeur de Plugin ATM

Ce manuel explique comment creer un plugin pour **ATM — Atomy Tools Modules**.

Un plugin ATM est un module Bash local et fiable. Il gere la logique specifique d'un outil: installation, versions, PATH, lanceurs desktop, manifestes, menus et traductions.

## 1. Contrat du Plugin

Chaque plugin vit dans:

```text
plugins/<plugin_id>/
```

Fichiers minimum:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. Regles de Nom

Utilise un `plugin_id` stable:

```text
example_tool
```

```text
- Minuscules, chiffres et underscores.
- Plugin id unique.
- Fonctions publiques prefixees par atm_<plugin_id>_.
- Pas de logique specifique du plugin dans lib/.
```

## 3. Structure Initiale

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

Inclure ID, nom, icone, version `0.0.1`, ordre, description, entrypoint et fonctions menu/status/install/path/desktop/remove/uninstall/use.

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Utilise `ATM_PLUGIN_<ID>_*` et `atm_t`. Ne pas hardcoder les textes finaux des menus.

## 7. Fonctions Obligatoires

Implementer des fonctions `atm_<plugin_id>_` pour download, install_dir, current_path, cache, manifest, status, liste des versions, version courante, install, use, remove, uninstall, menu et path_entries.

## 8. Semantique

```text
install   Installe une version, met a jour current et ecrit le manifeste.
use       Bascule current vers une version installee.
remove    Supprime une version specifique, jamais current.
uninstall Supprime tout ce que le plugin gere, avec confirmation.
status    Imprime une ligne courte.
menu      Menu interactif sans numeros dupliques.
path_entries Imprime uniquement des chemins.
```

Respecte toujours `ATM_DRY_RUN`.

## 9. Manifeste

Utilise `atm_manifest_write` avec `ATM_PLUGIN_VERSION="0.0.1"` et inclus version courante, chemin courant, install root et versions installees.

## 10. Desktop

Les lanceurs desktop vont uniquement dans:

```text
~/.local/share/applications
```

Ne pas utiliser sudo.

## 11. Validation

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
- Version initiale du plugin: 0.0.1.
- Pas de sudo.
- Pas de textes UX finaux hardcodes.
- Pas d'IDs dupliques.
- dry-run ne modifie pas le disque.
```


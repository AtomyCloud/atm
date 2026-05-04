# Prompt IA — Créateur de Plugin ATM

Utilise ce prompt pour demander à une IA de créer un plugin pour **ATM — Atomy Tools Modules**.

```text
Tu es un ingenieur Bash senior et tu dois creer un plugin pour ATM — Atomy Tools Modules.

Contexte:
ATM est un outil Bash modulaire pour installer et gerer des outils de developpement sous Linux. Chaque plugin vit dans plugins/<plugin_id>/ et gere la logique specifique de l'outil: installation, versions, menu, statut, changement de version, suppression, desinstallation, manifestes, entrees PATH, lanceurs desktop si necessaire et fichiers de langue.

Outil:
<decris l'outil>

Plugin ID:
<exemple: nodejs, rust, deno, bun>

Nom affiche:
<exemple: Node.js>

Icone:
<exemple: 🟩>

Version initiale du plugin:
0.0.1

Versions dans le menu:
<liste de versions>

Version par defaut:
<version>

Schema de telechargement:
<URL ou schema officiel. Si incertain, marque-le clairement comme point a verifier.>

Cree ces fichiers:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Regles obligatoires:

- Bash compatible avec set -Eeuo pipefail.
- Toutes les fonctions publiques utilisent le prefixe atm_<plugin_id>_.
- Utiliser local dans les fonctions.
- Mettre les variables entre guillemets.
- Respecter ATM_DRY_RUN.
- Ne pas utiliser sudo.
- Ne pas ecrire hors du modele ATM sans raison claire.
- Ne pas placer de logique specifique du plugin dans lib/.
- Lanceurs desktop uniquement dans ~/.local/share/applications.
- Textes UX via atm_t et cles ATM_PLUGIN_<ID>_*.

plugin.metadata doit inclure les champs ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT et toutes les fonctions menu/status/install/path/desktop/remove/uninstall/use.

Implemente:

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

Semantique:

- install accepte --version <version> et --version=<version>.
- install detecte une version deja installee avant le telechargement.
- install met a jour current et ecrit le manifeste.
- use ne bascule que vers des versions installees.
- remove interdit la suppression de la version current.
- uninstall demande confirmation.
- status imprime une ligne courte et commence par "✅ " si installe.
- path_entries imprime uniquement des chemins, un par ligne.
- menu ne doit pas avoir de numeros dupliques et doit inclure b) Back et q) Exit.

Manifeste:

Utilise atm_manifest_write et inclus:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

Finis avec les commandes de validation:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Fournis le contenu complet de chaque fichier dans des blocs separes.
```


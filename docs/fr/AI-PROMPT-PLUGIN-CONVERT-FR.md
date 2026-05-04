# Prompt IA — Convertir un Script Bash en Plugin ATM

Utilisez ce prompt pour demander à une IA de convertir un script Bash existant en plugin pour **ATM — Atomy Tools Modules**.

---

## Prompt

```text
Vous êtes un ingénieur Bash senior qui convertit un script Bash existant en plugin ATM.

Analysez le script et convertissez-le en plugin ATM complet et vérifiable. Ne vous contentez pas d'encapsuler le script original dans une seule commande; refactorisez la logique en fonctions de plugin.

SCRIPT ORIGINAL:
<collez ici le script Bash complet>

INFORMATIONS DU PLUGIN:
- Outil: <nom>
- Plugin id: <id en minuscules>
- Nom affiché: <nom>
- Icône: <icône>
- Version initiale du plugin: 0.0.1
- Version par défaut: <version ou vide>
- Versions connues: <liste ou vide>
- Entrées PATH: <oui/non et chemins>
- Desktop launcher: <oui/non et détails>

Retournez d'abord une Conversion Analysis couvrant commandes externes, téléchargements, archives, chemins, versions, variables, PATH, desktop, sudo, opérations destructives, chemins hardcodés et changements requis pour ATM_DRY_RUN.

Créez:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Si nécessaire, créez plugins/<plugin_id>/<plugin_id>.desktop.in.

plugin.metadata utilise shell key=value, version 0.0.1 et fonctions atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use.

plugin.conf utilise des variables ATM_<PLUGIN_ID_EN_MAJUSCULES>_* sans secrets ni chemins utilisateur hardcodés.

plugin.sh commence par #!/usr/bin/env bash, utilise le préfixe atm_<plugin_id>_, des variables locales, des guillemets, ${VAR:-}, set -Eeuo pipefail, ATM_DRY_RUN, atm_t pour les textes, aucune logique spécifique dans core et aucun .desktop système.

Sémantique: install installe/configure et écrit le manifest; use change current ou fait un no-op clair; remove supprime une version/payload; uninstall demande confirmation; status imprime une ligne; path_entries imprime seulement des chemins; menu est interactif et inclut b) Back et q) Exit.

Convertissez les comportements dangereux: supprimez sudo caché ou rendez-le explicite, déplacez /usr /opt /etc vers les chemins utilisateur si possible, utilisez path_entries au lieu de modifier les RC, utilisez atm_download_file pour les téléchargements.

Utilisez atm_manifest_write avec ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION ou ATM_CURRENT_STACK, ATM_CURRENT_PATH ou ATM_INSTALL_ROOT, ATM_INSTALL_ROOT et ATM_INSTALLED_VERSIONS si applicable.

Retournez analyse, arbre de fichiers, contenu complet, commandes de validation et notes de changement.

Validation:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

# Prompt de IA — Criador de Plugins ATM

Use este prompt para pedir a uma IA que crie um plugin para o **ATM — Atomy Tools Modules**.

```text
És um engenheiro senior de Bash e vais criar um plugin para o ATM — Atomy Tools Modules.

Contexto:
ATM é uma ferramenta modular em Bash para instalar e gerir ferramentas de desenvolvimento em Linux. Cada plugin vive em plugins/<plugin_id>/ e é responsável pela lógica específica da ferramenta: instalação, versões, menu, estado, troca de versão, remoção, desinstalação, manifestos, entradas PATH, desktop launchers quando aplicável e ficheiros de idioma.

Ferramenta:
<descreve a ferramenta>

Plugin ID:
<exemplo: nodejs, rust, deno, bun>

Nome apresentado:
<exemplo: Node.js>

Ícone:
<exemplo: 🟩>

Versão inicial do plugin:
0.0.1

Versões no menu:
<lista de versões>

Versão predefinida:
<versão>

Padrão de download:
<URL ou padrão oficial. Se não tiveres certeza, deixa claramente marcado como ponto a rever.>

Cria estes ficheiros:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Opcionalmente cria também:

plugins/<plugin_id>/lang/pt-br.lang
plugins/<plugin_id>/lang/pt-pt.lang
plugins/<plugin_id>/lang/es.lang
plugins/<plugin_id>/lang/it.lang
plugins/<plugin_id>/lang/fr.lang
plugins/<plugin_id>/lang/de.lang
plugins/<plugin_id>/lang/ru.lang
plugins/<plugin_id>/lang/ja.lang
plugins/<plugin_id>/lang/zh-cn.lang
plugins/<plugin_id>/lang/ko.lang

Regras obrigatórias:

- O plugin deve usar Bash compatível com set -Eeuo pipefail.
- Todas as funções públicas devem usar o prefixo atm_<plugin_id>_.
- Usar local dentro das funções.
- Citar variáveis com aspas.
- Respeitar ATM_DRY_RUN.
- Não usar sudo.
- Não escrever fora do modelo ATM sem motivo claro.
- Não colocar lógica específica do plugin em lib/.
- Não instalar desktop launchers fora de ~/.local/share/applications.
- Não instalar ícones em temas de sistema.
- Strings de UX devem usar atm_t e chaves ATM_PLUGIN_<ID>_*.

plugin.metadata deve conter:

ATM_PLUGIN_ID="<plugin_id>"
ATM_PLUGIN_NAME_VALUE="<Nome>"
ATM_PLUGIN_ICON_VALUE="<ícone>"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="<ordem>"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage <Nome> versions"
ATM_PLUGIN_ENTRYPOINT="plugin.sh"
ATM_PLUGIN_FULL_SETUP_VALUE="1"
ATM_PLUGIN_DEPENDS_VALUE=""
ATM_PLUGIN_OPTIONAL_DEPENDS_VALUE=""
ATM_PLUGIN_MENU_FUNC_VALUE="atm_<plugin_id>_menu"
ATM_PLUGIN_STATUS_FUNC_VALUE="atm_<plugin_id>_status"
ATM_PLUGIN_INSTALL_FUNC_VALUE="atm_<plugin_id>_install"
ATM_PLUGIN_PATH_FUNC_VALUE="atm_<plugin_id>_path_entries"
ATM_PLUGIN_DESKTOP_FUNC_VALUE=""
ATM_PLUGIN_REMOVE_FUNC_VALUE="atm_<plugin_id>_remove"
ATM_PLUGIN_UNINSTALL_FUNC_VALUE="atm_<plugin_id>_uninstall"
ATM_PLUGIN_USE_FUNC_VALUE="atm_<plugin_id>_use"

Implementa em plugin.sh:

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

Semântica:

- install aceita --version <versão> e --version=<versão>.
- install detecta versão já instalada antes de descarregar.
- install actualiza current e escreve manifesto.
- use só troca para versões instaladas.
- remove não permite remover a versão current.
- uninstall pede confirmação.
- status imprime uma linha curta e começa com "✅ " quando instalado.
- path_entries imprime apenas caminhos, um por linha.
- menu não pode ter números duplicados e deve ter b) Back e q) Exit.

Manifesto:

Usa atm_manifest_write e inclui:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

No final, fornece os comandos de validação:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Entrega o conteúdo completo de cada ficheiro, em blocos separados.
```


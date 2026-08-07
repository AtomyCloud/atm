# Prompt de IA — Converter um Script Bash num Plugin ATM

Use este prompt para pedir a uma IA que converta um script Bash existente num plugin para **ATM — Atomy Tools Modules**.

O objectivo é preservar o comportamento útil do script original e adaptá-lo à arquitectura de plugins do ATM.

---

## Prompt

```text
És um engenheiro Bash sénior a converter um script Bash existente num plugin do ATM — Atomy Tools Modules.

Analisa o script abaixo e converte-o num plugin ATM completo e revisto. Não envolvas o script original como um único comando grande. Refactoriza a lógica em funções de plugin com responsabilidades claras.

SCRIPT ORIGINAL:
<cola aqui o script Bash completo>

INFORMAÇÃO DO PLUGIN:
- Nome da ferramenta: <nome>
- Plugin id: <id em minúsculas>
- Nome apresentado: <nome>
- Ícone: <ícone>
- Versão inicial do plugin: 0.0.1
- Versão predefinida: <versão ou vazio>
- Versões conhecidas: <lista ou vazio>
- Precisa de entradas PATH: <sim/não e caminhos>
- Precisa de desktop launcher: <sim/não e detalhes>

Primeiro devolve uma Análise de Conversão com:
- O que o script instala ou configura.
- Comandos externos, downloads, arquivos, caminhos e versões.
- Variáveis de ambiente, alterações de PATH e lógica desktop.
- Uso de sudo, operações destrutivas e caminhos hardcoded.
- O que deve mudar para respeitar ATM_DRY_RUN.

Cria estes ficheiros:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Se necessário, cria também plugins/<plugin_id>/<plugin_id>.desktop.in.

plugin.metadata usa shell key=value, versão 0.0.1 e funções atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use.

plugin.conf usa variáveis ATM_<PLUGIN_ID_EM_MAIÚSCULAS>_* e não guarda segredos nem caminhos do utilizador actual.

plugin.sh deve começar com #!/usr/bin/env bash, usar prefixo atm_<plugin_id>_, variáveis locais, aspas, ${VAR:-}, ser compatível com set -Eeuo pipefail, respeitar ATM_DRY_RUN, não escrever lógica específica no core, não criar .desktop de sistema e usar locales para texto final.

Semântica:
install instala/configura e escreve manifest.
use muda current quando houver versões ou devolve no-op claro.
remove remove uma versão/payload.
uninstall pede confirmação e remove apenas o que o plugin gere.
status imprime uma linha curta.
path_entries imprime apenas caminhos.
menu é interactivo e inclui b) Back e q) Exit.

Converte comportamento inseguro: remove sudo escondido ou torna-o explícito, move /usr /opt /etc para paths de utilizador quando possível, usa path_entries em vez de editar RC, e usa atm_download_file para downloads.

Usa atm_manifest_write com ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION ou ATM_CURRENT_STACK, ATM_CURRENT_PATH ou ATM_INSTALL_ROOT, ATM_INSTALL_ROOT e ATM_INSTALLED_VERSIONS quando aplicável.

Cria lang/en-us.lang e usa atm_t.

Retorna análise, árvore de ficheiros, conteúdo completo, validação e notas de alterações.

Validação:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

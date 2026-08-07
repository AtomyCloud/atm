# Prompt de IA — Converter um Script Bash em Plugin ATM

Use este prompt para pedir a uma IA que converta um script Bash existente em um plugin para **ATM — Atomy Tools Modules**.

O objetivo é preservar o comportamento útil do script original e adaptá-lo à arquitetura de plugins do ATM.

---

## Prompt

```text
Você é um engenheiro Bash sênior convertendo um script Bash existente em um plugin do ATM — Atomy Tools Modules.

Analise o script abaixo e converta-o em um plugin ATM completo e revisável. Não embrulhe o script original como um único comando grande. Refatore a lógica em funções de plugin com responsabilidades claras.

SCRIPT ORIGINAL:
<cole aqui o script Bash completo>

INFORMAÇÕES DO PLUGIN:
- Nome da ferramenta: <nome>
- Plugin id: <id em minúsculas, ex.: nodejs, docker_cli, custom_tool>
- Nome exibido: <nome exibido>
- Ícone: <ícone>
- Versão inicial do plugin: 0.0.1
- Versão padrão: <versão ou vazio>
- Versões conhecidas: <lista ou vazio>
- Precisa de PATH entries: <sim/não e caminhos>
- Precisa de desktop launcher: <sim/não e detalhes>

Primeiro retorne uma Análise de Conversão explicando:
- O que o script instala ou configura.
- Comandos externos, downloads, arquivos, caminhos e versões.
- Variáveis de ambiente, mudanças de PATH e lógica desktop.
- Uso de sudo, operações destrutivas e caminhos hardcoded.
- O que precisa mudar para respeitar ATM_DRY_RUN.

Crie estes arquivos:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Se necessário, crie também:
plugins/<plugin_id>/<plugin_id>.desktop.in

plugin.metadata deve usar sintaxe shell key=value e versão de plugin 0.0.1. Exporte as funções padrão:
atm_<plugin_id>_menu, atm_<plugin_id>_status, atm_<plugin_id>_install, atm_<plugin_id>_path_entries, atm_<plugin_id>_remove, atm_<plugin_id>_uninstall e atm_<plugin_id>_use.

plugin.conf deve conter padrões usando variáveis ATM_<PLUGIN_ID_EM_MAIÚSCULAS>_*. Não armazene segredos e não use caminhos hardcoded do usuário atual.

Regras para plugin.sh:
- Começar com #!/usr/bin/env bash.
- Usar prefixo atm_<plugin_id>_ nas funções públicas.
- Usar variáveis locais e aspas nas expansões.
- Ser compatível com set -Eeuo pipefail.
- Usar ${VAR:-} para variáveis opcionais.
- Respeitar ATM_DRY_RUN.
- Não colocar lógica específica do plugin no core.
- Não criar .desktop global/sistema.
- Usar keys de locale para textos finais de usuário.

Semântica obrigatória:
install instala/configura a ferramenta, escreve manifest e suporta --version quando houver versões.
use troca a versão current quando houver versões, ou retorna um no-op claro.
remove remove uma versão ou payload quando aplicável.
uninstall pede confirmação e remove apenas arquivos gerenciados pelo plugin.
status imprime uma linha curta para o menu.
path_entries imprime apenas caminhos, um por linha.
menu é interativo, evita numeração duplicada e inclui b) Back e q) Exit.

Conversão de comportamento inseguro:
- Se houver sudo, remova elevação escondida ou torne-a explícita e explique.
- Converta escritas em /usr, /opt, /etc e desktop de sistema para paths de usuário do ATM quando possível.
- Prefira path_entries + atm path apply em vez de editar shell RC diretamente.
- Use atm_download_file para downloads quando possível.

Use atm_manifest_write e inclua pelo menos:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION ou ATM_CURRENT_STACK
ATM_CURRENT_PATH ou ATM_INSTALL_ROOT
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS quando houver versões

Crie lang/en-us.lang com todos os textos visíveis e use atm_t.

Retorne:
1. Análise de Conversão
2. Árvore de arquivos
3. Conteúdo completo de cada arquivo
4. Comandos de validação
5. Notas sobre comportamentos alterados do script original

Validação:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

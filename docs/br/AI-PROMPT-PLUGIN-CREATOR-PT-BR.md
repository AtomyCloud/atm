# AI Prompt — ATM Plugin Creator

Use este prompt para pedir a uma IA que crie um plugin para o **ATM — Atomy Tools Modules**.

O objetivo é gerar um plugin completo, revisável e compatível com a arquitetura atual do ATM.

---

## Prompt

```text
Voce e um engenheiro senior de Bash e esta criando um plugin para o projeto ATM — Atomy Tools Modules.

ATM e uma ferramenta modular em Bash para instalar e gerenciar ferramentas de desenvolvimento em Linux. O projeto usa plugins locais confiaveis, cada um dentro de plugins/<plugin_id>/, e cada plugin e responsavel pela logica especifica da ferramenta: paths de instalacao, download, versoes, status, menu, troca de versao, remocao, uninstall, manifestos, PATH entries, desktop launchers quando aplicavel, e locales.

Crie um plugin completo para a ferramenta:

FERRAMENTA:
<descreva aqui a ferramenta, por exemplo: Node.js, Rust, Docker CLI, Deno, Bun, Python, etc.>

PLUGIN ID desejado:
<exemplo: nodejs, rust, deno, bun>

Nome exibido:
<exemplo: Node.js>

Icone:
<exemplo: 🟩>

Versao inicial do plugin:
0.0.1

Versoes da ferramenta que devem aparecer no menu:
<exemplo: 22.11.0 22.10.0 20.18.1 20.17.0 18.20.5>

Versao padrao:
<exemplo: 22.11.0>

Arquitetura alvo:
linux-x64 ou linux-amd64, conforme a ferramenta.

URL/padrao de download:
<explique o padrao de URL oficial da ferramenta. Se nao souber, deixe uma funcao de download claramente marcada para revisao.>

Requisitos:

1. O plugin deve seguir a arquitetura do ATM.

Arquivos obrigatorios:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Arquivos de locale recomendados para release:

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

2. plugin.metadata deve usar shell key=value.

Use este formato, ajustando nomes e funcoes:

ATM_PLUGIN_ID="<plugin_id>"
ATM_PLUGIN_NAME_VALUE="<Nome exibido>"
ATM_PLUGIN_ICON_VALUE="<icone>"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="<numero de ordem sugerido>"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage <Nome exibido> versions"
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

Se a ferramenta tiver interface grafica e precisar de launcher desktop, tambem implemente:

ATM_PLUGIN_DESKTOP_FUNC_VALUE="atm_<plugin_id>_desktop"

3. plugin.conf deve conter todas as configuracoes padrao.

Use variaveis prefixadas com ATM_<PLUGIN_ID_EM_UPPERCASE>_.

Exemplo:

ATM_<PLUGIN>_DEFAULT_VERSION="<versao>"
ATM_<PLUGIN>_ARCH="linux-x64"
ATM_<PLUGIN>_INSTALL_ROOT="$ATM_APPS_DIR/<plugin_id>"
ATM_<PLUGIN>_CACHE_DIR="$ATM_DOWNLOAD_DIR/<plugin_id>"
ATM_<PLUGIN>_MANIFEST_FILE="$ATM_MANIFEST_DIR/<plugin_id>.manifest"
ATM_<PLUGIN>_VERSION_OPTIONS="<lista de versoes>"

4. plugin.sh deve ser Bash compativel e seguir estas regras:

- Comecar com #!/usr/bin/env bash.
- Todas as funcoes publicas devem usar prefixo atm_<plugin_id>_.
- Usar variaveis locais dentro das funcoes.
- Sempre colocar variaveis entre aspas.
- Ser compativel com set -Eeuo pipefail.
- Evitar variaveis opcionais sem fallback. Use ${VAR:-}.
- Respeitar ATM_DRY_RUN.
- Nao escrever em paths fora do modelo ATM sem motivo claro.
- Nao usar sudo.
- Nao implementar logica especifica do plugin no core.
- Nao criar .desktop global/sistema.
- Nao instalar icones em temas nativos do sistema.

5. O plugin deve implementar estas funcoes:

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

Se for uma ferramenta GUI:

atm_<plugin_id>_desktop

6. Semantica obrigatoria:

install:
- Recebe --version <versao> e --version=<versao>.
- Usa a versao default se nenhuma for passada.
- Normaliza/valida a versao.
- Detecta se a versao ja esta instalada antes de baixar.
- Baixa para cache em "$ATM_DOWNLOAD_DIR/<plugin_id>".
- Extrai/instala em "$ATM_APPS_DIR/<plugin_id>/<version>" ou padrao equivalente.
- Atualiza symlink current.
- Escreve manifesto.
- Nao executa comandos de versao em dry-run.

use:
- Recebe atm use <plugin_id> <version>.
- Verifica se a versao esta instalada.
- Atualiza symlink current.
- Escreve manifesto.
- Se houver desktop launcher, regenera o launcher.

remove:
- Remove uma versao especifica.
- Nao permite remover a versao current.
- Atualiza manifesto depois da remocao.

uninstall:
- Pede confirmacao interativa.
- Remove tudo que o plugin gerencia.
- Remove manifesto.
- Remove desktop launcher se aplicavel.

status:
- Imprime uma linha curta para o menu principal.
- Se instalado, comece com "✅ ".
- Se nao instalado, use locale key ATM_PLUGIN_<ID>_STATUS_NOT_INSTALLED.

path_entries:
- Imprime apenas paths, um por linha.
- Nao imprime mensagens humanas.

menu:
- Deve ser interativo.
- Deve evitar numeracao duplicada.
- Deve usar opcoes dinamicas quando a lista de versoes mudar.
- Sempre incluir:
  b) Back
  q) Exit
- Depois de executar uma acao, aguardar "Press any key to continue...".

7. Manifesto:

Use atm_manifest_write.

O manifesto deve incluir pelo menos:

ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

8. Download e extracao:

Use helpers do core quando possivel:

atm_download_file "$url" "$cache_file"
atm_archive_extract_tar_gz "$cache_file" "$dest" 1

Se o formato for zip:
- Verifique se existe helper de zip no projeto.
- Se nao existir, implemente com unzip de forma simples e dry-run aware.

9. Locales:

Nao hardcode strings finais no menu.

Crie as chaves com prefixo:

ATM_PLUGIN_<ID>_*

Exemplo minimo para en-us:

ATM_PLUGIN_<ID>_MENU_TITLE="<Tool> Installer"
ATM_PLUGIN_<ID>_CURRENT="Current"
ATM_PLUGIN_<ID>_LATEST_STABLE="Latest Stable"
ATM_PLUGIN_<ID>_CHOOSE_VERSION="Choose specific version"
ATM_PLUGIN_<ID>_LIST_INSTALLED="List installed versions"
ATM_PLUGIN_<ID>_REMOVE_VERSION="Remove specific version"
ATM_PLUGIN_<ID>_UNINSTALL="Uninstall <Tool> completely"
ATM_PLUGIN_<ID>_ENTER_VERSION="Enter <Tool> version:"
ATM_PLUGIN_<ID>_STATUS_NOT_INSTALLED="not installed"
ATM_PLUGIN_<ID>_INSTALLING="Installing <Tool>"
ATM_PLUGIN_<ID>_ALREADY_INSTALLED="<Tool> already installed"
ATM_PLUGIN_<ID>_INSTALLED="<Tool> installed"
ATM_PLUGIN_<ID>_USING="Using <Tool>"
ATM_PLUGIN_<ID>_REMOVED="<Tool> version removed"
ATM_PLUGIN_<ID>_UNINSTALL_WARNING="This will remove all <Tool> versions managed by ATM."
ATM_PLUGIN_<ID>_UNINSTALLED="<Tool> uninstalled"
ATM_PLUGIN_<ID>_CANCELLED="Cancelled."

10. Desktop launcher, apenas se aplicavel:

Se a ferramenta for GUI, crie:

plugins/<plugin_id>/<plugin_id>.desktop.in

O plugin deve renderizar a template para arquivo temporario com mktemp e chamar:

atm_desktop_install_file "$rendered_file" "<target>.desktop"

Regras:
- Destino final e ~/.local/share/applications.
- Nao usar sudo.
- Nao instalar icones em temas do sistema.
- Plugin decide Exec, Icon, StartupWMClass e MimeType.

11. Saida esperada:

Forneca primeiro uma lista de arquivos que serao criados.

Depois forneca o conteudo completo de cada arquivo, um por vez, nesta ordem:

1. plugins/<plugin_id>/plugin.metadata
2. plugins/<plugin_id>/plugin.conf
3. plugins/<plugin_id>/plugin.sh
4. plugins/<plugin_id>/lang/en-us.lang
5. demais arquivos lang, se solicitados
6. desktop template, se aplicavel

12. Validacao obrigatoria:

No fim, forneca os comandos:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \\;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Se o plugin tiver desktop:

ATM_LANG=en-us bin/atm --dry-run path apply

13. Explique riscos e pontos a revisar:

- URL oficial de download.
- Formato do pacote.
- Nome real do binario dentro do pacote.
- Estrutura de diretorios depois da extracao.
- Comando correto de versao.
- PATH entries.
- Desktop launcher, se aplicavel.
- Licenca e termos de redistribuicao/download da ferramenta.

Importante:

Gere codigo simples, robusto e revisavel. Nao invente dependencias desnecessarias. Prefira seguir os padroes dos plugins existentes do ATM.
```

---

## Checklist Para Revisar A Resposta Da IA

Use este checklist antes de aplicar o plugin no repositorio.

```text
[ ] plugin.metadata tem ATM_PLUGIN_ID unico.
[ ] ATM_PLUGIN_VERSION_VALUE esta em 0.0.1.
[ ] plugin.conf nao contem segredos.
[ ] plugin.sh passa em bash -n.
[ ] Todas as funcoes usam prefixo atm_<plugin_id>_.
[ ] install respeita ATM_DRY_RUN.
[ ] use valida versao instalada antes de trocar current.
[ ] remove bloqueia remocao da versao current.
[ ] uninstall pede confirmacao.
[ ] path_entries imprime apenas paths.
[ ] status imprime uma linha curta.
[ ] menu nao tem numeros duplicados.
[ ] Todas as strings finais do menu usam atm_t.
[ ] lang/en-us.lang contem todas as keys usadas.
[ ] Manifesto usa ATM_PLUGIN_VERSION=\"0.0.1\".
[ ] Desktop launcher, se houver, e instalado apenas em ~/.local/share/applications.
[ ] Nao ha sudo no plugin.
[ ] Nao ha logica especifica do plugin em lib/.
```

---

## Prompt Curto

Use quando quiser uma versao mais direta.

```text
Crie um plugin completo para ATM — Atomy Tools Modules.

Ferramenta: <nome>
Plugin ID: <id>
Icone: <icone>
Versao do plugin: 0.0.1
Versao default da ferramenta: <versao>
Versoes do menu: <lista>
Padrao de download oficial: <url/padrao>

Gere:
- plugins/<id>/plugin.metadata
- plugins/<id>/plugin.conf
- plugins/<id>/plugin.sh
- plugins/<id>/lang/en-us.lang
- locales adicionais se solicitado
- desktop template se for GUI

Siga os padroes do ATM:
- funcoes atm_<id>_*
- install/use/remove/uninstall/status/menu/path_entries
- dry-run aware
- manifesto com atm_manifest_write
- versoes instaladas em ~/Apps/<id>/<version> ou padrao justificado
- symlink current quando houver troca de versao
- strings via atm_t
- sem sudo
- desktop apenas em ~/.local/share/applications

Inclua comandos de validacao no final.
```

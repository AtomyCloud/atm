# Manual do Desenvolvedor de Plugins ATM

Este manual explica como criar um plugin para o **ATM — Atomy Tools Modules**.

Um plugin ATM e um modulo Bash local e confiavel. Ele e responsavel pela logica especifica da ferramenta: instalacao, deteccao de versoes, PATH, desktop launchers, manifestos, menus e traducoes.

## 1. Contrato do Plugin

Todo plugin vive em:

```text
plugins/<plugin_id>/
```

Arquivos minimos:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

Locales recomendados:

```text
pt-br pt-pt es it fr de ru ja zh-cn ko
```

## 2. Regras de Nome

Use um `plugin_id` estavel:

```text
example_tool
```

Regras:

```text
- Use letras minusculas, numeros e underscores.
- O plugin id deve ser unico.
- Todas as funcoes publicas usam atm_<plugin_id>_.
- Nao coloque logica especifica do plugin em lib/.
```

## 3. Estrutura Inicial

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

`plugin.metadata` e shell `key=value` e e carregado pelo ATM.

```bash
ATM_PLUGIN_ID="example_tool"
ATM_PLUGIN_NAME_VALUE="Example Tool"
ATM_PLUGIN_ICON_VALUE="🧩"
ATM_PLUGIN_VERSION_VALUE="0.0.1"
ATM_PLUGIN_ORDER_VALUE="90"
ATM_PLUGIN_DESCRIPTION_VALUE="Install and manage Example Tool versions"
ATM_PLUGIN_ENTRYPOINT="plugin.sh"
ATM_PLUGIN_FULL_SETUP_VALUE="0"
ATM_PLUGIN_DEPENDS_VALUE=""
ATM_PLUGIN_OPTIONAL_DEPENDS_VALUE=""
ATM_PLUGIN_MENU_FUNC_VALUE="atm_example_tool_menu"
ATM_PLUGIN_STATUS_FUNC_VALUE="atm_example_tool_status"
ATM_PLUGIN_INSTALL_FUNC_VALUE="atm_example_tool_install"
ATM_PLUGIN_PATH_FUNC_VALUE="atm_example_tool_path_entries"
ATM_PLUGIN_DESKTOP_FUNC_VALUE=""
ATM_PLUGIN_REMOVE_FUNC_VALUE="atm_example_tool_remove"
ATM_PLUGIN_UNINSTALL_FUNC_VALUE="atm_example_tool_uninstall"
ATM_PLUGIN_USE_FUNC_VALUE="atm_example_tool_use"
```

## 5. plugin.conf

`plugin.conf` guarda defaults, paths, versoes e URLs.

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Todas as strings finais do menu devem usar `atm_t`.

```bash
ATM_PLUGIN_EXAMPLE_TOOL_MENU_TITLE="Example Tool Installer"
ATM_PLUGIN_EXAMPLE_TOOL_CURRENT="Current"
ATM_PLUGIN_EXAMPLE_TOOL_STATUS_NOT_INSTALLED="not installed"
ATM_PLUGIN_EXAMPLE_TOOL_INSTALLING="Installing Example Tool"
ATM_PLUGIN_EXAMPLE_TOOL_INSTALLED="Example Tool installed"
ATM_PLUGIN_EXAMPLE_TOOL_CANCELLED="Cancelled."
```

Uso no Bash:

```bash
printf '%s\n' "$(atm_t ATM_PLUGIN_EXAMPLE_TOOL_INSTALLING)"
```

## 7. Funcoes Obrigatorias

Implemente em `plugin.sh`:

```text
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
```

## 8. Semantica

```text
install   Instala uma versao, atualiza current e escreve manifesto.
use       Troca current para uma versao ja instalada.
remove    Remove uma versao especifica, nunca a current.
uninstall Remove tudo que o plugin gerencia, com confirmacao.
status    Imprime uma linha curta para o menu principal.
menu      Menu interativo sem numeros duplicados.
path_entries Imprime apenas paths, um por linha.
```

Todos os fluxos devem respeitar `ATM_DRY_RUN`.

## 9. Manifesto

Use:

```bash
atm_manifest_write "example_tool" \
  "ATM_PLUGIN_NAME=\"Example Tool\"" \
  "ATM_PLUGIN_VERSION=\"0.0.1\"" \
  "ATM_INSTALLED=\"1\"" \
  "ATM_CURRENT_VERSION=\"$version\""
```

Inclua:

```text
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION
ATM_INSTALLED
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS
```

## 10. Desktop Launcher

Plugins GUI podem registrar:

```bash
ATM_PLUGIN_DESKTOP_FUNC_VALUE="atm_example_tool_desktop"
```

Regras:

```text
- Instalar apenas em ~/.local/share/applications.
- Nao usar sudo.
- Plugin possui e renderiza sua propria template .desktop.in.
```

## 11. Validacao

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 12. Regras de Release

```text
- Versao do plugin deve ser 0.0.1 para release inicial.
- Nao usar sudo.
- Nao hardcodar strings finais de UX.
- Nao duplicar plugin IDs.
- remove remove uma versao.
- uninstall remove tudo gerenciado pelo plugin.
- dry-run nao modifica disco.
```


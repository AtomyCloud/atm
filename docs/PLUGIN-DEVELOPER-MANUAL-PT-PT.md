# Manual do Programador de Plugins ATM

Este manual explica como criar um plugin para o **ATM — Atomy Tools Modules**.

Um plugin ATM e um modulo Bash local e confiavel. O plugin gere a logica especifica da ferramenta: instalacao, versoes, PATH, launchers de ambiente de trabalho, manifestos, menus e traducoes.

## 1. Contrato do Plugin

Todo plugin vive em:

```text
plugins/<plugin_id>/
```

Ficheiros minimos:

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

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Use chaves `ATM_PLUGIN_<ID>_*` e `atm_t`.

```bash
ATM_PLUGIN_EXAMPLE_TOOL_MENU_TITLE="Example Tool Installer"
ATM_PLUGIN_EXAMPLE_TOOL_CURRENT="Current"
ATM_PLUGIN_EXAMPLE_TOOL_STATUS_NOT_INSTALLED="not installed"
```

## 7. Funcoes Obrigatorias

```text
archive_name, download_url, install_dir, current_path, cache_dir,
cache_file, manifest_file, normalize_version, version_from_args,
status, list_installed_versions, current_version, write_manifest,
install, use, remove, uninstall, menu, path_entries
```

Todas devem ter prefixo `atm_<plugin_id>_`.

## 8. Semantica

```text
install   Instala uma versao, actualiza current e escreve manifesto.
use       Troca current para uma versao instalada.
remove    Remove uma versao especifica, nunca a current.
uninstall Remove tudo que o plugin gere, com confirmacao.
status    Imprime uma linha curta.
menu      Menu interactivo sem numeros duplicados.
path_entries Imprime apenas caminhos.
```

Respeite sempre `ATM_DRY_RUN`.

## 9. Manifesto

Use `atm_manifest_write` e inclua `ATM_PLUGIN_VERSION="0.0.1"`, current version, current path, install root e versoes instaladas.

## 10. Desktop

Launchers desktop devem ir apenas para:

```text
~/.local/share/applications
```

Nao use sudo.

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

## 12. Release

```text
- Versao inicial do plugin: 0.0.1.
- Sem sudo.
- Sem strings finais hardcoded.
- Sem IDs duplicados.
- dry-run nao modifica disco.
```


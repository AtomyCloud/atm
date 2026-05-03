# Руководство разработчика плагинов ATM

Это руководство объясняет, как создать плагин для **ATM — Atomy Tools Modules**.

Плагин ATM — это локальный доверенный Bash-модуль. Он управляет логикой конкретного инструмента: установкой, версиями, PATH, desktop launcher, манифестами, меню и переводами.

## 1. Контракт плагина

Каждый плагин находится в:

```text
plugins/<plugin_id>/
```

Минимальный набор файлов:

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. Правила именования

Используйте стабильный `plugin_id`:

```text
example_tool
```

```text
- Только строчные буквы, цифры и подчеркивания.
- ID плагина должен быть уникальным.
- Публичные функции используют префикс atm_<plugin_id>_.
- Не добавляйте логику конкретного плагина в lib/.
```

## 3. Начальная структура

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

Содержит ID, имя, иконку, версию `0.0.1`, порядок, описание, entry point и ссылки на функции для menu/status/install/path/desktop/remove/uninstall/use.

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

Используйте `ATM_PLUGIN_<ID>_*` и `atm_t`. Не хардкодьте финальные UX-тексты.

## 7. Обязательные функции

Реализуйте функции `atm_<plugin_id>_` для download, install_dir, current_path, cache, manifest, status, списка версий, текущей версии, install, use, remove, uninstall, menu и path_entries.

## 8. Семантика

```text
install   Устанавливает версию, обновляет current и записывает манифест.
use       Переключает current на уже установленную версию.
remove    Удаляет конкретную версию, но никогда current.
uninstall Удаляет все, чем управляет плагин, после подтверждения.
status    Печатает короткую строку состояния.
menu      Интерактивное меню без повторяющихся номеров.
path_entries Печатает только пути.
```

Всегда соблюдайте `ATM_DRY_RUN`.

## 9. Манифест

Используйте `atm_manifest_write` с `ATM_PLUGIN_VERSION="0.0.1"` и текущей версией, текущим путем, install root и установленными версиями.

## 10. Desktop

Desktop launchers создаются только в:

```text
~/.local/share/applications
```

Не используйте sudo.

## 11. Проверка

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 12. Релиз

```text
- Начальная версия плагина: 0.0.1.
- Без sudo.
- Без хардкода финальных UX-текстов.
- Без дублирующихся ID.
- dry-run не изменяет диск.
```


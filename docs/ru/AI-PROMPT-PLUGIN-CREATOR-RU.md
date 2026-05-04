# ИИ-промпт — Создание плагина ATM

Используйте этот промпт, чтобы попросить ИИ создать плагин для **ATM — Atomy Tools Modules**.

```text
Ты senior Bash-инженер и создаешь плагин для ATM — Atomy Tools Modules.

Контекст:
ATM — модульный Bash-инструмент для установки и управления инструментами разработки в Linux. Каждый плагин находится в plugins/<plugin_id>/ и отвечает за специфичную логику инструмента: установку, версии, меню, статус, переключение версии, удаление, полную деинсталляцию, manifest-файлы, PATH entries, desktop launchers при необходимости и языковые файлы.

Инструмент:
<опиши инструмент>

Plugin ID:
<пример: nodejs, rust, deno, bun>

Отображаемое имя:
<пример: Node.js>

Иконка:
<пример: 🟩>

Начальная версия плагина:
0.0.1

Версии в меню:
<список версий>

Версия по умолчанию:
<версия>

Схема загрузки:
<официальный URL или шаблон. Если не уверен, явно отметь как пункт для проверки.>

Создай файлы:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Обязательные правила:

- Bash совместимый с set -Eeuo pipefail.
- Все публичные функции имеют префикс atm_<plugin_id>_.
- Использовать local внутри функций.
- Заключать переменные в кавычки.
- Соблюдать ATM_DRY_RUN.
- Не использовать sudo.
- Не писать вне модели ATM без ясной причины.
- Не помещать специфичную логику плагина в lib/.
- Desktop launchers только в ~/.local/share/applications.
- UX-строки через atm_t и ключи ATM_PLUGIN_<ID>_*.

plugin.metadata должен содержать ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT и ссылки на функции menu/status/install/path/desktop/remove/uninstall/use.

Реализуй:

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

Семантика:

- install принимает --version <version> и --version=<version>.
- install проверяет уже установленную версию перед загрузкой.
- install обновляет current и записывает manifest.
- use переключает только на установленную версию.
- remove не позволяет удалить current-версию.
- uninstall запрашивает подтверждение.
- status печатает короткую строку и начинается с "✅ ", если установлено.
- path_entries печатает только пути, по одному в строке.
- menu не должен иметь повторяющихся номеров и должен включать b) Back и q) Exit.

Manifest:

Используй atm_manifest_write и включи:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

В конце дай команды проверки:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

Выведи полный контент каждого файла отдельными блоками.
```


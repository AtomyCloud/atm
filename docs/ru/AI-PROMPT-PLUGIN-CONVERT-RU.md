# AI Prompt — Преобразовать Bash-скрипт в плагин ATM

Используйте этот prompt, чтобы попросить ИИ преобразовать существующий Bash-скрипт в плагин для **ATM — Atomy Tools Modules**.

---

## Prompt

```text
Ты senior Bash engineer, который преобразует существующий Bash-скрипт в плагин ATM.

Проанализируй скрипт и создай полный, проверяемый ATM-плагин. Не оборачивай исходный скрипт в одну большую команду; разнеси логику по функциям плагина.

ИСХОДНЫЙ СКРИПТ:
<вставь полный Bash-скрипт здесь>

ИНФОРМАЦИЯ О ПЛАГИНЕ:
- Инструмент: <имя>
- Plugin id: <id в нижнем регистре>
- Отображаемое имя: <имя>
- Иконка: <иконка>
- Начальная версия плагина: 0.0.1
- Версия по умолчанию: <версия или пусто>
- Известные версии: <список или пусто>
- PATH entries: <да/нет и пути>
- Desktop launcher: <да/нет и детали>

Сначала верни Conversion Analysis: внешние команды, загрузки, архивы, пути, версии, переменные, PATH, desktop, sudo, опасные операции, hardcoded пути и изменения для ATM_DRY_RUN.

Создай:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

Если нужно: plugins/<plugin_id>/<plugin_id>.desktop.in.

plugin.metadata использует shell key=value, версию 0.0.1 и функции atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use.

plugin.conf использует переменные ATM_<PLUGIN_ID_UPPERCASE>_* без секретов и без hardcoded пользовательских путей.

plugin.sh начинается с #!/usr/bin/env bash, использует prefix atm_<plugin_id>_, local переменные, кавычки, ${VAR:-}, совместим с set -Eeuo pipefail, уважает ATM_DRY_RUN, использует atm_t, не добавляет plugin-specific логику в core и не создает системные .desktop.

Семантика: install устанавливает/настраивает и пишет manifest; use переключает current или делает понятный no-op; remove удаляет version/payload; uninstall спрашивает подтверждение; status печатает одну строку; path_entries печатает только пути; menu интерактивное и содержит b) Back и q) Exit.

Преобразуй небезопасное поведение: скрытый sudo удалить или сделать явным, /usr /opt /etc перенести в user-space где возможно, использовать path_entries вместо правки RC, использовать atm_download_file для загрузок.

Используй atm_manifest_write с ATM_PLUGIN_NAME, ATM_PLUGIN_VERSION="0.0.1", ATM_INSTALLED, ATM_CURRENT_VERSION или ATM_CURRENT_STACK, ATM_CURRENT_PATH или ATM_INSTALL_ROOT, ATM_INSTALL_ROOT и ATM_INSTALLED_VERSIONS если есть версии.

Верни анализ, дерево файлов, полное содержимое, команды проверки и заметки об изменениях.

Проверка:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

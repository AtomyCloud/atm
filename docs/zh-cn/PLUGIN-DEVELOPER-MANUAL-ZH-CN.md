# ATM 插件开发者手册

本手册说明如何为 **ATM — Atomy Tools Modules** 创建插件。

ATM 插件是本地可信的 Bash 模块。它负责特定工具的逻辑：安装、版本、PATH、desktop launcher、manifest、菜单和翻译。

## 1. 插件约定

每个插件位于：

```text
plugins/<plugin_id>/
```

最小文件集合：

```text
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang
```

## 2. 命名规则

使用稳定的 `plugin_id`：

```text
example_tool
```

```text
- 只使用小写字母、数字和下划线。
- 插件 ID 必须唯一。
- 公共函数使用 atm_<plugin_id>_ 前缀。
- 不要把插件专属逻辑放入 lib/。
```

## 3. 初始结构

```bash
mkdir -p plugins/example_tool/lang
```

## 4. plugin.metadata

包含 ID、名称、图标、版本 `0.0.1`、排序、描述、entry point，以及 menu/status/install/path/desktop/remove/uninstall/use 的函数引用。

## 5. plugin.conf

```bash
ATM_EXAMPLE_TOOL_DEFAULT_VERSION="1.0.0"
ATM_EXAMPLE_TOOL_VERSION_OPTIONS="1.0.0 0.9.0 0.8.0"
ATM_EXAMPLE_TOOL_INSTALL_ROOT="$ATM_APPS_DIR/example-tool"
ATM_EXAMPLE_TOOL_CACHE_DIR="$ATM_DOWNLOAD_DIR/example-tool"
ATM_EXAMPLE_TOOL_MANIFEST_FILE="$ATM_MANIFEST_DIR/example_tool.manifest"
```

## 6. Locales

使用 `ATM_PLUGIN_<ID>_*` 和 `atm_t`。不要硬编码最终 UX 文案。

## 7. 必需函数

为 download、install_dir、current_path、cache、manifest、status、版本列表、当前版本、install、use、remove、uninstall、menu 和 path_entries 实现 `atm_<plugin_id>_` 函数。

## 8. 语义

```text
install   安装一个版本，更新 current，并写入 manifest。
use       将 current 切换到已安装版本。
remove    删除指定版本，但绝不删除 current。
uninstall 确认后删除插件管理的所有内容。
status    输出一行简短状态。
menu      交互式菜单，不能有重复编号。
path_entries 只输出路径。
```

始终遵守 `ATM_DRY_RUN`。

## 9. Manifest

使用 `atm_manifest_write`，并传入 `ATM_PLUGIN_VERSION="0.0.1"`、当前版本、当前路径、install root 和已安装版本。

## 10. Desktop

Desktop launchers 只能创建在：

```text
~/.local/share/applications
```

不要使用 sudo。

## 11. 验证

```bash
bash -n plugins/example_tool/plugin.metadata
bash -n plugins/example_tool/plugin.conf
bash -n plugins/example_tool/plugin.sh
find plugins/example_tool/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install example_tool --version 1.0.0
ATM_LANG=en-us bin/atm --dry-run path apply
```

## 12. 发布

```text
- 初始插件版本：0.0.1。
- 不使用 sudo。
- 不硬编码最终 UX 文案。
- 不使用重复 ID。
- dry-run 不修改磁盘。
```


# AI 提示词 — ATM 插件创建器

使用此提示词让 AI 为 **ATM — Atomy Tools Modules** 创建插件。

```text
你是一名资深 Bash 工程师，需要为 ATM — Atomy Tools Modules 创建一个插件。

背景:
ATM 是一个模块化 Bash 工具，用于在 Linux 上安装和管理开发工具。每个插件位于 plugins/<plugin_id>/，并负责该工具的专用逻辑：安装、版本、菜单、状态、版本切换、移除、卸载、manifest、PATH entries、需要时的 desktop launcher，以及语言文件。

工具:
<描述工具>

Plugin ID:
<示例: nodejs, rust, deno, bun>

显示名称:
<示例: Node.js>

图标:
<示例: 🟩>

插件初始版本:
0.0.1

菜单版本:
<版本列表>

默认版本:
<版本>

下载模式:
<官方 URL 或模式。如不确定，请明确标记为需要审查。>

创建这些文件:

plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

强制规则:

- Bash 兼容 set -Eeuo pipefail。
- 所有公共函数使用 atm_<plugin_id>_ 前缀。
- 函数内使用 local。
- 变量必须加引号。
- 遵守 ATM_DRY_RUN。
- 不使用 sudo。
- 没有明确理由不要写入 ATM 模型之外的位置。
- 不要把插件专用逻辑放入 lib/。
- desktop launcher 只能写入 ~/.local/share/applications。
- UX 字符串使用 atm_t 和 ATM_PLUGIN_<ID>_* keys。

plugin.metadata 必须包含 ATM_PLUGIN_ID, ATM_PLUGIN_NAME_VALUE, ATM_PLUGIN_ICON_VALUE, ATM_PLUGIN_VERSION_VALUE="0.0.1", ATM_PLUGIN_ORDER_VALUE, ATM_PLUGIN_DESCRIPTION_VALUE, ATM_PLUGIN_ENTRYPOINT，以及 menu/status/install/path/desktop/remove/uninstall/use 函数引用。

实现:

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

语义:

- install 接受 --version <version> 和 --version=<version>。
- install 在下载前检测已安装版本。
- install 更新 current 并写入 manifest。
- use 只能切换到已安装版本。
- remove 不允许删除 current 版本。
- uninstall 请求确认。
- status 输出简短一行，已安装时以 "✅ " 开头。
- path_entries 只输出路径，每行一个。
- menu 不能有重复编号，并且必须包含 b) Back 和 q) Exit。

Manifest:

使用 atm_manifest_write 并包含:
ATM_PLUGIN_NAME
ATM_PLUGIN_VERSION="0.0.1"
ATM_INSTALLED="1"
ATM_CURRENT_VERSION
ATM_CURRENT_PATH
ATM_INSTALL_ROOT
ATM_INSTALLED_VERSIONS

最后提供验证命令:

bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id> --version <version>
ATM_LANG=en-us bin/atm --dry-run path apply

分别输出每个文件的完整内容。
```


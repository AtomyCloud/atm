# AI Prompt — 将 Bash 脚本转换为 ATM 插件

使用此 prompt，让 AI 将现有 Bash 脚本转换为 **ATM — Atomy Tools Modules** 插件。

---

## Prompt

```text
你是一名 senior Bash engineer，正在把现有 Bash 脚本转换为 ATM 插件。

分析下面的脚本，并转换成完整、可审查的 ATM 插件。不要把原脚本简单包装成一个大命令；请把逻辑重构为职责清晰的 plugin functions。

ORIGINAL SCRIPT:
<在这里粘贴完整 Bash 脚本>

PLUGIN INFORMATION:
- Tool name: <名称>
- Plugin id: <小写 id>
- Display name: <显示名称>
- Icon: <图标>
- Initial plugin version: 0.0.1
- Default version: <版本或空>
- Known versions: <列表或空>
- PATH entries: <yes/no 和路径>
- Desktop launcher: <yes/no 和详情>

首先返回 Conversion Analysis，说明外部命令、下载、归档、路径、版本、环境变量、PATH、desktop、sudo、破坏性操作、hardcoded path，以及为了支持 ATM_DRY_RUN 需要修改的逻辑。

创建文件:
plugins/<plugin_id>/plugin.metadata
plugins/<plugin_id>/plugin.conf
plugins/<plugin_id>/plugin.sh
plugins/<plugin_id>/lang/en-us.lang

需要时也创建 plugins/<plugin_id>/<plugin_id>.desktop.in。

plugin.metadata 使用 shell key=value，版本 0.0.1，并导出 atm_<plugin_id>_menu/status/install/path_entries/remove/uninstall/use functions。

plugin.conf 使用 ATM_<PLUGIN_ID_UPPERCASE>_* variables，不保存 secrets，不写死当前用户路径。

plugin.sh 必须以 #!/usr/bin/env bash 开始，使用 atm_<plugin_id>_ prefix、local variables、quotes、${VAR:-}、set -Eeuo pipefail、ATM_DRY_RUN、atm_t，不把 plugin-specific logic 放入 core，不创建 system .desktop。

语义: install 安装/配置并写 manifest；use 切换 current 或明确 no-op；remove 删除 version/payload；uninstall 请求确认；status 输出一行；path_entries 只输出路径；menu 交互式并包含 b) Back 和 q) Exit。

转换不安全行为: 隐藏 sudo 必须删除或显式化；/usr /opt /etc 尽量转到 user-space paths；优先使用 path_entries 而不是直接修改 RC；下载优先使用 atm_download_file。

使用 atm_manifest_write，并包含 ATM_PLUGIN_NAME、ATM_PLUGIN_VERSION="0.0.1"、ATM_INSTALLED、ATM_CURRENT_VERSION 或 ATM_CURRENT_STACK、ATM_CURRENT_PATH 或 ATM_INSTALL_ROOT、ATM_INSTALL_ROOT，以及有版本时的 ATM_INSTALLED_VERSIONS。

返回 analysis、file tree、完整文件内容、validation commands、与原脚本相比的行为变更说明。

Validation:
bash -n plugins/<plugin_id>/plugin.metadata
bash -n plugins/<plugin_id>/plugin.conf
bash -n plugins/<plugin_id>/plugin.sh
find plugins/<plugin_id>/lang -name '*.lang' -print -exec bash -n {} \;
ATM_LANG=en-us bin/atm plugins list
ATM_LANG=en-us bin/atm --dry-run install <plugin_id>
ATM_LANG=en-us bin/atm --dry-run path apply
```

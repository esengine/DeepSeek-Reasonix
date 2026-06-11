# Reasonix 使用指南

<a href="../README.zh-CN.md">README</a>
&nbsp;·&nbsp;
<a href="./GUIDE.md">English</a>
&nbsp;·&nbsp;
<a href="./SPEC.md">规格</a>

> 日常配置与使用。工程契约与内部实现（数据类型、registry、包结构、路线图）见
> **[规格 SPEC.md](./SPEC.md)**。

## 目录

- [配置](#配置)
- [模式快捷键速查](#模式快捷键速查)
- [权限与沙盒](#权限与沙盒)
- [插件（MCP）](#插件mcp)
- [斜杠命令](#斜杠命令)
- [@ 引用](#-引用)
- [双模型协同](#双模型协同)

## 配置

优先级：**flag > `./reasonix.toml` > `~/.config/reasonix/config.toml` > 内置默认值**。
密钥经环境变量通过 `api_key_env` 注入，绝不写入配置文件。

```toml
default_model = "deepseek-flash"   # 执行器；设 [agent].planner_model 可加规划器
# language    = "zh"               # 界面语言；为空则按 $LANG / $REASONIX_LANG 自动检测

[ui]
# shortcut_layout = "desktop"      # classic|desktop；兼容旧配置

[agent]
max_steps = 0                    # 执行器工具调用轮数；0 表示不限
planner_max_steps = 12           # 规划器只读工具调用轮数；0 表示不限
# planner_model = "mimo-pro"          # 可选的低频规划器
# subagent_model = "deepseek-pro"     # runAs=subagent skill 的默认模型
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
auto_plan = "off"                  # off|on；off 表示计划模式仅手动开启
# auto_plan_classifier = "deepseek-flash"   # 可选；只在边界任务上调用

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
# 还有预设：deepseek-pro、mimo-pro（mimo-v2.5-pro）、mimo-flash（mimo-v2.5） @ token-plan-cn.xiaomimimo.com/v1

[tools]
enabled = []   # 省略/为空 = 全部内置工具
bash_timeout_seconds = 120   # 前台安全上限；设为 0 表示不设工具层超时

[skills]
# paths = ["~/my-skills", "../shared/skills"]   # 额外的自定义技能目录
# excluded_paths = ["~/.agents/skills"]         # 隐藏约定来源，不删除目录
# disabled_skills = ["review"]                  # 隐藏技能，直到 /skill enable <name>

[permissions]
mode  = "ask"                                # 无规则命中时 writer 的兜底：ask|allow|deny
deny  = ["Bash(rm -rf*)", "Bash(git push*)"] # 任何模式下都硬阻断
allow = ["Bash(go test:*)"]                  # 从不询问

[sandbox]
# workspace_root = ""          # 文件写工具被限制在此目录；留空 = 当前目录
# allow_write    = ["/tmp"]    # write_file/edit_file/multi_edit 额外可写的目录

[[plugins]]
name    = "example"
command = "reasonix-plugin-example"
```

完整 schema 与每个字段的契约见 [`SPEC.md` §5](./SPEC.md#5-configuration-toml)。

## 模式快捷键速查

这里按使用端来写，因为用户通常是先知道“我现在在桌面端/CLI”，再找对应按键。
核心规则很小：`Shift+Tab` 只管 Plan，`Ctrl/Cmd+Y` 只管 YOLO，粘贴继续走系统粘贴快捷键。

### 桌面端 GUI

| 按键或控件 | 作用 | 说明 |
| --- | --- | --- |
| `Shift+Tab` | 切换 Plan 开/关 | 输入框快捷键。Plan 是只读规划，不会循环 Ask/Auto/YOLO。 |
| `Ctrl+Y` / `Cmd+Y` | 切换 YOLO 开/关 | 输入框快捷键。关闭 YOLO 时会尽量恢复之前的 Ask/Auto 基底。 |
| Ask / Auto / YOLO 审批控件 | 直接选择工具审批姿态 | 点击操作不受快捷键规则影响。 |
| Plan 控件 | 切换 Plan 开/关 | 和 `Shift+Tab` 是同一个模式。 |
| 协作菜单里的 Goal | 启动、查看或清除 Goal | Goal 不进入任何快捷键循环。 |
| macOS `Cmd+V`，Windows/Linux `Ctrl+V` | 粘贴剪贴板内容 | 图片也可以直接拖进输入框。 |

### CLI / TUI

| 按键或命令 | 作用 | 说明 |
| --- | --- | --- |
| `Shift+Tab` | 切换 Plan 开/关 | Plan 是只读规划，不会循环 Ask/Auto/YOLO。 |
| `Ctrl+Y` | 切换 YOLO 开/关 | 关闭 YOLO 时会尽量恢复之前的 Ask/Auto 基底。终端若能转发 Command/Super，也可能识别 `Cmd+Y`，但稳定可用的是 `Ctrl+Y`。 |
| `--yolo`、`--dangerously-skip-permissions` | 启动时进入 YOLO | 和 `Ctrl+Y` 是同一个运行时模式。 |
| Ask / Auto | 没有键盘循环 | Ask 是默认交互基底；Auto 不通过 `Shift+Tab` 进入，需要由暴露工具审批姿态的客户端或 API 直接设置。 |
| `Ctrl+V` | 粘贴剪贴板内容 | CLI 会先尝试剪贴板图片，失败后再按文本粘贴。 |
| `/paste-image` | 粘贴剪贴板图片 | 适合只想贴图片，或终端应用自己接管文本粘贴的场景。 |
| `/goal <目标>`、`/goal status`、`/goal clear` | 启动、查看或清除 Goal | Goal 不进入任何快捷键循环。 |

`[ui].shortcut_layout` 仍被接受以兼容旧配置，但上面的快捷键行为已经跨布局统一。

模式含义：

| 模式 | 含义 |
| --- | --- |
| Ask | writer 兜底审批时询问。 |
| Auto | 自动放行兜底审批；显式 `ask` / `deny` 规则仍生效。 |
| YOLO | 跳过普通工具审批；`deny`、用户 `ask` 问题、计划批准提示仍会等待。 |
| Plan | 下一轮保持只读规划，直到计划被批准或关闭 Plan。 |
| Goal | 持续追一个已保存目标，直到完成、阻塞或清除。 |

## 权限与沙盒

权限逐次调用把关：`deny` > `ask` > `allow` > 兜底。Bash 和文件修改都要审核；
只读工具一般不需要。审核规则不是按“按钮文案”存，而是按权限规则匹配，比如
`Bash(npm run build)`、`Bash(npm run test:*)`、`Edit(docs/**)` 这种形式。
`reasonix chat` 会在 writer 调用前征求同意（普通工具为 `1` 本次 · `2` 本会话允许此范围 · `3` 总是允许此范围（保存） · `4` 拒绝；Bash 可额外选择命令前缀授权）；
其中 Bash 默认按具体命令记，也可按安全推导出的命令前缀记（如 `Bash(go test:*)`）；文件编辑类工具的本会话授权按编辑能力记，持久授权则写入 `Edit(<path>)` 文件路径规则；
`reasonix run` 保持自主运行但仍然遵守 `deny`。

权限是**策略**（哪些调用放行/询问），**沙盒**是**强制**：文件写工具
（`write_file` / `edit_file` / `multi_edit`）拒绝 `[sandbox] workspace_root`
之外的任何路径（默认当前目录，编辑不出项目），并解析符号链接与 `..`，使链接无法
打洞越界。读不受限。`bash` 本身在 macOS 默认进沙盒（`[sandbox] bash`，Seatbelt）：
命令只能写这些 root（外加临时目录与工具链缓存），`[sandbox] network` 为真时才能联网；
其它平台暂回退为不沙盒运行（越界问一次与 Linux 支持见
[`SPEC.md` §9](./SPEC.md#9-roadmap-not-in-current-scope)）。

## 插件（MCP）

Reasonix 是一个 MCP 客户端。`[[plugins]]` 的 `type` 选择传输：`stdio`（默认）启动本地子进
程（`command`/`args`/`env`）；`http`（Streamable HTTP）连接远程 `url`，可带静态
`headers`（`${VAR}` / `${VAR:-default}` 从环境展开，密钥不入文件）。工具以
`mcp__<server>__<tool>` 暴露给模型，与 Claude Code 一致；声明 MCP `readOnlyHint: true`
的工具会参与并行调度并命中权限层的只读默认放行。

服务器的 **prompts** 会暴露成 `/mcp__<server>__<prompt>` 斜杠命令（命令后空格分隔参
数）；**resources** 通过在消息里写 `@<server>:<uri>` 拉入；`/mcp` 列出已连接服务器及
各自暴露的内容。`make build` 还会产出 `bin/reasonix-plugin-example`——一个可直接运行的
stdio 参考实现（`echo`、`wordcount`、一个 `review` prompt、一个 style-guide 资源），
可照抄。

```toml
[[plugins]]                       # 本地 stdio 服务器
name    = "example"
command = "reasonix-plugin-example"

[[plugins]]                       # 远程 Streamable HTTP 服务器
name    = "stripe"
type    = "http"
url     = "https://mcp.stripe.com"
headers = { Authorization = "Bearer ${STRIPE_KEY}" }
```

启用的 MCP 服务器会在会话开始后于后台自动连接，因此工具上线期间聊天仍可正常使用。
用 `/mcp` 或桌面端 MCP 面板可刷新状态、重连服务器、查看失败原因，或在当前会话内禁用某个服务器。

**已有 Claude Code 的 `.mcp.json`？** 直接放到项目根目录，Reasonix 会原样读取——其
`mcpServers` 规范（`command`/`args`/`env`、`type`/`url`/`headers`、`${VAR}` 展开）
与 `[[plugins]]` 字段一一对应。两处来源会合并加载；同名时以 `reasonix.toml` 为准。

```json
{
  "mcpServers": {
    "filesystem": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path"] },
    "stripe": { "type": "http", "url": "https://mcp.stripe.com", "headers": { "Authorization": "Bearer ${STRIPE_KEY}" } }
  }
}
```

**从 `0.x` 升级？** 旧的 `~/.reasonix/config.json` 仍会被读取（读其 `mcpServers`、并遵从
`mcpDisabled`），作为最低优先级来源——所以 MCP 服务器照常可用；方便时再把它们挪进
`reasonix.toml` 的 `[[plugins]]` 或 `.mcp.json`。

## 斜杠命令

`reasonix chat` 里，内置命令（`/compact`、`/new`、`/clear`、`/rewind`、`/tree`、`/branch`、`/switch`、`/todo`、`/model`、`/mcp`、`/skills`、`/hooks`、`/memory`、`/output-style`、`/sandbox`、`/language`、`/auto-plan`、`/help`）在本地执行——`/help` 可列出全部。
`/new` 会开启新会话，同时保存之前的 transcript 供历史记录和恢复使用；`/clear` 会二次确认，确认后丢弃当前上下文且不保存。
`/tree` 查看已保存的对话分支，`/branch [name]` 从当前对话末端分支，`/branch <turn> [name]`
从较早的 checkpoint 轮次分支，`/switch <id|name>` 切换到另一个分支。**自定义命令**
是放在 `.reasonix/commands/`（项目）或 `~/.config/reasonix/commands/`（用户）下的 Markdown 文件——
`review.md` 即 `/review`，子目录构成命名空间（`git/commit.md` → `/git:commit`）。文件正文
是 prompt 模板，调用即作为一轮对话发出。

```markdown
---
description: Review the staged diff
argument-hint: [focus-area]
---
Review the staged diff. Focus on $ARGUMENTS, list bugs with file:line.
```

`$ARGUMENTS` 展开为全部空格分隔参数，`$1`…`$N` 为位置参数。MCP prompts 也以
`/mcp__<server>__<prompt>` 形式出现在这里。

## @ 引用

在消息里写 `@` 引用，Reasonix 会在发送前解析成带标签的上下文块：`@path/to/file`（或
`@dir`）注入本地文件内容（或目录清单），`@<server>:<uri>` 注入 MCP 资源。本地路径**只有
真实存在**时才当作引用，普通 `@mention` 保持原文。敲 `/` 或 `@` 会弹出补全菜单——斜杠
命令，或**逐层**的文件导航（一次只列当前一层目录、可下钻进子目录）外加 MCP 资源。

## 双模型协同

`reasonix setup` 刻意保持首次体验极简：选 provider → 输入 key（所选 provider 的所有
SKU 都会启用）。若要让两个模型协同（执行器 + 规划器，各自独立、缓存稳定的
session），向导后手动在 `reasonix.toml` 加一行即可：

```toml
[agent]
planner_model = "deepseek-pro"   # 作为低频规划器
planner_max_steps = 12           # 暂停前允许的只读工具调用轮数
```

Planner 会看到已加载的 `REASONIX.md` / `AGENTS.md` 记忆，并拿到一小组只读研究工具，
因此可以先检查相关文件再把计划交给执行器。写入类和流程类工具仍只给执行器使用。
`max_steps` 限制执行器；`planner_max_steps` 只限制规划器，两者都可设为 `0` 表示不限。

个人偏好的轮数上限建议放在用户级配置。只有当某个仓库确实需要团队共享覆盖时，
再写进项目的 `./reasonix.toml`，例如超大代码库需要更高的 planner 上限。

Subagent skills 默认继承执行器模型。设置 `subagent_model` 可让它们统一走另一个已配置
模型；设置 `subagent_models` 则只覆盖 `review`、`security_review` 等指定 skill。

交互式前端中，计划模式默认手动开启。设置 `agent.auto_plan = "on"` 后，看起来复杂
的任务会自动进入 plan mode：Reasonix 先只读生成计划，待用户批准后才
编辑文件或执行有副作用的命令。`auto_plan_classifier` 可以指定便宜的 provider，例如
`deepseek-flash`；它只在边界输入上调用，分类失败会回退到启发式规则。也可以用
`reasonix chat` 里的 `/auto-plan off|on` 修改用户级设置，或在 shell/脚本里用
`reasonix config auto-plan off|on`。只有明确想写项目级覆盖时，才给 shell 命令加
`--local`。

分离 session（让各模型前缀缓存稳定）背后的取舍见
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator)。

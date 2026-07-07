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
- [环境变量](#环境变量)
- [Serve Web 前端](#serve-web-前端)
- [配置路径](./CONFIG_PATHS.zh-CN.md)
- [思考语言](./REASONING_LANGUAGE.zh-CN.md)
- [任务合约与暂停策略](./TASK_CONTRACT.zh-CN.md)
- [自定义 OpenAI-compatible provider](#自定义-openai-compatible-provider)
- [桌面端 Hooks](./DESKTOP_HOOKS.zh-CN.md)
- [快捷键](#快捷键)
- [权限与沙盒](#权限与沙盒)
- [插件（MCP）](#插件mcp)
- [斜杠命令](#斜杠命令)
- [@ 引用](#-引用)
- [双模型协同](#双模型协同)

## 配置

优先级：**flag > `./reasonix.toml` > 用户配置文件 > 内置默认值**。从
**Reasonix v1.8.1** 开始，用户配置位于 macOS/Linux 的
`~/.reasonix/config.toml`，Windows 为 `%AppData%\reasonix\config.toml`；迁移和相关数据路径见
[配置路径](./CONFIG_PATHS.zh-CN.md)。标注为“仅用户/全局”的字段（包括 agent 轮数上限）不会被 `./reasonix.toml` 覆盖。
Provider 通过 `api_key_env` 命名密钥，真实密钥值保存在 CLI 与桌面端共用的
Reasonix 全局 `<Reasonix home>/.env`。项目 `.env`、home `.env`、继承的 shell 环境变量、旧 credentials 和系统 keyring 都不再作为 provider key 的运行时 fallback；旧凭据只作为迁移来源读取。项目 `.env` 仍会作为当前 workspace 范围内的 MCP/plugin 非 provider `${VAR}` 展开来源，但不会导入 provider key 或 Reasonix 控制变量。全局 `config.toml` 和 `.env` 的完整结构见
[配置路径](./CONFIG_PATHS.zh-CN.md)。

桌面端和 CLI 端的可见思考语言设置，见 [思考语言](./REASONING_LANGUAGE.zh-CN.md)。
桌面端 Hooks 的 JSON 配置、事件 key 和 payload 字段，见 [桌面端 Hooks](./DESKTOP_HOOKS.zh-CN.md)。
`SessionStart` hook 可通过 stdout 或 `hookSpecificOutput.additionalContext` 把插件/工作流 bootstrap 内容一次性注入下一轮真实用户输入上下文，而不是写入稳定 system prompt。
插件包可通过 `hooks/session-start-codex` 或插件根目录 `CLAUDE.md` 提供该启动上下文；Claude 风格 `.claude/settings.json` command hooks 也会按同名事件映射到 Reasonix hooks。

```toml
default_model = "deepseek-flash"   # 执行器；设 [agent].planner_model 可加规划器
# language    = "zh"               # 界面语言；为空则按 $LANG / $REASONIX_LANG 自动检测

[ui]
# shortcut_layout = "desktop"      # classic|desktop；兼容旧配置
# cursor_shape = "underline"       # block|underline|bar；CLI/TUI 输入光标

[agent]
max_steps = 0                    # 仅用户/全局；执行器工具调用轮数；0 表示不限
planner_max_steps = 0            # 仅用户/全局；规划器只读工具调用轮数；0 表示不限
reasoning_language = "auto"      # 可见思考过程语言：auto|zh|en
# plan_mode_allowed_tools = ["custom_reader"]   # 仅声明额外只读自定义工具；
#                                                # 不能解锁被计划模式阻断的工具或 unsafe bash
# plan_mode_read_only_commands = ["gh issue view", "gh pr diff"]   # 计划模式额外只读 shell 前缀
# planner_model = "deepseek-pro"      # 可选的低频规划器
# subagent_model = "deepseek-pro"     # runAs=subagent skill 的默认模型
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
# max_subagent_depth = 2              # 子代理嵌套委派深度；设为 1 可恢复旧的单层边界
auto_plan = "off"                  # 仅用户级生效；off|on；off 表示计划模式仅手动开启
# auto_plan_classifier = "deepseek-flash"   # 可选；只在边界任务上调用
tool_result_snip_ratio = 0.6       # 在摘要 compaction 前先缩短旧工具输出

[[providers]]
name        = "deepseek-flash"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
# 还有预设：deepseek-pro

[tools]
enabled = []   # 省略/为空 = 全部内置工具
bash_timeout_seconds = 120   # 前台安全上限；设为 0 表示不设工具层超时
mcp_call_timeout_seconds = 300   # MCP 调用默认安全上限；可用 plugin/tool 覆盖

[environment]
enabled = true   # 启动时把 OS、shell 和常见工具摘要稳定注入 prompt
# [environment.tools]
# go = "/opt/homebrew/bin/go"   # 可选：显式可信路径；workspace 内路径不会在启动时自动执行

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
# allow_write    = ["/tmp"]    # write_file/edit_file/multi_edit/move_file 额外可写的目录
# forbid_read    = ["${HOME}/.ssh"]   # agent 不可读取或列出的目录

[serve]
auth_mode = "none"             # none|token|password；绑定到非 localhost 前请先开启认证
# token = ""                   # 可选固定 token；token 模式为空时启动时自动生成
# password_hash = ""           # 用 reasonix serve --hash-password --password '...' 生成
# behind_proxy = false         # 只在可信反向代理后方设为 true

[[plugins]]
name    = "example"
command = "reasonix-plugin-example"
call_timeout_seconds = 600   # 可选：单个 MCP server 的调用超时
tool_timeout_seconds = { "generate_video" = 1800 }   # 可选：raw MCP tool 名称
```

完整 schema 与每个字段的契约见 [`SPEC.md` §5](./SPEC.md#5-configuration-toml)。

`[agent].plan_mode_allowed_tools` 用于把 Reasonix 无法自动分类的自定义/外部工具声明为额外只读工具。
对 MCP/plugin 工具，像 `mcp__github__issue_read` 这样的具体模型可见名也会把该工具提升为
planner / read-only research 可用的可信只读工具。优先使用 MCP 只读信任的一次性确认；需要预置已审过工具时，
再在 plugin 上写 `trusted_read_only_tools`，`plan_mode_allowed_tools` 保留为兼容逃生阀。它不再解锁 `bash`、`task`、
写文件工具、安装器、记忆变更工具等计划模式已知阻断项，也不会绕过 bash 在计划模式下的安全检查。

当计划阶段需要运行 Reasonix 尚不能自动分类、但你确认只读的 shell 查询命令时，使用
`[agent].plan_mode_read_only_commands`，例如 `gh issue view`、`gh pr diff` 或内部只读查询 CLI。
这里声明的是具体命令前缀，不是工具名：`["gh issue view"]` 会允许 `gh issue view 4572`，
但 `bash`、`sh` 等 shell 解释器前缀会被忽略。shell 操作符、重定向、命令替换、后台进程、
以及内置命令里的写能力参数在计划模式下仍会被阻断。交互式计划模式第一次需要某个未知查询前缀时，
Reasonix 也可以提示你确认是否信任它为只读；选择持久信任会写入同一个
`[agent].plan_mode_read_only_commands` 配置。Auto/YOLO 审批不会替用户回答这个信任提示。

### 环境变量

多数日常设置应写在 `config.toml` 或前文提到的 Reasonix 全局 `.env` 中。下面这些变量是进程级高级开关；
需要在启动 Reasonix 之前设置。项目 `.env` 不是 Reasonix 控制变量的运行时来源。

`REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION=true` 会为 Memory v5 启用可选的 LLM 任务/聊天分类器。
默认关闭，此时 Reasonix 使用本地 heuristic classifier，不会产生额外 provider 调用。开启后，分类缓存未命中时，
Reasonix 可能先通过已配置 provider 发送一个很小的分类请求，再决定用户输入是任务还是普通对话；这会增加少量延迟、
provider 用量和 token 成本。分类结果会在单个 session 内短时间缓存。只有去掉首尾空白后精确等于 `true`
才会启用；未设置、`false`、`1`、`TRUE` 都会保持默认 heuristic 路径。

```bash
REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION=true reasonix
```

开发运行时，把变量放在启动进程的命令前，例如：

```bash
REASONIX_MEMORY_COMPILER_LLM_CLASSIFICATION=true wails dev -forcebuild
```

从系统图形界面直接启动的打包桌面端通常不会继承交互式终端里的环境变量；如果确实要开启这个高级开关，
请从受环境变量管理的启动方式打开应用。

## Serve Web 前端

`reasonix serve` 会用同一个本地 Reasonix 引擎启动浏览器 UI。适合不安装桌面端但想用可视化界面、
在远程开发机上通过 tunnel 使用，或把当前会话临时共享给浏览器查看的场景。

```bash
cd your-project
reasonix serve
# 打开 http://127.0.0.1:8787
```

默认监听 `127.0.0.1:8787`，认证模式是 `auth_mode = "none"`。这个默认值只适合本机使用。
如果要绑定到非 loopback 地址、通过 tunnel 暴露，或放到反向代理后面，请先开启认证再分享 URL：

```bash
reasonix serve --auth token
reasonix serve --addr 0.0.0.0:8787 --auth token
reasonix serve --auth password --password 'temporary-password'
```

Token 模式会在终端打印带 `?token=...` 的分享链接；可通过 `--token` 或 `[serve].token`
复用固定 token。Password 模式必须在启动时传 `--password`，或在配置里保存 bcrypt hash：

```bash
reasonix serve --hash-password --password 'strong-password'

# ~/.reasonix/config.toml
[serve]
auth_mode = "password" # none|token|password
password_hash = "$2a$12$..."
behind_proxy = true    # 仅可信反向代理后方使用
```

Web UI 提供聊天、工具审批、会话历史、rewind/fork/summarize、模型与 reasoning effort 控件、
Goal、由 `todo_write` 工具驱动的实时 Todo 面板，以及已配置 provider 的余额显示。临时启动可用
`--model`、`--max-steps` 或 `--resume`；不传 `--model` 时，`serve` 使用用户全局
`default_model`。

## 自定义 OpenAI-compatible provider

在桌面端打开 **设置 -> 模型 -> 接入 -> 添加模型服务 -> 自定义供应商**，用于接入代理、
聚合平台或自建 OpenAI-compatible chat API / Anthropic-compatible Messages API 服务。

常用服务优先使用 **添加模型服务 -> 推荐预设**。Reasonix 可以预填可编辑的自定义 provider：
Kimi CN、Kimi Global、Kimi Coding Plan、MiMo API、MiMo Anthropic、MiMo Token Plan
CN/SGP/AMS 及其 Anthropic-compatible 变体、MiniMax CN/Global API、MiniMax
CN/Global Anthropic、GLM CN、Z.AI Global、GLM/Z.AI Coding Plan 的
OpenAI-compatible 与 Anthropic-compatible 端点、OpenCode Go、OpenCode Go
Anthropic、OpenCode Zen Anthropic、Qwen/DashScope CN/Global、Qwen Coding Plan
CN/Global 的 OpenAI-compatible 与 Anthropic-compatible 端点、StepFun
OpenAI-compatible 与 Anthropic-compatible 端点、NovitaAI、GMI Cloud、Vercel AI
Gateway、HuggingFace Router、NVIDIA NIM、KiloCode 和 Ollama Cloud。Plan 表示
访问/付费形态；只有服务商确实提供不同区域端点时，预设名才同时带 CN/Global。
因此 Kimi Coding Plan 是独立 plan 端点，Kimi 直连 API 才拆成 CN 和 Global。
预设路径通常只需要填写服务商 API Key：真实 key 会写入 Reasonix home `.env`，
`config.toml` 只保存端点、模型列表、key 环境变量名、上下文窗口、视觉模型元数据、
中国区端点直连、MiniMax `reasoning_split`、GLM/MiniMax thinking heuristic、
Anthropic-compatible 网关需要的 Bearer 认证、Ollama Cloud max-effort 支持，
以及 OpenCode Go 的每模型 reasoning 覆盖。添加后仍然可以打开 provider 卡片，
继续修改模型、请求头、端点或兼容设置。

**API 地址** 填写服务端点。默认模式下，Reasonix 会预览并把聊天请求发送到：

```text
<API 地址>/chat/completions
```

如果服务商给的是完整请求 URL，例如 `https://gateway.example.com/v1/chat/completions`，
开启 **完整 URL**。开启后 Reasonix 会直接使用该地址，不再追加 `/chat/completions`。
输入框下方的预览就是最终请求地址。

模型发现会基于 API 地址尝试 `/models`、`/v1/models` 等候选地址。如果网关要求单独的
模型列表端点，在 **兼容设置** 中填写 `models_url`，例如
`https://gateway.example.com/v1/models`。如果接口不支持模型发现，也可以手动填写模型列表。

**完整 URL** 仍使用 OpenAI-compatible chat 请求体；它不会切换成 OpenAI Responses API
的请求 schema。

### 兼容设置

**兼容设置（通常不用改）** 用于处理认证变量、模型发现地址、请求头、以及 reasoning/thinking
请求格式和普通 OpenAI-compatible 默认行为不一致的网关。除非服务商文档明确要求，或代理报错说明
不兼容，否则保持默认值即可。Kimi Coding Plan、MiniMax CN/Global Anthropic 这类 Anthropic-compatible 服务，
保存前在基础区域把接入协议切到 **Anthropic-compatible**。

| 字段 | 作用 | 什么时候改 |
| --- | --- | --- |
| `api_key_env` | 该 provider 使用的 API key 环境变量名。桌面端保存的真实 key 会写入 Reasonix home `.env` 的同名变量；TOML 配置里只保存变量名。 | 多个 provider 需要不同 key 时改名；服务不需要 API key 时可以留空。 |
| `models_url` | 只用于自动发现模型列表的 URL。聊天请求仍使用上方的 API 地址或完整 URL。 | `/models` 或 `/v1/models` 不是该网关模型列表地址时填写。 |
| 额外请求头 | 静态 HTTP header，一行一个 `Header: value`。 | OpenRouter 等网关要求 `HTTP-Referer`、`X-Title` 或类似站点来源 header 时使用。API key 仍放在上方密钥字段，不要重复写到这里。 |
| 额外请求体 | 合并到聊天请求体顶层的 JSON 对象。 | 仅用于服务商专用开关，例如 `{"enable_thinking": true}`。`model`、`messages`、`tools`、`stream`、`thinking` 等核心字段仍由 Reasonix 控制，且不接受 `null` 值。 |
| Authorization: Bearer | 对 Anthropic-compatible provider，把已保存的 API key 用 `Authorization: Bearer <key>` 发送，而不是 `x-api-key`。 | MiniMax Global、Vercel AI Gateway 等网关文档明确要求 Bearer 认证时开启。 |
| 模型能力模式 | 指定 Reasonix 对该 provider 使用哪种 reasoning 请求协议。 | 默认用“自动识别”。只有网关被误判，或模型文档要求特定 reasoning 格式时再切换。 |
| Thinking 覆盖 | provider 专用的 `thinking.type` 覆盖项。 | 默认用 Auto。只有后端文档明确支持 `enabled`、`disabled` 或 `adaptive` 时再手动指定；不支持的值可能让中转站拒绝请求。 |
| 余额查询 URL | 可选的钱包余额查询接口。 | 服务商提供余额接口，且希望桌面端状态栏显示余额时填写。 |
| 上下文窗口 | 该 provider 可保留的最大上下文 token 数。`0` 表示使用模型服务默认值。 | 模型实际上下文大小和 Reasonix 默认值或内置元数据不一致时填写。 |

模型能力模式选项：

| 选项 | 作用 |
| --- | --- |
| 自动识别（推荐） | Reasonix 根据模型能力元数据和端点自动选择请求格式。 |
| DeepSeek 思考 | 使用 DeepSeek 风格的 thinking 控制，包括 `thinking.type` 和 DeepSeek 支持的推理深度。 |
| OpenAI reasoning | 使用标准 OpenAI-compatible 的 `reasoning_effort` 档位。 |
| 普通聊天（不发送思考参数） | 不发送 reasoning 或 thinking 控制字段。适合会拒绝 reasoning 参数的普通文本代理。 |

Thinking 覆盖选项：

| 选项 | 作用 |
| --- | --- |
| Auto（使用服务默认） | 不写 provider 级 `thinking` 覆盖，让 Reasonix 使用 provider/model 默认行为。 |
| Enabled（开启） | 对兼容 provider 发送 `thinking.type = "enabled"`。 |
| Disabled（关闭） | 对兼容 provider 发送 `thinking.type = "disabled"`。DeepSeek 风格 provider 下还会避免继续发送推理深度提示。 |
| Adaptive（自适应） | 仅在服务文档明确支持 adaptive thinking 时使用，例如 MiniMax-M3 风格端点；语义是发送或保留 `thinking.type = "adaptive"`。 |

## 快捷键

这里按使用端来写，因为用户通常是先知道“我现在在桌面端/CLI”，再找对应按键。
核心模式规则很小：`Shift+Tab` 只管 Plan，`Ctrl/Cmd+Y` 只管 YOLO，粘贴继续走系统粘贴快捷键。

`[ui].shortcut_layout` 仍被接受以兼容旧配置，但下面的快捷键行为已经跨布局统一。

CLI/TUI 文本输入可通过 `[ui].cursor_shape` 设置光标形状，支持 `underline`、`block`
和 `bar`。默认值是 `underline`，因为部分终端中的 block 光标会在中英混排输入时覆盖
CJK 双宽字符，造成视觉错位。想保留旧的终端块状光标可设为 `block`，想使用细插入线可设为
`bar`。该设置不影响桌面端或 Web 输入框。

### 桌面端 GUI

桌面端快捷键在 **设置 → 快捷键** 中管理。选择一行后按下新的组合键，Reasonix 会为桌面端保存该绑定。
如果新组合键和已有动作冲突，会拒绝保存，避免一个快捷键触发两个动作。按 `?` 或点击 topic bar
里的帮助按钮可打开快捷键帮助表；帮助表由同一份快捷键 registry 生成，因此会同步显示自定义后的绑定。

全局快捷键：

| 按键或控件 | 作用 | 说明 |
| --- | --- | --- |
| macOS `Cmd+K`，Windows/Linux `Ctrl+K` | 打开或关闭命令面板 | 打开时会聚焦搜索框；`Esc` 关闭命令面板。 |
| macOS `Cmd+,`，Windows/Linux `Ctrl+,` | 打开设置 | 在设置里的 **快捷键** 页可自定义桌面端绑定。 |
| macOS `Cmd+W`，Windows/Linux `Ctrl+W` | 关闭当前顶部标签页 | 最后一个标签页仍由原有关闭保护保留。 |
| `Cmd+B` / `Ctrl+B` | 显示或隐藏左侧边栏 | 和点击侧边栏开关是同一个动作。 |
| `Cmd+Shift+B` / `Ctrl+Shift+B` | 展开或收起最近的 shell 输出 | 和点击折叠 shell 输出提示是同一个动作。 |
| macOS `Cmd+1`-`Cmd+9`，其它平台 `Ctrl+1`-`Ctrl+9` | 跳转到侧边栏中对应编号的可见对话 | 短暂按住 `Cmd`/`Ctrl` 会显示编号标记；已有自定义快捷键占用相同按键时，自定义动作优先生效。 |
| macOS `Cmd++`、`Cmd+-`、`Cmd+0`；其它平台 `Ctrl++`、`Ctrl+-`、`Ctrl+0` | 放大、缩小或重置文字大小 | 对把加号上报为 `=` 的键盘也兼容。 |
| `?` | 打开键盘快捷键帮助表 | 帮助表显示当前实际生效的桌面端绑定。 |

输入框快捷键：

| 按键或控件 | 作用 | 说明 |
| --- | --- | --- |
| `Enter` | 发送当前消息 | IME 组合输入确认不会被截获。 |
| `Shift+Enter` | 插入换行 | 输入框保持焦点。 |
| `Shift+Tab` | 切换 Plan 开/关 | Plan 是只读规划，不会循环 Ask/Auto/YOLO。 |
| `Cmd+Y` / `Ctrl+Y` | 切换 YOLO 开/关 | 关闭 YOLO 时会尽量恢复之前的 Ask/Auto 基底。 |
| macOS `Cmd+V`，Windows/Linux `Ctrl+V` | 粘贴剪贴板内容 | 剪贴板图片会作为附件加入；图片也可以拖进输入框。 |
| 输入边界处的普通 `Up` / `Down` | 回放更旧或更新的已提交提示词 | 带修饰键的方向键和原生文本导航仍交给 textarea。 |
| 运行中按 `Esc` | 取消当前 turn | 如果后端尚未开始回复，会恢复草稿。 |

菜单与控件：

| 按键或控件 | 作用 | 说明 |
| --- | --- | --- |
| 斜杠、`@` 或 past-chat 菜单中的 `Up` / `Down` | 移动高亮项 | past-chat 搜索框使用同一套导航键。 |
| 这些菜单中的 `Enter` / `Tab` | 接受高亮项 | 类似目录的条目可能继续打开下一层菜单。 |
| 这些菜单中的 `Esc` | 关闭当前菜单或退出 past-chat 搜索 | 关闭后可继续正常输入。 |
| Ask / Auto / YOLO 审批控件 | 直接选择工具审批姿态 | 点击操作不受快捷键规则影响。 |
| 工具审批卡片 | `Left` / `Right`、`Enter`、`1`-`4`、`Esc` | 移动高亮动作、确认当前高亮、直接选择编号动作，或拒绝。默认高亮是“允许一次”。 |
| 计划审批卡片 | `Left` / `Right`、`Enter`、`1`-`3`、`Esc` | 在“修改计划 / 开始执行 / 退出计划”之间移动。默认高亮是“开始执行”。 |
| Plan 控件 | 切换 Plan 开/关 | 和 `Shift+Tab` 是同一个模式。 |
| 协作菜单里的 Goal | 启动、查看或清除 Goal | Goal 不进入任何快捷键循环。 |

### CLI / TUI

聊天与 transcript：

| 按键或命令 | 作用 | 说明 |
| --- | --- | --- |
| `Enter` | 发送当前消息 | turn 运行中输入非空内容时，会排队作为后续反馈。 |
| `Shift+Enter`、`Alt+Enter` 或 `Ctrl+J` | 插入换行 | 普通 `Enter` 保留给发送/确认。 |
| 空闲时普通 `Up` / `Down` | 回放更旧或更新的已提交提示词 | turn 运行中同一组按键用于导航排队反馈。 |
| `PageUp` / `PageDown` | 滚动 transcript | 不受当前聊天状态影响。 |
| `Ctrl+Home` / `Ctrl+End` | 跳到 transcript 顶部或底部 | 长工具输出后很有用。 |
| `Ctrl+L` 或 `/cls` | 只清空可见 transcript | LLM 上下文、session 文件、工具、记忆和插件都保持加载；想丢弃对话上下文时用 `/clear`。 |
| `Esc` | 退出当前最具体的动作 | 可在无回复前撤回刚提交的 turn、取消运行中的 turn，或清空非空输入。 |
| 空闲且输入为空时双击 `Esc` | 打开 rewind 选择器 | 和 `/rewind` 是同一个入口。 |
| 终端原生选择 | 复制 transcript 文本 | Reasonix 默认不启用鼠标报告，因此终端自己的选择/复制仍可使用。 |
| `Ctrl+C` | 取消、清空或退出 | 取消运行中的 turn、清空非空输入；空输入下连按两次退出。 |
| `Ctrl+D` | 退出 TUI | 立即退出。 |
| `Ctrl+V`、`Ctrl+Shift+V`、`Meta+V` 或 `Super+V` | 粘贴剪贴板内容 | CLI 会先尝试图片，再回退到文本或文件引用。 |
| `/paste-image` | 粘贴剪贴板图片 | 适合只想贴图片，或终端应用自己接管文本粘贴的场景。 |
| 以 `!` 开头的一行 | 直接运行 shell 命令 | 命令在本地执行，不经过模型。 |

模式与显示：

| 按键或命令 | 作用 | 说明 |
| --- | --- | --- |
| `Shift+Tab` | 切换 Plan 开/关 | Plan 是只读规划，不会循环 Ask/Auto/YOLO。 |
| `Ctrl+Y` | 切换 YOLO 开/关 | 关闭 YOLO 时会尽量恢复之前的 Ask/Auto 基底。终端若能转发 Command/Super，也可能识别 `Cmd+Y`，但稳定可用的是 `Ctrl+Y`。 |
| `--yolo`、`--dangerously-skip-permissions` | 启动时进入 YOLO | 和 `Ctrl+Y` 是同一个运行时模式。 |
| `Ctrl+O` | 切换详细 reasoning 显示 | 也可通过 `/verbose` 使用。 |
| `Ctrl+B` | 展开或收起较长 shell 输出 | TUI 默认不启用鼠标报告，因此可和终端原生文本选择共存。 |
| Ask / Auto | 没有键盘循环 | Ask 是默认交互基底；Auto 不通过 `Shift+Tab` 进入，需要由暴露工具审批姿态的客户端或 API 直接设置。 |
| `/goal <目标>`、`/goal --research <目标>`、`/goal --simple <目标>`、`/goal status`、`/goal clear` | 启动、查看或清除 Goal | Goal 不进入任何快捷键循环；明显长周期目标会自动启用 AutoResearch。普通输入命中强 AutoResearch 信号时也会自动升级为 Goal。 |
| `/migrate`、`/migrate --from <旧目录>` | 重试旧数据迁移，或从指定 v0.x 来源导入 sessions | Windows v0.52 自定义安装/数据目录用 `--from`；该形式只导入 sessions。详见[配置路径](./CONFIG_PATHS.zh-CN.md)。 |

选择器与审批：

| 上下文 | 按键 | 作用 |
| --- | --- | --- |
| 斜杠或 `@` 补全 | `Up` / `Down`、`Tab` / `Enter`、`Esc` | 移动、接受或关闭补全菜单。 |
| 工具审批提示 | `y`/`1`、`a`/`2`、`p`/`3`、`n`/`4`、`Enter`、`Esc`、`Ctrl+C` | 允许一次、本会话允许、持久允许、拒绝、默认允许一次、拒绝，或取消当前 turn。 |
| Ask 问题卡 | `Up`/`Down` 或 `j`/`k`、`Left`/`Right` 或 `h`/`l`、`Space`、`Enter`、`1`-`9`、`Esc`、`Ctrl+C` | 导航答案/问题标签、切换多选、提交/激活、选择编号选项、关闭，或取消当前 turn。 |
| Rewind 选择器 | `Up`/`Down` 或 `j`/`k`、`Enter`、`b`、`c`、`d`、`f`、`s`、`u`、`Esc` | 选择 turn，应用 both/conversation/code/fork/summarize 动作，或返回/关闭。 |
| Resume 选择器 | `Up`/`Down` 或 `j`/`k`、`Enter`、`Esc` | 选择已保存 session 或关闭选择器。 |
| MCP 导入选择器 | `Up`/`Down` 或 `j`/`k`、`Space`、`Enter`、`Esc` / `Ctrl+C` | 移动、勾选服务器、导入勾选服务器，或取消。 |
| MCP 管理器 | `Up`/`Down` 或 `j`/`k`、`Enter`、`Left`/`Right` 或 `h`/`l`、`r`、数字键、`q` / `Ctrl+C` | 导航服务器列表/详情、刷新、选择动作，或关闭。 |
| `/clear` 确认 | 方向键或 `j`/`k` / `Tab`、`Enter`、`y`、`n`、`Esc` / `Ctrl+C` | 在 Clear/Cancel 间切换、确认清空，或取消。 |

模式含义：

| 模式 | 含义 |
| --- | --- |
| Ask | writer 兜底审批时询问。 |
| Auto | 自动放行兜底审批；显式 `ask` / `deny` 规则仍生效。 |
| YOLO | 跳过普通工具审批；`deny`、用户 `ask` 问题、计划批准提示、MCP 只读信任提示仍会等待。 |
| Plan | 下一轮保持只读规划，直到计划被批准或关闭 Plan。 |
| Goal | 持续追一个已保存目标，直到完成、阻塞或清除。 |

## 权限与沙盒

权限逐次调用把关：`deny` > `ask` > `allow` > 兜底。Bash 和文件修改都要审核；
只读工具一般不需要。审核规则不是按“按钮文案”存，而是按权限规则匹配，比如
`Bash(npm run build)`、`Bash(npm run test:*)`、`Edit(docs/**)` 这种形式。
`reasonix` 会在 writer 调用前征求同意（普通工具为 `1` 本次 · `2` 本会话允许此范围 · `3` 总是允许此范围（保存） · `4` 拒绝；Bash 可额外选择命令前缀授权）；
其中 Bash 默认按具体命令记，也可按安全推导出的命令前缀记（如 `Bash(go test:*)`）；文件编辑类工具的本会话授权按编辑能力记，持久授权则写入 `Edit(<path>)` 文件路径规则；
`reasonix run` 保持自主运行但仍然遵守 `deny`。

权限是**策略**（哪些调用放行/询问），**沙盒**是**强制**：文件写工具
（`write_file` / `edit_file` / `multi_edit` / `move_file`）拒绝 `[sandbox] workspace_root`
之外的任何路径（默认当前目录，编辑不出项目），并解析符号链接与 `..`，使链接无法
打洞越界。`forbid_read` 可选地隐藏敏感目录，使 agent 的读文件、列目录和搜索工具不能读取或列出它们；
建议使用绝对路径或 `${HOME}` / `${VAR}`，不要写 `~`，因为配置只做环境变量展开。
`bash` 本身默认进 OS 沙盒（`[sandbox] bash`：macOS 使用 Seatbelt，Linux 使用 bubblewrap，
原生 Windows 使用 native helper）：命令只能写这些 root（外加平台按命令提供的临时/缓存 root），
OS 沙盒生效时也不能读取配置的 `forbid_read` roots，`[sandbox] network` 为真时才能联网。
原生 Windows helper 使用 Reasonix 内置的 Windows sandbox backend：
只读命令使用 AppContainer，可写命令使用 low-integrity token；它会临时授予
workspace、每次命令专用 temp root 和目标可执行文件的访问权，对 `forbid_read`
（文件和目录皆可）临时添加 deny ACE，修改前记录被触碰目录的 DACL，命令结束后尽力恢复。
作用于同一 workspace 的并发命令会被串行化，避免各自的 ACL 修改互相破坏；被强杀命令
残留的 low-integrity 标签或 `forbid_read` deny ACE 会由下一次运行清理。由于可写命令跑在
low-integrity token 下，除配置的 root 外它仍能写入 Windows 对任何 low-integrity 进程开放的
少数位置（例如 `%USERPROFILE%\AppData\LocalLow`），但 workspace 边界与 `forbid_read`
拒绝依然有效。只读 AppContainer 命令在关闭网络时不给 network capability；可写 Windows 命令遇到
`[sandbox] network = false` 时会 fail closed。没有可用 OS 沙盒时，`bash = "enforce"` 会拒绝 bash 执行，不会无沙盒运行
（越界询问与可选的 Windows elevated 加固见
[`SPEC.md` §9](./SPEC.md#9-roadmap-not-in-current-scope)）。

Windows 沙盒排障：沙盒会把 Reasonix 可执行文件自身以隐藏 helper 方式重新拉起，
CLI 与桌面端都内置了这个 helper 入口——若某个构建缺少入口而又请求 enforce，
bash 会以明确报错拒绝执行，而不是返回空输出。同一 workspace 上排队等待另一条
沙盒命令时会打印一行“waiting for another sandboxed command”提示，并在可识别时
标出持锁命令及其 PID。前台命令排队 1 分钟后即失败并给出同样的持锁信息（被挡住
的回合应尽快报错，而不是挂住）；后台任务最多等 10 分钟，`WINDOWS_SANDBOX_LOCK_MS`
可同时覆盖两者。应先停止提示中点名的命令；调大等待上限只会让后续命令等更久。
如果只有 Git-for-Windows/MSYS2 bash
下的沙盒命令失败，可试 `[tools.shell] prefer = "powershell"`——MSYS 运行时在
low-integrity token 下较脆弱。运行 `reasonix doctor` 可查看解析到的 shell、沙盒
可用性，以及项目 `reasonix.toml` 是否固定了 `[sandbox]`（项目文件优先级高于
Settings/用户配置；沙盒配置变更需 reload session config 或新开会话才生效）。

## 插件（MCP）

Reasonix 是一个 MCP 客户端。`[[plugins]]` 的 `type` 选择传输：`stdio`（默认）启动本地子进
程（`command`/`args`/`env`）；`http`（Streamable HTTP）连接远程 `url`，可带静态
`headers`（`${VAR}` / `${VAR:-default}` 从环境展开，密钥不入文件）。工具以
`mcp__<server>__<tool>` 暴露给模型，与 Claude Code 一致；声明 MCP `readOnlyHint: true`
的工具会参与并行调度并命中权限层的只读默认放行，但 planner / read-only research 会先确认第三方
自报只读。交互式会话里，第一次需要时允许即可；选择持久允许会把 raw MCP tool name 记住。
这个信任提示属于用户决策，Auto/YOLO 工具审批不会代答；选择本会话允许或持久允许后，同一个 MCP
工具不会在本会话里重复弹。
高级用户也可以在 plugin 上预置审过的第三方读工具：

```toml
[[plugins]]
name = "github"
command = "github-mcp"
trusted_read_only_tools = ["issue_read", "pull_request_read"]
```

桌面端 MCP 面板保留为高级管理入口：展开已配置的服务器并打开工具列表；只有在想提前批准工具时，
才使用 **预先信任只读** 或单个工具旁的 **预先信任**。用 **取消信任** 可以移除已记住的读工具。
桌面端会把 raw MCP tool name 写入拥有该服务器的配置源：项目 `.mcp.json` 里的服务器会更新到
`mcpServers.<server>.trusted_read_only_tools`，普通 Reasonix plugin 会写入用户级 Reasonix config。
只信任无副作用的读取工具；create/update/delete 这类写工具应保持未信任。

服务器的 **prompts** 会暴露成 `/mcp__<server>__<prompt>` 斜杠命令（命令后空格分隔参
数）；**resources** 通过在消息里写 `@<server>:<uri>` 拉入；`/mcp` 列出已连接服务器及
各自暴露的内容。`make build` 还会产出 `bin/reasonix-plugin-example`——一个可直接运行的
stdio 参考实现（`echo`、`wordcount`、一个 `review` prompt、一个 style-guide 资源），
可照抄。

```toml
[[plugins]]                       # 本地 stdio 服务器
name    = "example"
command = "reasonix-plugin-example"
# call_timeout_seconds = 600       # 可选：单个 MCP server 的调用超时
# tool_timeout_seconds = { "generate_video" = 1800 }   # 可选：raw MCP tool 名称

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

交互式 `reasonix` 会话里，内置命令（`/compact`、`/new`、`/clear`、`/rewind`、`/tree`、`/branch`、`/switch`、`/todo`、`/model`、`/mcp`、`/skills`、`/hooks`、`/memory`、`/memory-v5`、`/goal`、`/output-style`、`/sandbox`、`/language`、`/auto-plan`、`/reasoning-language`、`/help`）在本地执行——`/help` 可列出全部。
`/new` 会开启新会话，同时保存之前的 transcript 供历史记录和恢复使用；`/clear` 会二次确认，确认后丢弃当前上下文且不保存。
`/tree` 查看已保存的对话分支，`/branch [name]` 从当前对话末端分支，`/branch <turn> [name]`
从较早的 checkpoint 轮次分支，`/switch <id|name>` 切换到另一个分支。**自定义命令**
是放在 `.reasonix/commands/`（项目）或 `~/.reasonix/commands/`（用户）下的 Markdown 文件——
`review.md` 即 `/review`，子目录构成命名空间（`git/commit.md` → `/git:commit`）。文件正文
是 prompt 模板，调用即作为一轮对话发出。

`/memory` 会同时列出记忆文档（`REASONIX.md` / `AGENTS.md`）和已保存的 auto-memory 条目。
在 agent 回合中，只读的 `history` 和 `memory` 工具可以按需检索历史 session 决策、
compaction archive 和已保存事实；这些动态内容不会被塞进稳定的 system prompt 前缀。
`/forget <name>` 会把已保存事实归档而不是永久删除；CLI/TUI 和桌面记忆面板能显示归档文件用于追溯，
但它们不会作为 active memory 被检索。检索会保留 BM25 最强命中，同时裁掉弱的泛词命中；
agent 发起的 `remember` 和 `forget` 每次都会要求新的人工确认，并在执行前展示将保存或归档的记忆摘要；
Guardian 审查不能代替用户批准，非交互运行会拒绝这类工具而不是自动批准。
0 结果会提示 agent 改用更少、更有区分度的词继续查。
Memory v5 在 CLI/TUI、`reasonix serve` 和桌面端默认开启，因为这些入口共用同一套本地
controller。它会把本地、按项目隔离的执行轨迹和编译器状态写在 Reasonix home 下，并且只有
历史结果产生可行动约束时，才把下一轮用户输入编译成精简 execution contract。早期轮次可能
只写入轨迹而不注入任何内容。默认的 `verbosity = "observe"` 只做本地学习和内容无关指标，
不会把 `<memory-compiler-execution>` 发送到 provider 可见的用户轮次；只有显式切到
`verbosity = "compact"`（或旧的 `on` 命令别名）时才恢复精简 execution contract 注入，
并把选中的精简 memory reference 放进 provider 可见的用户轮次。Memory v5 不会绕过
memory 审批，也不会修改 cache-stable system prompt、Provider 前缀或工具 schema。

交互式会话里可用 `/memory-v5 off|observe|compact|on|status` 控制后续轮次，也可在 shell/脚本里用
`reasonix config memory-v5 off|observe|compact|on|status`。桌面端还可以在设置 → 通用 → Memory v5 中控制。
设置 → 更新 → 共享聚合质量指标控制可选的聚合上报；开启后只会上报匿名计数/大小桶，例如是否
注入、编译后 token 大小桶、IR overhead 大小桶、memory reference 数量、constraint/risk/step
数量，以及记忆图规模桶。它不会包含记忆正文、提示词、工具输出、文件路径、ID、密钥、base URL
或文件内容。

CLI/TUI 和 `reasonix serve` 使用同一个 user/global 配置。项目内的 `reasonix.toml` 不能覆盖
这个 user/global 设置。CLI 命令会更新底层配置；高级用户也可以手动编辑 Reasonix home 下的
用户配置：

```toml
[agent]
memory_compiler = { enabled = true, verbosity = "observe" }
```

CLI 可以在本地轮次使用 Memory v5，但不会运行桌面端的聚合指标上传管线。使用
`reasonix run --metrics <path>` 时，JSON 还会输出内容无关的 `memory_compiler_*` 汇总字段，
以及 `memory_compiler_turn_details` 逐轮明细数组，包含是否注入、编译后 token 和 IR overhead
估算、引用记忆/constraint/risk/step 数量，以及当前记忆图计数。
技术实现细节见 [`SESSION_MEMORY_RETRIEVAL.md`](SESSION_MEMORY_RETRIEVAL.md)。

```markdown
---
description: Review the staged diff
argument-hint: [focus-area]
---
Review the staged diff. Focus on $ARGUMENTS, list bugs with file:line.
```

`$ARGUMENTS` 展开为全部空格分隔参数，`$1`…`$N` 为位置参数。MCP prompts 也以
`/mcp__<server>__<prompt>` 形式出现在这里。

## Goal 与 AutoResearch

Goal 是长期目标的统一运行机制。普通 `/goal` 继续走轻量 Goal：Reasonix 会持续推进，直到
完成、阻塞或被清除。对于明显长周期的目标，Goal 会自动进入 AutoResearch 策略，而不是
要求用户单独运行 `/auto-research` skill；`auto-research` 也不会作为独立 builtin skill 出现在
Settings -> Skills 或斜杠菜单里。普通聊天输入如果命中很强的长周期信号，也会被 host 自动
升级为等价的 `/goal --research <原输入>`。

复杂任务建议把目标写成[任务合约](./TASK_CONTRACT.zh-CN.md)：Context、Request、
Output format、Constraints 和 Pause policy。Goal 模式会把这些部分当作自主执行的边界；
除非下一步需要不可逆或对外可见操作、任务范围变化，或必须由用户提供信息，否则会继续采用合理默认值推进，并在最后汇报假设与结果。

AutoResearch 会在这些目标里自动启用：包含“持续”“长期”“彻底”“直到根因明确”“多轮排查”
“不要原地打转”“完整方案”“跑实验”“反复验证”“系统性研究”等强信号；或者目标同时包含
研究/排查、实现/修复、验证/测试、优化/文档/发布等多个阶段；或者用户明确给出
`.reasonix/autoresearch/<task-id>/` 任务目录。高级用户可以用
`/goal --research <目标>` 强制启用，也可以用 `/goal --simple <目标>` 强制保持轻量 Goal。
普通聊天里的自动升级比 `/goal` 内部判断更保守：单独说“长期”“优化”“研究一下”或
“验证一下”不会自动创建 AutoResearch 任务。

进入 AutoResearch 后，agent 会把目标当成有状态的研究循环，而不是只靠聊天上下文续写。
它会创建或复用项目级 `.reasonix/autoresearch/<task-id>/` 目录。新任务默认使用
`YYYYMMDD-HHMMSS-slug` 作为 id，例如 `20260618-224530-cache-audit`；创建前会先检查
当前项目目录，只有同名已存在时才追加 `-2`、`-3` 等后缀。任务状态包括
`task_spec.md`、`progress.json`、`findings.jsonl`、`directions_tried.json` 和
`iteration_log.jsonl`，记录每轮方向、证据、验证结果和卡住原因，并用 `stale_count` 判断
是否在低质量重复。连续停滞时，它会要求结构性 pivot，例如换证据源、入口、测试 oracle、
拆解方式、benchmark 或 worker 策略，而不是继续重复同一种尝试。

worker/subagent 可以独立探索，但 canonical state 由 orchestrator 负责写入。完成前必须
对照 `task_spec.md` 的 success criteria 做逐项证据审计；窄范围检查通过不能证明宽范围需求
完成。动态运行态只写进 `.reasonix/autoresearch/...`，不写入 `REASONIX.md`、`AGENTS.md`、
project memory、tool schema 或 cache-stable system prompt。公开发布、破坏性操作、凭证、
付款和外部通知仍然遵守正常的 approval、privacy 与 cache gate。

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
```

Planner 会看到已加载的 `REASONIX.md` / `AGENTS.md` 记忆，并拿到一小组只读研究工具，
因此可以先检查相关文件再把计划交给执行器。写入类和流程类工具仍只给执行器使用。
`max_steps` 限制执行器；`planner_max_steps` 只限制规划器，两者都可设为 `0` 表示不限。

轮数上限请放在用户级配置。项目 `./reasonix.toml` 不会覆盖 `max_steps` 或
`planner_max_steps`。

Subagent skills 默认继承执行器模型。设置 `subagent_model` 可让它们统一走另一个已配置
模型；设置 `subagent_models` 则只覆盖 `review`、`security_review` 等指定 skill。

Subagent 默认允许再委派一层：根会话是 depth 0，第一层 subagent 是 depth 1，
`max_subagent_depth = 2` 表示 depth 1 的 workflow 可以再派 depth 2 的 reviewer
或 implementer；depth 2 不再拿到递归 agent/skill 工具。设
`agent.max_subagent_depth = 1` 可恢复旧的单层边界。这主要用于 Superpowers 这类
workflow skill 派发 reviewer subagent 的场景，同时避免无限递归和后台 fanout。

当计划阶段需要隔离上下文做更深的调研时，用 `read_only_task`，而不是放开可写的
`task`。如果这类调研更适合复用已有 skill，用 `read_only_skill`。两者都会启动
ephemeral 只读 subagent，只暴露只读研究工具和安全前台 bash，只返回最终答案，不创建
可续接的 subagent transcript。只读嵌套委派会在 `max_subagent_depth` 内可用，但
可写的 `task` / `run_skill` 仍不可用。在 token economy 模式下，只用
`connect_tool_source(source="read_only_skill")` 连接这条窄入口；完整的 `skills`
source 仍会启用可写 skill 工具，plan mode 下继续阻断。

交互式前端中，计划模式默认手动开启。设置 `agent.auto_plan = "on"` 后，看起来复杂
的任务会自动进入 plan mode：Reasonix 先只读生成计划，待用户批准后才
编辑文件或执行有副作用的命令。`auto_plan_classifier` 可以指定便宜的 provider，例如
`deepseek-flash`；它只在边界输入上调用，分类失败会回退到启发式规则。也可以用
在 `reasonix` 会话里用 `/auto-plan off|on` 修改用户级设置，或在 shell/脚本里用
`reasonix config auto-plan off|on`。Auto-plan 只认用户级设置；项目
`reasonix.toml` 里的 `agent.auto_plan` 会被忽略。可见思考语言也采用类似形态：
会话里用 `/reasoning-language auto|zh|en`，shell/脚本里用
`reasonix config reasoning-language auto|zh|en`。Memory v5 使用 `/memory-v5 off|observe|compact|on|status`
或 `reasonix config memory-v5 off|observe|compact|on|status`，并且只认用户级设置。只有明确想为
reasoning-language 写项目级覆盖时，才给 shell 命令加 `--local`。

桌面端“协作方式”菜单里的计划模式、目标模式和省 token 模式的使用方法与注意事项，
见 [`COLLABORATION_MODES.zh-CN.md`](./COLLABORATION_MODES.zh-CN.md)。

桌面端“工具权限”里的询问、自动和 Yolo 模式的区别与使用场景，
见 [`TOOL_APPROVAL_MODES.zh-CN.md`](./TOOL_APPROVAL_MODES.zh-CN.md)。

分离 session（让各模型前缀缓存稳定）背后的取舍见
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator)。

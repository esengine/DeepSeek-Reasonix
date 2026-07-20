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
- [CLI 命令参考](./CLI.zh-CN.md)
- [环境变量](#环境变量)
- [Serve Web 前端](#serve-web-前端)
- [配置路径](./CONFIG_PATHS.zh-CN.md)
- [思考语言](./REASONING_LANGUAGE.zh-CN.md)
- [任务合约与暂停策略](./TASK_CONTRACT.zh-CN.md)
- [自定义 OpenAI-compatible provider](#自定义-openai-compatible-provider)
- [桌面端 Hooks](./DESKTOP_HOOKS.zh-CN.md)
- [快捷键](#快捷键)
- [权限与沙盒](#权限与沙盒)
- [能力诊断](#能力诊断)
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
# cursor_shape = "bar"             # block|underline|bar；CLI/TUI 输入光标

[agent]
reasoning_language = "auto"      # 可见思考过程语言：auto|zh|en
# plan_mode_allowed_tools = ["mcp__legacy__reader"]   # 旧 MCP 只读信任别名；不改变 Plan 可用性
# plan_mode_read_only_commands = ["gh issue view"]   # 仅兼容旧配置；Plan bash 现由 Permissions 决定
# planner_model = "deepseek-pro"      # 可选的低频规划器
# subagent_model = "deepseek-pro"     # runAs=subagent skill 的默认模型
# subagent_models = { review = "deepseek-pro", security_review = "deepseek-pro" }
# max_subagent_depth = 2              # 子代理嵌套委派深度；设为 1 可恢复旧的单层边界
# max_subagent_concurrency = 6        # 会话级子代理总并发（task/fleet/skills）
# max_parallel_writers = 3            # 互不重叠 write_paths 时的并行写入上限
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
# forbid_read    = ["${HOME}/.ssh"]   # agent 不可读取或列出的路径

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

旧字段 `[agent].plan_mode_allowed_tools` 仍会被解析并重新渲染。具体的
`mcp__<server>__<tool>` 条目继续充当本地只读信任别名，但推荐把已审计的 raw MCP reader
名称写到对应 server 的 `trusted_read_only_tools`。这个字段不会放行或阻断主 Plan 工作流中的调用。

`[agent].plan_mode_read_only_commands` 也继续参与配置 round-trip，但主 Plan 工作流不再维护独立的
bash allowlist 或信任提示。Plan 与常规模式使用相同的 Permissions 规则做 bash 分类和审批；Sandbox
仍是文件系统、进程和网络的强制边界。独立 planner 和显式只读 subagent runner 继续使用自己的严格
只读工具 registry 与前台命令分类器。

### 环境变量

多数日常设置应写在 `config.toml` 或前文提到的 Reasonix 全局 `.env` 中。下面这些变量是进程级高级开关；
需要在启动 Reasonix 之前设置。项目 `.env` 不是 Reasonix 控制变量的运行时来源。

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

# <Reasonix home>/config.toml
[serve]
auth_mode = "password" # none|token|password
password_hash = "$2a$12$..."
behind_proxy = true    # 仅可信反向代理后方使用
```

Web UI 提供聊天、工具审批、会话历史、rewind/fork/summarize、模型与 reasoning effort 控件、
Goal、由 `todo_write` 工具驱动的实时 Todo 面板，以及已配置 provider 的余额显示。临时启动可用
`--model`、`--max-steps` 或 `--resume`；不传 `--model` 时，`serve` 使用用户全局
`default_model`。

## 通过 ACP 接入编辑器

`reasonix acp` 把 Reasonix 作为 ACP v1 stdio agent 提供给编辑器和其他 host 客户端。
独立的 **[ACP 编辑器接入](./ACP.zh-CN.md)** 文档集中说明启动方式、能力协商、会话生命周期、
彼此独立的模型/工作/协作/审批控制轴、客户端文件与 terminal 能力、MCP server、权限请求，
以及 Reasonix 的回合中引导扩展。

## 远程 SSH

远程模块让 Reasonix 在远端主机上运行,并通过你自己的 SSH 连接访问它 —— 即 VS Code
Remote-SSH 式的体验。它在远端主机上引导一个常驻的 headless `reasonix serve`,把本地一个
回环端口转发过去,再经隧道打开现有的 serve Web 客户端。agent、工具与文件全部原生运行在远端
主机上,保真度 100%,不经过有损的文件代理。V1 支持 Linux 与 macOS 远端主机。

主机保存在 `config.toml` 的用户级 `[remote]` 段。与 `[secrets]` 一样,项目级
`reasonix.toml` 无法注入或覆盖远程主机 —— 克隆的仓库永远无法左右 Reasonix 向何处发起 SSH
连接。凭据沿用 provider 惯例:主机只记录环境变量名(`passphrase_env`、`password_env`),其值
存放在 Reasonix 全局 `.env` 中;私钥内容本身从不存储 —— `identity_file` 只是路径。

```toml
[remote]
[[remote.hosts]]
name          = "gpu-box"
host          = "203.0.113.7"
user          = "dev"
identity_file = "~/.ssh/id_ed25519"
workspace     = "~/projects/app"
serve_install = "auto"            # auto | npm | upload | never

[[remote.hosts.forwards]]
type   = "local"                  # local (-L) | remote (-R)
bind   = "127.0.0.1:5432"
target = "127.0.0.1:5432"
```

命令行:

```bash
reasonix remote add gpu-box dev@203.0.113.7 --workspace '~/projects/app'
reasonix remote import --all              # 导入别名；连接时通过 ssh -G 解析 Include/Match 等规则
reasonix remote test gpu-box              # 拨号 + 认证 + 主机密钥确认
reasonix remote connect gpu-box --open    # 引导 serve、建隧道、打开 URL
reasonix remote serve status gpu-box
reasonix remote fs ls gpu-box:'~/projects/app'
```

启用 `use_ssh_config` 的主机会通过本机 OpenSSH `ssh -G` 获取最终有效配置，因此支持
`Include`、通配 `Host`、`Match`（包括 `Match exec`）、多个 `IdentityFile`、`ProxyJump` 和
`IdentitiesOnly`。导入时只保存原始别名，不复制一份容易过期的解析结果。

`connect` 是前台守护(相当于 `ssh -N` 加上 serve 引导):它保持隧道与已配置的转发存活,断线时
以指数退避自动重连,并在重连后重新挂载转发。Ctrl-C 只断开本地一侧 —— 远端 serve 继续运行,
下次 `connect` 会复用它。V1 无后台守护进程。

主机密钥会对照你的 OpenSSH `~/.ssh/known_hosts`(只读)以及 Reasonix 托管的
`~/.reasonix/remote/known_hosts` 校验。首次见到的密钥会提示 TOFU 确认并记入托管文件;与已记录
密钥冲突的密钥会硬失败并指明出错的行,绝不自动接受。

远端侧状态位于远端主机的 `~/.reasonix/remote/`:`serve-<工作区 slug>.json`(pid、绑定的回环
地址、工作区)、`serve-<slug>.token`(0600;认证 token,经 `--token-file` 传给 serve,因此不会
出现在 `ps` 中)、`serve-<slug>.log`。

在桌面端,于 **设置 -> 远程 SSH** 管理主机,再通过状态栏徽标或主机行的 **远程浏览器** 按钮经
SFTP 浏览与编辑文件、管理端口转发、启动/打开远程工作区。打开工作区时会创建一个类似 VS Code
Remote SSH 的独立 Reasonix 原生窗口。主窗口持有 SSH 隧道；远程窗口是隔离的轻量外壳，不会恢复
或抢占本地对话会话。

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
| 上下文窗口 | Reasonix 用于自动清理上下文的 provider 级 token 预算。`0` 表示禁用自动 compaction。 | 按该 provider 的模型上下文上限填写；所选模型规格不同时使用下方的逐模型覆盖。 |

每个已选模型还提供一个可选的 **上下文窗口** 输入框。留空时继承 provider
级设置；填写正整数时只覆盖该模型。这样，同一端点下的长上下文模型不会过早
compaction，短上下文模型也不会在 Reasonix 清理前被服务端拒绝。
这里应填写模型文档标注的上下文窗口，而不是最大输出 token。例如 128K 通常填
`128000`；如果服务商明确标注 `131072`，则按该精确值填写。小于 16384 时界面会
显示非阻断警告，因为过小的窗口可能导致频繁 compaction 并降低缓存命中率。

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
桌面端仍用 `Shift+Tab` 切换 Plan；CLI 则用它在 Ask、Auto、Plan 之间循环。
`Ctrl/Cmd+Y` 只管 YOLO。桌面端粘贴继续走系统快捷键；CLI 则把终端原生文本粘贴
和应用接管的图片粘贴拆成不同快捷键。

`[ui].shortcut_layout` 仍被接受以兼容旧配置，但下面的快捷键行为已经跨布局统一。

CLI/TUI 文本输入可通过 `[ui].cursor_shape` 设置光标形状，支持 `underline`、`block`
和 `bar`。默认值是 `bar`：位置清晰，同时不会在中英混排输入时覆盖 CJK 双宽字符。
想使用传统终端块状光标可设为 `block`，偏好更弱的下划线光标可设为 `underline`。
该设置不影响桌面端或 Web 输入框。

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
| `Shift+Tab` | 切换 Plan 开/关 | Plan 只改变“先规划”的工作流；内置 writer 仍走当前 Ask/Auto/YOLO 与 Sandbox，MCP writer/destructive 目标在整个规划阶段保持硬阻断。 |
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

输入框上下边线使用当前主题强调色，默认光标为细竖线。长草稿会增长到可用的最大高度；
超过后，在输入框内滚轮只滚动草稿视图，不移动插入光标，在 transcript 区域滚轮仍滚动
对话。使用 `/theme auto|light|dark` 选择背景模式，也可运行不带参数的 `/theme` 查看
命名配色，再用 `/theme <style>` 选择强调色。

响应式底栏左侧保留当前 Ask/Auto/Plan 或 YOLO 姿态和交互状态；终端较宽时，模型、推理
强度和工作模式作为一组靠右显示，第二行按可用性显示 Git 标识、缓存命中率、上下文占用、
压缩余量、后台任务和余额。“就绪”只表示输入框空闲，并不是模型健康检查；选择器、审批、
图片粘贴、shell 模式等活动会替换这个状态。窄终端会按完整信息组移动、换行或压缩。
标签和展示用的工作模式值跟随 `/language`，但 `/work-mode` 的命令参数继续使用稳定的
英文标识。

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
| transcript 文本选择 | 复制 transcript 文本 | 应用内拖选松开后，本地会话通过可验证的系统剪贴板路径写入（macOS `pbcopy`、Linux 可用的 Wayland/X11 工具、Windows 系统剪贴板）；SSH 才回退到 OSC 52，并明确标记为回退而不是宣称原生复制成功。`Ctrl+C`/`Super+C`/`Meta+C` 或右键当前选区可再次复制。 |
| 输入框文本选择 | 选中、复制或替换草稿文本 | 应用内拖选松开后，会通过与 transcript 相同的可验证剪贴板路径复制；输入或粘贴会替换选区，方向键会收起选区。 |
| 没有活动选区时右键 | 在本地会话粘贴剪贴板文本 | 本地会话开启鼠标接管时，Reasonix 只读取文本并交给正常的 bracketed-paste 处理。SSH 下远端进程无法读取本机剪贴板，请使用终端粘贴快捷键；`/mouse` 可恢复终端原生右键菜单。存在活动选区时，右键仍优先复制该选区。 |
| `/mouse` | 切换应用内鼠标接管 | 关闭后由终端处理原生拖选和右键菜单，但会失去应用内选区、滚动条和滚轮。可用 `REASONIX_DISABLE_MOUSE=1` 让每次会话默认关闭。 |
| `Ctrl+C` | 复制、取消、清空或退出 | 有 transcript 或输入框活动选区时优先复制；否则取消运行中的 turn、清空非空输入，或在空输入下连按两次退出。 |
| `Ctrl+D` | 退出 TUI | 立即退出。 |
| 终端的文本粘贴快捷键 | 粘贴文本 | 文本保持终端原生 bracketed-paste 路径：macOS 通常是 `Cmd+V`，Linux 通常是 `Ctrl+Shift+V`，其它环境使用终端自身配置。Reasonix 只消费收到的文本粘贴事件，不会先探测图片。 |
| macOS/Linux `Ctrl+V`；Windows `Alt+V` | 粘贴剪贴板图片 | 图片粘贴是独立的应用动作。读取期间底栏显示“正在粘贴图片…”，完成后在光标处插入可编辑的 `[image #N]` 标记。 |
| `/paste-image` | 粘贴剪贴板图片 | 与图片快捷键相同的纯图片命令入口。 |
| 以 `!` 开头的一行 | 直接运行 shell 命令 | 命令在本地执行，不经过模型。 |

模式与显示：

| 按键或命令 | 作用 | 说明 |
| --- | --- | --- |
| `Shift+Tab` | 按 Ask → Auto → Plan → Ask 循环 | YOLO 不进入这个输入模式循环；底部状态栏会显示当前模式。 |
| `Ctrl+Y` | 切换 YOLO 开/关 | 关闭 YOLO 时会尽量恢复之前的 Ask/Auto 基底。终端若能转发 Command/Super，也可能识别 `Cmd+Y`，但稳定可用的是 `Ctrl+Y`。 |
| `--yolo`、`--dangerously-skip-permissions` | 启动时进入 YOLO | 和 `Ctrl+Y` 是同一个运行时模式。 |
| `/work-mode [economy|balanced|delivery]` | 查看或切换当前会话的工作模式 | `/profile` 是兼容别名。切换会原子重建运行时，保留对话和审批姿态；有工作正在进行时会拒绝切换。 |
| `/theme [auto|light|dark|style]` | 查看或切换 CLI 主题 | 不带参数会列出背景模式和命名配色。选择会保存到用户配置；单次运行可用 `REASONIX_THEME` 和 `REASONIX_THEME_STYLE` 覆盖。 |
| `Ctrl+O` | 切换详细 reasoning 显示 | 也可通过 `/verbose` 使用。 |
| `Ctrl+B` | 展开或收起较长 shell 输出 | 较长 shell 输出的提示行也可点击；全屏 TUI 开启鼠标接管时，文本选区由应用内处理。 |
| `/goal <目标>`、`/goal --research <目标>`、`/goal --simple <目标>`、`/goal status`、`/goal clear` | 启动、查看或清除 Goal | Goal 不进入任何快捷键循环；明显长周期目标会自动启用 AutoResearch。普通输入命中强 AutoResearch 信号时也会自动升级为 Goal。 |
| `/migrate`、`/migrate --from <旧目录>` | 重试旧数据迁移，或从指定 v0.x 来源导入 sessions | Windows v0.52 自定义安装/数据目录用 `--from`；该形式只导入 sessions。详见[配置路径](./CONFIG_PATHS.zh-CN.md)。 |

选择器与审批：

| 上下文 | 按键 | 作用 |
| --- | --- | --- |
| 斜杠或 `@` 补全 | `Up` / `Down`、`Ctrl+P` / `Ctrl+N`、`Tab` / `Enter`、`Esc` | 移动、接受或关闭补全菜单。 |
| 工具审批提示 | `y`/`1`、`a`/`2`、`p`/`3`、`n`/`4`、`Enter`、`Esc`、`Ctrl+C` | 允许一次、本会话允许、持久允许、拒绝、默认允许一次、拒绝，或取消当前 turn。 |
| Ask 问题卡 | `Up`/`Down` 或 `j`/`k`、`Left`/`Right` 或 `h`/`l`、`Space`、`Enter`、`1`-`9`、`Esc`、`Ctrl+C` | 导航答案/问题标签、切换多选、提交/激活、选择编号选项、关闭，或取消当前 turn。 |
| Rewind 选择器 | `Up`/`Down` 或 `j`/`k`、`Enter`、`b`、`c`、`d`、`f`、`s`、`u`、`Esc` | 选择 turn，应用 both/conversation/code/fork/summarize 动作，或返回/关闭。 |
| 模型、provider 或 Resume 选择器 | `Up`/`Down` 或 `Ctrl+P`/`Ctrl+N`；搜索词为空时可用 `j`/`k`；输入文字过滤；`Enter`；`Esc` | 搜索、选择或关闭选择器；开始搜索后 `j`/`k` 会作为查询字符输入；`/provider` 会继续打开该 provider 的模型列表。 |
| MCP 导入选择器 | `Up`/`Down` 或 `j`/`k`、`Space`、`Enter`、`Esc` / `Ctrl+C` | 移动、勾选服务器、导入勾选服务器，或取消。 |
| MCP 管理器 | `Up`/`Down` 或 `j`/`k`、`Enter`、`Left`/`Right` 或 `h`/`l`、`r`、数字键、`q` / `Ctrl+C` | 导航服务器列表/详情、刷新、选择动作，或关闭。 |
| `/clear` 确认 | 方向键或 `j`/`k` / `Tab`、`Enter`、`y`、`n`、`Esc` / `Ctrl+C` | 在 Clear/Cancel 间切换、确认清空，或取消。 |

模式含义：

| 模式 | 含义 |
| --- | --- |
| Ask | writer 兜底审批时询问。 |
| Auto | 自动放行兜底审批；显式 `ask` / `deny` 规则仍生效。 |
| YOLO | 跳过普通工具审批；`deny`、用户 `ask` 问题和计划批准提示仍会等待。 |
| Plan | 要求模型先规划——这是 plan-first 工作流，不是全部工具只读。内置 writer 仍遵守当前 Ask/Auto/YOLO 与 Sandbox；已安装 MCP writer、destructive 目标与未信任 reader 在整个规划阶段硬阻断（审批不能放行，退出 Plan 后恢复）；`complete_step` 等显式阶段工具需等到计划批准后。 |
| Goal | 持续追一个已保存目标，直到完成、阻塞或清除。 |

## 权限与沙盒

权限逐次调用把关：`deny` > `ask` > `allow` > 兜底。Bash 和文件修改都要审核；
只读工具一般不需要。审核规则不是按“按钮文案”存，而是按权限规则匹配，比如
`Bash(npm run build)`、`Bash(npm run test:*)`、`Edit(docs/**)` 这种形式。
`reasonix` 会在 writer 调用前征求同意（普通工具为 `1` 本次 · `2` 本会话允许此范围 · `3` 总是允许此范围（保存） · `4` 拒绝；Bash 可额外选择命令前缀授权）；
其中 Bash 默认按具体命令记，也可按安全推导出的命令前缀记（如 `Bash(go test:*)`）；文件编辑类工具的本会话授权按编辑能力记，持久授权则写入 `Edit(<path>)` 文件路径规则；
`reasonix run` 保持自主运行但仍然遵守 `deny`。

Ask 不是只读模式：writer 获得批准后仍会执行。Permissions 决定放行或询问，Sandbox 才是强制能力边界。

权限是**策略**（哪些调用放行/询问），**沙盒**是**强制**：文件写工具
（`write_file` / `edit_file` / `multi_edit` / `move_file`）拒绝 `[sandbox] workspace_root`
之外的任何路径（默认当前目录，编辑不出项目），并解析符号链接与 `..`，使链接无法
打洞越界。`forbid_read` 可选地隐藏敏感文件或目录，使 agent 的读文件、列目录和搜索工具不能读取或列出它们；
建议使用绝对路径或 `${HOME}` / `${VAR}`，不要写 `~`，因为配置只做环境变量展开。
`bash` 本身默认进 OS 沙盒（`[sandbox] bash`：macOS 使用 Seatbelt，Linux 使用 bubblewrap）：
命令只能写这些 root（外加平台按命令提供的临时/缓存 root），
OS 沙盒生效时也不能读取配置的 `forbid_read` roots，`[sandbox] network` 为真时才能联网。
Reasonix 始终会从工具子进程环境中移除已保存的 provider 与 bot 凭据变量，并自动把
全局凭据 `.env` 加入运行时禁读边界；项目 `.env` 仍保持现有的 workspace 范围行为。
**Windows 说明：**Reasonix 不在 Windows 上提供 OS 级 Bash 沙箱，生效模式固定为
`off`。旧配置即使写了 `bash = "enforce"` 也会解析为 `off`，`reasonix doctor`
会提示该设置被忽略，桌面设置中的选择器也为只读。Bash 命令会在不受 OS 沙箱限制的
环境中运行；专用文件工具仍会在进程内执行 `workspace_root`、`allow_write` 和
`forbid_read` 边界。已保存的凭据变量仍不会进入子进程环境，但获得批准的无沙箱 shell
以当前用户身份运行，不能作为保护其他用户可读文件的安全边界。

没有可用 OS 沙盒时，`bash = "enforce"` 会拒绝 bash 执行，不会无沙盒运行。
Windows 上兼容的值始终为 `off`。

反馈编码质量问题时，可运行 `reasonix doctor quality <branch-id-or-path>`（加
`--json` 输出结构化结果）。命令会读取指定 session，但只输出不含内容的计数与
Profile 分类：模型家族、运行模式、协作/审批模式、消息和工具调用数、验证与已持久化的
compaction 摘要数，以及可用时的桌面端 token/cache telemetry。结果不会包含对话正文、
路径、session 标识、工具参数与输出、服务端点或自定义模型名，适合粘贴到公开 Issue
或 Discussion。它不同于 `reasonix doctor session`：后者生成的支持 zip 含完整未脱敏
会话，只能在可信支持渠道分享。

## 能力诊断

当 skill、斜杠命令、Hook、插件包、MCP 或 `AGENTS.md` 缺失、被覆盖、未信任或启动失败时，用统一只读诊断。完整参数、JSON schema 与 issue code 见
**[能力诊断](./CAPABILITY_DIAGNOSTICS.zh-CN.md)**。

```bash
# 静态（默认）：无网络、不启动 MCP 子进程
reasonix doctor capabilities

# 机器可读（stdout 仅为合法 JSON）
reasonix doctor capabilities --json

# 指定工作区
reasonix doctor capabilities --root /path/to/project

# Live MCP 探测——仅在你明确允许启动第三方服务器时使用
reasonix doctor capabilities --live --timeout 5s
```

| 入口 | 用法 |
| --- | --- |
| CLI | 见上方 `reasonix doctor capabilities` |
| 桌面端 | **设置 → 诊断** — 刷新、复制脱敏 JSON、可选「包含当前会话运行状态」（只读活动标签 Host，**不**启动 MCP） |
| Agent | `/reasonix-guide`（内置 inline Skill）或自然语言描述症状；优先静态 doctor JSON，再问是否 `--live` |

退出码：`0` 允许 warning/info；`1` 表示存在 `error`（或 live 启动失败）；`2` 为参数错误。与 `reasonix doctor`（provider/沙箱）以及 `reasonix plugin doctor <name>`（单个插件包）相互独立。

## 插件（MCP）

Reasonix 是一个 MCP 客户端。`[[plugins]]` 的 `type` 选择传输：`stdio`（默认）启动本地子进
程（`command`/`args`/`env`）；`http`（Streamable HTTP）连接远程 `url`，可带静态
`headers`（`${VAR}` / `${VAR:-default}` 从环境展开，密钥不入文件）。

普通配置流程现在只有一步：使用桌面端的“添加并连接”、`/mcp add`，或直接让 Reasonix
安装一个 package、URL 或 `.mcp.json`。这次明确安装本身就是授权：server 会保存并在当前
会话连接，现在和下次启动都不会再弹出第二套信任步骤。显式 deny 仍然优先；未配置高级审批
覆盖时，包括声明 `destructiveHint` 的工具在内都直接执行。如需保留新鲜审查，可主动设置
`auto`、`prompt` 或 `writes`。Plan 与只读 subagent 仍只暴露符合条件的工具身份。只有被动从仓库
`reasonix.toml` 或 `.mcp.json` 发现的 server 会在第一次启动前，请用户确认一次精确命令或
地址；Reasonix 会先记录该决定而不启动临时检查进程，然后只启动一次正式连接。内容不变时
以后自动连接，发生变化时才重新确认。

stdio server 从初始化到读写都复用同一个进程，因此浏览器等有状态 MCP 能保留会话和
已打开页面。由于进程启动后无法按调用切换 OS 沙箱，这个共享进程始终使用该 server 的普通
writer 沙箱；`readOnlyHint` 与只读 subagent 过滤属于调用分发策略，不再对应第二个按调用隔离
的进程沙箱。

工具以 `mcp__<server>__<tool>` 暴露给模型，与 Claude Code 一致；声明 MCP `readOnlyHint: true`
的工具会参与并行调度并命中普通权限层的只读默认放行。这个标注来自第三方 server，主 Plan 只把它
当作普通权限分类；它不会让工具进入独立 planner 或只读研究 subagent。已审计的 reader 应写入本地
`trusted_read_only_tools`。没有 `readOnlyHint` 的工具仍按写工具处理。计划期间，内置 writer 仍走
Permissions/Sandbox；已安装 MCP 与代理解析后的 MCP writer、destructive 目标和未信任
reader 在任何审批前硬阻断，退出 Plan 后才恢复正常审批流程。

MCP `destructiveHint: true` 比只读分类更严格。在 `auto`、`prompt` 或 `writes` 下，即使工具
同时声明 `readOnlyHint`、当前是 Auto/YOLO，或已经存在 allow 规则，每次破坏性调用仍需要
全新的人工审批——Guardian、`auto_review`、自动/YOLO 与会话授权都不能代答。最终策略为
`approve` 表示用户已经授权该 server 或工具，因此调用直接执行。

`approvals_reviewer = "auto_review"` 只把真正需要审查的调用——`prompt` 模式、`writes`
命中的写调用、以及全局策略本会 Ask 的 `auto` 调用——交给当前会话的 Guardian；成功给出的
allow/deny 结论立即最终生效。reviewer 缺失、超时、失败或未给出结论时，调用降级为一次
全新的人工审批：自动/YOLO、计划批准窗口与会话授权都不能代答。非交互运行和子代理在所有
需要 reviewer 的场景下直接拒绝。

server 和 raw tool 的审批策略只存在于本地，不会改变发给模型的 schema：

```toml
[[plugins]]
name = "github"
command = "github-mcp"
default_tools_approval_mode = "writes" # auto|prompt|writes|approve
tools = { "delete_repository" = { approval_mode = "prompt" } }
approvals_reviewer = "auto_review"     # user|auto_review
trusted_read_only_tools = ["issue_read", "pull_request_read"]
```

用户已授权的 server 若省略这些高级审批字段，所有调用都会直接放行。显式配置后，`auto`
交给全局 Ask/Auto/YOLO；`prompt` 每次调用都审查；`writes` 只审查写工具；`approve`
直接放行所有调用，包括 destructive 调用。显式 deny 永远优先；除 `approve` 外，
`destructiveHint` 会在其余模式下强制一次新审查。`tools` 中的 raw tool 配置覆盖 server 默认值。
`trusted_read_only_tools` 继续作为兼容字段和显式本地声明，用于已审计、但没有可靠 annotation
的 reader。

两条边界值得注意：`writes` 信任 server 自己的只读分类，把写工具谎报成 `readOnlyHint`
的 server 会绕过这层审查——对不可信的 server 请用 `prompt`。启用 Guardian 且未配置
`approvals_reviewer` 时，`prompt`/`writes` 的审查保留 legacy 路由：Guardian 预审通过即可放行、
不再弹人工提示；要求每次都由人决定时请显式配置 `approvals_reviewer = "user"`。项目
`.mcp.json` 会把这些字段并入会话，所以像审代码一样审查别人仓库里的 `approve`/`writes`
策略——显式 deny 规则仍然生效；最终策略不是 `approve` 时，`destructiveHint` 审查仍然生效。

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
若要跨 skills / hooks / 插件包 / MCP 做只读健康检查（不改配置），见
[能力诊断](./CAPABILITY_DIAGNOSTICS.zh-CN.md)
（`reasonix doctor capabilities` 或 **设置 → 诊断**）。

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

交互式 `reasonix` 会话里，内置命令（`/compact`、`/new`、`/clear`、`/rewind`、`/tree`、`/branch`、`/switch`、`/todo`、`/model`、`/work-mode`、`/mcp`、`/skills`、`/hooks`、`/memory`、`/goal`、`/output-style`、`/sandbox`、`/language`、`/auto-plan`、`/reasoning-language`、`/help`）在本地执行——`/help` 可列出全部。
内置 **Skill**（如 `/init`、`/explore`、`/test`、`/reasonix-guide`）也会出现在斜杠菜单，
并可通过 `run_skill` 调用（正文按需加载；只有索引行进入缓存稳定前缀）。配置或能力排障时
用 `/reasonix-guide`，它会引导运行 `reasonix doctor capabilities`（见
[能力诊断](./CAPABILITY_DIAGNOSTICS.zh-CN.md)）。
`/new` 会开启新会话，同时保存之前的 transcript 供历史记录和恢复使用；`/clear` 会二次确认，确认后丢弃当前上下文且不保存。
`/tree` 查看已保存的对话分支，`/branch [name]` 从当前对话末端分支，`/branch <turn> [name]`
从较早的 checkpoint 轮次分支，`/switch <id|name>` 切换到另一个分支。**自定义命令**
是放在 `.reasonix/commands/`（项目）或 `~/.reasonix/commands/`（用户）下的 Markdown 文件——
`review.md` 即 `/review`，子目录构成命名空间（`git/commit.md` → `/git:commit`）。文件正文
是 prompt 模板，调用即作为一轮对话发出。

### 子智能体 Profile

子智能体 profile 是带有 `runAs: subagent` 和 `invocation: manual` 的手动 Skill。
它与桌面设置页共用项目级/全局 Skill 目录，因此任一端创建的 profile 在会话刷新后都会被
另一端发现。交互式聊天里使用 `/<name> <任务>` 调用；Reasonix 会启动隔离子智能体，
父会话只保留任务和最终答案。

Headless CLI 提供显式管理和运行命令，同时不改变普通 `reasonix run` 的任务语义：

```bash
reasonix subagent list
reasonix subagent create reviewer --description "审查改动" --prompt-file reviewer.md --tools read_file,grep,bash
reasonix subagent edit reviewer --effort high --model deepseek-pro
reasonix subagent try reviewer "审查当前 diff"   # 始终只读
reasonix subagent run reviewer "审查并修复当前 diff"
reasonix subagent delete reviewer --yes
```

workspace 可用时，`create` 默认写入项目级目录，否则默认写入全局目录；可用
`--scope project|global` 明确选择。`edit` 只修改显式传入的字段，`--model=`、`--tools=`
这类空值会清除对应配置。Profile 编辑器会拒绝
custom path 或包含更多手写结构的 Skill，避免丢失 frontmatter、references 或 scripts；
这些文件仍应通过 Skills 工作流管理。内置 profile 没有可编辑文件，因此 `edit` 对它们只接受
`--model` 和 `--effort`，并写入与桌面设置页相同的按名称覆盖配置。

完整 CLI 参数、Skill 文件格式、模型优先级、安全行为和排障说明见
[子智能体 Profile](./SUBAGENT_PROFILES.zh-CN.md)。

`/memory` 会同时列出记忆文档（`REASONIX.md` / `AGENTS.md`）和已保存的 auto-memory 条目。
在 agent 回合中，只读的 `history` 和 `memory` 工具可以按需检索历史 session 决策、
compaction archive 和已保存事实；这些动态内容不会被塞进稳定的 system prompt 前缀。
`/forget <name>` 会把已保存事实归档而不是永久删除；CLI/TUI 和桌面记忆面板能显示归档文件用于追溯，
但它们不会作为 active memory 被检索。检索会保留 BM25 最强命中，同时裁掉弱的泛词命中；
agent 发起的 `remember` 和 `forget` 每次都会要求新的人工确认，并在执行前展示将保存或归档的记忆摘要；
Guardian 审查不能代替用户批准，非交互运行会拒绝这类工具而不是自动批准。
0 结果会提示 agent 改用更少、更有区分度的词继续查。
Memory v5 执行编译器已经移除。早期版本（至 v1.17.x）可能把用户轮次编译成
`<memory-compiler-execution>` contract 并写入本地编译器状态；当前版本不再有这两种行为，
`[agent].memory_compiler` 配置键已退役（一次性迁移会从既有配置中清除），那些旧版本录制的
会话转写仍能正常显示——预览和历史会从 legacy contract 块中恢复原始提示词。
会话记忆检索的技术实现细节见 [`SESSION_MEMORY_RETRIEVAL.md`](SESSION_MEMORY_RETRIEVAL.md)。

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

`reasonix setup` 现在统一管理 provider、模型列表、凭据、连接测试和默认模型；所有修改
会暂存到“保存并退出”，并同步维护桌面端 provider access。完整用法见
[CLI 命令参考](./CLI.zh-CN.md#配置供应商)。若要让两个模型协同（执行器 + 规划器，
各自独立、缓存稳定的 session），向导后手动在 `reasonix.toml` 加一行即可：

```toml
[agent]
planner_model = "deepseek-pro"   # 作为低频规划器
```

Planner 会看到已加载的 `REASONIX.md` / `AGENTS.md` 记忆，并拿到一小组只读研究工具，
因此可以先检查相关文件再把计划交给执行器。写入类和流程类工具仍只给执行器使用。
Reasonix 会自动管理正常执行：活跃 Todo 连续 8 个工具调用轮次没有新的完成项、唯一读取、
命令或修改时，宿主会要求执行器重新评估；连续 16 个无进展轮次后暂停并保存工作，可在
下一轮用户消息中继续。完全重复的操作不算进展，新的宿主可观测工作会自动续期。两级任务
列表保持同一"唯一当前项"契约：唯一的 `in_progress` 是活跃的 level-1 子步骤，其 level-0
阶段保持 `pending`；子步骤按顺序推进并签核，全部完成后阶段本身转为 `in_progress` 做
最后签核。

升级时仍可解析已有的 `[agent].max_steps` 和 `planner_max_steps`，但其值会被忽略，并在一次性
迁移提示后从配置中移除，避免隐藏的旧上限截断自动进度管理或子 Agent 的继承任务。确实需要
为单次运行设置预算时使用 CLI `--max-steps`；无人值守 Bot 仍保留 `[bot].max_steps`。

Subagent skills 默认继承执行器模型。设置 `subagent_model` 可让它们统一走另一个已配置
模型；设置 `subagent_models` 则只覆盖 `review`、`security_review` 等指定 skill。

Subagent 默认允许再委派一层：根会话是 depth 0，第一层 subagent 是 depth 1，
`max_subagent_depth = 2` 表示 depth 1 的 workflow 可以再派 depth 2 的 reviewer
或 implementer；depth 2 不再拿到递归 agent/skill 工具。设
`agent.max_subagent_depth = 1` 可恢复旧的单层边界。这主要用于 Superpowers 这类
workflow skill 派发 reviewer subagent 的场景，同时避免无限递归和后台 fanout。

当计划阶段需要**明确隔离为只读**的深度调研时，用 `read_only_task`；如果更适合复用已有 skill，
用 `read_only_skill`。两者都会启动
ephemeral 只读 subagent，只暴露只读研究工具和安全前台 bash，只返回最终答案，不创建
可续接的 subagent transcript。只读嵌套委派会在 `max_subagent_depth` 内可用，其内部仍不提供
可写的 `task` / `run_skill`。在 token economy 模式下，只用
`connect_tool_source(source="read_only_skill")` 连接这条窄入口；完整的 `skills`
source 也可在 Plan 中加载，后续 writer 调用仍通过 Permissions/Sandbox。

所有严格只读子会话都经过同一对共享构造入口——批处理子会话用
`RunReadOnlySubAgentWithSession`，交互式双模型 planner 用 `NewReadOnlyAgent`——
两者都会把子会话标记为永久只读并做最终 registry 过滤：移除 writer、destructive MCP
目标、只有第三方 server hint 的 reader，以及一切会改变 host capability 的工具。MCP reader
只有在本地配置中显式声明，或由当前已验证的官方签名 package 声明时才符合条件；符合条件的
reader 仍可按需启动。严格只读入口一览：

| 入口 | 用途 |
| --- | --- |
| `read_only_task` | 主会话派生的隔离只读调研子会话 |
| `parallel_tasks`（只读） | 并发只读调研子会话 |
| `fleet` 且 `read_only: true` | 可带 Profile 的并行批量（单项强制只读） |
| `read_only_skill` | 以既有 skill 驱动的同等隔离 |
| `reasonix review`（CLI） | 只读评审 diff 或分支 |
| 桌面端 preview/review 子代理 | 桌面端只读分析面 |
| 双模型 planner | 独立 planner 的只读 registry |

在严格只读子会话内：`use_capability` 在 Commit/permission/hook/执行前会对解析出的
真实目标再次校验；未连接且符合条件的 MCP reader 可从当前 schema cache 按需启动，
initialize/tools-list 后会在 `tools/call` 前核对缓存与 live security 指纹；发现 schema 或
safety 漂移则零执行，普通重试会使用当前策略。
`auto_review` 在这里不能提升权限；需要本地人工审批的 reader 直接 fail-closed。
这一层比主 Plan 更严格：Plan 在整个规划阶段硬阻断 MCP writer/destructive 目标——
审批也不能放行，退出 Plan 后才恢复——内置 writer 仍走 Permissions/Sandbox，
而严格只读子会话根本不暴露 writer。

启动会话时可以用 `--profile economy|balanced|delivery` 选择运行模式，例如
`reasonix run --profile delivery "修复并验证这个 bug"`。Economy（轻量）初始只带 9 个工具：
直接读/bash/编辑/写入、后台 shell 生命周期控制、`ask` 和 `connect_tool_source`；专用搜索/文件/
workflow 工具、session history、memory 写入、slash command、Skills、MCP、LSP、网络、安装与
subagent 都在任务需要时才连接。
Balanced（均衡）是提供完整工具面的默认档；Delivery（交付优先）
保留完整工具面，额外增加稳定能力代理 `use_capability`（按需 inspect/call MCP，包括
`auto_start=false`，且不改变主工具 Schema），并增加“明确验收标准、修复根因、运行验证、复审最终
diff”的稳定交付合约。该合约由宿主运行时强制执行：没有具体 `todo_write` 验收清单时会阻止变更和验证
命令；发生变更后，必须复查结果、在最后一次变更之后运行验证，并用带证据的 `complete_step` 签收后才能
结束；Skill/MCP 的 require/prefer 路由会被门禁；中/高风险改动强制结构化 review；`task`/`run_skill`
等元工具本身不算 mutation。纯只读分析不会被迫产生写入。

交互式 TUI 会话内可用 `/work-mode` 查看当前模式，或用
`/work-mode economy|balanced|delivery` 热切换；`/profile` 是兼容别名。切换会原子重建
Controller，同时保留 history、session 路径、Lease 和 Ask/Auto/Yolo 审批姿态；当前 turn、审批/询问、
后台任务或另一场运行时切换尚未结束时会拒绝切换。构建失败时旧 Controller 继续可用。该命令只修改当前
会话，不持久化新的全局默认值。跨 Profile 切换会产生一次新的 provider 缓存前缀。均衡与交付优先模式下，
system contract 和工具 Schema 在后续轮次保持稳定；轻量模式下，每次成功调用 `connect_tool_source`
都会在下一次请求加入对应工具 Schema，形成一次新前缀，之后在工具面再次变化前保持稳定。

桌面端标签页提供相同三档并持久化轻量或交付优先
模式；旧的空值/`full` 继续解释为均衡模式。

交互式前端中，计划模式默认手动开启。设置 `agent.auto_plan = "on"` 后，看起来复杂
的任务会自动进入 plan mode：Reasonix 先生成计划，待用户批准后工作流才切换到实施；规划期间的
工具调用仍遵守当前 Permissions 与 Sandbox。`auto_plan_classifier` 可以指定便宜的 provider，例如
`deepseek-flash`；它只在边界输入上调用，分类失败会回退到启发式规则。也可以用
在 `reasonix` 会话里用 `/auto-plan off|on` 修改用户级设置，或在 shell/脚本里用
`reasonix config auto-plan off|on`。Auto-plan 只认用户级设置；项目
`reasonix.toml` 里的 `agent.auto_plan` 会被忽略。可见思考语言也采用类似形态：
会话里用 `/reasoning-language auto|zh|en`，shell/脚本里用
`reasonix config reasoning-language auto|zh|en`。只有明确想为
reasoning-language 写项目级覆盖时，才给 shell 命令加 `--local`。

桌面端“协作方式”菜单里的计划模式、目标模式和“轻量 / 均衡 / 交付优先”三档运行模式的使用方法与注意事项，
见 [`COLLABORATION_MODES.zh-CN.md`](./COLLABORATION_MODES.zh-CN.md)。

桌面端“工具权限”里的询问、自动和 Yolo 模式的区别与使用场景，
见 [`TOOL_APPROVAL_MODES.zh-CN.md`](./TOOL_APPROVAL_MODES.zh-CN.md)。

分离 session（让各模型前缀缓存稳定）背后的取舍见
[`SPEC.md` §3.5](./SPEC.md#35-two-model-collaboration-coordinator)。

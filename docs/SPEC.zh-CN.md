# Reasonix 工程规格

<a href="./SPEC.md">English</a>

> Reasonix 是一个 coding agent：由极薄的 harness 驱动多个模型，所有能力都由配置和插件提供。本文是工程契约，代码应遵循它；需要改变行为时，应先更新契约，再修改代码。

英文原文是规范性版本；本文按相同章节提供中文说明，代码标识符、配置键和协议名保持原样。

## 1. 设计原则

1. **配置与插件驱动。** 核心只依赖接口；具体模型和工具通过 registry 按名称解析、在配置中声明，或由插件注入，不硬编码 `switch model`。
2. **单一静态二进制。** 使用 `CGO_ENABLED=0`，一条命令完成跨平台编译，CLI 开箱即用。
3. **精简依赖。** 默认使用标准库。第三方依赖必须是纯 Go、足够轻量，且不能破坏单二进制、跨平台和分发体验；TOML parser 是当前唯一接受的基础依赖。
4. **两级扩展。** 编译期 built-in 通过 `init()` 自注册；运行时外部插件以 stdio JSON-RPC 子进程或 MCP 兼容传输接入。
5. **接口优先、registry 驱动。** `Provider` 与 `Tool` 都是接口。
6. **持续演进，不过度设计。**

所有代码、注释、面向用户的字符串、工具描述、system prompt 和英文规范以英语为主；README 同时维护英文版 `README.md` 与中文版 `README.zh-CN.md`。

## 2. 目录与依赖方向

```text
reasonix/
├── go.mod / go.sum
├── Makefile
├── README.md / README.zh-CN.md
├── reasonix.example.toml
├── docs/SPEC.md / docs/SPEC.zh-CN.md
├── cmd/reasonix/main.go
├── cmd/reasonix-plugin-example/
└── internal/
    ├── cli/
    ├── config/
    ├── provider/
    │   └── openai/
    ├── tool/
    │   └── builtin/
    ├── permission/
    ├── command/
    ├── plugin/
    ├── remote/
    │   ├── forward/
    │   ├── sftpfs/
    │   └── bootstrap/
    └── agent/
```

核心依赖方向保持无环：

```text
cli → {agent, plugin, config} → {tool, provider}
```

`provider/openai`、`tool/builtin` 等 built-in 子包导入父包完成自注册，父包不反向导入子包。Remote-SSH 采用 `cli → remote/bootstrap → remote` 的分层，`remote` 及其子包不依赖 `cli`、`agent` 或 `serve`；host key 和 secret prompt 等交互都通过 callback 暴露，供桌面端复用。

## 3. 核心抽象

### 3.1 Provider 与 registry（`internal/provider`）

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

type Factory func(cfg Config) (Provider, error)

func Register(kind string, f Factory)
func New(kind string, cfg Config) (Provider, error)
```

- `openai` kind 实现 OpenAI-compatible `/chat/completions`。
- OpenAI-compatible vendor 只是 `kind = "openai"` 的不同配置实例，通过 `base_url`、`model`、`api_key_env` 区分；新增兼容模型通常只需改配置。
- 一个 provider 表示一个 vendor endpoint，可通过 `models` 暴露多个模型，并以 `default` 指定默认项。`default_model`、`--model` 和桌面端模型选择器都经 `Config.ResolveModel` 解析，可接受 provider 名、裸模型名或 `provider/model`。
- `context_window` 是 provider 级默认值；`model_overrides.<model>.context_window` 可覆盖单个模型。
- streaming tool-call delta 在 provider 内按 index 聚合，只向上层发出完整 `ToolCall`。

### 3.2 Tool 与 registry（`internal/tool`）

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}
```

- built-in tool 通过 `tool.RegisterBuiltin` 注册到进程级集合。
- 每次运行创建独立 `*Registry`，由启用的 built-in 与插件工具组成；agent 只看到该 registry。
- tool schema 在插入 registry 时 canonicalize；内置契约见[工具合约](./TOOL_CONTRACT.zh-CN.md)，测试会校验文档与 canonical schema 不漂移。
- `Execute` 自行解析原始 JSON 参数。错误作为结果返回给模型，让模型有机会自我修正，而不是直接终止进程。

### 3.3 插件与 MCP（`internal/plugin`）

外部插件是配置中声明的 MCP server。协议统一为 JSON-RPC 2.0，传输由 `transport` 接口抽象：

- `stdio`：本地持久子进程，每行一条 JSON 消息。
- `http` / `streamable-http`：向远程 `url` POST，支持 `application/json` 和 SSE 响应，并复用 `Mcp-Session-Id`。
- `sse`：兼容旧版 2024-11-05 HTTP+SSE；持久 GET 接收 server 公布的相对 POST endpoint、JSON-RPC 响应与 server 消息。为避免静态 header 泄漏，会拒绝跨域 endpoint。

`${VAR}` 与 `${VAR:-default}` 可用于 `command`、`args`、`env`、`url` 和 `headers`，使 secret 留在环境中。生命周期为 `initialize` → `notifications/initialized` → `tools/list`，调用使用 `tools/call`。

存在工作区根目录时，初始化会声明 `roots` 能力，并用文件 URI 响应 `roots/list`。`tools/call` 会附带逐调用 `_meta.progressToken`；匹配的 `notifications/progress` 会进入现有工具进度事件链路。

远程工具适配为 `Tool`，命名为 `mcp__<server>__<tool>`。`annotations.readOnlyHint` 映射为 `Tool.ReadOnly()`，默认 false；只有显式声明为只读的工具才进入并行读取与默认只读权限路径。MCP prompt 暴露为 slash command，resource 可通过 `@<server>:<uri>` 引用。

### 3.4 Agent loop（`internal/agent`）

`Session` 保存 `[]Message`。`Run(ctx, input)` 的主循环为：

1. 构建包含历史消息和 tool schema 的 `Request`。
2. 调用 `provider.Stream` 并实时输出 text delta。
3. 收集完整 tool call；若没有 tool call，则本回合结束。
4. 执行 built-in 或 plugin tool，把结果加入会话后继续，直到完成或达到安全边界。

`ctx` 贯穿调用链，Ctrl-C 可以取消进行中的请求。`Agent` 与 `Coordinator` 都实现 `Runner`，因此 CLI 不需要区分单模型或双模型执行。

### 3.5 双模型协作（`Coordinator`）

当 `agent.planner_model` 与 executor 不同时，planner 与 executor 使用独立 session：

- planner 低频运行，只暴露经筛选的只读研究工具，生成简洁计划；
- executor 在另一 session 中使用完整工具执行计划；
- 两条会话互不混合，prompt prefix 都只追加增长，避免切换模型破坏 prefix cache。

### 3.6 上下文管理

Reasonix 通过低频 compaction 保持 cache-first：

- 低于 `agent.tool_result_snip_ratio` 时不改写历史；
- 达到 snip ratio 后，归档并缩短较旧 tool result；
- 达到 `agent.compact_ratio` 后，先把旧 tool result 修剪为占位符，仍超阈值才调用摘要；
- 达到 `agent.compact_force_ratio` 后，可执行强制折叠；
- `context_window = 0` 会关闭该实例的 compaction。

tool result 的 snip/prune 不删除消息，确保 assistant `tool_calls` 与 tool result 配对。摘要只折叠 assistant/tool 工作；正常大小的用户回合和既有 digest 原样保留。被移除的原文归档到 `reasonix/archive/<timestamp>.jsonl`。

`history` tool 支持对 session 与归档进行 BM25 搜索；`memory` tool 用于检索自动记忆，`remember` 与 `forget` 负责写入和归档。智能体发起的记忆写操作每次都需要人工确认，不能由 YOLO、自动审查或子智能体代为批准。详细约定见 `SESSION_MEMORY_RETRIEVAL.md`。

### 3.7 权限

权限层按单次 tool call 返回 `Allow`、`Ask` 或 `Deny`：

```go
type Decision int
const (Allow Decision = iota; Ask; Deny)

type Policy struct { Mode Decision; Allow, Ask, Deny []Rule }
func (p Policy) Decide(toolName string, readOnly bool, args json.RawMessage) Decision
```

- rule 可以是 `Tool` 或 `Tool(specifier)`，例如 `Bash(go test:*)`、`Edit(docs/**)`。
- 优先级为 `deny > ask > allow > fallback`；只读工具 fallback 为 Allow，写工具 fallback 使用 `Mode`。
- 交互模式中的 Ask 由用户选择单次允许、session scope 允许、持久允许或拒绝；显式 Deny 在所有模式下都不可绕过。
- 安装 MCP server 即授权其全部工具，不再有 server、raw tool、writer 或 destructive 的第二套审批策略；显式全局 `deny` 仍然优先。仓库声明的 server 在启动前只确认一次精确身份，身份变化时才重新确认。`readOnlyHint` 与 `destructiveHint` 仅用于调度、Plan/严格只读边界及缓存到实时安全分类复核，不会新增逐调用审批。
- Plan 是协作流程，不等于全工具只读。普通 built-in 与 Bash 仍走 Ask/Auto/YOLO 和 Sandbox；MCP 写入/破坏性目标及不受信任读取工具在规划阶段保持阻止。
- Plan 只能由用户显式选择进入，与当前工具审批姿态相互独立；普通聊天不会自动切换到 Plan。Auto/YOLO 不会回答 `ask`，也不会替用户批准 `exit_plan_mode`，获批计划的短期自动执行窗口也不会自动批准后续计划。
- 桌面端协作模式分为 `normal`、`plan` 和 `goal`。Goal 会持续推进目标，直到完成、同一阻塞状态重复三次、用户停止或达到安全续跑边界。只有用户在输入框中选择 Goal 或运行 `/goal` 显式启动后，长周期研究、调试、优化或实现目标才可启用 AutoResearch；普通聊天不会隐式切换协作模式，也不会创建持久化 AutoResearch 状态。动态状态保存在 `.reasonix/autoresearch/.../`。

### 3.8 Slash command

Slash command 分为三类：

- built-in action：`/compact`、`/new`、`/clear`、`/effort`、`/mcp`、`/help`；
- `.reasonix/commands/*.md` 与用户配置目录中的自定义命令；
- MCP prompt：`/mcp__<server>__<prompt>`。

自定义命令支持简单 frontmatter、`$ARGUMENTS`、`$1…$N` 和 `$$`。加载失败的单个命令会被跳过，不应使应用整体退出。

Bubble Tea TUI 的 modal overlay 必须隐藏 composer；slash/`@` autocomplete 等 input-owned overlay 保留 composer。新增 overlay 时必须更新 `chat_tui.hideComposer()` 与 layout test。

### 3.9 `@` 引用

- `@<server>:<uri>` 读取 MCP resource；
- `@<path>` 仅在本地路径真实存在时读取文件或目录，普通 `@mention` 与邮箱保持原文本；
- 文件内容有大小限制，binary 只标记不展开；目录按深度优先列出并跳过 `.git`、`node_modules` 等噪音；
- 解析异步进行，失败显示 notice 但不阻止本回合；
- autocomplete 每次只读取一层目录，避免在大型目录中递归遍历。

### 3.10 子智能体 Profile

子智能体 Profile 是带 `runAs: subagent` 的 Skill。桌面端和 CLI 只允许修改简单、手动调用的 project/global profile；包含 `references/`、`scripts/` 或非托管 frontmatter 的丰富 Skill 不会被编辑器扁平化覆盖。

`reasonix subagent try` 使用只读 Skill runner；`reasonix subagent run` 使用常规权限与 Sandbox。`task` 支持 `profile`、`model`、`effort` 和 `write_paths`；`fleet` 在 session scheduler 上并发调度多个任务。详见[子智能体 Profile](./SUBAGENT_PROFILES.zh-CN.md)。

## 4. 数据类型

provider 层的核心类型包括 `Role`、`Message`、`ToolCall`、`ToolSchema`、`Request` 和 streaming `Chunk`。`Message` 保留 `tool_calls`、`tool_call_id` 与 `name`；`Chunk` 区分 text、tool call、done 和 error。字段定义以英文规范及 `internal/provider` 源码为准。

## 5. 配置

配置优先级：

```text
flag > ./reasonix.toml > 用户 config.toml > 内置默认值
```

从 v1.8.1 起，用户配置位于 macOS/Linux 的 `~/.reasonix/config.toml` 或 Windows 的 `%AppData%\reasonix\config.toml`。provider key 保存在 Reasonix home 的 `.env`；项目 `.env` 只用于 workspace 范围的非 provider 变量展开。完整路径见[配置路径](./CONFIG_PATHS.zh-CN.md)。

```toml
default_model = "deepseek"

[agent]
temperature = 0.0
reasoning_language = "auto"

[[providers]]
name           = "deepseek"
kind           = "openai"
base_url       = "https://api.deepseek.com"
models         = ["deepseek-v4-flash", "deepseek-v4-pro"]
default        = "deepseek-v4-flash"
api_key_env    = "DEEPSEEK_API_KEY"
context_window = 1000000

[tools]
enabled = []
bash_timeout_seconds = 120
mcp_call_timeout_seconds = 300

[permissions]
mode  = "ask"
deny  = ["Bash(rm -rf*)", "Bash(git push*)"]
allow = ["Bash(go test:*)", "Bash(git status:*)"]

[sandbox]
# workspace_root = ""
# allow_write = ["/tmp"]
# forbid_read = ["${HOME}/.ssh"]

[serve]
auth_mode = "none"
```

`[sandbox]` 是权限策略之下的强制执行层。file writer 默认限制在 workspace root、Reasonix 用户配置目录和 `allow_write`；`forbid_read` 可阻止读取敏感路径。macOS 使用 Seatbelt，Linux 使用 bubblewrap；若声明 enforce 但平台 backend 不可用，Bash 应拒绝执行而不是静默降级。Windows 当前没有 OS 级 Bash sandbox，file tool 的路径限制仍然生效。

`[serve]` 控制 `reasonix serve` 的 browser frontend。默认 `auth_mode = "none"` 仅适合 loopback；暴露到其他机器时必须使用 token 或 password。只有位于可信 reverse proxy 后方时才能启用 `behind_proxy`。

项目根目录的 `.mcp.json` 可使用 Claude Code 的 `mcpServers` schema；与 `reasonix.toml` 同名时，以后者为准。

## 6. 错误处理

- library code 使用 `fmt.Errorf("...: %w", err)` 包装并返回错误，不打印也不调用 `os.Exit`；
- 只有 `cli` / `main` 决定 exit code 和面向用户的信息；
- tool error 返回给模型，不直接终止 agent loop；
- network layer 应对 429 / 5xx 使用有界指数退避。

## 7. 代码风格

- `gofmt`、`go vet` 必须通过；
- package name 使用小写，exported identifier 必须有文档；
- 注释解释“为什么”，而不只是复述“做了什么”；
- 避免过早抽象，优先清晰直接的实现。

## 8. 分发

- 构建：`CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" -o reasonix ./cmd/reasonix`
- 目标矩阵：`darwin|linux|windows × amd64|arm64`
- 版本通过 ldflags 注入，来源为 `git describe --tags --always`
- 支持预编译二进制、`go install` 与 Homebrew。

## 9. 路线图（当前范围之外）

- 完成 Sandbox Phase 1 的 escape prompt：检测 sandbox 不可用或拒绝时，提供一次明确、受权限控制的非 sandbox 重试。
- MCP long tail：OAuth 2.0、`headersHelper`、更多 `.mcp.json` scope、tool-search 延迟加载、`list_changed`、channel、elicitation、root，以及可提供 provider 的插件。
- 增加 Anthropic-native provider kind，用于验证 registry 不依赖单一 wire format，并支持原生 prompt cache control。
- 把“始终允许”规则持久化到项目配置，以及为 `reasonix run` 提供 session 级权限覆盖。

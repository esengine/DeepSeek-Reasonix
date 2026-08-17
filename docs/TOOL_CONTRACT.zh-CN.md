# 工具合约

<a href="./TOOL_CONTRACT.md">English</a>

本文记录 Reasonix 编译期内置工具的 provider-visible 合约。运行时 registry 使用同一条 canonical schema 路径；测试会校验这里列出的工具名、read-only 标记和 schema 快照不会漂移。

| 工具 | Read-only | 说明 |
| --- | --- | --- |
| `bash` | false | 执行 shell 命令并返回 stdout/stderr。构建、测试、git、包管理器等使用它；读写查找文件优先使用专用工具。 |
| `bash_output` | true | 读取后台 `bash` 或 `task` job 自上次读取后的新增输出和状态。 |
| `code_index` | true | 轻量内置代码符号索引；优先使用 `lsp_*` 或代码图 MCP，缺失时用它兜底。 |
| `complete_step` | true | 用证据记录已批准计划中一个步骤的完成情况。 |
| `compress` | true | 压缩当前模型可见对话中选定的范围，不删除可见历史。仅在用户明确要求压缩上下文时使用；锚点必须是某条真实用户消息中唯一、精确的原文片段。 |
| `delete_range` | false | 用精确 start/end 文本锚点删除文件中的连续范围。 |
| `delete_symbol` | false | 用 Go AST 删除 Go 源文件中的命名符号。 |
| `edit_file` | false | 将文件中的唯一精确字符串替换为另一个字符串。 |
| `glob` | true | 查找匹配 glob pattern 的文件。 |
| `grep` | true | 在文件或目录下按正则搜索文本。 |
| `kill_shell` | false | 终止后台 `bash` 或 `task` job。 |
| `ls` | true | 列出目录条目，可递归。 |
| `move_file` | false | 移动或重命名文件。 |
| `multi_edit` | false | 对单个文件原子应用多个编辑。 |
| `notebook_edit` | false | 编辑 Jupyter notebook 的单个 cell。 |
| `read_file` | true | 按可分页的行号格式读取文本文件。 |
| `todo_write` | true | 记录并替换当前工作的结构化任务列表。 |
| `wait` | true | 等待后台 job 完成并返回最终输出。 |
| `web_fetch` | true | 通过 HTTP/HTTPS 获取 URL 文本内容。 |
| `write_file` | false | 写入文件内容，必要时创建父目录。 |

## Schema 快照

完整 canonical schema 不在文档中手写，避免文档和代码手工漂移。运行：

```bash
go test ./internal/tool -run TestBuiltinToolContractDocumentation
```

该测试会用 `tool.BuiltinContractEntries` 校验每个内置工具都有文档行、read-only 标记、非空 description 和 canonical JSON schema。

## 默认 Full Boot Surface

默认 full-token boot 会发送上面的内置工具，并额外发送 session、memory、skill、subagent、LSP、install 和 slash-command 工具：

每个会话都使用这套 Executor 工具面，并额外提供稳定代理 `use_capability`
（list/inspect/call/decline），用于在不改变 provider 可见 Schema 的前提下发现和调用按需
MCP（含 `auto_start=false`）。宿主根据真实工具动作建立验证义务：后续相关写入会使旧的
验证、复查和签收重新变为未满足；Goal 项和已批准 Plan 的验收项为 Strict；`complete_step`
必须引用最后一次相关写入之后的证据。Skill/MCP 的 require/prefer 路由受门禁约束（只读回答
同样不能跳过 require 能力）；触及认证、Schema 或破坏性路径后，结构化 review 的
`reviewed_paths` 必须有宿主观测到的 read/diff 证据。

## 统一 Boot 工具面

每个会话都使用同一套 provider 可见核心工具和同一个 `use_capability` 代理。

双模型 Planner 与全部 task/fleet 子 Agent 同样使用 `use_capability`（且从不暴露直接
`mcp__*` schema）。Planner 与普通可写子 Agent 可调用已安装或项目配置 MCP，不要求
`readOnlyHint`；Planner 将 `destructiveHint` 留给 Executor，普通子 Agent 走可信 MCP 路径
（实时授权复核 + 仅显式 deny）。writer/destructive 调用仍会串行并按 mutation 记录，继续受
证据、工作区租约和闭环门禁约束。严格只读子 Agent 共享同一代理 schema 与 Host 连接，但执行仍要求 `readOnlyHint` 且
非 destructive。双模型会给 Planner 与 Executor 分别挂载独立代理 frontend，确保规划阶段
发现的 capability 在 handoff 后仍可直接调用；两者 ledger/audit 隔离，但共享 Host 连接。
单模型会话不启用独立 Planner。

`use_capability` 的解析阶段无副作用：`action=list` 返回已配置 MCP 服务器的排序列表且不启动服务器；
对未连接服务器的 `action=call` 只生成惰性目标；Plan 只会对真实目标重新检查显式阶段 opt-out，服务器进程只在
权限门禁与 PreToolUse Hook 放行之后才启动。按需启动的
子进程随会话存活（不会随单次调用结束而退出）；`action=inspect` 对已连接服务器列出实时工具，未连接
时只读取缓存 schema，绝不启动进程。无 schema 缓存的服务器首次发现走 `mcp-server:` id 的
`action=call`：解析为受门禁保护的连接目标（权限名为独立的
`mcp_connect__<server>`；例如精确拒绝规则 `deny = ["mcp_connect__github"]`
会在进程启动前拦截），放行后连接并返回实时工具目录。MCP 工具名规则仍为精确匹配，
`mcp__github__*` 不是工具名通配规则。安装 MCP 即授权 Planner 使用其非 destructive 工具；
第三方若错误省略 `destructiveHint`，远程副作用属于用户安装信任范围。每次 connect 或
`tools/call` 前，frontend 都会再次复核当前 runtime 的 enable、授权与精确 Host 连接身份；另一个
项目/tab 在共享 Host 上的同名 client 会在进程、网络或工具分发前被拒绝。

固定代理的 provider 可见 name、description、schema 与顺序不会随 MCP inventory 变化。

`ask`, `docs`, `explore`, `fleet`, `forget`, `history`, `install_skill`, `install_source`,
`list_sessions`, `lsp_definition`, `lsp_diagnostics`, `lsp_hover`,
`lsp_references`, `memory`, `parallel_tasks`, `read_only_skill`,
`read_only_task`, `read_session`, `read_skill`, `read_subagent_result`, `remember`, `research`,
`review`, `run_skill`, `security_review`, `slash_command`, `task`.

`parallel_tasks` 与 `fleet` 会为每个已持久化子 Agent 返回公平分配的预览和稳定的
`Subagent reference`，使合并结果始终低于单工具输出上限。`read_subagent_result`
按 UTF-8 字节偏移分页读取某个引用对应的完整最终答案，因此长篇并行调研无需一次性全部
注入父会话也不会丢失。引用只允许在当前会话 lineage 和工作区内读取。

`use_capability`（`action` = `list` | `inspect` | `call` | `decline`）在 provider
可见工具面上始终存在（没有按任务复杂度切换的工具档位）。可选工具仍在 host
registry 中供调度，但不会展开到 top-level provider schema；模型通过 `use_capability`
调用，避免缓存前缀因 schema 变化而失效。

`internal/boot.TestBootToolContractMatchesProviderVisibleSurface` 会校验真实 boot registry 合约和 provider request 一致，包括 read-only 标记和 canonical schema。

## 统一启动工具面（所有任务）

每个任务共享同一套精简的 provider 可见核心：直接编码工具、后台 shell 生命周期工具，
以及稳定的能力代理：

`bash`, `bash_output`, `edit_file`, `kill_shell`, `read_file`, `wait`,
`write_file`, `compress`（若注册），以及 `use_capability`。

可选工具（`glob`、`grep`、`ls`、`web_fetch`、MCP、skills、subagents、docs、会话历史、
记忆写入、workflow 等）仍在 host registry 中可调度；模型通过 `use_capability` 列举、
检查、调用或拒绝它们，且不会改变 provider 工具列表。改变的是宿主根据真实动作建立的
验证义务，而不是 provider 可见工具集合。已退役的 `connect_tool_source` 不再注册。

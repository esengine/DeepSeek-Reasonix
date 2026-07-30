# 工具合约

<a href="./TOOL_CONTRACT.md">English</a>

本文记录 Reasonix 编译期内置工具的 provider-visible 合约。运行时 registry 使用同一条 canonical schema 路径；测试会校验这里列出的工具名、read-only 标记和 schema 快照不会漂移。

| 工具 | Read-only | 说明 |
| --- | --- | --- |
| `bash` | false | 执行 shell 命令并返回 stdout/stderr。构建、测试、git、包管理器等使用它；读写查找文件优先使用专用工具。 |
| `bash_output` | true | 读取后台 `bash` 或 `task` job 自上次读取后的新增输出和状态。 |
| `code_index` | true | 轻量内置代码符号索引；优先使用 `lsp_*` 或代码图 MCP，缺失时用它兜底。 |
| `complete_step` | true | 用证据记录已批准计划中一个步骤的完成情况。 |
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

单模型均衡（Balanced）Executor 使用这套工具面。配置独立 Planner 的 Balanced 与全部交付优先
（Delivery）会在保留既有工具的同时增加一个稳定代理 `use_capability`（list/inspect/call/decline），
用于在不改变 provider 可见 Schema 的前提下发现和调用按需 MCP（含 `auto_start=false`）。Delivery
还会增加稳定执行合约，并由宿主运行时强制执行：变更和
验证命令必须先建立验收标准；变更后的工作必须完成复查、验证并通过带证据的 `complete_step` 签收；
Skill/MCP 的 require/prefer 路由受门禁约束（只读回答同样不能跳过 require 能力）；中/高风险改动
强制结构化 review/security_review，且 `review_report` 的 `reviewed_paths` 必须有宿主观测到的
read/diff 证据。

双模型 Planner 与全部 task/fleet 子 Agent 同样使用 `use_capability`（且从不暴露直接
`mcp__*` schema）。Planner 与普通可写子 Agent 可调用已安装或项目配置 MCP，不要求
`readOnlyHint`；Planner 将 `destructiveHint` 留给 Executor，普通子 Agent 走可信 MCP 路径
（实时授权复核 + 仅显式 deny）。writer/destructive 调用仍会串行并按 mutation 记录，继续受
证据、工作区租约和 Delivery 门禁约束。严格只读子 Agent 共享同一代理 schema 与 Host 连接，但执行仍要求 `readOnlyHint` 且
非 destructive。Balanced 双模型会给 Planner 与 Executor 分别挂载独立代理 frontend，确保规划阶段
发现的 capability 在 handoff 后仍可直接调用；两者 ledger/audit 隔离，但共享 Host 连接。Economy
仍为单模型，不启用独立 Planner。

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

固定代理的 provider 可见 name、description、schema 与顺序不会随 MCP inventory 变化；但 Balanced
Executor 刻意保留直接 `mcp__*` 工具，因此安装、连接或刷新这些直接工具时，Executor 的整体 provider
前缀仍可能变化。

`ask`, `explore`, `fleet`, `forget`, `history`, `install_skill`, `install_source`,
`list_sessions`, `lsp_definition`, `lsp_diagnostics`, `lsp_hover`,
`lsp_references`, `memory`, `parallel_tasks`, `read_only_skill`,
`read_only_task`, `read_session`, `read_skill`, `remember`, `research`,
`review`, `run_skill`, `security_review`, `slash_command`, `task`.

`use_capability`（`action` = `list` | `inspect` | `call` | `decline`）：Delivery Executor，
以及 Balanced 双模型会话中的 Planner 和 Executor；Economy 不启用。

`internal/boot.TestBootToolContractMatchesProviderVisibleSurface` 会校验真实 boot registry 合约和 provider request 一致，包括 read-only 标记和 canonical schema。

## Token Economy Boot Surface

token economy 模式只带 9 个初始工具：4 个直接编码工具、3 个后台 shell 生命周期工具、
`ask`，以及按需启用来源的 connector：

`ask`, `bash`, `bash_output`, `connect_tool_source`, `edit_file`, `kill_shell`,
`read_file`, `wait`, `write_file`。

其余能力都显式按需加载。`connect_tool_source` 支持 `search`（`code_index`、
`glob`、`grep`、`ls`）、`files`（专用移动、多编辑、删除与 notebook 工具）、
`workflow`（`todo_write`、`complete_step`）、`sessions`（`history`、
`list_sessions`、`read_session`）、`memory`（`memory`、`remember`、`forget`）、
`commands`（`slash_command`）、`skills`、`read_only_skill`、`mcp`、`lsp`、
`web_fetch`、`install_source`、`task` 和 `read_only_task`。所有来源都可在 Plan 中连接；后续 reader
与 writer 调用和常规模式一样进入 Permissions/Sandbox。`workflow` 是阶段性例外：规划期间只安装
`todo_write`，`complete_step` 需在计划批准后重新连接 `workflow` 才会加入。
需要专用 `search` 来源之前，使用 `bash` 完成目录查看与搜索。

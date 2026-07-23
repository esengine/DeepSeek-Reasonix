# 设计：Checkpoints 与 Rewind

<a href="./CHECKPOINTS.md">English</a>

状态：**Phase 1 + 2 已实现**——包括快照存储、统一捕获切入点、Esc-Esc / `/rewind` CLI 选择器、桌面端悬停 rewind，以及完整的 Claude Code 风格菜单：恢复代码、恢复会话、同时恢复、从此处分叉、从此处开始摘要或摘要到此处。当前实现基于快照并与 Claude Code 对齐；可选的 git-backed 模式是优先级较低的后续工作。这补上了 v1 用户最常请求的编辑安全网 / 撤销能力。

本文说明 rewind 快照机制。关于自主运行期间智能体何时应暂停并询问用户，参见[任务合约与暂停策略](./TASK_CONTRACT.zh-CN.md)。

## 目标

让用户把会话回退到之前的节点，并恢复**代码**、**会话**或**两者**，且不改动 git 历史。CLI 与桌面端采用同一套机制，并与 Claude Code 的 Esc-Esc / `/rewind` 行为对齐。

## 机制：文件快照，而不是 git

与 Claude Code（以及 v1 的 `checkpoints.ts`）相同，checkpoint 是独立于 git 的**文件快照**：

- **不污染 git**：不提交、不暂存，也不修改 `.git/`；在非 git 目录中同样可用。
- **只跟踪可预览的编辑工具变更**：包括 `write_file`、`edit_file` 和 `multi_edit`。`move_file` 遵循同一工作区权限边界，但移动操作尚不会出现在 checkpoint 预览中。
- **不跟踪 `bash` 副作用**：系统无法判断 shell 命令改动了哪些内容，这一点与 Claude Code 相同；高风险 Bash 操作由权限层负责拦截。
- 编辑前保存完整文件内容。实现简单，存储量通过下文的保留策略限制。

可选的 **git-backed 模式**（v1 的 `auto-git-rollback`）适合需要 git 级安全保障的用户，但不在当前范围内。

## 锚点与捕获

- **每个用户回合创建一个 checkpoint。** 回合开始时（`Controller.Send` / `runTurn`）创建，并以用户提示作为标签。
- **编辑前快照。** 在 `agent.(*Agent).executeOne` 中，执行非只读且实现 `tool.Previewer` 的工具前，调用 `Preview(args)` 获得 `diff.Change{Path, Kind, OldText}`，再把文件快照记录到当前 checkpoint。文件写工具已经实现 `tool.Previewer`，因此只需这一处统一切入点，不必逐个修改工具。
  - 同一回合内按路径去重：只保存**第一次**触碰前的内容，即该文件在回合开始时的状态。
  - `Kind == create` 表示文件原本不存在，保存 `Content = nil`，恢复时删除该文件；`modify` / `delete` 保存 `OldText`。
  - `bash` 没有实现 `Previewer`，因此自然排除，符合“只跟踪编辑工具”的约定。

## 数据模型

```go
type FileSnap struct {
    Path    string  // workspace-relative
    Content *string // nil → file did not exist at the anchor (restore deletes it)
}

type Checkpoint struct {
    Turn   int        // user-message index this anchors (0-based)
    Time   time.Time
    Prompt string     // user message text — the picker label
    Files  []FileSnap // distinct files touched during this turn, turn-start state
}
```

## 存储

- **作为会话 sidecar 保存**：位于 `config.SessionDir()` 下的 `<session-id>.ckpt/`，每个 checkpoint 一个 JSON 文件并附带小型索引。这样删除成本低，单个快照损坏也只影响自身。它与消息 JSONL（`agent.Session.Save`）分开，因此无需改动会话格式。
- **跨进程保留**：恢复会话时会重新加载 checkpoint，重启后仍可 rewind，与 Claude Code 保持一致。
- **保留策略**：随会话清理，默认约 30 天并可配置，以限制完整内容快照占用的磁盘空间。

## Controller API：两个前端共用的统一入口

Checkpoint 位于 `control.Controller`，与 `SetPlanMode`、`Compact`、`NewSession` 并列。终端 TUI、桌面 WebView 和 HTTP/SSE server 都通过同一入口触发 rewind，不在各自前端重复实现逻辑。

```go
type RewindScope int // Code | Conversation | Both

func (c *Controller) Checkpoints() []CheckpointMeta      // for the picker
func (c *Controller) Rewind(turn int, scope RewindScope) error
```

- **Code**：遍历从 `turn` 到最新的所有 checkpoint，按路径选取最早的 `FileSnap`，把文件恢复到对应内容；若为 `nil` 则删除。也就是撤销 `turn` 及之后的全部编辑。恢复前会再次按当前工作区根目录检查路径逃逸。
- **Conversation**：把 `Session.Messages` 截断到 `turn` 对应用户消息之前，重新 `Save`，并发送替换后的历史事件供前端重绘。所选回合的提示会回填到输入框，方便修改后重发。
- **Both**：同时恢复代码与会话。

统一的 `Rewound` 事件（或复用 history-replace 事件）让所有前端以相同方式重绘。

## CLI 体验（与 Claude Code 对齐）

- 输入框为空时按两次 **`Esc`**，或执行 **`/rewind`**，打开用户回合列表，显示时间和每个回合改动的文件。`chat_tui` 已跟踪双 Esc 的时间窗口。
- 选择一个回合后显示子菜单：**`[code+conversation] [conversation] [code] [cancel]`**。
- 恢复 conversation 或 both 时，把所选提示回填到输入框。

## 桌面端体验（与 VS Code 扩展对齐）

- Transcript 中每条用户消息悬停时显示 **rewind** 控件，并提供：恢复代码、恢复会话、同时恢复、从此处分叉。
- 前端通过 Wails binding 调用同一个 `controller.Rewind`；Controller 事件流推送恢复结果，React 负责重绘。前端不包含独立 rewind 逻辑。

## 非目标与边界情况

- **Bash / 外部副作用**：`rm`、`mv`、数据库写入、部署等不会被跟踪，也无法通过 rewind 撤销，这与 Claude Code 一致。
- **回合之间的外部编辑**：快照保存的是回合开始时的内容，因此恢复会覆盖期间由 Reasonix 之外产生的改动。
- **删除**：编辑工具执行的删除可以恢复，因为快照保存了原内容；`bash rm` 无法恢复。
- **大文件**：当前保存完整快照，由保留策略控制磁盘占用；若成为问题，再引入按内容寻址的去重。

## 阶段划分

1. **Phase 1**：快照存储、`executeOne` 捕获切入点、`Controller.Rewind`（code / conversation / both）、CLI 选择器（Esc-Esc + `/rewind`）。
2. **Phase 2**：桌面端悬停 rewind、“从此处分叉”、“从此处开始摘要 / 摘要到此处”，以及可选的 git-backed 模式。

## 待确认问题

- 是否在 `/compact` 和 `NewSession` 边界创建快照？
- 默认保留窗口应为多久，是否需要在 `[checkpoints]` 配置中公开？
- 是否从一开始就使用内容寻址去重，而不是每个快照一个文件？

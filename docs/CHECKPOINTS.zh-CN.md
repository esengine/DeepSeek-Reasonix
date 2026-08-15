# 设计：Checkpoints 与 Rewind

<a href="./CHECKPOINTS.md">English</a>

状态：**Phase 1 + 2 已实现**——包括快照存储、统一捕获切入点、Esc-Esc / `/rewind` CLI 选择器、桌面端悬停 rewind，以及完整的 Claude Code 风格菜单：恢复代码、恢复会话、同时恢复、从此处分叉、从此处开始摘要或摘要到此处。当前实现基于快照并与 Claude Code 对齐；可选的 git-backed 模式是优先级较低的后续工作。这补上了 v1 用户最常请求的编辑安全网 / 撤销能力。

本文说明 rewind 快照机制。关于自主运行期间智能体何时应暂停并询问用户，参见[任务合约与暂停策略](./TASK_CONTRACT.zh-CN.md)。

## 目标

让用户把会话回退到之前的节点，并恢复**代码**、**会话**或**两者**，且不改动 git 历史。对话回溯改为显式分叉，父会话永不截断。详见 [会话所有权](./SESSION_OWNERSHIP.zh-CN.md)。CLI 与桌面端采用同一套机制。

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

- **作为会话 sidecar 保存**：位于 `config.SessionDir()` 下的 `<session-id>.ckpt/`。它与消息 JSONL（`agent.Session.Save`）分开，因此无需改动会话格式。
- **跨进程保留**：恢复会话时会重新加载 checkpoint，重启后仍可 rewind，与 Claude Code 保持一致。
- **Schema v3 布局**：每轮一个目录，包含 `turns/<turn>/meta.json` 和原始字节
  `files/NNNN.before`。新捕获不再把同一份 pre-image 重复写入内容寻址 blob。
  升级后仍可读取 v1/v2 JSON 和 blob；事务 / undo 载荷仍可使用 blob。每个 v3
  回合还会写入一个不含文件载荷的 v2 兼容标记（`turn-<turn>.json`）。降级到旧版
  Reasonix 后，旧版可以据此保持 turn 编号单调递增，但不能恢复该标记对应的 v3
  文件快照。该 marker 同时是 v3 回合的存活标记：旧版截断 marker 后，后续升级会
  忽略遗留目录，而不会把未来回合重新加载出来。
- **保留策略**：默认保留最近 100 个 v3 回合目录，并对原始 v3 pre-image 使用
  1 GiB 软上限。当前回合或事务保护中的回合可以暂时超过上限；解除保护后，从最旧
  的完整回合目录开始清理。旧版 blob 在独立的兼容存储中使用相同的上限值。删除
  会话时会清理整个 checkpoint sidecar。

## Controller API：两个前端共用的统一入口

Checkpoint 位于 `control.Controller`，与 `SetPlanMode`、`Compact`、`NewSession` 并列。终端 TUI、桌面 WebView 和 HTTP/SSE server 都通过同一入口触发 rewind，不在各自前端重复实现逻辑。

```go
type RewindScope int // Code | Conversation | Both

func (c *Controller) Checkpoints() []CheckpointMeta
func (c *Controller) PrepareRewind(turn int, scope RewindScope) (RewindPlan, error)
func (c *Controller) CommitRewind(planID string) (RewindResult, error)
```

- **Code**：遍历从 `turn` 到最新的所有 checkpoint，按路径选取最早的 `FileSnap`，把文件恢复到对应内容；若为 `nil` 则删除。也就是撤销 `turn` 及之后的全部编辑。恢复前会再次按当前工作区根目录检查路径逃逸。
- **Conversation**：在该回合边界创建新会话分支。父会话 transcript 永不截断。详见 [会话所有权](./SESSION_OWNERSHIP.zh-CN.md)。
- **Both**：先创建新会话分支，再恢复代码；如果文件校验冲突，保留分支并返回
  `partial=true`。

统一的 `Rewound` 事件（或复用 history-replace 事件）让所有前端以相同方式重绘。

## CLI 体验（与 Claude Code 对齐）

- 输入框为空时按两次 **`Esc`**，或执行 **`/rewind`**，打开用户回合列表，显示时间和每个回合改动的文件。`chat_tui` 已跟踪双 Esc 的时间窗口。
- 选择一个回合后显示子菜单：**`[code+conversation] [conversation] [code] [cancel]`**。
- 恢复 conversation 或 both 时，把所选提示回填到输入框。

## 桌面端体验（与 VS Code 扩展对齐）

- Transcript 中每条用户消息悬停时显示 **rewind** 控件，并提供：恢复代码、恢复会话、同时恢复、从此处分叉。
- 前端通过 Wails binding 调用同一个 prepare / commit rewind API；Controller 事件流推送恢复结果，React 负责重绘。前端不包含独立 rewind 逻辑。

## 非目标与边界情况

- **Bash / 外部副作用**：`rm`、`mv`、数据库写入、部署等不会被跟踪，也无法通过 rewind 撤销，这与 Claude Code 一致。
- **回合之间的外部编辑**：恢复前会比较当前文件的存在性、SHA-256 和 mode 与
  Reasonix 最后一次 after-image；不匹配时报告冲突，不会覆盖。
- **删除**：编辑工具执行的删除可以恢复，因为快照保存了原内容；`bash rm` 无法恢复。
- **大文件**：保存完整快照，但单文件捕获上限为 32 MiB。回合数和软字节上限共同
  限制历史占用；当前回合或事务保护中的回合可以暂时超过字节上限。

## 阶段划分

1. **Phase 1**：快照存储、`executeOne` 捕获切入点、Controller prepare / commit（code / conversation / both）、CLI 选择器（Esc-Esc + `/rewind`）。
2. **Phase 2**：桌面端悬停 rewind、“从此处分叉”、“从此处开始摘要 / 摘要到此处”，以及可选的 git-backed 模式。

## 待确认问题

- 是否在 `/compact` 和 `NewSession` 边界创建快照？
- 是否需要在 `[checkpoints]` 配置中公开 100 回合与 1 GiB 软字节上限？

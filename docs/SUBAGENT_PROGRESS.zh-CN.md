# 本地子 Agent 进度展示

状态：**已实现** —— 桌面端与 CLI 为本地子 Agent 运行（`task`、`read_only_task`、`parallel_tasks`、`fleet`）提供逐子任务的进度预览，构建在已持久化的子 transcript 与 `read_subagent_result` 之上（持久化模型见 [`CHECKPOINTS.md`](CHECKPOINTS.zh-CN.md)）。

## 目标

子 Agent 工作时，用户应能看到**它正在做什么**，且子 Agent 的 reasoning/正文不进入父对话：进度卡片显示子任务的阶段、运行耗时与最近活动；桌面卡片可展开查看受限的 reasoning / 回答 / notice 预览；CLI 在 `/verbose` 模式下显示同样的预览。全部零配置——不新增任何设置项。

## 线上合同

进度预览复用现有 `ToolProgress` 事件，使用四个保留的 `Tool.Name` 值。这些名称是 agent 进度 tracker 与本地前端之间的内部合同；绝不能作为 provider 可见的工具名出现：

| 名称 | 载荷 |
|---|---|
| `reasonix.subagent.status` | 恰好为 `queued`、`running`、`reasoning`、`responding`、`tool`、`retrying`、`completed`、`failed`、`cancelled` 之一 |
| `reasonix.subagent.reasoning` | 受限的 UTF-8 文本增量（子任务的思考） |
| `reasonix.subagent.text` | 受限的 UTF-8 文本增量（子任务的回答预览） |
| `reasonix.subagent.notice` | 受限的 UTF-8 文本增量（子任务的提示） |

字段约定：

- `Tool.ID` —— 子任务卡片 ID（进度查找以 ID 为准，绝不依赖正文）。
- `Tool.Output` —— 阶段值（status）或文本增量（预览）。
- `Tool.Truncated` —— 本轮预览发生截断或合并时为 `true`。
- `Tool.DurationMs` —— 最终耗时，随 terminal 状态事件携带。
- `Tool.ParentID` —— 沿用现有嵌套关系（顶层 `task` 为空；`parallel_tasks`/`fleet` 子任务为组调用 ID）。

## 行为

状态机（由统一执行链 `RunProfileSpec` 发出，`task`、`read_only_task`、`parallel_tasks`、`fleet` 共用，不在各入口复制）：

- 前台运行以 `running` 开始。
- 后台任务在注册成功后发出 `queued`，真正获得执行槽时发出 `running`。
- `parallel_tasks`/`fleet` 组卡片拥有自己的显式生命周期：children 开始时分发 `running`，所有 children 落定后发出唯一 terminal（`completed`；取消/deadline 为 `cancelled`；任一 child 失败或调用出错——包括验证失败——为 `failed`）。前端绝不根据"当前已观察到的 children"推断组完成，因为后台 children 是异步分发的，快的首个子任务可能在后续子任务出现前就已完成。
- 子任务的 `Reasoning`/`Text`/`Notice`/`Retrying` 事件转换为对应预览频道；子任务真实工具活动把阶段更新为 `tool`，嵌套工具卡片渲染不变。
- 每次运行恰好发出**一个** terminal 状态：成功为 `completed`，context 取消或 deadline 为 `cancelled`，provider/工具/存储/panic 错误为 `failed`。terminal 前同步 flush 待发送预览；terminal 后的迟到事件被忽略。

限流与内存边界（按父任务组）：

- 每个 (子任务, 频道) 只保留一个待发送槽；预览最多合并 250ms 后发出一条事件，增量不会无界累积。
- 每组每秒最多 32 条非终态事件——阶段变化与内容预览共享同一预算，按子任务轮转，避免高活跃子任务饿死其他任务。仅初始 `queued`/`running` 状态与 terminal 事件不受限。
- 预算裁剪丢弃缓冲内容时，丢失会以 `Truncated` 标记传播到下一条实际发出的频道（或在 terminal flush 时以截断 notice 呈现），前端总能得知部分预览被丢弃。
- 每个子任务未发送缓冲总计上限 8 KiB（优先丢弃 notice，其次 reasoning，最后 text）；超出后保留 UTF-8 安全的尾部并设置 `Truncated`。桌面端按频道保留（reasoning/text 各 8 KiB、notice 2 KiB）；CLI 为 `/verbose` 保留 4 KiB reasoning/text 尾部。

明确不做：

- 子任务的 `Message`、reasoning 与正文绝不进入父 transcript 或 provider 上下文。
- 不新增事件 kind、不新增线上字段、不改 provider 工具列表/工具 schema/system prompt、不新增配置。
- 预览不持久化：重启后完整子 transcript（与 `read_subagent_result`）仍是事实来源。
- ACP 与 bot 消费者继续整体忽略 `ToolProgress` 正文。

## 桌面端

- 子 Agent 工具卡片的头部显示阶段徽标（阶段 + 运行耗时 + “N 秒前”最近活动）；子任务存活期间每秒跳动一次，结束后定格为阶段 + 时长摘要。
- 展开卡片显示独立的 reasoning / 回答预览 / notice——绝不与普通工具输出混排。
- 后台调用即使已返回 job id，只要子进度仍为非终态，卡片仍保持运行状态；`parallel_tasks`/`fleet` 组卡片只由其自身生命周期 terminal 事件定格——job-id result 先于任何子任务到达、或快的首个子任务先于后续子任务完成，都不会让组卡片提前定格。
- `completed`/`failed`/`cancelled` 分别沿用现有 done/error/stopped 视觉语义；terminal 后默认折叠，用户手动展开的选择在状态变化后保留。

## CLI

- 每个子任务维护独立进度状态与固定 transcript 槽位（按调用 ID 键控），独立于单一 live 工具流——并发子任务绝不串流。
- 默认只显示阶段、耗时与最近活动；reasoning/正文在 `/verbose`（Ctrl+O）模式下显示，受限为最近 4 KiB 尾部。
- terminal 后默认折叠为一行摘要；verbose 保留受限预览。
- 无法原地重绘的终端（Termux native scrollback）仅在阶段变化与 terminal 时输出状态行；verbose 预览每子任务每 2 秒最多输出一次。

## 合同稳定性

前端按 `reasonix.subagent.` 前缀匹配保留名称，因此较新 agent 新增的频道会被较旧前端忽略（绝不追加进普通工具输出）。

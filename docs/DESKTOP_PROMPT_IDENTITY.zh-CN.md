# Desktop 交互卡片身份

Desktop 决策卡片由创建它的 controller 所有。每个新的交互请求都携带
prompt ID、所属 turn ID，以及 tab 可见的 runtime epoch。`kind` 标识交互类型：
`ask`、`approval`、`plan`、`recovery` 或 `mcp`。

前端通过 `ResolvePromptForTab` 提交这些字段。controller 在 exact resolve
边界内校验 runtime epoch、active turn、prompt owner 和 pending 状态，然后
持久化 `PromptAnswered` 并唤醒原始等待方。旧 turn 或旧 runtime 的提交会被
拒绝，不会被路由到替换后的 controller。持久化失败时 prompt 会恢复为
pending，用户可以重试。

prompt 请求和生命周期事件会暴露 `promptId`、`promptKind` 和 `turnId`。
Desktop 事件 envelope 会携带 tab 的 runtime epoch。缺少 turn identity 的旧
事件会标记为 `promptLegacy`，只能通过兼容路径处理。

`AnswerQuestionForTab`、`ApproveTab` 和 `ResolveRecoveryTab` 等旧 Wails 方法
仍为旧客户端保留。新前端统一使用 `ResolvePromptForTab`，不会静默降级到
没有 fence 的方法。收到 stale 响应后，旧卡片会从当前决策面移除，并按 tab
请求一次 prompt replay；只有新的 pending identity 才能重新显示卡片。

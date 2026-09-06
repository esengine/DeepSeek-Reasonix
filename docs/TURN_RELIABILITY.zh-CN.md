# 回合可靠性与中断恢复

对应 #9825、#9805、#9683 以及 #9566 的重放部分：已经发生的副作用不能重复执行，
没有回传给模型的结果不能被伪造，每个回合都必须以持久化的终态结束。

方案在现有基础上增量扩展：`turns.jsonl` 生命周期账本、会话事件、
`InterruptedTurnRecovery` 和 `executeBatch` 执行框架，不新建平行持久化系统。

## 工具调用状态

每个工具调用在账本和存储的 tool 消息里各有且仅有一个 `ToolRunState`，
两者由同一个分类器产出，不可能出现分歧。

| 情况 | 状态 | 自动重试 |
|---|---|---|
| 在起始屏障前被取消或被拒绝 | `cancelled` / `not_started` | 允许 |
| 工具明确返回成功 | `completed` | 不重试 |
| 工具明确返回失败 | `failed` | 仅只读或幂等工具 |
| 已启动但结果未确认（取消、崩溃、写入未确认） | `unknown` | 禁止 |
| unknown 但后置条件检查证明副作用已存在 | `completed` + 已满足写入 | 不重试 |

`ToolStarted` 事件就是屏障。越过屏障却没有确认结果的调用一律是 `unknown`，
即使随后整个 batch 被取消。`not_started` 只为旧记录保留，新读取端视同 `cancelled`。

## 执行规则

- 只读调用并行；写入、`bash`、代理工具按 provider 顺序单个串行。
- 一个写入失败、被拒绝或变成 `unknown` 后，batch 内后续写入被跳过并附带依赖说明；
  只读诊断仍可执行。
- 取消时按起始屏障分类剩余调用：已启动的变成 `unknown`，其余为 `cancelled`。
- 被中断调用的参数只保存在本地 `ToolCallRecord`，永不进入模型可见的恢复块。

## 恢复交接

下一次真实用户回合携带有界的 `<interrupted-turn-recovery>` 块，列出
`completed_tools`、`failed_tools`、`cancelled_tools`、`not_started_tools`、
`outcome_unknown_tools`、写入后置条件，以及部分助手输出是否被排除。
块里只有名称、ID 和效果摘要。

对 `unknown` 副作用调用的完全相同重发会被拒绝，直到其效果被检查过；
已满足的写入后置条件直接短路为"已完成"。失败调用保留配对的错误结果，
不会被列为 unknown。

控制器把账本回合 ID 盖在交接记录上；回合被二次中断时合并先前的交接。

## 回合终态

每个回合以 `completed`、`failed`、`interrupted` 或 `recovery_required` 结束。
取消时若存在未证明的副作用，或回合在产生任何内容前就终止（`silentInterruption`），
终态升级为 `recovery_required`。Desktop 从 `TurnDone` 事件读取。

## Provider 转录门

请求冻结前，用适配器实际发送的同一规范化视图做校验：无法解码的参数、
缺失或错序的结果、孤儿结果都会在上线前被拒绝。规范化本身已经修复截断参数
和悬空调用；这道门把规范化回归变成本地错误，而不是 provider 400。

## 状态

已落地（Phase 1，状态正确性）：

- 显式运行状态、本地参数回执与起始屏障
- 账本事件与存储消息共用一个分类器
- `unknown` 触发 batch 依赖屏障
- 恢复交接新增 `failed_tools`；回合 ID 盖章
- `recovery_required` 终态升级
- 每次 provider 请求前的转录门

后续：

- Phase 2：账本 schema v3 携带工具调用记录、首个副作用调用前的 checkpoint、
  重启后恢复 pending/unknown
- Phase 3：Desktop 恢复卡片（检查、标记已完成、重新执行；新 attempt ID，
  原 call ID 永不复用）、静默中断提示、普通工具中断不再自动 fork 会话
- Phase 4：`git commit` 与 `bash` 的后置条件检查、checkpoint GC、恢复指标

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Agent-root token budget / Agent 根任务 token 预算

An explicit `task_token_budget` now counts the main-loop reported prompt and
completion tokens of an Agent-root delegation tree. Cached input still counts.
The default threshold remains zero. This is a cooperative landing gate after
tool rounds, followed by the existing finalization round.

显式 `task_token_budget` 现在累计一个 Agent 根委派树的主循环已报告输入及输出
token，缓存命中输入同样计入。默认阈值仍为零。这是在工具轮次结束后触发的协作式
收尾门禁，仍保留既有的最后收尾轮次。

## Identity and lifetime / 身份与寿命

The root Agent owns a process-local atomic counter in its existing task ledger.
Derived contexts share that counter; a child cannot enlarge an inherited root
limit through its local override. A root continuation keeps the counter. A new
root task, root session replacement or explicit root reset starts another counter.

根 Agent 在既有任务账本内持有进程内原子计数器。派生 context 共享计数器；子任务
不能通过局部 override 放宽继承的根阈值。继续同一根任务会保留计数器；新根任务、
替换根会话或显式重置根预算则使用另一个计数器。

Task and fleet background jobs project the opaque root scope through
`agent.InheritRootTokenBudget`. It carries token bounds and explicit
[wall bounds](ROOT_WALL_BUDGET_SECURITY.md), while Job Manager cancellation and
deadlines remain authoritative.

A parent turn ending does not erase the handed-off scope. An old job still
charges its old counter after the parent starts a different task.

task 和 fleet 后台 job 通过 `agent.InheritRootTokenBudget` 传递不透明根 scope，
包括 token 与显式[耗时门禁](ROOT_WALL_BUDGET_SECURITY.md)，取消与截止时间仍由
Job Manager 掌握。父 turn 结束不会抹掉已交出的 scope。父任务
换成另一项工作后，旧 job 仍记入旧计数器，不会扣减新任务的预算。

## Authority and limits / 权限与边界

The context key is private. The internal Go projection helper copies an existing
scope; it does not construct a new allowance or copy parent cancellation. Known
usage is added once per observed Agent round. Negative usage cannot credit the
counter and unsigned overflow saturates instead of reopening a budget.

context key 是私有类型。内部 Go 投影函数复制既有 scope，不创建新额度，也不复制
父任务的取消权。每个被观察到的 Agent 轮次只记账一次。负用量不能返还额度，
无符号整数溢出会饱和，不能重新打开预算。

This is not request admission, token reservation, a hard spending ceiling or
process termination. Already-running requests and each Agent's finalization
round can exceed the threshold. Queued work is not rejected before its first
request by this gate. A missing usage report is unknown, not proof of free work.

该机制不是请求准入、token 预留、硬费用上限或进程终止机制。正在执行的请求以及
各 Agent 的收尾轮次可能越过阈值。该门禁不会在排队工作发出首个请求前拒绝它。
缺失用量报告代表未知，不能据此认为工作免费。

Auxiliary provider calls outside `observeRunBudget`, remote processes, durable
restart accounting and a shared planner/executor Coordinator root are not
covered. Existing local cost/time gates and per-Agent budget telemetry retain
their meanings. No currency amounts are mixed; unpriced models still count tokens.

`observeRunBudget` 之外的辅助模型调用、远程进程、持久化重启记账，以及跨 planner
与 executor 的统一 Coordinator 根预算均不在本次范围内。既有费用、时间门禁及
每 Agent 预算遥测保持原含义；不混加不同币种金额，无定价模型仍累计 token。

## Compatibility and validation / 兼容与验证

There is no persisted format, wire, tool schema, permission or collaboration-mode
change. Existing positive token limits become shared across this Agent subtree;
zero still disables the root token gate. Provider-visible system/tool prefixes
receive no budget metadata. Pauses use the existing host-owned token pause.

不修改持久化格式、wire、工具 schema、权限或协作模式。既有正 token 阈值现在由
这个 Agent 子树共享，零仍关闭根 token 门禁。系统与工具缓存前缀不写入预算元数据，
暂停继续使用既有 host-owned token pause。

Regressions exercise actual parent/child provider calls, nested and concurrent
children, continuations and replacement-task isolation. Real boot covers direct
and capability-routed foreground/background tasks plus a background fleet, with
actual file reads and a parent cancellation before delayed background work.

回归覆盖实际父子 provider 调用、嵌套与并发子任务、继续运行和替换任务隔离。
真实 boot 覆盖直接及 capability 路由的前后台 task 和后台 fleet，验证实际文件
读取，并让延迟后台工作在父 turn 取消后继续执行。

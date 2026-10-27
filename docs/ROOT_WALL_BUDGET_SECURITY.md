---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Agent-root wall budget / Agent 根任务耗时预算

An explicit host `agent.TaskBudget.Wall` now follows the Agent-root delegation
scope. The existing local time gate remains; a child also checks elapsed time
from the root task's start after its tool round. Defaults remain unlimited.

host 显式传入的 `agent.TaskBudget.Wall` 现在跟随 Agent 根委派 scope。既有局部
时间门禁保留；子任务在工具轮结束后，也会检查从根任务起点累计的耗时。默认仍不限。

## Clock and authority / 时钟与权限

The opaque scope carries an immutable start/limit pair alongside the existing
token counter. The start comes from the root's existing task/turn clock.
Derived contexts share the pair; child overrides cannot relax an inherited
root limit. Local narrower limits still apply first.

不透明 scope 在既有 token counter 旁携带不可变的起点与阈值，起点来自根任务
既有 task/turn 时钟。派生 context 共享它；子任务 override 不能放宽继承的根阈值，
更严格的局部门禁仍优先适用。

The existing `InheritRootTokenBudget` API name is retained. Task and fleet job
producers already copy the opaque scope, so they carry the wall bound too.
They retain Job Manager cancellation and deadlines; a parent turn ending or
being canceled does not cancel those contexts.

保留既有 `InheritRootTokenBudget` API 名称。task 与 fleet 的 job producer 已复制
不透明 scope，因此也携带耗时门禁。取消与截止时间仍由 Job Manager 掌握；父 turn
结束或被取消不会因此取消 job context。

## Lifetime / 寿命

A root continuation preserves the task start. New tasks or explicit budget
reset start a new clock. Old jobs retain their old immutable pair. A new root
turn snapshots its current explicit limit; it does not rewrite limits already
handed to older jobs.

继续同一根任务会保留 task 起点；新任务或显式重置预算启用新时钟。旧 job 保留
原来的不可变起点与阈值。新根 turn 捕获当前显式阈值，不会改写此前已交给旧 job
的阈值。

Elapsed time includes queueing and gaps between continuations while the same
ledger remains active. It is not CPU time. The scope is process-local and is
not a durable deadline across restart. An unknown start cannot prove expiry.

同一账本存续时，耗时包含排队及 continuation 间隔，不是 CPU 时间。scope 仅在
进程内有效，不是跨重启的持久截止时间。未知起点不能证明已经超时。

## Landing and compatibility / 收尾与兼容

Expiry uses the existing host-owned time pause and finalization round.
It does not cancel requests, install a timer, reject queued work before launch
or terminate processes. Each child can execute an in-flight round and its
existing finalization before landing.

超时沿用既有 host-owned time pause 与收尾轮，不取消请求、不安装 timer、不在
启动前拒绝排队任务，也不终止进程。各子任务仍可能完成进行中的轮次及既有收尾轮。

No new configuration field, public API, persisted shape, wire, tool schema or
permission is added. This uses existing host Options/context budgets; it does
not add a user-facing wall-budget setting or change provider-visible prefixes.
Local cost telemetry and token accounting remain unchanged.

没有新增配置字段、公共 API、持久化形状、wire、工具 schema 或权限。使用既有
host Options/context 预算，不新增面向用户的耗时设置，也不改写供应商可见前缀。
局部费用遥测及 token 计账保持不变。

## Evidence boundary / 验证边界

Agent regressions exercise foreground/nested/concurrent calls and disabled
limits. Clock lifetime tests select the existing host continuation flag.
Real boot runs task, capability task and fleet background jobs after the parent
has returned and been canceled, with actual fixture reads.

Agent 回归覆盖前台、嵌套、并发调用及禁用阈值。时钟寿命测试选择既有 host
continuation 标记。真实 boot 让后台 task、capability task、fleet 在父 turn
返回并取消后继续执行，并验证真实夹具读取。

This does not establish physical process quiescence, root currency aggregation,
auxiliary provider admission, a shared Coordinator root, durable recovery or the
complete concurrency/isolation plan. Author analysis is not independent review.

该机制不证明物理进程静止、根币种汇总、辅助 provider 准入、共享 Coordinator 根预算、
持久恢复或完整并发隔离方案。作者分析不等于独立审查。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Planner local task budgets / Planner 局部任务预算

The tool-enabled planner now receives the same existing configured TaskBudget
conversion as the executor. Previously its Agent Options omitted that field, so
ordinary explicit planning ignored configured token, cost and elapsed-time
limits unless the host supplied a context override.

启用工具的 planner 现在使用与 executor 相同的既有 TaskBudget 配置转换。此前
其 Agent Options 漏传该字段，普通显式规划会忽略配置的 token、费用和耗时阈值，
除非 host 通过 context 提供 override。

## Behavior and authority / 行为与权限

The existing `agent.task_token_budget`, `agent.task_cost_budget` and
`agent.task_time_budget_minutes` settings apply to the planner's own Agent run.
Zero remains unlimited. An explicit context budget still takes precedence,
including an explicit zero override. Unpriced or missing usage cannot prove a
cost crossing.

Existing reported-usage and known-cost rules remain unchanged.

既有 `agent.task_token_budget`、`agent.task_cost_budget`、
`agent.task_time_budget_minutes` 配置作用于 planner 自己的 Agent 执行。零值仍不限。
显式 context 预算仍优先，包括显式零 override。缺少价格或用量不能证明费用跨线；
既有 reported usage 与已知费用规则保持不变。

These are cooperative limits checked after a tool round, with the existing
finalization round. A normal planning pause follows the existing Coordinator
fallback to the executor.

It does not stop the whole task, share one monetary ledger across roles, reserve
before a request, cancel a provider request or terminate a process. Concurrent
work and finalization can exceed a threshold.

这些是工具轮结束后检查的协作式阈值，并保留既有收尾轮。普通规划暂停沿用
Coordinator 回退到 executor 的行为，不会停止整个任务、跨角色共用费用账本、
请求前预留、取消供应商请求或终止进程。并发工作与收尾仍可能超过阈值。

No new config, public API, persisted format, wire, tool permission or default is
added. The boot repair does not change prompt prefixes or tool serialization.
Existing Agent root token/wall scopes keep their existing context boundaries;
this change does not create a shared Coordinator root.

没有新增配置、公共 API、持久化格式、wire、工具权限或默认值。启动接线修复不
改写前缀或工具序列化。既有 Agent 根 token/耗时 scope 继续保留原 context 边界；
本改动不建立共享 Coordinator 根预算。

## Evidence boundary / 验证边界

Real boot regression fixtures use separate planner/executor models behind a
local OpenAI-compatible HTTP/SSE server and verify actual planner file reads.
Configured token, cost and wall limits land after one read and a finalization
request.

Disabled, unknown-cost and context-override controls pin the adjacent behavior,
including the executor-only route and ordinary executor fallback.

真实 boot 回归使用本地 OpenAI-compatible HTTP/SSE 服务中的独立 planner/executor
模型，并验证 planner 实际读文件。token、费用、耗时配置使其在一次读取和收尾
请求后停下；禁用、费用未知与 context override 控制覆盖相邻行为，包括仅 executor
路由及普通回退。

Serialized system messages and tool schemas stay identical within each fixture
run. This is a request-surface guard, not a measured provider cache hit rate.
The wall fixture establishes a lower bound beyond its configured threshold;
it does not set a machine-speed requirement.

各夹具执行内的序列化 system message 与工具 schema 保持一致。这是请求表面守卫，
不是供应商缓存命中率实测。耗时夹具建立超过显式阈值的下界，不要求机器处理速度。

Shared Coordinator budgets, auxiliary provider admission, currency aggregation,
reservations, durable accounting and physical cancellation remain separate work.

See [root token budgets](ROOT_TOKEN_BUDGET_SECURITY.md),
[root wall budgets](ROOT_WALL_BUDGET_SECURITY.md) and
[usage completeness](BUDGET_USAGE_COMPLETENESS.md).

共享 Coordinator 预算、辅助 provider 准入、币种汇总、预留、持久计账和物理取消
仍需独立完成。作者分析与本机测试不等于独立 review 或原生平台执行。

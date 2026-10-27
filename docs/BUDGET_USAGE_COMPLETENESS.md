---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Budget usage completeness / 预算用量完整性

Shared quote/ledger provenance is covered in
[Estimated usage and quote completeness](ESTIMATED_USAGE_QUOTE_SECURITY.md).
共享报价及账本来源的后续边界见该双语说明。

`RunBudgetTotals.Priced` requires reported usage and a price table for every
observed round. A missing record or an existing `Usage.Estimated` record keeps
the turn and continuing task incomplete. `Cost` retains only their known priced
subtotal; unknown spend is not zero spend.

`RunBudgetTotals.Priced` 要求每个已观察轮次都有供应商用量及价格表。缺失记录或
已有的 `Usage.Estimated` 记录会让 turn 和继续执行的 task 保持成本不完整状态。
`Cost` 仅保留已知且已定价的部分；未知费用不等于零费用。

## Producer and observer / 生产者与观察者

The shared HTTP-attempt helper preserves the exact request count when the
provider returns no usage, and marks that synthesized record with the existing
`Estimated` flag. It does not invent token counts or change a provider-reported
zero record. Existing estimated records remain estimated; inputs are copied.

共享 HTTP attempt helper 在供应商没有返回用量时保留准确的请求次数，并通过已有
`Estimated` 标记说明补出的记录缺少供应商计量。它不编造 token 数，也不改写供应商
明确报告的零用量。原有估算记录仍是估算；输入记录通过复制保留。

The Agent budget observer counts available requests and tokens as before, but
excludes estimated or unpriced costs from its known subtotal. Missing rounds
remain visible in `Rounds`. An aggregate marked estimated is excluded as a
whole; this layer cannot recover its exact partial charges.

A wholly unknown cost cannot trip the money gate.
An already-known subtotal reaching the configured limit still triggers the
existing soft landing even if other rounds are unknown.

Agent 预算观察者仍统计可用的请求次数及 token，但不会把估算或未定价费用加入已知
金额。缺失轮次仍计入 `Rounds`。标记为估算的聚合记录整体排除；这一层无法恢复其中
准确的部分费用。

完全未知的费用不会触发金额门禁；已知金额达到
显式阈值时，即使另有未知轮次，也仍会触发既有的软收尾。

## Lifetime and compatibility / 寿命与兼容

The incomplete marker belongs to the existing turn/task budget. A continuation
keeps task uncertainty while its own fully reported turn can be priced. A new
task or explicit task-budget reset drops the old task reading. This does not
repair historical totals or infer whether an unreported request was billed.

完整性标记归既有 turn/task 预算所有。继续执行会保留 task 的未知状态，但新 turn
若报告完整，仍可单独标记为已定价。新任务或显式重置预算会丢弃旧 task 读数。
这不会追溯修正历史统计，也不会推断未报告用量的请求是否产生了账单。

No field, serialized shape, configuration, tool schema or permission changes.
Existing readers already accept `Estimated` and `Priced`; old records missing
those flags retain their old zero values. Request-only records now say they
are incomplete, and estimated costs no longer certify the Agent money gate.

没有新增字段、序列化形状、配置、工具 schema 或权限。既有读取方已支持
`Estimated` 和 `Priced`；历史记录缺少这些标记时仍使用旧零值。仅含请求次数的
新记录现在明确其不完整性，估算金额也不再成为 Agent 金额门禁的确定依据。

## Evidence and limits / 验证与边界

Real `boot.Build` tests use the OpenAI HTTP adapter, one actual fixture read and
two SSE responses, omitting usage from either response or neither. Agent tests
cover explicit zero usage, known-cost landing, unknown estimates and task lifetime.

The continuation fixture selects the existing host continuation flag;
it is not a frontend interaction test.

真实 `boot.Build` 测试经过 OpenAI HTTP adapter，完成一次夹具读取及两次 SSE
响应，分别省略第一轮、末轮或不省略用量。Agent 测试覆盖明确零用量、已知费用
收尾、未知估算及任务寿命。continuation 夹具选择既有 host continuation 标记，
不冒充前端交互测试。

This is honest local accounting, not a root money reservation or hard cap.
In-flight work and the existing finalization round can exceed a soft limit.

Root currency aggregation, auxiliary-call budget admission, durable accounting
and physical process cancellation remain separate work. Provider-visible prompt
prefixes and default limits are unchanged.

This author assessment is not an independent review.

这是本地计账完整性修复，不是根任务金额预留或硬上限。进行中的工作及既有收尾轮
可能超过软阈值。根任务货币汇总、辅助调用预算准入、持久计账及物理进程取消仍属
后续工作。供应商可见的提示前缀及默认阈值不变。作者评估不等于独立审查。

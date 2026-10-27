---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Estimated usage and quote completeness / 估算用量与报价完整性

## Contract / 约定

Reconstructed token counts support an estimated amount, but cannot certify a
complete cost fact. `BuildQuote` reads existing `UsageTokens.Estimated` and uses
the existing incomplete-usage result. Fixed-point original amounts and official
occurrence-time valuations remain available as estimates.

重建 token 数可以计算估算金额，但不能证明完整成本。共享报价入口读取既有
`UsageTokens.Estimated`，复用不完整用量结果。原币种定点金额及官方时段估值仍
保留为估算值，不因此成为供应商实际账单。

`CostQuote.Estimated` has a different meaning: rate-card quotes already carry
that flag even with reported usage. Reported-usage controls retain complete
coverage; this repair does not mark every rate-card quote incomplete.

`CostQuote.Estimated` 表示使用价目估算，语义不同；已报告完整用量时原本也为真。
本修复不把所有价目报价改为不完整。

## Producer and consumers / 来源与消费

An interrupted stream without terminal usage reaches
`agent.estimateFailedAttemptUsage`. It reconstructs positive counts, sets
`provider.Usage.Estimated`, and combines them with recovered attempts. The event
quote adapter and `config.QuoteForUsage` already forward this identity.

中断流缺少最终用量时，Agent 重建正数计数并标记估算。恢复尝试合并后仍携带
该身份。事件及配置报价入口已传递该字段；缺口在共享报价判断。

Main turns, guardian assessments and configured auxiliary quoting reach the
same owner. `CostComplete`, `DisplayComplete` and `Complete` stay false, coverage
stays incomplete, and the reason is existing `missing_price_or_usage`.
No new reason, enum or wire field is added.

Existing selected-cost aliases in wire/ACP and recorded rows remain absent for
an incomplete selection. Their original amounts remain in the quote/row. The
reported-usage controls retain those optional aliases. This uses the existing
unavailable-display path, without a wire version or permission change.

wire、ACP 与统计行的既有选定金额别名在不完整选择时保持缺省，原金额仍在报价
或行内。准确用量对照保留这些可选别名；复用已有不可用显示路径，不改版本或权限。

主循环、guardian 及配置辅助报价共用同一入口。三个完整性字段保持假，覆盖为
不完整，原因复用已有值。没有新增原因码、枚举或序列化字段。

Ledger folding combines reported and reconstructed calls into partial coverage.
Later reported calls or display selection cannot restore full ledger coverage.
Original sums still include estimated amounts: they are not known-charge
subtotals and must not be used as hard reservations.

账本混合准确与重建用量后保持部分覆盖，后来准确调用或切换显示币种不会恢复
完整覆盖。原汇总仍含估算值，不是已知扣费小计，也不能用作硬预算预留。

## Authority and compatibility / 权限与兼容

The flag is trusted accounting provenance, not authentication. This grants no
filesystem, tool, process or model permission and changes no prompt, request,
retry policy or cache prefix.

该标记是可信计账来源，不是认证证明。不扩展任何权限，也不改变提示词、请求、
重试策略或缓存前缀。

| Surface | Existing data | New writes | Previous reader |
| --- | --- | --- | --- |
| CostQuote fields/version | Decodes unchanged | Same fields and existing reason | Existing incomplete flags/coverage remain readable |
| Ledger/version1 | Existing totals retained | Same amounts, partial/incomplete coverage | Existing coverage fold retains uncertainty |
| Old complete quotes | Remain historical | No rewrite or migration | Broad CostQuote.Estimated is not reinterpreted |
| Usage identity | Existing Estimated flag | Producer unchanged | No schema change |

旧数据及账本版本不变。历史错误完整报价不追溯修正：旧报价的广义 Estimated
不能证明当时 token 是否重建，不得据此批量改写历史。

## Evidence and limits / 验证与边界

Tests cover official currencies, amounts, normalization, display selection,
same/separate ledger keys, JSON reopen and later exact calls. Event/config tests
cover main, guardian and auxiliary quoting. A compiled preceding pricing owner
distinguishes regressions from controls.

单元回归覆盖官方币种、金额、规范化、显示选择、账本键、JSON 重开与后续准确
调用。事件及配置入口另有回归；编译的前版报价入口用于验证区分能力。

Real `boot.Build` uses fixture HTTP/SSE: a stream ends after partial output and
without terminal usage, recovery performs an actual fixture read, then a
reported final turn completes. The sink observes reconstructed usage and partial
ledger coverage. Ordinary reported streams remain complete.

真实组装使用临时 HTTP/SSE 服务：部分输出后缺少最终用量即结束，恢复后实际
读取夹具，最后准确轮完成。前端 sink 观察到估算身份与部分账本覆盖，普通准确
流保持完整。这是可重复夹具，不是线上事故证据。

This repairs a monetary-fact prerequisite of #14025/#14133. Shared root-money
admission, currencies, auxiliary reservations, durable accounting and physical
quiescence remain separate. Author evidence is distinct from independent review
and exact-head hosted runs.

此项仅修复完整方案的金额事实前提。根金额预算、跨币种、辅助预留、持久计账及
物理进程终止仍独立待做，作者证据也不等于独立 review 或托管精确 head 验证。

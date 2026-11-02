---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-28
---

# Local task cost quotes / 本地任务费用报价

## Behavior / 行为

An Agent's existing task cost gate consumes the same occurrence-time quote
sent to accounting sinks. Vendor schedules, configured billing identity and
the original price-book currency come from that quote's owner. Delivery reuses
the quote; it does not read the clock or price the request a second time.

Agent 的既有任务费用门禁消费与记账事件相同的发生时刻报价。供应商费率时段、
配置的计费身份和原价目表币种由报价 owner 决定。事件交付复用该报价，不再次
读取时钟或重新定价。

At the repository's existing peak-rate fixture, a CNY 30 local limit now stops
after the first CNY 36 tool round, then performs the existing finalization
request. Previously the gate counted CNY 18 and allowed a second tool round.
The off-peak and custom-relay controls keep their own rates.

在仓库已有高峰费率 fixture 中，30 CNY 的本地限额会在首个 36 CNY 工具轮后
停止，再执行既有收尾请求。旧门禁按 18 CNY 累计，因此会允许第二个工具轮。
离峰与自定义 relay 对照仍使用各自费率。

## Accounting boundary / 记账边界

| Surface | Behavior / 行为 |
| --- | --- |
| Accumulation | Fixed-point `pricing.Amount` per original currency / 按原币种用固定点金额累计 |
| Display-currency preference | Does not select the spend bucket / 不选择支出累计桶 |
| Model changes currency | Keep separate buckets; compare the current quoted currency / 保留分币种桶，比较当前报价币种 |
| Mixed-currency scalar telemetry | Current bucket only, completeness false / 仅当前桶，完整性为 false |
| Missing or estimated usage | Preserve known subtotal and incomplete coverage / 保留已知小计与不完整状态 |
| Explicit reported zero | Retain the existing known-zero local reading / 保留既有已报告零值语义 |
| Nil or legacy sink | Quote only its supplied rate card; no catalog guess / 只使用提供的价目表，不猜测官方目录 |
| New task, reset and session lifetime | Existing budget lifecycle / 沿用既有预算生命周期 |

`UsageQuoter` is an optional internal Go sink capability forwarded by the
standard wrappers. `CostQuoteSink` owns it. The normal Agent loop consumes a
snapshot of the quote before downstream sinks can modify the emitted value.

The public cost sample remains a float compatibility projection of fixed-point
money; arithmetic no longer sums legacy floating-point costs.

`UsageQuoter` 是标准包装器转发的可选内部 Go sink 能力，由 `CostQuoteSink`
拥有。普通 Agent 循环在下游 sink 可修改事件之前保留报价快照。公开 cost sample
仍是固定点金额的浮点兼容投影；累计运算不再累加旧浮点费用。

No configuration, persisted schema, wire field, provider request, cache prefix,
permission or cancellation authority is added. Already-recorded totals are not
repaired. Custom wrappers must forward the optional quoting capability to retain
their authoritative source; legacy fallback does not infer vendor identity.

不新增配置、持久化 schema、wire 字段、provider 请求、缓存前缀、权限或取消权限。
不追溯修复已记录统计。自定义包装器必须转发可选报价能力，才能保留权威来源；
旧 sink 的回退不推断供应商身份。

This is a local after-tool soft gate. In-flight work and finalization can exceed
the limit. Shared root money, auxiliary cost aggregation, currency reservation,
durable billing and process quiescence remain separate unfinished work.

这是本地工具轮后的软门禁；在途工作与收尾仍可越过限额。共享根费用、辅助调用
费用汇总、按币种预留、持久化账单和进程静默仍是独立的未完成工作。

## Verification / 验证

Real Agent and Boot assembly tests use scripted providers, actual fixture reads,
the production catalog-host mapping and a fixed quote clock. They cover peak,
off-peak and relay behavior. Unit tests cover quote reuse, wrapper forwarding,
legacy/nil sinks, original currency buckets and fixed-point thresholds.

真实 Agent 与 Boot 装配测试使用脚本 provider、实际 fixture 读取、生产目录宿主
映射与固定报价时钟，覆盖高峰、离峰和 relay 行为。单元测试覆盖报价复用、包装器
转发、旧/nil sink、原币种桶及固定点阈值。这不是线上账单或真实供应商请求验证。

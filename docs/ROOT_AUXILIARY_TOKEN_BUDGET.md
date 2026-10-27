---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Auxiliary usage in root token budgets / 根 token 预算中的辅助用量

Summary and failure-diagnosis requests made inside an active Agent root context
now charge that context's existing token counter. Their usage events and
compaction receipts were already recorded; the root counter omitted them.

在活跃 Agent 根 context 内执行的摘要与失败诊断请求，现在计入该 context 既有
token counter。此前用量事件与压缩收据已有记录，但根计数漏掉了这些调用。

## Accounting boundary / 计账边界

Each auxiliary terminal folds its usage once, beside the existing usage-event
owner. Positive prompt and completion counts use the same saturating counter as
normal rounds, including positive estimates under that existing rule. Missing
counts remain unknown; request counts do not imply token counts.

每个辅助调用终态在既有用量事件 owner 处仅计一次。正 prompt/completion 计数使用
与正常轮次相同的饱和 counter，包括该既有规则下的正估算值。缺失计数仍未知，
请求次数不能推导为 token 数。

Usage received before a summary or diagnosis fails still counts, even when its
answer is discarded. Provider retries retain their existing aggregate-usage
semantics. No extra charge is generated from a compaction receipt or ledger
replay. Normal-round local telemetry remains separate and unchanged.

摘要或诊断失败前已收到的用量仍计入，即使答案被舍弃。供应商重试保留既有聚合
用量语义，不通过压缩收据或账本重放再次扣计。正常轮次的局部遥测保持独立且不变。

The scope comes from the request context, never from the Agent's current task
field. A budget-only projected job context retains its original scope after a
new task starts. Manual maintenance without a root context still reports its
usage but does not create or charge an unrelated root task.

scope 来自请求 context，不读取 Agent 当前 task 字段。仅投射预算的 job context
在新任务开始后仍保留原 scope。没有根 context 的手动维护仍上报用量，但不会
创建或扣计无关的根任务。

## Cooperative limits and compatibility / 协作式阈值与兼容

The explicit token gate remains cooperative after a tool round, with the
existing finalization. An auxiliary request can cross the threshold before a
normal request or tool runs; this change does not reserve or reject in advance.
Zero limits still leave execution unbounded.

显式 token 门禁仍在工具轮后协作式检查，并保留既有收尾。辅助请求可能在正常
请求或工具执行前已经越线；本改动不做提前预留或拒绝。零阈值仍不限执行。

No public API, configuration, persisted format, wire, permission or default is
added. Compaction timing, request construction, schemas and prefix reuse are
unchanged. Cost completeness and currency ledgers retain their existing rules.

没有新增公共 API、配置、持久化格式、wire、权限或默认值。压缩时机、请求构造、
schema 和前缀复用保持不变。费用完整性与币种账本沿用既有规则。

## Evidence boundary / 验证边界

Agent regressions exercise automatic pressure compaction, manual maintenance
and protected-failure diagnosis inside a real root Run. They cover billed
failure, missing usage, disabled limits and original-scope projection after a
new task. The late-scope fixture is constructed, not a production job incident.

Agent 回归通过真实根 Run 覆盖自动压力压缩、手动维护与受保护失败诊断，并检查
有用量的失败、缺失用量、禁用阈值及新任务后的原 scope 投射。延后 scope 夹具是
构造控制，不是生产 job 事故复现。

Real boot HTTP/SSE fixtures start with existing public transcript history,
trigger automatic compaction and verify actual file reads and usage provenance.
Existing cache-prefix regressions remain required; their mock results do not
measure a live provider's cache hit rate.

真实 boot HTTP/SSE 夹具通过既有公共 transcript 历史触发自动压缩，验证实际文件
读取和用量来源。既有缓存前缀回归仍须通过；mock 结果不是供应商命中率实测。

This is not complete auxiliary-provider admission, a shared Coordinator root,
currency aggregation, hard reservation, durable accounting or physical process
cancellation. See [root token budgets](ROOT_TOKEN_BUDGET_SECURITY.md).

本改动不等于完整辅助 provider 准入、共享 Coordinator 根预算、币种汇总、硬预留、
持久计账或物理进程取消。作者分析不等于独立 review 或原生平台执行。

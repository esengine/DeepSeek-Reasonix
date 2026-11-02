---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-28
---

# Reported totals and token budgets / 已报告总量与 token 预算

## Contract / 合同

A completion may report a positive total without an input/output breakdown.
The Agent's local and shared-root token budgets count the larger of that total
and the sum of nonnegative input/output counts. These overlapping measures are
never added together. Negative values contribute zero.

一次完成可能报告正的总 token 数而没有输入/输出拆分。Agent 本地与共享根 token 预算
统一取该总数和非负输入/输出之和中的较大值，不把两种重叠计数相加。负值按零计入。

Accumulation saturates instead of wrapping. A configured local role cap still
applies when the role inherits a larger root cap. Root usage belongs to the
original context: a late auxiliary request after a task reset charges its old
root, and the new root starts empty.

累计计数饱和而不回绕。角色继承更大的根额度时，已配置的本地额度仍有效。根用量属于
原上下文：任务重置后迟到的辅助请求计入旧根，新根从零开始。

No input/output split is invented. Provider usage, its existing normalization,
pricing, currencies, persisted data and wire formats are unchanged. The optional
RunBudget shadow sample continues to expose reported component counters; it does
not gain a total field. Budget pause details report the counted total.

不补造输入/输出拆分。provider usage、已有规范化、计价、币种、持久化数据和 wire 格式
不变。可选 RunBudget shadow 样本仍暴露已报告的分项计数，不新增 total 字段；预算暂停
说明报告用于门禁的总数。

## Effect and limits / 效果与限制

A real OpenAI HTTP/SSE adapter and Boot fixture issue actual read_file calls.
With a 1500-token budget and 1100 reported tokens per completion, total-only and
complete-split records both pause after two reads. The existing finalization
request still runs. This is a synthetic wire fixture, not a live vendor incident.

真实 OpenAI HTTP/SSE 解析器与 Boot 夹具执行实际 read_file。每次完成报告 1100 tokens、
预算 1500 时，仅总数和完整拆分两种记录都在两次读取后暂停，原有收尾请求仍执行。
这是合成 wire 夹具，不是线上供应商事故。

The budget remains an after-tool soft gate. In-flight requests and finalization
may exceed it; this adds no hard reservation, monetary root budget or durable
billing. Missing usage remains unknown. Zero configured limits remain unlimited.
Older binaries retain their previous handling of total-only records.

Counters keep their existing in-process lifetime. This does not add
restart-persistent budget enforcement.

预算仍是工具执行后的软门禁。进行中的请求和收尾可以越界；本次不新增硬预留、共享根
金额预算或持久化计费。缺失 usage 仍为未知，配置零额度仍不限额。旧二进制保留原有的
仅总数记录处理方式。

计数器保留原有的进程内生命周期，本次不新增跨重启的预算执行能力。

Provider request construction, tool schemas and cache-stable prefixes are
unchanged. A run may intentionally pause sooner when previously uncounted known
tokens reach its configured limit.

Windows/Linux compilation does not establish
native runtime acceptance; the Windows root-token inventory requires all new
named regressions on the PR's own head.

provider 请求构造、工具 schema 与稳定缓存前缀不变。过去漏计的已知 tokens 达到用户
额度时，运行会按预期更早暂停。Windows/Linux 编译不代表原生运行验收；Windows 根
token 清单要求在 PR 自己的 head 上通过全部新增命名回归。

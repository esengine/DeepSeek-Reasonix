---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Native search root tokens / 原生搜索根 token 计账

Native provider-backed `web_search` now reports finalized usage to the optional
active-root observer. It uses the accepted provider contract; Agent root
binding owns the scope and counter, while the search terminal owns one charge.

原生 provider-backed `web_search` 现在将终态用量上报给可选活跃根 observer。
它复用已接受的 provider 契约；Agent 根绑定拥有 scope/counter，搜索终态仅扣计一次。

## Accounting owner / 计账所有权

Observation follows the existing independent HTTP request counter and precedes
the optional usage reporter. A missing reporter does not hide known token
usage. The observer receives a copy, so it cannot alter the reporter's record.

观察发生在既有独立 HTTP 请求计数之后、可选 usage reporter 之前。没有 reporter
不隐藏已知 token；observer 收到副本，不能改写 reporter 的记录。

Known usage received before a stream error or cancellation still counts once.
Input refusal, pre-cancellation, unavailable factories and missing token usage
do not invent charges. Request-only records preserve exact HTTP counts and
their estimated identity; request counts never imply tokens.

流错误或取消前收到的已知用量仍仅计一次。输入拒绝、预先取消、factory 不可用
和缺失 token 用量不推造扣计。仅请求记录保留准确 HTTP 次数与 estimated 身份，
不能把请求次数当作 token。

Positive prompt/completion counts follow the existing conservative root rule,
including positive estimates. Normal-round local telemetry, model/source/
pricing emission and reporter behavior remain unchanged. Outside an observer
context, the tool keeps its existing behavior.

正 prompt/completion 沿用既有保守根规则，包括正估算值。正常轮次局部遥测、
model/source/pricing 上报和 reporter 行为保持不变。没有 observer 的 context
继续沿用工具原行为。

## Limits and search authority / 阈值与搜索权限

Explicit token limits may land earlier after a search, at the existing
after-tool check with finalization. Zero stays unbounded. This is not request
reservation, a monetary ledger, provider cancellation or process termination.

显式 token 阈值可能在搜索后更早触发既有工具轮后检查与收尾；零值仍不限。
本机制不是请求预留、金额账本、供应商取消或进程终止。

The ordered-backend `Chain` uses request-priced backends, not this native token
terminal. Its paid consent, fallback and request billing remain unchanged.
Search model selection, credentials, redirects, native completion, isolated
query history and external-content provenance are unchanged.

有序 backend `Chain` 使用按请求计费的后端，不经过本原生 token 终态；其付费
同意、回退与请求计费保持不变。搜索模型选择、凭据、重定向、原生完成边界、
独立查询历史和外部来源标签均保持原样。

## Evidence boundary / 证据边界

Real boot HTTP/SSE drives an OpenAI-compatible worker through the actual
capability gateway into an Anthropic-native search response. Tests inspect
the real summary, citation source, usage identity and independent request
counts, with disabled-limit and missing-usage controls.

真实 boot HTTP/SSE 将 OpenAI-compatible worker 经实际 capability gateway 接到
Anthropic 原生搜索响应。回归检查实际摘要、引用来源、用量身份与独立请求次数，
并保留禁用阈值和缺失用量对照。

The fixture simulates the provider protocol; it does not query the public web.
Serialized main prefix/schema equality is a cache guard, not a live-provider
hit-rate measurement. No API, config, persisted or provider-wire field changes.

夹具模拟供应商协议，不查询公共互联网。主序列化前缀/schema 相等是缓存守卫，
不是供应商命中率实测；不改变 API、配置、持久化或 provider-wire 字段。

This partial consumer does not complete all auxiliary admission, shared
Coordinator budgets, root currency aggregation, durable accounting or the full
isolation/recovery plan. See [bounded root usage](BOUNDED_ROOT_TOKEN_BUDGET.md).

本 consumer 不完成全部辅助准入、共享 Coordinator 预算、根币种汇总、持久计账
或完整隔离恢复方案。作者分析、独立 review 与原生平台执行分别记录。

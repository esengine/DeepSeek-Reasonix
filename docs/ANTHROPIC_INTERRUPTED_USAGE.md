---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Anthropic interrupted usage / Anthropic 中断流用量

## Behavior / 行为

Anthropic `message_start` and `message_delta` can report counters before a
response ends. Previously, errors and premature EOF bypassed the terminal
usage emitter. Bounded consumers retained only a request count, while normal
Agent recovery reconstructed counts instead of retaining the known counters.

Anthropic 可在响应完成前报告输入、输出、缓存和服务端工具计数。此前错误和提前
EOF 绕过用量出口：有界调用只保留请求数，普通 Agent 恢复则重新估算已有计数。

One response now owns one cumulative accumulator and one delivery attempt.
Explicit errors deliver known usage before `ChunkError`, because consumers may
return on that error. Repeated counters still use their maximum, not a sum.
`message_stop` emits ordinary exact usage once and stops reading immediately.

每个响应现在由同一个累积器和一次交付尝试负责。显式错误先交付已知用量，再交付
错误；重复累计值取最大值而非相加。`message_stop` 仍只交付一次正常用量并停止读取。

Partial records set the existing `Estimated` flag and `FinishReason=interrupted`.
The counts retain what the provider reported; missing output is not invented by
the decoder. The flag describes incomplete accounting, not exact billing.
Normal Agent recovery may still augment counts under its existing policy.

部分记录使用已有 `Estimated` 和 `interrupted` 标记，表示记账不完整，不能认证完整
账单。解码器保留供应商已报告的计数，不猜测缺失输出；普通 Agent 恢复仍可按既有
策略补充估算。金额消费者沿已有不完整报价路径处理，不把它当已知完整金额。

## Source, sink and authority / 来源、出口与权限

| Boundary | Contract / 约定 |
| --- | --- |
| Wire input | Existing SSE decoding and nonnegative cumulative maxima; provider counters remain external claims / 沿用解码与非负最大值，供应商计数仍为外部声明 |
| Usage sink | One record before explicit errors; bounded observer and reporter retain their existing contracts / 错误前交付一次，沿用 observer 和 reporter |
| Error identity | Classify upstream error before accounting emission so accounting alone cannot disable the before-output retry route / 先分类错误，用量交付不改变原重试资格 |
| Cancellation | Existing cancellation-aware sink; canceled or abandoned consumers may lose the best-effort record / 沿用可取消出口，消费者离开时不保证必达 |
| Money and budgets | Partial usage cannot certify a complete quote; positive counts follow existing soft-budget policies / 不认证完整报价，沿用已有软预算策略 |

Known cache counts, native cache-write units and server-tool counts survive.
Request counts still come from the existing HTTP attempt counter. Recovery sums
separate body attempts once; repeated deltas within one response are not extra
requests. Usage delivery does not commit speculative tools or a partial answer.

已知缓存、原生缓存写入计价单位和服务端工具计数均保留。请求数仍取自已有 HTTP
计数器；恢复只累加各次响应，重复 delta 不算新请求。交付用量不提交推测工具或答案。

No request construction, prefix, tool schema, retry budget, permission, config,
wire shape or default changes. The change consumes the existing uncertainty
contract; older persisted complete records are not rewritten. Other providers
and the distinction between reported usage and a price-table estimate remain.

不改变请求、前缀、工具 schema、重试预算、权限、配置、数据形状或默认值；不回写
旧持久化账单。其他供应商及供应商用量与价目估算之间的区别保持不变。

## Evidence and limits / 证据与限制

HTTP/SSE regressions cover upstream/payload errors, EOF, malformed JSON, repeated
partial counters, zero, missing and committed controls. Reader tests cover IO
cause identity, idle timeout, buffered cancellation and an abandoned consumer.
Real bounded calls retain the usage at the observer and reporter before failing.

真实 HTTP/SSE 回归覆盖上游和负载错误、EOF、JSON 错误、重复计数、零值、缺失及完成
对照；读取回归覆盖 IO 原因、空闲超时、可交付的取消和消费者离开。有界调用保留错误
前的 observer 与 reporter 用量。

Actual Boot and Anthropic HTTP recovery verify three requests, one real file
read, exact known attempt totals, unchanged replay request JSON and incomplete
quote/ledger coverage. A normal two-request control remains complete. These are
protocol fixtures, not a live-provider incident or cache-hit-rate measurement.

真实 Boot 与 Anthropic HTTP 恢复核验三次请求、一次实际文件读取、已知计数、重放
请求 JSON 不变及不完整报价和账本；两次正常请求对照仍完整。这是协议夹具，不是
线上事故复现或真实供应商缓存命中率测量。

This does not guarantee accounting after consumer abandonment, process death or
an unreported provider charge. It adds no durable ledger, hard reservation,
shared Coordinator budget or physical process completion proof. The complete
isolation/integration/recovery plan remains open under #14025.

消费者离开、进程退出或供应商未报告的费用仍无必达保证。本单元不提供持久账本、
硬预留、共享 Coordinator 预算或物理进程完成证明；#14025 完整方案仍未完成。

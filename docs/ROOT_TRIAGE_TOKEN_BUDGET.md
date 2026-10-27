---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Root triage token accounting / 根任务 triage token 计账

Command classification and external-content screening share one triage terminal.
Its reported prompt/completion counts now charge the active context's existing
Agent-root token counter, including usage received before a stream failure.

命令分类和外部内容筛查共用一个 triage 终态。其已报告的 prompt/completion 计数
现在计入活跃 context 已有的 Agent 根 token counter，包括流失败前收到的用量。

## Ownership and compatibility / 所有权与兼容

The terminal uses the existing private root accounting helper. It reads the
request's scope, not mutable current-task state, and does not create a scope for
out-of-run classifications. Nested and budget-only projected contexts retain
the existing root lifetime and target-job cancellation authority.

终态复用既有私有根计账 helper，读取请求 scope，不读取可变当前任务状态；Run
之外的分类不创建 scope。嵌套和仅投射预算的 context 沿用既有根寿命与目标 job
取消权限。

Root observation is independent of the optional sink and positive-total
emission condition. Positive prompt/completion counts still count when the
provider omits its total; event source, model, pricing and emission stay the same.
Missing usage stays unknown, with no invented token or HTTP counts.

根观察不依赖可选 usage sink 或既有正 total 上报条件。供应商省略 total 时，
正 prompt/completion 计数仍扣计。既有事件 source、model、pricing 与上报条件
保持不变，不推造缺失 token 或 HTTP 次数；缺失用量仍未知。

One classification verdict still governs the same cached read-only decision.
The screen remains advisory: CLEAN, INJECTION and failed screening do not block,
drop or rewrite the fetched result. An INJECTION verdict keeps its existing
host notice. The accounting change adds no permission or safety exception.

分类结论继续控制同一缓存的只读判断。筛查仍为提示性质：CLEAN、INJECTION 或
筛查失败都不阻断、丢弃或改写已获取结果。INJECTION 保留既有 host 提示。
计账改动不新增权限或安全例外。

## Limit boundary / 阈值边界

An explicit token threshold may end the normal loop earlier, after a tool
round with existing finalization. Zero stays unbounded. No reservation, timer,
provider cancellation or process termination is added. Normal-round telemetry
and monetary completeness/currency rules stay unchanged.

显式 token 阈值可能更早结束正常循环；检查仍在工具轮后执行，并保留既有收尾。
本机制不做请求前预留、timer、供应商取消或进程终止。零值仍不限。辅助根计账
不改变正常轮次局部遥测、金额完整性或币种规则。

The classification and screening prompts, model selection, output/time bounds,
verdict cache and default screening setting stay the same. No API, config,
persisted or provider-wire field is added. Serialized main prefixes and tool
schemas are unchanged.

分类/筛查 prompt、模型选择、输出/时间边界、结论缓存与默认筛查开关保持原样。
不新增 API、配置、持久化或 provider-wire 字段；主序列化前缀和工具 schema 不变。

## Evidence boundary / 证据边界

Real Agent regressions cover classifier/screen usage, billed failures, missing
counts, disabled limits, absent sinks and omitted totals. Boot HTTP/SSE uses an
actual file-reading tool with declared external provenance through screening.
This is a constructed fixture, not live web-fetch or a production incident.

真实 Agent 回归覆盖分类/筛查用量、已计费失败、缺失计数、禁用阈值、无 sink 与
省略 total。真实 boot HTTP/SSE 夹具使用声明外部来源的文件读取工具，执行实际
筛查管线。这是构造的外部结果夹具，不是 live web-fetch 传输或生产事故复现。

The flagged control keeps both the actual file result and existing notice in
the next provider request. Disabled and missing-usage controls retain their
round counts. Serialized prefix equality in the fixture is a cache guard,
not a live-provider hit-rate measurement.

flagged 对照保留实际文件结果及下一供应商请求中的既有提示。禁用与缺失用量
对照保持轮次数。夹具中的序列化前缀相等是缓存守卫，不是供应商命中率实测。

This unit does not complete all auxiliary-provider admission, shared Coordinator
budgets, root currency aggregation, durable accounting or the full isolation
and recovery plan. See [root auxiliary tokens](ROOT_AUXILIARY_TOKEN_BUDGET.md).

本单元不完成全部辅助 provider 准入、共享 Coordinator 预算、根币种汇总、持久
计账或完整隔离恢复方案。作者分析、独立审查与原生平台证据分别记录。

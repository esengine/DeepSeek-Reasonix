---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Bounded calls in root token budgets / 根 token 预算中的有界调用

An active Agent root now receives usage from bounded reviewer/advisor calls
through one optional internal Go context observer. Bounded calls already emit
their own usage; they previously bypassed the root token counter.

活跃 Agent 根任务现在通过单个可选的内部 Go context observer 接收有界 reviewer/
advisor 调用的用量。此前这些调用已有自己的用量事件，却绕过了根 token 计数。

## Ownership / 所有权

Agent root binding installs the observer, and the budget-only job projection
copies it with the opaque scope. A child replaces the inherited observer rather
than stacking callbacks, so one logical bounded terminal charges the root once.

Agent 根绑定安装 observer；仅投射预算的 job context 将其与不透明 scope 一起携带。
子任务替换继承 observer，不叠加回调，因此一个逻辑有界调用终态只计一次。

The callback captures the original counter, not the Agent's mutable current-task
field. Late projected calls remain charged to the old task after a replacement.
Job cancellation and deadlines remain owned by the target job context.

回调捕获原 counter，不读取 Agent 可变的当前 task 字段。替换任务后的延后调用
仍计入旧任务。job 的取消与 deadline 继续由目标 job context 掌握。

The provider contract adds two internal Go helpers: one replaces the observer,
and one reports a copy of finalized usage. An observer cannot mutate the record
that the existing usage sink receives. Callbacks are trusted host code and own
their synchronization; this is not a callback panic-isolation boundary.

The helpers require a non-nil context, as usual in Go. The bounded owner retains
its existing context normalization before it calls them.

provider 契约新增两个内部 Go helper：一个替换 observer，一个上报终态用量副本。
observer 不能改写既有 usage sink 收到的记录。回调属于可信 host 代码，自行负责
同步；本机制不是回调 panic 隔离边界。

helper 要求非 nil context；有界 owner 调用前保留既有 context 规范化。

## Accounting and limits / 计账与阈值

The bounded owner reports its finalized usage once, including usage received
before a stream error. Input refusal without a request does not charge it.
Accounting does not depend on whether the optional usage sink/source is set.

有界调用 owner 仅上报一次终态用量，包括流错误前收到的用量。请求前的输入拒绝
不会扣计。计账不依赖可选 usage sink/source 是否启用。

Positive prompt/completion counts follow the existing saturating root rule,
including positive estimates. Missing counts remain unknown, not inferred from
request counts. Normal-round local telemetry and cost/currency rules stay as
they were; this does not add a monetary ledger.

正 prompt/completion 计数沿用既有根饱和规则，包括正估算值。缺失计数仍未知，
不能由请求次数推导。正常轮次的局部遥测与费用/币种规则保持原样，不新增费用账本。

The existing cooperative check runs after a tool round and keeps finalization.
Explicit limits can land earlier after an advisor call; zero still means
unbounded. There is no request reservation, provider cancellation or process
termination in this change.

既有协作式检查仍在工具轮后执行，并保留收尾轮。显式阈值可能在 advisor 调用后
更早收尾；零值仍不限。本改动不做请求预留、供应商取消或进程终止。

## Caller and compatibility boundary / 调用者与兼容边界

Advisor, recovery reviewer, Goal evaluator, best-of judge, commit-message and
prompt-refinement callers share the bounded owner. Participation depends on an
active context observer, not their label. Calls before/after Agent Run or
outside its context remain outside that root scope.

advisor、恢复 reviewer、Goal evaluator、best-of judge、提交信息和提示改写调用者
共用有界 owner。是否计入取决于活跃 context observer，而不是调用者标签。
Agent Run 前后或其 context 之外的调用仍不属于该根 scope。

The two helpers add no RPC, config, persisted or provider-wire field. Standalone
reviewers retain their two-message, no-tool requests and existing time/output
bounds. Main system messages, tool schemas and prefix construction are unchanged.

两个 helper 不新增 RPC、配置、持久化或 provider-wire 字段。独立 reviewer 保留
两条 message、无工具请求及既有时间/输出边界。主 system message、工具 schema
与前缀构造保持不变。

Real Agent and boot HTTP/SSE regressions cover actual advisor answers, nested
scope replacement, missing usage, disabled limits and serialized prefix guards.
Late projection is a constructed control, not a production background incident;
mock request equality is not a live-provider cache-rate measurement.

真实 Agent 与 boot HTTP/SSE 回归覆盖实际 advisor 答案、嵌套 scope 替换、缺失
用量、禁用阈值与序列化前缀守卫。延后投射属于构造控制，不是生产后台事故；mock
请求相等不是供应商命中率实测。

This is not complete auxiliary-provider admission, a shared Coordinator root,
durable accounting, root currency aggregation or the full isolation plan.
See [root token budgets](ROOT_TOKEN_BUDGET_SECURITY.md).

本机制不等于完整辅助 provider 准入、共享 Coordinator 根预算、持久计账、根币种
汇总或完整隔离方案。作者分析不等于独立 review 或原生平台执行。

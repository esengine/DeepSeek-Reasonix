---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Controller routing root budget / Controller 路由根预算

## Behavior / 行为

Controller prepares the executor-owned token/wall root after turn admission,
checkpoint setup and blocking prompt hooks, before semantic capability routing.
Both headless Run and the interactive turn orchestrator use the same helper.
Coordinator and Agent inherit that existing scope instead of replacing it.

Controller 在回合准入、检查点和可阻止提交的 hook 之后、语义能力路由之前准备
executor 所有的 token/wall 根预算。headless Run 和交互编排走同一 helper；
Coordinator 和 Agent 继承该根，不再为路由后的执行重新创建根。

The semantic router reports its existing finalized usage to the accepted
auxiliary observer before its optional audit and public usage event. Reported
usage counts even when parsing fails or a billed attempt returns an error.
Cached decisions send no request and incur no second charge.

语义路由把原有终态用量交给已有辅助 observer，再执行可选审计和公开事件。答案
解析失败或已报告用量后出错仍计已知用量；缓存命中不发请求，也不再次记账。

## Lifetimes and authority / 生命周期与权限

| Boundary | Contract / 约定 |
| --- | --- |
| Root ownership | Existing Agent preparation owns the counter, clock and immutable limits / 沿用 Agent 根预算所有权 |
| Continuation | Same host delivery scope retains the root / 同一宿主 delivery scope 保留根 |
| New task or scope | Starts a new root, even with a cached semantic decision / 新任务或 scope 换根，路由缓存不改变此边界 |
| Reset | Existing ResetTaskBudget replaces the counter generation / 沿用显式 reset 的代际替换 |
| Usage identity | Existing Estimated flag and request count remain / 保留已有估算身份与请求数 |
| Cancellation | Existing turn, provider and Manager authorities remain / 不复制或扩大取消权限 |
| Permission | Routing suggestions do not grant execution authority / 路由建议不授予执行权限 |

Zero limits remain unbounded. Positive estimated token counts follow the
existing conservative root rule; missing counts are unknown, not inferred from
request counts. Normal Agent streams retain their own accounting path, so the
semantic terminal does not charge a normal request twice.

零额度仍不限。正数估算 token 沿用已有保守根预算规则；缺失计数保持未知，不能用
请求数推断。普通 Agent 流仍自行记账，不通过语义路由出口重复计算普通请求。

Checks remain cooperative after a tool round with existing finalization. An
already exhausted root can still issue its first request and run a tool before
landing. This is not request admission, reservation, provider cancellation,
root money/currency aggregation, durable accounting or physical quiescence.

仍在工具轮后协作检查并保留原有收尾。已超额的根仍可先发首个请求并执行工具，再
进入软停止；不是请求准入、硬预留、供应商取消、根金额或币种汇总、持久记账或物理静默。

The wall clock now starts before semantic routing, after the earlier host gates.
It does not include boot, earlier queuing, input interception, reference
resolution or title generation. This unit does not cover every auxiliary
provider entry or unify each Agent's stricter local budget and telemetry.

wall 起点移到语义路由之前、前述宿主 gate 之后；不包括 boot、更早排队、输入拦截、
引用解析或标题生成。本单元未覆盖全部辅助供应商入口，也不统一各 Agent 更严格的
本地额度和遥测。

## Evidence and compatibility / 证据与兼容

Actual Boot HTTP/SSE exercises headless, interactive and separate planner/model
paths, reading a real fixture through use_capability. Configured routing spend
reduces executor reads from two to one. Zero configuration, explicit context
zero, missing usage and balanced-profile controls retain their behavior.

真实 Boot HTTP/SSE 覆盖 headless、交互和独立 planner 模型，通过 use_capability
读取实际夹具。配置额度时，路由用量使 executor 读取从两次变一次；零配置、显式
context 零值、缺失用量和 balanced profile 对照保持原行为。

Repeated real Controller runs cover a cached decision across a new task, same
delivery scope, explicit reset and a new scope. These are constructed host-API
sequences, not frontend interaction, a production incident or process restart.
Router tests retain error fallback, optional sinks, usage copying and cache hits.

重复执行真实 Controller，覆盖缓存路由下的新任务、同 scope 续跑、显式 reset 和
新 scope。这是构造的宿主 API 序列，不是前端交互、线上事故或进程重启复现。
路由 owner 测试保留错误回退、可选 sink、用量复制和缓存命中。

No API, config, wire, persisted schema, permission or default changes. Existing
model requests, routing decisions and stable prefixes remain; the HTTP fixture
checks prefix/schema equality within each run, not a live-provider cache rate.
The complete #14025 isolation, recovery and final-composition plan remains open.

不改变 API、配置、传输、持久化 schema、权限或默认值。保留模型请求、路由决策和
稳定前缀；HTTP 夹具只核对每轮前缀和 schema 相同，不测真实供应商缓存命中率。
整个 #14025 隔离、恢复及最终组合验收仍未完成。

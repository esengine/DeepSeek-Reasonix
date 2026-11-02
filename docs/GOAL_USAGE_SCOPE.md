---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-27
---

# Delegated Goal usage attribution / 委派任务的 Goal 用量归属

## Behavior / 行为

A background child keeps the Goal usage recorder captured when its tool call
starts. A child launched outside Goal mode stays outside Goal accounting even
if a new Goal is running when its usage arrives. Replacing, clearing or resuming
a Goal cannot redirect an old child's usage to the new lifecycle.

后台子任务保留工具调用开始时捕获的 Goal 用量记录器。在普通聊天中启动的子任务，
不会因结束时另一个 Goal 正在运行而计入它。替换、清除或恢复 Goal，都不会把旧
子任务的用量转移到新的生命周期。

Nested delegation inherits the captured owner. Within the same unchanged Goal
lifecycle, usage arriving between turns still updates the original recorder and
cumulative Goal totals. Existing scope and continuation-epoch checks reject
stale records. Unscoped synchronous producers retain active-turn attribution.

嵌套委派继承已捕获的归属。在同一个未变更的 Goal 生命周期中，两轮之间到达的
用量仍更新原记录器及 Goal 累计统计。既有 scope 和 continuation epoch 检查拒绝
过期记录。未绑定归属的同步生产者仍按活动轮记账。

## Shared boundary / 共享边界

`Agent.WithCallContext` binds an optional event-sink scope for model-selected
tools and host-invoked tools. `AuditForwarder`, `Sync` and `Coalesce` forward the
capture capability. A bound wrapper preserves earlier attribution instead of
capturing the current Goal again for grandchildren.

`Agent.WithCallContext` 为模型选择和宿主显式调用的工具绑定可选事件 scope。
`AuditForwarder`、`Sync` 和 `Coalesce` 转发捕获能力。已绑定的包装器保留更早
的归属，孙任务不会再次捕获当时活动的 Goal。

The Goal tee recognizes only its own private token. The original sink still
quotes, records and forwards usage once; source, model, token amounts and
session accounting are unchanged. Title usage keeps its existing exclusion.
The shared Agent token/wall budget and job cancellation are separate owners.

Goal tee 只识别属于自己的私有 token。原事件链仍负责一次性报价、记录和转发；
source、model、token 数量及会话统计不变。标题用量保持既有排除规则。共享 Agent
token/wall 预算与任务取消仍由各自 owner 管理。

The captured scope wrapper also forwards `UsageQuoter`. An Agent may obtain an
occurrence-time quote before emitting usage; nested binding retains that same
quote and the earlier Goal owner.

`CostQuoteSink` owns quoting while the bound
wrapper owns scope capture, so neither capability is counted as forwarding to
the downstream recorder when its wrapper is the owner.

已绑定 scope 的包装器也转发 `UsageQuoter`。Agent 可在发送用量前取得发生时刻的
报价；嵌套绑定保留同一报价及更早的 Goal 归属。`CostQuoteSink` 拥有报价能力，
已绑定包装器拥有 scope 捕获能力；各自拥有的能力不计作向下游记录器转发。

## Compatibility and limits / 兼容与限制

| Surface | Contract / 合同 |
| --- | --- |
| Event JSON, Desktop RPC and persisted records | No new serialized field / 不新增序列化字段 |
| Provider requests and stable prefix | Unchanged / 不变 |
| Sinks without the optional capability | Existing forwarding behavior / 保持既有转发行为 |
| Captured scope | Process-local; not restored by replay / 仅当前进程有效，回放不恢复 |
| Old goal totals already misattributed | Not repaired retroactively / 不追溯修复 |
| Late cumulative usage | In-memory until existing persistence points / 由既有持久化时点保存 |
| Budget enforcement, money and permissions | Unchanged / 不变 |

This is accounting attribution, not a reservation, billing reconciliation,
restart recovery or proof that a child process and its descendants have stopped.
Third-party sink wrappers must forward the optional capability to preserve it.

这是用量归属修复，不是预留、账单核对、重启恢复，也不证明子进程及后代已停止。
第三方事件包装器必须转发可选能力，才能保留该归属。

## Verification / 验证

Real Boot tests hold the new Goal's provider request while an old background
child reads a fixture and reports usage. They cover chat-to-Goal, replaced Goal,
nested children, same-Goal attribution and completion before a new Goal starts.

Unit tests cover clear/resume epochs, between-turn delivery, nested capture,
session tally, title exclusion and byte-identical JSON through the same quote chain.

真实 Boot 测试阻塞新 Goal 的 provider 请求，同时让旧后台子任务读取 fixture
并报告用量，覆盖聊天到 Goal、替换 Goal、嵌套子任务、同 Goal 归属，以及新 Goal
开始前已完成的对照。单元测试覆盖清除和恢复 epoch、轮间交付、嵌套捕获、会话
统计、标题排除及同一报价链上的 JSON 字节一致性。

These are constructed host-API sequences with an in-process scripted provider,
not a production incident or browser interaction. The Windows inventory must
pass 17 named test events across three packages without skips; parent tests are
included in that count. Cross-compilation alone is not native execution evidence.

这是使用进程内脚本 provider 的宿主 API 构造序列，不是线上事故或浏览器交互。
Windows 清单要求三个包的 17 个命名测试事件全部通过且无 skip；计数包含父测试。
交叉编译不能代替原生执行证据。

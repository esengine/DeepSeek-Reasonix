---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Coordinator root budget / Coordinator 根预算

The Coordinator prepares the executor's root token/wall scope before planning.
Its tool-enabled planner inherits that scope through Agent.Run; the legacy raw
planner reports one finalized usage record through the existing auxiliary
observer. Executor work then contributes to the same atomic token counter.

Coordinator 在规划前准备 executor 的根 token 和时间作用域。工具型 planner 经
Agent.Run 继承它，旧无工具流式 planner 经已有 observer 记录一次终态用量；随后
executor 使用同一个原子 token 计数器。

Each Agent's stricter local gate still takes precedence. Resetting the shared
root does not erase another Agent's local telemetry or loosen its own limits.

每个 Agent 更严格的本地上限仍优先。重置共享根不会清除其他 Agent 的本地遥测，
也不会放宽其已有本地限制。

Previously, the two Agent instances had independent roots, allowing an extra
executor tool round under a configured token limit. Local telemetry remains
per Agent. Local monetary limits are still separate; no floating-point sum or
new monetary ledger is introduced.

此前两个 Agent 的根计数独立，显式 token 限额下 executor 会多运行一轮工具。
本地遥测仍按 Agent 分开，金额上限也仍独立；不新增浮点汇总或金额账本。

## Lifetime and authority / 寿命与权限

| Transition | Root behavior / 根行为 |
| --- | --- |
| New ordinary task | Fresh counter and wall start / 新计数和时间起点 |
| Same host delivery scope | Keep accumulated tokens and original clock / 保留同一宿主任务累计值和时钟 |
| Explicit preserved continuation | Keep the old counter / 保留已有计数 |
| Plan-only or denied approval | Preserve planning spend without executing tools / 保留规划支出，不执行工具 |
| ResetTaskBudget | Next preparation creates a fresh counter / 下次准备新计数 |
| Replacement conversation | Fresh root even if the host scope ID is reused / 换会话对象后即使任务 ID 相同也换根 |
| Old projected job | Continue charging its captured old counter / 只计入捕获的旧计数 |
| Inherited root | Keep parent authority; child limits cannot widen it / 继承父级根，子级不能放宽 |

The session object and host scope identify preparation, independently of the
executor's evidence ledger. A no-executor turn persists conversation but never
calls Agent.Run; it must not lose its planning spend merely because that Run
has not opened an evidence ledger. No user text is parsed to infer continuation.

准备状态按会话对象和宿主任务作用域识别，不依赖 executor 证据账本。无执行回合
会保存会话但不调用 Agent.Run，不能因它尚未打开账本而遗失规划支出；不从用户
文字猜测是否继续。

Only the preparing executor adopts its own prepared root after opening its
turn. A companion Agent inherits accounting without claiming ownership of the
executor's preparation. Root limits are immutable per turn; overrides create
a new limit snapshot over the same counter, leaving an old job's tuple intact.

只有准备它的 executor 在开启回合后采纳该根；其他 Agent 仅继承计账。每回合的
根限制不可变，覆盖配置时用同一计数器生成新限制快照，不改旧任务捕获的组合。

PrepareRootTaskBudget is an internal Go host seam, serialized with Run,
SetSession and ResetTaskBudget. It carries context values, not cancellation or
permission changes. Existing Manager-owned jobs keep their own cancellation
authority and the accepted budget-only projection.

PrepareRootTaskBudget 是内部 Go 宿主接口，与 Run、SetSession 和 ResetTaskBudget
串行调用。它不增加取消或权限能力；Manager 作业保留自己的取消权限和已有预算投射。

## Evidence and limits / 证据与限制

Real Boot HTTP/SSE verifies configured, unconfigured, context-disabled,
missing-planner-usage and executor-only paths. With2,000 tokens and a1,100-token
plan, executor work now reads once and finalizes, rather than reading twice.
Request counts and source-specific usage events remain separate.

真实 Boot HTTP/SSE 覆盖显式限制、默认关闭、上下文关闭、planner 缺失用量及仅执行
路径。2,000 token 限制和 1,100 token 规划支出下，executor 从两次实际文件读取变为
一次后收尾；请求数和按来源区分的用量事件保留。

Coordinator tests exercise raw/tool planners, failure fallback, plan-only,
declined approval followed by continuation, and approved execution. Agent tests
cover reset, conversation replacement, scope replacement, late old-scope
accounting, immutable overrides and independent job cancellation.

Coordinator 回归覆盖两类 planner、失败回退、仅规划、拒绝审批后继续及批准执行。
Agent 回归覆盖重置、换会话、换任务、晚到旧作用域记账、不可变覆盖和独立作业取消。

These remain cooperative checks after a tool round plus existing finalization.
An in-flight first request/tool, concurrent children and approval waiting can
exceed a limit. The wall clock starts at Coordinator preparation, not boot or
earlier frontend queuing. There is no request reservation, timer or kill.

仍只在工具轮后合作式检查并保留已有收尾。在途首个请求或工具、并发子任务和审批
等待均可越界。根时钟始于 Coordinator 准备，不包含更早启动或前端排队；无预留、
定时器或强制终止。

Missing usage remains unknown. Routing/title work before Coordinator.Run,
cross-currency money, durable accounting and physical process completion are
not covered. Prefixes, request schemas, permissions, defaults and plan approval
policy do not change. The complete #14025 plan remains open.

缺失用量仍未知，不覆盖 Coordinator.Run 前的路由和标题、跨币种金额、持久计账或
物理进程完成。不改变前缀、请求 schema、权限、默认值或规划审批策略；#14025 完整
方案仍未完成。

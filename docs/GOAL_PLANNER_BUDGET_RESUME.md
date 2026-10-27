---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-27
---

# Goal budget resume across roles / Goal 跨角色预算恢复

## Behavior / 行为

Resuming a Goal stopped by an explicit spend budget renews the local budget
slice of both executor and tool-enabled planner. The next serialized turn keeps
the existing Goal objective and delivery evidence, with cumulative usage still
visible. Its shared token/wall root is prepared by the existing executor owner.

显式支出预算耗尽后恢复 Goal，会同时续给 executor 和启用工具的 planner 新的本地
预算切片。下一轮仍按原序列化入口执行，保留 Goal 目标、交付证据和累计用量；共享
token/wall 根仍由既有 executor owner 准备。

The planner has its own Agent ledger and local gate. Renewing only the executor
root leaves old planner spend active: fresh planning can stop after its first
tool round despite the user renewing the task budget.

Planner 有独立 Agent 账本和本地门禁。只重置 executor 根，仍会保留 planner 的旧
支出：即使用户已续给任务预算，新的规划也可能在首个工具轮后提前收尾。

## Authority and lifetime / 权限与寿命

| Boundary | Behavior / 行为 |
| --- | --- |
| Budget-spend stop, successful ResumeGoal | Reset both participating local accumulators / 重置两个参与角色的本地累计 |
| Manual pause or another stop reason | Preserve spend / 保留支出 |
| Resume refused | Preserve spend / 不改变支出 |
| Raw planner or executor-only runner | No planner Agent to reset / 没有额外 planner Agent |
| Goal statistics, transcript and delivery checkpoint | Preserved / 保留 |
| Existing projected background context | Keeps its old root counter / 仍绑定旧根计数 |
| New resumed turn | Uses the existing fresh-root preparation / 采用既有的新根准备 |

`ResumeGoal` changes Goal state; it does not start a turn. Frontends continue
through the same Controller API. Agent budget reset retains its existing
requirement that the affected Agent has no active Run.

`ResumeGoal` 只改变 Goal 状态，不自行启动新一轮。各前端仍使用同一 Controller
API。重置 Agent 预算仍要求该 Agent 没有活动 Run，沿用原生命周期约定。

This does not reset every Agent in the process or renew arbitrary jobs. There
is no new API, configuration, schema, persisted format, permission, default,
request construction, prefix or cancellation authority.

不重置进程内所有 Agent，也不续给任意后台任务预算。不新增 API、配置、schema、
持久化格式、权限、默认值、请求构造、前缀或取消权限。

Token/wall limits remain cooperative after tool rounds, with existing
finalization. In-flight work can exceed them. This is not a request admission,
money reservation, durable account, physical-process boundary or completion of
the overall isolation/recovery plan.

Token/wall 仍在工具轮后协作检查并沿用原收尾；在途工作可能越界。此处不是请求
准入、金额预留、持久化账本或物理进程边界，也不代表完整隔离恢复方案完成。

## Verification / 验证

The real Controller/Coordinator/Agent loop exhausts a first budget, resumes
through ResumeGoal and measures the second planner's requests. Manual-pause
and refused-resume controls keep the old planner budget.

真实 Controller/Coordinator/Agent 循环耗尽首个预算，再通过 ResumeGoal 恢复并
测量第二轮规划请求。手动暂停和拒绝恢复对照保留原 planner 预算。

Actual Boot plus local HTTP/SSE runs separate planner/executor models and reads
real fixture files through the exposed tools. It asserts three planner reads
plus finalization per slice, reported usage identity, and unchanged cumulative
Goal statistics across resume.

This is a constructed host sequence, not a live
provider incident, frontend interaction or restart test.

实际 Boot 与本地 HTTP/SSE 使用独立 planner/executor 模型，通过暴露工具读取
真实文件；每个切片断言三次规划读取和一次收尾、用量身份，以及恢复时累计统计
不变。这是构造的宿主调用序列，不是线上事故、前端交互或重启测试。

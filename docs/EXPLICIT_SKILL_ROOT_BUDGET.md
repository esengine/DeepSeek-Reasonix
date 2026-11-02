---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-27
---

# Explicit skill root budget / 显式技能根预算

## Behavior / 行为

Explicit subagent slash commands and composer selections share the executor's
existing token/wall root. Preparation occurs after checkpoint and blocking
prompt hooks. An active Goal uses its existing budget and usage recorder.

显式子代理 slash 命令和 composer 选择共享 executor 已有的 token/wall 根预算。
在检查点及可阻止提交的 hook 之后准备；活跃 Goal 使用已有预算和用量 recorder。

Synchronous CLI profile run and preview use the Controller's existing blocking
turn admission before root preparation. Busy, closed and draining controllers
refuse entry; Controller cancellation reaches the admitted profile. Existing
workspace activity and write-authority checks now apply to this entry too.

同步 CLI profile run 和 preview 在准备根预算前走 Controller 已有同步准入。
忙碌、关闭或 draining 时拒绝进入；Controller 取消传给已准入的 profile。
这一入口也遵守已有工作区活动和写入 authority 检查。

## Lifetime and limits / 生命周期与边界

Same delivery scope retains accumulated spend. New tasks, sessions, scope IDs
and explicit budget resets start a fresh root. An inherited root remains the
owner; projecting an old context into a job retains the old counter without
copying its cancellation. Multiple selected skills share one root.

同 delivery scope 保留累计用量；新任务、session、scope ID 和显式预算 reset
开始新根。继承的根保持所有权；旧 context 投射给 job 保留旧计数但不复制取消。
一次选择的多个技能共享同一根。

Zero remains unlimited, including an explicit context override. Checks are
cooperative after tools and keep existing finalization.

A paused child returns
its existing typed error; subsequent selected skills are not started after
that error. A later child may still perform its first tool round on an already
spent root. This is not request admission or a hard reservation.

零额度仍不限，包括显式 context 覆盖。在工具后协作检查并保留已有收尾。
暂停的 child 返回已有类型化错误，之后的选中技能不再启动；后续 child 仍可能
在已耗尽的根上执行首个工具轮。这不是请求准入或硬预留。

The clock starts at preparation, not boot or earlier queueing. Local Agent
budgets remain stricter independent gates. Money, durable recovery, all
auxiliary consumers and physical process quiescence remain separate work.

时钟从准备根开始，不含 boot 或更早排队。Agent 本地额度仍是独立的更严格 gate。
金额、持久恢复、全部辅助消费者及物理进程静默仍是独立工作。

## Evidence / 证据

Boot HTTP/SSE tests exercise profile, preview, slash, composer multi-selection
and Goal against real file reads. Zero configuration and explicit zero remain
controls.

Real Controller/Agent sequences cover root lifetime, old job
projection and cancellation/admission. These are constructed local tests, not
an observed production incident, frontend interaction or restart proof.

Boot HTTP/SSE 测试通过真实文件读取覆盖 profile、preview、slash、composer
多选和 Goal，保留零配置与显式零值对照。真实 Controller/Agent 序列覆盖根寿命、
旧 job 投射及取消/准入。这些是本机构造测试，不是线上事故、前端交互或重启证明。

No new API, configuration, wire or persisted schema is introduced. Model
selection, tool permissions and request construction stay with their owners.
The HTTP fixture checks stable child prefix/schema bytes within a run; it does
not measure live provider cache hit rate. The complete #14025 plan remains open.

没有新增 API、配置、传输或持久化 schema。模型选择、工具权限和请求构造保留
原有 owner。HTTP 夹具只核对同轮 child 前缀/schema 字节稳定，不测线上缓存命中率。
完整 #14025 计划仍未闭环。

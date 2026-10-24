---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Conversation and task continuation / 聊天与任务续跑

## 中文

`continue_from` 在当前聊天内继续原任务；在同源工作区的后代聊天中引用祖先任务，
则创建当前聊天自己的副本。已有副本时，祖先引用解析到它，不反复复制。
多个候选存在歧义时必须使用准确的副本引用。`fork_from` 不属于公开工具定义；
旧调用的同聊天拒绝语义不变。

隔离副本使用新的 `sa_`、`iso_` 及 generation，复制来源候选的封存 Git 场景，
保留来源最初的整合比较基线。Git 的 checkout/属性规则仍然适用。

声明的环境输入复制到独立目录，重新核验当前禁止读取规则。
来源验证凭据不传给副本。候选外部漂移、未封存内容、未发布的依赖修改、
活动来源或未结算的子成果/整合尝试都会明确拒绝复制，并保留原成果。

当前聊天与最终祖先引用的准备别名由 OS 锁协调。准备、后台排队取消及失败
均释放本次所有权；新引用已持久化为 running 后释放准备锁，模型运行不持有它。
重复旧回调不会释放新所有者。共享任务保持原环境；已有共享任务不能在续跑中
静默迁移，显式要求不兼容环境时拒绝。

复制一次申请来源候选、目标候选及 Git common directory；取得资源后重查
generation、封存内容及来源身份。嵌套副本在快照之前登记当前执行的直接父任务，
不能把祖先来源当作执行父任务。开始物化前持久化准备记录；中断的候选保持
可发现并进入恢复，不假装已继续运行。原候选之后安全丢弃不会破坏副本。

祖先副本使用结果格式 v6、`snapshot_origin.version=2` 和
`kind=ancestor_copy`。旧 v5 reader/writer 拒绝该结果；保存、依赖及整合登记
不得降级版本。不可变根导出继续使用 origin v1，不能当作可继续执行的任务副本。

聊天分支与子任务续跑是两条不同路径。持久根环境中的 `BranchAt` 为新聊天
建立独立环境绑定，继承已冻结的检查契约；关闭后重新 Build/Resume 使用同一环境。
父运行时拒绝绑定子环境的 transcript，子聊天修改不进入父现场或源工作区。

这些约束协调遵守协议的写入者。外部进程仍可能越过文件协调，观察不完整或
内容变化时必须拒绝或保留未知状态，不能宣称文件锁构成完整沙箱。

## English

`continue_from` resumes a task within its owning conversation. A descendant
conversation in the same source workspace creates its own copy of an ancestor
task. Later ancestor references resolve to that existing copy. Ambiguous aliases
require an exact copied reference.

`fork_from` remains absent from the public
tool schema; its legacy same-conversation refusal remains unchanged.

An isolated copy receives new transcript, result and execution identities. It
materializes the sealed candidate Git scene while preserving the original
source comparison baseline. Git checkout and attribute rules still apply.

Declared preparation inputs use independent storage and current read-denial
checks. Source verification receipts are never inherited.

Active sources,
external drift, unpublished dependency changes, incomplete scenes and unsettled
descendants or integration attempts refuse copying and retain the source.

An OS preparation owner coordinates each current-conversation/nearest-ancestor
alias. Failures and queued cancellation release only that owner. Durable running
publication releases preparation before model execution. Old callbacks cannot
release a successor.

Shared tasks retain their environment and refuse an
incompatible explicit isolation request during continuation.

Copying acquires source, destination and common Git resources together, then
rechecks generation, sealed content and source identity. A nested copy registers
its executing direct parent before snapshot creation.

Preparation is durable
before materialization. Interrupted candidates remain discoverable for recovery;
source disposal does not invalidate an independent copy.

Copies use result format v6 with origin version 2 and kind `ancestor_copy`.
Previous v5 readers/writers refuse them. Ordinary saves and dependency or
integration registration preserve that barrier.

Immutable root exports retain
origin v1 and cannot resume as task copies.

Root conversation branching is separate. `BranchAt` creates an independent
environment binding and inherits frozen host checks. Rebuilding and resuming
after close reopens the same scene. A parent runtime refuses the child's bound
transcript, and child writes do not reach the parent scene or source workspace.

The protocol coordinates participating writers. External processes can bypass
it; incomplete observation or content drift must remain a refusal or unknown
state, never a claim that file coordination is a complete sandbox.

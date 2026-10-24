---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Nested isolation result management / 嵌套隔离结果管理

## 中文

本页记录新版协作策略的嵌套执行及成果管理实现，不改变新安装默认策略。
总体边界见[并发变更说明](CONCURRENT_MUTATIONS.md)和[协议矩阵](CONCURRENT_MUTATIONS_PROTOCOL.md)。

根会话的隔离成果列表沿已登记的直接父子关系发现保留的子成果及其整合尝试。
未登记的候选不会因共享存储目录而获得管理权限。列表显示直接父任务及实际整合目标；
子成果的目标是其直接父环境。管理路由提供查询、明确预览后的丢弃和日志恢复，
不会仅因查到结果就获得整合或向祖先写入的权限。

具有宿主环境工厂的新版运行时支持显式嵌套 worktree；`shared` 和只读任务继续共享当前现场。
`auto` 沿用同一后台/并行选择规则。没有工厂的旧版或自定义运行时仍拒绝显式嵌套隔离。
候选 Controller 和工具执行使用同一继承审批模式；子任务不扩展调用方的工具及路径授权。

每一层都核对实际执行中的 delegation 身份、当前直接源权限和捕获的祖先授权链。
未执行子代理的辅助 Controller 仅检查运行时是否仍有效，不能用它的会话路径冒充实际写入者。
实际文件路径按字面值逐层投影，不能误按 glob 解析；符号链接逃逸拒绝。

续跑子环境时，在取得子资源及 entry 锁后重新取得父 entry 锁，并保持至子新 generation 落盘。
父执行结束、换代或封口后不能继续准入；等待资源期间父封口同样会拒绝续跑。
未结算后代仍会保留父环境；父已结束时，先通过根成果管理处理后代，再继续父任务。

父执行结束但后代尚未结算时，父记录保留 generation 和文件并标记中断，清除未封存的验证声明。
崩溃遗留的 running 记录若已无物理 run owner，列表只在读取结果中显示中断，不改写原文件。

源会话的 `list_subagents` 按同一来源工作区及会话所有权规则发现隔离任务，
可以找到失败父任务的引用；显式处理后代后，可在重建的运行时继续原父候选。

父记录终态迁移时允许同一候选目录的旧 marker 别名，保持子成果的不可变反向关联。
ID、generation 和 token 仍须匹配；运行中的父记录不接受该迁移别名。

同 ID 重复、反向关联不匹配、未知版本、坏 JSON 或非普通记录文件会保留现场并拒绝管理。
丢弃仍绑定完整预览版本；候选变化后需要重新预览。清理子成果不会修改任何祖先工作副本。

嵌套成果首次准备整合时，在同一 child entry 锁内核验原始清单和执行身份、持久发布 v5 屏障，
再登记独立的 int 尝试。登记拒绝后，失败收尾不能绕过该边界补建记录。
屏障与首条 int 之间中断时，child 仍可被发现和处理，不会仅因 v5 永久占用父环境。

即使 child 已丢弃或整合，其尚未结算的 int 仍保留直接父环境。只有关联尝试全部处于终态、
完成清理且释放物理 OS owner，父任务才可换代或清理。child 自身续跑也不能跨越待处理的 int。
旧 generation 的已结算收据可以保留，不阻止之后合法的续跑。

普通持久化和父子关系更新保留 v5 及未知字段。已验证的先前 v4 实现拒读/拒写该版本。
版本屏障不能撤销已经越过旧版快照阶段的在途直接 Store API 调用；不将其宣传为完整跨版本 fencing。
先前生产入口未开放嵌套 worktree；其兼容边界不能被新版工厂接线追溯补足。

后代整合中断时，根列表显示直接源工作区、待恢复路径及“恢复未完成的整合”。
CLI 使用 `/isolated recover <iso 或 int ID>` 生成预览，再执行其确认命令。

恢复请求携带可选 `resultId`、当前 runtime 和精确 journal revision；旧根恢复请求保持兼容。

Controller 执行前重新解析登记关系，Store 在锁内重新核验日志及实际路径，
把后代路径投影回根后检查当前写入权限。恢复只处理实际直接源中的 journal，
不把子成果应用到根，也不恢复“验证通过”的声明。写后版本已被外部修改时保留现场。

## English

This page records nested execution and management under the new collaboration
strategies. It does not change installation defaults. See the
[concurrency guide](CONCURRENT_MUTATIONS.md) and [protocol matrix](CONCURRENT_MUTATIONS_PROTOCOL.md).

The root catalog follows registered direct-parent edges to retained descendants
and their integration attempts. Sharing a storage root grants no authority over
unregistered candidates.

Each descendant displays its direct parent and actual integration target.
This route supports observation, explicitly previewed discard and journal
recovery. Discoverability alone grants neither integration nor ancestor writes.

New runtimes with a host environment factory support explicit nested worktrees.
Shared and read-only tasks keep the current scene; auto uses the existing
background/parallel selection rule. Legacy or custom runtimes without a factory
still refuse explicit nested isolation.

Candidate controllers and tools inherit
one approval posture without broadening tool or path grants.

Every level checks the actual delegation execution, current direct-source
permissions and captured ancestor authorization chain. An unused helper
controller checks runtime validity only; its session is never substituted for
the actual writer.

Concrete paths are projected literally, with containment and
symlink-escape checks, rather than parsed as glob declarations.

Child continuation acquires child resources and entry before taking the parent
entry, then holds that parent entry through durable publication of the new child
generation. Stopped, replaced or sealed parents refuse admission, including
sealing while continuation waits for resources.

Unsettled descendants retain
their parent; settle them through root management before resuming a stopped parent.

A stopped parent with unsettled descendants retains its generation and files as
interrupted, clearing unsealed verification claims. Catalogs project an unowned
running crash record as interrupted without rewriting the original record.

The source conversation's list_subagents uses the same origin-workspace and
conversation ownership rule as read and continue. Failed parent references remain
discoverable; after explicitly settling descendants, a rebuilt runtime can
continue the retained parent candidate.

Terminal marker promotion accepts only the old marker in the same candidate
directory, preserving immutable child backlinks. ID, generation and token must
still match. Running parents do not accept this promotion alias.

Duplicate IDs, mismatched backlinks, unsupported versions, malformed JSON and
non-regular markers retain state and refuse management. Discard remains bound to
the complete preview revision. Cleaning a descendant never writes ancestor files.

Initial nested integration registration rechecks the immutable manifest and
execution identity under the child entry lock, durably publishes a v5 barrier,
then creates the int record. Rejected registration cannot publish a record from
failure cleanup.

Interruption between the barrier and initial record leaves a
discoverable child whose explicit settlement can still release its parent.

Even a terminal child pins its direct source while an associated int is pending,
uncleaned or physically owned. Parent generation changes and cleanup wait until
all attempts settle. Child resume also refuses pending attempts; retained settled
receipts from earlier generations do not prevent a later valid resume.

Ordinary persistence and lineage updates preserve v5 and extension fields. The
qualified earlier v4 implementation refuses this boundary.

A version upgrade
cannot revoke an old direct API preparer already past its snapshot; complete
fencing of that unsupported in-flight call is not claimed. The previous
production entry point did not allow nested worktrees.

Interrupted descendant integration exposes its actual source and affected paths
in the root catalog. Use the recovery action or `/isolated recover <iso or int ID>`
and its preview confirmation. Requests add optional resultId while preserving
legacy root recovery.

The controller refreshes registered identity and runtime;
the Store rechecks journals and actual paths under coordination, projecting them
to the root for current authorization. Recovery handles the direct-source journal
without applying child changes into the root or restoring verification claims.

External changes to postimages retain the scene instead of being overwritten.

### Durable task admission / 任务准入持久化

The transcript is saved before a delegated run is published as running. Its
actual admitted task message is saved before the first provider request. A failed
delegated admission save refuses execution.

A process crash retains the original task prompt and candidate files. Continuing
requires a new execution generation and settling unfinished descendants.
In-flight assistant and tool messages after admission may be lost until the
terminal save.

子代理发布运行状态前保存初始会话，首次模型请求前保存实际准入的任务消息。
子代理的准入保存失败会阻止执行。进程崩溃后保留原任务文本和候选文件；续跑仍
需要新的执行代次，并先处理未结算的后代。准入之后尚未完成保存的模型和工具消息
可能丢失，不承诺完整恢复进行中的回合。

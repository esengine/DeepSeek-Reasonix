---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Concurrent mutation protocol compatibility / 并发变更协议兼容

This is an implementation and compatibility record. Qualification scopes and
new-install activation are recorded in [acceptance](CONCURRENT_MUTATIONS_ACCEPTANCE.md).
See [the user and implementation guide](CONCURRENT_MUTATIONS.md).

Direct-parent management and retention are described in the
[nested result guide](CONCURRENT_MUTATIONS_NESTED.md).

本文记录实现及兼容边界；验收范围及新安装默认策略见[验收记录](CONCURRENT_MUTATIONS_ACCEPTANCE.md)。
操作方式见[使用与实现说明](CONCURRENT_MUTATIONS.md)。
直接父子关系的管理与保留规则见[嵌套成果说明](CONCURRENT_MUTATIONS_NESTED.md)。

## 中文

| 数据或协议 | 旧数据及新读写器 | 旧读写器 | 结论 |
| --- | --- | --- | --- |
| 已有配置未写 collaboration | 沿用兼容模式；不重写旧开关 | 继续使用旧设置 | 不自动迁移活动运行 |
| collaboration-default-v1.json | 全新目录持久选择 auto；实际读到旧用户配置时仍按 legacy 解码；只读加载不写入 | 旧程序忽略选择文件，保留其原行为 | OS 锁协调首次初始化；未知 marker 保留拒绝，不删除旧值 |
| auto_integrate | 未配置视为开启；用户配置可关闭；保存值和有效值分开 | 不认识该选项 | 新运行时生效；旧程序不能据此提供自动整合保障 |
| mutation 资源记录 | 校验版本、身份、路径及 OS 所有权；未知/损坏记录保留并拒绝绕过 | 旧 workspace claim 保持互斥 | 兼容长锁仍可能阻塞短事务 |
| 无版本 iso_ 结果 | 保留身份并重新观察候选 | 仍可能使用旧清理规则 | 不能把缺失的旧证据视为通过 |
| v2 iso_ 结果 | 独立 marker、封存清单、保留未知字段 | 仅支持旧格式的程序拒绝该版本 | 候选变化需恢复；终态记录保留 |
| v3 根导入结果 | 记录基线、不可变根快照、依赖副本和来源身份 | 本实现先前的 v2 reader 拒绝 v3；未版本化旧程序不发现独立 marker | 活动根现场不会成为可删除候选 |
| v4 父子依赖记录 | 子快照前登记直接父关系；封口后拒绝新增；待处理孩子阻止父封存、换代和清理 | v2/v3 reader 拒绝 v4 | 新版工厂统一 BeginChild/ResumeChild；子换代保持父准入锁至落盘 |
| v4 preparation-lock-v1 | 登记前持准备 OS 锁；无记录、准备者及 run 都已结束才回收登记 | 先前 v4 写者保留未知字段但不回收；未标记的旧登记保留 | 不按超时撤销活准备者；扫描错误不能证明不存在 |
| v5 嵌套整合依赖 | child 锁内先发布版本屏障再登记 int；父结算同时核对关联 int 的终态、清理和 OS owner；普通保存不降版本 | 先前 v4 reader/writer 拒绝 v5，不能删除仍被关联 int 引用的父环境 | 不追溯拦截已越过旧版快照阶段的在途直接 API 调用；旧生产 nested guard 未开放 |
| v6 祖先任务副本 | origin v2/ancestor_copy 固定来源身份、物化快照及原整合基线；副本独立续跑 | 先前 v5 reader/writer 拒绝；不降版本 | 来源凭据不继承；origin v1 根导出保持不可变 |
| publication revocation v1 | 同一 entry 锁内撤销、应用及提交；先持久撤销再更新根记录 | 不支持撤销的早期实验 v3 reader 可忽略 sidecar | 早期实验 v3 不属于安全降级范围 |
| int_ 尝试 v1 | 独立候选及精确快照检查；恢复保持成果身份 | 旧程序不读取该目录 | 历史验证不代表当前工作区仍有效 |
| recovery.resultId | 可选字段路由至登记的直接源，检查 runtime、revision 及当前根权限 | 缺字段保持根恢复；旧读者忽略新增的行级 recovery | 管理可发现性不授予整合权限；旧内核拒绝不属于根工作区的恢复目标 |
| 根环境 v1 | 可重开现场；无冻结检查契约时不从现场补取检查 | 实验 v1 reader 不能理解新的安全规则 | 不授予自动验证发布资格 |
| 根环境 v2 | 检查契约在可写前冻结；fork 继承；版本及契约不可替换 | v1 reader 拒绝 v2 | 未变化的未知契约字段保留 |
| publication v1 | preparing、pending、acknowledged；只推进导出的根游标 | 未知格式保留 | 新 ID 替换旧对象；同 ID 才递归保留未知字段 |
| 环境绑定 v1 + 会话事件 envelope v6 | 首条日志持久绑定；sidecar 缺失从日志恢复，冲突拒绝；绑定不改变原始消息日志、revision 或摘要 | 已验证的前版 reader/writer 将 schema 6 作为未来格式拒绝；不能忽略日志回落 checkpoint | 追加、压缩及修复保留屏障；旧 sidecar-only 实验会话须由当前会话 authority 在执行前升级 |
| 检查进程崩溃 | 取得 run 锁后核验封存内容，同 ResultID 重检 | 不共享过程内受控凭据 | 重启后证据保持未知，不能反序列化为有效 |
| 容器验证日志 v1 | OS owner、Engine/端点及对象身份校验；未知版本、字段和尾随数据保留并拒绝回收 | 没有容器工厂的内核拒绝镜像 URI，不回收该资源 | 本地 Unix socket/命名管道；过程内凭据不能跨重启复用 |
| 终态收据 | 清理后重复同操作幂等；相反操作拒绝 | 按其理解的格式读取 | 应用成功和当前验证有效是两个事实 |
| 结果身份及迁移 | 工作区过滤前严格扫描同 ID；仅允许同目录完全相同的终态迁移对，并先归一化再更新 | 宽松旧 reader 没有全域去重保证 | 重复、不可读或无法归因的记录拒绝执行并保留 |
| 等待事件 | 可选事务、会话、执行及原因字段 | 旧事件继续读取 | 无身份的结束事件仅闭合无身份的旧等待 |
| 未完成源整合日志 | 当前程序所有策略均阻止普通写入；覆盖最终写根及重建复用的旧 owner；按预览条件恢复 | 不理解日志的旧程序仍可能忽略它；独立旧会话不受新会话 envelope 保护 | 当前程序不能切换 legacy 绕过；不能据此承诺任意旧程序的降级写入安全 |

验证平台及性能报告必须绑定具体源码快照。源码审计、受影响包测试、原生平台测试、
浏览器模拟界面、托管 CI 和发布验收是不同证据，不能互相替代。

v4 并存回归使用本分支先前的 v2 实现编译独立写入进程，不代表该实现已经发布。
验收记录应同时保存旧源码提交、测试 helper、构建命令和二进制摘要；环境变量提供
一个二进制路径本身不能证明其版本。已发布的未版本化读写器另受 marker 发现边界约束。

环境绑定先在会话保存锁内以严格原子替换发布 v6 envelope，再写派生 sidecar。
Clear、Fork 和恢复副本均在消息发布前绑定目标身份；恢复使用保存 owner 发出的
限时 publication capability，不能再次取得同一保存锁。

历史版本保留绑定并标记
`archive_only`，允许阅读但拒绝执行。移动、原生导出及跨项目采用尚不支持迁移绑定，
因此明确拒绝，不能产生可执行的无约束副本。物理重命名同样拒绝。

这些边界约束支持协议的读写器。旧程序或任意外部进程完全不读取会话/整合协议时，
新程序不能在崩溃后凭一个 sidecar 阻止其直接写文件；这种降级不属于安全兼容范围。

Linux 的显式离线根文件系统及各宿主的本地容器验证可以产生过程内受控凭据。
容器验证不改变 Windows 候选 Bash/hook 的写入约束要求。

普通 Shell 观察不会被升级为完整输入证明。非执行位或目录
权限变更无法由当前 Git 应用协议重现时，会拒绝发布并保留成果。

候选完整性是执行环境要求，独立于审批。完整文件变更边界不受支持时，在 Windows
ACL 授权、hook 启动及 Bash 逃逸审批之前拒绝。源保护路径按最终可写范围核验，
包含实际私有临时目录。这是运行时策略，不迁移配置，也不扩大创建限制令牌的承诺。

## English

| Format or protocol | Existing data and current reader/writer | Previous reader/writer | Conclusion |
| --- | --- | --- | --- |
| Existing config missing collaboration | Preserve compatibility mode and old switches | Keeps its settings | No migration during active execution |
| collaboration-default-v1.json | Persist auto only for fresh profiles; actual old config bytes still decode as legacy; read-only loading writes nothing | Ignores the marker and keeps previous behavior | OS initialization owner; retain and refuse unknown markers without deleting existing choices |
| auto_integrate | Missing means enabled; user-only choice; saved/effective values separate | Ignores the unknown setting | Applies to new runtimes; does not establish old-program integration guarantees |
| Mutation resources | Validate version, identity, paths and OS ownership; retain unsupported records | Exclusion through legacy workspace claims | Legacy long holds can still block short transactions |
| Unversioned iso_ | Retain identity and observe current candidate | May retain old cleanup behavior | Missing evidence is unknown |
| v2 iso_ | Separate marker, sealed manifest, unknown-field preservation | Older-format-only readers refuse | Content drift requires recovery; terminal records remain |
| v3 root imports | Baseline, immutable root snapshot, frozen dependencies and origin | Earlier v2 implementation readers refuse v3; unversioned legacy readers do not discover the separate marker | Persistent live scenes never become disposable candidates |
| v4 parent/child dependencies | Register the direct parent before a child snapshot; seal admission; unsettled children block parent sealing, generation changes and cleanup | v2/v3 readers refuse v4 | New factories use BeginChild/ResumeChild; child generation publication retains parent admission ownership |
| v4 preparation-lock-v1 | Own preparation OS lock before registration; reclaim an absent record only after preparation and run end | Earlier v4 writers preserve unknown fields without reclaiming; untagged registrations remain | Timeouts do not revoke active preparers; scan errors do not prove absence |
| v5 nested integration dependencies | Under the child lock, publish the version barrier before int registration; parent settlement checks associated int decisions, cleanup and OS ownership; saves never downgrade | Earlier v4 readers/writers refuse v5 and retain the source environment pinned by its integration attempt | Does not fence an old direct API preparer already past its snapshot; the previous production entry point did not allow nested worktrees |
| v6 ancestor task copies | Origin v2/ancestor_copy pins source identity, materialization and original comparison baseline; copies resume independently | Previous v5 readers/writers refuse; saves never downgrade | Source receipts are not inherited; origin v1 root exports remain immutable |
| Revocation v1 | Revoke, Apply and Commit fence at the same entry lock; revocation persists first | Early experimental v3 readers can ignore the sidecar | Those experimental readers are outside the safe downgrade scope |
| int_ v1 | Separate candidate and snapshot-bound checks; recover same identity | Does not read this directory | Historical verification does not prove current source validity |
| recovery.resultId | Optional direct-source routing rechecks registered identity, runtime, revision and current root permissions | Absent means legacy root recovery; older readers ignore added per-row recovery | Discoverability grants no integration authority; older kernels refuse foreign-workspace recovery targets |
| Root environment v1 | Reopen retained scene; never refill missing contracts from mutable instructions | Experimental v1 lacks new guarantees | No automatic verification publication authority |
| Root environment v2 | Host contract frozen before writable execution; inherited by fork; immutable version/contract | v1 readers refuse v2 | Unchanged opaque contract fields retained |
| Publication v1 | preparing/pending/acknowledged; cursor advances only to root export | Unsupported formats retained | New identities replace old objects; recursive preservation only within one ID |
| Environment binding v1 + session-event envelope v6 | Durable first-record binding; recover absent sidecar from WAL, refuse conflicts; preserve original message WAL, revision and digest | Qualified previous reader/writer refuses schema 6 as a future format, without checkpoint fallback | Append, compact and repair retain the fence; live session authority upgrades experimental sidecar-only sessions before execution |
| Check-process crash | Acquire run owner, verify sealed content and recheck same ResultID | No reusable process-owned receipts | Restart leaves verification unknown |
| Container verification journal v1 | OS owner, Engine/endpoint and object identity checks; retain unknown versions, fields and trailing data | Kernels without the factory refuse image URIs and do not reclaim these resources | Local Unix socket/named pipe; process-owned receipts cannot survive restart |
| Terminal receipts | Same-action retries idempotent after cleanup; opposite action refuses | Reads supported formats | Publication and current verification validity remain separate facts |
| Result identity and migration | Strict global ID scan before workspace filtering; only identical terminal markers in one directory may normalize before updates | Permissive old readers lack global deduplication | Duplicate, unreadable and unidentified records refuse execution and remain retained |
| Wait events | Optional transaction/session/execution identity and reason | Legacy events remain readable | Identity-free closure matches only identity-free waits |
| Source integration journals | Every strategy in the current program blocks ordinary writes; cover final write roots and reused owners after rebuild; preview-bound recovery | Unaware older programs can ignore journals; independent old sessions have no new session envelope | Switching the current program to legacy cannot bypass recovery; arbitrary old-program downgrade writing is outside this guarantee |

Bind platform and performance reports to a specific source snapshot. Source
review, owning-package tests, native platform execution, mocked browser flows,
hosted CI and release qualification are distinct evidence classes.

The v4 coexistence regression compiles an independent writer from this branch's
earlier v2 implementation; it does not establish that implementation was released.

Retain its source commit, helper, build command and binary digest. An environment
variable naming a binary cannot prove its version. Released unversioned readers
have the separate marker-discovery boundary.

Binding publishes the v6 envelope with strict atomic replacement under the
session save lock before creating the derived sidecar. Clear, Fork and recovery
bind target identity before publishing messages.

Recovery reuses a scoped
publication capability from the save owner and never reacquires the same lock.

Historical copies retain their binding with `archive_only`: readable, never
executable. Move, native export, cross-project adoption and physical rename
refuse bound sessions until they support constraint-preserving migration.

These boundaries govern participating readers and writers. An unaware old
program or external process can ignore every session and integration protocol;
a sidecar cannot prevent its direct file writes after a crash. Such downgrades
are outside safe compatibility.

Explicit Linux offline roots and local container verification on all hosts can
issue process-owned controlled receipts. Container verification does not change
Windows candidate Bash/hook mutation-bound requirements.

Ordinary observations remain unknown. Permission changes outside the Git apply protocol's
representation refuse publication and retain the result.

Candidate launch integrity is an execution requirement, separate from approval.
Unsupported full mutation bounds refuse before Windows ACL grants, hook launch
or Bash escape approval.

Source protection is checked against the final writable
surface, including private temporary storage.

This runtime-only policy adds no
persisted configuration migration and does not upgrade a creation-only token to
a complete integrity claim.

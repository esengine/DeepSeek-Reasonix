---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-28
---

# Best-of result ownership and recovery / Best-of 结果归属与恢复

## Behavior / 行为

`best_of_n` creates candidates through the session's isolation Store. Every
candidate keeps its original `iso_` ID through execution, judging, publication,
cleanup and restart. A new process reads the existing record; it does not adopt
a raw path under a newly minted ID.

`best_of_n` 通过会话的 isolation Store 创建候选。每份候选从执行、评选、发布、
清理到重启恢复均保留原 `iso_` ID；新进程读取原记录，不把任意路径重新收编成新 ID。

Both candidates use the accepted `Store.Prepare` baseline. The Store writes
creation intent before worktree materialization, then writes running state
before invoking a candidate. Normal return and successful inspection select an
immutable result image. A panic is caught on the worker goroutine itself.

候选共用既有 `Store.Prepare` 基线。Store 在创建 worktree 前记录意图，在调用候选
前记录运行状态。正常返回且检查成功后才选择不可变结果镜像；panic 在各 worker
自己的 goroutine 调用边界捕获。

A runner error or panic retains the group and prevents automatic judging and
publication. A judge failure also retains completed candidates. Reports name
the original IDs, including when no attempt finished. A failed or unknown
execution is not treated as an empty result.

运行错误或 panic 会保留该组候选，并阻止自动评选与发布；评选失败也保留已完成
候选。即使没有候选完成，返回信息仍包含原 ID。失败或状态未知不等于空结果。

## Persisted states / 持久化状态

| State | Meaning and permitted recovery / 含义与恢复 |
| --- | --- |
| `creating` | Intent precedes materialization; interruption is retained, never presumed no-launch. / 创建意图已记录；中断后保留，不事后猜测未启动。 |
| `prepared` | Materialization returned; candidate Runner has not started. Discard is permitted. / 创建已返回，候选未启动，可丢弃。 |
| `running` | Runner admission recorded; restart alone cannot complete it. / 已记录执行准入，重启不能推定完成。 |
| `unknown` | Error or panic without a completion proof; no apply, discard or expiry. / 错误或 panic 后范围未知，禁止应用、丢弃与过期清理。 |
| `ready` | Normal return and immutable inspected result; existing result controls may settle it. / 正常返回且结果已固定，可由既有结果控制处理。 |
| `applying` | Publication intent precedes source writes; interrupted publication requires explicit same-ID retry. / 发布意图先于源写入，中断后按原 ID 显式重试。 |
| `cleanup` | Applied/discarded disposition is committed; retry only removes the candidate. / 应用或丢弃决定已提交，重试只清理候选。 |
| `removed` | Stable tombstone; repeating the same operation is harmless, the opposite disposition is refused. / 保留稳定终态，同类操作幂等，拒绝相反操作。 |

The journal is outside the removable worktree, in the existing settlement-fence
namespace. Immutable identity/result images have SHA-256 revisions; mutable
progress selects those revisions and the original ID. A damaged image prevents
settlement while its original ID remains available for diagnosis.

日志位于可删除 worktree 之外，使用既有 settlement fence 命名空间。不可变身份与
结果镜像带 SHA-256 revision，可变进度引用 revision 和原 ID。镜像损坏会阻止处理，
但保留可诊断的原 ID。

Files are synced before publication; images are published without replacement
using a hard link, and progress uses rooted rename. Unsupported filesystems
fail closed. Directory sync is best effort where the platform does not support
it; this is not a universal sudden-power-loss guarantee.

文件发布前先同步，镜像通过硬链接以不可覆盖方式发布，进度通过 rooted rename
切换。不支持这些能力的文件系统拒绝执行；平台不支持目录同步时仅尽力同步，
不声称任意存储设备上的突然掉电保证。

## Publication and cleanup / 发布与清理

The first automatic winner publication keeps the existing whole-workspace
comparison. Explicit recovery uses the existing per-path comparison: a path
must still match the original baseline or already match the immutable target.
The user's index is not staged.

首次自动发布赢家仍执行原有整工作区比较。显式恢复沿用逐路径比较：路径必须仍是
原基线，或已等于不可变目标。用户的 index 不会被暂存。

This is forward recovery, not a multi-file atomic transaction or automatic
rollback. If publication fails after some writes, the ID remains `applying`;
discard and age-based cleanup are refused. Another external edit can still
make recovery conflict, which preserves the record instead of overwriting it.

这是向前恢复，不是多文件原子事务或自动回滚。部分写入后失败会保留 `applying`，
拒绝丢弃与按时间清理；外部再次修改仍可能引发恢复冲突，此时保留记录而非覆盖。

Cleanup is recorded before removing the worktree. Removal errors keep the ID
and committed disposition. A restart can retry even after the worktree was
removed but its Git administrative entry remains, subject to source and exact
backlink validation. An applied result is never republished just to retry cleanup.

The immutable identity includes directory device/inode identities on supported
Unix platforms and volume/file-index identities on Windows. Partial directory
cleanup requires those original identities; a recreated directory is refused.
These checks do not establish ACL, ownership, or adversarial physical quiescence.

删除 worktree 前先记录清理状态。删除失败保留原 ID 和已提交决定。即使 worktree
已删除但 Git 管理项尚存，重启后仍可在核对来源及精确回链后重试；清理重试不会
重新发布已应用结果。

不可变身份包含受支持 Unix 的设备号/inode 及 Windows 的卷号/file index；部分
目录清理必须匹配原身份，重建的目录会被拒绝。这不等于 ACL、所有者或对抗性
并发写入场景下的物理静默证明。

Sweep retries committed cleanup and applies the existing retention period to
unstarted/completed candidates. It never publishes source changes or expires
creating/running/unknown/applying records. Tombstones and immutable journal
images remain; their coordinated garbage collection is a separate protocol.

Sweep 重试已提交清理，并对未启动或已完成候选使用既有保留期限；它不发布源修改，
不清理 creating/running/unknown/applying。终态及不可变日志镜像继续保留，其协调式
垃圾回收属于另一个协议，不能直接按文件年龄删除。

## Compatibility and authority / 兼容与权限

1. Existing legacy isolated results remain readable and use their existing
   settlement behavior. New records use `candidate-run-v1`; unknown kinds or
   phases are preserved and refused rather than reinterpreted.
2. Readers with the accepted settlement fence refuse new records. Downgrading
   may require returning to a compatible reader; it does not mint replacement IDs.
3. Best-of and ordinary isolation share one Store, but tool exposure keeps its
   existing setting. Best-of alone does not add apply/discard tools. Completed
   results can be recovered after enabling the existing worktree-isolation tools.
4. No new tool schema, permission, stable prompt-prefix state or default is added.
   Old unmanaged best-of directories lack Store IDs and are not silently adopted.

The compatibility floor is a writer/reader that honors the settlement fence.
Older writers that ignore it cannot be made safe by this reader alone. Image
digests detect corruption; they are not signatures or protection against a
party authorized to replace the entire managed journal.

1. 既有隔离结果仍可读取，沿用原处理行为；新记录使用 `candidate-run-v1`，未知 kind
   或阶段原样保留并拒绝，不重新解释。
2. 已接受 settlement fence 的旧 reader 会拒绝新记录；降级后可能需回到兼容版本，
   不生成替代 ID。
3. Best-of 与普通隔离共用 Store，但工具曝光仍由原设置决定。仅启用 best-of 不会
   增加 apply/discard；启用既有 worktree isolation 后可处理已完成结果。
4. 不新增工具 schema、权限、稳定前缀状态或默认配置。旧的无 Store ID 的 best-of
   目录不被静默收编。

兼容下限是遵守 settlement fence 的 reader/writer；本 reader 无法单方面保证更早
且忽略隔离标记的 writer 安全。镜像摘要检测损坏，不是签名，也不防御有权整体
替换 managed journal 的主体。

## Evidence boundaries / 证据边界

Tests cover real candidate writes, worker panic, model-visible original IDs,
Boot rebuild and original-ID apply, actual child-process exit, locked-worktree
cleanup failure, partial-removal retry, interrupted-publication state, corrupt
images, unknown versions, empty results and legacy settlement fences.

测试覆盖真实候选写入、worker panic、模型可见原 ID、Boot 重建后按原 ID 应用、
真实子进程退出、锁定 worktree 导致的清理失败、部分删除重试、发布中断状态、
镜像损坏、未知版本、空结果及旧 settlement fence。

Normal Runner return is not proof of arbitrary descendant or remote-scope
quiescence. Unknown scope remains retained until a compatible host can establish
completion; neither process exit nor an unlocked file lock supplies that proof.

This change does not claim full OS identity, rooted native-metadata publication,
noncooperating-writer safety, universal restart recovery or completion of #14025.

Runner 正常返回不证明任意后代或远端范围已静默。未知范围保留到兼容 host 能证明
完成；进程退出和文件锁可获取都不能代替该证明。本改动不声称完整 OS 身份、原生
元数据发布、非协作写入者安全、通用重启恢复或整个 #14025 已完成。

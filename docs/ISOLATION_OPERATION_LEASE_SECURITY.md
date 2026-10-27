---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Result operation leases / 隔离结果操作租约

## Cause and repair

Reinspection fixes stale cached authority, not overlapping operations. Two
Stores can inspect the same published result before either applies or removes
it. Inspection can itself delete an empty result; Finish, Abort and age cleanup
also remove candidates. These paths need one operation owner.

All result inspection and settlement now use one OS lease per managed root and
normalized result ID. The lease covers reinspection, mutation and synchronous
cleanup. Store map mutexes do not cover OS waits, Git commands or file I/O.

The lock lives outside the removable candidate, under `.result-locks-v1`.
Its filename is SHA-256 of the trimmed opaque ID. Cleanup never unlinks the lock
file: recreating that pathname could create a second mutex inode while an older
holder still owns the first. Releases are idempotent and close only their file.

## Authority and data flow

| Boundary | Contract |
| --- | --- |
| Public ID | Trimmed as before; hashed before use as a lock filename |
| Allocation | Unknown queries cannot allocate arbitrary permanent keys; cache, publication or existing key only permits admission |
| Authorization | A known key is not result authority; rooted record/candidate inspection runs again while leased |
| Host namespace | Open managed directory, validate real child directory, open child handle, then open lock relative to that handle |
| OS ownership | Existing flock/LockFileEx implementation, shared with legacy path locks on the same physical file |
| Cancellation | Bounds acquisition, with a second context check before operation entry; no forced unlock |
| State mutation | Existing inspector, apply, publication and removal owners; no new decoder or shell command |

The utility adds `AcquireRoot` and `TryAcquireRoot` for an `os.Root` plus a local
name. They reject nonlocal names and nonregular lock files, preserve file bytes
and modes, and never reopen a filename by concatenating `Root.Name()`.

Relative child-directory opens follow the opened directory after a rename.
Windows may forbid renaming a directory opened directly with os.OpenRoot;
the result owner uses a child handle. Outside symlinks are refused by os.Root.

Closing the directory handle does not release an admitted file lease.
Existing path APIs keep their global-path and wait-budget contracts.

## Lifecycle coverage

| Entry | Lease boundary |
| --- | --- |
| Paths and path declaration | Context-aware inspection, including empty-result removal |
| Apply and Discard | Reinspect after admission; hold through source mutation and cleanup |
| Finish | Hold through inspection and publication or admitted failure retention |
| Abort | Hold through cleanup; return admission error instead of deleting through another owner |
| SweepExpired | Try admission; skip busy IDs; re-read that rooted record and age before removal |
| Pending | Read-only publication discovery; never authorizes settlement |

Public path queries have an additive context method; the existing method remains
a wrapper. `Abort` now returns an admission error; existing statement callers
remain valid. No tool definition, provider prefix, permission grant, config,
result schema, inspection version, wire payload or default strategy changes.

The wait limit is 30 seconds or the caller's shorter budget. Timeout only abandons
the waiter. A holder doing real I/O keeps its lease until completion or process
death. FIFO fairness and model-capacity separation are not added here.

If first Finish cannot obtain operation admission, it keeps the Entry in its
live Store and reports that durability is unavailable. It does not publish an
inspection marker without owning the lease. A retry can publish the same ID.
This is an explicit live-only limit, not durable Begin or crash recovery.

## Compatibility and limits

| Surface | Behavior |
| --- | --- |
| Existing result/inspection records | Same schemas, identity checks and unknown-field preservation |
| New lock files | Empty advisory files in a versioned namespace; no serialized state or automatic deletion |
| Previous readers | No `entry.json` in the lock namespace, so legacy result discovery does not offer it |
| Previous writers | Versions without this result protocol do not honor these leases; coexistence is not fenced |
| Existing OS lock API | Same-file rooted/path acquisitions exclude one another |

Future protocols must preserve the mutex identity or establish a quiescent
migration boundary. Changing the namespace while old writers run is unsafe.
The lock key contains no interpreted recovery state; it never substitutes for
the result's publication, baseline, target or permission.

This is advisory coordination for writers using this protocol. `os.Root` does
not exclude hardlinks, bind mounts or arbitrary external mutation. A hardlink
to the same lock inode shares the OS owner; namespace access is not a sandbox.

Begin/running identity, immutable outcomes, reliable snapshot bytes, safe
retention, shared source-file transactions, multi-file journals and conditional
rollback remain separate work.

Existing age policy is unchanged; an unlocked
expired legacy result can still be deleted. Candidate removal can fail after
publication removal; this lease does not make that sequence crash atomic.

Different result IDs have distinct leases. They do not serialize shared source
paths or Git common metadata: existing workspace claims and Git rules still
apply. No complete integration or fresh verification receipt is promised.

## Evidence and reachability

Tests hold the operation owner, start an uncancelled real Apply, and force its
parked state before cancelling. Predecessor owners compile and write the source
or delete candidates despite that owner. Discard, path/empty inspection, Abort,
sweep, first-Finish publication and canonical ID cleanup have sibling controls.

A test Git executable delegates to the actual Git binary. A local socket pauses
real restore and worktree-removal command boundaries. At each pause the Store
must still own the OS lease and a peer must not delete its candidate.

That executable shim and its environment are test-only. Production keeps the
existing hardened Git launcher; no model input selects a new program or socket.
The helpers access disposable fixtures, not user repositories or services.

Real subprocess tests confirm exclusion and physical release on normal exit or
termination. Tests also cover disjoint progress, repeated releases, preserved
mutex inode, unknown-ID allocation bounds, malformed namespace preservation,
directory rename, aliases, cancellation and compatible path locks.

Holding an explicit operation fixture proves the protocol contract given an
active owner. The command-boundary tests additionally prove actual Store callers
hold it during work. These are not claims of an observed production incident.

Local author tests/security analysis, hosted native checks, independent review
and full-series acceptance remain distinct gates.

## 中文说明

缓存重新检查不能阻止两个 Store 同时操作一个结果。现在查询中的空结果清理、
Apply、Discard、Finish、Abort 和过期清理共用按结果 ID 的跨进程 OS 租约。
租约覆盖实际检查、变更和同步清理；取消或超时只撤回等待者，不抢占正在执行的持有者。

锁文件位于候选目录外的版本化命名空间，不随结果删除，避免路径被重建成第二个
mutex inode。通过宿主打开的目录句柄创建锁，拒绝越界符号链接和非普通文件。
未知 ID 查询不会无限创建永久锁；文件存在仅表示可以申请，不能授权应用结果。

首次 Finish 无法准入时只承诺当前 Store 的内存重试，并明确说明未持久化；不在
别人的租约期间发布记录。已准入的检查失败仍使用既有 inspectionVersion1 保留机制。

旧结果格式、权限、工具定义和默认方式不变。旧版隔离写入者不认识新租约，不能
宣称已经约束其并发。过期删除政策、Begin 持久化、字节快照、跨文件日志、条件回滚、
验证证据与自动整合仍未完成；锁文件也不是沙箱或恢复日志。

回归包含真实 Apply 排队、进程持有与终止，以及实际 Git restore/remove 边界的暂停。
前一版所有者在编译成功后发生真实写入或删除。目录清理断言检查结果、发布记录与
工作副本被清除，同时允许保留协议需要的锁目录；没有通过删除活动锁来满足旧断言。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Background writer admission / 后台写入者准入

## Cause and repair

`MutationObserver.RegisterWriter` held the shared parent/child registry mutex
while waiting for checkpoint admission, retiring undo on disk and notifying the
undo owner. Unregistration also persisted the checkpoint under that mutex.
Registry queries and unrelated writer retirement could block behind these steps.

Task and fleet background registration used the context-free method. A cancelled
call could remain waiting behind an exclusive checkpoint operation. Changing only
the barrier did not pass cancellation through these three registration sites.

The registry now reserves an identified pending registration under its mutex and
prepares outside it. Pending entries do not count as active writers.

Publication
uses a separate serialization mutex, takes the latest registry snapshot briefly,
then releases the registry mutex before calling the existing checkpoint Store.
This prevents an older snapshot from overtaking a newer removal.

The context method returns cleanup bound to that registration's identity. Task
and fleet transfer this cleanup to the background job. Cancellation bounds
admission; it does not revoke a successfully handed-off job's writer permit.
Repeated or old cleanup cannot remove a replacement with the same string ID.

## Source, sink and authority

| Boundary | Decision and guarantee |
| --- | --- |
| Source | Existing host execution ID, writer kind, turn and caller context |
| Pending reservation | One token per ID; duplicate callers await that token |
| Preparation | Existing barrier and durable undo retirement, outside registry mutex |
| Publication | Existing Store active-writer projection; latest snapshots serialized |
| Handoff | Task/fleet job owns the returned identified cleanup |
| Retirement | Remove only the expected token, cancel its preparation, release its permit |

Cancelled followers do not cancel their leader. A retired pending registration
cannot resurrect after preparation returns. Panic cleanup releases the acquired
barrier permit, closes duplicate callers' rendezvous and rethrows the panic.
Undo already retired durably remains retired after cancellation or panic.

The undo notification callback is an existing host callback, not new untrusted
executable input. A callback that queries the writer registry is a public-API
regression fixture; no production callback deadlock is asserted from that fixture.
The production controller callback emits `RewindChanged`.

## Compatibility and limits

Legacy registration remains context-free. Legacy `UnregisterWriter(id)` retains
its current-ID meaning; new production callers use identified cleanup instead.
Duplicates share a registration rather than owning reference-counted permits,
as before.

This change introduces no persisted format, cross-process protocol,
path authority, permission grant, tool schema, provider prefix or default policy.

Synchronous Store I/O and ordinary mutex acquisition are not context-abortable.
Cancellation is checked around preparation; queries of the registry stay available
while Store I/O is blocked. Publication can still serialize behind slow disk I/O.
Store persistence remains best effort under its existing contract.

The registry and checkpoint barrier are in-process rewind coordination. This is
not file-level mutual exclusion, process-scope completion, automatic isolation,
result integration, recovery journaling or root-budget enforcement. Asynchronous
hooks and processes require their own physical-lifetime integration.

The accepted [job finalization owner](https://github.com/esengine/DeepSeek-Reasonix/pull/14469)
may prove that a closure will not be invoked.

Task/fleet register returned writer-token cleanup with that owner alongside
transcript/progress cleanup, instead of a closure-only defer or ID unregistration.

Cleanup runs on actual closure return or proven no-launch, before completion
publication. This is not proof of descendant process-scope completion.

## Verification boundaries

Deterministic tests cover parked exclusive admission, cancellation, duplicate
followers, pending retirement, replacement generations, panic and durable undo.
A Store-mutex fixture parks publication and checks that registry access continues;
its watchdog detects a stuck fixture, not a product latency requirement.

Real task/fleet calls exercise cancelled background admission. Real `boot.Build`
tests cover direct and dynamically resolved task/fleet handoff, a completed parent
turn while children remain live, and final job cleanup.

The fixture installs the
shared observer through the public Agent setter; it does not claim a concurrent
default Controller rewind incident.

Compiling preceding-owner overlays reproduce four observer failures; preceding
task/fleet callers reproduce two cancellation failures plus their parent test.

The observer overlay adds only a context-shaped adapter to legacy registration
and ID-only cleanup; it does not assert that the old API offered identified cleanup.

The combined job-owner regression checks both ordinary writer release and a
same-ID replacement admitted before the old job finishes. Its finalizer must
release only its original token.

Accepted no-launch tests and checkpoint
admission tests remain part of the combined validation, including real boot
entry points. These do not establish unknown process-scope termination.

Local race and root results, hosted CI, native platforms and independent review
are reported separately in the PR.

## 中文说明

原登记流程持有父子代理共享的登记表锁等待检查点、持久化撤销状态并调用通知；
移除登记时也在该锁内持久化，导致查询及其他写入者的清理一起等待。
task/fleet 三个生产入口没有把调用上下文交给后台写入者准入。

新流程先在锁内创建带身份的待准入记录，再在锁外等待及准备；待准入记录不计入
活动写入者。单独的发布锁串行提交最新快照，登记表锁不覆盖 Store I/O。
成功后返回绑定本次身份的清理回调，并转交后台任务；旧回调不能释放同 ID 的新登记。

取消只终止准入等待，不撤销已经交接的后台执行。取消跟随者不影响首个申请，
待准入记录被移除后不能复活；panic 关闭等待出口并释放本次许可，继续传播原 panic。
已经持久化的撤销退休状态不会恢复成可撤销。

旧接口保留原语义，没有变更权限、持久格式、工具定义、稳定提示前缀或默认策略。
磁盘 I/O 和普通互斥锁本身仍不可取消；这是进程内的 rewind 协调，不能替代跨进程文件
事务、进程物理完成、自动隔离或恢复日志。原生平台、CI 与独立审查以各自实际证据为准。

已接受的 #14469 统一实际闭包返回与已证明不启动时的资源终结。本分支把登记 token 的
清理回调交给这个所有者，与进度和 transcript 资源一起在完成发布前终结，不再只靠闭包
defer，也不退回按字符串 ID 移除。组合回归覆盖普通释放及旧任务结束前同 ID 新登记；
旧 finalizer 不能释放新 token。

这仍不证明后代进程 scope 完成或所有后台生命周期闭合。

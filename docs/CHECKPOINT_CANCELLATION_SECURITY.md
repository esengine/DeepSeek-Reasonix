---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Checkpoint cancellation / 检查点准入取消

## Cause and owner

The checkpoint barrier waited on a condition variable without the invocation's
context. Cancellation could leave a call waiting behind an exclusive checkpoint
operation, then allow it to enter tool-use hooks or execution after that owner
released. The Agent could not finish its deferred admission cleanup until then.

The shared barrier now accepts a context for writer and exclusive admission.
Cancellation wakes condition waiters under the same mutex used for admission;
the loop checks cancellation before changing ownership. A cancelled waiter does
not decrement active writers, release exclusive access or advance generation.

Existing context-free methods retain their background-context behavior.

`MutationObserver.BeginWriteContext` uses this admission, retains the existing
durable undo retirement, and checks cancellation again after preparation. A
successful permit has an idempotent release closure: repeated cleanup cannot
release a second writer.

The Agent passes its current invocation context after
real target resolution and permission, before synchronous hooks or execution.

## Authority and data flow

| Boundary | Input and decision |
| --- | --- |
| Target and permission | Existing tool/proxy resolution and permission gates; unchanged grants |
| Admission | Invocation context plus current barrier ownership; cancellation only abandons this request |
| Undo preparation | Existing checkpoint Store and transaction persistence; completed retirement stays retired |
| User code | Starts only after preparation returns a live permit; tool `Executed` is not forged |
| Cleanup | Existing invocation defer and one permit's idempotent release |

The context callback only broadcasts the condition variable. It cannot grant
admission, change another caller's ownership or interrupt an active writer.

No file path, command, shell launcher, approval bypass or arbitrary callback is
introduced into the barrier. No persisted format, cross-process claim, wire
field, tool schema, provider prefix, permission setting or default changes.

## Compatibility and limits

The barrier coordinates a Store's writers against rewind. Concurrent writers
remain supported; this is not file-level exclusion or a cross-process lease.

Existing nonblocking `TryEnter*` calls keep their behavior. Production rewind
uses these nonblocking methods; the new exclusive context method does not turn
rewind into a queued operation or change its busy refusal.

Cancellation governs admission, not a revocable execution permit. Once user
code starts, its own cancellation and physical completion rules still apply.

Undo persistence and ordinary mutex acquisition are not interrupted. If
cancellation occurs during durable preparation, the completed retirement is
preserved, this permit is released, and the call stops before user code.

Old undo is not restored after another action may have invalidated it.

Background task/fleet callers use identified, cancellable registration outside
the registry mutex; see [background writer admission](CHECKPOINT_WRITER_ADMISSION_SECURITY.md).
The legacy context-free method remains available to existing callers.

An asynchronous hook
can outlive its synchronous callback; this slice does not register or reconcile
that process.

No FIFO, bounded wait, model-capacity separation, complete effect
receipt, safe isolated integration or whole-series completion is claimed.

## Reachability and validation

Deterministic Agent tests hold an exclusive barrier, park direct and deferred
writer/reader-hook calls, cancel, then require completion while the owner still
holds access. They verify no callback/tool entry or changed user bytes, and no
leaked writer. Original owners compile and fail these behavioral assertions.

A live actor's aborted checkpoint wait must also release its provisional
workspace paths so a second Owner can acquire them before the actor ends.

Real `boot.Build` tests retain built-in tools, capability resolution and shell
hooks. The fixture injects a Store-bound observer through the public Agent
binding and cancels at the permission boundary.

Original reader-hook cases
remain blocked and attempt hook entry after cleanup releases exclusive access;
the shell launcher itself refuses that cancelled command.

Built-in writer
cases are controls: the existing workspace lease already refuses them first.

This is an assembly regression with an explicit collaborator, not evidence
that production rewind deliberately holds the barrier indefinitely.

Barrier tests cover cancelled/free admission, queued writer/exclusive waits,
deadline expiry, shutdown, successful legacy/context waiters and preservation
of another owner and generation. Fake time and `synctest` establish ordering;
the boot watchdog only detects a stuck call, not a performance threshold.

Public checkpoint APIs create a committed undo offer. Cancelling before
admission preserves it across reload. Cancelling through the Store's undo-change
notification after retirement keeps it retired across reload. Repeated permit
cleanup cannot admit rewind while a second writer remains.

These fixtures
exercise owner contracts rather than asserting an observed production incident.

Local validation and authored security analysis do not replace exact-head
hosted platform checks or independent review.

## 中文说明

旧检查点屏障不接收调用 context，取消无法唤醒等待者；独占方稍后退出后，旧调用
仍可能进入 hook 或工具。现在等待、取消和准入由同一屏障所有者协调，取消只撤回
本次请求，不会释放别人的许可或推进版本。Agent 在实际目标解析和权限检查之后，
将调用 context 传给检查点准入，成功许可仍由调用退出路径释放。

准入前取消保留既有 undo；持久化退役期间发生取消，则保留已经完成的退役并退出，
不恢复可能已失效的旧 undo。重复释放同一许可不会减少其他写入者的计数。

普通上下文无关 API、非阻塞 rewind、并行写入、权限、持久化格式和工具定义保持
既有约定。本次不强制打断进行中的文件 I/O，不解决后台登记锁、异步 hook 进程、
公平队列、跨进程文件事务、验证证据或整个隔离恢复生命周期。

真实装配测试明确注入检查点 Store；旧读取 hook 路径会阻塞并尝试进入回调，但实际
shell 启动器会拒绝已取消的命令，不能把它描述成已运行 shell 写入。写工具对照由
现有工作区租约先行拒绝。确定性交错、磁盘重载、本地 race 和后续 CI 分开记账。

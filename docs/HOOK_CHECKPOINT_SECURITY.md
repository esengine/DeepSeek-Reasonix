---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Hook checkpoint admission / Hook 检查点写入准入

## Root cause and repair

A reader's tool-use hooks can write files. Checkpoint admission previously used
only the concrete tool's read-only classification, so these callbacks bypassed
the rewind barrier and omitted the explicit hook coverage gap.

When an observer is installed, the invocation requires checkpoint write
admission if either the tool or its hook set may write. Admission stays before
callbacks. The permit spans synchronous callbacks, execution and failure cleanup.

An admitted hook set records `GapHookWrite` even around a reader. This reports
unknown coverage; it does not invent file preimages or make an incomplete
checkpoint complete. Tool execution and read-only classification are unchanged.

## Authority and data flow

| Boundary | Contract |
| --- | --- |
| Target access | Existing permission and target resolution finish before checkpoint admission |
| Hook capability | Existing optional capability report; legacy custom hooks remain conservatively write-capable |
| Checkpoint owner | `MutationObserver.BeginWriteContext`: enter rewind barrier, durably retire undo, then return permit |
| Failure | Closed barrier or failed undo retirement refuses the call before hook/tool entry |
| Cleanup | Existing invocation defer releases the permit on success, refusal or failure |
| Coverage | Existing `GapHookWrite`, never a fabricated path or captured preimage |

No new hook launch path, permission grant, shell program, dependency, persisted
field, wire schema, tool definition or configuration default is introduced.
The repaired caller uses the same checkpoint owner as an ordinary writer.

Empty and non-tool hook runners do not acquire a reader's write permit or
retire undo. An enabled tool-hook set can conservatively require admission
even if no individual command matches this invocation.

## Concurrency and compatibility bounds

The checkpoint barrier excludes rewind; it does not serialize all writers or
replace file-level, cross-process mutation coordination. Readers with possible
hook writes now follow the existing writer wait/shutdown contract.

Invocation admission accepts its context; cancellation can abandon a wait
without releasing the exclusive owner. See [cancellation bounds](CHECKPOINT_CANCELLATION_SECURITY.md).
This does not add a fair queue, global execution mutex or filesystem scan.

Imported asynchronous hooks can outlive their callback. Their process lifetime
is not covered by this permit; registering and reconciling those background
writers remains a separate lifecycle requirement.

Old records and formats are unchanged. `GapHookWrite` already has supported
readers; its presence now accurately includes reader-side hooks. A checkpoint
can therefore advertise incomplete coverage where it previously omitted it.

This slice is independent of #14397's provisional-lease repair and #14398's
optional host invalidation callback. It does not finish delivery-receipt or
verification-evidence accounting, capture arbitrary hook writes, or sandbox
the approved program.

## Validation and reachability

Tests construct an Agent with the same hooks/observer wiring used by boot.
The callback writes real bytes and attempts a nonblocking exclusive entry:
it must hold a writer permit, and rewind must be refused until it exits.

Pre-success, pre-refusal, post-success and post-failure paths cover direct and
deferred reader targets. Negative cases cover closed admission and a deliberate
durable-retirement I/O fault, plus empty/non-tool hook runners.

Public checkpoint APIs create a real committed rewind/undo offer. The callback
must see it retired before entry, and reloading must not revive it. Fault fixtures
model unavailable storage; they do not claim an observed production disk fault.

The undo fixture enters the Agent invocation directly to test that contract.
Ordinary visible Controller turns already retire undo when opening a checkpoint;
the fixture is not evidence that such a turn normally retains an undo offer.

Real `boot.Build` tests run a helper process that writes another file around a
read, and inspect the actual controller checkpoint's coverage. A compiling
original-owner overlay must fail these assertions. Author tests do not replace
independent review or exact-head native CI.

## 中文说明

只读工具不代表整个调用只读。原接线按工具分类跳过 checkpoint 写屏障，导致周围
可能写文件的 hook 未参与 rewind 互斥，也未记入覆盖缺口。

现在工具或 hook 任一可能写入，都先通过已有写入准入：进入屏障并持久化退役旧的
undo，再允许回调执行。准入失败则 hook 和工具都不执行；调用退出后释放许可。

hook 的未知写入只记录既有覆盖缺口，不伪造预映像或修改工具 `Executed`。无工具
hook 的读取保持原行为。权限、工具定义、配置和持久化格式不变。

该屏障只保护 checkpoint 与 rewind，不是完整文件事务或跨进程锁；调用等待支持
取消，但后台登记、验证证据、自动隔离与恢复仍需后续实现。测试的存储故障是明确故障注入，
不冒称已在线上发生。

导入的异步 hook 进程可能晚于回调结束，当前许可不覆盖它的完整运行期；这类后台
写入的登记和恢复仍是后续生命周期工作。

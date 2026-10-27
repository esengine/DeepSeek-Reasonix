---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Job resource finalization / 后台任务资源终结

See [job panic results](JOB_PANIC_RESULT_SECURITY.md) for legacy nil-panic and
typed-cause handling at invocation and finalization boundaries.

执行与清理边界的 legacy nil panic 和错误身份处理，另见上述说明。

## Cause and owner

Task and fleet prepared checkpoint writer registrations before calling the job
Manager, then released them only inside the supplied job closure. Task also put
its transcript release there; fleet handed its progress merger to that closure.

The Manager can prove no closure will run: invalid session/kind identifiers take
`startInvalid`, or the root context is closed before `launchJob`. Closure-only
defers therefore missed those paths. Public task/fleet fixtures reproduce both
refusals without invoking a child provider; no production incident is asserted.

The Manager now owns optional host resource finalizers at its actual execution
boundary. It invokes them after a managed closure returns, including errors or
panic, or after proving that closure will not be invoked. It finishes cleanup
before closing the job completion channel or publishing completion to its observer.

## Inputs, decisions and sinks

| Boundary | Authority and meaning |
| --- | --- |
| Registration | Host code supplies `WithFinalizer`; no model field or tool argument installs callbacks |
| `Finalization.Invoked` | Manager called the managed closure, or proved it would not; not a process-scope receipt |
| `Finalization.Err` | Original closure error/panic result, or the Manager's no-launch cause |
| Cleanup | Once per job, without Manager/Job mutexes; every callback is attempted |
| Failure | `ErrFinalization` preserves callback error identities; panic becomes a cleanup error |
| Completion | Cleanup failures join the original error and invalidate artifact-complete claims |

Repeated internal finalization returns the same terminal cleanup error. Callback
errors and panic do not skip sibling resources or leave completion unclosed.
Callbacks may query the Manager; they must not synchronously wait for their own
job to complete. Cleanup itself can block, and the job remains unfinished then.

Existing logical cancellation can publish a cancelled status before the closure
returns. That status does not authorize finalization: the Manager retains the
closure completion channel and waits for actual return. A bounded Close timeout
likewise does not finalize a still-live closure.

## Task and fleet integration

The background task's finalizers settle a never-invoked delegation/progress card,
save its existing failed/cancelled transcript status, release its transcript and
retire its checkpoint writer. Normal execution keeps its existing result handling.

Fleet finalizers publish a never-invoked group's terminal progress, close the
progress merger and retire its checkpoint reservation. The real job manager owns
these releases even when it declines launch; caller status/list heuristics do not.

This slice uses the accepted registry API. The separate pending admission
and token-cleanup change is [#14464](https://github.com/esengine/DeepSeek-Reasonix/pull/14464).

Adapting the finalizer to that returned token is a separate composition step;
this slice does not retroactively give legacy ID-only cleanup generation safety.

## Compatibility and limits

The new callbacks and invocation receipt are process-local Go values. No persisted
schema, cross-process record, RPC field, tool definition, provider prefix,
permission grant or default policy changes. Existing artifact metadata records
the original status/error fields; no callback or authority is recovered from disk.

Existing callers without finalizers keep their execution and error contracts.
The background tool's existing start-response shape and status values remain;
completion results provide the terminal outcome when a Manager declines launch.

This is managed-closure lifetime, not root command Wait, descendant/sandbox scope
quiescence, durable crash recovery, automatic isolation/integration or root-budget
enforcement.

A process that outlives its managed closure needs its own completion
protocol before its physical resources can safely use this finalizer boundary.

Arbitrary host StartOptions and lifecycle sinks remain trusted Go code under
their existing contracts. This change contains finalizer panic; it does not claim
to make arbitrary event sinks or startup option functions panic-safe.

## Verification

Manager tests cover successful/error/panic closure return, invalid identifiers,
an already closed Manager, Close between publication and launch, repeated and
concurrent cleanup, sibling cleanup after errors/panic, lock-free callback queries,
completion ordering and logical cancellation before actual closure return.

Real task/fleet tests assert no child request, no stranded writer/transcript lock,
one terminal progress event and saved cancelled/failed metadata.

Real `boot.Build`
tests keep direct and dynamic capability dispatch. They close only the public job
Manager after the parent turn is bound, not a simulated full Controller shutdown.

An existing liveness test assumed a 30ms sleep always resumes before its 60ms
warning threshold. A loaded local race run disproved that assumption; the warning
correctly described the observed quiet interval. The progress and silence controls
now run on a controlled clock without changing production thresholds.

The deterministic pair is repeated under race, and a compiling owner overlay
that stops refreshing activity fails the active-progress test while preserving
the real-silence control. Final package inventories and platform/review evidence
are reported in the PR; author analysis is not independent approval.

Windows PR CI runs complete jobs/delegation packages and retains a JSON inventory
requiring both terminal package passes and fifteen new portable case passes.
Pure delegation changes also select native platform legs. Linux PR race coverage
includes delegation. Workflow configuration is not native execution evidence.

## 中文说明

原 task/fleet 在交给任务管理器前创建检查点登记，但只在任务闭包中清理资源。
非法标识或管理器已经关闭时，Manager 不调用该闭包，导致写入者、transcript 锁或
进度合并器失去清理出口。回归通过公开接口复现，未声称已观察到生产事故。

新流程在 Manager 所有者层登记宿主终结回调，确认闭包不会启动，或闭包实际返回后，
才执行一次性清理；完成通道关闭和完成观察者通知都在清理之后。回调不占住 Manager/
Job 互斥锁，错误与 panic 保留诊断且不跳过其他资源。

逻辑取消、状态变更或 Close 的等待超时不等于闭包已返回，不能据此提前释放资源。
未启动的 task/fleet 也会闭合进度终态、保存既有取消/失败状态并释放所持资源。

本批没有修改持久格式、权限、工具定义、稳定提示前缀或默认策略，不提供后代进程
结束、崩溃恢复、自动整合或根预算保证。#14464 的登记代次接口仍按其独立审查结果适配。
受控时钟只修正测试的调度假设，没有提高产品告警阈值；原生平台、CI 和独立审查分别验收。

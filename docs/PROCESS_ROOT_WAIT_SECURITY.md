---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Root wait observation / 根进程等待观察

## Cause and scope / 根因与范围

Tracked foreground execution deliberately returns after a cancellation grace
period even when `cmd.Wait` has not returned. That bounded result is useful for
responsive cancellation; it cannot also prove physical completion.

`TrackedCommand.RootWaitCompleted` exposes only the registered root wait. Its
channel closes after the wait function returns and before its result is sent.
Cancellation and cleanup-retry expiry do not close it. No production lease,
tool, hook or resource consumer uses the new method in this unit.

前台取消采用有界返回以保持响应，但返回时 `cmd.Wait` 仍可能未完成。本次保留
这一行为，新增明确命名的根等待完成信号，不把调用返回等同于物理执行结束。

## Source, sink and authority / 输入、副作用与权威

- The existing tracked-command owner registers a channel before starting its
  wait goroutine. Access to channel identity uses its existing mutex.
- The goroutine closes the channel only after `wait()` returns. It closes
  before publishing the buffered result, so receiving the result cannot race
  ahead of notification. A blocked result receiver cannot delay notification.
- The buffered result remains available to the existing foreground or retry
  consumer. The signal adds no goroutine, timer, command, path or OS handle.
- Root completion preserves success, exit errors and cancellation error
  identities. Existing launch, tracking, kill and retry policies stay unchanged.
- Nil handles and handles without a registered wait return nil. Nil is not
  completion evidence or permission to release a resource.

信号身份沿现有 mutex 发布，在等待返回后、结果发送前关闭。既有取消、重试、
错误分类和进程操作保持原样，没有新命令、路径、句柄或后台 goroutine。

## Meaning of each boundary / 各边界的含义

| Observation | What it proves | What it does not prove |
| --- | --- | --- |
| Foreground cancellation returns | Grace policy returned control | Root wait or descendants ended |
| Retry window expires | Automatic retry budget ended | Root wait completed |
| RootWaitCompleted closes | Registered root `cmd.Wait` returned | Descendants or external scope ended |
| Method returns nil | No wait was registered on this handle | Command never ran or all effects ended |
| ProcessState exists | os/exec observed a root process result | Output copies or descendants ended |

A root wait may return an error and still be completed. Conversely a failed
launch can return a tracked handle without registering a wait. In particular,
Windows launch preparation may start and reap a suspended child before returning
an error. The nil signal makes no stronger claim about that path.

完成可以伴随错误；失败启动也可能产生未登记等待的句柄。Windows 的启动准备
可能先创建再回收挂起子进程，因此 nil 不能被解释为“肯定未执行”。

## Verification / 验证

- Deterministic synthetic-time tests keep Wait blocked through cancellation,
  grace expiry and retry expiry, then release it and observe the same channel.
- An unbuffered receiver control proves completion notification precedes result
  publication without depending on goroutine scheduling speed.
- Actual portable Go child commands cover success, nonzero exit, missing
  executable, cancellation before launch and unchanged untracked behavior.
- An actual child writes into a deliberately blocked output sink. Bounded
  cancellation returns while `cmd.Wait` remains pending. A test-only consumer
  binds the signal to an existing mutation lease and ends it logically.
- Conflicting admission remains busy, disjoint admission proceeds, and releasing
  the output sink lets the lease close. Repeating old cleanup does not release
  the next transaction. All fixture processes and waits are joined.

真实子进程测试固定阻塞输出处理，因此不靠时间快慢猜测 Wait 是否完成。将信号
接到租约的调用仅存在于回归测试，不代表运行时已启用写事务。

## Compatibility / 兼容性

| Contract | Before | After |
| --- | --- | --- |
| Tracked foreground return/error | Existing bounded cancellation | Unchanged |
| Untracked RunCommand | Nil tracked handle | Unchanged |
| Root wait observation method | Absent | Additive, local-only method |
| Diagnostics/wire/session records | Existing fields | Unchanged |
| OS tracking and sandbox policy | Existing backend behavior | Unchanged |

There is no new persisted field, migration, model-visible schema or prompt
prefix. This method has no preceding API implementation to compare. Deliberately
weakened completion ordering or cancellation closure controls validate the new
contract; they are not claims that an older API already promised this signal.

本次没有持久化或 wire 变更。新增接口此前不存在，故意削弱信号规则的对照用于
验证新测试，不能冒称旧版已经承诺这个接口却违反它。

## Limits and next integration / 边界与后续接入

Do not use this signal alone to release a lease for an unresolved shell or hook.
The command may spawn descendants, detach, delegate work to a service or return
`exec.ErrWaitDelay`. Root Wait is not a process-scope termination protocol.

Full write-lifetime integration must separately establish the backend's supported
scope, distinguish unknown termination, retain ownership when completion cannot
be proved, and provide attributable diagnostics and recovery. Closing every
channel is not a valid substitute for proving completion.

Until those boundaries are implemented, this unit does not activate shared
transactions, settle unknown scopes, recover process crashes or guarantee every
descendant is gone. Existing cancellation cleanup remains best-effort where the
backend is best-effort. Native platform evidence is recorded separately.

不能仅凭根等待完成就释放未知 shell/hook 写入租约。后代、脱离进程、服务委托和
输出超时都有独立边界。无法证明范围终止时必须保留占用并给出诊断及恢复路径，
不能为了关闭 channel 而伪造完成。本次不启用共享事务，也不完成未知范围恢复。

This is the author's security assessment, not independent review or disposition
of the original large PR's CodeQL findings.

本说明为作者评估，不替代独立安全审查或原大 PR 的告警逐项判断。

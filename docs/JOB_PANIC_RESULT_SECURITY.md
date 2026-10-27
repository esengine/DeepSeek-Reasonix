---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Job panic results / 任务 panic 结果

The delegated-run graph/transcript boundary is assessed separately in
[delegation panic outcomes](DELEGATION_PANIC_OUTCOME_SECURITY.md).
委派 run 的图与 transcript 边界另见上述说明。

## Cause and boundary / 原因与边界

The invocation and finalizer boundaries treated a non-nil `recover()` value as
the only evidence of panic. With explicit `GODEBUG=panicnil=1`, a recovered
`panic(nil)` returns nil.

An invocation or cleanup callback could then appear successful, and an ordinary
job could publish `Done`. Default nil-panic handling already produced an error.

Both boundaries now share a private normal-return guard. A panic is a failure
unless the callback actually returned, independent of the recovered payload.
Error-valued panic causes remain inspectable through `errors.Is`/`errors.As`.
Finalizer failures continue to include `ErrFinalization`.

任务执行与清理边界原来只根据 `recover()` 是否非 nil 判断 panic。显式设置
`GODEBUG=panicnil=1` 时，`panic(nil)` 恢复值为 nil，可能被误报为成功并发布
`Done`。默认模式本来就能把 nil panic 表示成错误值。

两个入口现在共用私有的正常返回标记，不再从恢复值推断成功。错误值 panic 的
cause 保留在错误链中，清理失败仍包含 `ErrFinalization`。

## Input, sink and authority / 输入、输出与权威

- Inputs remain trusted host job closures and finalizers. No new callback
  source, schema, executable, permission or setting is introduced.
- The existing Manager owns terminal status, notification, artifact failure
  marking and finalizer ordering. Model text does not decide cleanup success.
- Finalizers still run once, attempt siblings after an error or recovered panic,
  and cache their terminal error. No-launch keeps its original cause.
- The guard adds no goroutine, timer or OS process. It does not release an
  external process scope or make logical cancellation physical completion.

输入仍是既有可信宿主回调，没有新增可执行来源、权限、配置项或持久协议。
Manager 继续负责终态与通知；资源终结顺序、一次执行及错误缓存不变。

## Compatibility / 兼容性

| Path | Result |
| --- | --- |
| Normal return or returned error | Existing result and error semantics |
| Default nil panic | Still a failure; typed cause remains inspectable |
| Explicit legacy nil panic | Failure instead of false successful completion |
| Error-valued panic | Existing message shape, preserved error identity |
| Finalizer panic | `ErrFinalization`; siblings attempted and error cached |
| Proven no-launch | Existing interrupted/failed state and original cause |

No persisted field/version, wire payload, tool schema, provider prefix, default
policy or caller permission changes. Corrected failures use existing statuses
and records, which older readers already understand.

Invocation errors retain their existing stack-bearing message contract.
Finalizer errors do not gain a stack trace. Trusted error values can still
contain details under the existing reporting contract; this adds no general
redaction or host-only diagnostic policy.

没有新增持久字段、版本、wire、工具定义、稳定提示前缀或默认策略。普通返回与
返回错误的行为不变；仅纠正 legacy nil panic 的假成功，并保留错误值的身份。
执行错误保留原有堆栈消息形式，清理错误不新增堆栈。

## Evidence / 验证

Actual Manager tests run their compiled test executable with explicit
`panicnil=0` and `panicnil=1`. Each child exercises invocation, finalization and
proven no-launch finalization, recording actual terminal states and errors.

The parent logs child cases. The child helper is a no-op in ordinary runs.
These settings affect only owned test subprocesses, not the parent environment.

Separate error-valued panic cases verify the original cause for all three
paths, sibling cleanup, cached finalization failure and retained cancellation.
Existing success, returned-error, cancellation and finalizer-ordering tests
remain neighbours of this change.

Preceding production owners compile and fail the legacy nil-panic and error
identity assertions. These are constructed trusted-callback regressions, not
evidence of a production incident or remote exploit.

真实 Manager 测试通过自身编译出的测试程序分别设置两种 nil panic 模式，覆盖执行、
清理和未启动清理。另验证错误身份、后续清理、缓存错误及原取消原因。旧生产 owner
可编译后真实失败；不是编译失败对照，也不宣称线上事故或远程利用。

## Limits / 限制

Recovery covers only the current invocation stack. `runtime.Goexit`, another
goroutine's panic, OS termination and process crashes remain outside it.
Callback return or panic does not prove descendant process-scope completion.

A callback may partly change a resource before panicking. Error reporting does
not roll those effects back or prove the resource safe. This adds no journal,
durable Begin, recovery protocol, scope receipt or budget.

Automatic isolation/integration and the broader concurrency/recovery plan
remain incomplete. Author analysis does not replace independent review.

恢复只覆盖当前调用栈，不覆盖 `Goexit`、其他 goroutine 的 panic、OS 终止或进程
崩溃。回调停止也不等于后代进程 scope 完成。

错误报告不会回滚回调的部分副作用，不证明资源安全，也不建立日志、持久化 Begin、
恢复协议、scope receipt 或自动隔离策略。整个并发与恢复方案仍未闭合。

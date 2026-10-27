---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Isolated runner panic retention / 隔离 runner panic 成果保留

## Cause and authority / 根因与权限

`Store.Execute` creates a worktree before calling its host-supplied `Runner`.
An ordinary error reaches `Finish`; a panic previously unwound past that call
without publishing either a completed result or an inspection identity.
The worktree could contain edits but could not be found through `Pending`.

`Store.Execute` 在调用宿主 runner 前创建工作树。原实现只处理正常返回的错误；
panic 会直接越过 `Finish`，既不发布完成结果，也不登记待检查身份。
因此，已写入的成果仍在磁盘上，却无法通过 `Pending` 找回。

The only production caller is the task isolation adapter, reached through direct
task calls and deferred capability calls. Its boot runner builds an unattended
kernel.

A trusted provider callback can panic on the next request after that
kernel has written a file. The assembly regression forces precisely this path;
it is an injected reachable failure, not a reported production incident.

生产调用方是 task 隔离适配器，包括直接调用与动态 capability 调用。boot runner
创建独立子内核；子内核写文件后，下一次可信 provider 回调可能 panic。
真实装配回归明确注入此路径，不将该注入描述为已观察到的生产事故。

## Recovery boundary / 恢复边界

The invocation owner recovers only a panic unwinding the runner's own goroutine.
It reports `ErrRunnerPanic` and preserves an error-valued panic as an `errors.Is`
cause.

String and nil panic values remain failures, including Go's legacy
`panicnil=1` behavior. A successful return and an ordinary returned error retain
their original paths and classification.

调用边界只恢复 runner 当前 goroutine 的 panic，以 `ErrRunnerPanic` 标识，并保留
error 类型的原始原因。字符串、nil 及 `panicnil=1` 的旧语义均不误作成功。
正常成功和直接返回的错误保留原有流程和分类。

A panic does not prove that subprocesses, descendants or asynchronous hooks
have stopped. Consequently this branch does not call `CandidateTree`, classify
the result as empty, or automatically remove its worktree.

Under the existing
per-result operation owner it publishes the existing version-1 `inspection.json`
for the now-unwound runner. No running/preparing meaning is added to that format.

panic 不能证明子进程、后代进程或异步 hook 已结束。本分支因此不调用 `CandidateTree`、
不判空、不自动删除候选；在现有结果互斥协议内，为已退出调用栈的 runner 发布既有
version-1 `inspection.json`。该格式没有新增运行中或准备中语义。

Cancellation is removed only for recovery publication, as with ordinary Finish.
Operation admission still checks the stable settlement fence.

If admission fails,
the identity remains live-Store only; if marker publication fails, the existing
retention helper returns both the panic and the publication failure and discloses
that restart durability could not be established. No lock or record is stolen.

恢复发布沿用普通 Finish 的不带取消 context；结果准入仍检查稳定 settlement fence。
准入失败时仅在当前 Store 保存身份；标记发布失败时同时返回 panic 和发布失败原因，
明确不能保证重启后可发现。不会夺取锁或覆盖其他记录。

## Compatibility and consumers / 兼容与消费方

| Boundary / 边界 | Behavior / 行为 |
| --- | --- |
| Existing complete result / 已完成结果 | Unchanged `entry.json` and ordinary Finish / 原有格式与流程 |
| Unwound panic runner / panic 退出调用栈 | Existing inspection-v1 marker, original `iso_` ID / 原待检查格式与原 ID |
| Reader supporting inspection-v1 / 支持 v1 的读取方 | Finds and reinspects through existing Paths/Apply/Discard / 原入口发现并重新检查 |
| Older entry-only reader / 更早读取方 | Does not discover or age-sweep the inspection marker / 不发现、不按年龄清理 |
| Unknown existing marker or settlement fence / 未知标记或恢复围栏 | Existing refusal/preservation rules / 原有拒绝与保留规则 |
| Prompt, tools, permissions, wire / 提示、工具、权限、传输 | No definition or authorization change / 不改变定义或授权 |

The returned error identifies retained work; it does not authorize applying it
or establish verification evidence. An explicit recovery operation uses the
existing rooted identity and fresh path inspection.

Those existing operations
do not establish process-scope quiescence either; a future lifetime protocol must
govern that boundary before claiming safe shared execution leases.

错误中的 ID 只用于发现保留成果，不授予应用权限，也不表示验证通过。显式恢复继续使用
原有目录身份准入和重新检查路径。现有恢复操作同样不证明进程作用域已静止；后续生命周期
协议必须约束该边界，才能承诺安全的共享执行租约。

## Validation and limits / 验收与限制

Tests cover changed work, cancellation with unavailable Git, failed recovery
publication with truthful live-only retry, error/string/nil panic values, ordinary
error controls, and Store reconstruction followed by same-ID apply.

Real
`boot.Build` direct/deferred task calls write through the child tool, panic in the
next provider request, preserve the model-visible ID, reconstruct the Controller,
and apply that same result. CRLF normalization preserves Windows checkout meaning.

测试覆盖写后 panic、取消且 Git 不可用、标记失败的当前 Store 重试、不同 panic 类型、
普通错误对照、Store 重建后同 ID 应用；真实 boot 直接和动态调用通过子工具写文件，
在下一次 provider 请求 panic，检查模型可见 ID，再重建 Controller 并应用该结果。
断言兼容 Windows 的 CRLF checkout，不用本地编译替代原生平台证据。

This repair does not implement durable Begin, process-crash recovery, SIGKILL,
`runtime.Goexit`, cross-goroutine panic recovery, process-scope receipts, or journaled
settlement.

Best-of candidates have a separate runner owner and remain outside
this Store repair. The legacy cleanup-error and finished-result age-retention
gaps remain open.

Neither successful reinspection nor a passing test retroactively
proves that the panicking task was verified or physically quiescent.

本批不实现持久化 Begin、进程崩溃、SIGKILL、`runtime.Goexit`、跨 goroutine panic
恢复、进程作用域凭据或 journal 整合。best-of 的 runner 归属不同，不属于此 Store 修复。
既有清理错误与已完成结果按年龄回收问题仍待解决。重新检查或测试成功不能追溯证明
发生 panic 的任务已验证通过或进程作用域已静止。

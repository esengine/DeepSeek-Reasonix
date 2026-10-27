---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Delegation panic outcomes / 委派 panic 结果

## Cause and owner / 原因与所有者

Foreground delegation settled its graph while the named error was still nil
during panic unwinding. A later progress defer reported failure, leaving the
two projections inconsistent. Its transcript could remain running on disk.

Both foreground and background recovery also used a non-nil recovered value
as their panic test. Explicit `GODEBUG=panicnil=1` makes a recovered nil panic
nil, so a callback failure could be swallowed or published as successful work.

前台委派在 panic 展开期间先根据尚未赋值的错误结算图状态，随后进度才报告失败，
导致两种投影不一致，磁盘 transcript 也可能保留运行中状态。前台与后台还以
恢复值非 nil 判断 panic，旧式 nil panic 因而可能被吞掉或误报为成功。

One private invocation boundary now distinguishes normal return from panic
before graph, progress or transcript completion. Foreground failures save the
existing failed transcript state before releasing that run and repanic with
the original value.

Background failures retain the existing reference-bearing
result and Manager finalization contract. Error-valued causes remain wrapped.

现在由同一私有调用边界先区分正常返回与 panic，再完成图、进度及 transcript
结算。前台在释放 run 前保存既有失败状态，并重新抛出原值；后台保留已有结果
引用和 Manager 清理协议。错误值 panic 的 cause 身份不会因格式化而丢失。

## Authority and lifetime / 权限与寿命

The input is a trusted host/provider callback on the current invocation stack.
The output uses existing failed statuses and the already-prepared run identity.
No caller can provide a raw transcript path or adopt a fresh result identity
through this change. No production panic incident or remote exploit is claimed.

输入是当前调用栈内的受信 host/provider 回调；输出使用既有失败状态及已准备的
run 身份。本变更不接受任意 transcript 路径或新建身份来接管结果，也不把构造
回归描述为线上 panic 事故或远程利用。

The Store terminal save remains fallible. A failed save is joined into the
projected error, not replaced by success or a claim of durable recovery.

The prepared run remains owned through foreground persistence and is released
before terminal callbacks. The returning old call cannot retire a replacement
reservation made by a terminal callback.

Background ownership stays with the accepted Manager finalizers, including
the generation-bound writer cleanup already present on the base branch.

Startup options, event sinks and completion callbacks remain trusted. This
does not make arbitrary host callback panic or partially completed job startup
transactional. Cancellation keeps the existing context-based outcome precedence.

Store 终态保存仍可能失败；保存错误会进入投影错误，不会被转换成成功或持久
恢复承诺。前台在持久化期间持有 run，终态回调前释放；旧调用返回不会退休
回调取得的新预留。后台继续由已合并的 Manager finalizer 管理资源，包括基线
中已有的代次绑定 writer 清理。

启动选项、事件 sink 与完成回调仍受信；本变更不让任意 host 回调 panic 或部分
完成的 job 启动具备事务回滚能力。取消保持既有基于 context 的结果优先级。

## Compatibility / 兼容

There is no new exported API, schema, wire field, permission, tool prefix,
configuration mode or default. Ordinary success/error/cancellation and job
handoff retain their contracts.

Foreground panic still propagates with its
original payload; background panic still becomes a failed job with a reference.
Previously persisted inconsistent outcomes are not retroactively reassessed.

没有新增公开 API、格式、wire 字段、权限、工具前缀、配置模式或默认策略。普通
成功、错误、取消和后台交接保持原有协议；前台仍抛出原 panic 值，后台仍返回
带引用的失败结果。已经持久化的旧错误结果不会被追溯重写。

| Boundary / 边界 | Reader and writer behavior / 读写行为 |
| --- | --- |
| Transcript metadata | Existing failed state, same version and fields / 既有失败值，版本和字段不变 |
| Graph and progress | Existing failed projection, exactly one terminal / 既有失败投影，单一终态 |
| Foreground caller | Original panic value propagates / 原 panic 值继续传播 |
| Background Manager | Existing failure and cleanup contract / 既有失败与清理协议 |

## Evidence and limits / 证据与限制

Real compiled delegation test processes set each nil-panic mode independently.
Foreground/background cases check graph state, terminal progress, fresh-Store
metadata reads, original foreground payload and background error identity.
The ordinary error and successful-progress cases remain controls.

真实编译的测试子进程分别设置两种 nil-panic 模式，覆盖前台及后台图状态、进度
终态、新 Store 读回元数据、前台原值传播和后台错误身份。普通错误与成功进度
作为相邻控制。真实 boot 装配另覆盖前台/后台与直接/capability 四条入口。

Panic unwinding is not process-scope completion. This does not contain
`Goexit`, other-goroutine panic, detached processes or OS termination, and
adds no scope receipt, durable Begin, rollback, recovery journal or retention
policy. It activates none of the pending mutation/isolation interfaces.

panic 栈展开不等于进程域物理完成。本单元不覆盖 `Goexit`、其他 goroutine 的
panic、脱离进程或 OS 终止，也不增加 scope receipt、durable Begin、回滚、恢复
journal 或保留策略；不会激活待审 mutation/isolation 接口。

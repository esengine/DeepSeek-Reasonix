---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Hook workspace invalidation / Hook 工作区失效通知

## Scope and root cause

`Executed` answers whether the concrete tool was entered. A user-configured
PreToolUse callback can change files before denying that tool. Post callbacks
can change files outside the tool's declared paths, including after a reader.

The execution-only invalidation gate missed these cases. Nested calls also
bypassed publication because it belonged to the batch caller.

The common invocation exit now publishes invalidation after its cleanup. An
entered tool-hook callback with possible writes independently widens that
invocation's invalidation to workspace content, tree and Git metadata.

Unknown hook scope uses `AllPaths`, without attributing the hook's changes to
the tool's declared file. Nested results carry the same existing metadata as
batch results. Tool execution, error and refusal remain independent.

## Sources, sinks and authority

| Boundary | Input and treatment |
| --- | --- |
| Hook configuration | Existing user-approved shell hooks; no new discovery, permission or launch path |
| Callback entry | Host records possible effects before entering PreToolUse, covering subsequent post callbacks and partial failures |
| Optional capability report | Empty/non-tool runners report no tool mutation capability; legacy custom hooks remain conservatively unknown |
| Concrete target | Existing direct/proxy resolution and target-access checks run before hooks |
| Host sink | Existing `WorkspaceMutationSink`, serialized by the existing publication mutex |
| Tool result | Existing mutation metadata; `Executed` stays false when a pre-hook denies the tool |

The notification is an invalidation request, not proof that bytes changed.
Reporting an enabled hook set is conservative: a callback can match no command,
or run a command that changes nothing.

Broad invalidation is preferable to retaining stale resource state when effects
are unknown. No contents, shell output, environment values or credentials are
added to this notification.

Publication occurs after invocation cleanup, on normal, refused and panic exits.
It no longer depends on the batch completing or a parallel peer finishing.
The sink contract remains synchronous and serialized; expensive refresh work
belongs to the sink's existing scheduling boundary.

## Compatibility and limits

There are no new persisted fields, event fields, RPC schemas, dependencies,
permissions, launch commands, configuration defaults or provider-visible tool
definitions. Empty hooks and calls rejected before hook entry do not gain false
hook invalidations. Ordinary writer invalidation retains its existing scope.

This change covers the tool-use callback sequence, not every lifecycle hook or
arbitrary external process. It does not sandbox approved hooks, enlarge their
authorization, or claim to observe exact paths they changed.

Workspace leases, checkpoint preimages, delivery receipts and verification
evidence have separate owners. Their complete hook-effect accounting remains a later series slice;
this host invalidation is not a verification receipt or a new transaction mode.

The current built-in frontends do not implement this optional sink themselves.
This slice fixes the host callback contract for consumers that opt in; Desktop
resource-refresh wiring is a separate series step. Host-only result metadata
is not exported through the ordinary wire transport.

## Verification contract

Real `boot.Build` tests launch a disposable helper that writes outside the tool's
declared path. Direct and deferred-capability cases cover refused readers/writers,
a post-reader hook, and a failure hook after an edit error.

They assert actual bytes, broad sink invalidation, result metadata and the denied
tool's unchanged `Executed` value. Owner tests cover panic, successful writers
and readers, absent/non-tool hooks, access refusal and nested writers.

A compiling overlay of original owners must fail the effect assertions;
compilation failure is not a negative control. The PR separates local platform
results from exact-head hosted checks.

## 中文安全说明

根因是把“工具进入执行”和“整个调用可能改变工作区”当成同一件事。用户已授权的
hook 可以先改文件再拒绝工具，也能在只读工具之后改其他路径；嵌套调用则没有经过
原来的批处理通知出口。

修复在公共调用退出、资源清理后发布失效通知。可能写入的 hook 一旦进入调用链，
便保守刷新工作区内容、目录和 Git 元数据；它不冒称已确认写入，也不把实际范围
限制为工具参数中的路径。工具被拒绝时 `Executed` 仍为 false。

没有扩大权限、增加外传内容或改变持久化格式、RPC、工具定义及默认配置。通知
可能因未匹配或未写入的 hook 多刷新一次，这是范围未知时的保守行为。完整的
checkpoint、验证证据失效、生命周期 hook 与事务协议仍属于后续工作，不能用这批
通知修复宣称它们已经完成。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Guardian execution boundary / Guardian 执行边界

## Cause and authority / 根因与权限

The Guardian is a separate safety-review model. Boot filters its visible tools
through `FilterReadOnlyRegistry`, but `use_capability` is itself read-only and
can resolve a state-changing target from the executor's shared tool registry.

`guardian.NewSession` previously constructed an ordinary agent without the
permanent read-only execution flag. Filtering the proxy's schema therefore did
not restrict the resolved target: a review could write a file while deciding
whether the executor's requested action should run.

Guardian 是独立的安全审查模型。启动时过滤可见工具，但 `use_capability` 的
外层声明为只读，实际目标却可来自执行代理的完整工具注册表。此前构造器没有
启用永久只读执行检查，因此审查模型能通过代理执行写入，绕过角色权限边界。

The repair sets `ReadOnlyExecution` at the session constructor that owns every
Guardian agent. Existing agent admission checks inspect the actual resolved
tool, its effective read-only status and declared host effects before execution.

The planner-only MCP exception remains disabled. Normal read-only investigation
and the Guardian's verdict on the executor's action continue independently.

修复在 Guardian 的统一会话构造器启用永久只读标志，复用真实工具解析后的
宿主检查。该检查拒绝写入及已声明的宿主变更，不启用 Planner 的 MCP 例外。
只读调查和对执行代理操作的裁决仍保持独立；允许执行代理写入不等于授权审查
模型自行写入。

## Source, sink and trust / 来源、执行点与信任

- Model tool-call arguments and parent transcript text are untrusted input.
  They cannot change the constructor's host-owned role flag.
- The source registry and dynamic resolver remain host-owned. Actual targets
  still pass the existing path policy, hooks and execution checks; a read-only
  proxy declaration cannot replace the target's classification.
- Tests exercise the ordinary local capability resolver with both a writer and
  a reader that declares host mutation. Their execution counters remain zero.
  A benign reader still runs and the review still produces a valid verdict.
- A real `boot.Build` run reads a disposable file during Guardian review,
  attempts to overwrite it through `tool:write_file`, observes refusal, and
  successfully performs the separately authorized executor write.

模型参数和父会话文本均不可信，不能修改宿主设定的角色标志。测试覆盖实际本地
能力解析、写工具及声明宿主变更的工具；真实装配回归证明审查模型的写入被拒绝，
同时执行代理被批准的写入仍能成功。测试只使用临时目录，不访问生产资源。

## Bounds and compatibility / 边界与兼容

This is an execution authorization check, not an OS sandbox. A trusted tool that
misreports its effects, an external process, or hooks with their own side effects
are not made read-only by this flag. MCP authorization and effect declarations
retain their existing trust boundary.

No new persistence, wire format, model
prompt, tool schema, policy setting or migration is introduced.

这是角色执行权限检查，不是操作系统沙箱；错误声明影响的可信工具、外部进程及
hooks 自身的副作用仍受各自原有边界约束。本批不改持久格式、消息协议、模型
提示词、工具定义、配置或默认策略。独立安全审查及各平台 CI 仍须分别验收。

File-view ownership is a separate concern: preventing a reviewer from refreshing
the executor's stale view is tracked in the #14133 series and is not claimed by
this permission repair. The transaction/isolation/recovery series is not
activated here. Author analysis does not substitute for independent review.

读取版本归属是另一个独立问题，由 #14133 系列继续处理，本权限修复不声称解决
该问题，也不启用事务、隔离或恢复层。作者说明不能替代独立审查。

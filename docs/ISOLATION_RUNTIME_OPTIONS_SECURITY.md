---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Isolated runtime options / 隔离任务运行参数

## Cause and scope / 根因与范围

The task tool dispatched `isolation: "worktree"` before ordinary task-spec
resolution. That branch forwarded the prompt and explicit model, but silently
dropped `effort`, `max_steps` and the configured subagent model.

Its separate
kernel could therefore run with a different model/depth and without the requested
step cap. This is parameter loss, not evidence of a permission bypass.

An adjacent shared model-resolution defect reloaded a parent-model alias from
configuration instead of preserving the actual parent entry, losing a session
effort override. Canonical references already preserved that entry.

任务工具在普通任务规格解析前分流到 worktree，只转发提示与显式模型，遗漏 effort、
max_steps 和已配置的子代理模型。隔离子内核因此可能使用错误模型、思考深度或步数上限。
这属于参数丢失，不能据此声称权限绕过。相邻的模型别名解析还会重新读取配置，丢失父
会话已生效的 effort 覆盖；规范模型引用原本会保留该覆盖。

## Source, owner and sink / 来源、所有者与执行端

- Task arguments and existing task defaults enter the same model/effort and
  child-budget resolution owners used by ordinary tasks. Vision routing remains
  in the existing child-runtime resolver.
- The isolated runner receives ephemeral `Run.Effort` and `Run.MaxSteps` values.
  Boot resolves a `provider.Selection` through the shared subagent model/effort
  validation owner, then supplies it and the cap to the unattended child's Build.
  No extra provider is constructed merely to validate a selection.
- Parent aliases resolving to the same canonical model reuse the runtime parent
  entry. A different model keeps its own defaults; explicit effort still wins.
- Unsupported explicit model/effort combinations remain errors. Existing
  inherited-effort policy may drop an incompatible inherited setting on a
  call-selected model, while a configured model/effort pair remains strict.
  A custom provider resolver retains its existing selection authority.
- Best-of uses the same private unattended builder. It continues to pass its
  model with no effort override or explicit cap, preserving its prior policy.

显式参数和默认值通过普通任务已有的解析所有者，执行端将结果传入子内核 Build。
新增字段只在内存中的 runner 调用传递，不增加额外 provider 构造。

父模型别名保留
实际会话值，其他模型使用自身默认值；显式 effort 优先。不兼容显式参数继续报错，
继承值沿用既有逐模型策略，自定义 resolver 的权限不变。共享 unattended builder
的 best-of 调用继续使用原先无显式 effort/上限的策略。

## Authority and compatibility / 权限与兼容

This does not enable isolation in a session that did not offer it. Profiles,
tool restrictions, write paths, background execution and continuation/fork
arguments remain explicitly refused by this isolation path before execution.

They are not silently removed or treated as implemented. Existing approvals,
workspace selection, sandbox policy and apply/discard behavior are unchanged.

Only the in-process runner and private builder signatures change. Persisted
result records, config files, wire messages, public tool schemas and tool order
do not change. The Store Execute signature remains compatible.

Zero MaxSteps
retains the existing automatic behavior; a positive explicit cap wins, otherwise
the ordinary child-budget rule derives it from the parent cap. This is a local
step cap, not a shared root resource or concurrency budget.

不会为原本关闭的会话启用隔离。profile、工具限制、write_paths、后台和续跑/fork
仍在执行前明确拒绝，不能视为已支持或静默删除。原审批、沙箱、应用/丢弃行为不变。

只改进程内调用，不改持久格式、配置、wire、工具定义及顺序。零上限沿用自动行为，
显式正值优先，否则复用普通子任务预算规则；这不是根任务共享预算或并发限额。

## Regression boundary / 回归边界

Real `boot.Build` regressions observe actual provider construction and requests
through both direct task and deferred capability dispatch.

They cover explicit
and configured models, supported/incompatible effort, parent runtime overrides,
parent aliases, explicit/derived/automatic step caps and unchanged system/tool
prefixes over the child run. Finite scripted providers bound the negative
control even when the old implementation ignores its cap.

The predecessor owners can be compiled with the unchanged public-assembly
behavior regression. The additional private-helper unit is excluded from that
overlay because its helper did not exist before this repair; compilation errors
are not counted as behavior failures.

真实装配测试观察 provider 构造和请求，覆盖直接与动态 capability 入口、模型与 effort
及三类步数策略，并比较子任务各回合的系统/工具前缀。反向测试保留真实装配行为测试，
只排除旧版本不存在的私有 helper 单测；编译错误不能算行为反例。

## Remaining work / 后续边界

Automatic isolation, durable Begin/running identity, background/continuation,
profile/tool/write-path support, nested/root budgets, environment preparation,
cross-process settlement, recovery journals, snapshot-bound verification and
automatic integration remain independent series work.

Worktrees do not isolate
network services, databases, credentials, ports or all shared Git metadata.

This author assessment is not independent security approval or native-platform
qualification, and does not resolve historical findings from the original large
PR. Native and hosted evidence must name the tested revision separately.

自动隔离、持久运行身份、后台/续跑、profile/工具/路径支持、根预算、环境准备、
整合日志和快照验证仍独立推进。worktree 不隔离网络、数据库、凭证、端口及全部
Git 共享元数据。作者分析不能替代独立安全审查或原生平台验证。

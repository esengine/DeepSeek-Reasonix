---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Review-role observations / 审查角色读取记录

## Cause and repair / 根因与修复

Planner and Guardian sessions are separate actors, but their direct file tools
copied the executor's `FileViews`. Their capability frontends also resolved local
tools from the shared registry. A role's read could authorize the executor's
stale whole-file overwrite without the executor seeing the changed bytes.

Planner 与 Guardian 是独立角色，但其直接文件工具和动态能力仍可能引用执行
代理的读取记录。角色读到了新内容，会错误刷新执行代理的过期覆盖保护。

Role registry construction now forks one record for the actor and binds both
direct tools and the fresh capability frontend to it. Boot supplies the planner's
fresh ledger-bearing frontend before binding, avoiding a later replacement that
would discard either the binding or capability accounting.

本批在角色注册表构造阶段分配独立记录，并绑定直接工具与新建的能力前端。
Planner 在绑定前接收带独立账本和审计的前端，避免随后替换前端丢失绑定或记账。

## Authority and effects / 权限与执行

- The binder changes only already-protected builtin file-tool state. Disabled
  protection, path roots, overlays and tool metadata retain their existing values.
- Generic `FilterReadOnlyRegistry` still filters within one actor. Only explicit
  Planner/Guardian builders create new observation ownership.
- Guardian retains the generic filter's authorized read-only MCP selection.
  Planner retains its existing role-specific registry and execution policy.
- Model arguments cannot provide the binder or replacement frontend. The boot
  host constructs the fresh frontend and installs its own ledger and audit.
- The resolver still applies disabled checks and resolves actual tool identity.
  New observations do not grant write authority or bypass approval.

绑定只修改已启用保护的内置文件工具状态，不改变路径、overlay、工具定义或
授权。通用只读过滤仍属于同一代理，只有明确的新角色构造器分离读取归属。
前端由宿主在发布前构造，不接受模型提供的绑定、账本或审计对象。

## Evidence and bounds / 证据与边界

Real `boot.Build` tests cover Planner/Guardian with direct/deferred reads. The
executor sees original bytes, the reviewer sees a later user edit, and the
executor's stale write must be refused while the user content survives.

真实装配测试覆盖两个角色及直接、动态读取。执行代理先看到旧内容，审查角色
再读到用户新内容；执行代理的过期写入必须被拒绝，用户修改必须保留。

Registry tests cover shared runtime and cloned frontend paths, planner ledger
and audit preservation, and the executor's own subsequent reread. Serialized
schemas and system prefixes remain stable in the real role runs.

注册表测试还验证共享运行时、前端克隆、Planner 账本及审计、执行代理后续自行
重读。真实角色请求同时验证工具定义和系统提示前缀稳定。

This does not impose mandatory reads, atomic CAS or OS confinement. Custom
wrappers hiding builtin state remain outside this binder. Guardian's permanent
read-only execution repair is separate; this slice makes no claim to fix that
permission boundary or activate transactions, isolation or recovery.

本批不提供强制先读、原子比较写入或系统沙箱；隐藏内置状态的自定义包装器不在
绑定范围内。Guardian 永久只读权限修复独立提交。本批不启用事务、隔离或恢复，
作者安全说明也不能替代独立审查。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# Child file-view ownership / 子代理读取记录归属

## Cause / 根因

The child registry copied file tools carrying the parent's `FileViews` pointer.
A child's read could refresh the parent's overwrite guard without the parent
receiving those bytes. A fresh capability frontend separated its ledger but
resolved file tools from the shared root registry, repeating the same problem.

子代理复制文件工具时也复制了父代理的读取记录指针。子代理读取新内容后，父代理
未收到该内容，过期覆盖却会被放行。动态 capability 的前端虽独立，文件工具仍从
共享根 registry 解析，因此存在同一问题。

## Ownership and inputs / 归属与输入

One new `FileViews` belongs to each child registry. Its direct file tools and
local capability resolver use the same immutable binding callback. A sibling or
descendant gets another record; no observed content is copied from its parent.

每个子代理 registry 新建一份读取记录，直接工具与动态本地工具共用它。兄弟与
嵌套子代理分别创建自己的记录，不复制父代理已经观察到的内容。

The shared capability runtime resolves tools from its root registry even at
deeper levels. Mapping a new record only by the parent tool's record pointer
splits a grandchild's direct and dynamic observations. The actor, rather than
that source pointer, therefore owns the binding.

深层 capability 仍从根 registry 解析。仅按父工具的记录指针映射，会把孙代理的
直接读取和动态读取分成两份。本批以当前代理作为记录所有者，避免这种分裂。

| Input / 输入 | Authority and handling / 权限与处理 |
| --- | --- |
| Parent registry | Host-provided tool instances; only supported builtin file tools with protection already bound receive a fresh record. |
| Task/profile tool selection | Existing filtering, recursion limits and read-only registry construction still select the tool set. |
| `tool:` capability ID | Disabled-tool checks and registry lookup precede local binding; the callback runs before the resolved target is published. |
| Binding callback | Host-installed on a new frontend before publication. `CloneForAgent` does not copy a parent's execution-local callback. |
| MCP capability | No remote tool or server binding changes. Restricted MCP-only proxies do not authorize local builtin capability IDs. |

根 registry、回调及工具实例由宿主提供；任务和 profile 的工具限制继续由原入口
执行。动态本地工具通过禁用检查和查找后才绑定记录。MCP 远端工具与权限不变，
只允许 MCP 的受限代理不会因此获得本地工具权限。

## Sinks and preserved boundaries / 消费方与保留边界

`BindFileViews` copies the concrete builtin value and changes only its view
pointer. Path roots, forbidden reads, editor overlays and other configuration
stay on that value. The callback does not unwrap grant wrappers or change
`ReadOnly`, tool descriptions or schemas.

绑定只复制具体内置工具的值并替换读取记录指针；原路径根、禁止读取、编辑器
缓冲区等配置继续保留。不拆权限包装，不改变只读属性、工具描述或 schema。

Consumers remain the existing file read/edit/write owners. Permission checks,
read-only execution of resolved targets, write grants, approval, foreground-only
Bash and capability allowlists retain their existing owners. No shell command,
persistent format, cross-process state or wire field is added.

文件工具继续消费读取记录，权限、动态目标只读检查、写入授权、审批、Bash 及
capability 白名单继续由原模块负责。本批不新增命令、持久格式或跨进程协议。

## Limits / 限制

| Boundary / 边界 | Limit / 限制 |
| --- | --- |
| Overwrite guarantee | An optimistic guard, not atomic CAS or a filesystem lock. External writers can race after its check. |
| Never-read files | A child starts empty; legacy never-seen overwrites remain allowed. Mandatory full reads of all existing files remain deferred. |
| Observation source | #14377 independently fixes second-read/paging provenance. This slice builds without it and does not claim to include that repair. |
| Custom tools | Wrappers hiding builtin state and third-party file tools are not introspected or claimed covered. |
| Restart/continuation | New registries start with runtime-local views. Historical read receipts are not reconstructed or migrated. |
| Isolation | No worktree, journal, sandbox, automatic integration or default-strategy activation. |

这仍不是原子比较写入或锁；外部写入竞争、全局强制读取、重启后读取证据等留给
后续批次。#14377 独立处理读取内容来源。本批也不声称覆盖隐藏内部状态的自定义
包装，不接入 worktree、恢复日志或默认策略。

## Evidence and independent review / 验证与独立审查

Real `boot.Build` tests exercise writer/read-only tasks with direct/capability
reads: the child receives new bytes, the parent's stale overwrite is refused,
and user content survives. Read-only dynamic writes remain blocked. Serialized
tool definitions and system prefixes stay equal across each actor's requests.

Registry tests cover sibling separation, three nested levels, shared runtimes and
frontend-clone fallback. Neighbour tests cover disabled tools, clone-local
bindings and disabled file protection. Original owners reproduce the overwrite;
a parent-pointer mapping control exposes the nested direct/dynamic split.

真实装配覆盖写入及只读子任务的直接和动态读取，同时检查用户内容、父代理拒绝、
子代理只读限制及稳定前缀。registry 回归覆盖兄弟、三层嵌套、共享运行时和克隆
回退。旧实现与按父指针映射的反向验证分别复现覆盖和深层记录分裂。

This is an author-written assessment. Hosted checks and independent code-owner
security review remain required; extraction does not dismiss original CodeQL
findings.

这是作者书面分析，不能替代独立审查与当前 head 的 CI，也不撤销原 CodeQL 发现。

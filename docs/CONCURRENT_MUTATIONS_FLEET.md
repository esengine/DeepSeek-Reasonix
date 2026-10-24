---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Fleet coordination / 批量任务协调

`write_paths` is an authorization boundary. In shared transaction mode, two fleet
items may have overlapping grants. Each actual mutation still acquires its full
resource set and checks its own read version under coordination.

`write_paths` 是授权边界。共享短事务模式允许两个 fleet 任务的授权范围重叠，
实际变更仍须申请完整资源集合，并在协调范围内检查各自读取的版本。

| Runtime / 运行方式 | Overlapping grants / 授权范围重叠 | Execution / 执行方式 |
| --- | --- | --- |
| Legacy / 兼容模式 | Concurrent reservations refused / 拒绝并发预约 | Existing task claims / 原任务预约 |
| Shared / 共享 | Allowed / 允许 | Short transactions and version checks / 短事务及版本检查 |
| Auto / 自动 | Allowed / 允许 | Eligible parallel writers get candidates / 符合条件的并行写任务隔离 |

Automatic selection knows the dependency graph before the first item starts.
Independent writable items carry that scheduling intent even if one reaches
admission before another. Ordered writers, read-only items and adopted results
do not by themselves request parallel isolation.

自动策略在第一个任务启动前已知依赖图。互相独立的可写任务携带并行调度信息，
不会因为某个任务先进入准入流程而丢失隔离选择。仅有顺序写任务、只读任务或
已采纳结果时，不据此要求并行隔离。

Isolation preserves the same grants, model, effort, tool restrictions and root
budget. A shared override stays shared. Preparation failure in automatic mode
reports its cause and uses shared transactions; required isolation still refuses.

隔离保留原有路径授权、模型、推理强度、工具限制和根预算。明确选择共享仍使用
共享模式。自动隔离准备失败时报告原因并回退共享事务；要求隔离时仍明确拒绝。

Same-file shared writers keep independent read histories. A stale view returns
`workspace.mutation_version_conflict`. The coordinator can request a fresh read
and reschedule. Different candidates may change the same file independently;
their later integration still uses baseline checks and can report a conflict.

同文件共享写入者保留独立读取历史。过期视图返回明确的版本冲突，协调代理可
要求重新读取并重调度。不同候选环境可以分别修改同一文件，后续整合仍按基线
检查，可能产生冲突，不能据此静默覆盖。

Fleet is a delegation operation. The parent records the actual child mutations,
including unknown or failed observations, rather than inventing another opaque
workspace mutation for the fleet call. Child checks do not become current
workspace verification merely because the group returned.

Fleet 属于委派操作。父代理记录子代理的实际变更，包括未知或失败的观察，不把
fleet 调用本身再虚构为一次范围未知的工作区写入。批量任务返回也不会把子代理
检查自动变成当前工作区的验证通过证据。

Regression coverage uses real `boot.Build` foreground/background fleets with
overlapping grants, independent candidate roots, a live unrelated transaction
during inference, and forced same-file reads before competing edits.

回归通过真实 `boot.Build` 覆盖前台和后台 fleet、重叠授权、独立候选环境、推理
期间无关事务持续可用，以及强制两个任务先读取同一版本后再竞争编辑。

Tool definitions describe both protocols and stay stable during a runtime.
This change does not enable the new-install automatic default; the full
compatibility, recovery, platform and performance gates still apply.

工具定义说明两套协议，在运行时创建后保持稳定。本批改动不启用新安装的自动
默认策略；完整兼容、恢复、跨平台和性能验收条件仍然适用。

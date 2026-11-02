---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Opaque write admission / 未知范围写入准入

## Scope and cause / 范围与根因

A directory reservation cannot enumerate hardlink aliases outside its tree.
The actual filesystem probe holds a Workspace lease, then admits a named write
through an external hardlink and changes the held source.

Named-file aliases and direct descendants are already excluded. This is a documented primitive
limitation, not an observed production incident; shared runtime activation is
still separate.

`OpaqueWrite` explicitly requests conservative exclusion across all path-bearing
transactions in the same trusted registry. Named disjoint writes still proceed
concurrently. Capacity-only/model requests do not participate in that exclusion.
No tool, host adapter, permission, default or prompt definition changes here.

目录范围不能枚举树外硬链接。真实文件探针证明旧 Workspace 租约不能阻止通过该
别名的具名写入；具名文件别名和目录内直接路径已有互斥。本次明确新增
`OpaqueWrite`，不悄悄扩大旧目录协议含义，也不启用运行时新策略。

## Source, sink and authority / 输入、副作用与权威

- Hosts select `Intent.Effect` and the trusted state directory. This package
  does not parse tool arguments, approve commands or authorize filesystem access.
- Opaque records publish version 3; only this effect is supported in version 3.
  Versions 1 and 2 retain their existing path/capacity semantics and encoding.
  The shared `mutations-v1` directory remains the admission authority; a separate
  directory would let old named writers miss the new owner entirely.
- One symmetric resource relation makes opaque owners and older opaque waiters
  conflict with every path-bearing request, including other roots and Git paths.
- Queue order, complete-set admission, live OS ownership, compatibility claims
  and the final fresh locked check remain in the common owner. No new singleton
  execution lock is added to every named writer.
- The paths retained on an opaque lease still constrain nested calls, expected
  content versions and compatibility claims. Global exclusion is not global
  filesystem permission. A directory parent cannot satisfy an opaque child.
- Continuations retain the opaque effect and acquire a new transaction. Cancel
  and process death use the existing identified cleanup, not forced lock theft.

影响类型由宿主选择，权限检查仍属于宿主。新版本记录显式表达全注册表互斥，旧版本
保持原义。嵌套调用同时检查路径、额度和影响覆盖，不能靠父目录将普通租约升级为
全域租约；全域互斥也不扩大嵌套路径授权。

## Compatibility / 兼容性

| Input or participant | New coordinator | Preceding coordinator |
| --- | --- | --- |
| Known version 1/2 records | Existing semantics | Existing semantics |
| Version 3 opaque owner, live or dead | Interpret/reclaim when understood | Refuse and preserve bytes |
| Version 1/2 opaque effect | Refuse and preserve | Unsupported semantics; do not publish |
| Version 3 nonopaque effect or missing paths | Refuse and preserve | Refuse and preserve |
| Unknown version/field | Refuse and preserve | Version refusal; field behavior is version-specific |
| Legacy workspacelease writer | Existing workspace claim coverage | No registry-wide opaque guarantee |

The preceding accepted coordinator is compiled and executed against live/dead
version-3 records, with real legacy cleanup as a control. No new normal runtime
publishes these records yet.

While one exists, preceding coordinators refuse all new admission instead of
trying to interpret global semantics. That is an intentional availability
boundary; an implementation that understands version 3
can reclaim a known dead owner. Do not delete unknown records to bypass it.

已接受的前版通过真实编译二进制验证：新记录持有者存活或死亡时都保留字节并拒绝
准入，旧记录清理控制正常执行。旧程序遇新协议会阻塞，必须由理解该协议的实现
处理；不能通过手删记录制造并发。旧 workspacelease 写入者仍只有原工作区域保障。

## Evidence / 验证

- Real hardlink aliases in both acquisition orders, with an actual write after
  release proving the alias is effective; unrelated-root cross-process owners
  exclude each other and recover after the child is killed and joined.
- Disjoint named writers proceed; active opaque ownership and queued opaque
  writes do not consume unrelated model capacity. Cancellation closes the wait
  notice and removes the new contribution without releasing the active owner.
- An older opaque waiter prevents later named bypass. Nested directory upgrades
  refuse; outside nested paths refuse even under opaque ownership. Continuation
  retains exclusion after the parent ends.
- Versioned publication with and without capacities, legacy directory semantics,
  unknown versions/fields and invalid resource shapes are covered separately.
- Deliberately restoring path-only conflict and path-only nested coverage
  compiles and breaks the new invariants. This is a weakened-control test of a
  new effect, not proof that the preceding API already promised that effect.

真实文件、子进程、队列、取消、嵌套、续接和版本兼容分别验证。故意削弱新规则的
对照用于证明测试有效，不能冒充旧版已实现新接口却违反承诺的缺陷复现。

## Limits and next integration / 边界与后续接入

This is cooperative exclusion inside one trusted state namespace. Other state
directories, arbitrary processes and external services remain outside it.
The registry is not authenticated or bounded by this change. Existing Workspace
and directory-valued NamedPaths leases retain their documented alias limitation.

Before runtime activation, map unresolved tool/hook effects explicitly, authorize
first and keep the lease only during physical mutation.

Lifetime completion, unknown-scope recovery, strict per-actor reads/CAS and evidence remain separate.
Never hold it through thinking, queuing or user waits. This unit does not close
the future shell/hook completion API or automatically select isolation.

Applying this global effect to every worktree command would serialize independent
tasks. A private execution adapter needs demonstrated write-root, alias, backend
and shared-Git boundaries before choosing narrower coordination.

A changed cwd or initially distinct inodes does not prove isolation. No worktree availability,
complete sandbox, Windows runtime or final performance claim is made here.

同一可信注册表以外的进程和外部副作用不受约束。后续必须先权限检查，再按真实影响
接入并限定物理执行生命周期；模型思考、任务排队和等待用户不应持有该租约。
不能对所有 worktree 命令简单套全域锁后声称隔离任务仍可并行。私有执行域需要实际
后端、授权根、别名和共享 Git 边界证据。本次不是完整沙箱或最终性能验收。

This is the author's security assessment, not independent approval or a
disposition of the original large PR's CodeQL findings.

本说明为作者评估，不替代独立安全审查，也不批量判定原大 PR 的告警。

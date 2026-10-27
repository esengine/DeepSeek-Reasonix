---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Result reinspection / 隔离结果重新检查

## Cause / 根因

Finished results were also cached as Git tree identities in each Store. A
second Store could settle the result while the first still trusted its cache.
Git objects survive worktree removal, so a discarded result could still be
applied again from that stale tree. No concurrent interleaving is necessary.

每个 Store 缓存成果的 Git tree。另一个 Store 应用或丢弃成果后，旧缓存仍被当作
操作依据。Git 对象不会随 worktree 删除，旧 tree 因此仍可再次写回主工作区。
顺序操作即可复现，不依赖竞态时序。

## Owner and authority / 所有者与权限

All result lookups now use the existing durable discovery, rooted record
admission and candidate inspector. In-memory trees no longer authorize a
settlement or supply missing results to Pending.

Missing or malformed publication cannot be bypassed by a previously populated
Store. Refusal does not delete an unrecognized record or its candidate bytes.
Unknown JSON fields retain the existing no-rewrite behavior.

所有结果读取重新经过既有持久发现、目录身份准入和候选检查，不再由内存 tree
授权操作。Pending 也不能靠缓存重新加入已不存在的成果。

缺失或损坏的持久记录不能被已有缓存绕过。拒绝操作不会删除不识别的记录或候选，
未知字段沿用原不重写行为。

## Path admission / 路径准入

Reinspection must not silently widen an earlier path declaration. If the
inspected baseline, candidate identity or target tree differs from the Store's
previous observation, this operation refuses and requires fresh path admission.
The refreshed observation is available for a subsequent operation.

The tool regression declares one path, adds another file to the candidate,
and verifies that Execute refuses without changing the source. Declaring paths
again sees both files; the subsequent execution applies their current content.

重新检查不能静默扩大旧路径声明。基线、候选身份或目标 tree 发生变化时，本次操作
拒绝并要求重新检查路径。工具回归先声明一个文件，再向候选增加文件，确认执行拒绝且
源文件不变；重新准入后声明两个文件并应用当前内容。

## Compatibility and verification / 兼容与验证

No persisted format, tool definition, permission, configuration, default or
provider prefix changes. The existing record inspector remains authoritative;
there is no second JSON decoder or new directory authorization mechanism.

The regression uses real Git worktrees and two public Store instances for
apply/discard followed by stale paths/apply/discard. A deliberately malformed
record is separately labelled as external metadata replacement.

Each lookup now pays the existing inspector's cost. No performance improvement
or native-platform result is inferred from local tests. Complete-tree and
platform results must identify their tested source separately.

不改持久格式、工具定义、权限、配置、默认值或 provider 前缀，也不新增 JSON 解析器。
回归使用真实 Git 与两个 Store，另将人为篡改记录明确区分于正常执行。
每次读取增加既有检查器的成本；本地通过不能推断性能改善或其他原生平台通过。

## Remaining limits / 尚未闭合的边界

- Reinspection is not a cross-process settlement lock. A writer can still race
  after inspection; apply/discard serialization and journals remain separate.
- Git's actual-byte snapshot fidelity remains the independent snapshot-content
  repair. This change does not upgrade a Git tree to a complete content receipt.
- Failed-first-Finish recovery, running identity, safe expiration, frozen
  verification and automatic integration retain their own review units.
- This does not authenticate local state against an arbitrary hostile process
  or sandbox external writers. Author analysis is not independent approval.

本批不完成跨进程整合锁、日志、实际字节快照、完整运行身份、保留策略、冻结验证或
自动整合。检查后的外部竞争仍存在，作者分析不能替代独立审查。

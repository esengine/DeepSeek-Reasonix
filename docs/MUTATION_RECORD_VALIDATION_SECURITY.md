---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Mutation registry validation / 写入事务注册表校验

## Scope and cause / 范围与根因

This change does not activate the transaction primitive in the shared agent
runtime. Its registry must distinguish a recognized dead owner from a record
it cannot interpret.

The previous `json.Unmarshal` ignored unknown fields. Version 1 also accepted
every effect except `capacity`. An unfamiliar record could permit unrelated
requests while live, then be deleted when its owner's OS lock became free.

Tests construct these inputs to prove the protocol gap. They do not show that
ordinary shipped executions produce them or that a remote party can write the
registry.

本次不启用共享运行时事务。此前解码忽略未知字段，版本 1 也接受除 `capacity`
外的任意影响类型，因而可能忽略未知语义继续准入，并在进程死亡后删除记录。

测试人为构造输入，证明协议边界缺口；不代表正常运行已产生此类记录，也不声称
远程攻击者能够写注册表。

## Source, authority and sink / 输入、权威与副作用

- Input remains `mutations-v1/*.json` in the host-selected state directory.
  Model tool arguments do not choose a new registry location.
- The existing OS registry lock owns admission. The common reader validates
  JSON fields before probing ownership or scavenging the record.
- Unknown object fields, including capacity-object fields, are refused.
  Exactly one JSON value is required, as before.
- Version 1 accepts omitted/empty effect, `named_paths`, `workspace` or
  `git_metadata`; other effects cannot describe path-owning records.
- Existing version-2 capacity/resource validation remains authoritative.
  Writers keep exactly the same encoding and resource semantics.
- Unsupported input returns `ErrProtocol` with a quoted record filename.
  It neither rewrites/removes that record nor admits the new request.
- Failed acquisition cleans up only its own request. Recognized dead-owner
  records remain reclaimable.

输入目录和 OS 准入锁不变。统一入口先校验字段、版本、身份、资源和绝对路径，
再判断持有者是否存活。未知内容返回 `ErrProtocol`，保留原字节并拒绝新申请。

失败调用只清理自己的申请；已知旧记录仍可读取和正常回收。省略影响字段的
版本 1 保持兼容，写入格式不变。

## Compatibility / 兼容性

| Input | New reader | Preceding reader |
| --- | --- | --- |
| Version 1, omitted effect and absolute paths | Recognized | Recognized |
| Version 1, known path effects | Recognized | Recognized |
| Version 2, known capacities/path effects | Recognized | Recognized |
| Unknown top-level/capacity fields | Refuse; preserve | May ignore and scavenge |
| Future version or malformed JSON | Refuse; preserve | Already refuses |
| Version 1, future/read-only/external effect with paths | Refuse; preserve | May admit/scavenge |

An unfamiliar field deliberately blocks admission, even if it appears merely
informational. Its compatibility needs an explicit decision; no rewrite strips
it. A compatible implementation must interpret and settle the record.

Older programs can still ignore these fields. This reader cannot retroactively
fence them or establish complete cross-version coordination. Deleting an
unfamiliar record is not a recovery instruction.

Exact field names and duplicate-member rejection are documented in
[record member validation](MUTATION_RECORD_MEMBERS_SECURITY.md). Legal spacing,
member order and unique escaped names remain supported; this is not a
canonical-byte JSON protocol.

即使未知字段看似仅用于展示，本版也保守拒绝，等待明确的兼容性决策，不通过删字段
继续执行。旧进程仍可能忽略这些字段，本次不能单方面约束它们。

应由能够理解记录的兼容实现处理，不建议手动删记录绕过。精确字段名和重复字段拒绝
另见[字段校验说明](MUTATION_RECORD_MEMBERS_SECURITY.md)；合法空白、换序及唯一转义
名称仍受支持，不要求规范字节格式。

## Evidence / 验证

- Public `TryAcquire` covers path and capacity-only requests against unfamiliar
  records with live and dead OS owners.
- Refusal retains bytes, leaves no new publication and preserves another
  owner's OS lock.
- A real subprocess publishes a normal transaction; the fixture adds an
  unknown field, checks refusal, kills/joins only that process and checks again.
- Known version-1/2 records remain readable while live and reclaimable after
  release, including omitted-effect and capacity-only records.
- Preceding production owners compile with the same tests. Unfamiliar-record
  tests fail by admitting requests or deleting records; known-shape controls pass.

公共入口覆盖具名路径、容量申请、存活/死亡持有者、未知字段与影响类型。真实子进程
测试覆盖 OS 锁释放后的重复准入；已知版本作为正常回收对照。

编译替换回此前生产所有者后，未知记录测试确实因准入或删除而失败，正常记录对照
仍通过。具体本地、CI 和原生平台结果分别记录在 PR，不从其他提交继承。

## Limits / 未完成边界

No permission, tool, controller, isolation journal or default policy changes.
The state directory must remain trusted. This author's assessment is not
independent security approval or disposition of the original large PR's alerts.

File authentication, bounded registry reads, rooted namespace hardening,
arbitrary external writes, tree-external hardlinks and complete old-writer
compatibility remain outside this change.

A scan may reclaim other understood dead records before encountering an
unsupported record. Preservation and refusal are guaranteed for the unsupported
record; the scan is not atomic across all records.

The separate wait-observation proposal may defer a retry but must return to
fresh registry validation before a grant. No cached observation grants authority.

本次不改变权限、工具、controller、隔离日志或默认模式。状态目录仍需可信；文件
认证、读取大小限制、目录句柄加固、外部写入、目录外硬链接和完整旧写入者兼容性
仍未完成。

扫描可能先回收其他已知的死亡记录；保证的是未知记录保留与新准入拒绝，不是整次
扫描的原子性。等待快照只能延后重试，授权前仍须重新校验。本文不等同独立安全批准。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Isolated record admission / 隔离记录准入

## Scope / 范围

`entry.json` survives a Store rebuild. Previously discovery checked only its
claimed workspace, then trusted embedded snapshot/candidate paths for inspection,
application and removal. Discovery now checks each record against the actual
managed slot and the repository identity opened by the host.

`entry.json` 会跨 Store 重建保留。以前发现流程只检查记录声称的工作区，随后信任其中的
快照和候选路径。本批将实际托管槽位、宿主已解析的仓库身份与记录绑定后再允许恢复或清理。

## Reachability and authority / 可达性与权限

- Normal `Begin`/`Finish` writes matching fields. The counterexamples deliberately
  copy or edit a real published record before constructing a new Store. They do
  not show ordinary execution generating mismatched metadata.
- Restart recovery and boot's age sweeper consume these records. This is local
  persistence hardening, not evidence of a remote exploit. A principal able to
  modify arbitrary host files already has broader authority.

正常 `Begin`/`Finish` 产生匹配字段。反向测试明确在磁盘上复制或修改真实记录，再重建
Store；不能据此声称正常执行会产生错配。实际消费者是重启恢复和启动时的过期清理。
本批加固宿主持久化边界，不把可任意修改宿主文件的主体描述成远程越权者。

## Checked boundaries / 已检查边界

- The marker must be a regular file; its immediate slot and candidate must be
  directories, without a leaf symlink. The slot belongs to the configured
  managed root and contains the claimed candidate, using existing file identity.
- The snapshot root and optional pinned Repo must match the Store's opened
  checkout. The prefix must map both tool roots inside their respective trees.
- The candidate's checked Git identity must belong to that candidate and share
  the source common directory, with a distinct linked-worktree administrative
  directory. Stored identities are not accepted merely because they are valid
  identities of some other repository.
- The administrative backlink must name the actual candidate directory. A
  hard-linked `.git` file cannot make another candidate's backlink authoritative
  merely by sharing its file identity.
- Marker enumeration and reads use a host-opened managed directory. Identity
  stats and candidate/common metadata reads use scoped os.Root handles. A
  stored administrative path is bounded to the opened common directory before
  its backlink is read. Literal brackets in the managed path are not globs.
- All durable consumers use this admission: pending discovery, path discovery,
  recovery for apply/discard, and the existing sweep. Invalid records are left
  byte-for-byte on disk and are not offered as recoverable results. Lookup uses
  its existing unknown-ID refusal; a dedicated quarantine diagnostic is deferred.

标记必须为普通文件，槽位和候选目录不能是末端符号链接；按实际目录身份校验所属关系。
源快照、项目子目录和候选 Git 身份均需匹配宿主身份；候选必须属于同一 common directory
下的独立 worktree。所有磁盘消费者共用准入。拒绝的记录原样保留，不提供恢复操作；
当前仍返回既有 unknown-ID 拒绝，专用隔离诊断留待后续批次。

标记枚举和读取、身份查询、候选和公共 Git 元数据读取均使用宿主打开的目录句柄。
记录提供的管理路径必须先限定在公共 Git 目录内，才能读取回链。现有路径解析仍会检查
别名元数据，但不会因此获得任意内容读取权限；目录名中的括号按字面处理。

## Compatibility / 兼容

| Surface / 边界 | Current reader / 新读取方 | Previous reader / 旧读取方 |
| --- | --- | --- |
| Existing pinned records / 已有固定身份记录 | Verify filesystem identity without starting Git / 仅文件系统校验 | Existing behavior / 原行为 |
| Legacy absent Repo fields / 缺少 Repo 的旧记录 | Read the linked marker/backlink, no Git launch or rewrite / 读取关联标记和回链，不启动 Git、不重写 | Existing behavior / 原行为 |
| Unknown JSON fields / 未知字段 | Preserve exact bytes on discovery / 发现时原样保留 | No new format written / 不写新格式 |
| Subdirectory / linked source / 子目录和 linked 源 | Remain usable after restart / 重启后可用 | Existing behavior / 原行为 |
| Mismatched/copied records / 错配或复制记录 | Retained, excluded from settlement / 保留并拒绝操作 | Old trust policy remains unsafe / 仍使用旧信任规则 |

No new marker name, serialized field, wire type, tool schema, permission or
default is introduced. Older processes cannot gain these checks from a new
process. Missing files or unverifiable legacy identity fail closed;
records are retained rather than repaired or deleted.

没有新增文件格式、wire、工具定义、权限或默认策略。旧进程不会自动获得新检查。
文件缺失或旧身份无法解析时保留记录，不能据此修复或删除。

## Evidence and limits / 证据与限制

- Real-Git regressions cover copied-record path/apply/discard/sweep, identity
  mismatch, symlinked slots/markers, hard-linked Git markers, old records, unknown fields, subdirectory
  projects and linked source checkouts.
- Selected unchanged behavior tests compile against the original discovery owner
  and fail there. Platform evidence and exact commands are recorded in the PR.

- Scoped metadata handles are not authentication, a task sandbox, transaction-wide
  filesystem pinning, settlement locks or protection against external check/use races.
- Pending-result expiration, first-publication loss, inspector failures, content
  snapshots, duplicate-ID reconciliation and journaling remain separate units.
- No CodeQL alert is dismissed by this author assessment. Independent
  code-owner/security review remains required for delivery.

本批不是认证、沙箱、跨进程整合锁，也不承诺消除任意外部进程造成的检查后竞态。
过期策略、首次发布失败、检查器错误、内容快照、重复 ID 和事务日志均未在本批完成。
作者分析不能替代独立审查，也不代表历史 CodeQL 告警已经解决。

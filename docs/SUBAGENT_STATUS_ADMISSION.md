---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-27
---

# Subagent status admission / 子任务状态准入

## Contract / 约定

Continuation and transcript copying accept only the existing `completed` and
`cancelled` states. Existing checks still verify the execution owner, workspace,
persona, model, effort and tool scope. Running, failed and interrupted records
retain their existing refusals.

续跑和 transcript 复制仅接受已有 `completed`、`cancelled` 状态。现有校验仍验证
执行所有者、工作区、persona、模型、effort 和工具范围。running、failed、interrupted
保留原拒绝行为。

Missing, empty, differently cased and unknown states are refused before a run is
prepared. Rejection preserves metadata, unknown fields and transcript bytes;
it creates no copied transcript and releases the per-reference lock.

缺失、空值、大小写不同及未知状态在准备 run 前被拒绝。拒绝保留 metadata、未知字段
和 transcript 字节，不创建副本，并释放每个 reference 的锁。

The shared check covers direct continuation, copy, ancestor continuation,
legacy fork conversion and reuse of an existing descendant copy. Loading
metadata for diagnosis remains possible. Unknown status does not become a
completed result or grant permission to resume.

共享检查覆盖直接续跑、复制、祖先续跑、旧 fork 转换及已有后代副本复用。诊断仍可读取
metadata。未知状态不会变成已完成结果，也不授予续跑权限。

## Compatibility / 兼容性

| Stored input / 存储输入 | Behavior / 行为 |
| --- | --- |
| completed, cancelled | Existing identity checks and admission remain / 保留原身份校验与准入 |
| running, failed, interrupted | Existing refusal remains / 保留原拒绝 |
| Missing or empty status | Refuse preparation, retain bytes / 拒绝准备，保留字节 |
| Unknown future status or changed casing | Refuse preparation, retain bytes / 拒绝准备，保留字节 |
| Unknown metadata fields | Refusal does not rewrite or remove them / 拒绝不改写或删除它们 |

There is no new persisted field, enum value, migration or writer behavior.
Current writers emit explicit known states. This change does not claim support
for an undocumented legacy writer that omitted status. Previous readers still
have the permissive unknown-state behavior; this is not a downgrade fence.

没有新增持久化字段、枚举值、迁移或 writer 行为。当前 writer 明确写入已知状态。
本改动不声称支持未经证实、会省略状态的旧 writer。此前 reader 仍可能放行未知状态，
因此本改动不是降级隔离机制。

## Evidence and limits / 证据与限制

Tests write real store records, alter status on disk, reopen the store and call
the five preparation paths. Known states provide controls. This models damaged
or future-version input; it is not evidence that the current normal writer
produces unknown states or that a production incident occurred.

测试创建真实 store 记录，在磁盘修改 status 后重新打开 store，调用五条准备路径；
已知状态作为对照。这模拟损坏或未来版本输入，不证明当前正常 writer 产生未知状态，
也不是线上事故证据。

The fix changes no provider request, prefix, tool permission or default. It
does not add recovery for failed/interrupted work, a journal, cross-process
writer coordination, physical quiescence or complete #14025 acceptance.

修复不改供应商请求、前缀、工具权限或默认值；不新增失败或中断任务恢复、journal、
跨进程 writer 协调、物理静默，也不代表完整 #14025 验收。

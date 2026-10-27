---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# First Finish retention / 首次完成检查失败保留

## Cause and scope / 根因与范围

`Store.Execute` finishes the runner before invoking `Finish` with cancellation
removed. Previously an error inspecting the candidate, computing its statistics,
or publishing the first completed-result marker invoked candidate removal. A
simultaneous runner error also hid the finalization error and the recovery ID.

The error branches now retain the candidate, record an inspection-pending identity
and return its original `iso_` ID. Path discovery, apply and discard retry the
existing inspector before using that result.

A failed inspection is never an
empty result or proof of validation. Successful ordinary results retain the
legacy format and behavior.

`Execute` 在 runner 结束后使用不带取消的 context 调用 `Finish`。原实现会在候选检查、
统计或首次完成标记发布失败时清理候选；runner 同时失败时，还会隐藏完成阶段的错误和 ID。
本批保留错误分支的候选及原 `iso_` 身份，让路径查询、应用和丢弃先重新检查。
检查失败不能被解释为空结果或验证通过。正常完成结果仍使用原格式。

These are reachable source error paths, not a claim that an observed production
incident reached them. Tests deliberately remove Git from the process PATH or
make the completed-marker destination unavailable. Runner cancellation alone
does not exercise this fault because `Execute` uses `context.WithoutCancel`.

测试明确注入 Git 不可用、完成标记目标不可用，不将这些故障描述成已观察到的生产事故。
单独取消 runner 不能证明此错误路径，因为实际 `Execute` 会移除完成阶段的取消信号。

## Persistence and compatibility / 持久化与兼容

Inspection-pending results use a separate `inspection.json` marker with integer
`version: 1` and an `entry` containing the existing snapshot/candidate identity.
Failed returned runners and
[recovered runner panics](ISOLATION_RUNNER_PANIC_SECURITY.md) produce it only
after their call stack has unwound.

This is not a running-task
lifecycle record. No legacy-looking marker is published for that incomplete
inspection. Reinspection and repeated `Finish` preserve the marker's namespace.

Publication uses the existing non-overwriting atomic-create owner. An existing
different/unsupported record is refused and retained. Readers never rewrite the
JSON: unknown fields survive byte-for-byte.

Unknown versions with an authentic
known identity remain discoverable but cannot be applied or discarded. Malformed
or unauthenticated records remain on disk without authorizing settlement. A
cached inspected result rechecks a present marker before settlement.

Version 1 always means inspection of an ended runner. Additive unknown fields
cannot redefine that meaning. Introducing running/preparing semantics requires
a newer version or separate namespace that this reader cannot settle.

待检查结果使用独立 `inspection.json`，含整数 `version: 1` 和既有身份的 `entry`。
只有已结束 runner 的失败完成阶段写入此记录，它不是运行中生命周期格式。

采用既有不覆盖原文件的原子创建，不覆盖不同或不支持的已有记录；重新检查不重写 JSON，
未知字段原样保留。可验证身份的未知版本可以发现，但拒绝应用和丢弃。无效记录保留且不能
授权操作。缓存结果也会在操作前检查仍存在的标记。重复 `Finish` 不降级为旧标记。

版本 1 只表示已结束 runner 的成果检查；未知字段不能改变其含义。运行中或准备中状态
必须使用更高版本或独立命名空间，使此版本读取方不能误作已结束成果处理。

| Format / 格式 | Current reader / 新读取方 | Previous reader / 旧读取方 |
| --- | --- | --- |
| Genuine `entry.json` / 真实旧记录 | Existing rooted admission and recovery / 原准入和恢复 | Existing behavior including age cleanup / 原行为含过期清理 |
| `inspection.json` v1 / 新待检查记录 | Same-ID inspection retry; no age sweep / 同 ID 重试，不按年龄清理 | Not offered or swept; upgrade to retry / 不发现也不清理，升级后重试 |
| Unknown fields / 未知字段 | No discovery/recovery rewrite / 发现和恢复不重写 | New namespace ignored / 忽略新命名空间 |
| Unknown version / 未知版本 | Retain and refuse settlement / 保留并拒绝操作 | New namespace ignored / 忽略新命名空间 |
| Recovery storage unavailable / 恢复存储不可用 | Files and live Store ID retained; explicit durability error / 保留文件和存活 Store ID，明确持久化错误 | No new readable record / 无新增可读取记录 |

The previous executable is compiled from the preceding accepted isolation
owners for compatibility qualification. It must leave new markers and candidate
bytes untouched while an expired genuine legacy control proves its sweeper
actually runs.

This is an explicit visibility boundary: downgrade cannot settle
these new results, but does not reinterpret them as completed legacy results.
It does not establish compatibility with every historical executable or a
manually forged directory containing both marker formats.

兼容验收编译此前已接受的真实 isolation owner，让旧可执行文件处理新记录，并使用真实
过期旧记录验证清理器确实执行。降级后不能操作新结果，但不会把它误读为旧完成结果。
不据此承诺所有历史版本或人为伪造双标记目录的行为。

If even the recovery marker cannot be published, only the live Store can retry
the known ID. The error explicitly states this limitation. A successful later
retry publishes recovery identity before inspection. Files are retained, but
cross-restart discoverability is not promised when every publication fails.

Atomic publication is one-file visibility, not a power-loss transaction or
cross-file commit.

若恢复标记也无法写入，只能由仍存活的 Store 按已知 ID 重试，错误会明确说明。
后续重试必须先发布恢复身份再检查；所有发布均失败时，不承诺重启后仍可发现。
单文件原子发布不等于断电事务或跨文件提交。

## Authority and consumers / 权限与消费者

Discovery uses host-opened managed slots and the shared `bindRecord` admission:
workspace, candidate, source/common Git identities and backlinks remain checked
through scoped directory handles.

Stored paths do not authorize another slot or
workspace. The marker does not grant write permission or bypass the existing
apply tool's declared paths, permissions and conflict checks.

Consumers are Store reconstruction, `Pending`, path discovery and apply/discard;
the real `boot.Build` regression checks the model-visible failure ID, reconstructs
the Controller/session, then applies exactly that result. Both runner and Finish
errors retain their Go error identities through `errors.Join`.

发现流程复用宿主托管目录句柄与 `bindRecord`，不信任记录自报路径；不能据此获得其他
槽位或工作区权限。应用仍经过原路径声明、权限和冲突检查。真实装配测试验证模型收到
失败 ID，重建 Controller 和会话后按同一 ID 应用。runner 与 Finish 错误同时保留。

## Limits and later units / 限制与后续层

- Begin still has no durable running identity. Crashes before this error handler,
  OS execution identity, background/nested jobs and root budgets remain separate
  lifecycle work. Runner panic retention does not establish process completion.
- Successful legacy results still have their existing age policy. This change
  is not whole-store retention, archival or an automatic integration policy.
- Snapshot/empty-result fidelity remains the separate snapshot-content change;
  this slice does not claim to solve Git index cache masking.
- Existing same-ID settlement races, conditional rollback, integration journals,
  cancellation-aware inspection, immutable validation receipts and UI status
  projection remain unimplemented. Store reconstruction is not proof of process
  crash recovery or a resumed worker.
- Rooted admission is not authentication, sandboxing or filesystem pinning over
  a whole operation. Noncooperating writers still have check/use race windows.
- This author assessment does not replace independent code-owner/security review
  or resolve the original large PR's historical CodeQL findings.

本批不完成 Begin 持久身份、进程崩溃恢复、后台/嵌套预算、完整保留归档、整合事务日志、
条件回滚、验证证据或界面状态。正常旧结果的过期策略与 Git 缓存掩盖问题仍独立推进。
目录准入不是认证、沙箱或整段操作的文件系统固定；作者说明不能替代独立安全审查。

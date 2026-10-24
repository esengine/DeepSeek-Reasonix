---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Provisional write admission / 写入准入临时租约

## Reachable failure / 可达故障

A real parent can write A, start an explicit background task owning B, then try
to write B. Workspace admission succeeds before the local scheduler refuses B.

The old call cleanup decrements in-flight calls but retains the expanded extent
while the parent waits for its next model response. Another session therefore
cannot acquire B even though this call never entered a hook or Execute.

真实父任务先写 A、启动明确负责 B 的后台任务，再尝试写 B。旧工作区租约在本地调度
拒绝前已扩大到 B，调用清理只减少执行计数。父任务继续等模型时，未执行的 B 仍占用。

真实 boot.Build 回归通过通道固定该顺序，同时确认 A 的确已写入、B 未创建。

## Ownership and authority / 所有权与权限

- The existing workspacelease Owner keeps a retained extent and a set of
  identified provisional calls. An unset extent differs from the existing
  empty-path representation of the whole workspace.
- PreparePaths admits the entire normalized set through the existing registrar.
  Retain runs before possibly writing PreToolUse hooks and concrete execution;
  success, failure and partial execution then preserve legacy run retention.
- Close removes only its identified call and computes the union of retained
  paths and remaining calls. It serializes registration changes, publishes a
  smaller claim under the same registration guard and keeps the same OS lock.
  It never releases and reacquires valid older paths to shrink a claim.
- Publication failure keeps the old extent and returns an error. Agent cleanup
  exposes that error in the tool result instead of claiming resource cleanup
  succeeded. Cancellation while another registration is active also closes the
  call's in-flight identity, conservatively retaining its extent on failure.
- A completed callback cannot affect a new run. Suspended calls keep their own
  identities; resume restores the current union without promoting provisional
  paths to retained paths or resurrecting an already aborted call.
- An expired restoration returns ErrHoldEnded. Repeated restoration/cleanup returns the
  same outcome, including failures, instead of falsely reporting success.
- Registrar waits close on acquisition or refusal. Completed cleanup clears
  Waiting state; the registration guard never covers hooks, tools or models.

既有 Owner 区分已保留范围与每次临时申请。权限检查仍先执行，不扩大权限或工具集合。
进入可能写入的 hooks 或 Execute 前才保留新增范围；错误不等于没有写入。清理按调用身份
重新计算并集，在原注册临界区发布缩小后的范围，不释放已有有效 OS 占用再重新申请。

发布失败保守保留旧范围并报告错误。取消不能留下失去所有者的 in-flight 计数。
等待用户后的恢复保留每次调用身份；旧回调不能释放新一轮的资源。

## Entry points and evidence / 入口与证据

- The common Agent execution owner covers parent/child, direct tools and resolved
  capabilities without changing their schemas. Controller frontends inherit it.
- The boot regression keeps the parent and background task alive in actual model
  requests, then admits an independent session to B while requiring A to remain
  protected. Direct and use_capability calls both exercise this sequence.
- Owner tests cover overlapping preparations, concurrent retention and upgrades,
  complete-set refusal in opposite order, cancellation, whole versus no extent,
  explicit legacy acquisition, Yield/resume, repeated cleanup and newer runs.
- Separate OS processes verify that a pending path is excluded, an aborted path
  becomes available and a previously written path remains protected. The existing
  owner suite also exercises process death and legacy exclusive-lock exclusion.
- Execution tests physically change a file and then return a tool error or hook
  denial. Neither outcome is treated as an unexecuted write for lease cleanup.
- Serialized real provider requests compare tool definitions and system prefixes.
  Local results, hosted platform checks and independent review are recorded
  separately in the PR; author analysis is not independent approval.

统一执行所有者覆盖父子代理和动态工具，不按名称增加旁路。真实装配验证被拒绝的 B
释放而 A 保留；跨进程探针验证磁盘协议而非仅测试内存对象。部分写入与 hook 写后拒绝
使用实际文件变化验证。完整包、race、平台和独立审查证据分别记录，不相互冒充。

## Compatibility / 兼容

| Contract / 合约 | Existing reader/writer / 既有行为 | New behavior / 新行为 |
| --- | --- | --- |
| Claim JSON and locks | Same paths/holder/sessionId fields and OS locks | Same format; only the current record's admitted extent may shrink |
| AcquirePaths, HoldPaths | Retain paths until the final participating run | Retention preserved; active call also protects against premature EndRun |
| Whole/optimistic/folder policy | Existing normalization and undeclared-write rules | Rules unchanged; an aborted whole claim can restore an earlier named extent |
| Mixed processes | Legacy shared/exclusive workspace protocol and live records | Existing exclusion retained; old processes may still hold long claims |
| Yield | Release while asking, restore on resume | Restore current retained/provisional union; aborted calls do not reappear |
| Configuration, wire, tools | Existing fields, defaults and permissions | No field, schema, default or authorization change |

没有新增持久格式或迁移；不改写其他会话的记录。旧进程继续遵守原有锁协议，但旧程序
自己的未执行占用缺陷不会被新版单方面修复。未声明范围、额外目录和路径身份的既有
保障边界不因本批而扩大。

## Remaining boundaries / 未完成边界

This is admission cleanup in the current policy, not short transactions after
successful writes. A genuinely entered write still retains legacy protection
through its run, and unknown hooks still require their conservative extent.

It does not implement CAS, fair queues, automatic worktrees, frozen validation,
integration journals or crash-recoverable isolated task lifecycles. External
writers can bypass this cooperation protocol. No new default is enabled.

Retention after a hook-side failure is conservative lock protection. Unified
mutation outcomes and hook-side frontend/evidence invalidation remain in the
later effect layer; this slice does not make tool Executed describe hook work.

本批只清理未进入写入阶段的临时申请。已执行写入仍遵守旧的任务保留周期，不宣称完成
短事务、CAS、公平排队、自动隔离、冻结验证、整合日志或隔离任务持久恢复。外部进程
可绕过合作协议，整套并发根治计划仍需后续可独立审查批次完成。

hook 失败后的保留属于锁保护；统一变更结果以及 hook 对界面、验证证据的失效通知仍属
后续影响层。工具的 Executed 标志不能同时代表 hook 已运行。

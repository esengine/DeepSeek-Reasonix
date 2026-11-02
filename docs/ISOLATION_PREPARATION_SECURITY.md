---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Store-owned candidate preparation / 结果存储拥有的候选准备

## Scope / 范围

`Store.Prepare` captures one Git snapshot. Its process-local `Preparation`
creates independent candidates from that snapshot through `Begin`. The source,
workspace, managed root and result IDs come from the Store and its existing
creation owner; callers do not pass an arbitrary snapshot or candidate path.

Existing `Store.Begin` still captures a fresh snapshot per call. Its snapshot
and candidate creation bodies share private helpers with the new entry point.
No runtime caller uses the new preparation API in this change.

宿主可先捕获一次 Git 快照，再从同一个结果 Store 创建多个独立候选。调用者不能向
准备对象传入任意快照或候选路径。原 `Store.Begin` 仍逐次捕获新快照，仅提取了共享
私有实现；本次没有运行时接线或默认策略变更。

## Input, authority and effects / 输入、权威与副作用

- The host constructs a Store with its selected Git repository, workspace and
  managed root. `Prepare` takes only a context. The returned object's fields
  are private; nil and zero preparations refuse creation.
- Preparation checks that the Store workspace is the captured source's actual
  directory or selected subdirectory using the existing rooted identity helper.
  Mismatched host configuration is refused before creating a candidate.
- It records filesystem identities for source, selected workspace, Git common
  and Git administrative directories. `Begin` rechecks them before creating a
  candidate; matching path spelling alone does not accept replaced directories.
- Existing Git repository/backlink validation still runs in candidate creation.
  Context cancellation is checked before capture and before candidate creation.
- A snapshot operation writes dangling Git objects. Candidate creation writes
  a managed worktree and its Git administration. This is not a pure read or a
  filesystem transaction, although it does not apply work into the source.
- Each candidate gets its own existing result ID and worktree. There is no
  group-wide abort, automatic Finish or publication of running results.

源目录、所选工作区域与 Git 元数据目录会核对实际目录身份，创建候选前再次检查。
相同路径下替换目录不能仅凭名称继续使用准备对象；既有 Git 回链校验仍执行。

捕获会写 Git 对象，创建会写托管工作树与管理信息，不是纯读取或文件系统事务。
每个候选拥有独立 ID；本次不自动终结整批候选，也不发布运行中身份。

## Compatibility / 兼容性

| Surface | Behaviour |
| --- | --- |
| Existing `Store.Begin` | Fresh capture per call; existing arguments and error policy |
| Prepared candidates | One captured Git commit/tree, distinct IDs and worktrees |
| Finished `entry.json` | Existing encoding, readers, apply/discard and discovery |
| Failed inspection | Existing inspection-v1 owner and fence; no new state |
| A preparation during restart | No persisted representation; not discoverable |
| Returned `Entry` | Existing trusted-host mutable type; no new unforgeability claim |

No persisted version or field, wire payload, permission, tool definition or
provider-visible prefix changes. Old readers handle finished candidates as
ordinary results. An un-Finished candidate retains the existing crash window;
this API does not establish durable Begin or running-result recovery.

已有完成记录格式、检查失败记录、恢复读取器和 apply/discard 接口不变。准备对象仅
存在于进程内，重启后不能重建它。未 Finish 的候选仍有原有崩溃窗口，不是持久化
Begin、运行中恢复或新的权限协议。

## Evidence / 验证

Tests create two candidates after the source changes and assert their original
captured Git baseline. An ordinary Begin beside them sees the new source.
Concurrent creation yields separate IDs and candidate state; a write in one
candidate reaches neither its sibling nor the source.

Selected-subdirectory identity, mismatched Store configuration, nil/zero
objects, cancellation and real directory replacement are covered. Finished
prepared results are discovered by a reconstructed Store, refuse an ordinary
stale apply and remain available for existing discard.

Unsafe compiled controls can deliberately refresh the baseline or ignore
recorded directory identity. These test the new API's promises; the previous
release has no preparation API, so they are not preceding-product defects.

测试覆盖源变化后的同快照候选、原 Begin 的新快照、并发独立性、子目录、错配配置、
取消和真实目录替换。准备出的完成记录可由重建 Store 发现，普通变化导致的过期应用
仍拒绝，随后可用既有 discard 处理。故意削弱新接口的编译对照不冒充旧版缺陷复现。

## Limits and next owner / 边界与下一层所有者

A captured Git tree is not a complete immutable filesystem view. Existing stat
cache, hidden edit, ignored-file, filter, symlink and native metadata boundaries
remain; [snapshot content work](https://github.com/esengine/DeepSeek-Reasonix/pull/14371)
is a separate pending review.

Stable Git identities do not prove identical
native bytes across changed checkout configuration.

Directory identity checks are observations before Git operations, not held
authority over every subsequent access. They do not fence an arbitrary
external writer or establish sandbox/private-worktree isolation. The Store's
managed directory remains trusted under its existing creation contract.

The snapshot's dangling objects are not pinned against Git garbage collection.
Creation/partial cleanup errors retain existing semantics; this change does not
repair the separate ignored-cleanup-error, retention or settlement-journal
gaps. Capturing once does not implement budgets, CAS or a short transaction.

Best-of has not been connected. Its runner panic still needs a separate
goroutine boundary and an uninspected-result owner before uncertain work can
be kept out of judging and automatic cleanup. This preparation API alone
neither catches that panic nor makes a result durable before execution.

Git 快照不等于完整冻结的文件系统；隐藏编辑、过滤器、忽略文件和原生元数据等边界
仍在，快照内容修复另行待审。目录身份只是 Git 操作前的观察，不会约束任意外部
写入者，也不构成沙箱证明。托管目录仍需可信。

本次不固定 Git 对象的 GC 寿命，不修复既有部分创建清理、回收或结算日志缺口，
不建立额度、CAS 或短事务。best-of 尚未接入，其 goroutine panic 与未检查结果保留
仍是下一层工作。本文是作者安全评估，不代替独立审查。

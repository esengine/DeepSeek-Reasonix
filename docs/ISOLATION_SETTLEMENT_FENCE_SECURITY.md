---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Isolated settlement fence / 隔离结果整合屏障

## Purpose and limits / 目的与边界

This prerequisite teaches ordinary result writers to stop at an external
settlement record. It creates no record and does not implement application,
recovery, retention or journal serialization. No current normal task starts
using the future record protocol.

这是后续持久日志的读取方屏障：看到候选目录之外的记录时，普通写入必须停止。
本批不创建记录，不完成日志、恢复、保留或自动整合，也不让正常任务切换协议。

The existing cleanup failure remains: `removeLocked` forgets legacy markers
before ignoring candidate-removal errors. Returning an error or keeping a
marker alone cannot recover partially deleted Git state. A complete repair
still needs retained immutable bytes, manifests, progress and conditional recovery.

现有清理失败窗口仍未修复；后续必须保留不可变输入、文件内容和逐文件进度，并支持
有条件恢复。这里的拒绝不能被描述为清理失败已根治。

## Authoritative owner / 权威所有者

`acquireResultOperation` checks the fence **after** acquiring the existing
per-result OS owner and checking cancellation. A blocked call cannot rely on
its earlier view. Refusal releases only its own lease.

共享操作所有者在取得按结果 ID 的 OS 租约后才检查，排队前的观察不能决定准入。
拒绝时只释放本次占用，不删除锁文件、恢复记录或他人的锁。

The same boundary covers Apply, Discard, Paths inspection (which may remove an
empty result), Finish, Abort and expired-result sweeping. Ordinary read-only
pending projection continues using the existing legacy records.

同一入口覆盖应用、丢弃、路径检查、Finish、Abort 和过期清理。路径检查可能删除
空结果，因此也是受控入口。待处理列表仍沿用原记录，本批不增加新日志发现能力。

## Namespace contract / 命名空间契约

- External namespace: `<managed-root>/.settlements-v1/`.
- Per-result filename: the existing normalized result-lock SHA-256 key, with
  `.json` replacing `.lock`. No record-supplied path is consulted.
- Missing namespace/entry preserves ordinary legacy behavior. Any existing
  per-result entry, including a directory or symlink, refuses ordinary operations.
- This reader never reads record contents. Malformed, empty and future versions
  all remain uninterpreted and preserved, rather than falling back to legacy writes.
- Namespace metadata/open failures refuse admission. A symlink or file in place
  of the namespace is rejected. All metadata operations are relative to host-opened roots.
- The namespace is outside the removable candidate slot, so partial candidate
  deletion does not remove the fence. Its absence does not prove a candidate exists.

命名空间及文件名由宿主决定，记录内容完全不解释。任何记录条目都拒绝普通写入；
坏记录不能降级回旧协议。宿主句柄约束元数据访问，不按记录中的路径读取文件。
候选目录消失后屏障仍可保留，但本批不恢复候选内容或补回待处理列表。

## Writer and downgrade compatibility / 写入方与降级兼容

| Writer / 写入者 | No fence / 无屏障 | Fence present / 有屏障 |
| --- | --- | --- |
| Preceding writer | Existing behavior | Ignores the new fence; compiling counterexample executes/destructively cleans results. |
| This ordinary writer | Existing behavior | Refuses under per-result ownership and preserves bytes. |
| Future recovery writer | Not implemented | Must validate the manifest and recovery authority under ownership; this slice grants no bypass. |

The preceding writer is not retroactively fenced. Active journal publication
must establish the minimum cooperating writer protocol before use. Keep the
compatibility fence namespace even if a future manifest schema changes; a
new schema namespace alone cannot teach an earlier writer to stop.

更早程序仍忽略屏障，不能靠新锁单方面获得跨版本安全。实际发布日志之前必须明确
最低协作写入协议；后续格式升级也必须保留兼容屏障，不能只更换目录名。

Future writers must publish/check under the same stable per-result mutex,
including Finish/Abort/sweep. That mutex alone is insufficient: every writer
must recognize the persistent fence. Namespace replacement/removal and arbitrary
external filesystem writers remain outside this cooperative guarantee.

后续所有变更入口都必须持有同一租约并识别持久屏障；仅有互斥锁不够。不能删除或
替换仍被协作写入者使用的命名空间，也不承诺约束任意外部文件系统写入。

`inspection.json` version 1 still means ended-runner inspection. This change
does not overload it with running, applied or recovery semantics. The future
manifest's contents and writer implementation remain separate review units.

原 inspection v1 的含义不变，不把它扩展成运行中或整合状态。屏障存在时应由兼容
恢复所有者处理，不能建议用户删除记录来绕过拒绝。

## Evidence / 验证

Real result fixtures cover all destructive siblings, a queued operation seeing
a later fence, another result progressing, malformed metadata, a directory entry,
candidate disappearance and an actual child-process Apply. Refusal is followed
by nonqueuing acquisition to check that it released its own new OS lease.

真实 Git 候选和工具入口覆盖各变更路径；固定排队顺序验证锁后复查，真实子进程
验证跨进程读取同一屏障。另一个 ID 继续执行，未知字节保持原样，拒绝后可再次
非排队检查，避免以遗留占锁实现拒绝。

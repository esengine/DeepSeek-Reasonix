---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# Worktree content snapshot review / Worktree 内容快照审查

## Purpose / 目的

This is the author's security assessment of the content-snapshot slice extracted
from #14133 at `467a61668a0781cab171536158536283d931e4d9`. It is independent of
the transaction-library PR #14368. It is not independent security approval.

本批修复已有快照入口受 Git stat 缓存影响的问题，独立于事务基础库。本文是
作者审查，不代表独立安全审批，也不替代后续隔离、整合及恢复层的审查。

## Reference tables / 边界对照

| Value / 输入 | Owner and use / 所有者及用途 |
| --- | --- |
| Repository and Git index path | Existing resolved `gitcmd.Repo` and `rev-parse --git-path index`. Existing copy reads that index. No model-provided arbitrary index path is added. |
| Private index | Existing `os.MkdirTemp` directory and fixed `index` leaf. `GIT_INDEX_FILE` is passed as environment data to every new Git command. Only this private index is removed/rebuilt. |
| Tree identity | `write-tree` output from the private index is passed as one argument to `read-tree`. No shell string concatenation or new shell executable is introduced. |
| Workspace bytes | Existing `Repo.StageAll` reads the authorized source through the established Git driver. Git configuration/filter policy is not broadened. |
| Source Git state | The real index, HEAD and refs are not updated. Writing unreachable tree/commit objects remains an existing snapshot side effect governed by Git's own object publication. |

各路径来自现有宿主解析的仓库身份或新建的私有临时目录。新命令仍经现有 Git
驱动执行；参数不作为 shell 脚本拼接。真实索引和 refs 不参与私有索引重建。

The existing driver disables Git hooks/fsmonitor and submodule recursion for
`read-tree`. The extra invocations inherit that policy. Index rebuilding and
rehashing add work; this slice makes no capture-latency improvement claim.

## Rules and failure / 规则与失败

1. The copied index MUST first preserve its staged tree, including explicitly
   staged ignored files. Rebuild from that tree before staging current bytes,
   so trusted size/mtime cache entries cannot select stale content.
2. An unmerged copied index MUST be staged from working bytes before its tree
   can be written. This records conflict markers as bytes; it does not resolve
   or rewrite the user's actual conflicted index.
3. Every new Git invocation MUST retain the existing driver and deadline.
   Failure returns an error. The temporary directory is removed by its existing
   owner; Git object cleanup remains ordinary Git garbage collection.
4. Callers MUST still coordinate the source during capture. This repair does
   not make a directory scan atomic against concurrent external writers, grant
   access, isolate execution or issue a verification receipt.
5. Existing snapshot/result formats remain unchanged. No metadata field,
   migration, settings change or automatic-isolation policy is introduced.

规则保证私有索引重建时保留已暂存的忽略文件，并处理真实冲突状态。它没有把
目录扫描变成跨文件原子快照；调用者仍需协调源文件写入。失败必须返回错误，
不能把不完整捕获当作有效验证证据。

## Verification / 验证

1. `TestSnapshotHashesBytesDespiteTrustedCachedStat` covers trusted size/mtime,
   `assume-unchanged` and `core.ignorestat`. Old code captures old bytes; fixed
   code captures new bytes, keeps staged ignored input, and preserves index/HEAD.
2. `TestSnapshotRecordsAWorkspaceMidConflict` uses a real merge conflict and
   checks both captured conflict-marker bytes and unchanged real index bytes.
3. The complete worktree race suite covers neighbouring creation, cancellation,
   filtering, relative roots, application and rollback behavior. Platform and
   broader assembly results are reported separately in the PR.
4. `TestApplyRejectsStatCacheHiddenSourceEdit` reproduces an actual stale
   candidate overwriting a hidden user edit with the old code. The repair
   returns `ErrWorkspaceMoved` and keeps the user's bytes.

回归先证明旧实现在真实 Git 仓库中捕获旧内容，再验证修复后内容、忽略文件、
索引及 HEAD 均符合预期。其他平台和装配层结论按各自实际测试结果记录。

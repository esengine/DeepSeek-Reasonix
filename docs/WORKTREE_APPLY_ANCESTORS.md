---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-28
---

# Worktree apply ancestors / 工作树应用的父目录边界

## Behavior / 行为

An isolated candidate names Git directory entries. If a source ancestor becomes
a directory link while that candidate runs, matching leaf bytes do not establish
that the candidate still addresses the original path.

Applying now refuses that
ancestor with the existing path-conflict result before reading leaf content or
writing a sibling change. Links inside the repository are refused too, including
links from a selected project folder to its surrounding repository.

隔离候选使用 Git 目录项路径。候选运行期间，如果源文件的父目录被换成目录链接，
即使最终文件内容相同，也不能认定仍是原路径。应用现在会在读取最终文件内容或写入
其他变更前，以已有的路径冲突结果拒绝该父目录。仓库内部链接也会被拒绝，包括从所选
项目目录指向仓库其他目录的链接。

Deletion ancestors are checked in the shared worktree publisher, including
ordinary isolation and best-of callers. Path-by-path apply checks every changed
path's ancestors before hashing leaves.

Deletion uses an open `os.Root` for the source repository, so a parent
link cannot redirect that filesystem operation outside the opened root. A link
at the final deleted entry is removed as an entry; its target is not removed.

Whole-workspace application may delete a file before creating a directory at
the same name. Its deletion preflight preserves that conversion; Git owns the
subsequent restoration. Path-by-path application's existing refusal of
unsupported tree entries is unchanged.

共同 worktree 发布层检查删除路径的父目录，覆盖普通隔离和 best-of 调用方。逐路径应用
还在计算文件摘要前检查所有变更路径的父目录。

删除通过源仓库的已打开 `os.Root` 执行，父目录链接不能将该删除
操作重定向到根目录外。被删除的最终目录项本身若是链接，只删除链接，不删除其目标。

整工作区应用可能先删除一个文件，再创建同名目录。删除前置检查保留这一转换，后续
恢复由 Git 负责。逐路径应用原有的“不支持的 tree 条目”拒绝规则不变。

The existing apply tool reports the conflict and retains the original result ID
and candidate. After restoring the directory, reopening the Store can apply that
same ID. New directories, ordinary deletions, and unrelated workspace edits keep
their existing behavior.

No configuration, tool schema, prompt prefix, journal,
or persisted format changes; existing results use the checks when applied by the
new reader. Older binaries retain their previous behavior.

已有应用工具报告冲突并保留原 result ID 和候选。恢复目录后，重新打开 Store 可使用
同一 ID 应用。新建目录、正常删除及工作区其他修改保留原行为。不修改配置、工具
schema、提示词前缀、journal 或持久化格式；新版本应用旧结果时同样执行检查。
旧二进制仍保留原行为。

## Evidence and limits / 证据与限制

Regression fixtures create real Git candidates, then replace source ancestors
with external, internal, dangling, or non-directory entries. They check both
deletions and writes, sibling preservation, and Store reconstruction followed by
same-ID retry.

Windows fixtures use directory junctions; Unix fixtures use
symbolic links. The existing Windows isolation inventory requires every named
regression to pass, with no missing, duplicate, failed, or skipped terminal.

回归测试创建真实 Git 候选，再将源父目录换成外部链接、内部链接、悬空链接或普通文件，
覆盖删除、写入、其他文件保持不变，以及重建 Store 后用原 ID 重试。Windows 使用目录
junction，Unix 使用符号链接。已有 Windows 隔离清单要求每个指定测试通过，拒绝缺失、
重复、失败或跳过的终态。

These are ancestor-type preconditions and confined deletion, not an atomic
multi-file transaction or an inode compare-and-swap. A directory replaced by
another real directory containing identical files is not distinguished.

Checks cannot fence arbitrary writers between inspection and Git restoration. File
restoration still uses Git, and errors after publication begins may leave partial
changes.

Physical process settlement, remote effects, mount changes, complete
native publisher ownership, and crash-safe rollback remain separate work.

本修补提供父目录类型前置条件及受根目录约束的删除，不是多文件原子事务或 inode CAS。
真实目录被换成包含相同文件的另一真实目录时，不会区分。

检查无法阻止任意外部写入者
在检查与 Git 恢复之间修改路径。文件恢复仍由 Git 执行，开始发布后的错误可能留下部分
变更。进程物理终结、远程效果、挂载点变化、完整原生发布所有权与崩溃安全回滚仍是
独立的未完成工作。

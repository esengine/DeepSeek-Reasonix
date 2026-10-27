---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Worktree entry identity / 工作副本条目身份

## Cause and owner

ApplyPaths compared blob IDs alone. Executable-mode changes keep the same blob;
regular files and symbolic links can also have identical stored bytes. This
could discard a candidate's metadata or overwrite a source metadata change.
The shared reconciliation owner now compares Git mode and blob together.

This is a file identity repair, not an atomic comparison-and-swap or filesystem
sandbox. It does not introduce transaction activation, journaling or a new
collaboration policy.

## Source, sink and authority

| Input | Validation and use |
| --- | --- |
| Snapshot/candidate trees | Existing host-resolved repository; ls-tree mode and blob form one comparable identity |
| Live paths | Existing ChangePaths scope checks, Lstat kind, Git hashing with the snapshot's filter rules |
| Real symlink | Hash the link target without regular-file filters; never identify it as a regular file with equal bytes |
| Executable bit | Git's owner executable bit, not group/other bits or arbitrary permission flags |
| Checkout preferences | Read core.filemode/core.symlinks as booleans through the existing hardened Git launcher |
| Unsupported entries | Refuse unsupported tree/file kinds before source writes, rather than treating them as absent |
| Write sink | Existing Git restore/deletion implementation, after every changed path passes reconciliation |

No new shell, command resolver, network endpoint or grant is added. Config keys
are host constants. Tree IDs and scoped paths keep their existing authority
checks. The user's index is read when needed and remains byte-for-byte unchanged.

## Checkout compatibility

Git can retain index modes on filesystems where executable bits or symlinks are
not trusted. Read that logical mode when core.filemode or core.symlinks is false;
do not pretend every ordinary Windows checkout lost its tracked executable mode.

A regular file representing an indexed symlink uses the same regular-file
filter path as Git's snapshot staging. Actual symlink targets use raw hashing.
Tests distinguish these policies with CRLF payloads and text attributes. This
is Git's logical identity; it is not a promise of unfiltered snapshot bytes.

When a new mode/type transition cannot be represented without changing the
source index under those preferences, refuse before writing any path. Preserve
already-target idempotence and existing-mode content edits. Never report an
unmaterialized transition as applied.

| Surface | Compatibility |
| --- | --- |
| Result and snapshot records | No schema changes; existing and unknown fields retain their existing handling |
| Tool/public APIs | No schema, definition, prefix or signature changes |
| Existing regular content edits | Same blob/filter semantics, with mode now included |
| Executable/link transitions | Applied when representable, refused when checkout policy cannot safely materialize them |
| Unsupported Git entries | Explicit error; no silent equivalence with an absent file |
| User configuration/index | Read only; no migration, default rewrite or staging |

## Evidence and limits

Real Git candidates reproduce chmod-only loss, source chmod overwrite,
same-byte regular/link loss and source link overwrite. Public apply-tool tests
also assert conflicted identity/candidate retention and no sibling mutation.
Positive cases check already-target idempotence.

Compatibility tests cover ignored file modes, changed index modes, ordinary
content writes, unsupported transitions, index preservation, link emulation and
owner-only executable bits. Gitlink refusal checks no sibling was written.

POSIX fixtures and portable checkout-emulation cases have separate platform
boundaries. A skipped POSIX test is not native Windows evidence.

The prior shared owner compiles with the behavior regressions and fails them.
Author tests and this assessment do not substitute for independent security
review or new-head hosted/native checks.

The snapshot's cache correctness remains a separate pending repair. Shared
source-path leases, actual-byte CAS, hostile path aliases, external writers,
partial application, crash recovery and validation evidence remain separate
work. This change does not close the comparison-to-write race window.

## 中文说明

旧实现只比较内容哈希，无法区分执行位变化，也可能把内容相同的普通文件与符号
链接当成同一版本。现在三方比较同时使用 Git 模式和内容；发现源文件的模式或类型
变化时拒绝整批应用，保留候选结果，避免覆盖用户修改。

兼容 Windows 等检出方式：Git 不信任执行位或符号链接时，读取索引记录的逻辑模式。
普通文件形式的链接按 Git 实际暂存规则过滤，真实符号链接目标按原始文本哈希。
无法在不改用户索引的前提下表示的新模式或类型转换会明确拒绝，配置与索引不被重写。

回归覆盖真实 Git 候选、公开应用工具、失败时结果保留、不写相邻文件、成功后幂等、
执行位策略、链接模拟和不支持的 Git 条目。POSIX 与可移植用例分开记录平台证据。
此修复不等于原子 CAS、沙箱、可靠快照、日志回滚或完整恢复；外部写入竞争仍有窗口。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Literal Git file selection / Git 字面文件选择

## Root cause and reachability

An admitted filesystem name is not a Git pathspec. The `--` separator ends
option parsing but still permits wildcards, bracket patterns and pathspec magic.
ApplyPaths passed the same string to validation, inspection and restore, yet
Git interpreted a wider set than the host's declared write paths.

A real candidate named `*.txt` restored unrelated a.txt baseline content over a
later user edit. The public apply tool declared only the wildcard's literal
filename. A leading pathspec-magic filename could instead be silently dropped.
These are disposable local regressions, not claims of an observed user incident.

## Repair owner and data flow

| Boundary | Contract |
| --- | --- |
| Path scope | Existing ChangePaths remains the permission-scope owner |
| Git tree inspection | Every admitted path becomes an explicit `:(literal)` selection |
| Logical mode lookup | Index paths use the same literal selection rule |
| File hashing input | Each filename is Git byte-oriented C-quoted, preserving quotes, backslashes, control bytes and UTF-8 |
| Source restore | One shared writeChanges owner serves path-wise and whole-workspace application, with literal selections |
| Deletion | Existing exact filesystem path deletion; no pattern expansion |

No shell interpolation is introduced. Git receives argument arrays through its
existing hardened launcher. The host selects literal syntax, not the model.
This prevents a filename from becoming a second selection program inside Git.

The shared host launcher removes four ambient pathspec-selection variables.
In particular, inherited case-insensitive selection must not widen a literal
host request. Explicit host-selected command flags and intentional exclusions
remain available; the parent shell's environment is not modified.

Hash-object's stdin-paths protocol is line-delimited and understands C quoting.
Joining raw filenames with newlines can split one authorized path or invoke
quote interpretation. Encode bytes, including non-ASCII/control bytes, rather
than using Go Unicode escapes that Git's parser does not implement.

## Compatibility

Valid filenames keep their literal meaning, including brackets on Windows and
wildcards, colons or newlines where the OS permits them. They are not globally
banned to work around Git semantics. Names the OS cannot represent are explicit
test boundaries, not a claim of Windows support for POSIX-only names.

Intentional pathspec consumers such as StageAll's nested-repository exclusions
keep their semantics. Only host Git child environments drop ambient selection
overrides; no persisted format, tool definition, grant or default changes.

| Surface | Behavior |
| --- | --- |
| Ordinary names | Existing application, deletion and conflict semantics |
| Special names | Exact file selected and hashed; no expansion to siblings |
| Quotes/newlines/UTF-8 | Quoted hashing input retains one filename per record |
| Source index/config | Unchanged by application |
| Old result records | Same schema and identity admission |
| Whole-workspace Apply | Reuses the shared literal write owner after its existing whole-baseline check |

## Evidence and limits

Real Git tests cover literal wildcards, brackets, leading magic/quotes/dashes,
spaces, embedded newlines/tabs, backslashes and Unicode. Repeat application
checks idempotence; source conflicts check no sibling write. Both trusted and
index-derived modes run, so the mode-reader sibling is exercised too.

Public Store/tool tests assert the exact declared path, preservation of an
undeclared source edit and correct literal result bytes. Whole-workspace Apply
uses the same write path. Compiling predecessor owners reproduce the failures;
ordinary and already-correct cases remain controls.

Literal selection does not supply path-alias confinement, symlink-parent safety,
atomic content CAS or coordination with arbitrary external writers. Existing
ChangePaths restrictions and their hardening are separate work.

Multi-file partial failures, journals, conditional rollback, crash recovery and
snapshot-bound verification remain open. No sandbox claim is made.

Author analysis and local tests do not substitute for independent security
review, CodeQL or actual new-head native platform execution.

## 中文说明

文件名中的 `*`、括号或 Git pathspec 前缀原本可能被 Git 当作选择语法，导致写入
未声明的相邻文件，或把候选字面文件误认为已应用。`--` 只结束选项解析，不能解决它。

现在树检查、索引模式查询和实际 restore 统一使用显式字面路径；全工作区应用也复用
同一写入所有者。哈希输入按 Git 的字节级 C 引号编码，避免换行或引号拆成其他文件。
正常文件名与系统支持的特殊文件名仍可用，暂存区、配置、权限、工具定义和默认方式不变。

回归验证公开应用工具的声明路径、用户相邻修改不被覆盖、精确候选落盘、冲突拒绝和
重复应用。此修复不是沙箱、别名防护、原子 CAS 或完整恢复；外部写入竞争和日志层仍待完成。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-26
---

# Host Git storage environment / 宿主 Git 存储环境

## Behavior / 行为

Host Git commands now remove ambient `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY`
and `GIT_ALTERNATE_OBJECT_DIRECTORIES` before repository discovery, driver
inspection and execution. Detached URL commands remove them too, whether their
environment was explicit or inherited.

宿主 Git 命令在仓库发现、驱动检查和执行前移除上述三个继承变量。按 URL 运行的
Detached 命令也移除它们，包括显式环境和继承环境两种入口。

Pinning a Git directory and common directory alone does not choose its index or
object storage when these variables override them. See the official
[Git repository environment contract](https://git-scm.com/docs/git#_the_git_repository).

An inherited index could be overwritten while preparing another worktree;
objects could be written outside the resolved repository or found only through
an ambient alternate directory.

仅固定 Git 目录和 common 目录，不能覆盖这些变量指定的索引和对象存储。旧实现
创建另一个 worktree 时可改写外部索引；对象也可写入其他目录，或从继承的备用目录
读取。这里修复宿主命令的环境准备，不把 cwd 或独立索引当作完整隔离。

## Authority and compatibility / 权限与兼容

| Boundary | Contract / 约定 |
| --- | --- |
| Ambient storage variables | Removed from host Git constructors and Detached / 两个共享入口均移除 |
| Explicit host override | Trusted callers can append a private index or object directory after construction / 可信宿主仍可在构造后显式指定 |
| Repository alternates | Existing objects/info/alternates remains effective / 仓库声明的备用对象库仍有效 |
| User shell commands | Their environment is unchanged / 不改用户 shell 环境 |
| Repository selection | Existing Open and Repo identity rules remain / 沿用仓库解析与固定身份 |
| Credentials and transport | Existing secret filtering, SSH and network policy remain / 沿用密钥过滤、SSH 和网络策略 |

Host commands previously relying on ambient storage relocation now use the
resolved repository's storage. There is no migration of an existing external
index or object database. Explicit private-index snapshot staging remains
available. Legitimate linked worktrees still share their repository's common
object database.

此前依赖继承变量重定向存储的宿主命令，现在使用已解析仓库的存储；不迁移或修复
已有外部索引和对象库。快照的显式私有索引仍有效；合法 linked worktree 仍共享
仓库 common 对象库。

Only three storage variables are covered. This is not a blanket Git environment
allowlist, a sandbox, or authority against trusted explicit overrides, arbitrary
shells, other writers, repository configuration or declared object alternates.
No API, config, wire, persisted format, permission or model prefix changes.

仅覆盖三个变量，不是完整 Git 环境白名单或沙箱，也不约束可信显式覆盖、任意
shell、其他写入者、仓库配置和仓库声明的备用对象库。不改 API、配置、传输、持久化
格式、权限或模型前缀。

## Evidence and remaining work / 证据与未完成项

Disposable real Git fixtures exercise object writes, ambient alternate reads,
private-index staging, explicit object directories and declared alternates.
Delivery and candidate preparation preserve an existing ambient index and do
not create an absent one.

Actual Boot delegates an isolated write and applies
it, checking source-index bytes and that same ambient-index boundary.

真实临时 Git 夹具覆盖对象写入、继承备用目录读取、私有索引、显式对象目录及仓库
备用对象库。交付和候选 worktree 准备保留外部索引且不新建缺失索引。真实 Boot
委派隔离写入并应用，核对源索引字节和相同外部索引边界。

These are constructed regressions, not an observed user incident. They do not
establish complete snapshots, actor CAS, hardlink protection, physical process
quiescence, durable publication, cleanup recovery or the complete #14025 plan.
Native execution and hosted results belong to each exact PR head separately.

这些是构造回归，不是已观察到的用户事故；不能证明完整快照、逐 actor CAS、硬链接
防护、物理进程静默、持久化发布、清理恢复或整个 #14025 方案完成。平台执行和 CI
必须按各 PR 的精确 head 单独核验。

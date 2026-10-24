---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Collaboration selection and migration / 协作策略选择与迁移

## 中文

新安装策略的实现与验收独立于旧配置解码。`Default()` 继续作为兼容解码基线，
不能仅凭构造了一个默认配置对象就认定用户选择了自动隔离。

首次可写加载时，没有用户配置及有效历史的配置目录选择 `auto`，并在初始化其他
状态前持久化 `collaboration-default-v1.json`。只读加载预测该选择，但不创建文件。
日志、缓存、临时文件、运行记录目录及空目录不代表旧用户历史；已存在的会话、
凭证、用户文件及重定位状态目录中的有效历史会保留兼容策略。

多个进程首次启动时通过独立 OS 锁协调，完整 marker 以排他创建发布。
五秒超时只终止初始化等待，不删除锁、不夺取活动所有者。
未知版本、字段、尾随 JSON 或非普通 marker 保留并报告错误，不能静默启用写入。

任何已有用户配置只要缺少 `agent.collaboration`，都继续使用 `legacy`，包括导入
新安装目录的旧配置。该判断绑定实际读取的配置快照；配置在默认选择后出现，也
不能继承 `auto`。加载不会删除旧开关或改写用户配置原文。

设置中可明确选择 `auto`、`shared`、`isolated` 或 `legacy`。保存兼容选择使用显式
`legacy`，避免空值在新安装目录中再次被解释为自动选择。用户设置只影响新建或
重建的运行时；接口分别提供保存值与当前聊天有效值。项目文件不能替换这些选择。
首次保存用户配置会保存已选策略，并保留未知配置字段；用户配置文件使用私有权限。

自动模式需要宿主能运行具有完整文件变更边界的程序。宿主缺少所需后端时，在创建
自动候选前说明原因，回退共享短事务。Windows 当前创建限制令牌不足以覆盖删除和
重命名，因此默认自动任务使用此回退；不能因配置了容器验证器而声称候选 Bash
已经具备完整边界。

显式 worktree 的进程内文件工具及已保留现场继续按原协议使用，
程序启动仍独立核验实际写根。已绑定环境不会在续跑时静默迁回源工作区。

旧程序可能忽略安装 marker，或者拒绝显式的新策略。参与旧 claim 协议的进程仍与
新事务互斥，但不理解整合恢复日志的旧程序不属于安全降级写入范围。切回兼容策略
前必须结束活动操作，并处理未完成整合；不能删除日志来绕过恢复屏障。

完整协议及保障边界见[并发说明](CONCURRENT_MUTATIONS.md)和
[协议兼容矩阵](CONCURRENT_MUTATIONS_PROTOCOL.md)。实现这一迁移机制本身不表示
全部平台、故障及性能验收已完成；默认启用以最终验收记录为准。

## English

Installation selection is separate from decoding existing configuration.
`Default()` remains the compatibility decoding baseline; constructing it never
establishes a user choice to isolate automatically.

The first mutable load selects `auto` only when the profile has no user config
or meaningful history. It publishes `collaboration-default-v1.json` before
initializing other state. Read-only loads predict the choice without writes.

Logs, caches, temporary/runtime directories and empty directories do not count
as user history. Sessions, credentials, user files and relocated persistent
state retain compatibility behavior.

An OS initialization lock coordinates concurrent first starts. The complete
marker uses exclusive publication. Its five-second wait limit never revokes an
active owner. Unsupported versions, fields, trailing JSON and nonregular markers
remain intact and produce an error instead of silently enabling writes.

Every existing user file that omits `agent.collaboration` remains `legacy`,
including an old configuration imported into a new profile. The actual immutable
configuration read owns this decision, even if the file appears after default
selection. Loading does not remove old switches or rewrite the original file.

Settings explicitly accept `auto`, `shared`, `isolated` and `legacy`. Saving
compatibility writes explicit `legacy`, so omission cannot reselect `auto` in a
fresh profile. Changes apply to new or rebuilt runtimes; saved and effective
values remain separate.

Projects cannot replace user choices. A first user save
records the selected policy, preserves opaque fields and uses private file mode.

Automatic selection requires a host backend with complete program mutation
bounds. An unavailable backend reports its reason and falls back to shared short
transactions before creating an automatic candidate.

The current Windows
creation-only token cannot bound deletion and rename, so automatic tasks use
this fallback. Configuring a container verifier does not confine candidate Bash.

Explicit worktree file tools and retained scenes remain usable under their
existing protocol; actual process launch still checks its final writable surface.
Resuming a bound environment never silently moves it back to the source.

Older programs may ignore the marker or reject explicit new policy values.
Legacy claims still coordinate participating processes, but unaware old writers
cannot safely downgrade through integration recovery journals.

Drain active
operations and resolve unfinished integration before switching policies. Never
remove a journal to bypass the recovery barrier.

See the [concurrency guide](CONCURRENT_MUTATIONS.md) and
[compatibility matrix](CONCURRENT_MUTATIONS_PROTOCOL.md). Implementing migration
does not itself establish full platform, fault or performance acceptance;
default activation depends on the final qualification record.

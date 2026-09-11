# 会话恢复与并行工作

Reasonix 将会话记录持久化和工作区文件修改分成两条安全边界。只读任务
和互不重叠的文件声明可以并行执行；不透明写入（例如无限制 shell 或未知
MCP 修改）继续使用工作区写租约。需要独立工作区的任务可以使用 Git
worktree 隔离。

## 会话版本

格式 2 的会话日志把同一会话的所有版本保存为其只追加 DAG 的 head
（见 [会话所有权](./SESSION_OWNERSHIP.zh-CN.md)）。head 分为几类：

- `main`：日志起始的那条线；
- `fork`、`rewind`：来自用户操作——从消息分叉、`/branch` 和对话回溯；
- `concurrent`：保存时发现另一写者位于同一条链上而分出的 head。

其中一个 head 是*选中*的，打开会话即打开它。head 在日志内部列出、切换、
改名和退役，不会产生第二个会话文件；退役 head 的字节保留到单写者轮转日志
时才回收。

格式 1 的会话（1.39.0 之前保存且尚未升级）仍在 branch metadata 中带有
版本身份：

- `normal`：普通会话记录；
- `recovery`：保存冲突、文件锁超时或外部删除后保留的恢复记录；
- `subagent`：仅用于持久化的子代理会话。

旧 sidecar 仍然可读。缺少显式版本字段但带有 `Recovered=true` 的记录会被
解释为 recovery 版本。恢复元数据记录父会话、父版本以及冲突时观察到的
base/disk revision；恢复副本留在同一条逻辑会话谱系中，不会被当作普通会话
或子代理。

## 恢复生命周期

格式 2 的保存不会冲突。本地没有新增时直接跟随另一写者的追加；双方都有新增
时，保存分叉出 `concurrent` head，双方各收到一次提示。版本对话框同时列出两个
head，由用户选择。不再有 `pending` 状态，也没有需要重试的租约移交：lease 只
决定谁写派生 transcript、索引和回合账本。

格式 1 会话中，如果磁盘内容只是可采用的已保存前缀，系统直接采用磁盘版本，
不创建新版本；如果两边有独立新增内容，系统通过现有 CAS 和 digest 校验保留
recovery 版本。租约移交失败时，恢复版本会标记为 `pending`，占用者释放后桌面端
可以重试激活。恢复谱系收敛操作是幂等的：已被完整覆盖的副本可以移动到可恢复的
会话回收站，存在独立内容的分歧版本继续保留，等待用户明确选择。

桌面桥接层提供：`GetRecoveryLineage` 与 `GetSessionVersionState`，列出格式 2
日志的 head（状态为 `heads`；所有成员共享日志路径，并带有 `headId`、
`headKind`、`headName`、`selected`）；`ChooseRecoveryBranch` 与
`SetActiveSessionVersion` 接受 `headId`，把某个 head 设为当前（已打开的标签页
就地切换，未打开的会话写入 `select` 标记）；`CleanRecoveryLineage` 退役已覆盖
的 head，并把最近一分钟内仍有活动的 head 报告为“正在使用”；
`RenameSessionHead` 给 head 命名；以及面向格式 1 的 `RetrySessionRecovery`
和 `ReconcileRecoveryVersions`。若某个格式 1 根会话在已有恢复副本之后才被
升级，日志的 head 与这些副本会一起列出。worktree 状态查询和合并准备复用现有
后端检查及身份校验。

## 遗留恢复副本

升级前产生的 `-recovery-` 文件不会导入日志。它们仍是同一谱系中的格式 1
会话：在“查看版本”中列出、可以选择，已覆盖的副本继续由现有清扫和
`reasonix sessions cleanup` 移入可恢复回收站。格式 2 日志不会形成恢复分组，
因此 cleanup 对它的候选数恒为零；`reasonix sessions diagnose` 会在恢复副本
数字旁边给出会话日志数、head 数、已覆盖 head 数和已退役 head 数。

## 兼容性

| 字段或格式 | 旧数据行为 | 新读者行为 | 旧读者行为 | 结论 |
| --- | --- | --- | --- | --- |
| `.events.jsonl` 格式 1 | 不变 | 重放；消息 id 确定性派生 | 不变 | 兼容 |
| `.events.jsonl` 格式 2 | 无 | 原生 | 拒绝打开，文件不改 | 明确边界（≥ 1.39.0） |
| `.jsonl` transcript | 格式不变 | 由选中 head 派生 | 可读但不权威 | 兼容 |
| `.jsonl.meta` head 字段 | 缺省 → 视为格式 1 | 使用 | 忽略 | 兼容 |
| `.event-index.json` schema 2 | 拒绝 schema 1 索引 → 重放 | 原生 | 拒绝 → 慢路径 | 兼容 |
| `-recovery-<hex>.jsonl` | 格式 1 谱系 | 不导入，照常列出 | 不变 | 兼容 |
| 会话目录 `v8.sqlite` | v7 文件隔离 | 原生 | 各用各的代际文件 | 兼容 |

格式 2 需要 Reasonix 1.39.0 及更新版本打开。混用新旧版本时应先升级旧的一方
再共享会话目录；旧版本会报告遇到了更新的格式，且不会修改文件。

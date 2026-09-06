# 会话恢复与并行工作

Reasonix 将会话记录持久化和工作区文件修改分成两条安全边界。只读任务
和互不重叠的文件声明可以并行执行；不透明写入（例如无限制 shell 或未知
MCP 修改）继续使用工作区写租约。需要独立工作区的任务可以使用 Git
worktree 隔离。

## 会话版本

每个物理 JSONL 会话文件都在 branch metadata 中带有版本身份：

- `normal`：普通会话记录；
- `recovery`：保存冲突、文件锁超时或外部删除后保留的恢复记录；
- `subagent`：仅用于持久化的子代理会话。

旧 sidecar 仍然可读。缺少显式版本字段但带有 `Recovered=true` 的记录会被
解释为 recovery 版本。

恢复元数据记录父会话、父版本以及冲突时观察到的 base/disk revision。恢复
副本留在同一条逻辑会话谱系中，不会被当作普通会话或子代理。

## 恢复生命周期

如果磁盘内容只是可采用的已保存前缀，系统直接采用磁盘版本，不创建新版本。
如果两边有独立新增内容，系统通过现有 CAS 和 digest 校验保留 recovery
版本。租约移交失败时，恢复版本会标记为 `pending`；占用者释放后，桌面端
可以重试激活。

恢复谱系收敛操作是幂等的。已被完整覆盖的副本可以移动到可恢复的会话回收站，
存在独立内容的分歧版本继续保留，等待用户明确选择。

桌面桥接层提供 `GetSessionVersionState`、`SetActiveSessionVersion`、
`RetrySessionRecovery` 和 `ReconcileRecoveryVersions`。worktree 状态查询和
合并准备复用现有后端检查及身份校验。

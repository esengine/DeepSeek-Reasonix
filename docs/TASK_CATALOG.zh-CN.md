# Task Catalog

Reasonix 将跨项目任务投影保存到 `<cache root>/task-catalog/v1.sqlite`。任务 snapshot、
event JSONL、idempotency 文件、文件锁、version CAS 和 lease 继续作为权威数据。SQLite
绝不参与 stop、cancel、requeue 或 open-session 的接受判断。

Observed `FileStore` 只在 per-task lock 释放后发送非阻塞 snapshot/event 提示。Snapshot
按有界批次优先索引；event log 仅在任务展开、事件查询或追加新事件时惰性索引。byte
offset 和 sequence checkpoint 可在强退后续传；坏 snapshot 或 event line 只降级单个任务。

Desktop Task Center 对当前会话、当前项目或全部已注册项目分页。Project key 是 canonical
已保存 root 的完整 SHA-256，每次 action 都先解析 key，再由 control service 重读权威
snapshot。当前进程的 jobs/controller 状态始终覆盖 catalog；过期 lease 只在读取时对账，
不回写 snapshot。

```sh
reasonix doctor catalogs [--json]
reasonix catalogs reindex tasks [--project PATH ...] [--json]
```

Catalog 损坏或未完成索引不会禁用任务控制；reindex 只替换可丢弃投影。

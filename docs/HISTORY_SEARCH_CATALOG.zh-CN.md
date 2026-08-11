# 历史搜索 Catalog

Reasonix 将历史搜索投影保存到 `<cache root>/history-search/v1.sqlite`。
Session JSONL、event log、metadata、子 Agent transcript 和 archive 仍是唯一权威
数据。删除或重建数据库不会删除会话，旧版 Reasonix 也继续读取原有权威文件。

Catalog 的 FTS5 只保存规范化检索 token，不保存完整消息正文。英文沿用现有小写与
代码符号语义，CJK 沿用重叠 bigram。SQLite 选出最终候选后，snippet 和 `around`
上下文才从权威文件读取。user、assistant、tool input、tool error、tool output 都会索引，
但普通 tool output 仍不属于默认搜索种类。

权威会话提交成功且文件锁释放后，保存路径只发送非阻塞、按 path 合并的索引提示。
连续 append 只读取 display index 的新增范围；rewrite、通知丢失、外部进程写入或指纹
不一致会在后台重建单个 source。完整 transcript decoder 始终单并发，checkpoint 按
source 持久化，单个坏会话不会阻断其他历史。

Agent 的 `history` 工具与 Desktop 历史管理器共享该投影。搜索不会同步扫描目录；首次
索引未完成时立即返回已有结果和明确进度。open、running、current 等运行态只从内存
覆盖，SQLite 不能恢复陈旧所有权。面向 provider 的工具名、描述、schema、默认值和
顺序保持逐字节不变。

数据库遵循通用可丢弃投影策略：本地盘使用 WAL、`synchronous=NORMAL`、外键、用户
私有权限和短 busy timeout；远程或不可用缓存自动退化为内存。完整性或迁移失败只隔离
并重建缓存；遇到未来 schema 时保留原库并进入 degraded。

诊断与安全重建命令：

```sh
reasonix doctor catalogs [--json]
reasonix catalogs reindex history [--dir PATH ...] [--json]
```

诊断不会输出 query、token、snippet、消息、tool arguments 或 provider 内容；reindex
只替换该可丢弃投影。

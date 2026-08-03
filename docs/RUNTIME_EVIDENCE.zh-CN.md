# 运行证据与恢复闭环

Reasonix 把运行证据保留在本地，不放入 provider prompt。除权威 transcript 外，每个 session 还有两个配套存储：

- `<session>.run-journal.ndjson` 是带 schema 版本、只追加的 Run Journal。它只用计数器和 SHA-256 摘要记录 run 边界及工具 receipt 分类，不持久化原始 prompt、参数、命令、输出、错误或路径。文件权限为 `0600`，每次追加执行 `fsync`，sequence 单调递增；恢复时修复未写完的尾行，遇到未来 schema 则保守失败。
- `<session>.goal-state.json` 中的 `deliveryCheckpoint.evidenceLedger` 是有界 Goal Evidence Ledger。它保留最近 64 条成功证据身份、mutation generation，以及通过实时完成门禁的 generation。它只是增量审计信息：旧 checkpoint 布尔字段仍然有效，恢复出的账本不能跳过新的验证、审查或签收。

两种存储都随 session 切换，并纳入当前版本的 session 清理。缺少新字段的旧 goal state 会保守加载；旧二进制会忽略新增 JSON 字段。journal 使用 `.ndjson`，可避免旧版 `.jsonl` 扫描器把 sidecar 误认成会话。

每次成功 mutation 都会推进 `mutationGeneration`；验证、审查、检查、验收标准和签收 receipt 会投影到对应 generation。只有现有 host readiness 门禁全部通过时，`closedGeneration` 才会前进，`pendingMutation` 才会清除。因此崩溃后仍能用不含内容的记录解释哪个 generation 尚未闭环，而下一次运行依然必须提供有效的实时证据。

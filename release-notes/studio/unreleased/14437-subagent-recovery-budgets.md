## 新增

- Studio 可预览任务图并请求恢复，复用完成结果，未完成任务仍受当前权限约束。Refs #14437
- 共享预算落盘，请求、重试和压缩前预留额度；重启恢复与兼容续接保留原预算。Refs #14437

## 变更

- 历史分页使用固定长度索引，正常读取量不再随全部历史增长。Refs #14437
- 旧任务图凭唯一保存的原参数和已验证结果显式重建；缺失信息和未知版本不推测执行。Refs #14437

<!-- lang:en -->

## New

- Preview and request task-graph recovery in Studio through the current conversation and permissions. Refs #14437
- Persist shared task budgets and reserve allowance before requests, retries and compaction. Restarted tasks reopen their bound budget. Refs #14437

## Changed

- Seek fixed-width history index entries instead of decoding the whole index for each page. Refs #14437
- Rebuild legacy fleet plans only from unique saved arguments and verified results. Missing data and unknown versions fail closed. Refs #14437

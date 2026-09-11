# 用量 Catalog

Reasonix 将可丢弃的用量聚合保存到 `<cache root>/usage-catalog/v1.sqlite`。每日统计
JSONL 继续作为权威数据，并与旧版保持字节兼容。Catalog 记录文件 offset 和行 hash，
用于跨进程幂等，再派生按日、source、model/provider 的 rollup。

只有 JSONL append 成功后才会非阻塞提交 receipt；重复 receipt 不会重复累计。同一
offset hash 改变、文件缩短/替换、队列溢出或外部追加会触发 offset 对账或按日安全重建。
torn tail、空行、坏 JSON、旧 requests 默认值、时区、日期边界及 ActiveDays 语义都与
现有 JSONL decoder 一致。

只有所选日期全部完成索引时才使用 SQL；否则用户主动打开统计页时执行现有 JSONL 精确
查询，同时后台继续补齐。Catalog 故障不会阻塞 provider event、TurnDone、controller
启动或退出。

```sh
reasonix doctor catalogs [--json]
reasonix catalogs reindex usage [--json]
```

诊断只暴露 schema、完整性、lag、数量和错误，不包含模型请求内容；reindex 不修改每日
JSONL。

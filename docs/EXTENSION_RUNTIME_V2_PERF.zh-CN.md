# 扩展运行时 v2 — 性能基线

软 CI 阈值见 `internal/extension/bench_threshold_test.go`。
Benchmark 见 `internal/extension/benchmark_test.go`。

## 目标（开发机 / CI 软失败）

| 操作 | N | 软上限 |
| --- | --- | --- |
| `BuildDependencyGraph` | 32 组件 | < 50ms |
| `DiffRuntimePlan` no-op | 同图 | < 20ms |
| `EffectScope.Dispose` | 64 effects | < 50ms |

## 增量 vs 全量 rebuild

- **no-op / interceptor / UI / provider / MCP-only**（`RebuildFrom` + 真子图 patch）：**不得**调用 `BuildRuntime`。指标：`NoOpRebuilds` / `SubgraphRebuilds`。
- **全量**（`SubgraphSidecar` / `SubgraphFull`）：`FullRebuilds` + 完整 `BuildRuntime`。

测量命令：

```bash
go test ./internal/extension/ -run 'TestGraphAndPlanLatencyBaseline|TestEffectScopeDisposeBaseline' -count=1
go test ./internal/extension/ -bench 'BenchmarkDependencyGraphAndPlan|BenchmarkExtensionKernelStartup' -benchmem -count=3
go test ./internal/boot/ -run 'TestIntegrationNoOpDoesNotBuildNewController|TestRebuildFromNoOp' -count=1
```

## 缓存命中

no-op、UI/interceptor-only 以及只滚动 backend 的 Provider/MCP 计划会保持
system prompt、tool schemas 与 `CacheHash` 字节稳定。Provider capability 变化通过
`providerChanged` 呈现，不会误报 `prefixChanged`；MCP schema 新增、删除或改名则
归类为全量 rebuild，并有意重新计算 prefix。保留 `ReuseAssembly` 时仍可跳过
skill/command/hook rediscovery。

## Sidecar 启动 / drain

`StartPackagesWithPlan` 收养 Unchanged 客户端；仅启动 Added/Reloaded。
Publish 后 `DrainPlan`；默认 drain TTL 30s，超时先 fire cancel 再写
`drain-timeout-<gen>` receipt。
冷启动 publish 不创建 watcher；存在 drain 时，快速连续 publish 在每个 runtime
owner 上共用一个定时 watcher，并只等待最早 drain 的剩余 TTL。每个 owner 的
过期 generation 标记最多保留 256 个。

Receipt 证据仅存在于当前进程，最多保留 32 个 generation、每代 256 条。发生
淘汰时按保守策略处理：禁止声称 clean rollback，而不是隐藏证据缺失。
消息去重键会随对应 receipt 淘汰而释放。文件 prior 每次写入最多保留 8 MiB，
每个 runtime owner 合计最多 32 MiB；超限 prior 不保留，并阻止 clean rollback 判断。
已完成的 Provider stream 会立即移除 drain 回调，长寿命 generation 只保留活跃 stream
的取消状态。

# `use_capability` 回放评测

这个可选配对评测用于比较缓存稳定的 `use_capability` 代理，以及把 MCP 工具展开进
provider 可见 schema 的基线。它是性能诊断工具，不是 Stable 发布门禁。真实模型
运行可选，且必须使用一次性 Reasonix home。

## 测什么

同一任务集中的每道任务各跑两次：

1. **代理（默认）：** 只用 `use_capability`，共享 Host 与磁盘 schema 缓存。
2. **基线：** 临时配置仍把 MCP 工具展开进 provider 请求，原生 Tool Search 保持关闭。

记录 `tools/list` 次数、首 Token 延迟和缓存命中 Token。不得上传提示词、密钥、
工具参数或工作区路径。

## 步骤

1. 使用一次性 `REASONIX_HOME` 和 `REASONIX_CACHE_HOME`。
2. 选择一组需要先发现再调用 MCP 的代表性任务。
3. 每道任务分别运行代理和基线，并保持模型、effort、工作区、技能、Agent 与 MCP
   配置一致。
4. 记录与 `internal/eval/replay/testdata/paired_runs.json` 相同的无内容配对数据。
5. 运行中位数工具测试：

```bash
go test ./internal/eval/replay/ -run TestMedianReportFivePairedRuns
```

仓库夹具是合成数据，只用于证明中位数工具。团队可按需用真实观测做性能分析，但
Stable 发布不要求提供配对数据集，也不要求达到任何配对阈值。

第一方原生 Tool Search 仍默认关闭，不纳入本评测。

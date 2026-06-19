## 问题

当 agent.max_steps > 0 时，达到限制后模型被直接截断，返回 "paused after N rounds" 错误。之前做的工具调用全部浪费，用户得手动续一条消息。

## 改动

预算耗尽后给模型**一轮额外响应**（grace round）：

```
step 0: stream → tool calls → execute → compact
step 1: stream → tool calls → execute → compact → 预算用完 → 注入 nudge, graceRound=true
step 2 (grace): stream → 如果 final answer → 成功结束
                              → 如果还调工具 → hard stop
```

- Grace round 不算预算（白送一轮）
- 注入的 nudge 明确告诉模型："预算用完，基于已有结果输出 final answer"
- Grace round 再调工具 → 硬错误，原有消息可续跑

## 测试
`go test ./internal/agent/ -run TestCoordinatorPlannerMaxSteps` — 通过。
`go test ./internal/agent/ -run TestEarlyToolDispatch` — 通过。

Cache-impact: low — 注入一条 user message 导致 grace 轮前缀缓存 miss，不影响后续轮次

Cache-guard: go test -race ./internal/agent/ -count=1 -run 'Test(RunPopulatesCacheDiagnosticsOnUsageEvents|CompactRewriteVersionFeedsCacheDiagnostics|MaybeCompactThreshold|UsageLineReportsPrefixChurn|CoordinatorPlannerMaxSteps|EarlyToolDispatch)' — cache 回归 + grace round 功能测试全部通过；nudge 注入导致 grace 轮一次预期内 cache miss，不影响后续轮次前缀稳定性

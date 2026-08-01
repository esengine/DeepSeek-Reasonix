# Model Performance Baselines

Reference performance baselines collected with `tools/baseline` for
model-watchdog anomaly detection. Regenerate with:

```bash
# DeepSeek V4 Flash (OpenAI kind, api.deepseek.com)
# -verbose keeps the per-sample array so the artifact matches the checked-in one.
go run ./tools/baseline -model deepseek-flash -n 20 -verbose -out benchmarks/baselines/deepseek-v4-flash.json

# Qwen 3.8 Max Preview (dashscope-responses kind, Token Plan endpoint)
go run ./tools/baseline -model qwen-Token-plan-cn/qwen3.8-max-preview -n 20 -verbose -out benchmarks/baselines/qwen3.8-max-preview.json
```

Both runs need the proxy env (`https_proxy=http://127.0.0.1:10808`) and
`REASONIX_HOME` pointing at the user config that holds the API keys.

## 2026-08-01 baseline (20 samples each, 0 errors)

| Metric | DeepSeek-V4-Flash | Qwen3.8-Max-Preview |
|---|---|---|
| Latency P50 | 4802 ms | 7010 ms |
| Latency P95 | 17787 ms | 22462 ms |
| TTFT P50 | 2501 ms | 2821 ms |
| TTFT P95 | 17582 ms | 8324 ms |
| Out Tokens P50 | 496 | 272 |
| Out Tokens P95 | 1492 | 1158 |
| Errors | 0/20 | 0/20 |

Notes:
- Same host, same proxy, same day. Provider endpoints differ
  (api.deepseek.com vs token-plan.cn-beijing.maas.aliyuncs.com).
- DeepSeek is faster at the median; Qwen has lower P95 TTFT (8324 vs
  17582 ms) — DeepSeek has a heavier long tail.
- DashScope truncation returns all-zero usage; `tools/baseline` ignores
  those chunks so token stats stay meaningful.

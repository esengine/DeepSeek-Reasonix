# Reasonix + DeepSeek-V4-Flash E2E Benchmark（2026-07-31）

同一任务集（`benchmarks/e2e/`，5 任务）跑两种协议的对照。

## 总览

| 指标 | Responses API | Chat Completions | 差异 |
|------|--------------|-----------------|------|
| Accuracy | 5/5 (100%) | 5/5 (100%) | 持平 |
| Cache hit | 84% | 82% | +2pp |
| 总 tokens | 1,260,958 | 1,109,741 | +13.6% |
| Completion tokens | 4,088 | 5,970 | **-31.5%** |
| 总成本 | ¥0.2183 | ¥0.2189 | 持平 |

## 分任务

| 任务 | Responses | | | Chat | | |
|------|-----------|--|--|------|--|--|
| | Steps | Cache | Cost | Steps | Cache | Cost |
| compaction | 10 | 88% | ¥0.075 | 9 | 86% | ¥0.072 |
| fix-add-bug | 7 | 86% | ¥0.036 | 6 | 83% | ¥0.035 |
| fizzbuzz | 5 | 80% | ¥0.036 | 4 | 75% | ¥0.035 |
| palindrome | 8 | 87% | ¥0.038 | 6 | 83% | ¥0.037 |
| subagent-delegation | 5 | 63% | ¥0.034 | 6 | 71% | ¥0.040 |

## 分析

1. **准确性持平**：两种协议 5/5，无能力差异
2. **Completion 显著减少**：Responses API 省 31.5% 输出 token——reasoning 以 `reasoning_text` item 返回（不计入 completion 显示文本？需核实 pricing 口径），或模型行为差异
3. **缓存略优**：84% vs 82%
4. **Prompt tokens 更多**：Responses stateless 每轮发全量 input（无 previous_response_id），但 DeepSeek 自动缓存抵消了大部分
5. **成本持平**：¥0.2183 vs ¥0.2189

## 结论

Responses API 在本任务集上：同等准确率、略优缓存、显著少输出 token、成本持平。作为生产协议完全可用。

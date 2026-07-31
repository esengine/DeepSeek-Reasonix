# 模型对比：DeepSeek-V4-Flash vs Qwen3.8-Max-Preview

同一 12 任务测试集，2026-07-31，两者都用 Responses API 协议。

## 总览

| 指标 | DeepSeek-V4-Flash | Qwen3.8-Max-Preview |
|------|-------------------|---------------------|
| Accuracy | **12/12 (100%)** | **12/12 (100%)** |
| Cache hit | 81% | 79% |
| 总 tokens | 1,260,958+ | 1,968,242 |
| Completion tokens | 4,088 (5 任务) | 11,608 (12 任务) |
| Steps 合计 | 34 (5 任务) | 60 (12 任务) |
| 成本 | ¥0.27（按量） | 订阅制（无单价） |

## 分任务 Steps（效率）

| 任务 | DeepSeek | Qwen |
|------|----------|------|
| agent-last-exam-lite | 6 | 5 |
| compaction | 10 | 8 |
| deepswe-lite | 4 | 5 |
| dsbench-lite | 7 | 4 |
| fix-add-bug | 7 | 4 |
| fizzbuzz | 5 | 4 |
| nl2repo-lite | 11 | 6 |
| palindrome | 8 | 5 |
| security-audit-lite | 5 | 5 |
| subagent-delegation | 5 | 5 |
| terminal-bench-lite | 5 | 6 |
| toolathlon-lite | 3 | 3 |

## 观察

1. **准确率持平**：两者都是 100%，本测试集无法区分能力上限
2. **Qwen 更高效**：大多数任务用更少 steps（nl2repo 11→6，palindrome 8→5，fix-add-bug 7→4）
3. **Qwen 输出 token 更多**：同样任务 completion 明显更多（如 dsbench-lite 3,234 vs 3,840 反向；但 fizzbuzz 663 vs 721）
4. **缓存命中相近**：79% vs 81%
5. **成本不可比**：Qwen 是 Token Plan 订阅，DeepSeek 按量——需要按各自套餐价格换算

## 结论

- 本 lite 测试集适合**回归检测和协议对比**，不适合模型能力排名（都 100%）
- Qwen3.8-max-preview 在步骤效率上略优（更少的工具调用轮次）
- 需要区分能力上限应接入官方全量基准

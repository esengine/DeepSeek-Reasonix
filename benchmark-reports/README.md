# Reasonix 量化测试集基线

## 测试集构成（12 任务）

### 原始任务（5）
compaction、fix-add-bug、fizzbuzz、palindrome、subagent-delegation

### 新增 lite 任务（7，对标公开基准的简化版）
| 任务 | 对标 |
|------|------|
| terminal-bench-lite | Terminal Bench 2.1 |
| nl2repo-lite | NL2Repo |
| toolathlon-lite | Toolathlon |
| deepswe-lite | DeepSWE |
| agent-last-exam-lite | Agent Last Exam |
| dsbench-lite | DSBench |
| security-audit-lite | Cybergym（防御向） |

## 基线分数（2026-07-31，DeepSeek-V4-Flash）

### 原始 5 任务（全量）

| 指标 | Responses API | Chat Completions |
|------|--------------|-----------------|
| Accuracy | **5/5 (100%)** | **5/5 (100%)** |
| Cache hit | 84% | 82% |
| 总成本 | ¥0.2183 | ¥0.2189 |

### 新增 7 任务（Responses API）

| 任务 | 结果 | 缓存 | 成本 |
|------|------|------|------|
| terminal-bench-lite | ✅ | 80% | ¥0.036 |
| nl2repo-lite | ✅ | 91% | ¥0.044 |
| toolathlon-lite | ✅ | 66% | ¥0.033 |
| deepswe-lite | ✅ | 75% | ¥0.036 |
| agent-last-exam-lite | ✅ | 83% | ¥0.036 |
| dsbench-lite | ✅ | 86% | ¥0.045 |
| security-audit-lite | ✅ | 79% | ¥0.040 |

## 综合基线

**12/12 (100%) · 缓存 ~81% · 成本 ~¥0.27（全量）**

## ⚠️ 分数解读（重要）

1. **这不是官方基准分数**。DeepSeek 公告的 Terminal Bench 2.1 = 82.7、NL2Repo = 54.2 等是**严格全量基准**，我们的 lite 任务是简化版（单文件、单环节），难度远低于官方
2. **100% 反映的是"基本能力可用"**，不反映"顶尖能力排名"
3. 要获得可对标的官方分数，需要接入真实基准（Terminal Bench 全量、SWE-bench 等），工作量大
4. **本基线的价值**：
   - 协议对比（Responses vs Chat：准确率持平、缓存略优、输出 token 少 31.5%）
   - 模型对比（换模型跑同一套，看 accuracy/cost 变化）
   - 回归检测（改动后跑一遍，防能力退化）

## 跑法

```bash
# 全量 12 任务
go run ./cmd/e2ebench -bin ./bin/reasonix -model <provider>/<model> -budget 1500000

# 单任务（临时 suite）
mkdir -p /tmp/suite/tasks && cp -r benchmarks/e2e/tasks/<id> /tmp/suite/tasks/
go run ./cmd/e2ebench -bin ./bin/reasonix -model <provider>/<model> -budget 300000 -suite /tmp/suite
```

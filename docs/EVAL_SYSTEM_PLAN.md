# Agent Evaluation System Plan

## 目标

为 Reasonix 建立一个可复现的、自动化的评测体系，衡量 agent 在真实编码任务上的综合表现。不包含代码质量和安全性评测。

## 评测维度

| 维度 | 说明 | 测量方式 |
|---|---|---|
| **功能正确性** | agent 是否正确解决了给定的 issue | SWE-bench 测试套件 |
| **成本效率** | 解决每个 issue 消耗多少 token / 多少钱 | metricsSink 累积 |
| **时间效率** | 解决每个 issue 耗时多久 | wall time 计时 |

## 数据集：SWE-bench Verified

**选择理由：**

- 业界事实标准，Claude Code / Devin / Cursor 都报告这个
- 500 个人类工程师验证过的实例，排除了模糊不清的问题
- 基于 Docker 的可复现评测环境
- 每次 resolve rate 可以直接和其他产品横向对比

**不选的理由（备选方案）：**

| 候选 | 放弃理由 |
|---|---|
| SWE-bench 全量（2294 个） | 部分实例模糊、质量参差，不划算 |
| HumanEval | 太简单，单函数级别，不反映真实能力 |
| LiveCodeBench | 算法竞赛题，不是项目级任务 |
| WebArena | 浏览器操作为主，非编码 |

## 评测架构

```
                    ┌─────────────────────────────────┐
                    │      SWE-bench Docker 镜像       │
                    │  (Python 项目 + issue + 测试套件)│
                    └────────────┬────────────────────┘
                                 │
                    reasonix run --eval <issue-description>
                                 │
                    ┌────────────▼────────────────────┐
                    │   Agent 运行 (现有 run loop)     │
                    │                                  │
                    │   metricsSink 自动记录:           │
                    │   ├── PromptTokens               │
                    │   ├── CompletionTokens            │
                    │   ├── CacheHit/Miss               │
                    │   ├── Cost                        │
                    │   ├── Steps (LLM 调用次数)        │
                    │   ├── Tool Calls                  │
                    │   └── Wall Time                   │
                    └────────────┬────────────────────┘
                                 │
                    ┌────────────▼────────────────────┐
                    │   补丁生成 & 验证                 │
                    │                                  │
                    │   1. agent 生成 patch             │
                    │   2. 应用到 Docker 镜像          │
                    │   3. 运行 SWE-bench 测试套件     │
                    │   4. 记录 resolve / fail          │
                    └────────────┬────────────────────┘
                                 │
                    ┌────────────▼────────────────────┐
                    │   结果汇总                        │
                    │                                  │
                    │   报告:                           │
                    │   ├── Resolve Rate: N/500         │
                    │   ├── Avg Cost: $X.XX per issue   │
                    │   ├── Avg Tokens: XXXK per issue  │
                    │   ├── Avg Time: XXm per issue     │
                    │   └── Cost to resolve all: $XX    │
                    └──────────────────────────────────┘
```

## 复用现有基础设施

Reasonix 已经有的：

| 现有组件 | 位置 | 用于评测 |
|---|---|---|
| `metricsSink` | `internal/cli/run_metrics.go` | 累积 PromptTokens / CompletionTokens / Cost / Steps |
| `ReadinessAudit` | `internal/evidence/readiness_audit.go` | 记录最终答案是否被拦截 |
| `event.TurnStarted / TurnDone` | `internal/event/event.go` | 计算每个 issue 的 wall time |
| `reasonix run` | `internal/cli/run.go` | 非交互式执行 |
| session 持久化 | `internal/agent/save.go` | 保存每个 issue 的运行日志 |

需要新增的：

| 新增组件 | 位置 |
|---|---|
| SWE-bench 下载器 & Docker 编排 | `internal/eval/swebench.go` |
| 评测报告生成器 | `internal/eval/report.go` |
| `reasonix eval` 命令行 | `internal/cli/eval.go` |

## eval 子命令

```bash
# 完整评测（500 个实例）
reasonix eval --dataset swebench-verified

# 子集评测（快速验证）
reasonix eval --dataset swebench-verified --limit 10

# 从已有结果重新报告（不重新跑）
reasonix eval --report ./results.json

# 指定模型
reasonix eval --dataset swebench-verified --model deepseek/deepseek-v4-flash

# 输出格式
reasonix eval --dataset swebench-verified --output json
```

## 输出报告格式

```json
{
  "dataset": "swebench_verified",
  "model": "deepseek/deepseek-v4-flash",
  "timestamp": "2026-06-08T00:00:00Z",
  "summary": {
    "total_instances": 500,
    "resolved": 185,
    "failed": 315,
    "resolve_rate": 0.37,
    "total_cost_usd": 142.50,
    "avg_cost_per_resolved": 0.77,
    "total_prompt_tokens": 85000000,
    "total_completion_tokens": 32000000,
    "avg_prompt_tokens_per_resolved": 459459,
    "avg_completion_tokens_per_resolved": 172973,
    "total_time_seconds": 86400,
    "avg_time_seconds_per_resolved": 467
  },
  "instances": [
    {
      "id": "django__django-12345",
      "resolved": true,
      "prompt_tokens": 320000,
      "completion_tokens": 85000,
      "cost_usd": 0.72,
      "time_seconds": 245,
      "steps": 12,
      "patch_size_lines": 45
    }
  ]
}
```

## 指标定义

| 指标 | 计算公式 |
|---|---|
| **Resolve Rate** | `resolved / total * 100%` |
| **Avg Cost Per Resolved** | `total_cost / resolved` |
| **Avg Tokens Per Resolved** | `total_tokens / resolved` |
| **Avg Time Per Resolved** | `total_time / resolved` |
| **Cost Per Point (CPP)** | `total_cost / (resolved * 100 / total)` — 每提升 1% 花费多少 |

## 实现阶段

| 阶段 | 内容 | 估时 |
|---|---|---|
| **P1** | `internal/eval/` 包骨架：SWE-bench Docker 下载 + 实例解析 | 2d |
| **P2** | `reasonix eval` 命令：下载数据集、按顺序遍历实例、调用 agent | 2d |
| **P3** | metricsSink 集成：每个实例结束时导出 usage/cost/time | 1d |
| **P4** | 补丁验证：应用 patch → 在 Docker 中运行测试 → 记录 resolve/fail | 2d |
| **P5** | 报告生成：JSON 输出 + 终端表格 + 摘要 | 1d |
| **P6** | 断点续评：评测中断后从上次失败处继续 | 1d |
| **P7** | 多模型对比：一次运行评测多个配置 | 1d |
| **P8** | 缓存优化：相同 prompt 前缀跳过 LLM 调用 | 1d |
| **合计** | | **~11d** |

## 边界情况

| 场景 | 方案 |
|---|---|
| **Docker 镜像拉取失败** | 重试 3 次，跳过并记录原因 |
| **Agent 超时（> 30 分钟）** | 终止该实例，标记为 timeout |
| **Agent 生成空 patch** | 记录为 fail，不浪费环境运行测试 |
| **测试基础设施失败** | 标记为 infrastructure_error，不计入 resolve rate |
| **网络中断** | 保存进度，断点续评 |
| **ARM Mac** | SWE-bench Docker 镜像主要支持 x86_64，跑 eval 时需 Rosetta 或远程 x86 runner |
| **成本超预算** | `--max-cost` 参数，到达后停止 |
| **多个实例并行** | `--workers N` 参数（默认 1，避免 token 限速） |

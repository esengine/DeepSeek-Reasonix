# Harness A/B 实验

`e2ebench -mode harness-ab` 会在仓库的端到端任务集上执行配对、可断点续跑的实验。它适合比较两个 Reasonix 构建或 prompt profile，同时固定模型、任务、截止时间、grader、重试策略和每个实验臂的 token 预算。

## 运行实验

先准备两个二进制文件，再为实验指定一个新的持久化目录：

```bash
go run ./cmd/e2ebench \
  -mode harness-ab \
  -suite benchmarks/e2e \
  -model e2e \
  -environment-id deepseek-v4-flash-2026-08-03 \
  -baseline-bin /path/to/reasonix-baseline \
  -candidate-bin /path/to/reasonix-candidate \
  -baseline-profile baseline \
  -candidate-profile projection \
  -repetitions 3 \
  -infra-retries 1 \
  -budget 400000 \
  -run-dir .reasonix-bench/cache-projection-001
```

token 预算分别作用于两个实验臂。任务顺序保持稳定，但每个任务/重复 cell 中先运行哪个实验臂会交替，以降低顺序偏差。`-environment-id` 是不含秘密、由用户定义的 provider 路由、实际模型、计价版本及其他外部条件标签；harness 无法从凭证中安全推导这些信息，如果这些条件变化，应创建新的运行目录。

使用完全相同的参数和 `-run-dir` 再次执行即可恢复；已经完成的 cell 不会再次请求模型。每次 provider admission 都会在启动进程前写入日志；崩溃后，只有 admission 而没有持久化结果的尝试会被结算为 `infra_failed`，再按冻结的基础设施重试策略处理。同一个运行目录同一时间只能由一个 harness 进程使用。

如果任务集、任一二进制文件、模型、profile、重复次数、重试策略或预算发生变化，harness 会拒绝复用被冻结的运行目录，此时应创建新实验。

## 产物

运行目录包含：

- `manifest.json`：带 schema 版本的 harness 协议和实验身份，包括隐私安全的二进制/suite 标签及 SHA-256、任务摘要、模型/环境标签、profile、截止时间、重复次数、重试策略和预算；解析后的绝对路径只保留在当前进程内。
- `attempts.jsonl`：只追加并执行 `fsync` 的 WAL；一次实际尝试先写 `admission_started`，再写 `attempt_finished`。恢复时会移除末尾未写完的一行，较早位置的损坏则直接报错。
- `results.json`：最新 cell、实验臂汇总和配对统计的机器可读投影。
- `results.csv`：每个任务/重复配对一行，便于外部分析。
- `report.md`：每完成一次尝试都会刷新的可读报告。

产物不会写入 provider 凭证或解析后的本地路径，常见基础设施错误在持久化前也会清理路径。任务 ID、用户提供的标签与基准输出仍需遵循仓库既有隐私规范。

## 计分与重试

结果使用固定分类：`passed`、`verification_failed`、`agent_error`、`timeout`、`suite_budget_exhausted` 和 `infra_failed`。

只有 `infra_failed` 会自动重试。Agent 错误、验证失败、超时和预算耗尽都是终态计分结果。最终仍失败的基础设施 cell 会保留在报告中，但不进入准确率和配对统计。只要 metrics 可用，所有实际请求产生的 token 与成本（包括基础设施重试）都会计入实验臂总量。报告会显示 metrics 缺口；如果被终止的进程来不及落盘 metrics，总量应视为下界。

配对报告给出四格通过/失败计数、candidate 相对 baseline 的百分点变化，以及仅基于有效不一致配对计算的双侧精确 McNemar p 值。小任务集的统计功效有限，不能只凭正向点估计断言整体能力提升。

## 缓存解读

缓存命中率等于 `reasonix run --metrics` 报告的缓存 prompt token，除以 cache-hit 与 cache-miss token 之和。Harness A/B 本身不会改变 provider 可见 prompt。做缓存实验时，应保持 provider/model 路由和稳定前缀一致，只改变被测组件。

profile 可选 `baseline`、`delivery`、`projection`、`delivery-projection`。projection 变体使用只作用于当前进程的实验 override，不修改用户持久化配置。

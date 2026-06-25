# Reasonix v5 稳定版生产检查清单

Reasonix v5.9.9 已冻结为稳定发布候选版本。本清单是 PR #5217 以及后续 v5-stable 维护工作的发布门禁。

## 发布门禁

合并 v5 稳定发布候选版本前，以下检查必须全部通过。

## 1. 运行时安全

- sandbox 隔离已验证。
- 资源预算使用基于 ledger 的两阶段 reserve/commit。
- sandboxed execution 之间不会泄漏共享执行上下文。

## 2. 控制系统稳定性

- 分布式 control plane 已启用且具备确定性。
- global equilibrium layer 具备确定性。
- 不重新引入单点 meta-controller。

## 3. 记忆系统安全

- causal compression 保持稳定。
- long-tail predictive 和 causal signal retention 已验证。
- truth-lock decay 只改变影响权重，不改变事实正确性。

## 4. 预测系统隔离

- shadow observer 保持只读。
- prediction-action bridge 只输出 advisory。
- predictive warnings 不会自动反馈到 execution。

## 5. 时间系统

- logical time 和 physical time 的双时钟报告保持可见。
- lag window 和 damping window 保持分离。
- physical latency variance 不会被 logical time normalization 隐藏。

## 6. 架构冻结

- `system.StableSystemContract()` 能验证 v5.9.9 发布边界。
- `system.ArchitectureLocked` 已启用。
- v6-pre diagnostics 保持 observation-only。

## 7. 可观测性

- trace 和 diagnostic systems 保持 non-invasive。
- layer-collapse diagnostics 不影响 runtime、prompt、provider request 或 tool schema。

## 最终发布流程

1. 验证 stable system contract。
2. 确认 architecture lock 已启用。
3. 确认 v6-pre diagnostics 已隔离。
4. 通过 production checklist。
5. 通过 `system.ReleaseGuard()`。
6. 以 v5.9.9 stable release candidate 合并。

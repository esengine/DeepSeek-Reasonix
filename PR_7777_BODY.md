Cache-impact: medium - new code_verify tool added to the boot tool surface (tools hash / prefix hash change, golden baselines regenerated in-tree) and MCP auto-exposure adds use_capability routing condition; existing sessions' prefix cache invalidates once on upgrade.
Cache-guard: go test ./internal/boot/ -run 'TestChooseMCPExposure|TestBuildAutomaticallyUsesCapabilityForLargeMCPSurface|TestBuildKeepsSmallMCPSurfaceDirect|TestGoldenBaseline' && go test ./internal/agent/ ./internal/navigator/ ./internal/cosplay/ ./internal/config/
System-prompt-review: no system-prompt text changed; long-horizon compaction prompt gated on long_horizon (false = legacy 7-section, byte-identical), navigator/cosplay are tool-path only
Documentation-impact: updated - TOOL_CONTRACT.md (code_verify entry + MCP exposure), GUIDE docs from #7775, new DELIVERY_AUDIT.md and RX_BIG_REFACTOR_PLAN.md

## Summary / 摘要

基于已推送的 `feat/long-horizon-navigator` 分支，汇集三条工作线：

1. **OSWorld 2.0 状态型闭环**：navigator 观测模式（BeginAction/EndAction）接入主循环——针对 OSWorld 2.0 长任务三大缺陷（隐式状态失忆 / 动态界面死板 / 环境监控灯下黑）的对症修复；后台环境监控（StartBackgroundWatch，随 Run 生命周期）；OPT-261~265 token 治理模块聚合接线（全 advisory）。
2. **CoSPlay 协同进化**：新包 `internal/cosplay`——无真值数据、无微调的推理时协同进化（高区分度测试生成 → 代码×测试执行矩阵 → 多轮修复+淘汰无效测试 → 共识聚类选优）；`code_verify` 工具注册进 boot 工具面。
3. **PR #7775 适配**：MCP 自动暴露选择（use_capability 代理路由）与本分支 cherry-pick 合并共存，cache-guard 测试全过。

## Changes

- `internal/navigator`：BeginAction/EndAction 观测模式拆分、ContinuousStateManager 全方法加锁、后台环境监控、ProcessSensor 空模式短路
- `internal/agent`：NavigatorKernel 接口扩展 + navigatorBridge、applyNavigatorCorrection（reinject_facts/retry/rollback/ask_host）、TokenGovernance 聚合 OPT-261~265
- `internal/cosplay`（新包）：cosplay.go / gen.go / matrix.go / runner.go / tool.go + 4 测试
- `internal/boot`：code_verify 工具注册、navigatorWatchRunner、MCP 暴露接入（PR #7775）、TOOL_CONTRACT 同步
- `internal/config`：token_governance + cosplay 配置段
- `docs`：TOOL_CONTRACT.md、DELIVERY_AUDIT.md、RX_BIG_REFACTOR_PLAN.md

## Verification

- `go test ./... -short` → 104 包 ok（4 个 Windows 环境限制失败与本次改动无关）
- `go test ./...`（完整模式）→ 104 包 ok
- `go test -race` → 7/7 改动包（agent/navigator/boot/cosplay/config/capability/plugin）
- `go vet` 6 包零告警；boot golden 基线无漂移
- PR #7775 cache-guard：TestChooseMCPExposure ×6 + 2 个 build e2e 全过
- CLI 构建 + 冒烟运行（--version/--help）通过

完整验证矩阵见 `docs/DELIVERY_AUDIT.md`（提交 d8bd05cd）。

## Notes

- 4 个测试包（installsource/remote/repair/sessiontemp）在 Windows 因符号链接权限 / 本机 SSH 配置失败，Linux 或特权环境可过
- 本分支包含对 PR #7775（SivanCola/DeepSeek-Reasonix:feature/auto-mcp-exposure）的适配合并：cherry-pick 9880a2e7 + bb2b26b5，boot.go 冲突已解决并保留两边语义

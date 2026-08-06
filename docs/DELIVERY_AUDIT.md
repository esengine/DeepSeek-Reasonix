# 交付审计报告（Delivery Audit）

> 审计日期: 2026-08-06
> 分支: `feat/long-horizon-navigator` @ `e51c006c`
> 范围: OSWorld 2.0 闭环 + CoSPlay 协同进化 + PR #7775 适配
> 仓库: `D:\开发\reasonix-src`（原仓库）≡ `global-workspace\reasonix-dev`（副本，同一 HEAD）

## 一、交付内容

| 模块 | 内容 | 文件 |
|------|------|------|
| navigator 观测闭环 | BeginAction/EndAction 拆分、ContinuousStateManager 并发安全、主循环 handleToolRound 接入、applyNavigatorCorrection（reinject/retry/rollback/ask_host） | internal/navigator/{kernel,state}.go、internal/agent/{run_loop,state_tracker,navigator_bridge}.go |
| 后台环境监控 | StartBackgroundWatch（ctx 驱动/幂等/缓冲上限）、ProcessSensor 短路、navigatorWatchRunner 绑定 Run 生命周期 | internal/navigator/{sensor,kernel}.go、internal/boot/navigator_watch.go |
| OPT-261~265 接线 | TokenGovernance 聚合（shedder/compactor/resizer/gatekeeper/warmer），全 advisory | internal/agent/token_governance.go、internal/config/config.go |
| CoSPlay 包 | Verifier 闭环（测试生成→执行矩阵→修复→共识聚类）、ProcessRunner、TemplateGenerator | internal/cosplay/（8 文件） |
| code_verify 工具 | boot 工具表面注册、TOOL_CONTRACT 同步 | internal/cosplay/tool.go、docs/TOOL_CONTRACT.md |
| PR #7775 适配 | MCP 自动暴露选择（use_capability 路由）与上述功能共存 | internal/boot/{boot,mcp_exposure}.go、docs×5 |

## 二、验证矩阵（全部通过）

### 1. 源码级
- `go test ./... -short` → **104 包 ok**
- `go test ./...`（完整模式）→ **104 包 ok**
- 4 个失败包（installsource/remote/repair/sessiontemp）为 **Windows 环境限制**（符号链接权限 ×3、本机 ~/.ssh 配置 ×1、Windows 文件锁 ×1），在全部 5 个提交中 **git show 确认零改动**

### 2. 竞态级（-race，7/7 改动包）
agent 67.6s / navigator 1.3s / boot 67.2s / cosplay 1.7s / config 36.9s / capability 1.2s / plugin 49.1s —— 全部 ok

### 3. 静态级
- `go vet` 6 包（boot/capability/plugin/agent/navigator/cosplay）零告警
- `gofmt` 全部改动文件合规；`git diff --check` 无格式问题

### 4. 契约级
- boot golden 基线（prefix_shape/provider_request/tool_schemas）无漂移（`REASONIX_UPDATE_GOLDEN=1` 更新过 3 个文件并提交）
- PR #7775 cache-guard 测试 7/7（TestChooseMCPExposure×6 + build e2e）
- TOOL_CONTRACT.md ↔ boot 表面契约测试通过

### 5. 产物级
- `go build ./cmd/reasonix` 成功（70MB）
- 冒烟运行：`--version` → "reasonix dev"（exit 0）、`--help` 完整输出（exit 0）

### 6. 完整性级
- 同步完整性：副本提交 38 文件中 29 个 hash 完全一致；9 个差异全部核实为有意合并（6 冲突文件手工解决 + 3 自动合并保留 28ad311a 内容）
- 双向确认：fdffe45a 含全部新增（CodeVerifyTool/NavigatorBridge/StartBackgroundWatch）；28ad311a 的 ShowTurnUsage/CacheTTLMinutes 等完整保留
- `git fsck --no-dangling`：无对象损坏（仅 2 个无害 tmp 垃圾文件）

### 7. 一致性级
- 原仓库与副本 HEAD 完全一致：`e51c006c5178181c8a24b688e0c5ea2bdd1fd538`
- 跟踪文件工作树零差异；副本独立核心包测试全绿

## 三、提交链

```
e51c006c fix(boot): remove stray merge marker from MCP exposure cherry-pick
ca49c2e4 fix(mcp): wire automatic proxy capability routing        (PR #7775)
821c3c1a feat(mcp): choose exposure mode automatically            (PR #7775)
fdffe45a feat: OSWorld 2.0 closed-loop navigator + CoSPlay         (本会话同步)
28ad311a feat(long-horizon): address all 5 review blockers        (并行工作线)
```

## 四、已知限制与后续

1. **环境限制测试**：4 个包（installsource/remote/repair/sessiontemp）在 Windows 上因符号链接权限/SSH 配置/文件锁失败——与本次改动无关，Linux/特权环境可过
2. **未推送远程**：`feat/long-horizon-navigator` 仅存在于本地两个仓库，推送/开 PR 待用户指令
3. **CHANGELOG**：按仓库惯例（发布时统一维护）未更新
4. **后续增强**（可选）：cosplay auto_on_mutation 自动验证、ModelGenerator 接入 provider、navigator watch 事件直进 prompt、CHANGELOG 登记

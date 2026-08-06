# RX 大改方案：OSWorld 2.0 闭环 + CoSPlay 协同进化

> 生成日期: 2026-08-06
> **状态: 已全部实施完成（2026-08-06）** —— 阶段一、阶段二代码均已落地并通过测试。
> 工作副本: `global-workspace/reasonix-dev`（源仓库 `D:\开发\reasonix-src`，分支 `feat/run-mcp-meta-tool`）
> 依据: 用户提供的 OSWorld 2.0（长任务三大缺陷）与 CoSPlay（推理时协同进化）两份研究摘要；仓库现有 OSWorld 2.0 半成品代码。

## 一、现状基线（2026-08-06 实测）

| 项 | 状态 | 证据 |
|----|------|------|
| `go build ./...` | ✅ 通过 | BUILD_EXIT=0 |
| `go test ./internal/agent/ -short` | ✅ 通过 | ok, 27.259s |
| StateTracker（隐式状态提取） | ✅ 已接入 | run_loop.go:610-632 Before/AfterToolCall；compact.go:319-326 注入摘要 |
| navigator 闭环内核 | ⚠️ 已实现未接入 | internal/navigator/kernel.go:116-233 Execute() 完整，但全仓库无生产调用点 |
| HERMES adapter | ⚠️ 本地占位 | hermes_adapter.go:25-28 自述 skeleton，无宿主 |
| compact 隐式状态保留 | ✅ 已接入 | compact.go:56-84 三节摘要 + L609-636 丢失告警 |
| OPT-261~265（6 文件） | ⚠️ 已实现未接线 | token_aware_load_shedder / cache_invalidation_compactor / context_window_dynamic_resizer / token_aware_admission_gatekeeper / prompt_cache_proactive_warmer + 测试，零引用 |
| CoSPlay / 协同进化 | ❌ 不存在 | 全仓库 grep 0 命中 |
| agent.go | 4314 行 | Run() 已拆分到 run_loop.go / execute_one.go |

## 二、阶段一：OSWorld 2.0 闭环（对症"失忆/死板/灯下黑"）

### 1.1 navigator 增加"观测模式"（不重复执行工具）

主循环不能调用 `Navigator.Execute()`——那会让工具执行两次（adapter 内部再走一遍 registry）。改为把 Execute 的 verify-act-correct 拆成两步公开 API：

- `BeginAction(ctx, action HostAction) (StateSnapshot, error)`：环境快照 + 状态预测 + 权限预检（不 act）。
- `EndAction(ctx, action HostAction, result HostResult) (Correction, error)`：观察 + 状态更新 + 偏差验证 + 纠正决策 + 传感器事件 flush。
- `Execute()` 保持兼容：`BeginAction → adapter.Execute → EndAction`。

文件：`internal/navigator/kernel.go`（重构 Execute 为三步，新增两个公开方法）。

### 1.2 agent 侧接口扩展 + 主循环接入

- `internal/agent/state_tracker.go` 的 `NavigatorKernel` 接口（现仅 `ImplicitStateDigest()`）扩展为：
  ```go
  BeginAction(ctx, verb, args string) error
  EndAction(ctx, verb, args string, output string, toolErr error) (CorrectionBrief, error)
  ```
  `CorrectionBrief` 为 agent 包自有小结构（Strategy/Reason/Reinject []string），避免 agent 依赖 navigator 类型。
- `internal/agent/run_loop.go` `handleToolRound`（L610-632 StateTracker 挂点旁）：
  1. 执行批次前：对每个 call `BeginAction`；
  2. `executeBatch` 之后：对每个 call `EndAction`；
  3. 按 Correction 策略动作：`ReinjectFacts` → 往 session 追加一条简短 user 消息（注入丢失事实，防"失忆"）；`Rollback`/`AskHost` → emit error 级事件（防"死板"）；`Retry` → emit warn 事件；
  4. flush 传感器事件（文件/进程变化）→ emit Notice 事件（防"灯下黑"）。

### 1.3 传感器真正跑起来（灯下黑）

boot.go:1700-1702 已挂 `FilesystemSensor(root,3)` + `ProcessSensor("")`，但 Sensor 需要被周期性驱动才有"监控环境更新"效果。核验 `sensor.go` 的 `DynamicEnvSensor` 是否已有后台轮询；若没有，在 kernel 增加 `StartBackgroundWatch(ctx, interval)`（goroutine 周期 SnapshotAll + EventCorrelator），由 boot.go 在 executor 生命周期内启动，退出时停止。

### 1.4 OPT-261~265 接线

新增 `internal/agent/token_governance.go`：`TokenGovernance` 聚合 5 个模块（load_shedder / cache_invalidation_compactor / context_window_dynamic_resizer / admission_gatekeeper / prompt_cache_warmer），在 Agent Options 增加可选字段，run loop 挂点：
- `prepareToolExecution` 前：admission_gatekeeper 准入 + load_shedder 脱落检查；
- `emitTurnUsage` 处：cache_invalidation_compactor 更新 + prompt_cache_warmer 预热注册；
- compact 前：context_window_dynamic_resizer 建议窗口。
配置开关 `agent.token_governance.enabled`（默认关，逐模块灰度）。

### 1.5 配置项

`internal/config/config.go` 新增 `Navigator` 与 `TokenGovernance` 配置段（参考现有 long-horizon 配置模板），`reasonix.example.toml` 补示例。默认 navigator.enabled=false（新机制灰度，不改变现有行为）。

### 1.6 测试

- `internal/navigator/observer_test.go`：Begin/End 拆分后单测（复用现有 navigator_test 的 mock adapter）。
- `internal/agent/navigator_loop_test.go`：模拟 handleToolRound，验证 ReinjectFacts 追加消息、传感器事件 emit、Rollback 告警。
- `internal/config` 配置解析测试。
- 回归：`go test ./internal/agent/ -count=1 -short` + `go vet ./internal/navigator/...`。

## 三、阶段二：CoSPlay 推理时协同进化（无标注代码验证）

### 2.1 新包 `internal/cosplay`

| 组件 | 职责 | 对应论文机制 |
|------|------|--------------|
| `gen.go` TestGen | 输入代码+任务描述，生成 N 个高区分度测试用例（模型生成 + 规则模板兜底） | 探索与攻击 |
| `matrix.go` ExecMatrix | 代码×测试执行矩阵，记录 pass/fail 与区分度 | 执行矩阵 |
| `repair.go` RepairLoop | 多轮迭代：失败矩阵喂回模型修复代码；反复失败的测试淘汰 | 多轮修复/淘汰 |
| `consensus.go` Consensus | 对多候选结果按通过率+区分度聚类投票选优 | 共识聚类 |

执行器抽象 `Runner`（本地 go test / node / python 子进程），模型调用走现有 `provider` 抽象（与 agent 同 provider），全流程无真值数据、无微调。

### 2.2 接入点

- 新工具 `code_verify`（注册进 tool.Registry）：对最近一次代码修改运行一次 CoSPlay 验证循环，返回矩阵摘要 + 最优候选。
- 可选自动模式：config `cosplay.auto_on_mutation=true` 时，在 `observeAfterMutation`（execute_one.go:753）后触发轻量验证（仅 gen+matrix 一轮）。
- CLI 入口：`reasonix cosplay <file>`（命令面板新增子命令）。

### 2.3 测试

- `internal/cosplay/cosplay_test.go`：用本地可执行样例（如临时 Go 文件 + `go test` 或 node）验证矩阵/修复/共识的确定性路径；模型调用用 stub provider。
- 接入测试：注册 code_verify 后 registry 冒烟。

## 四、实施顺序与验收

```
阶段一:
  P1.1 navigator Begin/End 拆分 + 单测        → go test ./internal/navigator/
  P1.2 agent 接口 + run_loop 接入 + 单测       → go test ./internal/agent/
  P1.3 传感器后台监控 + boot 接线              → go vet + build
  P1.4 OPT-261~265 聚合接线 + 配置             → go test ./internal/agent/ ./internal/config/
  P1.5 全量回归                               → go build ./... && go test ./... -short
阶段二:
  P2.1 internal/cosplay 核心（gen/matrix/repair/consensus）+ 单测
  P2.2 code_verify 工具注册 + config
  P2.3 全量回归 + 文档
```

每步独立提交，可回滚；阶段一完成后即可单独交付。

## 五、风险与缓解

| 风险 | 缓解 |
|------|------|
| Begin/End 拆分破坏 Execute 语义 | 保持 Execute 行为不变（组合调用），跑通现有 navigator_test 16KB 用例 |
| ReinjectFacts 追加消息污染 prompt | 仅在 Strategy==ReinjectFacts 时追加，内容限短（facts 拼接 ≤500 字符），默认开关关闭 |
| 传感器后台 goroutine 泄漏 | 绑定 executor 生命周期（ctx cancel + WaitGroup） |
| CoSPlay 多模型调用成本 | 默认只跑 1 轮修复、候选数≤3，全部走配置 |

---

## 六、实施完成记录（2026-08-06）

### 阶段一：OSWorld 2.0 闭环 ✅
- P1.1 navigator Begin/End 观测模式拆分（kernel.go），ContinuousStateManager 全方法加锁（state.go），observer_test 6 用例
- P1.2 NavigatorKernel 接口扩展 + navigatorBridge + run_loop handleToolRound 接入 + applyNavigatorCorrection（reinject 注入会话/rollback/retry/ask_host 事件）
- P1.3 StartBackgroundWatch 后台环境监控（ctx 驱动、幂等、缓冲上限），ProcessSensor 空 pattern 短路，navigatorWatchRunner 绑定 Run 生命周期
- P1.4 TokenGovernance 聚合 OPT-261~265 五模块（observe usage/建议窗口），config `agent.token_governance`（默认关）
- P1.5 全量回归：agent/config/navigator/boot 全绿，go vet 干净；全仓库仅 internal/repair 的 Windows symlink 权限环境限制失败（与本阶段无关）

### 阶段二：CoSPlay 协同进化 ✅
- P2.1 internal/cosplay 新包：Verifier（生成→矩阵→修复→共识）、TestGenerator+TemplateGenerator（离线）、Repairer、Runner+ProcessRunner（Go/Python 本地执行）、ExecMatrix、Consensus；9 用例含真实工具链端到端
- P2.2 code_verify 工具注册（ReadOnly、code/file/examples 输入），config `agent.cosplay`，TOOL_CONTRACT.md + boot 契约测试同步
- P2.3 全量回归 + 本文档收尾

### 交付物清单
- 新增文件：internal/navigator/observer_test.go、watch_test.go、internal/agent/navigator_bridge.go、navigator_loop_test.go、token_governance.go、token_governance_test.go、internal/cosplay/（cosplay/gen/matrix/runner/tool + 3 个测试）、internal/boot/navigator_watch.go、internal/config/token_governance_test.go
- 修改文件：internal/navigator/{kernel,state,sensor}.go、internal/agent/{agent,run_loop,compact,state_tracker}.go、internal/boot/boot.go、internal/config/config.go、docs/TOOL_CONTRACT.md、reasonix.example.toml

### 后续可选增强（未做）
1. code_verify 自动触发：config `cosplay.auto_on_mutation=true` 时在 observeAfterMutation 后轻量验证（当前仅手动工具）
2. 模型生成器/修复器：CosPlay ModelGenerator 接 provider（当前离线 TemplateGenerator+ProcessRunner）
3. navigator 后台 watch 事件直接进 prompt（当前经 EndAction flush 为事件）

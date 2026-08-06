# Agent 重构计划

> 生成日期: 2026-08-05
> 目标: 满足 REASONIX.md 中 arch-optimize skill 的 7 条解封条件
> 状态: 规划完成，待执行

## 一、现状分析

### 1.1 核心指标

| 指标 | 实测值 | 目标值 | 差距 |
|------|--------|--------|------|
| agent.go 行数 | **5566** | ≤800 | 7.0x |
| Agent struct 字段数 | **328** | ≤50 | 6.6x |
| Agent.Run() 行数 | **956** (L3278-4233) | ≤100 | 9.6x |
| >50 行函数数（全包） | **51** | ≤20 | 2.55x |
| NewTaskTool 参数数 | **18** (task.go:234) | ≤5 | 3.6x |
| 测试 skip 数 | **6** 处 | 0 | — |
| Health score | **0** | ≥50 | — |

### 1.2 Agent struct 字段分布（328 字段，L253-1169）

| 分组 | 字段数 | 占比 | 说明 |
|------|--------|------|------|
| **OPT token 优化模块** (OPT-03~260) | **~249** | 76% | 每个 OPT 模块 1 个指针字段，是 struct 膨胀根因 |
| 核心运行时 | ~15 | 5% | prov, tools, session, sink, maxSteps, temperature 等 |
| 上下文管理 | ~12 | 4% | contextWindow, compactRatio, recentKeep, archiveDir 等 |
| Steer 队列 | ~5 | 2% | steerMu, steerQueue, steerConsumed 等 |
| Evidence/Todo | ~6 | 2% | evidence, todoState, todoMu 等 |
| 记忆编译器 | ~8 | 2% | memoryCompiler, compilerTurn 等 |
| 能力/交付 | ~10 | 3% | capabilityLedger, deliveryProfile 等 |
| Plan-mode | ~6 | 2% | planMode, gate 等 |
| 场景/审核 | ~7 | 2% | sceneClassifier, reviewGate 等 |
| Storm/Loop guard | ~6 | 2% | stormSig, stormCount 等 |
| 其他 | ~4 | 1% | hooks, asker, jobs 等 |

**关键发现**：OPT 模块字段占 76%，是唯一的结构性膨胀源。每个字段是独立指针，通过 `if a.xxxModule != nil { a.xxxModule.Method(...) }` 模式调用。

### 1.3 Agent.Run() 结构（956 行，L3278-4233）

| 逻辑块 | 行范围 | 行数 | 可提取性 |
|--------|--------|------|----------|
| Turn 初始化 | 3278-3365 | 87 | 易 → `a.initTurn()` |
| 场景分类 + OPT 重置 | 3366-3433 | 67 | 易 → `a.classifyScene()` + `a.resetOptModules()` |
| stream 调用 + 错误恢复 | 3434-3477 | 43 | 中 → `a.streamWithRecovery()` |
| **OPT 模块 hooks** | **3478-4107** | **~630** | **易 → `a.runOptHooks(usage, step)`** |
| Usage 发射 + 消息存储 | 4108-4133 | 25 | 易 → `a.emitUsage(usage)` |
| Final readiness 检查 | 4135-4190 | 55 | 中 → `a.handleNoCalls()` |
| 工具执行 + storm breaker | 4191-4232 | 41 | 中 → `a.executeCallsAndAdvance()` |

**关键发现**：OPT hooks 占 Run() 的 66%（630/956），全是机械重复的 `if a.xxx != nil { ... }` 块。

### 1.4 Top 15 长函数

| # | 函数 | 文件 | 起始行 | 行数 | 拆分难度 |
|---|------|------|--------|------|----------|
| 1 | `Agent.Run()` | agent.go | 3278 | 956 | 中（OPT hooks 提取后骤降） |
| 2 | `Agent.GetAllTokenOptStats()` | agent.go | 2520 | 719 | **极易**（239 行重复赋值） |
| 3 | `New()` | agent.go | 1751 | 691 | 中（OPT 初始化占 ~500 行） |
| 4 | `Agent.executeOne()` | agent.go | 5466 | 415 | 中（plan-mode 分支可提取） |
| 5 | `ParallelTasksTool.Execute()` | parallel_tasks.go | 86 | 197 | 中 |
| 6 | `migrateLegacySessionsWithMarkers()` | migrate.go | 107 | 191 | 低 |
| 7 | `Session.save()` | save.go | 185 | 184 | 中 |
| 8 | `Session.SaveRecoveryBranch()` | save.go | 550 | 136 | 中 |
| 9 | `UnifiedTokenOrchestrator.Orchestrate()` | unified_token_orchestrator.go | 104 | 134 | 中 |
| 10 | `Agent.executeBatch()` | agent.go | 5081 | 133 | 中 |
| 11 | `Session.checkSnapshotWrite()` | save.go | 405 | 129 | 中 |
| 12 | `Agent.stream()` | agent.go | 4922 | 125 | 中 |
| 13 | `tbnAllocate()` | token_budget_negotiator.go | 145 | 119 | 中 |
| 14 | `SmartContextPruner.PruneContext()` | smart_context_pruner.go | 59 | 115 | 中 |
| 15 | `Agent.finalReadinessCheck()` | agent.go | 4337 | 107 | 中 |

### 1.5 NewTaskTool 18 参数（task.go:234）

| 分组 | 参数 |
|------|------|
| Provider (3) | prov, pricing, resolveProvider |
| 上下文/压缩 (6) | contextWindow, recentKeep, softCompactRatio, toolResultSnipRatio, compactRatio, compactForceRatio |
| 步骤/采样 (2) | maxSteps, temperature |
| 模型 (2) | subagentModel, subagentEffort |
| 安全/Prompt (3) | gate, sysPrompt, keepPolicy |
| 路径 (1) | archiveDir |

合并后可降至 5 个：`prov, parentReg, ctxCfg, modelCfg, gateCfg`

### 1.6 测试 skip 分析

| 文件 | 行 | 原因 | 可修? |
|------|-----|------|-------|
| cachehit_e2e_test.go:380 | 环境门控 | 否（设计如此） |
| canonical_path_test.go:33 | Windows-only | 否（设计如此） |
| listsessions_bench_test.go:24,130 | 基准门控 | 否（设计如此） |
| **opt03_28_test.go:155** | "requires tool package import" | **是** |
| **session_events_test.go:379** | "checkpoints diverged" | **可能** |

真正的测试债务只有 1-2 处，条件 6 接近达成。

---

## 二、7 条条件可行性评估

| 条件 | 难度 | 评估 | 核心障碍 |
|------|------|------|----------|
| 1. agent.go ≤800 行 | 中 | 可达成 | 需提取大函数 + OPT registry |
| 2. struct ≤50 字段 | 中 | 可达成 | 249 OPT 字段合并 + 子 struct 分组 |
| 3. Run() ≤100 行 | 中 | 可达成 | OPT hooks 提取后骤降 |
| 4. >50 行函数 ≤20 | 中高 | 可达成但量大 | 当前 85 个，需拆 65+ |
| 5. NewTaskTool ≤5 参数 | **低** | **易达成** | 纯机械 config struct 合并 |
| 6. 测试 100% 通过 | **低** | **接近达成** | 只有 1-2 处真 skip |
| 7. Health ≥50 | 高 | 依赖前 6 条 | 是结果指标 |

**关键洞察**：条件 1/2/3 有共同根因——OPT 模块膨胀。引入 OPT registry 可同时解决这三个条件的 70%+ 工作量。

---

## 三、分步重构计划

### 步骤 0：前置准备（安全网）

- 确认 `go test ./internal/agent/ -count=1 -short` 基线通过
- 确认 `go vet ./internal/agent/` 无警告
- 记录当前测试通过率作为回归基线

**风险**：无

### 步骤 1：提取 GetAllTokenOptStats 到独立文件（热身）

- 新建 `internal/agent/opt_stats.go`
- 移动 `GetAllTokenOptStats()`（L2520-3238，719 行）
- agent.go: 6291 → ~5570

**风险**：极低（纯机械移动）

### 步骤 2：引入 OPT Module Registry（最高杠杆）

- 新建 `internal/agent/opt_registry.go`，定义 `OPTModule` 接口 + `OPTRedistry`
- Agent struct 249 个 OPT 字段 → 1 个 `optRegistry *OPTRedistry` 字段
- `New()` 中 ~500 行 OPT 初始化 → `a.optRegistry = newOPTRedistry(opts)`
- `Run()` 中 ~630 行 OPT hooks → `a.optRegistry.OnStreamComplete(ctx)`
- `GetAllTokenOptStats()` 719 行 → `a.optRegistry.CollectAllStats()`

**预计变化**：
- agent.go: 5570 → ~3870（-1700）
- struct: 328 → ~80（-248）
- Run(): 956 → ~280（-676）
- New(): 691 → ~190（-501）

**风险**：中高（249 模块接线变更，但内部逻辑不变）
**缓解**：分批迁移 OPT-03~50 验证后继续；适配器模式逐步迁移

### 步骤 3：拆分 Run() 为子方法

- 提取 6-7 个私有方法：`initTurn()` / `classifyScene()` / `resetPerTurnState()` / `runMainLoop()` / `streamWithRecovery()` / `emitTurnUsage()` / `handleNoCalls()`
- 新建 `internal/agent/run_loop.go`

**预计变化**：Run(): ~280 → ~80 行

**风险**：中（闭包变量传递、defer 语义保持）

### 步骤 4：拆分 New() + 提取 Agent 子 struct

- `New()` 拆分为 `initCore()` / `initContextManagement()` / `initSecurity()` / `initMemoryCompiler()`
- Agent struct ~80 字段分组为 8 个子 struct：
  - `optRegistry *OPTRedistry`（替代 249 OPT 字段）
  - `steerMgr *SteerManager`（5 字段）
  - `evidenceMgr *EvidenceManager`（6 字段）
  - `memCompiler *MemCompilerState`（8 字段）
  - `deliveryMgr *DeliveryManager`（10 字段）
  - `loopGuard *LoopGuardState`（6 字段）
  - `sceneMgr *SceneManager`（7 字段）
  - `ctxMgr *ContextManager`（12 字段）
- 总字段: ~15 核心 + 8 子系统 = ~23

**预计变化**：struct: ~80 → ~23 字段（≤50 达成）

**风险**：中高（子 struct 方法需传递 *Agent 或依赖）

### 步骤 5：拆分 executeOne 和其他长函数

- `executeOne()`（415 行）：提取 plan-mode 分支
- `executeBatch()`（133 行）：提取并行执行
- `stream()`（125 行）：提取流式子逻辑
- `finalReadinessCheck()`（107 行）：提取 todo 检查
- `save.go` 的 `save()`（184 行）：分阶段
- 其他中等函数按需

**预计变化**：>50 行函数 85 → ≤20

**风险**：中（逐函数拆分，每个独立提交）

### 步骤 6：重构 NewTaskTool 参数 + 修复测试 skip

- task.go:234：18 参数 → 1 个 `TaskToolConfig` struct
- 更新所有调用点（编译器会捕获遗漏）
- 修复 `opt03_28_test.go:155` 的 skip
- 评估 `session_events_test.go:379` 的 skip

**预计变化**：NewTaskTool 参数 18 → 1

**风险**：低（纯机械重构）

### 步骤 7：最终文件拆分（agent.go ≤800 行）

按职责拆分为 8-10 个文件：
- `agent.go` (~400) struct + New() + 核心方法
- `agent_setters.go` (~200) setter
- `agent_getters.go` (~150) getter
- `run_loop.go` (~300) Run() + 子方法
- `execute.go` (~500) executeOne + executeBatch
- `stream.go` (~150) stream + 恢复
- `readiness.go` (~200) finalReadinessCheck + todo
- `planmode_gating.go` (~200) plan-mode 决策
- `shell_analysis.go` (~250) bash redirect 分析
- `storm_breaker.go` (~150) storm breaker

**预计变化**：agent.go: ~2400 → ~400 行（≤800 达成）

**风险**：低（纯文件级移动）

---

## 四、执行顺序与依赖

```
步骤0 (基线)
  ↓
步骤1 (提取 GetAllTokenOptStats) ← 无依赖，可立即开始
  ↓
步骤2 (OPT Registry) ← 最高杠杆，步骤3/4 依赖
  ↓
步骤3 (拆分 Run())  ←  步骤4 (拆分 New()+子struct) ← 可并行
  ↓                      ↓
步骤5 (拆分长函数) ← 依赖步骤3/4
  ↓
步骤6 (NewTaskTool+测试) ← 独立，可与步骤5并行
  ↓
步骤7 (最终文件拆分) ← 最后
  ↓
条件7 (Health≥50) ← 自然达成
```

## 五、风险矩阵

| 步骤 | 风险 | 影响范围 | 回滚成本 | 缓解 |
|------|------|----------|----------|------|
| 1 | 极低 | 1 函数 | 1 commit revert | 无 |
| 2 | **中高** | 249 模块接线 | 高 | 分批迁移+适配器 |
| 3 | 中 | Run() 逻辑 | 中 | 完整 e2e 测试 |
| 4 | 中高 | struct 重组 | 高 | 逐子 struct 迁移 |
| 5 | 中 | 多函数 | 低 | 逐函数提交 |
| 6 | 低 | 1 函数签名 | 低 | 编译器验证 |
| 7 | 低 | 文件移动 | 极低 | git mv |

## 六、完成度预测

| 步骤后 | 条件1 | 条件2 | 条件3 | 条件4 | 条件5 | 条件6 | 条件7 |
|--------|-------|-------|-------|-------|-------|-------|-------|
| 初始 | ✗5566 | ✗328 | ✗956 | ✗51 | ✗18 | ✗6 | ✗0 |
| 步骤1 | ✗5570 | ✗328 | ✗956 | ✗85 | ✗18 | ✗6 | ✗ |
| 步骤2 | ✗3870 | ✗80 | ✗280 | ✗~60 | ✗18 | ✗6 | △~25 |
| 步骤3 | ✗3870 | ✗80 | ✓~80 | ✗~55 | ✗18 | ✗6 | △~30 |
| 步骤4 | ✗3200 | ✓~23 | ✓ | ✗~50 | ✗18 | ✗6 | △~35 |
| 步骤5 | ✗2400 | ✓ | ✓ | ✓~18 | ✗18 | ✗6 | △~42 |
| 步骤6 | ✗2400 | ✓ | ✓ | ✓ | ✓1 | ✓~2 | △~45 |
| 步骤7 | ✓~400 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓~55 |

> **2026-08-05 实测校正**：agent.go 实测 5566 行（非文档原写的 6291），>50 行函数实测 51 个（非 85）。上表"步骤 1-7"行的预测数字仍基于旧基线 6291/85，需按新基线 5566/51 重算——条件 1/4 的实际差距比原表小，重构工作量也相应降低。

**结论**：7 步全部完成后 7 条条件全部满足。步骤 2（OPT Registry）是最高杠杆，单独完成即可让条件 1/2/3 取得 70%+ 进展。步骤 1/5/6/7 风险极低，可作为热身和收尾。

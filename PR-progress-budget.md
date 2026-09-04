# PR: 进度预算（Progress Budget）可配置化

> 目标分支：`main-v2` · 提交：`9ff0324a2 add budget config`（合并上游后基线 `2e1d494f3`）

## Summary

Agent 内置了固定的进度检查点：当活跃 todo 连续 **8 轮**工具调用没有新的主机可见进展（新完成、新读取、新命令、新写入）时，主机强制注入一条“重新评估”提示。该阈值写死在 `internal/agent` 中，对长任务（大范围重构、批量迁移）过低，会频繁打断正常工作流，且用户无法调整或关闭。

本 PR 将该检查点变为**用户可拥有的设置**：

- **内核**：`agent.Options` 新增 `ProgressBudgetRounds`，由 `NormalizeProgressBudgetRounds` 统一归一化（`0` = 内置默认 8；负数 = 关闭；越界值钳制到 **3–64**）。Goal 模式的二级重规划检查点改为 `nudgeRounds × 2` 派生，默认值下与旧行为（8/16）完全一致。
- **配置**：`[agent]` 新增两个 TOML 键——`progress_budget`（`*bool`，缺省视为开启，仅显式 `false` 关闭）与 `progress_budget_rounds`（`0` = 内置默认）；提供 `SetProgressBudgetEnabled` / `SetProgressBudgetRounds` 编辑器方法（关闭时保留轮次数，重新开启即恢复用户阈值而非回退默认值），TOML 渲染/差异输出同步支持。
- **启动接线**：`boot` 构建 executor 时经 `progressBudgetRoundsFromConfig` 注入；禁用时传入 `agent.ProgressBudgetRoundsOff` 哨兵，保证不会静默回退到内置默认。
- **桌面端**：`AgentView` 暴露 `progressBudgetEnabled` / `progressBudgetRounds`（始终返回生效值，便于数字输入框直接渲染）；新增 `App.SetProgressBudgetEnabled` / `App.SetProgressBudgetRounds` Wails 方法，复用 `applyConfigChange` 自动热重建会话。
- **前端**：设置 → 模型 → 使用 → **Agent 运行**下方新增“进度预算”设置区：
  - 复选框开关：待办停滞时是否要求助手重新评估（关闭后零进展阶梯与循环守卫仍兜底）；
  - 触发轮次数字输入：3–64，Enter 应用 / Esc 还原，含范围校验与错误提示；
  - 旧后端缺省字段时默认开启，不会因字段缺失而禁用检查点。
- **文档**：`docs/GUIDE.md`、`docs/GUIDE.zh-CN.md`、`reasonix.example.toml` 补充两个新键的说明。

### 行为对照

| 场景 | 改动前 | 改动后 |
|---|---|---|
| 默认配置 | 8 轮后催促重新评估 | 相同（`0`/缺省 = 内置默认） |
| 长任务频繁被打断 | 无法调整 | 调高 `progress_budget_rounds`（最高 64） |
| 不希望被打扰 | 无法关闭 | `progress_budget = false` 或 UI 关闭开关 |
| Goal 二级重规划 | 固定 16 轮 | `2 × nudgeRounds`，默认下仍为 16 |

### 兼容性

- 旧配置文件不含新键：行为与之前完全一致（缺省 = 开启 + 默认 8 轮）。
- 旧前端 bundle 调新后端：`AgentView` 新字段仅追加，不破坏现有 JSON 契约。
- 新前端调旧后端：前端将缺失字段按“开启 + 默认 8”渲染，不误显示为关闭。

## Changed files

**内核（internal/）**

| 文件 | 说明 |
|---|---|
| `internal/agent/storm_breaker.go` | 新增 `NormalizeProgressBudgetRounds`、`ProgressBudgetRoundsOff`、`DefaultProgressBudgetRounds`、`progressRedirectRounds` |
| `internal/agent/agent.go` | `Options.ProgressBudgetRounds`；`New` 归一化后写入 agentConfig |
| `internal/agent/agent_config.go` | `progressBudgetRounds` 字段 |
| `internal/agent/goal_run_boundary.go` | `trackTodoProgress` 改用配置阈值；`<=0` 跳过催促与 Goal 重定向 |
| `internal/agent/todo_progress_guard_test.go` | 测试改用派生阈值 `progressRedirectRounds(...)` |
| `internal/config/config.go` | `ProgressBudget *bool`、`ProgressBudgetRounds` 字段 + `ProgressBudgetEnabled()` / `ProgressBudgetRoundsValue()` |
| `internal/config/edit.go` | `SetProgressBudgetEnabled` / `SetProgressBudgetRounds`（拒绝负数） |
| `internal/config/render.go` | 全量渲染与 diff 渲染支持两个新键 |
| `internal/boot/task_budget.go` | `progressBudgetRoundsFromConfig` 映射（禁用 → off 哨兵） |
| `internal/boot/boot.go` | executor `agent.Options` 注入 |

**桌面端（desktop/）**

| 文件 | 说明 |
|---|---|
| `desktop/settings_app.go` | `AgentView` 新字段；`desktopProgressBudgetRounds` 帮助函数；`SetProgressBudgetEnabled` / `SetProgressBudgetRounds` |
| `desktop/reasoning_display_app.go` | 默认视图补齐新字段 |
| `desktop/frontend/src/components/SettingsPanel.tsx` | “进度预算”设置区（开关 + 轮次输入 + 校验） |
| `desktop/frontend/src/lib/types.ts` | `AgentView.progressBudgetEnabled?` / `progressBudgetRounds?` |
| `desktop/frontend/src/lib/bridge.ts` | 绑定接口、mock 实现、事件路由分组 |
| `desktop/frontend/src/locales/{en,zh,zh-TW}.ts` | 每语言 9 条新文案 |
| `desktop/frontend/scripts/check-bundle-budget.mjs` | 显式上调有界预算：initial gzip 463.4→463.8、raw 2471.8→2473.7、zh 60.4→60.9、zh-TW 61.3→61.6（KiB） |

**文档**

| 文件 | 说明 |
|---|---|
| `docs/GUIDE.md` / `docs/GUIDE.zh-CN.md` | `[agent]` 示例补充两个新键 |
| `reasonix.example.toml` | 示例配置补充注释行 |

## Issues

无关联 issue（功能需求来自桌面端使用反馈：内置阈值过低且不可配置）。

## Verification

**Go**

```powershell
go build ./...                                  # 主模块 OK
cd desktop; go build ./...                      # desktop 模块 OK
go test ./internal/agent/ ./internal/config/    # ok
go test ./internal/boot/... ./internal/control/...  # ok
go test ./internal/agent/ -run "TestTodoProgress|TestGoalTodoProgress|TestCanonicalTodoProgress|TestMaxStepsGrace" -v
# TestTodoProgressGuardNeverPausesARun (chat/goal)、TestGoalTodoProgressGuardReplansWithoutPausing、
# TestTodoProgressGuardRenewsOnUniqueHostWork、TestCanonicalTodoProgressIgnoresTitleAndPendingListChurn、
# TestMaxStepsGraceSummaryBypassesIncompleteTodoReadiness — 全部 PASS
cd desktop; go test .   # 除 2 个依赖 bash 的打包脚本测试（TestVerifyWindowsPortableVersionedLayout /
                        # TestWindowsPackagerRejectsMissingOrPartialRequiredPayloadManifest，环境无 bash，
                        # 改动前即失败，与本 PR 无关）外全部通过
```

**前端**

```powershell
cd desktop/frontend
npx tsc --noEmit                       # OK
npx tsc --noEmit -p tsconfig.test.json # OK
npx eslint src/components/SettingsPanel.tsx src/lib/bridge.ts src/lib/types.ts  # 0 问题
npx tsx src/__tests__/settings-refresh-snapshot.test.tsx   # 91/91 PASS
```

**端到端构建**：`wails build` 成功（含 eslint → tsc → vite → bundle 预算全链），产物 `desktop/build/bin/reasonix-desktop.exe`；CLI `bin/reasonix.exe version` 输出正常。

## Documentation impact

Documentation-impact: updated — `docs/GUIDE.md`、`docs/GUIDE.zh-CN.md` 与 `reasonix.example.toml` 已补充 `agent.progress_budget` / `agent.progress_budget_rounds` 两个新键的语义（缺省开启、0 = 内置默认、关闭后保留轮次数）。

## Cache impact

Cache-impact: none — 不触碰系统提示词、memory 前缀、output style、工具 schema、provider 请求序列化或压缩逻辑。检查点仅改变一条主机生成的用户消息（“Host progress check”/“Host progress redirect”）在会话中的**注入时机**，该消息本身及注入机制与改动前一致；默认阈值下会话内容与旧版本逐字节相同。

Cache-guard: `go test ./internal/agent/ -run "TestTodoProgress|TestGoalTodoProgress|TestCanonicalTodoProgress"` 覆盖默认阈值下的催促/重定向注入时机与内容；`TestTodoProgressGuardNeverPausesARun` 保证检查点不会结束运行（与旧行为一致的额外防线）。系统提示词零改动，无需 System-prompt-review。

System-prompt-review: N/A

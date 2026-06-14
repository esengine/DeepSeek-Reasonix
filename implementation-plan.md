# Reasonix 常驻个人 AgentOS 落地计划

本文档描述如何把 `todo.md` 中的方向落成可交付工程。它关注执行顺序、PR
拆分、状态模型、测试策略、风险闸门和上线节奏。

## 1. 目标

把 Reasonix 从“用户发一轮消息，agent 跑一轮”的交互形态，逐步升级为：

- 可以恢复长期目标的 session runtime。
- 可以被用户、IM bot、cron、webhook、文件变化显式唤醒。
- 可以在后台常驻，但默认只在需要模型决策时消耗模型调用。
- 可以等待审批、等待 CI、等待时间或等待外部事件。
- 可以让桌面端、CLI、bot 都看到同一套任务状态。

第一阶段不追求完整 workflow engine，也不追求插件生态。最小可落地产品是：

> Reasonix 能记住一个 active goal，重启后恢复它，用户或 bot 可以显式继续，
> 后续再由 daemon + scheduler 负责自动唤醒。

## 2. 非目标

- 不在第一阶段实现企业多租户、团队权限或共享任务池。
- 不在第一阶段开放第三方插件市场。
- 不把动态 runtime 状态塞进 system prompt、工具 schema 或 provider 稳定前缀。
- 不把所有唤醒都交给模型判断。时间、去重、路由、预算等必须是确定性逻辑。
- 不在 resume 时默认自动跑任务，避免用户只是打开历史时触发写操作。

## 3. 成功标准

### MVP 成功标准

- 创建 goal 后，session snapshot 会保存 runtime sidecar。
- 重启或 resume 后，controller 能恢复 active goal、status、轮次和 block audit。
- `/goal continue` 能显式继续恢复后的 goal。
- 桌面 meta 和 bot `/status` 能显示恢复后的 goal 状态。
- 文档、单元测试和 cache 稳定性检查覆盖核心路径。

### V1 成功标准

- daemon 能管理 session registry，并在桌面端关闭后保持可用。
- cron 可以唤醒指定 session，并且具备去重和预算限制。
- bot gateway 可以绑定已有 session，并从 IM 继续 goal。
- waiting approval / waiting event / waiting time 有统一状态表达。

### V2 成功标准

- GitHub PR / issue / CI webhook 可以唤醒对应 goal。
- 常驻 agent 可以等待 CI，CI 通过后继续准备下一步。
- 用户能从桌面端或 bot 查看 run timeline、wakeup history 和待审批事项。

## 4. 架构原则

### 4.1 Runtime 状态外置

长期状态写入 session sidecar，而不是塞进 transcript 或 prompt 前缀。

建议新增：

```text
<session>.runtime.json
```

这样可以保持 `.jsonl` transcript 格式稳定，并避免把 runtime 状态混入
`BranchMeta` 的导航字段。

### 4.2 Controller 是权威状态源

桌面 tab profile 只能保存 UI 偏好。active goal、run status、blocked audit、
wait condition 等状态应由 controller runtime sidecar 恢复。

### 4.3 所有唤醒统一成 RunIntent

用户输入、bot 消息、cron、webhook、file watch 都先转成统一的 `RunIntent`，再由
controller 或 daemon 判断是否可以执行。

```text
Trigger -> RunIntent -> Scheduler/Queue -> Controller -> Agent.Run
```

### 4.4 明确执行与观察边界

daemon 持有执行权。桌面、CLI、bot 可以观察、提交 intent、审批、停止，但不能各自
直接并发驱动同一个 session。

### 4.5 高风险动作仍需审批

外部事件只允许唤醒和提供 bounded context。提交代码、发布、删除文件、修改远端状态
等仍走现有 approval 机制。

## 5. 核心数据模型

### 5.1 Runtime Sidecar

建议 schema：

```json
{
  "version": 1,
  "session_id": "20260613-120000.000000000-deepseek-chat",
  "updated_at": "2026-06-13T12:00:00Z",
  "goal": {
    "text": "review open PRs and prepare triage drafts",
    "status": "running",
    "turns": 2,
    "block_count": 0,
    "block_reason": "",
    "last_marker": "continue",
    "updated_at": "2026-06-13T12:00:00Z"
  },
  "run": {
    "status": "idle",
    "current_run_id": "",
    "last_run_id": "run_20260613_120000",
    "last_turn_at": "2026-06-13T12:00:00Z",
    "last_error": "",
    "resume_count": 1
  },
  "wait": {
    "kind": "",
    "reason": "",
    "until": "",
    "event_source": "",
    "event_key": ""
  },
  "scheduler": {
    "enabled": false,
    "next_wakeup_at": "",
    "last_wakeup_at": "",
    "last_wakeup_reason": "",
    "last_event_id": ""
  },
  "budget": {
    "daily_model_turns": 0,
    "daily_model_turn_limit": 0,
    "spent_usd_micros": 0,
    "spent_usd_micros_limit": 0,
    "window_started_at": ""
  }
}
```

### 5.2 RunIntent

`RunIntent` 是所有唤醒入口的统一输入。

```go
type RunIntent struct {
    SessionPath string
    Source      string // user | bot | cron | webhook | file_watch | daemon
    Reason      string
    EventID     string
    Text        string
    CreatedAt   time.Time
    RequiresModel bool
}
```

第一版可以先不公开这个类型，只在 daemon/scheduler 内部使用，但设计上要保持统一。

### 5.3 Run 状态机

```text
idle
  -> queued
  -> running
  -> idle
  -> waiting_approval
  -> waiting_event
  -> waiting_time
  -> blocked
  -> complete
  -> failed
  -> stopped
```

关键约束：

- 同一 session 同一时间只能有一个 `running` run。
- daemon 启动时发现 `running` 但 owner 不存在，应改成 `interrupted` 或 `failed`，
  不能直接继续。
- `waiting_*` 状态必须有可解释的恢复条件。

## 6. PR 拆分计划

### PR 1: Runtime Sidecar 基础

标题建议：

```text
Persist runtime sidecar for goals / 持久化目标运行状态
```

范围：

- 新增 runtime sidecar 类型和读写函数。
- 路径函数：`RuntimeMetaPath(sessionPath string)`。
- 原子写入：复用 `fileutil.ReplaceFile`。
- controller 暴露 `RuntimeSnapshot()`。
- snapshot 时写 runtime sidecar。

涉及文件：

- `internal/agent/runtime.go`
- `internal/agent/runtime_test.go`
- `internal/control/controller.go`
- `internal/control/controller_test.go`

验收：

- 设置 goal 后 `Snapshot()` 写入 runtime sidecar。
- goal complete / blocked 后 sidecar 更新。
- 损坏 sidecar 不影响 transcript load。
- `git diff --check` 通过。
- `go test ./internal/agent ./internal/control` 通过。

风险：

- 不改 provider 请求序列化。
- 不改 system prompt。
- 不改 tool schema。

### PR 2: Resume 恢复 Goal 状态

标题建议：

```text
Restore active goals on resume / 恢复会话目标状态
```

范围：

- `Controller.Resume` 读取 runtime sidecar。
- 增加 `RestoreRuntimeSnapshot`。
- 恢复 `goal`、`goalStatus`、`goalTurns`、`goalBlocks`、`goalBlock`。
- resume 只恢复状态，不自动执行。
- desktop meta 使用 controller 状态。

涉及文件：

- `internal/control/controller.go`
- `internal/control/resume_prune_test.go`
- `internal/control/goal_test.go`
- `desktop/app.go`
- `desktop/tabs.go`

验收：

- resume 后 `Goal()` 返回旧 goal。
- resume 后 `GoalStatus()` 返回旧状态。
- resume 后普通 user turn 会注入 active goal block。
- 预览历史不会触发 runtime restore 后的执行。
- `go test ./internal/control ./desktop` 中相关 focused tests 通过。

风险：

- blocked goal 的 audit 不能被错误清零。
- complete goal 不应恢复成 running。

### PR 3: `/goal continue` 显式续跑

标题建议：

```text
Add explicit goal continuation / 增加目标续跑入口
```

范围：

- 扩展 `/goal` 命令。
- 新增 `/goal continue`。
- 暴露 `ContinueGoal(ctx, reason)` 或等价 API。
- blocked goal 续跑时开启新的 blocked audit。
- 加入 user-facing notice。

涉及文件：

- `internal/control/slash.go`
- `internal/control/controller.go`
- `internal/control/goal_test.go`
- `docs/GUIDE.md`
- `docs/GUIDE.zh-CN.md`

验收：

- 无 active goal 时 `/goal continue` 给出清晰提示。
- running goal 可继续。
- blocked goal 可重新 audit。
- complete goal 不重复执行。
- `go test ./internal/control` 通过。

风险：

- 不允许 resume 时自动调用 `/goal continue`。
- 不绕过现有 approval。

### PR 4: Bot 绑定 Runtime Session

标题建议：

```text
Bind bot chats to persistent sessions / 绑定机器人会话
```

范围：

- bot gateway 创建 controller 时尝试恢复已有 session。
- 持久化 remote session mapping。
- `/status` 显示 goal 和 run 状态。
- 增加 `/goal continue` bot 命令。
- 处理 allowlist 和 session isolation。

涉及文件：

- `internal/bot/gateway.go`
- `internal/bot/session.go`
- `internal/bot/gateway_test.go`
- `internal/cli/bot.go`
- `internal/config/config.go`

验收：

- 同一 chat 重启后恢复同一 session。
- 群聊按当前 session key 规则隔离。
- 未授权用户无法触发绑定 session。
- bot `/status` 能看到 goal 状态。

风险：

- 不能把不同用户的长期 session 串起来。
- mapping 文件不能包含敏感 token。

### PR 5: Daemon Skeleton

标题建议：

```text
Add daemon runtime skeleton / 增加常驻进程骨架
```

范围：

- 新增 `reasonix daemon start`。
- session registry。
- local status API。
- 单实例锁。
- daemon doctor。
- 不接 cron/webhook，只做生命周期。

涉及文件：

- `internal/daemon/daemon.go`
- `internal/daemon/registry.go`
- `internal/daemon/server.go`
- `internal/daemon/daemon_test.go`
- `internal/cli/daemon.go`
- `internal/cli/cli.go`

验收：

- daemon 能启动并返回 status。
- 同一 config/session dir 第二个 daemon 启动失败。
- 启动时扫描 runtime sidecar。
- running orphan 被标记为 interrupted。

风险：

- 本地 API 默认只监听 loopback 或 unix socket。
- 不暴露外网控制面。

### PR 6: Cron Scheduler MVP

标题建议：

```text
Add cron wakeups for active goals / 增加定时唤醒
```

范围：

- schedule 配置。
- fixed interval 和 daily time。
- timezone。
- 去重窗口。
- budget precheck。
- 唤醒后生成 RunIntent。

涉及文件：

- `internal/daemon/scheduler.go`
- `internal/daemon/scheduler_test.go`
- `internal/config/config.go`
- `internal/config/render.go`
- `reasonix.example.toml`

验收：

- daily schedule 正确计算 next wakeup。
- missed wakeup 不补跑风暴。
- running session 不重复入队。
- budget 超限不触发模型调用。

风险：

- cron 唤醒要先做确定性检查。
- 不允许无 active goal 时盲目调用模型。

### PR 7: Webhook Wakeup MVP

标题建议：

```text
Add authenticated webhook wakeups / 增加鉴权事件唤醒
```

范围：

- webhook receiver。
- HMAC 或 shared secret 校验。
- payload size limit。
- event id 去重。
- GitHub workflow / PR event 初步路由。

涉及文件：

- `internal/daemon/webhook.go`
- `internal/daemon/webhook_test.go`
- `internal/config/config.go`
- `reasonix.example.toml`

验收：

- 无 secret 的请求被拒绝。
- 重复 event id 只处理一次。
- CI success event 可以唤醒 waiting_event run。
- 高风险操作仍走 approval。

风险：

- raw payload 不直接进 prompt。
- webhook 只生成 bounded summary。

### PR 8: Wait Conditions 和 Run Queue

标题建议：

```text
Track wait conditions in the run queue / 跟踪运行等待条件
```

范围：

- run queue。
- wait condition 持久化。
- waiting approval / event / time。
- timeline。
- status API 和 bot status 展示。

涉及文件：

- `internal/daemon/queue.go`
- `internal/daemon/timeline.go`
- `internal/agent/runtime.go`
- `internal/bot/render.go`
- `desktop/frontend/src/lib/types.ts`

验收：

- session 级串行执行。
- 不同 session 可限制并发。
- wait condition 可恢复。
- timeline 可查询。

风险：

- 状态机复杂度上升，需要 focused tests 锁住转移。

### PR 9: 桌面端观察和审批台

标题建议：

```text
Surface daemon tasks in desktop / 展示常驻任务状态
```

范围：

- daemon 连接状态。
- active goal / run status。
- waiting approvals。
- continue / stop / approve / deny 操作。
- 不做大规模视觉重构。

涉及文件：

- `desktop/app.go`
- `desktop/frontend/src/App.tsx`
- `desktop/frontend/src/components/StatusBar.tsx`
- `desktop/frontend/src/components/SettingsPanel.tsx`
- `desktop/frontend/src/lib/types.ts`

验收：

- 桌面端能显示 daemon 管理的 session。
- 用户能批准/拒绝等待中的任务。
- 关闭桌面端不影响 daemon。
- focused frontend tests 通过。

风险：

- 前端状态不能成为权威。
- Wails 和浏览器验证边界要分清。

### PR 10: 首批产品场景

标题建议：

```text
Add first persistent-agent workflows / 增加首批常驻场景
```

范围：

- PR / issue triage 模板。
- CI watcher 模板。
- 发布助手模板。
- 用户文档。
- 示例配置。

涉及文件：

- `docs/GUIDE.md`
- `docs/GUIDE.zh-CN.md`
- `docs/PERSISTENT_AGENT.md`
- `reasonix.example.toml`

验收：

- 用户能按文档配置一个 daily triage。
- CI watcher 能走 waiting_event。
- 发布助手在写操作前等待 approval。

风险：

- 场景模板不要假设 GitHub token 存在。
- 文档不要暴露个人仓库、账号或本机路径。

## 7. 里程碑计划

### M0: 设计定稿

周期：0.5-1 天

交付：

- runtime sidecar schema。
- 状态机图。
- `/goal continue` 行为定义。
- cache gate checklist。

完成条件：

- 确认 runtime 状态不会进入 stable prompt prefix。
- 确认 resume 不自动执行。
- 确认第一批 PR 拆分。

### M1: Runtime Sidecar

周期：1-2 天

交付：

- sidecar 读写。
- controller snapshot。
- focused tests。

完成条件：

- goal 状态能写盘。
- JSONL transcript 格式不变。
- 损坏 runtime sidecar 不阻断 resume。

### M2: Resume + Explicit Continue

周期：2-3 天

交付：

- resume 恢复 active goal。
- `/goal continue`。
- docs 更新。

完成条件：

- 用户手动恢复后能看到并继续 goal。
- 不会自动误跑。
- blocked audit 行为明确。

### M3: Bot Session Binding

周期：2-4 天

交付：

- bot 绑定已有 session。
- bot `/status`。
- bot `/goal continue`。

完成条件：

- IM 里能恢复并继续桌面/CLI 的 goal。
- allowlist 和 session isolation 测试通过。

### M4: Daemon Skeleton

周期：4-7 天

交付：

- daemon start。
- local status API。
- session registry。
- 单实例锁。
- interrupted run 恢复规则。

完成条件：

- 关闭桌面端后 daemon 仍运行。
- daemon 重启后 runtime 状态安全。
- 本地控制面不暴露外网。

### M5: Cron Wakeup

周期：3-5 天

交付：

- schedule config。
- scheduler loop。
- RunIntent queue。
- 去重和预算。

完成条件：

- daily triage 可配置。
- missed wakeup 不重复风暴。
- running session 不并发执行。

### M6: Webhook + Wait Conditions

周期：5-10 天

交付：

- webhook receiver。
- event id 去重。
- wait condition。
- CI watcher 场景。

完成条件：

- CI 绿灯后能唤醒 waiting run。
- webhook 安全校验覆盖。
- 高风险动作仍需 approval。

### M7: Desktop / Product Polish

周期：5-10 天

交付：

- 桌面状态展示。
- 审批台。
- 首批场景文档。
- release notes。

完成条件：

- 用户可观察、可暂停、可审批常驻任务。
- 端到端 daily triage 或 CI watcher demo 可跑通。

## 8. 控制器改造细节

### 8.1 新增 RuntimeSnapshot

建议：

```go
type RuntimeSnapshot struct {
    Version   int
    SessionID string
    UpdatedAt time.Time
    Goal      GoalRuntime
    Run       RunRuntime
    Wait      WaitRuntime
    Scheduler SchedulerRuntime
    Budget    BudgetRuntime
}
```

controller 内部提供：

```go
func (c *Controller) RuntimeSnapshot() agent.RuntimeSnapshot
func (c *Controller) RestoreRuntimeSnapshot(s agent.RuntimeSnapshot)
```

### 8.2 Snapshot 写入顺序

推荐顺序：

1. 保存 transcript `.jsonl`。
2. 保存 branch `.meta`。
3. 保存 runtime sidecar。

如果 runtime sidecar 写失败，应返回错误并让上层记录 warning。不要出现 transcript
已更新但 runtime 永久落后且无提示的情况。

### 8.3 Resume 读取顺序

推荐顺序：

1. 加载 transcript。
2. `Controller.Resume(session, path)`。
3. `maybeColdResumePrune(path)`。
4. 加载 runtime sidecar。
5. restore runtime。
6. emit notice。

注意：

- 如果 cold resume prune 改写 transcript，不应该改写 runtime goal。
- 如果 runtime sidecar 损坏，只降级为无 runtime 状态。

### 8.4 Goal Continue 行为

`/goal continue` 只在 active goal 存在时运行。

状态规则：

- `running`: 直接继续。
- `blocked`: 重置 blocked audit，然后继续。
- `complete`: 提示已完成，不继续。
- `stopped`: 如果仍有 goal text，可由用户显式恢复；第一版可以提示重新 `/goal <text>`。
- empty: 提示没有 active goal。

## 9. Daemon 设计细节

### 9.1 进程职责

daemon 负责：

- session registry。
- run queue。
- scheduler。
- webhook receiver。
- bot gateway 可选启动。
- local control API。
- runtime state repair。

daemon 不负责：

- 直接绕过 approval。
- 直接持久化密钥。
- 把外部大 payload 注入 prompt。

### 9.2 单实例锁

建议锁粒度：

```text
<config-dir>/daemon.lock
```

锁文件记录：

- pid
- started_at
- socket path 或 local API address
- version

启动时：

- 如果锁存在且进程活着，拒绝启动。
- 如果锁存在但进程不存在，清理并继续。
- 如果 local API 可用，提示用户已有 daemon。

### 9.3 Local API

第一版 API：

```text
GET  /status
GET  /sessions
GET  /sessions/{id}
POST /sessions/{id}/continue
POST /sessions/{id}/stop
POST /approvals/{id}/approve
POST /approvals/{id}/deny
POST /asks/{id}/answer
```

安全要求：

- 默认只绑定 loopback 或 unix socket。
- 如果使用 HTTP，应有本地 token 或随机 socket path。
- 不把 API 暴露到外网。

## 10. Scheduler 设计细节

### 10.1 Schedule 类型

第一版支持：

- `interval`: 每 N 分钟/小时。
- `daily`: 每天指定本地时间。

暂不支持复杂 cron 表达式。复杂表达式可以等确定性调度稳定后再加。

### 10.2 唤醒前检查

每次 wakeup 必须先检查：

- session 是否存在。
- runtime sidecar 是否可读。
- active goal 是否 running。
- run queue 是否已有任务。
- 是否超过自动轮次限制。
- 是否超过预算。
- schedule window 是否已经处理过。

只有通过检查后才生成 `RunIntent`。

### 10.3 去重键

建议：

```text
schedule_id + window_start + session_id
```

这样 daemon 重启后不会重复处理同一窗口。

## 11. Webhook 设计细节

### 11.1 事件接收

每个 webhook event 存储：

- source。
- event id。
- received_at。
- route key。
- bounded summary。
- raw payload hash。

raw payload 第一版可以只用于审计，不直接进入 prompt。

### 11.2 GitHub 路由

第一批支持：

- `issues.opened`
- `pull_request.opened`
- `pull_request.review_requested`
- `workflow_run.completed`
- `check_suite.completed`

路由策略：

- 如果事件关联已有 waiting condition，唤醒对应 session。
- 如果没有 waiting condition，但项目配置了 triage schedule，可创建 triage intent。
- 如果无法路由，只记录日志，不调用模型。

### 11.3 安全

- 必须校验 secret。
- 限制 payload size。
- 重放保护。
- 不接受 webhook 直接指定任意本地文件路径。
- 不接受 webhook 直接批准操作。

## 12. 测试矩阵

### 12.1 单元测试

- runtime sidecar path。
- runtime sidecar load/save。
- corrupt runtime fallback。
- controller restore。
- goal status transitions。
- scheduler next wakeup。
- schedule dedupe。
- webhook signature。
- run queue state transitions。

### 12.2 集成测试

- create goal -> snapshot -> new controller -> resume -> continue。
- bot message -> bind session -> restart gateway -> status。
- daemon start -> scan runtime -> status。
- cron wakeup -> queue -> run -> update runtime。
- webhook event -> waiting_event -> continue。

### 12.3 Cache 稳定性测试

需要确认：

- system prompt 未变化。
- tool schema 未变化。
- provider request 序列化未变化。
- runtime sidecar 内容不会进入 stable prefix。

如果某个 PR 碰到 provider 请求或 prompt 组装，需要跑更严格 cache guard。

### 12.4 手工验证

MVP 手工路径：

1. 启动桌面端。
2. 创建 goal。
3. 让它进入 `[goal:continue]` 或 blocked。
4. 关闭应用。
5. 重开并 resume session。
6. 确认 UI 显示 active goal。
7. 执行 `/goal continue`。
8. 确认继续执行并更新 sidecar。

Daemon 手工路径：

1. `reasonix daemon start`。
2. `reasonix daemon status`。
3. 创建 daily schedule。
4. 调整测试时间触发 wakeup。
5. 确认 run queue 和 runtime 更新。

## 13. 上线策略

### 13.1 Feature Flag

建议配置：

```toml
[persistent_agent]
enabled = false
runtime_sidecar = true
daemon = false
cron = false
webhook = false
file_watch = false
```

第一阶段可以只打开 runtime sidecar，因为它是低风险的本地持久化能力。

### 13.2 渐进发布

1. Runtime sidecar 默认启用，但只保存状态。
2. `/goal continue` 默认启用。
3. bot session binding 需要用户配置 bot 后启用。
4. daemon 作为实验命令。
5. cron/webhook 默认关闭，需要显式配置。
6. 产品场景模板默认只生成草稿，不自动发布。

### 13.3 回滚策略

- 如果 runtime sidecar 出问题，删除 sidecar 后 transcript 仍可 resume。
- 如果 daemon 出问题，用户仍可回到 CLI/desktop 单进程模式。
- 如果 scheduler 出问题，关闭 `[persistent_agent] cron = false`。
- 如果 webhook 出问题，关闭 receiver，不影响普通 chat。

## 14. 观测与诊断

### 14.1 Runtime Debug

新增诊断命令建议：

```text
reasonix runtime inspect <session>
reasonix runtime repair <session>
reasonix daemon status
reasonix daemon doctor
```

输出应包含：

- active goal。
- run status。
- wait condition。
- next wakeup。
- last wakeup。
- budget。
- sidecar path。

### 14.2 Timeline

timeline 事件：

- session resumed。
- goal restored。
- goal continued。
- goal completed。
- goal blocked。
- wakeup skipped。
- wakeup queued。
- wait condition set。
- wait condition satisfied。
- approval requested。
- approval resolved。

### 14.3 错误分类

- config error。
- runtime sidecar corrupt。
- session missing。
- lock conflict。
- budget exceeded。
- duplicate event。
- unauthorized webhook。
- provider error。
- approval timeout。

## 15. 风险与缓解

### 15.1 Cache 命中下降

风险：

- 动态 runtime 被加入 system prompt。
- tool schema 为 scheduler/webhook 动态变化。
- provider request 注入 per-turn metadata。

缓解：

- runtime sidecar 外置。
- 唤醒只生成普通 user turn。
- PR 中明确 cache review。

### 15.2 重复执行

风险：

- daemon 重启补跑。
- webhook 重放。
- cron window 重复。

缓解：

- event id 去重。
- schedule window 去重。
- session 级 run lock。
- run id 持久化。

### 15.3 误执行高风险操作

风险：

- webhook 触发发布。
- cron 自动 merge。
- bot 被未授权用户触发。

缓解：

- 外部事件只唤醒。
- 写操作继续 approval。
- allowlist 强制。
- bot session isolation。

### 15.4 状态不一致

风险：

- transcript 已保存，runtime 未保存。
- desktop tab profile 和 runtime 冲突。
- daemon 和桌面同时执行。

缓解：

- controller runtime 是权威。
- snapshot 错误必须上报。
- daemon 单实例锁。
- 桌面连接 daemon 后只观察/提交 intent。

### 15.5 产品复杂度过早膨胀

风险：

- 过早做完整 workflow engine。
- 过早做插件生态。
- 过早支持复杂 cron 表达式。

缓解：

- 第一阶段只做 goal restore + explicit continue。
- scheduler 只支持 interval/daily。
- webhook 先只支持 GitHub 常见事件。

## 16. 决策点

需要在动手前确认：

- runtime sidecar 独立文件还是扩展 `.meta`。
- `/goal continue` 是否允许从 blocked 状态恢复。
- daemon local API 使用 unix socket 还是 loopback HTTP。
- cron 配置归属：global、project、session，还是三者都支持。
- webhook 第一版是否只支持 GitHub。
- budget 第一版按模型轮次还是按金额估算。
- desktop 是否在第一版就显示 daemon 状态，还是先 CLI/bot。

## 17. 推荐默认决策

为了降低第一阶段风险，建议采用：

- runtime sidecar 使用独立 `<session>.runtime.json`。
- resume 只恢复，不自动继续。
- `/goal continue` 允许 blocked 状态重新 audit。
- daemon 第一版使用 loopback HTTP，但绑定随机本地 token。
- cron 第一版只支持 session 级 daily/interval。
- webhook 第一版只支持 GitHub workflow / PR / issue 事件。
- budget 第一版按每日模型 turn 数限制。
- desktop 第一版只显示 goal/runtime 状态，审批台放到 daemon 稳定后。

## 18. 第一批开发日程

### Day 1

- 定 runtime sidecar schema。
- 实现 `internal/agent/runtime.go`。
- 写 runtime load/save tests。

### Day 2

- controller `RuntimeSnapshot`。
- snapshot 写 runtime sidecar。
- goal 状态转换测试。

### Day 3

- `Controller.Resume` restore runtime。
- resume tests。
- desktop meta 对齐 controller runtime。

### Day 4

- `/goal continue`。
- blocked audit 恢复。
- docs 更新。

### Day 5

- bot `/status` 显示 runtime。
- bot `/goal continue`。
- focused bot tests。

Day 5 后可形成第一个可演示版本：

> 在桌面创建 goal，关闭重启，恢复 session，IM 或 CLI 查看 status，再显式 continue。

## 19. Definition of Done

每个 PR 完成前必须满足：

- 只包含当前 PR 范围。
- 有 focused tests。
- `git diff --check` 通过。
- 文档或示例配置同步更新。
- cache risk 已检查。
- public text 不包含本机路径、密钥、私人账号或内部截图内容。
- 如果推送分支，确认远端 head SHA。

## 20. 最小演示脚本

MVP demo 建议：

```text
1. Start Reasonix desktop.
2. Create a goal: "Prepare triage drafts for open PRs."
3. Let the model end with [goal:continue].
4. Close the app.
5. Reopen and resume the session.
6. Confirm active goal is visible.
7. Run /goal continue.
8. Confirm the goal continues from restored state.
9. Open bot /status and confirm the same goal state.
```

这个 demo 覆盖最核心价值：长期目标不是聊天窗口里的临时状态，而是 Reasonix
可以恢复、可以观察、可以继续推进的个人工作流。

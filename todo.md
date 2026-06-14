# Reasonix 常驻个人 AgentOS 路线 TODO

本文档把内部讨论中的方向拆成可执行任务。核心目标不是先做复杂的
dynamic workflow 或开发者平台，而是先把 Reasonix 从一次性会话工具推进为
低成本、可恢复、可被时间和外部事件唤醒的个人常驻 agent。

## 总原则

- 优先保护 prompt cache 命中率。动态状态必须写入 session sidecar 或普通
  user turn，不进入 system prompt、稳定 tool schema 或稳定 prefix。
- 优先复用现有控制器、session、goal、bot gateway 和 TurnDone snapshot 机制。
- 先做个人常驻 OS，不先做插件市场、企业平台或完整 SDK 生态。
- 每一步都保持增量可回滚：先能恢复，再能唤醒，再能调度。
- 常驻 agent 默认只在需要模型决策时调用模型；cron 检查、等待 CI、文件监听
  等确定性步骤应尽量不产生模型费用。

## 当前代码基础

- `internal/control/controller.go` 已有 goal 模式、`continueGoal` 自动续跑和
  `[goal:continue]` / `[goal:complete]` / `[goal:blocked]` 状态标记。
- `internal/control/controller.go` 的 `Resume` 会恢复 transcript、checkpoint 和
  runtime sidecar 中的 goal/run 状态，但不会自动触发续跑。
- `desktop/tabs.go` 已有 `TurnDone -> scheduleTabSnapshot` 的合并保存机制。
- `internal/agent/runtime.go` 已有 session `.runtime.json` sidecar，用于保存
  goal/run/wait/scheduler/file-watch/budget 动态状态。
- `internal/bot/gateway.go` 已有 IM 入口、session key、排队/合并、审批和 controller
  生命周期雏形。
- `cmd/reasonix bot start` 已经能启动 bot gateway，但还不是通用 daemon 调度器。

## Phase 1: 持久化 Goal / Run 状态

目标：关闭客户端或重启后，Reasonix 能知道一个 session 是否仍有活跃 goal，以及
上次自动推进到了哪里。

### 任务

- [x] 设计 session runtime sidecar schema。
  - [x] 路径：`<session>.runtime.json`。
  - [x] 字段覆盖 `version`、`session_id`、`goal.*`、`run.*`、
    `scheduler.*`、`wait.*`、`file_watch.*`、`budget.*`。
- [x] 在 controller 中提供只读 snapshot 方法。
  - [x] `RuntimeSnapshot()` 不暴露可变内部字段。
- [x] 在 controller 中提供恢复方法。
  - [x] `RestoreRuntimeSnapshot(snapshot)` 恢复 goal/run 账本。
  - [x] 恢复时处理空 goal、已完成 goal、blocked goal 和 running->interrupted。
- [x] 将 sidecar 写入接入现有 snapshot 节奏。
  - [x] CLI / serve / desktop 复用 controller 层写入逻辑。
  - [x] 桌面侧继续沿用 `TurnDone -> scheduleTabSnapshot` 的合并写入节奏。
- [x] 确保写入是原子的。
  - [x] 复用 `fileutil.ReplaceFile` 模式。
  - [x] 避免崩溃时留下半截 JSON。
- [x] 增加 focused tests。
  - [x] 设置 goal 后 snapshot 会写 sidecar。
  - [x] goal complete 后 sidecar 状态正确。
  - [x] repeated blocked 计数能保存。
  - [x] 损坏 sidecar 不影响 transcript resume。
  - [x] 空 session 不创建多余 runtime sidecar。

### 验收标准

- 重启或 resume 后，`Goal()` / `GoalStatus()` 能还原。
- sidecar 写入不会改变 `.jsonl` transcript 格式。
- 不修改 system prompt、tool schema、provider request 稳定前缀。

## Phase 2: Resume 后恢复自治上下文

目标：用户恢复旧 session 时，不只看到历史消息，还能恢复上次的目标和自治轮次。

### 任务

- [x] 在 `Controller.Resume` 中读取 runtime sidecar。
- [x] 恢复 active goal，但不要默认立刻自动执行。
  - [x] 防止用户只是打开历史记录时意外跑任务。
  - [x] 先提供显式入口，如 `/goal continue` 或 UI 按钮。
- [x] 增加 `resume` 通知。
  - [x] 例如：`resumed active goal: <short goal>`。
  - [x] blocked / complete 状态应显示不同提示。
- [x] 明确 warm resume 与 cold resume 行为。
  - [x] 保持现有 cold resume prune 逻辑。
  - [x] sidecar 恢复不得触发 prompt 重写。
- [x] 更新桌面 meta。
  - [x] `Meta.Goal` / `Meta.GoalStatus` 来自 controller runtime 状态。
  - [x] 避免只依赖 desktop tab profile。
- [x] 增加 CLI resume 测试。
  - [x] `/resume <n>` 恢复 goal 状态并显示 runtime goal notice。
  - [x] resume 后普通 user turn 会继续注入 active goal block。
  - [x] resume 后未触发显式继续时不自动跑 `continueGoal`。

### 验收标准

- 手动恢复 session 后，用户能看到活跃 goal。
- 用户发出继续指令后，能复用已有 `continueGoal`。
- 打开历史、预览历史、切换 tab 不会误触发模型调用。

## Phase 3: 显式 Goal 续跑入口

目标：在恢复或唤醒后，用统一入口继续推进 active goal。

### 任务

- [x] 增加 `/goal continue` 命令。
  - 若无 active goal，返回清晰提示。
  - 若 goal 已 complete，提示无需继续。
  - 若 goal blocked，默认开启新的 blocked audit。
- [x] 增加 controller API。
  - 例如 `ContinueGoal(ctx, reason string)`。
  - reason 用于区分 user、cron、webhook、file_watch。
- [x] 将自动续跑限制持久化。
  - [x] `goalTurns` 恢复后不能无限重跑。
  - [x] 通过 runtime `budget.max_goal_auto_turns` 明确每个 goal 最多自动推进多少轮。
  - [x] daemon `budget` API / CLI 可配置，bot `/status` 和 doctor 可见。
- [x] 增加通知和事件。
  - [x] goal continuation started。
  - [x] goal continuation complete。
  - [x] goal blocked。
  - [x] goal continuation limit reached。
- [x] 增加测试。
  - [x] `/goal continue` 恢复 blocked audit。
  - [x] context cancel 会把 goal 标记为 stopped。
  - [x] reached max auto turns 后不会继续自动唤醒。

### 验收标准

- 所有入口最终都走同一个 goal continuation API。
- 没有新增 prompt prefix 风险。
- 失败状态可解释、可恢复。

## Phase 4: Bot Gateway 消费 Runtime Session

目标：让 IM bot 不只是临时消息入口，而能连接到已存在的 Reasonix session。

### 任务

- [x] 设计 bot session mapping。
  - [x] remote chat/thread/user -> Reasonix session path。
  - [x] 支持 global 和 project scope。
  - [x] 复用 `bot.connections.session_mappings` 概念。
- [x] bot gateway 创建 controller 时优先恢复已有 session。
  - [x] 若 mapping 存在，加载对应 `.jsonl` 和 runtime sidecar。
  - [x] 若不存在，创建新 session 并写 mapping。
- [x] bot 命令增强。
  - [x] `/status` 显示 active goal、run status、wait、last wakeup 和预算。
  - [x] `/goal continue` 从 IM 触发继续。
  - [x] `/sessions` 列出可恢复 session。
  - [x] `/attach <session>` 将当前 IM 会话绑定到已有 session。
  - [x] `/timeline [n]` 从 IM 查看当前 session 最近 runtime 事件。
  - [x] `/wakeups [n]` 从 IM 查看当前 session 最近唤醒历史。
- [x] 将 approval / ask 状态纳入 runtime。
  - [x] 重启后至少能提示有未完成审批，而不是静默丢失。
  - [x] 第一版可以要求用户重新触发，不必恢复阻塞中的 channel。
- [x] 增加 bot gateway tests。
  - [x] 同一 remote session 重启后恢复 goal。
  - [x] 群聊按 user 隔离的 session key 不串线。
  - [x] allowlist 不允许的用户无法触发已绑定 session。
  - [x] `/status` 能从 runtime sidecar 显示 wait、wakeup 和预算。
  - [x] `/status` 能在只有 session mapping、没有活跃 controller 时显示状态。
  - [x] `/timeline` 能从 session mapping 显示最近 runtime timeline。
  - [x] `/wakeups` 能从 session mapping 显示最近唤醒历史。
  - [x] bot 记录并清除 approval / ask wait runtime 状态。

### 验收标准

- 用户可以在 IM 中继续桌面或 CLI 里创建的 goal。
- bot 重启后不会丢失 session 映射。
- bot 不会误把不同用户/群/线程合并到同一长期任务。

## Phase 5: Daemon 常驻进程

目标：让 run 生命周期脱离桌面客户端，客户端只负责观察、审批和下命令。

### 任务

- [x] 新增 `reasonix daemon start` 命令。
  - [x] 管理 session registry。
  - [x] 持有 scheduler。
  - [x] 可选择启动 bot gateway。
- [x] 新增本地控制接口。
  - [x] 使用 localhost HTTP。
  - [x] 提供 status、sessions、approvals、continue goal、stop run、approve、answer、wait-event、wait-time、wait-file。
- [x] 单实例控制。
  - [x] 同一 config/session dir 只允许一个 daemon 写 runtime state。
  - [x] 启动时检测锁文件。
- [x] 进程崩溃恢复。
  - [x] 启动时扫描 runtime sidecar。
  - [x] 将 running 但无 owner 的 run 标记为 interrupted。
  - [x] 不自动继续，除非 scheduler 明确允许。
- [x] 桌面端连接 daemon。
  - [x] 桌面 bridge 可查询 daemon 状态。
  - [x] 能打开 daemon 管理的 session。
  - [x] 能发送审批、ask 回答和 stop。
  - [x] 增加可见的桌面 daemon 管理 UI / 审批入口。
- [x] 日志与诊断。
  - [x] daemon log 文件。
  - [x] `reasonix daemon doctor`。
  - [x] 运行中 session 和 wakeup 历史。

### 验收标准

- 关闭桌面端后，daemon 仍可响应 bot/cron。
- daemon 重启后不会把 running 任务误判为成功。
- 多客户端观察同一 session 不造成重复执行。

## Phase 6: Cron / 时间唤醒

目标：实现每天、每小时或指定时间自动唤醒某个 goal。

### 任务

- [x] 设计 schedule 配置。
  - [x] session 级 schedule。
  - [x] project 级 schedule。
  - [x] global schedule。
  - [x] 支持 session/project/global 级 daily schedule timezone。
- [x] 实现轻量 scheduler。
  - [x] 第一版只支持 fixed interval 和 daily time。
  - [x] 持久化 `next_wakeup_at` / `last_wakeup_at`。
- [x] 定义 wakeup payload。
  - [x] reason: `cron`
  - [x] schedule id
  - [x] previous run status
  - [x] bounded event summary
- [x] 唤醒时先做确定性检查。
  - [x] session 是否仍 active。
  - [x] goal 是否 complete / blocked。
  - [x] 是否已有 run 正在执行。
  - [x] 是否超过每日预算。
- [x] 增加去重。
  - [x] 同一 schedule 在同一窗口只触发一次。
  - [x] daemon 重启后不会补跑重复任务。
- [x] 增加测试。
  - [x] daily schedule 计算 next run。
  - [x] timezone 正确。
  - [x] missed wakeup 不重复风暴。
  - [x] running session 不并发触发。

### 验收标准

- 可以配置“每天早上自动检查昨天没干完的 PR / issue”。
- scheduler 行为可预测、可解释。
- 不依赖模型来决定是否到了唤醒时间。

## Phase 7: Webhook / 外部事件唤醒

目标：把唤醒从时间扩展到 GitHub、CI、IM、HTTP webhook 等外部事件。

### 任务

- [x] 设计通用 event envelope。
  - [x] source
  - [x] event_id
  - [x] received_at
  - [x] project/session routing key
  - [x] payload summary
  - [x] raw payload storage reference
- [x] 新增 webhook receiver。
  - [x] 第一版只支持 localhost / 用户自托管。
  - [x] 必须有 secret 校验。
  - [x] payload 大小限制。
- [x] GitHub 事件路由。
  - [x] issue opened / assigned。
  - [x] pull_request opened / review_requested / checks completed。
  - [x] workflow_run completed。
  - [x] 更多 provider-specific 事件归一化。
    - [x] GitHub check_suite completed -> status/conclusion/PR number/ref。
    - [x] GitHub status / push / release 等事件归一化。
- [x] CI 等待场景。
  - [x] daemon API/CLI 可写 runtime event wait condition。
  - [x] webhook 匹配 event wait condition 后触发下一步。
  - [x] CI 绿灯语义过滤（status/conclusion）。
  - [x] CI 失败分支总结。
- [x] 幂等处理。
  - [x] event id 去重。
  - [x] 同一 PR 的相同状态变化不重复跑。
- [x] 增加安全边界。
  - [x] webhook 不能直接执行写操作。
  - [x] 外部事件只生成 bounded turn input。
  - [x] 高风险操作继续走 approval。

### 验收标准

- 新 issue / PR / CI 状态变化能唤醒对应 session。
- 重复 webhook 不会重复提交、重复发布或重复评论。
- 未授权 webhook 被拒绝并有审计日志。

## Phase 8: 文件监听唤醒

目标：对本地工作区文件变化作出反应，例如文档更新、测试结果落盘、构建产物生成。

### 任务

- [x] 选择文件监听实现。
  - [x] 第一版使用 portable polling 实现。
  - [x] 与 codegraph watcher 分清职责。
  - [x] 评估 Go 原生跨平台事件库替代 polling。
- [x] 配置 watched paths。
  - [x] 支持 watched paths。
  - [x] 支持 exclude / ignore patterns。
  - [x] 默认不监听大目录、依赖目录、secret 文件。
- [x] 防抖和批量。
  - [x] 短时间内多次变化合并成一个 event。
  - [x] 记录 changed files summary。
- [x] 事件转 turn input。
  - [x] 不把大文件内容直接塞进 prompt。
  - [x] 只提供路径、变更类型、摘要。
- [x] 测试。
  - [x] explicit file wait 匹配后一次性唤醒并清除 wait。
  - [x] explicit file wait 不匹配时不唤醒。
  - [x] event/time 等其他 wait 不会被文件变化误唤醒。
  - [x] rapid changes 合并。
  - [x] ignored path 不触发。
  - [x] running session 不并发触发。

### 验收标准

- 文件变化能唤醒指定 goal。
- 大型仓库不会因为监听导致 CPU 或 prompt 爆炸。

## Phase 9: 确定性调度层

目标：把“自动继续”升级成可观察、可暂停、可审批的任务状态机。

### 任务

- [x] 定义 task/run 状态机。
  - [x] idle
  - [x] queued
  - [x] running
  - [x] waiting_approval
  - [x] waiting_event
  - [x] waiting_time
  - [x] blocked
  - [x] complete
  - [x] failed
  - [x] stopped
- [x] 引入 run queue。
  - [x] 同一 session 串行。
  - [x] 不同 session 可限制并发。
  - [x] 支持优先级。
- [x] 引入 wait condition。
  - [x] wait for user approval
  - [x] wait for webhook / external event
  - [x] wait for CI status/conclusion
  - [x] wait until time
  - [x] wait for file change
- [x] 成本预算。
  - [x] session 级每日自动唤醒次数预算。
  - [x] 每日模型调用次数。
  - [x] 每日费用预算。
  - [x] 每个 goal 最大自动轮次。
- [x] 审批台（daemon 数据面 + bot/desktop bridge）。
  - [x] daemon `GET /approvals` 能列出 active approval / ask 和重启后保留在 runtime sidecar 的等待项。
  - [x] bot `/approvals` 能看到当前绑定 session 的等待审批或提问，并提示 `/approve`、`/deny`、`/answer` 命令。
  - [x] 桌面 bridge 暴露 `ListDaemonApprovals`，供后续可见 UI 面板读取。
  - [x] 审批后继续同一 run。
- [x] 可观测性。
  - [x] run timeline。
  - [x] wakeup history。
  - [x] last model decision。
  - [x] deterministic steps log。

### 验收标准

- 常驻 agent 可以“等 CI 绿了再继续发布”。
- 用户可以随时暂停、恢复、终止某个长期 goal。
- 一天内成本可控，并且能解释钱花在哪里。

## Phase 10: 产品化个人 AgentOS 场景

目标：把底座包装成用户能理解和复用的个人工作流。

### 推荐首批场景

- [x] 每天自动 PR / issue triage。
  - [x] `reasonix daemon daily-triage` 可为已有 triage session 配置每日唤醒。
  - [x] 默认设置每日自动唤醒预算，避免重复 triage 风暴。
  - [x] 拉取未处理 PR / issue、起草 triage 和审批由 active goal 与 approval 机制承接。
- [x] CI watcher。
  - [x] `reasonix daemon ci-watch` 可把 session 配置为等待 GitHub CI 成功。
  - [x] 支持 workflow_run / check_suite / commit status。
  - [x] CI 失败时复用 webhook failure diagnosis 唤醒总结错误。
  - [x] CI 成功后唤醒原 session，准备发布说明或 merge 建议。
- [x] 发布助手。
  - [x] `reasonix daemon release-assist` 可等待 changelog / version 文件变化。
  - [x] 文件变化后唤醒 release session 检查 changelog 和版本号。
  - [x] 发布动作继续走 tool approval / approval desk。
  - [x] 发布后通知 IM 由 active release goal 复用 bot gateway 承接。
- [x] 仓库健康巡检。
  - [x] `reasonix daemon repo-health` 可为已有巡检 session 配置每日唤醒。
  - [x] 默认只配置 deterministic schedule / budget，不直接执行高风险写操作。
  - [x] 轻量检查、flaky tests、长期未合并 PR、过期依赖报告由 active health goal 承接。
  - [x] 后续修复、升级依赖、关闭 PR 等动作继续走 tool approval / approval desk。
- [x] 个人任务复盘。
  - [x] bot `/recap [YYYY-MM-DD]` 可总结当天自动处理了什么。
  - [x] 复盘包含唤醒、运行完成、等待用户、预算阻断、模型调用 / token / cost 汇总。
  - [x] 复盘列出当前需要用户决策的 approval / ask 和下一步命令。

### 验收标准

- 用户能从 UI 或 bot 看到“今天 agent 帮我做了什么”。
- 用户能快速批准或拒绝下一步。
- 默认不执行高风险写操作。

## 风险清单

- [x] Cache 风险：把动态状态写进稳定 prompt prefix。
  - [x] 缓解：goal/run/wait/scheduler/file-watch/budget 动态状态写入
    `.runtime.json` sidecar，不进入 system prompt、tool schema 或稳定 prefix。
  - [x] 缓解：cron / webhook / file wait 唤醒时只把 bounded wakeup context
    注入普通 goal continuation user turn。
  - [x] 缓解：webhook 原始 payload 只保存为 payload ref，模型上下文只包含
    受限 summary，避免外部事件正文污染稳定 prefix。
  - [x] 缓解：cache prefix / hit-rate / compaction guard tests 守住 provider-visible
    请求前缀稳定性。
- [x] 重复执行风险：daemon 重启、webhook 重放、cron 补跑导致重复提交。
  - [x] 缓解：daemon startup recovery 只标记 interrupted，不补跑或排队 intent。
  - [x] 缓解：cron 使用 schedule window 的 event id / wakeup key 去重，同一窗口不重复唤醒。
  - [x] 缓解：webhook 使用 delivery event id 和语义 wakeup key 去重，重放不 enqueue intent、不重复消耗预算。
  - [x] 缓解：budget-blocked wakeup 也记录 wakeup key，避免同一窗口阻断后反复刷屏。
- [x] 权限风险：外部事件触发写操作。
  - [x] 缓解：webhook receiver 必须 HMAC secret 校验，未授权事件被拒绝。
  - [x] 缓解：外部事件只入队 bounded `RunIntent`，模型上下文只包含受限 summary / payload ref，不注入原始 payload。
  - [x] 缓解：daemon worker 恢复 controller 后启用 interactive approval gate，高风险工具调用会进入 approval wait。
  - [x] 缓解：daemon approval desk / bot `/approvals` 提供显式 approve / deny / answer 入口。
- [x] 状态漂移风险：transcript、runtime sidecar、desktop tab profile 不一致。
  - [x] 缓解：runtime sidecar 独立于 `.meta`，只承载 goal/run/wait/scheduler/file-watch/budget 动态状态。
  - [x] 缓解：`Controller.Resume` 从 runtime sidecar 恢复 goal/run，desktop profile 不作为执行状态来源。
  - [x] 缓解：controller snapshot 合并并保留 scheduler / file-watch / budget / wait，避免普通 turn 覆盖 daemon 状态。
  - [x] 缓解：bot `/status`、daemon `/sessions`、`/approvals` 都读取 runtime sidecar 作为用户可见状态。
- [x] 成本风险：常驻 agent 频繁唤醒模型。
  - [x] 缓解：确定性预检查、每日自动唤醒预算。
  - [x] 缓解：最大自动轮次。
  - [x] 缓解：每日模型调用 / 费用预算。
- [x] 用户体验风险：恢复历史时自动开跑。
  - [x] 缓解：`Controller.Resume` 只恢复 runtime 状态和 notice，不触发模型调用。
  - [x] 缓解：daemon startup recovery 只把 in-flight run 标记为 interrupted 并记录 timeline，不排队 intent。
  - [x] 缓解：继续执行只能来自显式 `/goal continue` / daemon `continue`，或已配置的 scheduler / webhook / file wait。
- [x] 平台化过早风险：插件生态、SDK、企业平台会拖慢个人 OS MVP。
  - [x] 缓解：当前路线只承诺个人常驻 OS、daemon、bot、cron、webhook、file watch、
    approval desk 和首批个人场景。
  - [x] 缓解：暂不做插件市场、企业多租户控制台、第三方 SDK 生态和工作流编排平台。
  - [x] 缓解：未来平台化必须从已验证的个人场景抽象，不能反向拖慢 MVP。

## 建议实现顺序

1. Runtime sidecar schema 和读写测试。
2. `Controller.Resume` 恢复 goal 状态。
3. `/goal continue` 显式续跑。
4. bot gateway 恢复已有 session。
5. daemon skeleton 和本地 status API。
6. cron wakeup。
7. webhook wakeup。
8. file watcher wakeup。
9. run queue 和 wait condition。
10. PR / issue / CI 场景产品化。

## 第一周可交付 MVP

- [x] 新增 runtime sidecar。
- [x] 恢复 active goal。
- [x] 提供 `/goal continue`。
- [x] 桌面 meta 能显示恢复后的 active goal。
- [x] bot `/status` 显示 goal 状态。
- [x] 增加核心单元测试。
- [x] 确认 provider request 稳定前缀无变化。

## 已定产品决策

- [x] Runtime sidecar 使用独立 `.runtime.json` 文件，不扩展 `.meta`。
  - 理由：`.meta` 继续承载结构信息，runtime sidecar 只承载可恢复执行状态。
- [x] daemon 与桌面通信第一版使用 localhost HTTP。
  - 理由：现有 daemon API、CLI、desktop bridge 和 auth token 已围绕 localhost HTTP 闭环。
  - 后续只有在权限、沙箱或平台兼容性需要时再评估 unix socket / serve transport。
- [x] cron 配置最终落到 session runtime sidecar。
  - 理由：session/project/global scope 是配置选择器，实际写入目标 session 的
    scheduler runtime，避免额外引入全局调度数据库。
- [x] webhook 第一版采用通用 event envelope，并优先内置 GitHub / CI adapter。
  - 理由：个人 AgentOS 首批高频场景是 PR、issue、CI、release；其他 provider 先通过
    通用 localhost webhook 接入。
- [x] 常驻 agent 默认不自动继续 interrupted goal。
  - 理由：crash / kill 后只标记 interrupted 和 timeline，继续必须来自用户显式命令
    或已配置的 cron / webhook / file wait。
- [x] 预算策略第一版按 session runtime 计费，project/global 只作为批量配置入口。
  - 理由：执行、wait、wakeup、timeline 都以 session 为最小可恢复单元；跨 session
    聚合预算留到个人场景稳定后再抽象。

## Post-MVP 优化池

这些任务不阻塞个人常驻 AgentOS MVP，但会决定它能否从“能用”变成“长期好用”。

### P0：常驻进程产品化

- [x] daemon 进程管理 UX。
  - [x] 桌面端能启动、停止、重启 daemon。
  - [x] 桌面端能显示 daemon PID、uptime、监听地址、session 数和最近错误。
  - [x] 提供 launchd / systemd / Windows startup helper 的安装与卸载命令。
  - 验收：用户不需要开终端也能知道常驻 agent 是否活着，以及为什么没被唤醒。
- [x] daemon 日志与凭据运维。
  - [x] daemon log 支持轮转和大小上限。
  - [x] daemon auth token 支持手动 rotate。
  - [x] doctor 能报告 token 缺失、权限异常、端口占用、stale lock 和日志不可写。
  - 验收：长期运行不会因为日志膨胀、token 漂移或 lock 残留变成黑盒。

### P0：桌面常驻控制台

- [x] 常驻任务总览。
  - [x] 列出所有 daemon managed sessions、goal、run、wait、budget 和 next wakeup。
  - [x] 支持按 project / global / waiting / running / blocked 过滤。
  - [x] daemon API / CLI / desktop bridge 支持 stop、continue、open session、disable schedule、disable watch。
  - [x] 桌面常驻任务总览页面提供上述操作入口。
  - 验收：用户能在一个页面看懂“现在 agent 正在等什么、接下来会做什么”。
- [x] 审批台体验升级。
  - [x] approval / ask 列表支持跨 session 排队处理。
  - [x] 明确 dormant wait 和 active wait 的差异。
  - [x] 高风险动作展示来源事件、目标 session、命令或工具参数摘要。
  - 验收：用户可以在 30 秒内批完当天积压的低风险任务，并识别高风险任务。

### P1：真实场景 E2E

- [x] 增加 daily triage 端到端测试脚本。
  - [x] 启动 daemon，配置 daily-triage，模拟时间唤醒，验证只入队一次。
  - [x] 验证预算耗尽时不调用模型，并写入 timeline。
- [x] 增加 CI watcher 端到端测试脚本。
  - [x] 配置 wait-event，模拟 GitHub workflow_run/check_suite/status。
  - [x] 验证失败走 diagnosis，成功继续原 goal，重复 webhook 不重复执行。
- [x] 增加 release assistant 端到端测试脚本。
  - [x] 配置 wait-file，写入 changelog/version 文件，验证 debounce 和一次性唤醒。
  - [x] 验证发布类写操作仍进入 approval desk。
  - 验收：三条首批场景都能在本地无真实外网依赖下稳定复现。

### P1：事件与监听能力升级

- [x] webhook adapter registry。
  - [x] 保留通用 envelope。
  - [x] GitHub adapter 作为默认内置 adapter。
  - [x] 新 provider 通过 adapter 归一化 event id、routing key、summary 和 failure semantics。
  - 验收：新增 provider 不需要改 daemon 核心 worker / scheduler。
- [x] 文件监听从 polling 升级为 hybrid watcher。
  - [x] 优先使用原生文件事件库。
  - [x] polling 作为跨平台 fallback。
  - [x] 大仓库下暴露 watcher 延迟、扫描目录数和 ignored change 计数。
  - 验收：大型仓库不会因为常驻监听造成明显 CPU 抖动。

### P2：跨 Session 策略

- [x] project / global 聚合预算。
  - [x] 保持 session runtime 预算为最小账本。
  - [x] 增加 project / global aggregate quota 视图。
  - [x] 支持“本项目今天最多 N 次自动模型调用”。
  - 验收：多个常驻 session 不会绕过用户设置的项目级成本上限。
- [x] 个人 AgentOS 场景模板库。
  - [x] 把 daily triage、CI watcher、release assistant、repo health 抽成可复制模板。
  - [x] 模板只生成 session runtime 配置和 goal starter，不引入插件市场。
  - 验收：新用户可以用一个命令创建可恢复、可审批、可复盘的常驻任务。

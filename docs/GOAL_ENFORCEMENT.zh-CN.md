# Goal 模式 — 连续执行、结构化完成协议与 Delivery 职责拆分

Reasonix 的 Goal 模式（`/goal`）将目标推进（Goal）、验收（Delivery）和权限（Ask/Auto/Yolo、Sandbox）三者保持正交：Goal 是唯一的跨 turn 调度器，Delivery 是纯质量门禁，工具权限与沙箱不受 Goal 开关影响。

## 功能一览

| 功能 | 触发方式 | 效果 |
|------|----------|------|
| 结构化完成协议 | `update_goal` 工具 | 每轮目标 turn 结束时模型通过工具报告 continue/complete/blocked（含 reason 与 next_action），取代旧的 `[goal:*]` footer 文本标记 |
| 完成校验 | 默认 | `complete` 必须通过 todos 与必做工作（mutation、capability）；Light/Balanced 下已声明 `unverified` 的检查缺口不阻塞；同一检查缺口被连续 `complete` 两次后直接完成，未完成的 todos 仍继续推进 |
| 完成自述与对账 | `update_goal` 的 `completion` | `complete` 可附带自述：`verified` 命令逐条与本会话真实 receipt 对账，没跑过 / 跑失败 / 早于最后一次改动都记为 unbacked claim；`unverified` 与 `risks` 是宿主推断不出的声明，只增不减，永远不阻塞完成 |
| 独立评审 | 无报告时 | 模型未调用 `update_goal` 时，宿主调用一次独立 bounded evaluator 判定；评审不可用/出错/不确定时安全暂停，绝不默认继续 |
| 连续执行 | 默认 | 不设默认 model rounds、Goal turns、墙钟时长或数字式卡死上限；相同宿主失败、零新增证据和 Todo 停滞只触发重新规划，不暂停 Goal |
| 显式预算 | `[agent].goal_token_budget` / `--max-steps` / 正数时间或成本预算 | 用户可选的边界耗尽后执行一次无工具总结并暂停当前执行；Goal 与进度保留，可继续。token 预算默认 `0`（关闭） |
| 暂停/恢复 | `/goal pause` / `/goal resume` | 暂停保留 Goal、todo、Delivery checkpoint 与累计运行历史；恢复 `budget_spend` 时授予新的显式预算切片，但不清零累计统计 |
| 立即阻塞 | `blocked` 报告 | 单个 blocked 报告立即结束目标，不再重复三轮确认 |
| 并行调度 | `parallel_tasks` 工具 | 并发派发多个子 agent，各自独立显示结果 |

## 使用方式

### 默认模式

```bash
/goal 实现一个 CLI 计算器
```

模型在每个目标 turn 结束时调用 `update_goal`：

- `continue`（附 `reason` 与可选 `next_action`）— 继续推进；
- `complete`（仅在请求完成、输出格式与约束满足、验证已尝试或声明不可用时）— 宿主会用 Delivery readiness 校验该声明；
- `blocked`（仅当下一步需要用户独有信息、不可逆或对外可见操作、或范围变化时）— 立即停止。

`complete` 还可以附一份自述 `completion`：

```json
{"status":"complete","completion":{
  "verified":["go test ./..."],
  "unverified":["desktop UI 未实际操作验证"],
  "risks":["迁移不可逆"]
}}
```

宿主对这份自述做两件事。`verified` 里的每条命令都会去本会话的真实 receipt 里找：没跑过、跑失败、或者最后一次运行早于最后一次改动，都会被记成一条 unbacked claim。`unverified` 与 `risks` 宿主无从推断，因此原样保留，也**只增不减**——一份自述永远不能抹掉宿主自己发现的缺口。在 Light/Balanced（以及用户禁止测试）时，诚实申报的检查缺口不再阻塞 `complete`；同一检查缺口连续两次 `complete` 后也会结束 Goal，而不是把模型打回验证循环。未完成的 todos 仍继续推进。

`update_goal` 只在活动 Goal turn 中可用；普通聊天调用会收到结构化错误且不改变任何状态。同值重复调用幂等，`continue` 可升级为 `complete`/`blocked`，终态后冲突调用被拒绝；目标被替换或清除后，迟到的报告/用量一律按 scope+epoch 拒绝。

### 统计、显式预算与暂停

旧的 simple/write/research 类别和 `/goal --simple`、`--research` 参数只为 sidecar/CLI 兼容保留，
不再改变执行额度。executor、planner、subagent、compaction、router、reviewer、evaluator 等计费用量
累计到 `tokensUsed`，真实 HTTP 请求（含重试）累计到 `requestsUsed`，Goal Run 的实际工作时间累计到
`workDurationMs`。默认情况下这些字段与 `turnsUsed` 都只做统计：

- 未配置 `goal_token_budget` 时 `tokensLimit` 为 `0`；配置正数后只表示用户选择的累计 token 阈值；
- 没有 provider 请求前的 token 预留/准入；
- 未配置对应预算时，累计 turn/token/request/work time 再大也不会单独暂停 Goal；
- `turnsLimit`、`noProgressLimit`、`budgetExtensions` 继续对外保留为 deprecated 兼容字段，固定返回 `0`。

可停止连续执行的条件：完成、模型通过 `update_goal(blocked)` 报告真实用户/外部阻塞、evaluator
故障或不确定、用户主动 pause/stop/clear、Provider/权限/宿主不可恢复错误，以及用户显式设置的
正数 token/步数/时间/成本预算。`task_time_budget_minutes = 0`（以及兼容读取的负数）关闭时间边界，
只有正数才启用。结构化卡死检测、Todo stall 与 `noProgressTurns` 只注入策略纠偏，不改变 Goal 状态。
**轮数不再是任何停止条件。** `/goal status` 在未设置 token 预算时显示纯统计：

```
runtime: turns 57 · requests 143 · tokens 2800000 · work time 42m
```

配置 `goal_token_budget` 时 token 统计会显示当前显式阈值；`/goal resume` 从 `budget_spend` 暂停
恢复时授予一个新的完整预算切片，但 turns、tokens、requests 与 work time 继续累计。旧版本因 `budget_turns`、`budget_tokens`、
`goal_run_budget`、`goal_stuck` 或 `no_progress` 暂停的 sidecar 在加载时自动改为 `running` 并原子持久化，
但加载本身不会发送模型请求。活动 Goal 在磁盘写 `turnsLimit: -1` 作为旧 reader 的无限制哨兵；
新 API 将其解释为 `0`。新的 `budget_spend`（用户显式预算）不会被自动迁移；manual pause、evaluator
failure、legacy archive block 和真实 blocker 同样不自动解锁。

上下文压缩继续使用全局既有策略：仅由 `compact_ratio`（默认 80%）触发 Harness 风格的 prune/摘要维护，不另设 soft/snip/force 多阈值。Goal 开启本身不额外触发 summarizer，也不改变工具 Schema 或稳定 prompt 前缀。

### 任务合约

复杂目标可以直接写成 Context / Request / Output format / Constraints /
Pause policy。Goal 模式会把这些段落当作执行边界：满足请求、输出格式、约束和必要验证后才结束；
除非下一步涉及不可逆或对外可见操作、范围变化，或必须由用户提供信息，否则继续采用合理默认值推进。

### 并行子任务

```bash
/goal 研究 Go 的三个标准库并写示例
```

Agent 可以调用 `parallel_tasks` 工具同时派发多个独立子任务：

```
parallel_tasks(tasks=[
  {prompt: "研究 encoding/json，写示例", description: "json research"},
  {prompt: "研究 net/http，写示例", description: "http research"},
  {prompt: "研究 sync，写示例", description: "sync research"},
])
```

每个子任务在独立 goroutine 中运行，工具调用会嵌套显示为独立卡片，结果聚合返回。

### 任务依赖

如果子任务之间有依赖关系，可以用 `depends_on` 指定：

```
parallel_tasks(tasks=[
  {prompt: "写一个加法函数到 add.py", description: "add"},
  {prompt: "写一个乘法函数到 mul.py", description: "mul"},
  {prompt: "在 main.py 中调用 add 和 mul", description: "main", depends_on: [0, 1]},
])
```

独立任务（add、mul）先并发执行；main 等前两个完成后再启动。

## Prometheus 规划面试

在写代码前，先让 AI 帮你理清需求：

```
/prometheus 重构用户认证模块，改成 JWT
```

Prometheus 会逐个问澄清问题：

```
1. 用户模块当前是 session 还是 token 认证？
2. 需要支持 refresh token 吗？
3. 现有用户表结构是什么样的？
```

回答完问题后，Prometheus 自动生成可执行的计划。然后你可以用 `/plan-exec` 来执行。

## 实现细节

### 每轮决策顺序

1. 运行工作模型；
2. 获取结构化 Delivery readiness；
3. 读取本轮的 `update_goal` 报告；
4. 没有报告时调用一次独立 evaluator（readiness 已明确缺失项时直接继续，不调用）；
5. 应用 readiness 与 evaluator fail-closed 结果；
6. 由 Goal FSM 独占决定 complete、continue、blocked 或 pause。

`complete` 在 readiness 通过、或仅剩检查缺口且模型已声明 `unverified`（Light/Balanced）或连续两次同一检查缺口时被接受；`blocked` 立即停止；evaluator 超时、报错、JSON 非法或返回 `uncertain` 一律安全暂停。未完成的 todos 仍继续推进。

### 证据审计门控（Delivery）

Delivery 收敛为纯 readiness 服务，宿主可消费的结构化结果为
`ReadinessResult{Ready, Missing, Reason, ProgressKey}`：

- Canonical todos（Goal、已批准 Plan 或 strict obligation 等闭环回合可跨轮回退；Standard 只读取当前任务证据 ledger 中成功 `todo_write` 的最新列表，不用历史列表判定新任务）
- Project checks（来自 AGENTS.md 的 verify 指令）
- Delivery 专属验收项（mutation、verification、review、complete_step 签收、capability 门禁）

Delivery 和 Standard 都不会注入隐藏模型消息做 readiness 重试。普通回合在可见模型回合结束后
立即停止并返回结构化缺口；Delivery 由前端展示显式的 `Continue checks` 恢复入口，只有用户
主动操作才会启动恢复回合。Standard 的 verification/review/signoff 缺口只作为完成提示处理。
Goal + Delivery 或 approved Plan 回合仍由 Goal/Plan FSM 自动续轮，不显示需要用户点击的重复卡片。

### 进展签名

Goal 的停滞计数只由宿主可验证且对当前 Goal **新颖**的信息重置：新的读取/搜索结果、Todo
状态变化、新的有效 mutation/verification/review/signoff receipt、Delivery checkpoint 变化或
终态 `update_goal` 报告。普通 Standard/Delivery 不再维护 task-progress 自动续跑指纹；用户的
显式恢复回合会重新建立当前 evidence 上下文。

### Todo 状态流

```
todo_write → agent 创建任务列表
complete_step → agent 标记某一步完成
advanceGoalAfterTurn → 读取 update_goal 报告 + readiness + evaluator
  ├─ complete + readiness 通过，或仅剩检查缺口的第二次 complete → 完成
  ├─ complete + readiness 缺失（首次，或仍有未完成 todos） → 拦截并列出缺失项，继续循环
  ├─ blocked → 立即阻塞
  ├─ 无报告 → evaluator 判定一次（失败则安全暂停）
  └─ 数字式无进展/Todo 阈值 → 重新规划并继续（不改变 Goal 状态）
```

### 并行调度架构

```
parallel_tasks Execute()
  ├─ 对每个子任务:
  │   ├─ 发射 ToolDispatch 事件（前端渲染卡片）
  │   ├─ 创建嵌套 sink（subSinkFor）
  │   ├─ 启动 goroutine 运行 RunSubAgentWithSession
  │   └─ 子任务工具调用自动嵌套显示
  ├─ WaitGroup 等待全部完成
  └─ 聚合结果返回
```

## 相关代码

- `internal/control/goal.go` — Goal FSM、turn recorder、兼容迁移、暂停/恢复与运行统计
- `internal/control/turn_orchestrator.go` — 每轮决策流程、evaluator 调用
- `internal/control/input.go` — `/goal` 命令解析与任务合约注入
- `internal/goaleval/` — 独立 bounded evaluator
- `internal/tool/builtin/updategoal.go` — `update_goal` 工具
- `internal/boot/boot.go` — 工具注册与 evaluator 装配

# Goal 模式 — 连续执行与结构化完成协议

Goal 将状态、续跑和模型报告分开。模型规划、验证并判断完成；宿主负责权限、可靠执行、持久化和显式资源限制，不生成质量门禁，也不调用独立完成 evaluator。
权限（仅可查看／工作区内修改／完全权限及沙箱）与 Goal 正交，工具权限和沙箱不受 Goal 开关影响。

## 功能一览

| 功能 | 行为 |
| --- | --- |
| `update_goal(complete)` | 正常结束边界提交模型完成声明，保留真实失败和未完成待办 |
| `update_goal(blocked)` | 记录原因并立即停止续跑 |
| `continue` 或漏报 | 正常结束后继续活动目标，不解析自然语言完成标记 |
| 取消、错误、权限或预算暂停 | 不解释为正常完成，不自动重启 |
| 恢复、分叉 | 只加载状态，显式启动或恢复后才允许续跑 |
| 实际检查记录 | 展示退出码、失败、中断和检查后的修改，不认证总体质量 |

## 使用方式

### 默认模式

```bash
/goal 实现一个 CLI 计算器
```

模型在每个目标 turn 结束时调用 `update_goal`：

- `continue`（附 `reason` 与可选 `next_action`）— 继续推进；
- `complete`（仅在请求完成、输出格式与约束满足、验证已尝试或声明不可用时）— 正常结束时提交模型声明，不附加宿主验收门槛；
- `blocked`（仅当下一步需要用户独有信息、不可逆或对外可见操作、或范围变化时）— 立即停止。

`complete` 还可以附一份自述 `completion`：

```json
{"status":"complete","completion":{
  "verified":["go test ./..."],
  "unverified":["desktop UI 未实际操作验证"],
  "risks":["迁移不可逆"]
}}
```

模型自述和真实命令回执分别展示。未执行、失败或早于最新修改的命令不能因自述变成验证通过；这种差异不会阻断 Goal。未完成的 todos 保持原状。

`update_goal` 只在活动 Goal turn 中可用；普通聊天调用会收到结构化错误且不改变任何状态。同值重复调用幂等，`continue` 可升级为 `complete`/`blocked`，终态后冲突调用被拒绝；目标被替换或清除后，迟到的报告/用量一律按 scope+epoch 拒绝。

### 统计、显式预算与暂停

旧的 simple/write/research 类别和 `/goal --simple`、`--research` 参数只为 sidecar/CLI 兼容保留，
不再改变执行额度。executor、planner、subagent、compaction、router、reviewer等计费用量
累计到 `tokensUsed`，真实 HTTP 请求（含重试）累计到 `requestsUsed`，Goal Run 的实际工作时间累计到
`workDurationMs`。默认情况下这些字段与 `turnsUsed` 都只做统计：

- 未配置 `goal_token_budget` 时 `tokensLimit` 为 `0`；配置正数后只表示用户选择的累计 token 阈值；
- 没有 provider 请求前的 token 预留/准入；
- 未配置对应预算时，累计 turn/token/request/work time 再大也不会单独暂停 Goal；
- `turnsLimit`、`noProgressLimit`、`budgetExtensions` 继续对外保留为 deprecated 兼容字段，固定返回 `0`。

可停止连续执行的条件：完成、模型通过 `update_goal(blocked)` 报告真实用户/外部阻塞、用户主动 pause/stop/clear、Provider/权限/宿主不可恢复错误，以及用户显式设置的
正数 token/步数/时间/成本预算。`task_time_budget_minutes = 0`（以及兼容读取的负数）关闭时间边界，
只有正数才启用。结构化卡死检测、Todo stall 与 `noProgressTurns` 只注入策略纠偏，不改变 Goal 状态。
**轮数不再是任何停止条件。** `/goal status` 在未设置 token 预算时显示纯统计：

```
runtime: turns 57 · requests 143 · tokens 2800000 · work time 42m
```

配置 `goal_token_budget` 时 token 统计会显示当前显式阈值；`/goal resume` 从 `budget_spend` 暂停
恢复时授予一个新的完整预算切片，但 turns、tokens、requests 与 work time 继续累计。旧版本因 `budget_turns`、`budget_tokens`、
`goal_run_budget`、`goal_stuck` 或 `no_progress` 暂停的 sidecar 在内存中归一化为可继续状态，正常保存时才写入，
但加载本身不会发送模型请求。活动 Goal 在磁盘写 `turnsLimit: -1` 作为旧 reader 的无限制哨兵；
新 API 将其解释为 `0`。新的 `budget_spend`（用户显式预算）不会被自动迁移；manual pause、legacy archive block 和真实 blocker 同样不自动解锁。

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

1. 执行当前模型回合，保留实际用量和执行结果。
2. 校验报告所属会话、Goal scope 和执行 epoch；拒绝迟到报告。
3. 取消、错误、权限等待和资源边界优先处理。
4. 正常结束时提交模型的完成或阻塞声明；否则保持活动。
5. 没有优先用户输入且允许续跑时，追加下一回合。

### 事实与历史

不再生成风险评分、验收比例或总体质量判定。新结果用 `assessmentKind: "facts"` 和
`verdict: "unknown"` 区分历史评估；unknown 不表示失败。旧回执和缺项不被清空或改写通过。
历史检查点不再阻塞当前任务，`/continue-checks` 保留一次性显式消费和启动失败回滚。

待办由 `todo_write` 更新。退役的 `complete_step` 兼容调用仅声明一个明确匹配的待办完成，
不要求证据、不推进下一项。Goal 或 Plan 结束均不会批量完成待办。

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
- `internal/control/turn_orchestrator.go` — 正常结束边界、用户输入优先和续跑驱动
- `internal/control/input.go` — `/goal` 命令解析与任务合约注入
- `internal/control/goal_start.go` — 显式激活与错误后停止续跑
- `internal/tool/builtin/updategoal.go` — `update_goal` 工具
- `internal/boot/boot.go` — 工具注册与权限装配

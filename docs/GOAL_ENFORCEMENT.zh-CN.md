# Goal 模式 — 结构化完成协议、预算控制与 Delivery 职责拆分

Reasonix 的 Goal 模式（`/goal`）将目标推进（Goal）、验收（Delivery）和权限（Ask/Auto/Yolo、Sandbox）三者保持正交：Goal 是唯一的跨 turn 调度器，Delivery 是纯质量门禁，工具权限与沙箱不受 Goal 开关影响。

## 功能一览

| 功能 | 触发方式 | 效果 |
|------|----------|------|
| 结构化完成协议 | `update_goal` 工具 | 每轮目标 turn 结束时模型通过工具报告 continue/complete/blocked（含 reason 与 next_action），取代旧的 `[goal:*]` footer 文本标记 |
| 完成校验 | 默认 | `complete` 声明必须通过 Delivery readiness（todos、验证、review、签收、能力门禁）才会真正完成；不满足时用缺失项开启下一轮 |
| 独立评审 | 无报告时 | 模型未调用 `update_goal` 时，宿主调用一次独立 bounded evaluator 判定；评审不可用/出错/不确定时安全暂停，绝不默认继续 |
| 执行预算 | 默认 | **轮次与无进展熔断**：简单 10 轮、写入型 20 轮、AutoResearch 40 轮；连续 4 轮无宿主可验证进展则暂停。累计 token 只做观测展示，**没有 token 硬上限**，也没有 provider 请求前预算准入 |
| 暂停/恢复 | `/goal pause` / `/goal resume` | 暂停保留 Goal、todo、Delivery checkpoint 与运行历史；轮次型暂停恢复时追加一档同类别**轮数**（`budget_extensions` 统计轮次追加次数） |
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

`update_goal` 只在活动 Goal turn 中可用；普通聊天调用会收到结构化错误且不改变任何状态。同值重复调用幂等，`continue` 可升级为 `complete`/`blocked`，终态后冲突调用被拒绝；目标被替换或清除后，迟到的报告/用量一律按 scope+epoch 拒绝。

### 预算与暂停

预算类别由目标文本推断，**只决定轮数**：

- **写入型（write，20 轮）**：含明确修改动词（修复/实现/更新…），或 Goal 中**不带问句/解释意图/只读诊断/否定修改约束**的故障陈述（如「数据模型管理器又出现历史 BUG 了」「应用打开设置时崩溃」）。
- **简单型（simple，10 轮）**：咨询、解释、「为什么…」、只分析/诊断/复现定位且不要修复等。
- **研究型（research，40 轮）**：AutoResearch 目标。

普通 Delivery 的只读/咨询分类不变；上述「裸故障默认 write」只作用于 Goal 轮数类别。

**Token 只观测、不设限**：executor、planner、subagent、compaction、router、reviewer、evaluator 等计费用量仍累计到 `tokensUsed` 并在 UI/CLI 展示，但：

- 不存在 `tokensLimit` 硬上限（对外字段固定为 `0`）；
- 没有 provider 请求前的 token 预留/准入；
- 累计 token 再大也不会单独暂停 Goal。

可停止 Goal 的条件：轮次耗尽、连续 4 轮无宿主可验证进展、evaluator 故障、显式 `blocked`、账号额度或人工暂停。

达到轮次预算后目标安全暂停（持久化层表现为 `blocked` + `stop_cause`，旧客户端安全显示为 blocked，不会误恢复自动运行）。`/goal status` 显示完整运行摘要：

```
runtime: turns 12/20, tokens 214000, no-progress 0/4, extensions 0
```

`/goal resume` 恢复目标：轮次型暂停追加一档同类别轮数（累计 token 与 `budget_extensions` 保留，no-progress 计数归零）；手动暂停或 evaluator 故障暂停不自动追加额度，除非原轮次预算已耗尽。旧版本因 `budget_tokens` 暂停的 sidecar 在新版本加载时会自动改为 `running` 并立即持久化。

上下文压缩继续使用全局既有策略（约 50% 提示、60% 工具结果清理、80% compact、90% 强制 compact）。Goal 开启本身不额外触发 summarizer，也不改变工具 Schema 或稳定 prompt 前缀。

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
5. 应用 readiness、轮次预算与 no-progress 门禁；
6. 由 Goal FSM 独占决定 complete、continue、blocked 或 pause。

`complete` 只有在 readiness 通过时才被接受；`blocked` 立即停止；evaluator 超时、报错、JSON 非法或返回 `uncertain` 一律安全暂停。

### 证据审计门控（Delivery）

Delivery 收敛为纯 readiness 服务，宿主可消费的结构化结果为
`ReadinessResult{Ready, Missing, Reason, ProgressKey}`：

- Canonical todos（当前 todo 列表）
- Project checks（来自 AGENTS.md 的 verify 指令）
- Delivery 专属验收项（mutation、verification、review、complete_step 签收、capability 门禁）

Delivery 不再自行注入隐藏模型消息做 3/6 次 readiness 重试：普通 Delivery 回合在第一次未满足的最终回答后立即结束并显示恢复卡；Goal + Delivery 回合由 Goal FSM 在统一轮次预算内自动续轮，不显示需要用户点击的重复卡片。

### 进展签名

只有宿主可验证信息才能重置停滞计数：todo 状态变化、新的有效 mutation/verification/review/signoff receipt、Delivery checkpoint 变化、新接受的 AutoResearch evidence、终态 `update_goal` 报告。任意工具调用、重复读取、仅改变措辞的回答或重复 continue 理由都不能伪造进展。

### Todo 状态流

```
todo_write → agent 创建任务列表
complete_step → agent 标记某一步完成
advanceGoalAfterTurn → 读取 update_goal 报告 + readiness + 预算
  ├─ complete + readiness 通过 → 完成
  ├─ complete + readiness 缺失 → 拦截并列出缺失项，继续循环
  ├─ blocked → 立即阻塞
  ├─ 无报告 → evaluator 判定一次（失败则安全暂停）
  └─ 轮次/无进展耗尽 → 安全暂停（blocked + stop_cause）
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

- `internal/control/goal.go` — Goal FSM、轮次预算、turn recorder、暂停/恢复、token 观测
- `internal/control/turn_orchestrator.go` — 每轮决策流程、evaluator 调用
- `internal/control/input.go` — `/goal` 命令解析与任务合约注入
- `internal/goaleval/` — 独立 bounded evaluator
- `internal/tool/builtin/updategoal.go` — `update_goal` 工具
- `internal/boot/boot.go` — 工具注册与 evaluator 装配

# Loop 模式深度分析：TUI `/loop` vs Controller Goal Loop

> 分析目的：确认 controller 层的 goal loop 体系（`SetGoal`/`runGoalLoopWithRaw`/`continueGoal`）能否真正替代 TUI 层的 `/loop` 命令，以及两种模式的适用场景和迁移路径。  
> 基于 `main-v2`（commit `1e280538`）

---

## 一、TUI `/loop` 实现分析

### 1.1 源码位置与规模

| 文件 | 行数 | 作用 |
|------|:----:|------|
| `internal/cli/loop.go` | 88 | `/loop` 命令的解析、启停、状态显示 |
| `internal/cli/chat_tui.go` | ~3,897 | loop 状态字段（`loopPrompt/loopInterval/loopIter`）+ tick 调度 + TurnDone 触发下一轮 |

### 1.2 核心状态

```go
// chatTUI 结构体（internal/cli/chat_tui.go:107-112）
loopPrompt   string        // 当前 /loop 的 prompt。空 = 无活跃 loop
loopInterval time.Duration // 重提交间隔。零 = 无活跃 loop
loopIter     int           // loop 已触发的次数
```

三个字段全部是 `chatTUI` 的私有成员，**无法从 controller 或其他前端复用**。

### 1.3 执行流程

```
用户输入: /loop 30 "检查系统状态"

  │
  ├─> runLoopCommand()  [loop.go:11]
  │    解析参数: interval=30s, prompt="检查系统状态"
  │    设置 m.loopPrompt, m.loopInterval, m.loopIter=0
  │    输出 "▸ /loop started: every 30s → 检查系统状态"
  │    m.loopIter++
  │    m.startTurnWithRaw(prompt, "/loop: "+prompt, prompt, prompt)
  │      └─> ctrl.SendWithRaw(sent, raw)    ← 标准的 controller 调用
  │
  ├─> TurnDone → chatTUI state = tuiIdle  [chat_tui.go:3427]
  │    由于 m.loopPrompt != "" && state != tuiRunning
  │    调度下一个 tea.Tick(m.loopInterval) → loopTickMsg  [chat_tui.go:1246-1247]
  │
  ├─> loopTickMsg 到达  [chat_tui.go:1368-1373]
  │    m.loopIter++
  │    输出 "▸ /loop iter 2 → 检查系统状态"
  │    m.startTurnWithRaw(...)  ← 再次发送完全相同的 prompt
  │
  └─> 重复直到停止
      停止条件:
       - 用户输入 "/loop stop"        [loop.go:18-19]
       - 用户在运行中输入任意文本      [chat_tui.go:1122-1126]
       - 用户按 Esc                   [chat_tui.go:978-979]
```

### 1.4 关键特征

| 特征 | 行为 |
|------|------|
| **调度机制** | `tea.Tick` 计时器，固定间隔 |
| **prompt 内容** | **每次都完全一样**，无历史感知 |
| **模型感知** | 模型不知道自己处于 loop 中 |
| **智能停止** | 无 — 仅靠外部信号停止 |
| **迭代上限** | 无 |
| **阻塞处理** | 运行中跳过 tick（`state == tuiRunning` 时丢弃 `loopTickMsg`）|
| **可复用性** | 仅 TUI，`chatTUI` 方法 |

### 1.5 与 Go 集成的关系

TUI `/loop` 的每次迭代最终调用的仍然是 `ctrl.SendWithRaw()`。**loop 的本质逻辑不在 controller 中，而是在 TUI 层拼装**——controller 每次收到的就是一个独立的 `"检查系统状态"` 消息，无法区分这是 loop 迭代还是用户新输入。

---

## 二、Controller Goal Loop 实现分析

### 2.1 源码位置与规模

| 文件 | 行数 | 作用 |
|------|:----:|------|
| `internal/control/controller.go` | 2,999 | `runGoalLoopWithRaw`(L510) / `continueGoal`(L590) / `advanceGoalAfterTurn`(L609) / `stopGoal`(L708) / `parseGoalStatusMarker`(L658) / `SetGoal`(L1293) / `Goal`(L1319) / `GoalStatus`(L1325) / `ClearGoal`(L1315) |
| `internal/control/input.go` | 291 | `GoalStatus*` 常量(L24-27) / `activeGoalBlock`(L134) / `ParseGoalCommand`(L191) / `Compose`(L93) |
| `internal/control/goal_test.go` | 151 | goal loop 的测试用例 |

### 2.2 核心状态

```go
// Controller 结构体（internal/control/controller.go:129-133）
goal        string   // 当前目标。空 = 无活跃目标
goalStatus  string   // "running" | "complete" | "blocked" | "stopped"
goalTurns   int      // 当前目标已执行的自动轮数
goalBlocks  int      // 连续相同阻塞原因的次数
goalBlock   string   // 最近的阻塞原因
```

### 2.3 执行流程

```
用户设置目标: /goal 部署这个微服务到 K8s

  │
  ├─> ctrl.SetGoal("部署这个微服务到 K8s")  [controller.go:1293]
  │    goal = "部署这个微服务到 K8s"
  │    goalStatus = "running"
  │    goalTurns = 0, goalBlocks = 0
  │
  ├─> ctrl.Send("开始执行")  [controller.go:446]
  │    └─> ctrl.SendWithRaw(input, input)  [controller.go:454]
  │         └─> ctrl.runGuarded(...)        [controller.go:455]
  │              └─> ctrl.runGoalLoopWithRaw(ctx, input, raw)
  │
  ├─> runGoalLoopWithRaw()  → runTurnWithRawDisplay()  [controller.go:514-521]
  │    │
  │    ├─> c.Compose(input)  [input.go:93]
  │    │    │  因为 goal!="" && goalStatus==running
  │    │    │  → input = activeGoalBlock(goal) + "\n\n" + input
  │    │    │  activeGoalBlock = "<active-goal>\n部署这个微服务到 K8s\n\n
  │    │    │  Goal mode: pursue this goal autonomously... 
  │    │    │  End every goal-mode reply with [goal:continue/complete/blocked:...]\n</active-goal>"
  │    │
  │    ├─> agent.Run(input)  ← 模型看到目标 + 执行指令
  │    │
  │    └─> 模型回复: "创建了 deployment.yaml\n...[goal:continue]"
  │
  ├─> continueGoal()  [controller.go:590-606]
  │    └─> advanceGoalAfterTurn()  [controller.go:609-656]
  │         │  解析模型回复的最后一行:
  │         │    "[goal:continue]"     → 继续
  │         │    "[goal:complete]"     → 完成，清除 goal，输出 notice
  │         │    "[goal:blocked:xxx]"  → 阻塞，同原因≥3次→终止
  │         │    无标记                → 继续（但不会自动触发新轮）
  │         │
  │         │  检查 maxGoalAutoTurns=50 → 超限自动阻止
  │         │
  │         │  继续？→ 进入下一轮
  │         │
  ├─> 下一轮: c.runTurnWithRawDisplay(ctx, goalContinueTurn, ...)
  │    │  goalContinueTurn = "Continue pursuing the active goal..."
  │    │
  │    └─> 模型继续执行，回复 "[goal:continue]"
  │
  └─> 重复直到 complete/blocked/stopped
```

### 2.4 Compose 方法的 goal 注入

`ctrl.Compose()` 是 goal loop 的关键桥梁：它在每次 `runTurnWithRawDisplay` 之前被调用，在用户输入前面插入 `<active-goal>` 块，让模型知道自己正处于目标执行模式。

```go
// internal/control/input.go:93-103
func (c *Controller) Compose(text string) string {
    // ...
    if strings.TrimSpace(goal) != "" && goalStatus == GoalStatusRunning {
        text = activeGoalBlock(goal) + "\n\n" + text
    }
    // ...
}
```

`activeGoalBlock()` 生成的注入内容（`input.go:134-146`）：

```
<active-goal>
部署这个微服务到 K8s

Goal mode: pursue this goal autonomously. Keep working across turns until 
the goal is complete. Prefer sensible defaults over asking the user; use 
ask only when you are truly blocked on a user-owned decision. Do not stop 
after describing a plan; execute the next useful step. End every goal-mode 
assistant reply with exactly one status marker on its own line: 
[goal:continue], [goal:complete], or [goal:blocked:<short reason>].
</active-goal>
```

### 2.5 目标循环的智能机制

| 机制 | 行为 |
|------|------|
| **模型自主决策** | 模型根据实际进度输出 `[goal:continue]`/`[goal:complete]`/`[goal:blocked:reason]` |
| **连续阻塞检测** | 相同阻塞原因连续出现 3 次 → 自动终止（`sameGoalBlock` 去重比较） |
| **轮次上限** | `maxGoalAutoTurns = 50`，超限自动阻止 |
| **上下文完整** | 每轮 sees 上一轮的输出（goal loop 非新 session）|
| **集成其他能力** | auto-plan 在首轮触发、checkpoint 为每轮建立边界、hook 正常触发 |
| **中转提示** | `goalContinueTurn` 每次继续时发送，引导模型接着干 |

---

## 三、对比分析矩阵

### 3.1 特性对比

| 特性 | TUI `/loop` | Controller Goal Loop | 能否替代 |
|------|:-----------:|:--------------------:|:--------:|
| **实现位置** | `cli/`（TUI 专属）| `control/`（传输无关）| ✅ 可替代 |
| **调度方式** | 固定时间间隔（`tea.Tick`）| 模型驱动（输出标记决定）| ❌ 不同范型 |
| **prompt 感知** | 无（每次都相同）| `<active-goal>` 注入提示 | ✅ 更优 |
| **历史累积** | 是（同一 session）| 是（同一 session）| ✅ 同等 |
| **智能停止** | ❌ 无 | ✅ `[goal:complete]` / 阻塞检测 / 上限 | ✅ 更优 |
| **迭代上限** | 无（无限）| 50（可调常量 `maxGoalAutoTurns`）| ⚠️ 需注意 |
| **固定间隔** | ✅ `tea.Tick(N秒)` | ❌ 无（连续循环）| ❌ 不可替代 |
| **Go 集成可用** | ❌ 仅 TUI | ✅ 全部前端 | ✅ 可替代 |
| **阻塞跳过** | ✅ 运行中跳过 tick | ✅ 控制器互斥锁保证 | ✅ 同等 |
| **状态查询** | `/loop` 查看 iter 数 | `Goal()` + `GoalStatus()` | ✅ 更优 |
| **测试覆盖** | ❌ 无单独测试 | ✅ `goal_test.go`（151 行，3 个测试用例）| ✅ 更优 |

### 3.2 行为对比（典型场景）

**场景 1：定时检查系统状态（每 30 秒一次）**

```
TUI /loop 30 "检查 /var/log/syslog 是否有错误"
  → 缺点：每次重新查，不记得上次查到哪里
  → 优点：定时触发轮询

Goal Loop SetGoal("监控 /var/log/syslog 中的错误")
  → 模型自主决定："检查了日志 → 只看新的日志 → 检查 → [goal:continue]"
  → 更智能：模型使用工具 tail/grep，每次读取新内容
  ← 结论：此场景 Goal Loop 更优
```

**场景 2：反复执行直到某条件满足**

```
TUI /loop 60 "检查 deployment 是否就绪"
  → 缺点：不知道已经检查了几次、结果如何
  → 循环直到外部停止

Goal Loop SetGoal("确认 production deployment 完全就绪")
  → 模型检查 → "有 3 个 pod 还在 Pending" → [goal:continue]
  → 模型再检查 → "所有 pod Running" → "deployment 就绪" → [goal:complete]
  ← 结论：Goal Loop 大幅更优
```

**场景 3：纯定时心跳，无状态累积**

```
TUI /loop 300 "输出 'alive' 到日志"
  → 完全适合：不需要智能，只需要定时重复

Goal Loop SetGoal("每 5 分钟输出一次 alive")
  → 模型可能会累积历史，不符合"纯心跳"语义
  → 需要额外 hack（每次 clear 会话历史）
  ← 结论：此场景 TUI /loop 更合适
```

### 3.3 触发链对比

```
TUI /loop:
  tea.Tick → loopTickMsg → startTurnWithRaw → ctrl.SendWithRaw → runTurnWithRawDisplay

Controller Goal Loop (via SetGoal):
  ctrl.Send → SendWithRaw → runGuarded → runGoalLoopWithRaw → 
    └─ runTurnWithRawDisplay (compose 注入 goal) → continueGoal →
        └─ advanceGoalAfterTurn (检查标记) →
            └─ runTurnWithRawDisplay (发送 goalContinueTurn) →
                └─ continueGoal (递归续接)
```

关键差异：TUI `/loop` 每次触发的是完全相同的 `SendWithRaw`（无 goal 注入），而 Goal Loop 在每次 `runTurnWithRawDisplay` 之前会通过 `Compose()` 注入 `<active-goal>` 块——这是 goal 感知的核心机制。

---

## 四、能否替代结论

### 4.1 可以完全替代的场景

| 场景 | 原因 |
|------|------|
| 需要模型自主完成复杂多步任务 | Goal loop 的 `[goal:continue]` 机制让模型做进度决策 |
| 监视/轮询类任务 | 模型使用工具（grep/tail/kubectl）比固定间隔更智能 |
| CI 级联任务（build → test → deploy）| 模型判断每一步结果再决定下一步 |
| 需要结果导向的迭代 | `[goal:complete]` 明确终止 |
| 需要 Go 集成 | Goal loop 在 controller 层，所有前端都可使用 |

### 4.2 不能直接替代的场景

| 场景 | 原因 | 变通方案 |
|------|------|---------|
| 纯定时心跳，无智能判断 | Goal loop 无时间间隔调度，模型会连续执行 | 在 Go 集成侧用 `time.Ticker` + `ctrl.Send()` 包装 |
| 固定间隔轮询（exactly N 秒一次）| Goal loop 模型回复 → 立即继续，无间隔 | 同上，外部控制间隔 |
| 无上限无限循环 | Goal loop 有 `maxGoalAutoTurns=50` | 修改常量或 SetGoal("") 后外部重新设置 |
| TUI 用户交互场景（Esc 取消、UI 反馈）| Goal loop 的取消通过 `ctrl.Cancel()`，TUI 行为需重新绑定 | `/goal` 命令已整合在 runGoalSubcommand |

### 4.3 迁移路径

**从 TUI `/loop` 迁移到 Controller Goal Loop**：

```
原: /loop 30 "检查系统状态"
新: /goal 检查系统状态
    （模型自动执行直到 [goal:complete]）
```

差异：
- `/goal` 没有时间间隔——模型完成后立即继续或停止
- `/goal` 在每轮提示前面注入目标上下文，模型知道自己在执行目标
- `/goal` 通过 `[goal:complete]` 智能停止，无需手动 `/goal stop`
- `/goal` 支持阻塞检测（3 次同原因阻塞自动停止）

**如果需要保留定时行为**（Go 集成场景）：

```go
// 保留定时能力但使用 controller goal loop
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    ctrl.SetGoal("检查 /var/log/syslog 中的错误")
    
    for range ticker.C {
        if ctrl.Running() {
            continue // 上一轮仍在执行，跳过
        }
        if ctrl.GoalStatus() == control.GoalStatusComplete {
            ctrl.SetGoal("检查 /var/log/syslog 中的错误") // 重置目标
        }
        ctrl.Send("继续检查系统状态")
    }
}()
```

---

## 五、TUI `/goal` 命令实现分析

需要特别注意：TUI 在 `runGoalSubcommand` 中对 `/goal` 的处理与 controller 的 `submit()` 不同。

### 5.1 TUI `/goal` 实现（chat_tui.go:3615-3640）

```go
func (m *chatTUI) runGoalSubcommand(input string) tea.Cmd {
    cmd, ok := control.ParseGoalCommand(input)
    // ...
    switch cmd.Action {
    case control.GoalCommandSet:
        m.planMode = false
        m.ctrl.SetPlanMode(false)
        m.ctrl.SetGoal(cmd.Text)
        m.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ...))
        return m.startTurn("Start pursuing the active goal now.", input, input)
        // ↑ 注意：这里用 startTurn（单轮执行），
        //   不是 runGoalLoopWithRaw（目标循环）！
    }
}
```

**关键发现**：TUI 的 `/goal` 命令**只调用了一次 `startTurn`**（单轮执行），**没有触发 controller 的 goal loop 自动续接**。这是因为 TUI 用了 `ctrl.Send()` 路径而非直接 `runGoalLoopWithRaw`——当你用 `ctrl.Send()` 时，controller 内部会调用 `runGoalLoopWithRaw`（见 `controller.go:455`），所以 goal loop 实际上是由 `Send` 触发的，不是由 `runGoalSubcommand` 显式调用的。

也就是说：
1. `ctrl.SetGoal(...)` → 设置目标
2. `return m.startTurn("Start pursuing the active goal now.", input, input)` → 内部调 `ctrl.SendWithRaw`
3. `ctrl.SendWithRaw` → `runGuarded` → `runGoalLoopWithRaw` → **循环开始**

对比 controller 的 `submit()` 路径：
```go
// controller.go:904-909
case GoalCommandSet:
    c.SetPlanMode(false)
    c.SetGoal(cmd.Text)
    c.notice(...)
    if c.runner != nil {
        c.runGuarded(func(ctx context.Context) error {
            return c.runGoalLoopWithRawDisplay(ctx, "Start pursuing the active goal now.", cmd.Text, display)
        })
    }
```

目标：`submit()` 路径显式调用了 `runGoalLoopWithRawDisplay`。但效果相同——`ctrl.Send()` 内部也会调用 `runGoalLoopWithRaw`。两类前端最终走的是同一条路径。

### 5.2 结论

无论是 TUI 的 `/goal` 还是 HTTP/其它前端的 `/goal`，最终都走 `runGoalLoopWithRaw` → `continueGoal` 路径。TUI 通过 `ctrl.SendWithRaw` 间接调用，HTTP 通过 `ctrl.Submit` 直接调用。goal loop 是传输无关的。

---

## 六、综合建议

### 6.1 给你的项目（Go 集成场景）

**推荐的循环方案**：

```
┌─ 你需要的是什么类型的循环？
│
├─ 模型自主执行多步任务 → 直接使用 controller goal loop
│    ctrl.SetGoal("重构 internal/boot 包")
│    ctrl.Send("开始执行")
│    // 模型会自动完成或 [goal:blocked]
│    status := ctrl.GoalStatus()
│
├─ 定时轮询 + 智能感知  → goal loop + 外部定时器
│    ticker := time.NewTicker(interval)
│    ctrl.SetGoal("检查系统状态")
│    for range ticker.C {
│        ctrl.Send("继续检查")
│    }
│
├─ 纯定时心跳（无状态）→ 自行封装
│    ctrl.SetAutoApproveTools(true)
│    for range time.Tick(5 * time.Minute) {
│        ctrl.Send("输出 alive 到 /var/log/health.log")
│    }
│
├─ TUI 你不需要 → 全部使用 controller API
│
└─ 结论：Goal loop 覆盖了 90%+ 的使用场景
```

### 6.2 可直接依赖的 controller API

| API | 签名 | 作用 |
|-----|------|------|
| `SetGoal(goal string)` | 设置目标，重置状态 | 入口 |
| `ClearGoal()` | 清除目标，停止循环 | 控制 |
| `Goal() string` | 获取当前目标 | 查询 |
| `GoalStatus() string` | 获取目标状态（running/complete/blocked/stopped）| 查询 |
| `Running() bool` | 当前是否有轮次在执行 | 并发控制 |
| `Send(input string)` | 启动一轮（内含 goal loop）| 触发 |
| `Cancel()` | 取消当前轮次 | 终止 |

### 6.3 与 TUI `/loop` 的兼容性

| TUI `/loop` 功能 | controller 对标 | 说明 |
|-------------------|-----------------|------|
| `/loop 30 "xxx"` | `SetGoal("xxx")` + `Send("...")` | 无时间间隔，模型驱动 |
| `/loop stop` | `ClearGoal()` + `Cancel()` | 清除目标 + 取消执行 |
| `/loop`（查看状态）| `Goal()` + `GoalStatus()` | 返回更丰富的状态 |
| 自动停止 | `[goal:complete]` 机制 | ✅ 更智能 |
| 阻塞停止 | 连续 3 次同原因 `[goal:blocked]` | ✅ 更智能 |
| 上限保护 | `maxGoalAutoTurns=50` | 差异：TUI 无限 |
| 迭代计数 | `goalTurns` | ✅ 返回实际执行轮数 |

---

## 七、潜在风险

1. **TUI `/goal` 不走 `submit()` 路径**：TUI 在 `runGoalSubcommand` 中调用了 `startTurn` 而非 `ctrl.Submit`，但最终都走到 `SendWithRaw` → `runGoalLoopWithRaw`，**行为一致**。可通过源码确认：`ctrl.Send()` 内部就是 `runGoalLoopWithRaw`（L455）。

2. **`maxGoalAutoTurns` 硬编码为 50**：建议暴露为 `boot.Options` 或 `control.Options` 的可配置字段。目前 Go 集成方只能修改源码常量。

3. **Goal loop 不支持定时执行**：如果你的用例需要 "每 5 分钟执行一次" 的纯定时语义，`/loop` 的 `tea.Tick` 机制是 TUI 的 built-in 方案。Go 集成方需自行添加 `time.Ticker` 包装。

4. **controller 的 goal 状态在 `Cancel()` 后会变成 `"stopped"`**：需要检查 `GoalStatus()` 来判断是被取消还是正常完成。

---

*报告生成日期：基于 main-v2 branch（commit `1e280538`）*

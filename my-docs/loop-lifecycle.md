# `/loop` 全生命周期追踪文档

**最后更新**: 2026-06-07  
**对应代码**: `internal/cli/loop.go` + `internal/cli/chat_tui.go`

---

## 用法

```
/loop <秒数> <提示词>

/loop stop
/loop off
/loop cancel

/loop              → 显示当前 loop 状态
```

秒数为整数，最小 5。例如 `/loop 30 check status` 表示每 30 秒执行一次 "check status"。

---

## 数据结构

```go
// chatTUI 结构体中的 loop 相关字段
loopPrompt   string        // 空字符串表示 loop 未激活
loopInterval time.Duration // 间隔时间（秒）
loopIter     int           // 已执行次数
```

---

## 核心逻辑

### 调度规则

```
TurnDone → 如果 loop 激活且没有 pendingInterject
         → 等 loopInterval 秒
         → loopTickMsg 触发

loopTickMsg → 如果 loop 激活 且 状态空闲
            → 执行 loop 迭代

loopTickMsg → 如果 loop 激活 但 正在运行
            → 丢弃（不做任何事）
```

**关键**: 调度**只发生在 TurnDone 时**。忙时不轮询、不重试。忙时到达的 tick 直接丢弃，等下次 TurnDone 重新调度。

### 会话恢复

```
TurnDone → loop 激活且无 interject → 调度 N 秒后 tick
       ↓
tick → 闲 → 执行（m.state = tuiRunning）
       ↓
    TurnDone → 重新调度（回到循环顶部）
```

---

## 29 个场景验证清单

### 启动与正常执行

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 1 | `/loop 30 check` | 立即执行第一次 | ✅ |
| 2 | TurnDone → 调度 30s 后 tick | ✅ |
| 3 | 空转 30s → tick 触发 → 执行下一轮 | ✅ |
| 4 | 第二轮的 TurnDone → 再次调度 30s | ✅ |

### 忙时行为

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 5 | 执行中 tick 触发 | 丢弃，不做任何事 | ✅ |
| 6 | 执行完毕后 TurnDone | 重新调度 | ✅ |

### 停止

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 7 | `/loop stop` 停掉等待中的 loop | 停止，下次 tick 因 `loopPrompt==""` 跳过 | ✅ |
| 8 | `/loop stop` 停掉执行中的 loop | 当前执行继续完成，TurnDone 不调度 | ✅ |
| 9 | 用户输入文字中断 loop | 立即停止 | ✅ |
| 10 | 中断后 TurnDone | 不调度（loop 已停） | ✅ |
| 11 | 中断前已排队的旧 tick 触发 | `loopPrompt==""` 挡住 | ✅ |
| 12 | Esc 停 loop | 调 `stopLoop()` | ✅ |

### 覆盖重启

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 13 | 执行中 `/loop 10 another` | 覆盖为新 prompt/间隔 | ✅ |
| 14 | 等待中 `/loop 10 another` | 覆盖，旧 tick 触发时用新 prompt | ✅ |

### 参数验证

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 15 | `/loop 0 x` | 低于最小 5，拒绝 | ✅ |
| 16 | `/loop abc x` | 非数字，拒绝 | ✅ |
| 17 | `/loop 30`（无提示词） | 拒绝 | ✅ |
| 18 | `/loop`（无参数） | 显示当前状态 | ✅ |
| 19 | 负数或零 | `n <= 0` 捕获 | ✅ |
| 20 | `" 30 "` 带空格 | TrimSpace 后正常解析 | ✅ |

### 与 interject 交互

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 21 | pendingInterject 存在时 | 不调度 tick | ✅ |
| 22 | 最后一个 interject 的 TurnDone | 调度 tick | ✅ |

### 边界条件

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 23 | 跨 session (`/new`) 不残留 loop | Controller 重置 | ✅ |
| 24 | 状态栏不显示 loop 信息 | 无 loop 相关 UI | ✅（已删除 TUI 显示）|

### 代码完整性

| # | 场景 | 预期 | 结果 |
|:--|:-----|:-----|:----:|
| 25 | `loopTickStart` 引用全部删除 | 不再使用 | ✅ |
| 26 | `loopTick()` 函数已删除 | 无残留 | ✅ |
| 27 | `formatDuration()` 已删除 | 不再使用 | ✅ |
| 28 | `pluralS()` 保留 | 仍在 stop 提示中使用 | ✅ |
| 29 | `time` import 在 loop.go 中保留 | parseIntSeconds 使用 | ✅ |

---

## 代码流程

```
用户输入 /loop 30 check status
  │
  ▼ runLoopCommand()
  ├── 解析 args
  ├── parseIntSeconds("30") → 30 * time.Second
  ├── m.loopPrompt = "check status"
  ├── m.loopInterval = 30s
  ├── m.loopIter = 0
  ├── Notice "▸ /loop started: every 30s → check status"
  ├── m.loopIter = 1
  └── m.startTurnWithRaw(...) → m.state = tuiRunning
      │
      ▼ TurnDone → ingestEvent → m.state = tuiIdle
      │
      ▼ agentEventMsg handler (processTurnDone)
      ├── balance refresh
      ├── pendingInterject 检查
      └── m.loopPrompt != "" && len(pendingInterject) == 0
          └── tea.Tick(30s) → loopTickMsg
              │
              ▼ 30 秒后
              loopTickMsg handler
              ├── m.loopPrompt != "" && m.state != tuiRunning → ✅
              │   ├── m.loopIter++ → 2
              │   ├── Notice "▸ /loop iter 2 → check status"
              │   └── m.startTurnWithRaw(...) → 执行新一轮
              │
              └── 如果此时 m.state == tuiRunning（用户正在说话或工具在执行）
                  └── 丢弃，不做任何事
```

---

## 所有可能的中断路径

```
loop 被停掉的路径:
  1. /loop stop / off / cancel → stopLoop()
  2. Esc 键 → stopLoop()
  3. 用户发送任何消息 → loopPrompt="" + loopInterval=0 + loopIter=0

loop 不会调度 tick 的路径:
  1. m.loopPrompt == ""
  2. len(m.pendingInterject) > 0（等 interject 队列清空）
  3. 当前 tick 触发时 m.state == tuiRunning（丢弃）
```

---

## 改动记录

| Commit | 变更 |
|:-------|:------|
| `342171fb` | 首次添加 `/loop` 命令，支持 s/m/h 后缀 |
| `ade5a622` | Esc 停 loop 逻辑 |
| `897cfec0` | 添加状态栏倒计时显示（`loopTickStart`）|
| `6811db75` | 简化：仅整数秒，忙时轮询改为丢弃，删除状态栏倒计时 |
| （本次） | 完全删除所有 TUI 显示代码（`loopTickStart`、View 块）|

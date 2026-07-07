# Goal 模式增强 — 强制执行与并行调度

Reasonix 的 Goal 模式（`/goal`）在 v1.9+ 中增加了来自 Oh-My-OpenAgent 的三个核心强制执行模式，让 AI agent 更可靠地完成复杂任务。

## 功能一览

| 功能 | 触发方式 | 效果 |
|------|----------|------|
| Todo 拦截 | 默认 | agent 声称 `[goal:complete]` 但 todos 未完成时，第一次拦截提醒 |
| Override | 默认 | 第二次连续 `[goal:complete]` 覆盖拦截，正常完成 |
| Strict 模式 | `/goal --strict ...` | 持续拦截直到 todos 全部完成，不允许覆盖 |
| 质量自检 | `--strict` | todos 全部完成后，提示 agent 做一轮自检（编译/测试/验证） |
| Idle 检测 | 默认 | 连续 2 轮无工具调用时，提醒 agent 推进或说明卡点 |
| 并行调度 | `parallel_tasks` 工具 | 并发派发多个子 agent，各自独立显示结果 |

## 使用方式

### 默认模式

```bash
/goal 实现一个 CLI 计算器
```

当 agent 干到一半就说"干完了"时，系统会自动拦截：

```
[!] goal intercept: incomplete todos remain
(override with a second [goal:complete])
```

拦截后 agent 会看到具体的未完成项列表。如果确实干完了但忘更新 todo，第二次 `[goal:complete]` 会正常放行。

### Strict 模式

```bash
/goal --strict 实现支付流程
```

每次 `[goal:complete]` 都会被拦住，直到 todos 全部完成。todos 完成后还会触发一轮自检（编译、跑测试、需求验证），通过后才真正结束。

适合对可靠性要求高的场景，如：
- 生产环境代码修改
- 需要完整测试覆盖的任务
- 多步骤复杂流程

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

### 证据审计门控

`Agent.GoalReadinessFailure()` 同时检查：
- Canonical todos（当前 todo 列表）
- Project checks（来自 AGENTS.md 的 verify 指令）

两者任一未通过就会阻止 `[goal:complete]`。

### Todo 状态流

```
todo_write → agent 创建任务列表
complete_step → agent 标记某一步完成
advanceGoalAfterTurn → 检查 todos + project checks
  ├─ 有不完整项 → goalInterceptMsg + 继续循环
  └─ 全部完成 → 正常结束（strict 模式先自检）
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

- `internal/control/controller.go` — Goal 状态机、advanceGoalAfterTurn
- `internal/control/input.go` — `/goal --strict` 命令解析
- `internal/agent/parallel_tasks.go` — 并行调度工具
- `internal/boot/boot.go` — 工具注册

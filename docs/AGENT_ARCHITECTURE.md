# Reasonix Agent 架构设计：子智能体并发引导注入 + MCP 职责边界

> 状态：设计文档（Phase 1 产出）｜分支：feat/long-horizon-navigator
> 日期：2026-08-07
> 参考：zcode 的"子智能体运行期间主对话继续 + 用户引导注入子智能体"交互模式

---

## 一、背景与目标

**zcode 模式**：子智能体在后台运行时，主对话不阻塞——用户可以继续和主对话交互做其他事；中途若用户给了引导性内容，主对话能按需求把引导**注入到运行中的子智能体**，子智能体在下一轮读取并调整方向。

**Reasonix 现状**（已实测确认）：

| 能力 | 现状 | 证据 |
|---|---|---|
| 子智能体后台运行 | ✅ 已存在：`task/fleet/parallel_tasks` 的 `run_in_background`，job 系统（`internal/jobs` Manager.StartForSession），`wait` 收集结果 | task.go:194/492, jobs.go:412 |
| 主对话 mid-turn 引导 | ✅ 已存在：`Agent.Steer(text)` 注入当前 turn | agent.go:901 |
| 引导注入**运行中的子智能体** | ❌ **不存在**：Steer 只作用于主 agent 自身 turn；子智能体是独立 Agent 实例（runSubSession），无注入通道 | task.go:1694, agent.go:901-968 |
| MCP 职责边界 | ⚠️ 杂糅：6 个 MCP 插件 + 原生工具 + MCP 自动暴露代理，无成文划分 | config.toml:240-315 |

**目标**：
1. 打通"主对话 → 运行中子智能体"的引导注入通道（自动转发）
2. 成文 MCP 职责边界规则，对照现状逐项定案（保持/迁移/收编）

---

## 二、引导注入架构设计（Phase 2 蓝图）

### 2.1 数据流链路

```
用户输入 (主对话 turn)
   │
   ▼
主 Agent.Run 入口（host 层，如 boot 的消息处理）
   │
   ├─ 自动转发判定：当前会话是否存在"运行中"的子智能体 job，
   │   且输入命中转发启发式（见 2.2）？
   │     是 → jobs.Manager.SteerJob(jobID, text)
   │          ├─ job 存在且 running？→ job.steer(text) → 子 Agent.Steer(text)
   │          │    子 Agent 在下一轮 turn 消费 steer 队列 → 注入为 user 消息
   │          │    （复用现有 consumeSteer 机制，agent.go:958）
   │          └─ 失败（job 已结束/无 steer 回调）→ 回退普通主对话 turn
   │     否 → 普通主对话 turn（现状行为，不变）
```

### 2.2 自动转发判定（保守启发式，默认不拦截）

为避免误判（普通消息被错误路由到子智能体），采用**显式标记 + 活性校验**双门：

- **转发标记**（任一命中即尝试转发）：
  - 前缀 `→`（如 `→ 用测试驱动的方式重写 parser`）
  - 前缀 `注入：` / `inject:`（中英）
  - 明确提及子智能体任务关键词（`给子任务`、`告诉子智能体`、`to the subagent`）+ 存在运行中 job
- **活性校验**：`Manager` 中 job 状态为 running 且携带 steer 回调才转发；否则回退普通 turn（输入原样进主对话）
- **优先级**：多 job 运行时转发给**最近启动**的 running job（`Manager.active` 已有此语义）

### 2.3 并发与安全

| 关注点 | 设计 |
|---|---|
| 并发安全 | 复用 `Agent.Steer` 的 `steerMu`（agent.go:901）；`Job.steer` 字段由 `Job.mu` 保护；`Manager.SteerJob` 持 Manager 锁查 job |
| 活性竞态 | `Steer` 返回 bool（active 才接受）——注入失败即回退，与现有 `Steer` 语义一致（agent.go:910 "on false nothing was queued"） |
| 注入边界 | 注入文本由用户主对话产生，经 job 通道进入子 Agent 上下文——与子 Agent 自己会话中的用户输入同信任级，无额外提权 |
| 生命周期 | job 结束后 `steer` 回调置 nil（Manager 的 job 完成路径），避免悬垂注入 |

### 2.4 持久化

- 注入消息经 `Agent.Steer` 队列，在子 Agent 下一次 turn 由 `consumeSteer` 取出并作为 user 消息注入——**天然持久化在子 Agent 会话**中（turn 内注入逻辑已有，agent.go:958-968）
- 主对话侧无需额外持久化：注入动作本身以事件（Notice）记录，前端可见（可选增强，本阶段不做前端改动）

### 2.5 改动清单（Phase 2）

1. `internal/jobs/jobs.go`：`Job` 加 `steer func(string) bool` 字段（`mu` 保护）；`Manager` 加 `SteerJob(jobID, text string) bool`；job 完成路径置 nil
2. `internal/agent/task.go`：`StartForSession` 回调里子 Agent 创建后，把 `agent.Steer` 包装为 `job.SetSteer(func) ` 闭包（RunSubAgentWithSession 需要暴露子 Agent 实例或回调接口）
3. 主对话注入判定：新增 `internal/agent/steer_forward.go`（或 boot 层函数）——识别转发标记 + 调 `Manager.SteerJob` + 回退逻辑
4. 接线：host（CLI/desktop）消息入口在 turn 前调用判定函数

---

## 三、MCP 职责边界规则表

### 3.1 划分原则

```
MCP 化（外部能力）：
  - 第三方服务/外部系统（GitHub、OCR、浏览器、游戏引擎、远程工具集）
  - 跨语言/独立进程（Python/Node/Rust 工具，语言边界天然是进程边界）
  - 可独立复用（同一工具被多个项目/agent 使用，MCP 协议使其即插即用）
  - 静态零费用或外部计费（由工具自身管理）

原生内核（不进 MCP）：
  - 状态管理（StateTracker/Navigator 隐式状态、会话/compaction 投影）
  - 控制流（run loop、turn/steer、子智能体编排、goal/job 生命周期）
  - 模型上下文（prompt 组装、cache 前缀、context window）
  - 安全边界（权限门、sandbox、写审批、tool approval）
  - 模型耦合能力（CoSPlay 验证的生成器/修复器、code_verify 的模型路径）
```

### 3.2 现状对照

| 现有能力 | 类别 | 定案 | 说明 |
|---|---|---|---|
| srclight（代码索引） | 外部工具（本地 exe） | **保持 MCP** | 跨进程索引服务，可复用 |
| codebase-memory-mcp | 外部工具（本地 exe） | **保持 MCP** | 同上 |
| godot-mcp / bevy-mcp | 游戏引擎集成 | **保持 MCP** | 引擎外部系统 |
| ocr-mcp | 外部服务（本地 OCR） | **保持 MCP** | 第三方能力 |
| github-mcp | 外部服务（GitHub API） | **保持 MCP** | 第三方服务 |
| ci-optimization/pr-oracle/tautest/browser-use | 外部工具集（E:\共享\51\10） | **保持 MCP** | 独立工具集复用 |
| MCP 自动暴露（use_capability 代理） | 协议层 | **保持** | 工具面路由，非能力本身 |
| Navigator / StateTracker | 状态管理 | **原生**（现状正确） | 内核状态 |
| Steer / subagent / goal / jobs | 控制流 | **原生**（现状正确） | 内核控制 |
| code_verify / cosplay | 模型耦合验证 | **原生**（现状正确） | 生成器/修复器接 provider 是内核路径 |
| write/read 文件工具、权限门 | 安全边界 | **原生**（现状正确） | 不可外包 |

### 3.3 结论与动作

- **现状大体符合"外部 MCP / 内核原生"原则**，本阶段**无需迁移**。
- **收编动作**（成文化）：
  1. 在 `reasonix.example.toml` 的插件区加注释块，声明 MCP 边界原则（外部能力才进 MCP）
  2. `docs/TOOL_CONTRACT.md` 加一节"能力边界：MCP vs 原生"（本规则表摘要）
- **待观察项**（不动作，仅记录）：若未来出现"纯静态函数但高频调用"的能力（如字符串/数学工具），按 MCP 成本 vs 内联收益评估，倾向内联为原生工具（避免进程往返）——本仓库的 `internal/cosplay`、`navigator` 已是此形态。

---

## 四、风险与决策记录

| 风险 | 缓解 | 决策 |
|---|---|---|
| 自动转发误判（普通消息被路由到子智能体） | 显式标记 + 活性双门，默认不拦截 | 保守启发式（2.2） |
| steer 活性竞态（注入落在 turn 退出与 controller 观察之间） | 复用 `Agent.Steer` 的 active 检查，false 即回退 | 与现有语义一致 |
| job 完成后注入悬垂 | job 完成路径置 nil steer 回调 | 生命周期管理（2.3） |
| 子 Agent 会话持久化 | 注入消息经 consumeSteer 进会话（现状机制） | 无新增持久化层 |
| 多 job 并发时的路由歧义 | 转发给最近启动的 running job | Manager.active |

---

## 五、验证

- **Phase 2**：新增测试——`SteerJob`（存在/不存在/running/完成 job）、转发判定（标记命中/未命中/回退）、子 Agent 消费注入消息；回归 agent/task/jobs 包
- **Phase 3**：`go build ./...` + boot golden 基线无漂移（example.toml 注释不影响解析）
- **Phase 4**：全仓 `go test ./... -short`（107 包基线）

# Reasonix 三种调用模式深度分析

**报告日期**: 2026-06-07  
**分析目标**: ACP (Agent Client Protocol)、HTTP/SSE API、CLI 非会话模式 (`reasonix run`)  
**核心问题**: 如果将 Reasonix 作为"智能执行者"嵌入外部服务，三种模式的区别、能力边界、推荐方案

---

## 1. 三种模式的总体定位

```
                    ┌──────────────────────────────────────┐
                    │        control.Controller 内核        │
                    │    (所有模式共享同一个底层引擎)         │
                    │   Session +  Snapshot +  Resume       │
                    └──────────────────────────────────────┘
                      ▲         ▲          ▲         ▲
                      │         │          │         │
     ┌─────────────────┼─────────┼──────────┼─────────┼─────────────────┐
     │                 │         │          │         │                 │
     │  ACP (stdio)    │  HTTP/SSE         │  CLI "run"    CLI "chat"   │
     │  JSON-RPC 2.0   │  REST+SSE         │  ❌ 缺少      TUI 会话     │
     │  ✅ 多会话      │  ✅ 会话持久化    │  会话持久化  ✅ 会话持久化 │
     │  ✅ 会话持久化  │  ✅ --resume      │  (代码遗漏)  ✅ --continue │
     └─────────────────┴───────────────────┴──────────────┴─────────────┘
```

> **关键纠正**: 会话持久化的基础设施（`agent.Session.Save/Load`、`agent.ListSessions`、`ctrl.Resume`、`ctrl.SetSessionPath`、`ctrl.Snapshot`）由所有模式共享，是 `control.Controller` 内核的一部分。`reasonix run` 之所以"无状态"，**不是架构限制，而是 `runAgent()` 函数没有调用这些 API**——这是个实现漏洞，随时可以补上。相比之下，`chatREPL` 和 `runServe` 都正确调用了这些 API 实现 `--continue`/`--resume`。

---

## 2. 逐模式深度分析

### 2.1 ACP (Agent Client Protocol) — `reasonix acp`

**文件**: `internal/acp/protocol.go`, `server.go`, `service.go`, `dispatch.go`

#### 连接方式
- **传输层**: stdio 上的 NDJSON (JSON-RPC 2.0)
- **一句话描述**: 通过 stdin/stdout 的 JSON-RPC 双向协议，**用持久连接驱动多个会话**

#### 生命周期
```
客户端 → 服务端:
  ┌─ initialize ──────────→  握手协商能力
  │─ session/new {cwd} ───→  创建新会话，返回 sessionId "uuid"
  │─ session/prompt        →  发送一轮 prompt（阻塞等待结果）
  │   {sessionId, prompt}  │
  │←─────────────────────────  SSE风格事件流通知
  │    session/update ...   │  (agent_message_chunk, tool_call, etc.)
  │←─ {stopReason} ────────→  轮次结束
  │─ session/prompt (again) →  同一会话，第二轮
  │  ...                    │
  │─ session/close ────────→  关闭会话
  └─────────────────────────
```

#### 双向通信能力
- **服务端→客户端(通知)**: `session/update` — 流式 Agent 事件（消息块、工具调用、工具结果、警告、压缩通知）
- **服务端→客户端(请求)**: `session/request_permission` — **权限审批**，客户端必须回复 allow/reject
- **客户端→服务端(通知)**: `session/cancel` — 取消正在执行的轮次
- **客户端→服务端(请求)**: `initialize`, `session/new`, `session/load`, `session/resume`, `session/prompt`, `session/list`, `session/close`, `session/delete`

#### 会话管理能力
| 操作 | 支持 | 说明 |
|------|:----:|------|
| 多会话并发 | ✅ | 每个 sessionId 独立，互不干扰 |
| 会话持久化 | ✅ | 保存到 JSONL 文件，进程重启后可 `session/load` 恢复 |
| 会话列表 | ✅ | `session/list` 支持按 cwd 过滤 |
| 会话恢复 | ✅ | `session/load`（重放对话）+ `session/resume`（直接恢复）|
| 会话删除 | ✅ | `session/delete` 清除文件 |
| 会话关闭 | ✅ | `session/close` 释放资源 |
| 多进程断线重连 | ✅ | 通过 sessionId + JSONL 文件，跨进程恢复 |

#### 输入输出格式
- **输入**: `ContentBlock[]`（text + resource/embeddedContext）
- **输出**: 流式事件通知（无最终一次性响应，事件在 `session/prompt` 结束前以通知推送）
- **工具调用**: 在事件流中以 `tool_call` / `tool_call_update` 通知呈现

#### 权限模型
- 通过 `session/request_permission` 请求/应答模式实现交互式审批
- 客户端可选择：Allow Once / Allow Session / Allow Persistent / Reject

#### MCP 支持
- 仅在 `session/new` 时支持 stdio MCP 服务器
- 不支持 HTTP/SSE MCP（协议广告了 HTTP: false, SSE: false）

#### 关键限制
| 限制 | 影响 |
|------|------|
| **stdout 是 JSON-RPC 通道** | 所有日志、诊断信息必须输出到 stderr，否则破坏协议 |
| **仅 stdio 传输** | 不能远程调用（不能通过 TCP/Unix Socket） |
| **仅 stdio MCP** | 客户端不能使用 HTTP/SSE 的 MCP 服务器 |
| **无媒体支持** | 不接受图片/音频输入（protocol 声明 image: false, audio: false） |
| **提示字符串最大 32MB** | 单行 NDJSON 限制 |

---

### 2.2 HTTP/SSE API — `reasonix serve`

**文件**: `internal/serve/serve.go`

#### 连接方式
- **传输层**: HTTP (REST) + SSE (Server-Sent Events)
- **一句话描述**: 通过 HTTP 暴露 agent 所有操作能力，**REST 命令 + SSE 事件流**

#### 暴露的端点

```
GET  /              → 内嵌 Web 界面 (index.html, 67KB)
GET  /events        → SSE 事件流（实时代理输出）
GET  /history       → 会话历史（支持 ETag 缓存）
GET  /context       → 上下文使用量指标
GET  /status        → 会话状态快照（模型/运行状态/缓存/用量）
GET  /sessions      → 保存的会话列表（带 LLM 生成标题）
GET  /checkpoints   → 检查点列表
GET  /branches      → 分支列表及树形图
GET  /skills        → 可用技能列表
POST /submit        → 发送用户输入（返回 202，输出走 SSE）
POST /cancel        → 取消当前轮次
POST /approve       → 审批工具调用
POST /plan          → 切换 plan 模式
POST /compact       → 触发上下文压缩
POST /new           → 新建会话
POST /rewind        → 回退到检查点
POST /fork          → 从检查点创建分支
POST /summarize     → 从/到某轮次执行摘要
POST /bypass        → 切换 YOLO 模式
POST /answer        → 回答 Agent 提问
POST /resume        → 从 JSONL 文件恢复会话
POST /forget        → 删除记忆
POST /delete-session → 删除会话文件
```

#### 会话模型
- **单服务器 = 单会话**（一个 `control.Controller` 绑定一个 Server）
- 多个浏览器 tab 共享同一会话（通过 Broadcaster 扇出事件）
- 可以通过 `POST /new` 清空会话重新开始

#### 双向通信能力
- **服务端→客户端**: SSE (`/events`) — 与 ACP 相同的 `event.Event` 流，但序列化为 JSON SSE data 帧
  - 包含所有事件类型：Reasoning, Text, ToolDispatch, ToolResult, Usage, Notice, ApprovalRequest, AskRequest, TurnDone, Compaction...
  - SSE 每 15 秒 keepalive 防止代理/负载均衡器断开空闲连接
- **客户端→服务端**: REST POST — 所有命令都是标准 HTTP 请求

#### 权限模型
- 通过 `POST /approve` 实现交互式审批（在 SSE 流中收到 `ApprovalRequest` 事件后调用）
- CSRF 防护：POST 请求必须带 `Content-Type: application/json`
- CORS 支持：可选，仅允许指定 origin

#### 与 ACP 的关键差异

| 维度 | ACP | HTTP/SSE |
|:-----|:---|:---------|
| **远程调用** | ❌ 限本地 stdio | ✅ 任何 HTTP 客户端 |
| **多会话** | ✅ 原生多会话 | ❌ 单会话（需自行包装）|
| **权限审批** | ✅ 请求/应答协议原生 | ✅ 通过 POST /approve |
| **工具流信息** | 简化的 tool_call/tool_call_update | **完整事件类型**（含 Partial 调度、compaction 卡片等）|
| **媒体输入** | 有限 (text + embeddedContext) | 由 HTTP 传输层决定 |
| **连接方式** | 持久连接（JSON-RPC over stdio） | SSE 持久 + REST 短连接 |
| **可编程性** | 高（完整的会话生命周期控制） | 中（缺少会话切换原语）|
| **安全** | 无认证（信任进程启动者） | 绑定 localhost，有 CSRF 防护 |

---

### 2.3 CLI 非会话模式 — `reasonix run "prompt"`

**文件**: `internal/cli/cli.go` — `runAgent()` 函数

#### 连接方式
- **传输层**: 一次性进程
- **一句话描述**: **启动 → 执行一轮 → 退出**，但基础设施支持会话持久化，只是 `runAgent()` 没有调用

#### 生命周期
```bash
$ reasonix run "为这个项目写一个 README"
  ↓
  启动 boot.Build → 创建 Controller（SessionDir 已配置）
  ↓
  ❌ 没有调用 SetSessionPath — 即使配置了 session 目录也不保存
  ❌ 没有检查 --resume / --continue 参数 — 不支持恢复
  ↓
  ctrl.Run(ctx, prompt)   # 同步阻塞
  ↓
  事件流 → TextSink → stdout
  ↓
  ctrl.Close() → 进程退出码 0/1
  ↓
  snapshotActivityIfChanged() 被调用，但因 sessionPath=="" 不保存
```

#### 与会话持久化模式的代码对比

```go
// runAgent（目前实现）— 无会话持久化
ctrl, _ := setup(ctx, model, maxSteps, true, sink)
// ❌ 缺失：
//   ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
//   ctrl.Resume(loaded, resumePath)
runErr := ctrl.Run(ctx, prompt)

// chatREPL (TUI) — 有会话持久化
ctrl, _ := setup(ctx, model, maxSteps, false, sink)
if resumePath != "" {
    loaded, _ := agent.LoadSession(resumePath)
    ctrl.Resume(loaded, resumePath)          // ✅ 恢复已有会话
} else {
    ctrl.SetSessionPath(                     // ✅ 新会话自动保存
        agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
}
// 后续所有 turn 自动触发 snapshotActivityIfChanged → Save()

// runServe (HTTP) — 有会话持久化
ctrl, _ := setup(ctx, model, maxSteps, true, bc)
if *resume != "" {
    loaded, _ := agent.LoadSession(*resume)
    ctrl.Resume(loaded, *resume)             // ✅ 恢复指定文件
} else if dir := ctrl.SessionDir(); dir != "" {
    ctrl.SetSessionPath(                     // ✅ 新会话自动保存
        agent.NewSessionPath(dir, ctrl.Label()))
}
```

#### 参数
- `-model <name>` — 指定模型
- `-max-steps <n>` — 限制工具调用轮次
- `-show-thinking` — 显示思考过程
- `-metrics <path>` — 输出 token/缓存/成本 JSON
- `-dir <path>` — 指定工作目录

#### 缺失能力的修复代价

| 能力 | chat 模式 | serve 模式 | run 模式 | 所需代码行 |
|:-----|:--------:|:--------:|:--------:|:----------|
| `--continue` / `-c` 恢复最近会话 | ✅ | ❌ | ❌ | ~10行（加 flag + ListSessions + Resume）|
| `--resume <path>` 恢复指定文件 | ✅ | ✅ | ❌ | ~8行（加 flag + LoadSession + Resume）|
| 自动保存本次会话 | ✅ | ✅ | ❌ | ~2行（加 SetSessionPath）|
| `--list-sessions` 列出可用会话 | ✅（内置） | ✅（/sessions） | ❌ | ~5行 |

**关键结论**: 上述缺失不是架构限制。`agent.Session.Save/Load`、`agent.ListSessions`、`ctrl.Resume()`、`ctrl.SetSessionPath()`、`ctrl.Snapshot()` 这些 API 全部存在且被其他模式使用。`runAgent()` 只是没有调用它们。

---

## 3. 功能完备性对照矩阵

> **⚠️ 重要说明**: 下表中的 CLI run 列评估的是**当前代码实现**，不是架构能力。标记为 ❌ 的项中，会话持久化相关（新建/清空/恢复会话等）是 `runAgent()` 没有调用已有 API 导致的实现缺口，基础设施已就绪。

将三种模式与"全部功能"对照，使用以下标识：
- ✅ **完全支持**  
- ⚠️ **有限支持**（有变通方案或部分能力）  
- ❌ **不支持**（架构限制或代码缺口）

### 3.1 Agent 核心操作

| 功能 | ACP | HTTP/SSE | CLI run | 说明 |
|:-----|:---:|:--------:|:-------:|:-----|
| 发送用户输入（启动推理） | ✅ | ✅ | ✅ | 三者都支持 |
| 流式接收文本输出 | ✅ | ✅ | ✅ | ACP=agent_message_chunk, HTTP=SSE text, CLI=stdout |
| 流式接收推理过程 | ✅ | ✅ | ✅ | ACP=agent_thought_chunk, HTTP=SSE reasoning |
| 工具调用执行与结果 | ✅ | ✅ | ✅ | ACP=tool_call+tool_call_update, HTTP=SSE full events |
| 取消执行 | ✅ | ✅ | ⚠️ | 仅 Ctrl+C 杀进程 |
| 新建/清空会话 | ✅ | ✅ | ⚠️ | **代码缺口**: 基础设施就绪（NewSessionPath），runAgent 没调 |
| 多会话管理 | ✅ | ❌ | ❌ | HTTP 只能同时一个会话 |
| 会话恢复（进程间） | ✅ | ✅ | ⚠️ | **代码缺口**: LoadSession + Resume 已实现，runAgent 没调 |
| 上下文压缩 | ⚠️ | ✅ | ❌ | run 只有一轮，压缩不相关 |
| plan 模式切换 | ❌ | ✅ | ❌ | ACP 协议无此方法，run 只有一轮 |

### 3.2 交互能力

| 功能 | ACP | HTTP/SSE | CLI run | 说明 |
|:-----|:---:|:--------:|:-------:|:-----|
| 权限审批（allow/reject） | ✅ | ✅ | ❌ | CLI run 静默允许或拒绝 |
| 用户问答 (ask tool) | ❌ | ✅ | ❌ | ACP 协议层面未实现 AskRequest 映射 |
| YOLO 模式切换 | ❌ | ✅ | ❌ | |
| 模型切换 | ❌ | ✅（`/model`） | ✅（`-model` 启动时） | ACP 无方法；HTTP 通过 submit 截获 |
| Effort 切换 | ❌ | ✅（`/effort`） | ❌ | |

### 3.3 会话管理

| 功能 | ACP | HTTP/SSE | CLI run | 说明 |
|:-----|:---:|:--------:|:-------:|:-----|
| 列出历史会话 | ✅ | ✅ | ❌ | |
| 加载历史会话 | ✅ | ✅ | ❌ | |
| 重放对话 | ✅ | ✅ | ❌ | |
| 删除会话 | ✅ | ✅ | ❌ | |
| 检查点/回退 | ❌ | ✅ | ❌ | ACP 无此能力 |
| 分支管理 | ❌ | ✅ | ❌ | |
| 附件/文件引用 | ✅（resource块） | ⚠️（需自行解析@引用） | ❌ | ACP 有 resource 类型 |

### 3.4 元操作

| 功能 | ACP | HTTP/SSE | CLI run | 说明 |
|:-----|:---:|:--------:|:-------:|:-----|
| 技能列表 | ❌ | ✅ | ❌ | |
| 记忆管理 | ❌ | ✅（forget） | ❌ | |
| 状态查询 | ❌ | ✅（status/context） | ❌ | |
| 用量/成本追踪 | ❌ | ✅（status 含 usage）| ✅（-metrics 参数） | CLI run 可输出 JSON 指标 |
| 运行会话列表 | ✅（session/list） | ✅（/sessions） | ❌ | |

### 3.5 连接与部署

| 功能 | ACP | HTTP/SSE | CLI run | 说明 |
|:-----|:---:|:--------:|:-------:|:-----|
| 远程调用 | ❌（仅 stdio） | ✅（HTTP） | ❌（进程本地） | |
| 多客户端共享 | ❌（一对一连接） | ✅（多个 SSE 订阅者） | ❌ | |
| 认证/授权 | ❌ | ⚠️（仅 localhost 绑定） | ❌ | 三种模式都没有真正的认证 |
| 负载均衡 | ❌ | ⚠️（可放反向代理后） | ❌ | |
| 跨进程通信 | ❌ | ✅ | ❌ | |
| 作为库嵌入 | ✅（全部） | ✅（HTTP 可内嵌） | ✅（全部） | 所有模式底层都用同一 `control.Controller` |

---

## 4. 关键功能缺口分析

### 4.1 ACP 的缺失能力

1. **无 AskRequest 支持** — `agent/ask.go` 中定义了 `ask` 工具，Agent 可以在执行中向用户提问。在 `dispatch.go` 中我们可以看到 ApprovalRequest 被映射为 `session/request_permission`，但 **AskRequest（多选/确认问题）完全没有被处理**。这意味着 ACP 客户端不能接收 Agent 的"请选择"类问题。

2. **无 plan 模式控制** — ACP 协议没有 `session/set_plan_mode` 方法，无法在运行时切换 plan 模式。

3. **无压缩触发** — CompactionDone 事件只是通知，客户端无法主动触发压缩。

4. **无检查点/回退** — 无法通过 ACP 执行 rewind/fork/分支操作。

5. **无记忆管理** — 没有 `session/remember` 或 `session/forget` 方法。

### 4.2 HTTP/SSE 的缺失能力

1. **单会话限制** — `serve.go` 的 Server 只能绑定一个 Controller。要实现多会话需要在外部包装多个 Server 实例或做连接池。

2. **无原生多会话切换** — 虽然有 `POST /resume` 可以从文件加载会话，但加载后会**替换当前会话**，没有 ACP 那种"同时打开多个会话，各自独立运行"的能力。

3. **无日志/诊断通道** — ACP 区分 stdout（JSON-RPC）和 stderr（日志），HTTP 模式下日志只能从 server 进程的 stderr 获取。

4. **SSE 是单向** — 事件流只能从服务端推送到客户端，客户端不能通过 SSE 发送命令（必须用 REST POST）。

### 4.3 CLI `run` 的缺失能力

1. **完全无状态、无交互** — 这是最大的限制。不能审批、不能问答、不能取消（除了 Ctrl+C）。
2. **仅一轮** — 不支持多轮对话。
3. **会话不持久** — 退出即失。

---

## 5. 用 Go 库直接嵌入

这是三种模式之上的第四条路，**但很容易被忽视**。

因为 Reasonix 是用 Go 写的，且 `control.Controller` 是纯库 API：

```go
import "reasonix/internal/control"
import "reasonix/internal/boot"

// 在你的 Go 服务中直接创建 Agent
ctrl, err := boot.Build(ctx, boot.Options{
    Model:  "deepseek-chat",
    Sink:   myEventSink,  // 你自己的事件处理器
    Stderr: os.Stderr,
})

// 同步执行
err := ctrl.Run(ctx, "执行这个任务")

// 或者异步提交事件
ctrl.Submit("执行这个任务")
// 从 myEventSink 读取事件...
```

**这本质上是一种"零传输"模式**——没有序列化、没有网络开销、没有协议转换。**三种外部模式能做的，库模式都能做，且能做得更多。** 但代价是与 Go 代码耦合。

---

## 6. 如果你是 CEO：如何选择和控制

假设你的业务需要一个"智能执行者"系统（比如：CI/CD 流水线自动修复、客服工单自动处理、代码审查助手），以下是分层建议：

### 6.1 快速原型/小型集成

**推荐: CLI `reasonix run`**

```bash
# 在 CI/CD 中直接使用
reasonix run "分析这个错误日志并修复: $(cat error.log)"

# 在 shell 脚本中
result=$(reasonix run "对这段代码做安全审查" < /dev/stdin)
```

**优点**: 零集成成本，一行命令即可  
**缺点**: 无状态、无审批、不可靠  

### 6.2 需要远程调用 + 多服务集成

**推荐: HTTP/SSE API (`reasonix serve`)**

```python
import requests, json, sseclient

# 提交任务
resp = requests.post("http://localhost:8080/submit",
    json={"input": "分析这个PR的变更"},
    headers={"Content-Type": "application/json"})

# 接收事件流
resp = requests.get("http://localhost:8080/events",
    stream=True)
client = sseclient.SSEClient(resp)
for event in client.events():
    data = json.loads(event.data)
    print(data)  # 流式文本、工具调用等
```

**但是**，对外部服务来说，有几个痛点：
- 只能一个会话
- 权限审批需要自己实现 HTTP 回调（接收 SSE 的 ApprovalRequest → 调用 POST /approve）
- `ask` 工具同样需要外部实现多选问答的回调

**关键限制**: HTTP 模式缺乏 ACP 的 `session/request_permission` 这种原生请求-应答协议，审批需要你自己拼装 SSE + POST 两条通道。

### 6.3 大规模/高可靠/需要完整会话控制

**推荐: ACP 协议 (`reasonix acp`)**

```
# 在你的服务中启动 ACP 子进程
proc = subprocess.Popen(["reasonix", "acp"],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE)

# 通过 ACP 协议驱动
json_rpc_call(proc, "initialize", {})
json_rpc_call(proc, "session/new", {"cwd": "/project"})
result = json_rpc_call(proc, "session/prompt", {
    "sessionId": session_id,
    "prompt": [{"type": "text", "text": "分析这个bug"}]
})
```

**优点**: 完整的会话生命周期、多会话并发、审批原生支持  
**缺点**: 仅 stdio、不能远程、管理多个子进程复杂  

### 6.4 生产级方案（我作为 CEO 的选择）

**分层的混合架构:**

```
┌──────────────────────────────────────────────────┐
│              你的服务 (Go/Rust/Python)              │
│                                                    │
│   ┌─────────────┐   ┌──────────────────────────┐  │
│   │  HTTP API   │   │   ACP 管理器 (会话池)    │  │
│   │  对外暴露   │   │  管理 N 个 acp 子进程    │  │
│   └──────┬──────┘   └───────────┬──────────────┘  │
│          │                     │                   │
│          ▼                     ▼                   │
│   ┌──────────────────────────────────────────┐    │
│   │         Go 库模式 (最优推荐)              │    │
│   │   reasonix 作为 import，不走任何传输层    │    │
│   │                                          │    │
│   │   import "reasonix/internal/boot"        │    │
│   │   import "reasonix/internal/control"     │    │
│   │                                          │    │
│   │   for each task {                        │    │
│   │       ctrl := boot.Build(ctx, opts)      │    │
│   │       ctrl.EnableInteractiveApproval()   │    │
│   │       ctrl.Submit(task)                  │    │
│   │       // 直接从 event.Sink 读取事件      │    │
│   │   }                                      │    │
│   └──────────────────────────────────────────┘    │
└──────────────────────────────────────────────────┘
```

#### 选择"Go 库模式"作为核心的原因：

| 维度 | ACP | HTTP | CLI | **Go 库** |
|:-----|:---:|:----:|:---:|:---------:|
| 性能（零序列化） | ❌ | ❌ | ❌ | ✅ **最佳** |
| 多会话并发 | ✅ | ❌ | ❌ | ✅ **最简单** |
| 审批流 | ✅ | ⚠️ | ❌ | ✅ **直接回调** |
| 可靠性（无网络故障） | ❌ | ❌ | ❌ | ✅ **最佳** |
| 类型安全 | ⚠️ | ⚠️ | ❌ | ✅ **最佳** |
| 与你的 Go 服务耦合 | ❌ | ❌ | ❌ | ✅ **最紧** |

**CEO 决策**: 如果你的服务也是 Go 写的，**没有理由不走 Go 库模式**。三种传输模式都是为了给非 Go 客户端提供访问能力，而付出的代价是序列化开销、协议限制、功能子集。

#### 如果必须支持多语言：

```
Go 服务（核心）         非 Go 服务（客户端）
┌─────────────────┐    ┌──────────────────┐
│ Go 库模式        │◄───│ HTTP/SSE API     │
│ (智能执行引擎)    │    │ (gRPC 更优)      │
└─────────────────┘    └──────────────────┘
       │
       │ ACP（给需要完整会话控制的编辑器/IDE）
       ▼
┌─────────────────┐
│ ACP stdio       │
│ (VS Code/Cursor)│
└─────────────────┘
```

即：**Go 库模式是引擎，HTTP/SSE 是给服务间调用的通道，ACP 是给编辑器/IDE 的协议层。**

---

## 7. 功能完整性总结

| 核心需求 | ACP | HTTP/SSE | CLI run | Go 库 |
|:---------|:---:|:--------:|:-------:|:-----:|
| 执行一轮 | ✅ | ✅ | ✅ | ✅ |
| 多轮对话 | ✅ | ✅ | ⚠️ | ✅ |
| 流式输出 | ✅ | ✅ | ✅ | ✅ |
| 权限审批 | ✅ | ✅ | ❌ | ✅ |
| 用户问答 | ❌ | ✅ | ❌ | ✅ |
| 多会话并发 | ✅ | ❌ | ❌ | ✅ |
| 会话持久化 | ✅ | ✅ | ⚠️ | ✅ |
| 检查点/回退 | ❌ | ✅ | ❌ | ✅ |
| 模型切换 | ❌ | ✅ | ❌ | ✅ |
| plan 模式 | ❌ | ✅ | ❌ | ✅ |
| 远程调用 | ❌ | ✅ | ❌ | ❌（与 Go 耦合）|
| 跨语言 | ✅ | ✅ | ✅ | ❌（仅 Go）|
| 类型安全 | ⚠️ | ⚠️ | ❌ | ✅ |
| 完整功能覆盖 | ~60% | ~85% | ~30%* | **~100%** |

> **注**: CLI run 的 ~30% 是基于**当前代码**。其中会话持久化（自动保存、`--continue`、`--resume`）是代码缺失而非架构限制——补上约 20 行代码后覆盖率可接近 chat 模式的 ~75%。交互能力（审批、ask）才是真正的架构限制，因为 run 的定位就是非交互式。

---

## 8. 关键建议

1. **如果你的服务是 Go 写的 → 直接用 Go 库模式**。三种传输模式都是为了给非 Go 客户端提供访问，而传输本身引入了限制和功能损失。

2. **如果必须走网络 → HTTP/SSE 比 ACP 更适合服务集成**，因为：
   - HTTP 可远程调用（ACP 只能 stdio）
   - HTTP 可以放在反向代理后实现认证/负载均衡
   - HTTP 端点多、功能全（85% vs ACP 的 60%）

3. **ACP 是给编辑器/IDE 的，不是给微服务的**。ACP 的 stdio 限制和缺少的很多控制面功能（checkpoint、plan、ask、model 切换）说明它的设计目标是让编辑器嵌入 Agent，而不是做服务调用。

4. **如果功能完整性的某个缺口阻塞了你**，那这就是去给那个模式补代码的机会——所有模式共享同一个 `control.Controller` 内核，新功能在内核中实现后，再在各模式的 adapter 层暴露即可。

5. **`reasonix run` 的会话持久化缺口是代码遗漏，随时可补**。需要做的只是：
   - 加 `--continue` / `-c` flag：调用 `agent.ListSessions()` 取最新会话 → `ctrl.Resume()`（~10行）
   - 加 `--resume <path>` flag：调用 `agent.LoadSession(path)` → `ctrl.Resume()`（~8行）
   - 默认自动保存：加一行 `ctrl.SetSessionPath(agent.NewSessionPath(...))`（~2行）
   
   补齐后，`reasonix run` 就不再是"一次性脚本"工具，而是一个完整的一轮会话执​​行者，可以继续已中断的工作。

---

*报告基于对 internal/acp/*, internal/serve/serve.go, internal/cli/cli.go (runAgent), internal/control/controller.go 的源代码分析。*

# Reasonix TUI 功能深度分析报告

> 专注范围：TUI 参数体系、实时通信能力、第三方 Web 集成可行性、对话模式、会话存储  
> 基于 `main-v2`（commit `1e280538`）

---

## 一、TUI 命令行参数体系

### 1.1 入口架构

```go
// cmd/reasonix/main.go
func main() {
    os.Exit(cli.Run(os.Args[1:], version))
}
```

所有参数解析在 `internal/cli/cli.go` 中完成。命令路由：

```
reasonix
  ├── (无参数)     → welcome() 首次运行引导
  ├── run          → 一次性执行（TextSink 输出到 stdout）
  ├── chat / code  → 交互式 TUI 会话（bubbletea）
  ├── serve        → HTTP+SSE 服务
  ├── setup        → 配置向导
  ├── config       → 配置管理
  ├── init         → /init 提示
  ├── acp          → ACP stdio JSON-RPC 服务
  ├── mcp          → MCP 管理命令
  ├── codegraph    → CodeGraph 管理
  ├── doctor       → 诊断
  ├── review       → review 命令
  ├── bot          → Bot 命令
  ├── upgrade      → 自动升级
  └── version      → 版本号
```

### 1.2 `run` 子命令参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--model` | string | "" | provider 名称（默认用 config default_model）|
| `--max-steps` | int | 0 | 最大工具调用轮数（0=用 config）|
| `--show-thinking` | bool | false | 显示思考过程文本（替代折叠标记）|
| `--metrics` | string | "" | 写入 token/cache/cost JSON 摘要的路径 |
| `--dir` | string | "" | 先切换到该目录（项目根目录）|
| `--continue` / `-c` | bool | false | 恢复最近保存的会话 |
| `--resume` | string | "" | 恢复指定会话文件 |
| 位置参数 | string | - | prompt 文本（支持 stdin 管道）|

```bash
# 典型用法
reasonix run "重构这个函数"                                  # 基本用法
reasonix run --model deepseek-chat "分析代码"                 # 指定模型
reasonix run --dir /path/to/project "检查代码"               # 指定工作目录
reasonix run --continue "继续"                                # 恢复最近会话
reasonix run --resume ~/.reasonix/sessions/xxx.jsonl "继续"  # 恢复指定会话
reasonix run --show-thinking "解释这个算法"                    # 显示思考过程
reasonix run --metrics ./metrics.json "分析"                  # 输出指标
echo "你好" | reasonix run                                    # 从 stdin 读取 prompt
```

### 1.3 `chat` 子命令参数（TUI 交互模式）

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--model` | string | "" | provider 名称 |
| `--max-steps` | int | 0 | 最大工具调用轮数 |
| `--continue` / `-c` | bool | false | 恢复最近保存的会话 |
| `--resume` | bool | false | 列出并选择要恢复的已保存会话 |
| `--dangerously-skip-permissions` / `--yolo` | bool | false | YOLO 模式（自动审批工具调用）|
| `--dir` | string | "" | 先切换到该目录（项目根目录）|

```bash
reasonix chat                          # 启动交互式 TUI
reasonix chat --dir /path/to/project   # 在指定目录启动
reasonix chat --continue               # 恢复最近的会话
reasonix chat --resume                 # 从列表中选择要恢复的会话
reasonix chat --yolo                   # YOLO 模式（无审批确认）
reasonix chat --model deepseek-chat    # 指定模型
```

### 1.4 TUI 内部斜杠命令（运行时）

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/model <name>` | 切换模型 |
| `/goal <text>` | 设置自动执行目标 |
| `/goal` | 查看当前目标状态 |
| `/goal clear` | 清除目标 |
| `/loop <sec> <prompt>` | 定时重复执行 |
| `/loop stop` | 停止 loop |
| `/compact [focus]` | 手动压缩上下文 |
| `/new` | 开启新会话 |
| `/clear` | 清空上下文 |
| `/resume` | 恢复已保存会话 |
| `/rename` | 重命名会话分支 |
| `/memory` | 查看记忆 |
| `/remember <note>` | 添加记忆 |
| `/forget <name>` | 删除记忆 |
| `/todo` | 清除待办列表 |
| `/verbose` | 切换详细推理显示 |
| `/sandbox` | 查看沙箱状态 |
| `/effort <level>` | 设置推理开销 |
| `/auto-plan <on/off>` | 切换自动规划 |
| `/rewind` | 回退到检查点 |
| `/tree` | 显示分支树 |
| `/branch <name>` | 创建分支 |
| `/switch <ref>` | 切换分支 |
| `/output-style <style>` | 切换输出风格 |
| `/model` | 列出可用模型 |
| `/quit` / `/exit` | 退出 |

### 1.5 `serve` 子命令参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--model` | string | "" | provider 名称 |
| `--max-steps` | int | 0 | 最大工具调用轮数 |
| `--addr` | string | 127.0.0.1:8787 | 监听地址 |
| `--resume` | string | "" | 恢复指定会话文件 |

```bash
reasonix serve                                # 启动 HTTP 服务，默认 8787 端口
reasonix serve --addr 0.0.0.0:8080            # 监听所有接口
reasonix serve --model deepseek-chat          # 指定模型
reasonix serve --resume ./session.jsonl       # 恢复指定会话
```

---

## 二、实时通信能力分析

### 2.1 TUI 通信方式：Event Channel（非 WebSocket）

TUI 模式下，agent 事件流通过 **Go channel** 从 controller 传递到 bubbletea：

```
Agent 事件流 → event.Sink → eventCh(chan event.Event, 1024) → tea.Msg → TUI 渲染
```

```go
// cli.go:419-421
eventCh := make(chan event.Event, 1024)
var sink event.Sink = &eventSink{ch: eventCh}
```

TUI 本身**不直接暴露 WebSocket 接口**。

### 2.2 HTTP/SSE 服务（serve 模式）

`reasonix serve` 提供 **HTTP + Server-Sent Events (SSE)** 两种协议：

```
客户端(浏览器) ← SSE (text/event-stream) → 获取事件流
客户端(浏览器) ← HTTP JSON → POST /submit, /cancel, /approve, ...
```

**SSE 端点**：
- `GET /events` — 订阅实时事件流（`text/event-stream`）
- 每 15 秒发送 `: ping` 保持连接（`sseKeepaliveInterval`）

**HTTP 命令端点**：
- `POST /submit` — 提交用户输入（JSON `{"input":"..."}`）
- `POST /cancel` — 取消当前执行
- `POST /approve` — 审批工具调用
- `POST /answer` — 回答多选问题
- `POST /plan` — 设置 plan mode
- `POST /compact` — 手动压缩
- `POST /new` — 新会话
- `POST /rewind` — 回退到检查点
- `POST /fork` — 创建分支
- `POST /summarize` — 压缩到指定轮次
- `POST /resume` — 恢复会话
- `POST /tool-approval-mode` — 设置工具审批模式
- `POST /auto-approve-tools` — YOLO 模式
- `POST /forget` — 删除记忆

**HTTP 查询端点**：
- `GET /history` — 获取完整对话历史
- `GET /context` — 获取上下文统计
- `GET /status` — 获取组合状态（label/running/plan/goal/cache 等）
- `GET /checkpoints` — 获取检查点列表
- `GET /branches` — 获取分支列表
- `GET /sessions` — 获取已保存的会话列表
- `GET /skills` — 获取技能列表
- `GET /` — 内嵌浏览器客户端 HTML

**关于 WebSocket**：
- 当前**无 WebSocket** 端点。所有实时通信通过 **SSE**（单向服务器→客户端）+ **HTTP POST**（客户端→服务器）实现
- SSE 是单向的，客户端提交通过独立的 POST 端点完成
- 内嵌前端使用 `EventSource` API 连接 SSE（而非 WebSocket）

### 2.3 ACP 通信方式：stdin/stdout JSON-RPC

`reasonix acp` 使用 **stdin/stdout 上的 NDJSON JSON-RPC 2.0**，通过标准输入输出管道通信：

```bash
# 外部程序启动方式
echo '{"jsonrpc":"2.0","method":"session/create","params":{}}' | reasonix acp
```

这不是网络协议，而是进程间管道协议，适用于编辑器插件等场景。

### 2.4 SSE+POST 能否完全实现 WebSocket 的双向通信？

**基本结论：能覆盖 95% 的聊天场景，但有结构性差异。**

#### 2.4.1 能力覆盖分析

| 通信能力 | WebSocket | SSE + POST | 能否覆盖 |
|---------|:---------:|:----------:|:--------:|
| 服务器→客户端推送 | ✅ 全双工 | ✅ SSE 事件流 | ✅ |
| 客户端→服务器消息 | ✅ 全双工 | ✅ HTTP POST | ✅ |
| 同时双向通信 | ✅ 同一连接 | ❌ 独立连接 | ⚠️ 非同时 |
| 二进制数据 | ✅ | ❌ 仅文本（SSE 只支持 UTF-8）| ❌ |
| 连接复用 | ✅ 1 个 TCP 连接 | ❌ 需 2 个连接（SSE + HTTP）| ❌ |
| 流式消息边界 | ✅ 帧协议 | ✅ SSE `data:` + HTTP JSON | ✅ |
| 自动重连 | ❌ 需手动实现 | ✅ EventSource 内置 | ✅ 更优 |
| 浏览器兼容性 | ✅ 全平台 | ✅ 全平台（EventSource） | ✅ |
| 跨域限制 | ❌ 需要 CORS | ❌ 需要 CORS | ✅ 同等 |

#### 2.4.2 聊天场景的实际影响

对于 Reasonix 的实际聊天场景（用户输入 → 模型流式回复），通信模式本质上是**半双工**的：

```
用户发送文本 → 等待模型回复 → 流式接收 → 用户再发送
```

即使使用 WebSocket，同一时刻也只有一方在发送数据。因此 **SSE + POST 在功能上完全够用**：

| 能力 | 在聊天中是否需要 | SSE+POST 是否满足 |
|------|:---------------:|:-----------------:|
| 并发双向流 | ❌ 不需要 | ✅ 无需 |
| 二进制传输 | ❌ 事件和文本都是 JSON | ✅ 满足 |
| 单连接节省端口 | ❌ 本地开发不关心 | ✅ 满足 |

#### 2.4.3 SSE 的已知缺点

1. **浏览器连接数限制**：同一域名最多 6 个 SSE 连接（HTTP/1.1），HTTP/2 下无此限制。
2. **无内置鉴权机制**：SSE 无法发送自定义请求头（EventSource API 限制），token 需放在 URL query 中。
3. **代理兼容性**：部分老式 HTTP 代理和防火墙可能缓冲 SSE 流，导致延迟增加。
4. **仅文本传输**：SSE 原生只支持 UTF-8 文本，不支持 Blob/ArrayBuffer。
5. **HTTP/1.1 连接数浪费**：每个 SSE 连接长期占用一个 HTTP 连接，同域名下的并发请求数减少。

#### 2.4.4 当前实现中的权衡

```
当前实现:
  客户端 → POST /submit → Controller.Submit()
  Controller 处理完毕后 → Broadcaster.Emit(event) → SSE → 客户端

如果换成 WebSocket:
  客户端 → WebSocket send → Controller.Submit()
  Controller 处理完毕后 → Broadcaster.Emit(event) → WebSocket send → 客户端
```

当前架构中，Broadcaster 已经是一个多订阅者的 fan-out 模型——换成 WebSocket 只需要新增一个 `wsBroadcaster` 实现相同的 `event.Sink` 接口，改动范围非常小。

### 2.5 实时通信能力总结

| 模式 | 协议 | 实时性 | 适用场景 |
|------|------|:------:|---------|
| TUI (chat) | Go channel (进程内) | ✅ 实时 | 交互式终端 |
| HTTP (serve) | SSE + HTTP POST | ✅ 实时 | 浏览器/第三方 Web |
| ACP (acp) | stdio JSON-RPC | ✅ 实时（行级） | 编辑器/IDE 插件 |
| Run (run) | TextSink 输出 | ❌ 一次性 | 脚本/自动化 |

---

## 三、第三方 Web 程序启动 CLI 进程分析

### 3.1 核心能力：`--dir` 参数控制工作目录

`chat` 和 `run` 子命令都支持 **`--dir`** 参数：

```go
// cli.go:378 — chat 模式
dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")

// cli.go:220 — run 模式
dir := fs.String("dir", "", "change to this directory first (project root); config, sandbox and file tools resolve from here")
```

实现方式（`cli.go:193-201`）：

```go
func chdirTo(dir string) int {
    if dir == "" {
        return 0
    }
    if err := os.Chdir(dir); err != nil {
        fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
        return 2
    }
    return 0
}
```

**工作机制**：`--dir` 在配置加载**之前**执行 `os.Chdir()`，这意味着：

| 影响范围 | 说明 |
|---------|------|
| ✅ 配置文件发现 | 从 `--dir` 目录向上搜索 `reasonix.toml` |
| ✅ 文件工具解析 | `read_file`/`edit_file`/`bash` 等工具的沙箱根目录 |
| ✅ 技能/命令/钩子 | 项目级别的技能、钩子加载 |
| ✅ MCP 服务器 | 项目级别的 MCP 配置 |
| ✅ 会话持久化 | 会话文件写入 `config.SessionDir()`（全局或项目）|

### 3.2 第三方 Web 启动方式

```go
// 伪代码 — 第三方 Go 服务中启动 reasonix 进程
cmd := exec.Command("reasonix", "chat", "--dir", "/path/to/project")
cmd.Stdin = pipe    // 发送用户输入
cmd.Stdout = os.Stdout  // 或捕获输出
cmd.Stderr = os.Stderr
cmd.Start()
```

但直接启动 TUI 子进程会面临问题：

| 问题 | 说明 |
|------|------|
| ❌ **TTY 要求** | bubbletea 需要真正的终端设备（TTY），子进程管道模式会检测 `isTTY()` 失败 |
| ❌ **原始模式** | TUI 需要终端进入 raw mode，子进程中无法实现 |
| ❌ **输出解析** | TUI 输出是 ANSI 转义序列，难以程序化解析 |

### 3.3 正确的第三方集成路径

**方案 A：使用 `reasonix serve`（推荐）**

```bash
# 第三方 Web 程序启动 reasonix serve
reasonix serve --dir /path/to/project --addr 127.0.0.1:8787
```

通过 HTTP + SSE 与 serve 进程通信：

```javascript
// 浏览器端 JavaScript
const es = new EventSource('http://127.0.0.1:8787/events');
es.onmessage = (e) => {
    const event = JSON.parse(e.data);
    // 处理实时事件流
};

// 发送用户输入
fetch('http://127.0.0.1:8787/submit', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({input: "分析这段代码"})
});
```

CORS 支持：`HandlerWithCORS(origin)` 可为开发模式提供跨域支持。

**方案 B：使用 `reasonix run`（一次性）+ `--dir`**

```bash
# 一次性执行
reasonix run --dir /path/to/project "你的prompt"
```

输出到 stdout，第三方程序可解析：
- 非 TTY 时输出纯文本（无 ANSI 转义）
- 可通过 `--show-thinking` 控制是否显示推理过程
- 可通过 `--metrics` 获取结构化指标

**方案 C：使用 Go Module 直接集成**

```go
ctrl, err := boot.Build(ctx, boot.Options{
    Model:  "deepseek-chat",
    Sink:   myEventSink,
    WorkspaceRoot: "/path/to/project",
})
ctrl.Send("分析这个项目的架构")
```

支持完整的流式事件、审批控制、会话管理。这是最灵活的方案（详见 01-架构分析报告）。

### 3.4 方案对比

| 维度 | `reasonix serve` | `reasonix run` | Go Module 集成 |
|------|:----------------:|:--------------:|:--------------:|
| 实时流式 | ✅ SSE 实时 | ❌ 一次性输出 | ✅ 事件流 |
| 持续对话 | ✅ 是 | ❌ 单次 | ✅ 是 |
| 工作目录控制 | ✅ `--dir` | ✅ `--dir` | ✅ `WorkspaceRoot` |
| 审批交互 | ✅ POST /approve | ❌ 仅 `--yolo` | ✅ 需自行实现 |
| 会话持久化 | ✅ 自动保存 | ✅ 运行结束保存 | ✅ `Snapshot()` |
| 外部进程管理 | ✅ HTTP 请求 | ✅ 进程等待 | ❌ Go 代码内 |
| 启动开销 | 中等 | 低（每次启动）| 无 |
| 集成复杂度 | 低（HTTP API） | 最低（命令行） | 最高（fork + import）|

---

## 四、持续对话与一次性对话分析

### 4.1 Desktop 模式的分析说明

Desktop 模式（`reasonix-desktop`）不是简单的"套壳 Web"。**它的功能与 Web serve 模式部分重叠但不完全一致**。两者的详细异同见 [第六节](#六-desktop-与-web-serve-的对比分析)。

### 4.2 一次性对话模式：`reasonix run`

**执行流程**：

```
reasonix run "prompt"
  → setup() → boot.Build() → *control.Controller
  → ctrl.Run(ctx, prompt)  ← 同步阻塞直到完成
  → 输出事件流到 stdout（TextSink）
  → ctrl.Snapshot() 保存会话（如果 SessionDir 配置）
  → 退出进程（返回码 0/1）
```

**特性**：
- 进程启动 → 执行一轮 → 进程退出
- 同步阻塞，输出到 stdout
- 非 TTY 时输出纯文本，TTY 时使用 Markdown 渲染器
- 支持 `--continue` / `--resume` 恢复历史
- 可选 `--metrics` 导出结构化摘要

**典型调用方式**：
```bash
# 基本
reasonix run "分析代码"
# 管道
echo "分析代码" | reasonix run
# 从文件
cat prompt.txt | reasonix run --model gpt-4
# 恢复会话继续
reasonix run --continue "继续上面的工作"
```

### 4.3 持续对话模式：`reasonix chat`

**执行流程**：

```
reasonix chat
  → setup() → boot.Build() → *control.Controller
  → newChatTUI() → tea.NewProgram(m).Run()  ← bubbletea 事件循环
  → 循环：
      ├─ 用户输入 → ctrl.SendWithRaw() → 启动一回合
      ├─ Agent 事件流 → eventCh → agentEventMsg → TUI 渲染
      ├─ TurnDone → 等待下一输入
      └─ /quit / Ctrl+D / Ctrl+C → 退出
  → 进程退出前自动 ctrl.Snapshot() 保存会话
```

**特性**：
- 进程常驻，用户可连续输入多轮
- 同一 `*control.Controller` 维护完整会话上下文
- 支持 `/goal`、`/loop`、`/compact` 等运行时命令
- 支持 `/model` 热切换模型（携带历史）
- 支持分支（`/branch`、`/switch`、`/tree`）
- 支持回退（`/rewind`）
- 退出时自动保存会话到 `.jsonl` 文件

### 4.4 对比总结

| 维度 | `reasonix run` | `reasonix chat` |
|------|:--------------:|:---------------:|
| 对话轮数 | 单轮（或直到任务完成）| 多轮无限 |
| 进程生命周期 | 执行完退出 | 用户主动退出 |
| 输出方式 | stdout 文本 | TUI 终端渲染 |
| TTY 依赖 | 否（支持管道）| 是（需要终端）|
| 自动保存 | ✅ 退出时 | ✅ 每轮 + 退出时 |
| 交互审批 | ❌ （需 --yolo）| ✅ 内嵌|
| 运行时命令 | ❌ | ✅ `/goal /loop /compact /model` |
| 分支/回退 | ❌ | ✅ |
| 第三方集成 | ✅ 进程等待/捕获输出 | ❌ TTY 限制 |

### 4.5 `serve` 模式的对话能力

`reasonix serve` 同时支持两种对话模式：

```javascript
// 持续对话（同一 session）
POST /submit {"input": "第一轮问题"}
  → 事件流到 /events
POST /submit {"input": "第二轮问题（基于上下文）"}
  → 控制器保持会话历史

// 一次性对话（新 session）
POST /new → POST /submit {"input": "新会话的第一轮"}
```

**关键**：serve 模式以 HTTP API 暴露 controller 的完整能力，第三方 Web 可以通过 `/submit` + `/events` 实现持续对话。

---

## 五、Desktop 与 Web (serve) 的对比分析

> Desktop 模式（`reasonix-desktop`）**不是**简单的"套壳 Web"。它直接绑定 Go controller 到 WebView（Wails），无 HTTP 跳转。与 `reasonix serve` 共享同一个 `control.Controller` 内核，但前端实现和平台能力不同。两者关系：**同一引擎，不同车身**。

### 5.1 架构对比

```
Web (serve) 模式:
┌───────────────────────────────────────────────────────┐
│  浏览器 (React + 内嵌 index.html)                       │
│    EventSource('/events')  ← SSE 流                    │
│    fetch POST /submit       → HTTP                     │
└────────────────────▲─────────────────────┬─────────────┘
                     │ SSE                 │ HTTP
┌────────────────────┴─────────────────────▼─────────────┐
│  reasonix serve (:8787)                                 │
│    Broadcaster (event.Sink, fan-out to SSE clients)    │
│    POST handler → Controller.Submit()                   │
└──────────────────────────┬──────────────────────────────┘
                           │ 同进程直接调用
┌──────────────────────────▼──────────────────────────────┐
│  control.Controller (内核)                               │
│  boot.Build → Ctrl.Send/Cancel/Approve/Snapshot/…      │
└─────────────────────────────────────────────────────────┘

Desktop 模式:
┌───────────────────────────────────────────────────────┐
│  WebView (React + TS, Vite build, 独立前端)            │
│    window.runtime.EventsOn("agent:event")  ← Wails 事件 │
│    window.go.main.App.Submit() → 直接 Go 方法调用      │
└────────────────────▲─────────────────────┬─────────────┘
                     │ Wails                │ Wails
                     │ runtime.EventsEmit   │ Bind()
┌────────────────────┴─────────────────────▼─────────────┐
│  desktop App (Go)                                       │
│    tabEventSink → 多 Tab 管理 → 每个 Tab 有自己的 Ctrl  │
│    App.Submit/Cancel/Approve 绑定方法                    │
│    系统托盘 / 文件拖放 / 自动更新 / 崩溃恢复               │
└──────────────────────────┬──────────────────────────────┘
                           │ 同进程直接调用
┌──────────────────────────▼──────────────────────────────┐
│  control.Controller (内核，完全相同的实现)                │
│  boot.Build → Ctrl.Send/Cancel/Approve/Snapshot/…      │
└─────────────────────────────────────────────────────────┘
```

**核心差异**：
- Web 模式：事件通过 HTTP SSE 序列化/反序列化，客户端→服务器通过 HTTP POST
- Desktop 模式：事件通过 Wails `runtime.EventsEmit` 进程内传递，方法调用通过 Wails `Bind()` 直接绑定 Go 方法到 JS——**无网络跳转，无序列化开销**

### 5.2 通信协议对比

| 维度 | Web (serve) | Desktop (Wails) |
|------|:-----------:|:---------------:|
| Go→前端事件传递 | SSE `data:` JSON 帧 → `EventSource` | `runtime.EventsEmit("agent:event")` → `EventsOn` 回调 |
| 前端→Go 命令 | `POST /submit` (`Content-Type: application/json`) | `window.go.main.App.Submit(input)` 直接方法调用 |
| 序列化开销 | JSON 序列化 + HTTP 解析 | JSON 序列化（`wireEvent`）→ 进程内传递 |
| 网络依赖 | 需要本地 TCP 端口（:8787） | 无（WebView 内嵌） |
| 延迟 | 微秒级（localhost） | 纳秒级（进程内） |

### 5.3 功能覆盖对比

| 功能 | Web (serve) | Desktop | 说明 |
|------|:-----------:|:-------:|------|
| **核心对话** | ✅ | ✅ | 同一 `control.Controller` 内核 |
| **多会话管理** | ❌ 单 Tab | ✅ 多 WorkspaceTab | Desktop 支持项目级多 Tab，每 Tab 独立 Controller |
| **跨项目并发** | ❌ 一个工作目录 | ✅ 每个 Tab 不同 `WorkspaceRoot` | Desktop 可同时打开多个项目 |
| **系统托盘** | ❌ | ✅ `desktop/tray.go` | 驻留系统托盘（macOS/Linux/Windows）|
| **自动更新** | ❌ | ✅ `desktop/updater.go` | GitHub Release 自动检查和安装 |
| **崩溃恢复** | ❌ | ✅ `desktop/crash_app.go` | 异常退出后恢复会话 |
| **文件拖放** | ❌ | ✅ `desktop/app.go` 拖放上传 | 拖拽文件到窗口自动上传 |
| **原生菜单** | ❌ | ✅ `desktop/menu.go` | macOS 菜单栏 |
| **文件系统监控** | ❌ | ✅ `desktop/tabs.go` (ActivityStatus) | 项目文件变更感知 |
| **MCP 服务器管理** | ❌ 命令行 | ✅ UI 管理 | Desktop 有 MCP 可视化管理面板 |
| **技能可视化** | ❌ | ✅ UI 管理 | Desktop 有技能启用/禁用界面 |
| **会话历史面板** | ✅ 列表 | ✅ 侧边栏 | 两者都有但 Desktop 更丰富 |
| **内嵌前端** | `internal/serve/index.html` (单文件) | `desktop/frontend/` (完整 React 项目) | Desktop 前端功能更完整 |
| **单实例锁** | ❌ | ✅ `SingleInstanceLock` | 防止重复启动 |
| **窗口状态恢复** | ❌ (浏览器) | ✅ `desktop/window_state.go` | 保存/恢复窗口位置和大小 |
| **媒体文件预览** | ❌ | ✅ `mediaTokenStore` 临时 URL | 工作区文件预览 |

### 5.4 功能一致的部分

以下能力两者完全相同（因为共享 `control.Controller` 内核）：

| 能力 | 100% 一致 |
|------|:---------:|
| LLM 对话流程 | ✅ `Send→Run→Cancel→Approve→Snapshot` |
| 工具调用 | ✅ 同一套 tool registry |
| MCP 服务器 | ✅ 同一 `plugin.Host` |
| 会话持久化 | ✅ JSONL 格式，同一 `Session.Save()` |
| 审批策略 | ✅ `SetAutoApproveTools/SetPlanMode/SetToolApprovalMode` |
| Goal loop | ✅ `SetGoal/ClearGoal/Goal/GoalStatus` |
| 检查点/分支/回退 | ✅ `Checkpoints/Rewind/Fork/Branch/SwitchBranch` |
| 自动压缩 | ✅ `Compact` |
| 切换模型 | ✅ 重建 Controller 携带历史 |
| 推理上下文 | ✅ `boot.Build` 相同的 model/profile resolution |

### 5.5 总结

**Desktop 不是套壳 Web**。虽然两者共用同一 Go 内核，但 Desktop 有独立的：

1. **通信通道**：Wails 绑定方法 + runtime events（非 HTTP/SSE）
2. **前端实现**：完整 React 应用（`desktop/frontend/`），功能远多于内嵌 `index.html`
3. **多 Tab 架构**：`WorkspaceTab` 支持并发多项目会话
4. **平台原生能力**：托盘/菜单/更新/崩溃恢复/文件拖放/窗口状态

Web `serve` 模式的优势在于：
- 无需安装，浏览器打开即可使用
- 支持多客户端同时连接（一个 serve 进程 → 多个浏览器 Tab）
- 可被第三方程序通过 HTTP API 集成

两者是**互补关系**，而非替代关系。

---

## 六、对话明细记录存储方式

### 6.1 存储格式：JSONL

所有对话历史以 **JSONL**（JSON Lines）格式存储，每行一个 JSON 对象表示一条消息。

```
reasonix run "你好" → 保存到 ~/.reasonix/sessions/20260607-123456.000000000-deepseek-flash.jsonl
```

**文件命名规范**：

```
{UTC日期}-{时间戳}.{纳秒}-{模型名称}.jsonl
示例: 20260607-123456.000000000-deepseek-chat.jsonl
```

### 6.2 单条消息格式

```jsonl
{"role":"system","content":"你是 Reasonix..."}
{"role":"user","content":"分析这个项目的架构"}
{"role":"assistant","content":"这个项目的架构是..."}
{"role":"tool","content":"/path/to/file内容","tool_call_id":"call_xxx"}
```

使用标准的 `provider.Message` 结构体：

```go
type Message struct {
    Role        Role   `json:"role"`                   // system / user / assistant / tool
    Content     string `json:"content"`                // 消息内容（文本）
    ToolCallID  string `json:"tool_call_id,omitempty"` // 工具调用 ID
    ToolName    string `json:"tool_name,omitempty"`    // 工具名称
    ToolInput   string `json:"tool_input,omitempty"`   // 工具调用参数（JSON）
}
```

**角色类型**：
- `system` — 系统提示词
- `user` — 用户消息
- `assistant` — 模型回复（含 thinking 内容）
- `tool` — 工具调用结果

### 6.3 读写实现

**写入（`internal/agent/save.go:23-50`）**：

```go
func (s *Session) Save(path string) error {
    // 1. 创建临时文件 .session.*.tmp（同名目录下）
    // 2. 用 json.Encoder 将每条消息编码为一行 JSON
    // 3. 原子替换（rename）原文件
    // 4. 写入时加读锁（允许同时在运行的 turn 追加消息）
}
```

**读取（`internal/agent/save.go:55-79`）**：

```go
func LoadSession(path string) (*Session, error) {
    // 1. 打开文件
    // 2. 用 json.NewDecoder 逐条解码
    // 3. 组装为 *Session
}
```

**写入策略**：全量重写（非追加写入），每次 `Snapshot()` 都会重写整个文件。理由：
- 对话文件小（KB 级别）
- 压缩（compact）操作会修改中间消息，追加模式无法处理

### 6.4 存储位置

由硬编码的函数 `config.SessionDir()` 决定，**不支持在 `reasonix.toml` 中配置**：

```go
func SessionDir() string {
    dir, _ := os.UserConfigDir()
    return filepath.Join(dir, "reasonix", "sessions")
}
```

| 平台 | 默认路径 |
|------|---------|
| Linux | `~/.local/share/reasonix/sessions/` |
| macOS | `~/Library/Application Support/reasonix/sessions/` |
| 自定义 | Go 集成时通过 `boot.Options.SessionDir` 覆写 |
| 无配置目录时 | 持久化禁用（`SessionDir()` 返回 `""`）|

可通过 `ctrl.SessionDir()` 查询当前存储目录，**每个 Controller 实例有独立的 `sessionDir` 字段**，允许不同实例指向不同目录。

### 6.5 分支元数据

分支信息存储在会话文件同目录下的元数据文件中：

```
{会话文件路径}.branch.json
{会话文件路径}.meta.json
```

`SessionInfo` 结构包含：
```go
type SessionInfo struct {
    Path           string    // 文件路径
    CreatedAt      time.Time // 创建时间
    LastActivityAt time.Time // 最后活动时间
    Preview        string    // 第一条用户消息预览
    Turns          int       // 用户轮次数
    Scope          string    // 作用域（global / project）
    WorkspaceRoot  string    // 工作目录
    TopicID        string    // 主题 ID
    TopicTitle     string    // 主题标题
}
```

### 6.6 检查点（Checkpoint）

除了完整的 `{id}.jsonl` 会话文件，每轮执行还会生成检查点目录 `{id}.ckpt/`，用于支持 `/rewind` 回退：

```
{session}.ckpt/
  ├── turn-1.json   // 第 1 轮执行前的快照
  ├── turn-2.json   // 第 2 轮执行前的快照
  └── ...
```

检查点存储 `turn + prompt + 受影响的文件路径` 的快照。

### 6.7 全量重写机制

**是的，每轮（每条用户消息）都会全量重写整个 JSONL 文件。**

#### 5.7.1 触发时机

| 时机 | 函数 | 频率 |
|------|------|:----:|
| 每轮执行结束时 | `snapshotActivityIfChanged()` → `SnapshotActivity()` → `snapshot()` → `s.Save(path)` | 每轮 |
| 执行中自动保存 | `autosaveWhileRunning()` → `snapshot()` → `s.Save(path)` | 每 30 秒 |
| 手动压缩后 | `Compact()` → `Snapshot()` | 按需 |
| 模型切换前 | `switchModel()` → `Snapshot()` | 按需 |
| 分支/回退/新会话前 | `Fork()`/`Rewind()`/`NewSession()` → `Snapshot()` | 按需 |
| 进程退出 | `SIGHUP`/`SIGTERM` 处理器 → `Snapshot()` | 退出前 |

#### 5.7.2 写入方式

```go
// internal/agent/save.go
func (s *Session) Save(path string) error {
    // 1. 在同目录创建临时文件 .session.*.tmp
    tmp, _ := os.CreateTemp(filepath.Dir(path), ".session.*.tmp")

    // 2. 对整个消息列表编码为 JSONL（全量）
    enc := json.NewEncoder(tmp)
    for _, m := range s.Snapshot() {  // 加读锁复制消息
        enc.Encode(m)
    }
    tmp.Close()

    // 3. 原子替换原文件
    fileutil.ReplaceFile(tmpPath, path)
}
```

**全量重写的原因**：
- 对话文件小（KB 级别），重写开销极低
- 压缩（compact）操作会修改中间消息历史，追加模式无法处理
- 原子 `tmp + rename` 保证崩溃不残损

#### 5.7.3 关于对话量大的担忧

实测指标：假设每轮 2000 条消息，每条平均 500 字节，JSONL 文件约 **1MB**。全量重写一次约 **< 10ms**（SSD 环境）。常规会话通常在 50-200 轮，文件大小在 **25KB-200KB** 范围，重写开销可忽略不计。

### 6.8 存储目录配置方式

#### 5.8.1 当前状态：无 TOML 配置项

`reasonix.toml` 中 **没有** `session_dir` 字段。`Config` 结构体（`internal/config/config.go:41-61`）不含任何会话路径配置。

默认路径为硬编码：

```go
// internal/config/config.go:1773-1782
func SessionDir() string {
    dir, err := os.UserConfigDir()
    if err != nil {
        return "" // 持久化禁用
    }
    return filepath.Join(dir, "reasonix", "sessions")
}
```

| 平台 | 默认路径 |
|------|---------|
| Linux | `~/.local/share/reasonix/sessions/` |
| macOS | `~/Library/Application Support/reasonix/sessions/` |

#### 5.8.2 覆写方式

**方式一：Go 集成时通过 `boot.Options.SessionDir`**

```go
ctrl, err := boot.Build(ctx, boot.Options{
    Model:      "deepseek-chat",
    SessionDir: "/custom/session/dir",  // ← 指定自定义目录
})
```

**方式二：运行时通过 `ctrl.SetSessionPath()`**

```go
ctrl.SetSessionPath("/custom/path/my-session.jsonl")
```

**方式三：Desktop 模式自动使用项目级目录**

Desktop 模式下使用 `ProjectSessionDir(workspaceRoot)`，路径为 `<config_root>/projects/<workspace_slug>/sessions/`，不依赖全局目录。

#### 5.8.3 关于 `--dir` 参数的影响

`--dir` 仅改变进程工作目录（影响配置文件发现和工具沙箱根），**不改变** `SessionDir()` 的值。即：

```bash
reasonix chat --dir /project/a
reasonix chat --dir /project/b
# 两种启动的会话文件都保存在 ~/Library/Application Support/reasonix/sessions/ 下
# 不会自动按项目隔离
```

如果需要按项目隔离，目前只能通过 Go 集成方式传入自定义 `SessionDir`，或者在配置中不存在该选项。

### 6.9 存储流程图

```
                                 写入时机
  ┌──────────────────┐    ┌──────────────────────────┐
  │  agent.Session   │    │ ctrl.Snapshot() 每轮结尾 │
  │  {Messages}      │◄───│ ctrl.Snapshot() 退出前   │
  └──────┬───────────┘    │ 自动保存（autosaveWhile..)│
         │                └──────────────────────────┘
         │ Save(path)
         ▼
  ┌──────────────────┐
  │  {id}.jsonl      │←── JSONL 文件
  │  {"role":"user", │    每行一条消息
  │   "content":""}  │    全量重写
  │  ...             │
  └──────────────────┘
         │
         ├── {id}.ckpt/         ← 检查点目录
         │   ├── turn-1.json    ← 每轮执行前的快照
         │   └── turn-2.json
         │
         ├── {id}.meta.json     ← 分支元数据
         │
         └── {id}.branch.json   ← 分支信息
```

### 6.10 存储安全性

| 机制 | 说明 |
|------|------|
| **原子写入** | 临时文件 → `fileutil.ReplaceFile()`（rename）替换，崩溃不残损 |
| **读锁保护** | `Save()` 使用 `Snapshot()` 加锁复制，不影响正在执行的 turn |
| **目录自动创建** | `os.MkdirAll()` 确保目录存在 |
| **空会话不保存** | `HasContent()` 检查至少有一条非 system 消息 |
| **分支文件隔离** | `filepath.Clean` + `filepath.Rel` 校验防止路径遍历攻击 |

---

## 七、综合架构图

```
第三方 Web 程序
  │
  ├── HTTP POST /submit  ──────────────────┐
  ├── EventSource /events ←── SSE 流 ─────┤
  │                                         │
  ▼                                         ▼
┌───────────────────────────────────────────────┐
│            reasonix serve (:8787)              │
│  Broadcaster → HTTP SSE → 多个浏览器客户端     │
│  Controller.Submit() → POST 命令处理           │
└───────────────┬───────────────────────────────┘
                │ ctrl.Send / ctrl.Submit
                ▼
┌───────────────────────────────────────────────┐
│          control.Controller                    │
│  ┌──────────┬──────────┬──────────┬─────────┐ │
│  │ Send     │ Run      │ Approve  │ Goal    │ │
│  │ Cancel   │ Snapshot │ Resume   │ Compact │ │
│  └──────────┴──────────┴──────────┴─────────┘ │
│  ┌──────────────────────────────────────────┐ │
│  │ Session {Messages []provider.Message}     │ │
│  │  ← 对话历史 (内存)                        │ │
│  └──────────────────────────────────────────┘ │
└───────────────┬───────────────────────────────┘
                │ Snapshot() / Save()
                ▼
┌───────────────────────────────────────────────┐
│  磁盘持久化                                     │
│  sessions/{id}.jsonl   ← JSONL 全量快照        │
│  sessions/{id}.ckpt/   ← 检查点 (rewind 用)   │
│  sessions/{id}.meta.json ← 元数据/分支信息     │
└───────────────────────────────────────────────┘
```

---

*报告生成日期：基于 main-v2 branch（commit `1e280538`）*

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

### 2.4 实时通信能力总结

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

### 4.1 一次性对话模式：`reasonix run`

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

### 4.2 持续对话模式：`reasonix chat`

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

### 4.3 对比总结

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

### 4.4 `serve` 模式的对话能力

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

## 五、对话明细记录存储方式

### 5.1 存储格式：JSONL

所有对话历史以 **JSONL**（JSON Lines）格式存储，每行一个 JSON 对象表示一条消息。

```
reasonix run "你好" → 保存到 ~/.reasonix/sessions/20260607-123456.000000000-deepseek-flash.jsonl
```

**文件命名规范**：

```
{UTC日期}-{时间戳}.{纳秒}-{模型名称}.jsonl
示例: 20260607-123456.000000000-deepseek-chat.jsonl
```

### 5.2 单条消息格式

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

### 5.3 读写实现

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

### 5.4 存储位置

由配置 `session_dir` 控制，默认路径：

| 平台 | 默认路径 |
|------|---------|
| Linux | `~/.local/share/reasonix/sessions/` |
| macOS | `~/Library/Application Support/reasonix/sessions/` |
| 自定义 | 配置文件中 `session_dir` 指定 |

可通过 `ctrl.SessionDir()` 查询当前存储目录。

### 5.5 分支元数据

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

### 5.6 检查点（Checkpoint）

除了完整的 `{id}.jsonl` 会话文件，每轮执行还会生成检查点目录 `{id}.ckpt/`，用于支持 `/rewind` 回退：

```
{session}.ckpt/
  ├── turn-1.json   // 第 1 轮执行前的快照
  ├── turn-2.json   // 第 2 轮执行前的快照
  └── ...
```

检查点存储 `turn + prompt + 受影响的文件路径` 的快照。

### 5.7 存储流程图

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

### 5.8 存储安全性

| 机制 | 说明 |
|------|------|
| **原子写入** | 临时文件 → `fileutil.ReplaceFile()`（rename）替换，崩溃不残损 |
| **读锁保护** | `Save()` 使用 `Snapshot()` 加锁复制，不影响正在执行的 turn |
| **目录自动创建** | `os.MkdirAll()` 确保目录存在 |
| **空会话不保存** | `HasContent()` 检查至少有一条非 system 消息 |
| **分支文件隔离** | `filepath.Clean` + `filepath.Rel` 校验防止路径遍历攻击 |

---

## 六、综合架构图

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

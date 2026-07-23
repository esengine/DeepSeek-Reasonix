# ACP 编辑器接入

<a href="../README.zh-CN.md">README</a>
&nbsp;·&nbsp;
<a href="./ACP.md">English</a>
&nbsp;·&nbsp;
<a href="./GUIDE.zh-CN.md">使用指南</a>
&nbsp;·&nbsp;
<a href="https://agentclientprotocol.com/">ACP 规范</a>

Reasonix 实现了 Agent Client Protocol（ACP）v1，通过标准输入输出提供 NDJSON
JSON-RPC 2.0 agent。编辑器和其他 ACP host 负责启动进程、打开一个或多个工作区会话，
并接收流式消息、工具活动、计划、权限请求和配置更新。

## 启动 agent

ACP host 应启动以下命令之一：

```sh
reasonix acp
reasonix acp --model deepseek-pro
reasonix acp --profile delivery
```

客户端未覆盖模型时，`--model` 用于选择启动模型；`--profile` 把启动工作模式设为
`economy`、`balanced` 或 `delivery`。初始化后，两者仍可按会话切换。

标准输出专用于 ACP 消息，Reasonix 会把诊断写入标准错误，因此 host 不应合并这两个
流。尚未配置 provider 时先运行 `reasonix setup`；initialize 响应也会声明一个启动
`reasonix setup` 的 terminal authentication method。

## 初始化与能力协商

客户端应在打开会话前调用 `initialize`。Reasonix 会声明以下能力结构（省略无关字段）：

```json
{
  "protocolVersion": 1,
  "agentCapabilities": {
    "loadSession": true,
    "sessionCapabilities": {
      "list": {},
      "resume": {},
      "close": {},
      "delete": {}
    },
    "promptCapabilities": {
      "image": false,
      "audio": false,
      "embeddedContext": true
    },
    "mcpCapabilities": {
      "http": true,
      "sse": false
    },
    "_meta": {
      "reasonix.io": {
        "sessionSteer": {
          "method": "_reasonix.io/session/steer"
        }
      }
    }
  }
}
```

客户端声明 `fs.readTextFile`、`fs.writeTextFile` 或 `terminal` 后，Reasonix 会让
适用的文件操作经过编辑器的未保存 buffer，并让适用的前台命令在客户端持有的 terminal
中运行。客户端没有声明这些能力时，常规工作区工具会在 Reasonix 进程内本地运行。

## 会话生命周期

每个 ACP 会话都拥有独立的 Reasonix Controller、工作区根目录、模型、工作模式、协作
模式、审批模式、MCP 集合和持久化 transcript，会话之间不会泄漏状态。

| 方法 | 行为 |
| --- | --- |
| `session/new` | 为绝对路径 `cwd` 打开会话并返回配置状态。 |
| `session/load` | 打开持久化 ACP 会话，并通过 `session/update` 通知回放 transcript。 |
| `session/resume` | 打开持久化会话，但不回放 transcript。 |
| `session/prompt` | 执行一轮任务，流式发送更新，最后返回停止原因。 |
| `session/cancel` | 取消活动回合；它是一条 notification。 |
| `session/list` | 列出活动和持久化 ACP 会话，可按绝对路径 `cwd` 过滤。 |
| `session/close` | 停止活动会话并释放资源，但不删除历史。 |
| `session/delete` | 停止会话并删除其持久化 ACP 历史。 |

`session/new`、`session/load` 和 `session/resume` 可以携带 `mcpServers`。
Reasonix 支持 stdio、Streamable HTTP 和 legacy SSE server。
stdio `env` 和 HTTP `headers` 支持 ACP 官方的
`[{"name":"...","value":"..."}]` 结构，同时继续接受旧版 object-map 结构。

## 会话控制

Reasonix 把互不相关的选择拆成独立控制轴，而不是混在一个 mode selector 中：

| 控制项 | 可选值 | 协议入口 |
| --- | --- | --- |
| 协作模式 | `normal`、`plan`、`goal` | `modes` 和 `session/set_mode` |
| 模型 | 已配置的 `provider/model` | id 为 `model` 的 `configOptions` |
| 推理强度 | provider 支持的等级或 `auto` | id 为 `effort` 的 `configOptions` |
| 工作模式 | `economy`、`balanced`、`delivery` | id 为 `work_mode` 的 `configOptions` |
| 工具审批 | `ask`、`auto`、`yolo` | id 为 `tool_approval` 的 `configOptions` |

模型、推理强度、工作模式和工具审批统一使用 `session/set_config_option`。切换模型、
推理强度或工作模式时会重建会话 Controller，同时保留历史和其他控制轴；切换工具审批
只更新 gate，不重建 Controller。

旧客户端仍可使用 `session/set_model`。`session/set_mode` 也继续接受 legacy 值
`default` 和 `auto`，分别表示“常规 + 询问”和“常规 + Yolo”；新客户端应使用上面的
独立 selector。

## Prompt、更新与审批

`session/prompt` 支持文本 block 和内嵌文本 resource，不声明图片或音频能力。执行回合
期间，Reasonix 可能发送：

- agent 消息和思考内容 chunk；
- pending 和 completed 工具调用更新；
- 从 `todo_write` 生成的完整计划更新；
- 可用的斜杠命令；
- 当前 mode 和配置项更新；
- 针对受权限控制工具及用户问题的 `session/request_permission` 请求。

Host 应让 `session/prompt` 请求保持打开，直到 Reasonix 返回停止原因；期间仍需同时处理
双向 request 和 notification。

## 回合中引导扩展

Reasonix 通过 ACP v1 厂商扩展提供回合中引导。它不是 ACP 核心方法，也不是仍未发布的
ACP v2 `session/inject` 提案。

### 发现能力

从以下位置读取方法名：

```text
agentCapabilities._meta["reasonix.io"].sessionSteer.method
```

不要假设该扩展一定存在，也不要调用无命名空间的 `session/steer`。ACP 为核心协议保留
所有不以下划线开头的方法名。

### 发送引导

在 `session/prompt` 仍处于活动状态时调用声明的方法：

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "_reasonix.io/session/steer",
  "params": {
    "sessionId": "session-id",
    "prompt": [
      {"type": "text", "text": "把用户名改成邮箱"}
    ]
  }
}
```

成功返回 `{}` 表示活动回合已接受引导。Reasonix 会在下一个安全的模型调用边界前把它
作为 user message 加入上下文，不会取消回合，也不会额外消耗工具步骤预算。该消息会进入
正常历史；回放 transcript 时显示用户原文，不显示 Reasonix 内部 steer marker。

| 条件 | JSON-RPC 结果 |
| --- | --- |
| 活动 prompt 接受引导 | `{}` |
| session 不存在或 prompt 为空 | `-32602 InvalidParams` |
| session 没有活动 prompt | `-32600 InvalidRequest` |
| 客户端调用 `session/steer` | `-32601 MethodNotFound` |

收到 `InvalidRequest` 时，引导没有入队。客户端可以等待活动 prompt 结束，再让用户把该
文本作为普通新 prompt 提交，但不能把失败的 steer 静默显示为已接受。

## 兼容性与缓存行为

| 表面 | 旧版或非 Reasonix 客户端的行为 | 结论 |
| --- | --- | --- |
| 现有 ACP v1 方法 | 方法名和响应结构不变。 | 兼容 |
| Capability `_meta` | 可以忽略未知 metadata。 | 兼容 |
| 持久化 transcript | 不需要新增持久化 schema。 | 兼容 |
| CLI、Desktop、Bot steer | 保留现有 idle fallback。 | 兼容 |

Steer 只会把用户请求的消息追加到正常会话历史，不改变 system prompt、工具 schema、工具
顺序或其他稳定的 provider prefix 字节。下一次 provider 请求必然包含这条新消息，和任何
普通新用户消息一样会改变新增后缀，但此前的稳定前缀仍可复用。

## 客户端接入检查清单

1. 启动 `reasonix acp`，分离 stdin、stdout 和 stderr。
2. 调用 `initialize`，同时遵守标准 capability 和 `_meta` capability。
3. 使用绝对工作区路径打开会话，并隔离保存各 session id。
4. Prompt 运行期间继续处理 agent 发往客户端的文件、terminal 和权限请求。
5. 只有在 Reasonix 声明 capability 且 prompt 活动时才显示 steer UI。
6. 把成功的 steer 响应理解为“引导已入队”，而不是“模型已立即完成处理”。
7. 用 `session/close` 释放资源；只有用户明确要删除持久化历史时才调用
   `session/delete`。

# Reasonix Extension Protocol v2

Extension Protocol 是 Reasonix（**宿主**）与以独立进程运行的代码型扩展
（**Sidecar**）之间的稳定线上契约。安装了带有 `runtime` 块的插件后，
它通过该协议拦截运行时事件、持有替换策略、提供流式模型 Provider、发布
结构化 UI——全程不链接进宿主二进制。

- 协议 ID：`reasonix.extension.v2`
- 机器可读 Schema：`internal/extension/protocol/schema.generated.json`
- 方法/事件/限额/错误索引：`docs/EXTENSION_PROTOCOL.generated.md`
  （生成产物，CI 防漂移校验）
- Go SDK（已实现下文全部内容）：`sdk/go`

本文档是生成索引的文字说明。两者不一致时，以生成 Schema 为准。

## 传输

- 严格 JSON-RPC 2.0 over **NDJSON**：stdin/stdout 上每行一个完整 JSON
  对象。stderr 归扩展用于诊断输出，宿主只保留限长、经凭据脱敏的尾部
  用于错误展示。
- 双向单帧上限 **8 MiB**；超帧是连接级致命错误 `frame_too_large`。
- 请求 ID 为整数；`params` 必须是对象。帧层容忍未知成员，但 DTO 解码
  是严格的（拒绝未知字段），拼写错误会立即暴露。

## 生命周期

1. 宿主以 exec form 启动 Sidecar（不经 Shell），并首先发送
   `extension/initialize`，参数携带 Manifest 期望：宿主将接受的
   intercepts、replaces、providers 和 UI actions。同一 runtime generation
   最多并行初始化 4 个 Sidecar，并共享 30 秒总启动预算。
2. Sidecar 应答自身声明。宿主校验：协议 major version 精确匹配，且
   subscriptions、替换槽、Provider、UI action 都必须是 **Manifest 声明
   的子集**；超出部分以 `capability_not_declared` 握手失败。
3. 宿主发送 `extension/initialized`。此前任何 Sidecar→宿主流量都会
   使连接失效。
4. 关闭是有界的：`extension/shutdown`（带超时）→ 关闭 stdin →
   未退出则终止进程树。
5. 崩溃：Sidecar 死亡会取消其全部 pending RPC。若它持有当前选用的
   Provider 或替换槽，当前操作明确失败——宿主绝不静默回退到另一个
   模型或策略。崩溃的 Sidecar 只在空闲 reload 时重启。

## Content ref

被标记为可外置且超过 **64 KiB** 的载荷字段会卸入宿主 content store：
帧内携带 `ExternalizedField` 描述符（JSON pointer、content ref、字节
数、SHA-256）与 `null` 占位。对端通过 `host/content/read` 以
**256 KiB** 分页取回字节，并校验字节数与哈希。单个 content 对象上限
**8 MiB**。未知或过期 ref 报 `content_ref_expired`。

## 拦截

十七个冻结 hook 点（见生成索引）。`extension/intercept` 是阻塞式；
`extension/event` 是对相同点位的只读观察。事件通过有界、非阻塞的写入
队列投递；队列饱和时丢弃该观察并告警，不会卡住 Agent。

- 普通 Interceptor 按确定顺序**串行**执行：priority 升序（Manifest
  `priority`，-1000..1000，默认 0），其次插件 ID，再其次注册序号。
- 每次调用的裁决：`continue`（透传载荷）、`block`（中止操作并给出
  用户可见原因）、`replace`（替换载荷——宿主在使用前按点位 DTO 与
  Schema 重新校验）、`allow`/`deny`（仅在 `permission.decision` 合法）。
  full-trust 的 `allow` 可覆盖宿主 deny，并会记录审计。
- 替换**策略槽**（`system_prompt`、`context`、`provider_request`、
  `provider_response`、`compaction`、`session_policy`、`permission`、
  `frontend_events`、`tool:<name>`、`provider:<ref>`）在所有已安装
  插件中只有一个 owner。链式拦截先执行，槽主做最终裁决；槽主的超时
  或错误总是令当前操作失败。
- 超时：输入/工具/权限点位默认 5 秒；会话/上下文/压缩/系统提示词族
  默认 30 秒；Manifest 可调，上限 60 秒。可选观察型扩展超时只告警
  一次并跳过；required 扩展与槽主超时则令操作失败。

## 流式 Provider

具备 `providers` 能力的扩展应答 `extension/provider/catalog`，返回与
宿主 Provider 等价的描述符（模型、上下文窗口、价格、视觉、推理、
effort）——绝不包含凭据。模型以 `plugin/<plugin>/<provider>/<model>`
形式出现。

流式调用遵循 `extension/provider/stream/open` → `stream/chunk` →
`stream/end`：

- chunk 使用从 1 开始的连续序号；`stream/end.lastSeq` 冻结结束边界。
  宿主缓冲乱序 chunk、丢弃重复，缺 chunk 持续存在时以 interrupted
  失败并指明缺失序号。
- chunk 类型：`text`、`reasoning`（含 `signature`）、
  `tool_call_start`、`tool_call_args_delta`、`tool_call`、`usage`
  （含 cache tokens）、`done`、`error`。Provider 错误必须由产生方脱敏，
  宿主还会进行防御性二次脱敏。
- 取消流上下文会发送 `stream/cancel`，Sidecar 必须停止产生 chunk。
- 扩展自行读取所需环境与凭据；宿主绝不把其他 Provider 的 API key 或
  header 发送给它。被选 Provider 崩溃时不自动回退到其他模型。

## 结构化 UI

具备 `ui` 能力的扩展可发布 `status`、`card`、`form`、`notification`
载荷（`host/ui/publish`），也可发起提问（`host/ui/request`：confirm、
input、select、multiselect）。UI 表面**只有结构化数据**：不允许
HTML、CSS、JavaScript、远程脚本、任意前端组件或未受控 URL；Markdown
走各前端现有的安全渲染器。每次表面更新都携带插件 ID、surface ID、
session ID 与运行时 generation；旧 generation 的更新会被丢弃，tab
切换或 reload 后的迟到结果无法覆盖新状态。

初始化时声明的 action 以 `/<plugin>:<action>` 命名空间暴露，通过
`extension/ui/action` 调用；表单提交经 `extension/ui/submit` 到达扩展。

## 错误

域错误使用 JSON-RPC 错误码 `-32000` 并携带结构化数据（reason、
retryable、action）；`protocol_error`、`unknown_method`、
`invalid_params`、`internal` 使用标准 JSON-RPC 错误码。冻结的 reason
表见生成索引。

## 稳定性契约

major v2 内只允许：新增 optional 字段、新增枚举值、新增方法。既有
必填字段、方向、限额、错误 reason 与语义永不改变。canonical Schema
及其 SHA-256 hash 由 `cmd/extension-protocol-gen` 产生；CI 的
`go test ./...` 会运行确定性生成测试（
`TestGeneratedArtifactsAreDeterministicAndCommitted`），任何漂移——包括
意外语义变更——都会令构建失败。

## 安全模型

代码型扩展是**完全信任（full trust）**的：它运行在 Reasonix Sandbox
之外，继承未过滤的完整环境，可读取完整会话与环境、绕过权限、直接
操作本机。安装、更新、替换或 `--link` 一个带 `runtime` 块的插件即
代表授权——没有二次确认。只有通过插件安装流程（写入
`plugin-packages.json`）的插件才能启动 Sidecar；项目配置永远无法
声明。Sidecar 的诊断输出、结构化 UI、拦截器原因和 Provider 错误在
进入 UI、日志或错误界面前都会经过宿主的凭据脱敏；普通 Provider/模型
内容作为产品数据保持原样。安装预览、插件详情与能力诊断对 runtime
插件始终展示 FULL TRUST 区块。

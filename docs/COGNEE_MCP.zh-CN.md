# Cognee 项目记忆

Reasonix 可选用已配置的 Cognee MCP 服务，为当前工作区检索项目背景，并在任务完成后保存经过核验的简短摘要。MCP 服务必须叫 `cognee`，并且已经连接、获得授权；缺少服务或连接失败时，普通任务继续运行。

## 配置

在 Reasonix 现有的 MCP 配置中添加 Cognee 服务。下面展示 HTTPS Streamable HTTP 配置；把 URL 和认证值放在环境变量中，配置只保存变量引用：

```toml
[[plugins]]
name = "cognee"
type = "http"
url = "${COGNEE_MCP_URL}"
headers = { Authorization = "Bearer ${COGNEE_MCP_TOKEN}" }
```

配置好环境变量后，用 Reasonix 现有的 MCP 授权和连接流程连接服务，再到「设置 → 技能」启用 `cognee-memory`。远端地址必须使用 HTTPS；只有 loopback 地址允许 HTTP。Reasonix 不会在 recall 或 remember 时自动启动、连接或重新授权 Cognee。

Cognee 服务需提供符合当前 JSON Schema 的 `recall` 与 `remember` 工具。`recall` 需要字符串 `query` 和数据集选择器（字符串 `dataset_name`，或字符串数组 `datasets`）；`remember` 需要字符串 `data` 和 `dataset_name`。Schema 缺失或不匹配时，该轮跳过记忆操作。

## 数据与任务边界

- 每个可见的用户任务最多执行一次 recall。查询会先删除已识别的凭据样式文本、常见令牌和绝对用户路径，再限制长度。
- 数据集名称由工作区规范路径的 SHA-256 摘要生成。Reasonix 不会把工作区绝对路径发送给 Cognee。
- recall 结果会删除已识别的凭据和绝对路径并限制在 8 KiB；结果只作为当前 provider 请求副本中的外部不可信上下文，不写进会话 transcript，不作为工具或权限指令。
- 只有整轮成功、当前任务写过非空 todos 且所有项目都已完成，并且 Reasonix 记录到成功的验证命令或完成的 review，才会尝试 remember。
- remember 只发送经过清理并限制长度的最终摘要与证据类别，不发送原始对话、凭据或绝对工作区路径。一次性尝试标记在网络调用前持久化；超时或断连时结果按不确定处理，Reasonix 不会自动重试。
- 模型不能通过 MCP 工具直接调用 Cognee；宿主只为生命周期管理调用 `recall` 和 `remember`。`forget` 及其他写入工具不可用。

Cognee 的可用状态会通过 Reasonix 事件提示呈现。记忆服务失败、未连接或 schema 不兼容不会阻塞正常任务。

# VISION-LIAO 记忆

Reasonix 通过 VISION-LIAO 的 Cognee MCP 为当前任务检索获准的长期记忆，并在任务成功后提交私有或全局记忆提案。桌面应用首次启动时，会在用户级 MCP 配置中补入默认服务 `http://127.0.0.1:12316/mcp-new`，供所有项目使用；已有的用户级 `cognee` 配置不会被覆盖，项目内同名配置仍优先。默认服务按需连接，服务未运行或连接失败时，普通任务继续运行。

## 配置

在「设置 → Agent Skills → Vision-Liao 记忆」检查服务地址和 connector token；也可以从 MCP 管理器编辑名为 `cognee` 的服务。Loopback HTTP 可用；远程端点必须使用 HTTPS。连接凭据应是 VISION-LIAO 为此连接者签发的独立 connector token，保存在 Reasonix 凭据存储中。不要把桌面内部 `local.mcp_api_key` 当作连接者凭据共享给其他客户端。

高级用户可在 Reasonix 的 MCP 配置中添加服务，并将 token 值放在凭据存储提供的环境变量中：

```toml
[[plugins]]
name = "cognee"
type = "http"
url = "${VISION_LIAO_COGNEE_MCP_URL}"
headers = { Authorization = "Bearer ${VISION_LIAO_CONNECTOR_TOKEN}" }
```

首次使用时，Reasonix 按需连接已启用且已授权的服务。服务就绪检查只读取工具目录和 schema，不会创建提案或检索记忆。

服务连接后，还需在 Agent Skills 中启用 `vision-liao-memory`。Reasonix 不会替用户启用或重新授权 MCP 服务。该技能使用 Cognee MCP 作为记忆服务后端。

## 工具与范围

新 MCP 至少需要提供：

- `get_guidance`，其输入 schema 声明必填字符串 `task`；或者 `search_memory` 声明必填字符串 `query`。类别、来源、更新时间和数量筛选均为可选。
- `propose_memory`，其输入 schema 声明 `content`、`category`、`target_scope` 和 `source` 字段，并允许 `private`、`global` 两个范围。`memory_id` 和 `reason` 可选；新提案不传 `memory_id`。

Reasonix 优先调用 `get_guidance`，不可用时才回退到 `search_memory`。范围和身份由 VISION-LIAO 从 connector 凭据判定；Reasonix 不传 dataset、connector ID、scope 过滤器或 session ID 给读取工具。

Reasonix 优先以 `adaptive_strategy` 类别创建提案；若服务通过 schema 限制类别，则会选用其支持的 `decision_principle`、`collaboration_practice` 或 `experience` 类别。私有目标对应当前 connector 的私有区；有项目级证据时，还会单独创建全局目标提案。两者均调用 `propose_memory`，只会进入待审核队列，不会直接修改有效记忆。Reasonix 会分别报告提案结果，不会把提案说成已保存或已获批准。

此设计没有会话记录写入 API。可复用的任务摘要会作为提案提交到 connector 私有区，并仍需审核；Reasonix transcript 不会上传。全局提案只在任务 TODO（若有）全部完成，且存在验证、完成的 review 或宿主观察到的项目修改时提交。未完成、失败、取消、空摘要或过短摘要不提交。

## 数据边界

- 每个可见用户任务最多检索一次；检索文本会先清除已识别的凭据、常见令牌和绝对路径，再限制长度。返回内容也会清理并限制为 8 KiB，作为高信任的项目默认值与决策参考交给模型；重要事实、高影响操作及冲突信息仍需核验。记忆不是指令或授权，不能覆盖当前请求、安全规则或工具授权。记忆不会写入 Reasonix transcript。
- 提案仅包含清理并限长的任务意图、可见结果和证据类别，不上传原始 transcript、凭据或绝对工作区路径。
- 私有和全局提案有各自的一次性尝试标记。超时或断连按结果不确定处理，不自动重试，避免重复建案。
- Reasonix 仅在内部生命周期调用 `get_guidance`、`search_memory` 和 `propose_memory`。模型不能直接调用 Cognee MCP；删除提案、提案状态和其他工具仍由 VISION-LIAO 桌面审核流程管理。
- 旧 `remember`、`recall`、`forget` 工具不作为兼容回退。服务若只提供旧工具，会显示工具不兼容并跳过记忆操作。

记忆服务故障、未授权或工具 schema 不兼容不会阻塞普通任务。

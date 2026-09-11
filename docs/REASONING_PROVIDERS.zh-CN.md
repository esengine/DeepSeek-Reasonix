# 按提供商划分的推理控制

<a href="./GUIDE.zh-CN.md">使用指南</a>
&nbsp;·&nbsp;
<a href="./REASONING_PROVIDERS.md">English</a>

Reasonix 只暴露一个 `/effort` 开关（以及 provider 级的 `effort` / `thinking`
配置字段），但 OpenAI-compatible 后端对*如何*在线上请求思维链（chain-of-thought）
存在分歧。`openai` provider 会按后端调整请求形态；下表是参考依据，说明每个已知
后端使用哪种协议、会采纳或忽略哪些参数。

## 自动识别的后端

这些后端按 Base URL 识别（见 `internal/provider/openai/host.go`），并自动获得
定制的请求形态——无需额外配置。

| Provider          | Base URL                                                    | 推理控制                                     | `/effort` 档位                           | 备注 |
|-------------------|-------------------------------------------------------------|----------------------------------------------|------------------------------------------|-------|
| DeepSeek V4 Flash | `api.deepseek.com`、`*.deepseek.com`                        | `thinking.type` + `reasoning_effort`（深度） | `auto`、`disabled`、`low`、`high`、`max` | 默认开启思考；`disabled` 通过 `thinking.type=disabled` 关闭。兼容性输入 `medium` 归一化为 `high`，`xhigh` 归一化为 `high`。请求携带 tools 时，历史 assistant 轮次只要携带 reasoning 都会回传，即使该轮没有工具调用；不带 tools 时该字段会被 DeepSeek 忽略。 |
| DeepSeek V4 Pro   | `api.deepseek.com`、`*.deepseek.com`                        | `thinking.type` + `reasoning_effort`（深度） | `auto`、`disabled`、`low`、`high`、`max` | 默认开启思考；`disabled` 通过 `thinking.type=disabled` 关闭。兼容性输入 `medium`、`xhigh` 归一化为 `high`。请求携带 tools 时，历史 assistant 轮次只要携带 reasoning 都会回传，即使该轮没有工具调用；不带 tools 时该字段会被 DeepSeek 忽略。 |
| MiniMax M3        | `api.minimaxi.com`、`*.minimaxi.com`                        | `thinking.type`（`adaptive`\|`disabled`）    | `auto`、`adaptive`、`disabled`           | 无深度档位；`reasoning_effort` 会被省略。 |
| Zhipu GLM         | `open.bigmodel.cn` / `*.bigmodel.cn`、`api.z.ai` / `*.z.ai` | `thinking.type`（`enabled`\|`disabled`）     | `auto`、`enabled`、`disabled`            | **端点会静默忽略 `reasoning_effort`**，因此推理完全由 `thinking.type` 驱动。 |

## 显式的逐模型档位

| Provider/模型              | Base URL                                   | 推理控制                                      | `/effort` 档位                | 备注 |
|----------------------------|--------------------------------------------|-----------------------------------------------|-------------------------------|-------|
| Kimi CN/Global `kimi-k3`   | `api.moonshot.cn/v1`、`api.moonshot.ai/v1` | `reasoning_effort`                            | `low`、`high`、`max`          | 始终思考；默认 `max`。Reasonix 会回放完整的 assistant 消息、使用 `max_completion_tokens`，并省略 K3 固定的采样字段。 |
| 自定义 Kimi K3 网关        | 任意 OpenAI-compatible K3 端点             | `reasoning_effort`                            | `low`、`high`、`max`          | 设置 `reasoning_protocol = "kimi-k3"`，显式启用 K3 的完整消息回放与请求形态。 |
| OpenCode Go `kimi-k3`      | `opencode.ai/zen/go/v1`                    | `reasoning_effort`                            | `high`、`max`                 | 中转站专属档位；默认 `max`，并保留中转站标准的 OpenAI-compatible 请求形态。 |
| Token Rhythm DeepSeek V4   | `tokenrhythm.studio/v1`                    | DeepSeek `thinking.type` + `reasoning_effort` | 模型专属的 DeepSeek 档位      | 通过预设的模型覆盖选择，与网关主机无关。 |
| Token Rhythm GLM 5/5.1/5.2 | `tokenrhythm.studio/v1`                    | GLM `thinking.type`（`enabled`\|`disabled`）  | `auto`、`enabled`、`disabled` | 通过预设的模型覆盖选择；`reasoning_effort` 会被省略。 |

在 Token Rhythm 端点上，精确的 GLM 模型 ID（`glm-5`、`glm-5.1` 和 `glm-5.2`）
会自动选择官方的 GLM 请求形态，即使现有配置没有 `reasoning_protocol` 字段也
如此。端点检查让不相关的混合模型网关保持向后兼容。对于别名和自定义模型 ID，
仍可在一个 `model_overrides` 条目中显式设置 `reasoning_protocol = "glm"`。
GLM 思考开启时，Reasonix 会按 GLM 交错与保留思考的要求，在后续历史中原样保留
并返回原始 `reasoning_content`。

如果自定义网关提供 Kimi K3，可在 provider 编辑器的高级设置中将推理协议选择为
**Kimi K3 推理**，或直接配置：

```toml
[[providers]]
name               = "my-kimi-gateway"
kind               = "openai"
base_url           = "https://my-gateway.example.com/v1"
model              = "kimi-k3"
api_key_env        = "MY_KIMI_API_KEY"
reasoning_protocol = "kimi-k3"
```

当网关域名无法被安全自动识别时，需要这个显式协议。它会在后续 assistant 历史中
保留 `reasoning_content`、使用 `max_completion_tokens`，并省略 K3 固定的采样字段。
不要把它加到精选的 OpenCode Go 预设中：该中转站有自己的 `high`/`max` 档位，
并且有意保持标准 OpenAI-compatible 请求形态。
启用该协议后，Reasonix 固定展示 K3 的 `auto`/`low`/`high`/`max` 档位，协议默认值
为 `max`；已有的 `supported_efforts` 配置仍会保留，但不会覆盖 K3 协议档位。

## DeepSeek Anthropic-compatible 端点

默认官方 DeepSeek provider 使用 `https://api.deepseek.com` 的 Chat Completions，
并开启[独立 `web_search` 工具](WEB_SEARCH.zh-CN.md)。搜索单独使用 Messages。
`deepseek-anthropic` 仍作为可选预设保留，主对话选择它时，Reasonix 会发送
`thinking.type=enabled|disabled` 与 `output_config.effort`，在请求携带 tools 时回放历史
assistant 轮次中未签名的 DeepSeek 思考块，省略不支持的图片，并依赖 DeepSeek 的自动前缀缓存，
而不是被忽略的 `cache_control` 标记。

该预设为 Flash 和 Pro 暴露相同的模型专属 effort 档位：`auto`、`disabled`、
`low`、`high` 和 `max`。Anthropic-compatible 端点在线上接受 `low|high|max`；
遗留的 `medium`、`xhigh` 均归一化为 `high`。

OpenAI-compatible 的 DeepSeek 路径采用相同的全轮回放规则：请求携带 tools 时，历史中
每个保存了 `reasoning_content` 的 assistant 轮次都会原样序列化回请求，不论该轮是否
调用过工具；不带 tools 时该字段会被 DeepSeek 忽略。如果旧会话仍因提供方特有的
reasoning 回传 HTTP 400 失败，Reasonix 只重建旧历史的 provider-visible 消息投影并
重试一次；后续新增轮次继续走正常 reasoning/tool replay，而 canonical session history
不会被修改。

## 缺失 reasoning 时的恢复

适配器决定回放要求。完整 DeepSeek Chat 响应允许空 `reasoning_content`，
Responses 允许缺省的 reasoning item；这些兼容路径直接继续，不为补齐 reasoning
额外生成一次响应。必要证明未完成或丢失时，严格协议最多恢复一次，优先修复
确有问题的历史，否则从原请求重新生成；不叠加两种恢复，也不切换模型或协议。
客户端截断的真实 reasoning 不能被空值替代。

## 其他所有后端（标准 `reasoning_effort`）

任何其他 OpenAI-compatible 后端都会回退到标准的 `reasoning_effort` 档位
（`low`\|`medium`\|`high`）。解析出的 provider/模型条目可以显式声明不同的支持
档位；在这种情况下，Reasonix 会保留这些声明的值，而不是套用通用上限。精选的
逐模型能力元数据可以像上面展示的那样选用其他档位。

以下主流提供商经调研无需**特殊处理**，因为它们已经遵循标准约定：

Qwen (`dashscope.aliyuncs.com`)、Yi (`api.01.ai`)、SiliconFlow
(`api.siliconflow.cn`)、Stepfun (`api.stepfun.com`)、Groq (`api.groq.com`)、
Together (`api.together.xyz`)、OpenRouter (`openrouter.ai`)、Perplexity
(`api.perplexity.ai`)、xAI (`api.x.ai`)。

对于使用二值 `thinking.type` 开关但**未被**自动识别的后端，在 provider 条目上
设置与厂商无关的 `thinking` 字段：

```toml
[[providers]]
name        = "my-glm-proxy"
kind        = "openai"
base_url    = "https://my-gateway.example.com/v1"
model       = "glm-4.6"
api_key_env = "MY_API_KEY"
thinking    = "disabled"   # enabled | disabled — 发送 thinking.type
```

## 故障排查

如果模型在你要求它不要思考时仍在思考（或反过来）：

1. 对照上表——后端可能**忽略**你设置的参数（例如 Zhipu 会忽略
   `reasoning_effort`；改用 `thinking`/`/effort`）。
2. 如果后端未被自动识别，就显式设置 `thinking` 字段。
3. 如果后端完全使用非 OpenAI 协议（例如百度文心），`openai` kind 无法驱动它
   的思考模式——那需要专门的 provider kind。

区分“provider 忽略字段”与 Reasonix 自身的 bug 从这里入手：Reasonix 发出的
请求形态按表格固定，因此表格与实际行为不一致时，问题在提供商而不是 Reasonix。

## Reasoning 回放与中断恢复

回放契约按适配器区分：DeepSeek Chat 保留空 `reasoning_content` 回退；
DeepSeek Responses 可以省略没有返回的 reasoning item，实际返回的 item 则会
保存。Anthropic unsigned thinking 与原生 Claude signed thinking 分开处理，
不能为缺失内容或签名制造占位块。未知中转不因模型名包含 DeepSeek 就获得
额外的空值兼容能力，继续使用显式协议配置。

Anthropic 保存首块 thinking、分片 signature、签名空文本块以及多个独立签名块。
Responses 保存完整 reasoning item，并采用 completed 响应中同一 ID 的终态内容。
缺失、显式空值、客户端截断和未完成状态分别记录；截断或未完成的必要 reasoning
不能通过空值回退放行工具执行。

先进行协议兼容转换，再判定是否需要严格恢复。原生 Claude 的完整、无签名、
不涉及工具的 assistant thinking，可以在出站请求中转换为普通 assistant 文本。
客户端或服务端工具轮、签名与无签名混合块、redacted 数据、未完成或截断的内容
不采用这种转换。本地原始 thinking 保持不变，不伪造签名。

未知 Anthropic 网关不会因为模型名称或 adaptive thinking 设置就被要求提供
Claude 签名。启用 thinking 回放时，保留实际收到的 unsigned 块，不添加签名；
整块缺失时不补造。显式 `reasoning_protocol = "deepseek"` 仍按 DeepSeek 契约
检查回放。若服务端确实拒绝回放，则进入既有的有限历史修复，携带已完成工具事实，
不重复执行工具。

原生签名和 DeepSeek 的健康回放前缀保持原样。某些 enabled 网关以前会丢掉真实
thinking，现在保留这些块会改变旧前缀一次；后续回放稳定。转换不新增普通用户
配置，也不新增持久化格式。

严格协议修复和 reasoning HTTP 400 修复共用一次预算，且消耗当前模型轮的统一重试次数。只修改供应商请求视图，保留本地原始记录和已完成工具事实；新的工具请求仍须通过回放检查。

工具完成后按调用顺序记录结果，下一个写操作开始前经过持久化检查；只读并行组
在组内全部返回后依次记录。持久化失败会阻止后续工具启动。恢复分别列出已完成、
确定未执行、结果未知的调用。结果未知时先核对文件或外部系统的副作用，不能把
“没有结果”当成“没有执行”，也不能因同批其他调用缺失而重复已完成的写入。

新增 `reasoning_state`、`thinking_blocks`、`tool_run_state` 以及恢复记录中的
`not_started_tools` / `unknown_tools` 均为可选字段。旧会话按现有字段推断状态，
旧版中断占位结果按结果未知处理。旧客户端可以忽略新字段并读取记录，但不保证
能续跑依赖多个签名块或不透明 Responses item 的新会话，这类会话应使用当前版本。
正常历史不逐轮清理 reasoning；故障修复可能降低被修复前缀的缓存命中，后续新增
的健康工具轮保留原样。


## 自动重试与等待

普通模型轮最多额外重试三次，退避为 2、4、8 秒；HTTP 连接、断流和协议修复不再分别重置预算。
服务端等待提示优先。仅主会话在明确的临时连接、限流或服务故障下，可在快速重试耗尽后
每约 60 秒继续等待；已经产生不完整内容、协议错误、凭据或额度问题不进入无限生成。
搜索、摘要、压缩和子任务使用有限重试。停止、既有任务期限和预算仍然有效；重启不自动联网续跑。
请求数和已知 usage 累计；缺失用量标为 unknown，不能解读为免费请求。

## 文件写入核验

内置写入和编辑在实际修改前持久化 `write_intents`，包括版本、原始及预期内容摘要、
编码、路径和执行通道；持久化失败时不写入。元数据不进入模型请求或工具 schema。
恢复通过原通道检查当前目标，满足全部预期条件时报告“修改目标已满足”，不虚构原执行结果。
文件冲突、未知版本、原通道失联或替换时保留未知；不会改读本地同名路径。
重复的未知写调用被阻止；只读核对仍然允许。Shell/MCP 不支持自动副作用核验。
旧会话缺少核验证据时仍可读取；旧客户端不保证新版恢复能力。


### 官方端点恢复实测

2026-09-05 的 Flash／Pro 实测区分了原始调用 ID 与替换后的 ID：省略 reasoning
在前者可能成功，在后者可能触发协议专属的 HTTP 400。Responses 错误字段为
`reasoning_text`，Messages 为 `content[].thinking`，Chat 为 `reasoning_content`；
均进入已有的有限恢复流程。不能从一次成功请求推断服务端无条件允许省略。

未结束 JSON 事件中的 EOF 进入有限断流恢复；完整事件中的非法 JSON 仍报错。
历史修复携带经过转义、有界、模型原本可见的已完成工具结果，并标明所属用户轮次；
修复边界定位在原始历史上，防止后续轮次重新引入已移除的错误协议历史。
RawContent 等本地内容不进入请求。只有故障恢复改变该前缀，健康工具 schema
和历史保持稳定。

文件写入的意图持久化绑定实际执行工具的上下文，在落盘前完成；缺少终态 usage
时，估算和合并也保留未知标记。真实端点结果、模型行为波动及服务端契约与故障
注入的区分，见[验证报告](RECOVERY_VALIDATION.md)。

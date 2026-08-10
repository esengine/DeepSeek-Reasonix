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
| DeepSeek V4 Flash | `api.deepseek.com`、`*.deepseek.com`                        | `thinking.type` + `reasoning_effort`（深度） | `auto`、`disabled`、`low`、`high`、`max` | 默认开启思考；`disabled` 通过 `thinking.type=disabled` 关闭。兼容性输入 `medium` 归一化为 `high`，`xhigh` 归一化为 `high`。 |
| DeepSeek V4 Pro   | `api.deepseek.com`、`*.deepseek.com`                        | `thinking.type` + `reasoning_effort`（深度） | `auto`、`disabled`、`high`、`max`        | 默认开启思考；`disabled` 通过 `thinking.type=disabled` 关闭。兼容性输入 `low`/`medium` 归一化为 `high`，`xhigh` 归一化为 `max`。 |
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

默认官方 DeepSeek provider 指向 `https://api.deepseek.com/anthropic`。
新建的官方条目使用原生 Messages API 路径并开启 provider 侧 `web_search`；已有显式
provider（包括旧的 `deepseek-anthropic` 条目）保留其原协议。Reasonix 会发送
`thinking.type=enabled|disabled` 与 `output_config.effort`，回放历史工具调用轮次
中未签名的 DeepSeek 思考块，省略不支持的图片，并依赖 DeepSeek 的自动前缀缓存，
而不是被忽略的 `cache_control` 标记。

该预设暴露当前模型专属的 effort 档位：Flash 支持 `auto`、`disabled`、`low`、
`high` 和 `max`，而 Pro 暴露 `auto`、`disabled`、`high` 和 `max`，因为其当前的
`low` 输入会映射到 `high`。Anthropic-compatible 端点在线上接受 `low|high|max`。
遗留的 `medium` 归一化为 `high`；遗留的 `xhigh` 对 Flash 归一化为 `high`、对
Pro 归一化为 `max`。Claude Opus 别名使用 Pro 映射，而 Sonnet/Haiku 以及不支持
的模型名遵循 DeepSeek 文档记载的 Flash 回退。

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

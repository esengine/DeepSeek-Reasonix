# Reasoning controls by provider

Reasonix exposes a single `/effort` knob (and the per-provider `effort` /
`thinking` config fields), but OpenAI-compatible backends disagree on *how*
chain-of-thought is requested on the wire. The `openai` provider adapts the
request shape per backend; this table is the reference for which protocol each
known backend uses and which parameters it honours or ignores.

## Auto-detected backends

These are recognised by base URL (see `internal/provider/openai/host.go`) and
get a tailored request shape automatically — no extra config needed.

| Provider | Base URL | Reasoning control | `/effort` levels | Notes |
|----------|----------|-------------------|------------------|-------|
| DeepSeek | `api.deepseek.com`, `*.deepseek.com` | `thinking.type` + `reasoning_effort` (depth) | `auto`, `disabled`, `high`, `max` | Thinking on by default; `disabled` turns it off via `thinking.type=disabled`. |
| MiniMax M3 | `api.minimaxi.com`, `*.minimaxi.com` | `thinking.type` (`adaptive`\|`disabled`) | `auto`, `adaptive`, `disabled` | No depth scale; `reasoning_effort` is omitted. |
| Zhipu GLM | `open.bigmodel.cn` / `*.bigmodel.cn`, `api.z.ai` / `*.z.ai` | `thinking.type` (`enabled`\|`disabled`) | `auto`, `enabled`, `disabled` | **`reasoning_effort` is silently ignored** by the endpoint, so reasoning is driven purely through `thinking.type`. |

## Explicit per-model scales

| Provider/model | Base URL | Reasoning control | `/effort` levels | Notes |
|----------------|----------|-------------------|------------------|-------|
| Kimi CN/Global `kimi-k3` | `api.moonshot.cn/v1`, `api.moonshot.ai/v1` | `reasoning_effort` | `low`, `high`, `max` | Always thinks; defaults to `max`. Reasonix replays the complete assistant message, uses `max_completion_tokens`, and omits K3's fixed sampling fields. |
| OpenCode Go `kimi-k3` | `opencode.ai/zen/go/v1` | `reasoning_effort` | `high`, `max` | Relay-specific scale; defaults to `max` and keeps the relay's standard OpenAI-compatible request shape. |

## Everything else (standard `reasoning_effort`)

Any other OpenAI-compatible backend falls through to the standard
`reasoning_effort` scale (`low`\|`medium`\|`high`). A resolved provider/model
entry may explicitly advertise a different supported scale; in that case
Reasonix preserves those declared values instead of applying the generic
ceiling. Curated per-model capability metadata can opt into another scale as
shown above.

Surveyed popular providers that need **no special handling** because they
already follow the standard convention:

Qwen (`dashscope.aliyuncs.com`), Yi
(`api.01.ai`), SiliconFlow (`api.siliconflow.cn`), Stepfun (`api.stepfun.com`),
Groq (`api.groq.com`), Together (`api.together.xyz`), OpenRouter
(`openrouter.ai`), Perplexity (`api.perplexity.ai`), xAI (`api.x.ai`).

For a backend that uses a binary `thinking.type` toggle but is **not**
auto-detected, set the vendor-agnostic `thinking` field on the provider entry:

```toml
[[providers]]
name        = "my-glm-proxy"
kind        = "openai"
base_url    = "https://my-gateway.example.com/v1"
model       = "glm-4.6"
api_key_env = "MY_API_KEY"
thinking    = "disabled"   # enabled | disabled — emits thinking.type
```

## Troubleshooting

If a model keeps thinking when you asked it not to (or vice versa):

1. Check the table above — a backend may **ignore** the parameter you set
   (e.g. Zhipu ignores `reasoning_effort`; use `thinking`/`/effort` instead).
2. If the backend isn't auto-detected, set the explicit `thinking` field.
3. If the backend uses a non-OpenAI protocol entirely (e.g. Baidu Wenxin), the
   `openai` kind cannot drive its thinking mode — that needs a dedicated
   provider kind.

Distinguishing "provider ignores the field" from a Reasonix bug starts here:
the request shape Reasonix emits is fixed per the table, so a mismatch between
the table and observed behaviour is the provider's, not Reasonix's.

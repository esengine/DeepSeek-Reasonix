# Reasoning controls by provider

Reasonix exposes a single `/effort` knob (and the per-provider `effort` /
`thinking` config fields), but OpenAI-compatible backends disagree on *how*
chain-of-thought is requested on the wire. The `openai` provider adapts the
request shape per backend; this table is the reference for which protocol each
known backend uses and which parameters it honours or ignores.

## Auto-detected backends

These are recognised by base URL (see `internal/model/openai/host.go`) and
get a tailored request shape automatically — no extra config needed.

| Provider | Base URL | Reasoning control | `/effort` levels | Notes |
|----------|----------|-------------------|------------------|-------|
| DeepSeek V4 Flash | `api.deepseek.com`, `*.deepseek.com` | `thinking.type` + `reasoning_effort` (depth) | `auto`, `disabled`, `low`, `high`, `max` | Thinking on by default; `disabled` turns it off via `thinking.type=disabled`. Compatibility input `medium` normalizes to `high`, while `xhigh` normalizes to `high`. |
| DeepSeek V4 Pro | `api.deepseek.com`, `*.deepseek.com` | `thinking.type` + `reasoning_effort` (depth) | `auto`, `disabled`, `high`, `max` | Thinking on by default; `disabled` turns it off via `thinking.type=disabled`. Compatibility inputs `low`/`medium` normalize to `high`, while `xhigh` normalizes to `max`. |
| MiniMax M3 | `api.minimaxi.com`, `*.minimaxi.com` | `thinking.type` (`adaptive`\|`disabled`) | `auto`, `adaptive`, `disabled` | No depth scale; `reasoning_effort` is omitted. |
| Zhipu GLM | `open.bigmodel.cn` / `*.bigmodel.cn`, `api.z.ai` / `*.z.ai` | `thinking.type` (`enabled`\|`disabled`) | `auto`, `enabled`, `disabled` | **`reasoning_effort` is silently ignored** by the endpoint, so reasoning is driven purely through `thinking.type`. |

## Explicit per-model scales

| Provider/model | Base URL | Reasoning control | `/effort` levels | Notes |
|----------------|----------|-------------------|------------------|-------|
| Kimi CN/Global `kimi-k3` | `api.moonshot.cn/v1`, `api.moonshot.ai/v1` | `reasoning_effort` | `low`, `high`, `max` | Always thinks; defaults to `max`. Reasonix replays the complete assistant message, uses `max_completion_tokens`, and omits K3's fixed sampling fields. |
| Custom Kimi K3 gateway | Any OpenAI-compatible K3 endpoint | `reasoning_effort` | `low`, `high`, `max` | Select `reasoning_protocol = "kimi-k3"` to opt into K3's complete-message replay and request shape. |
| OpenCode Go `kimi-k3` | `opencode.ai/zen/go/v1` | `reasoning_effort` | `high`, `max` | Relay-specific scale; defaults to `max` and keeps the relay's standard OpenAI-compatible request shape. |
| Token Rhythm DeepSeek V4 | `tokenrhythm.studio/v1` | DeepSeek `thinking.type` + `reasoning_effort` | Model-specific DeepSeek scale | Selected through the preset's model override, independent of the gateway host. |
| Token Rhythm GLM 5/5.1/5.2 | `tokenrhythm.studio/v1` | GLM `thinking.type` (`enabled`\|`disabled`) | `auto`, `enabled`, `disabled` | Selected through the preset's model override; `reasoning_effort` is omitted. |

On the Token Rhythm endpoint, exact GLM model IDs (`glm-5`, `glm-5.1`, and
`glm-5.2`) automatically select the official GLM request shape even when an
existing configuration has no `reasoning_protocol` field. The endpoint check
keeps unrelated mixed-model gateways backward-compatible. A `model_overrides`
entry with explicit `reasoning_protocol = "glm"` remains available for aliases
and custom model IDs. While GLM thinking is enabled, Reasonix retains and
returns the original `reasoning_content` unchanged in later history, as required
by GLM interleaved and preserved thinking.

For a custom gateway that serves Kimi K3, select **Kimi K3 reasoning** in the
provider editor's advanced reasoning protocol field, or configure it directly:

```toml
[[providers]]
name               = "my-kimi-gateway"
kind               = "openai"
base_url           = "https://my-gateway.example.com/v1"
model              = "kimi-k3"
api_key_env        = "MY_KIMI_API_KEY"
reasoning_protocol = "kimi-k3"
```

This explicit protocol is needed when the gateway host cannot be safely
auto-detected. It preserves `reasoning_content` in later assistant history,
uses `max_completion_tokens`, and omits K3's fixed sampling fields. Do not add
it to the curated OpenCode Go preset: that relay intentionally keeps its
standard OpenAI-compatible request shape and its own `high`/`max` scale.
While this protocol is selected, Reasonix always exposes K3's fixed
`auto`/`low`/`high`/`max` effort menu with `max` as the protocol default;
persisted `supported_efforts` metadata is retained but does not override it.

## DeepSeek Anthropic-compatible endpoint

The default official DeepSeek provider targets `https://api.deepseek.com/anthropic`.
New official entries use this native Messages API path and enable provider-side
`web_search`; existing explicit providers, including legacy
`deepseek-anthropic` entries, keep their configured protocol. Reasonix emits
`thinking.type=enabled|disabled` with `output_config.effort`, replays unsigned
DeepSeek thinking blocks from historical tool-call turns, omits unsupported
images, and relies on DeepSeek's automatic prefix cache instead of ignored
`cache_control` markers.

The preset exposes the current model-specific effort scales: Flash supports
`auto`, `disabled`, `low`, `high`, and `max`, while Pro exposes `auto`,
`disabled`, `high`, and `max` because its current `low` input maps to `high`.
The Anthropic-compatible endpoint accepts `low|high|max` on the wire. Legacy
`medium` normalizes to `high`; legacy `xhigh` normalizes to `high` for Flash and
`max` for Pro. Claude Opus aliases use the Pro mapping, while Sonnet/Haiku and
unsupported model names follow DeepSeek's documented Flash fallback.

## Anthropic-compatible endpoints (`kind = "anthropic"`)

Reasoning depth on the Messages API rides on fields Anthropic defines:
`thinking.type=adaptive` with `display=summarized`, `output_config.effort`, and
a raised automatic output budget. First-party endpoints — `api.anthropic.com`,
or an entry with no `base_url` — implement all of it, so they expose the full
`auto`/`low`/`medium`/`high`/`xhigh`/`max` menu and selecting a level engages
extended thinking by itself. Nothing writes a `thinking` mode into the config to
make the knob work.

A compatible gateway is not assumed to implement any of it. Undeclared, it
exposes the binary menu (`auto`/`enabled`/`disabled`) and receives
`thinking.type` alone — no `display`, no `output_config`, no budget raise —
because those are the fields a relay answers with a 4xx or 5xx for. A depth
level persisted for such an endpoint goes dormant rather than reaching the wire,
and a configured `thinking = "adaptive"` — a value only Anthropic's contract
defines — falls back to the provider default. `/effort enabled` still turns
thinking on through the toggle every compatible gateway implements.

Declare the contract to opt an endpoint in, either by naming the protocol or by
advertising a depth vocabulary:

```toml
[[providers]]
name               = "my-claude-relay"
kind               = "anthropic"
base_url           = "https://relay.example.com/anthropic"
model              = "claude-opus-4-8"
api_key_env        = "MY_RELAY_API_KEY"
reasoning_protocol = "anthropic"          # or: supported_efforts = ["low", "high", "max"]
```

An entry installed from a curated Anthropic-compatible preset (Kimi Coding Plan,
MiMo, GLM/Z.AI and Qwen coding plans, StepFun) needs neither: the `preset_id` it
records carries the vetting, so it keeps the thinking mode its preset declares.

## Chain-of-thought replay cost

Under the DeepSeek reasoning protocol the API 400s an assistant history turn
whose `reasoning_content` key is missing, so Reasonix replays it.

Replayed reasoning is billed as ordinary prompt input (measured ~500 extra
tokens per turn on a reasoner chain), for no cache-hit or coherence gain.

Set `strip_chain_of_thought = true` to send the key with an empty value instead:
the turn still satisfies the API's presence check, but the chain-of-thought is
not re-uploaded or billed. Off by default, which replays the exact reasoning.

```toml
[[providers]]
name                   = "deepseek"
kind                   = "openai"
base_url               = "https://api.deepseek.com"
model                  = "deepseek-v4-pro"
api_key_env            = "DEEPSEEK_API_KEY"
strip_chain_of_thought = true
```

This applies to the DeepSeek reasoning protocol only; Kimi K3 and GLM replay
their reasoning unchanged, because those contracts require the actual text.

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

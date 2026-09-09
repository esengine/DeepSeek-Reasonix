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
| DeepSeek V4 Flash | `api.deepseek.com`, `*.deepseek.com` | `thinking.type` + `reasoning_effort` (depth) | `auto`, `disabled`, `low`, `high`, `max` | Thinking on by default; `disabled` turns it off via `thinking.type=disabled`. Compatibility input `medium` normalizes to `high`, while `xhigh` normalizes to `high`. Reasoning is replayed on every historical assistant turn that carries it, including turns without tool calls. |
| DeepSeek V4 Pro | `api.deepseek.com`, `*.deepseek.com` | `thinking.type` + `reasoning_effort` (depth) | `auto`, `disabled`, `low`, `high`, `max` | Thinking on by default; `disabled` turns it off via `thinking.type=disabled`. Compatibility inputs `medium` and `xhigh` normalize to `high`. Reasoning is replayed on every historical assistant turn that carries it, including turns without tool calls. |
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

The default official DeepSeek provider uses Chat Completions at
`https://api.deepseek.com`, with the independent [`web_search` tool](WEB_SEARCH.md)
using Messages for search. The optional `deepseek-anthropic` preset remains
available. When selected for the main conversation, Reasonix emits
`thinking.type=enabled|disabled` with `output_config.effort`, replays unsigned
DeepSeek thinking blocks from every historical assistant turn that carries
reasoning when the request declares tools (tool-call turn or not), omits unsupported
images, and relies on DeepSeek's automatic prefix cache instead of ignored
`cache_control` markers.

The preset exposes the same model-specific effort scale for Flash and Pro:
`auto`, `disabled`, `low`, `high`, and `max`. The Anthropic-compatible endpoint
accepts `low|high|max` on the wire. Legacy `medium` and `xhigh` both normalize
to `high`.

The OpenAI-compatible DeepSeek path follows the same replay rule when a request
declares tools: every historical assistant turn with stored `reasoning_content`
is serialized back verbatim, whether or not that turn called a tool. Without
tools, DeepSeek ignores this field and does not concatenate it into context. If
an old session still fails with the provider's specific reasoning pass-back HTTP
400, Reasonix rebuilds only the provider-visible projection of the old history,
retries once, and leaves later turns on the normal replay path; canonical
session history remains unchanged.

## Missing-reasoning recovery

Adapters own replay requirements. Complete DeepSeek Chat responses can use empty
`reasoning_content`; compatible Responses endpoints can omit absent reasoning
items. These paths continue without an extra generation. Strict contracts allow
one recovery for missing or unfinished required proof, preferring repairable
history over exact regeneration. The two recoveries do not stack, and neither
changes the selected model or protocol. Client-truncated proof is never replaced
with an empty field.

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

## Reasoning replay and interrupted execution

Replay contracts belong to adapters: DeepSeek Chat retains its empty
`reasoning_content` fallback; DeepSeek Responses can omit an absent reasoning
item but retains items actually returned. Anthropic unsigned thinking and native
Claude signed thinking have separate requirements. Missing content or proofs
never produce fabricated blocks. Unknown gateways do not gain extra empty-value
compatibility from a DeepSeek model name; explicit protocol configuration applies.

Anthropic preserves initial thinking, signature fragments, signed empty text, and
separate signed blocks. Responses preserves full reasoning items and uses the
completed response's final snapshot for the same item ID. Missing, explicitly
empty, client-truncated, and unfinished states are distinct. Required truncated or
unfinished reasoning cannot use an empty fallback to release tool execution.

Compatibility conversion runs before strict recovery. Native Claude's complete,
unsigned thinking from a non-tool assistant turn can become ordinary assistant
text in the outbound view. It is not applied to client/server tool turns, mixed
signed/unsigned proofs, redacted data, or incomplete/truncated reasoning. Raw
local thinking stays intact; no signature is invented.

An unknown Anthropic gateway does not acquire Claude's signature requirement
from its model name or adaptive-thinking setting. With thinking replay enabled,
its actually received unsigned blocks are retained without adding a signature.
An absent block is not synthesized. Explicit `reasoning_protocol = "deepseek"`
continues to enforce the DeepSeek replay contract. A concrete replay rejection
from the server uses the existing bounded history repair, retaining completed
execution facts instead of repeating tools.

Native signed and DeepSeek replay prefixes stay unchanged. Gateways whose enabled
thinking was previously dropped now receive their actual blocks; this corrects
lossy serialization but can change the old prefix once. No ordinary user setting
or additional persisted format is introduced by these conversions.

Strict replay repair and reasoning HTTP 400 repair share one recovery and consume the current model round retry budget. Only the outbound view changes; original records and completed-tool facts remain available. New calls still pass replay validation.

Tool results are recorded in call order, crossing a durability barrier before the
next writer starts. Read-only parallel groups checkpoint after the group returns.
Persistence failure prevents later tools from starting. Recovery distinguishes
completed, definitely not started, and outcome unknown. Unknown calls require
checking filesystem or external side effects before retrying. Missing results do
not prove non-execution, and unfinished siblings do not hide completed writes.

The optional fields `reasoning_state`, `thinking_blocks`, `tool_run_state`, and
recovery fields `not_started_tools` / `unknown_tools` preserve legacy reads.
Older sessions infer state from existing fields; interrupted placeholders mean
unknown outcome. Older clients can ignore new metadata for display, but cannot be
guaranteed to resume sessions relying on multiple signed blocks or opaque Responses
items; use the current version for those sessions. Healthy histories are not
stripped each round. Fault repair can change the repaired prefix's cache hit;
newly appended healthy tool rounds stay outside the old-prefix repair.


## Automatic retries and waiting

Model rounds allow three additional attempts with 2/4/8-second backoff. HTTP,
stream, and protocol recovery no longer reset separate budgets. Server retry
delays take precedence. Only the main conversation can keep waiting after
transient connection, throttling, or service failures, normally every 60 seconds.
Partial generations, protocol failures, credentials, and exhausted quota cannot
enter unlimited generation. Search, summaries, compaction, and subagents use
finite retries. Cancellation and existing task limits remain effective; restarting
the app does not automatically resume network activity. Request counts and known
usage accumulate; missing usage is marked unknown, not interpreted as free.

## File write verification

Built-in writes and edits persist versioned `write_intents` before modification,
including before/after content digests, encoding, path, and execution route.
Failed persistence prevents writing. Metadata never enters model requests or tool
schemas. Recovery reads the original route and reports satisfied postconditions
only when every target matches; it does not invent the original execution result.
Conflicts, unknown versions, and unavailable or replaced transports stay unknown.
There is no fallback to a similarly named local file. Identical unresolved writes
are blocked; read-only inspection remains available. Shell/MCP effects are not
automatically verified. Old sessions remain readable without evidence; older
clients do not provide the new recovery guarantees.


### Verified official recovery behavior

The 2026-09-05 official Flash/Pro probes distinguished original provider call
IDs from replacement IDs: omitting reasoning can succeed with the former and
return a protocol-specific HTTP 400 with the latter. Responses names
`reasoning_text`, Messages names `content[].thinking`, and Chat names
`reasoning_content`; all use the existing bounded recovery path. Do not infer
unconditional omission support from one successful request.

EOF inside an unterminated JSON event now enters finite stream recovery;
malformed complete events remain errors. History repair carries bounded,
escaped, model-visible completed tool outputs with their originating user turns.
It retains the original repair boundary so later turns cannot reintroduce the
removed protocol history. Raw/local-only output remains excluded. Only fault
recovery changes that prefix; healthy schema and history stay stable.

File-write intent persistence runs on the actual prepared dispatch context,
before the effect. Missing terminal usage remains unknown through estimation
and aggregation. See [the validation report](RECOVERY_VALIDATION.md) for real
endpoint results, observed model variability, and the distinction between API
acceptance and fault-injected Agent tests.

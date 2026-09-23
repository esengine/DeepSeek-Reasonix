# Provider catalog

The desktop preset picker has one entry per provider brand. Access plan,
account platform and API format select a concrete existing preset, in that order. Only combinations
registered by the host are offered. OpenCode Go and Zen remain separate plans;
model-scoped routes remain selectable inside their plan.

Adding a preset keeps its existing ID, credentials, model overrides and install
conflict checks. Browsing the catalog does not modify installed connections.
Existing custom endpoints, connection names and session references are retained.
You can edit the address and model list after adding, or use Custom provider to
create another connection with its own name and credentials.

The API format selector names Anthropic Messages, Chat Completions and Responses
explicitly. During an explicit format change, known preset addresses follow the
matching registered route. Standard request suffixes can be updated for custom
connections; custom paths and query-bearing exact URL overrides are preserved.
Protocol changes may change request serialization and provider cache reuse.

## Added providers

OpenAI (Responses and Chat Completions), Anthropic, Google Gemini (OpenAI
compatibility), SiliconFlow, OpenRouter, Groq, Mistral AI, Yolo-Auto, local
Ollama and LM Studio. Example model names are editable starting points, not
guarantees of account access or local installation. Fetch or enter the actual
models after adding. Native Anthropic server tools are not enabled by the preset. Gemini's
native API, OAuth-only services, Azure deployment setup and other special
protocols are not implied by this catalog expansion.

## Sources

These definitions are maintained independently from Cherry Studio source code.
Endpoint sources:

- [OpenAI API](https://platform.openai.com/docs/api-reference/introduction)
- [Anthropic Messages](https://platform.claude.com/docs/en/api/http/messages/create)
- [Gemini OpenAI compatibility](https://ai.google.dev/gemini-api/docs/openai)
- [SiliconFlow quickstart](https://docs.siliconflow.cn/docs/userguide/quickstart)
- [OpenRouter quickstart](https://openrouter.ai/docs/quickstart)
- [Groq OpenAI compatibility](https://console.groq.com/docs/openai)
- [Mistral API](https://docs.mistral.ai/api)
- [Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility)
- [LM Studio OpenAI compatibility](https://lmstudio.ai/docs/developer/openai-compat)
- [Yolo-Auto docs](https://yolo-auto.com/docs)

Bundled brand icons come from LobeHub Icons under MIT. The pinned source revision
and full license are in `desktop/frontend/public/provider-icons/`. Brands without
a bundled icon use an initial. No third-party scripts or remote icon requests are
needed at runtime.

## Maintenance and compatibility

`internal/config/provider_catalog.go` owns brand/platform/plan metadata. Protocol
and default URL come from the preset's actual entries. After a catalog or added
preset change, regenerate browser fixtures from the repository root:

```sh
go run scripts/generate-provider-catalog.go
```

| Contract | Behavior |
| --- | --- |
| Saved provider TOML and credentials | Browsing makes no writes; the one-time MiMo model upgrade below preserves credentials and selections |
| Existing preset IDs | Preserved |
| Desktop `ProviderPresetView.catalog` | Additive display metadata; old clients ignore it |
| New frontend with older host | Generated known-ID fallback; unknown presets remain individually accessible |
| Provider request prefix | No catalog metadata is added to model requests |

## MiMo API and Token Plan

Choose **Pay-as-you-go API** or **Token Plan** first. The API uses
`MIMO_API_KEY` and does not show a region selector. Token Plan uses
`MIMO_TOKEN_PLAN_API_KEY` and then offers China, Singapore and Europe service
clusters. Changing clusters preserves a selected protocol when that preset is
available. Token Plan is restricted to supported AI coding tools; its key and
quota are independent of the ordinary API.

New connections default to `mimo-v2.6-pro`, with `mimo-v2.6-flash` also included.
V2.5 IDs remain available for existing selections. Schema version 12 appends
the V2.6 models once to eligible saved official V2.5 connections. It preserves
the current default, model order, custom rates, credential references and unknown
fields. Explicit custom request URLs and third-party endpoints are excluded.
The model additions and version marker are written atomically under the config
edit lock; subsequent starts do not restore models the user removes.

| Field or format | Old-data behavior | New reader | Previous schema-v11 reader | Conclusion |
| --- | --- | --- | --- | --- |
| `models`, `default`, model references | Existing V2.5 selections remain first/default | Appends V2.6 once; keeps selections | Reads and saves model IDs without switching defaults | Compatible |
| `config_version = 12` | Earlier versions are eligible for startup migration | Prevents repeated additions after deletion | Retains the marker when saving | Compatible |
| `prices`, `vision_models`, unknown fields | Preserves user rates and explicit vision choices | Extends known curated vision lists and missing V2.6 prices; preserves unknown data during migration | Reads the existing fields and canonical nested price tables | No new persisted field types |

Official references: [Token Plan](https://mimo.mi.com/docs/zh-CN/tokenplan/Token%20Plan/subscription),
[API pricing](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go),
[API rate limits](https://mimo.mi.com/docs/zh-CN/api/guidance/rate-limit).

## Connection display names

Optional `display_name` is UI metadata; `name` remains the stable connection,
model-reference and credential identity. Lists, details and model pickers prefer
the label, falling back to the existing name when empty. New presets initialize
it from their title. Duplicate labels never merge connections. The field is not
added to requests or prompts, and renaming does not rewrite historical sessions.

| Scenario | Behavior |
| --- | --- |
| Old configuration without the field | Existing display; no migration |
| Current writer and restart | Label and stable references preserved |
| Older frontend omits displayName | Current backend preserves the label |
| Explicit empty string | Clears the label and restores fallback |
| Older application reads and rewrites config | Connection remains readable; its writer may lose the label |

Connection details offer inline title editing: Enter saves, Escape cancels, and
blur does not submit. The dedicated rename operation changes only the label;
the detail configuration editor omits that field so stale drafts cannot overwrite it.

Built-in and custom connections share the same detail editor. Built-in entries
provide initial defaults; protocol, endpoint, credentials and models remain
editable. The detail layout does not depend on creation source. Actual endpoint
and model metadata continue to determine service capabilities.

Model editing uses one selection list, with comma-separated IDs supported in
manual addition. Discovery merges candidates without changing selection or
saving configuration. Context overrides live in per-model settings.
Refreshing verifies model discovery only, not inference; no redundant check button is shown. Keys are saved
separately; checks and discovery do not save keys or enable models automatically.

Keys normally show status with Change and a more-actions menu for source, sharing and removal. Refresh and Add sit beside the model heading.

### Compact connection editor

Connection fields share one aligned form. Model selection, refresh and manual additions share one toolbar. Adding and editing models share a modal with context-window and output-token overrides. Text input/output are fixed; image input can be overridden or restored to automatic detection. Video and PDF remain unavailable until the request pipeline supports them. Applying the modal updates the configuration draft; saving the connection persists it. Connection identity and the footer occupy fixed layout slots; navigation and configuration content scroll independently without an additional model-list scroller. The save footer distinguishes clean and unsaved states, and failed saves retain the draft. Credentials remain independently saved.

With the frontend running, `/dev/provider-layout-preview.html` provides an isolated preview using the production components and in-memory data; it never writes real connections or credentials. See root `design-qa.md` for visual verification.

### AMD GPU Cloud

Added independently maintained preset `amd-gpu-cloud`, OpenAI Chat Completions,
base URL `https://developer.amd.com.cn/radeon/v1`, and case-sensitive model IDs
from Cherry Studio's registry:
https://github.com/CherryHQ/cherry-studio/blob/main/packages/provider-registry/src/providers/radeon-cloud.ts

API key/account portal: https://developer.amd.com.cn/radeon/tokenfactory
Model availability and promotional quotas are account-dependent; refresh models
after connecting. No live authenticated AMD request was performed.
AMD icon is from Simple Icons (CC0), https://github.com/simple-icons/simple-icons/blob/develop/icons/amd.svg;
the AMD trademark remains its owner's property.

### Additional inference platforms

Added eight brands: Doubao (Chat/Responses), Baidu Qianfan, PPIO,
Qiniu, xAI (Chat/Responses), Cerebras, Together, Fireworks
(Chat/Anthropic/Responses). These twelve editable route templates share brand
identity across protocols. Adding a template still creates an independent connection.

Sources checked against Cherry Studio's provider registry:
https://github.com/CherryHQ/cherry-studio/tree/main/packages/provider-registry/src/providers
Also: https://docs.fireworks.ai/tools-sdks/openai-compatibility
https://docs.fireworks.ai/getting-started/quickstart
https://docs.fireworks.ai/guides/response-api
https://docs.together.ai/docs/inference/openai-compatibility
https://cloud.baidu.com/doc/qianfan/s/rmh4stp0j
https://models.dev/api.json (Cerebras, xAI, Qiniu model identifiers).

Defaults are starting points; account access can vary. Web search is off and
no unverified reasoning overrides are added. These are protocol presets, not
certification of every model's agent/tool/reasoning capabilities. Model discovery,
tool calls and thinking require authenticated platform verification; none was
performed in this batch. Icons use the existing pinned LobeHub MIT source.

### Yolo-Auto

Added independently maintained preset `yolo-auto`, OpenAI Chat Completions,
base URL `https://yolo-auto.com/v1` and key reference `YOLO_AUTO_API_KEY`. The
slugs `yolo` and `yolo-small` are editable starting points; the account catalog
is discoverable at `GET /v1/models`. Web search is off and no unverified
reasoning overrides are added. Refresh models after connecting; no authenticated
discovery or inference request was performed. Reference:
https://yolo-auto.com/docs

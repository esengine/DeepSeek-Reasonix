# Independent web search

Reasonix exposes `web_search` as an ordinary function tool. A search opens a
separate model request containing the query and the backend's native search
tool. The main conversation receives a bounded JSON result with `summary`,
`sources` (title and URL), and an optional `truncated` flag. Search reasoning,
encrypted source bodies and Responses replay items stay out of chat history.
The main model can use Chat Completions while search uses Messages or Responses.

## Account selection

Search uses the current chat account when its search switch is enabled and the
account is configured. Otherwise it uses the first configured, enabled search
account in configuration order. An explicit `web_search = false` on a current
search-capable account prevents fallback. The selected search account and model
are frozen for that runtime assembly; a rebuild resolves them again.

Official DeepSeek accounts default to enabled when `web_search` is omitted.
Exact official Messages, Responses and Chat Completions routes (the latter
optionally ending in `/v1`) use the same account and model on
`https://api.deepseek.com/anthropic` for structured native search sources.
The main conversation keeps its protocol. Request URL overrides are not
translated. Third-party Messages and Responses accounts must opt in with
`web_search = true`; their configured endpoint and credentials remain unchanged.
Reasonix never sends a relay's key to the official DeepSeek endpoint.

The existing Desktop search switch applies to independent search too. No new
configuration fields or session migration are required. If `[tools].enabled`
is an explicit allowlist, include `web_search`. Offline mode omits this tool.
Providers supplied exclusively by a remote broker or extension do not imply
local search credentials: an enabled local search account is still needed.

## Assigning a search model

Desktop **Model preferences → Model assignment → Web search** offers Automatic
or an explicit connection/model. Automatic preserves the account selection rules
above. An explicit assignment uses the search switch on the assigned connection,
independently of the conversation account's search switch.

```toml
[agent]
web_search_model = "my-search-connection/deepseek-v4-flash"
```

Omission, an empty string, and `"auto"` mean automatic selection. Explicit values
use `provider/model`, including model IDs containing `/`. Offline mode, the tool
allowlist and connection access restrictions still apply. Third-party candidates
indicate configured native-search eligibility, not live-verified model support.

Desktop writes the global user setting and displays an effective project override
when `reasonix.toml` owns the field. Selection is frozen when a runtime is built.
Saving in an idle session rebuilds its runtime; a running task cannot be forcibly
rebuilt by this setting. Other runtimes adopt it on their next rebuild.

If an assigned connection is removed, disabled or loses credentials, the reference
is retained. New runtimes omit the search tool and report the problem once; normal
chat remains available. There is no fallback to another account. Select Automatic
or a valid model to recover. Search requests and usage belong to the assigned
connection/model, without changing the conversation model.

No config-version or session migration is required. Previous versions ignore the
new field and retain their old search-selection behavior. Their general config
writer re-renders the file and may discard this field, comments and unknown keys;
downgrading does not preserve explicit search-account selection. Saving this
setting in the new Desktop changes only the field and preserves other content.

Switching between valid search assignments preserves the main model's tool
schema. Enabling or disabling the tool changes the tool list and may invalidate
an existing prompt-cache prefix.

## Requests and results

Each call contains only its query, so include necessary context in the query.
The service cannot read the conversation, attachments, workspace or other
searches. Concurrent calls use independent provider clients. No client tools
are made available to the search model. Search fails if the provider returns
only prose without a completed native search result.

Calls have a 90-second deadline and an 8192-output-token budget. Results retain
up to eight distinct HTTP(S) sources and a bounded summary; the encoded result
is at most 24000 bytes so normal tool-output limits do not cut its JSON. Read
source pages with `web_fetch` when the summary is insufficient. Retrieved text
is untrusted content, not instructions. Search requests reject redirects.

Search token usage and HTTP attempt counts are reported separately as
`web-search` and included in task usage with the selected account's pricing.
Each search adds an auxiliary model request; cost and latency depend on that
model and endpoint. Search failures return through the normal tool-error path.
A third-party backend may complete search without exposing structured sources;
in that case `sources` is empty. Sources are never guessed from generated prose.

## Compatibility and cache impact

Existing provider search switches and their explicit off values are preserved.
Old `server_search` records and native Responses items remain readable and
replayable. New results are ordinary tool messages, readable by older versions;
older source-card renderers may display their JSON as text. Current CLI and
Desktop accept both structured results and legacy title/URL output.

Replacing a native tool declaration with the function schema changes the
provider-visible tools and can invalidate an existing cache prefix. Within an
unchanged runtime, the tool schema/order remains fixed; queries and search
results append through ordinary tool turns. Search does not rewrite system
prompts, old messages or main-model thinking settings. Reduced total cost or a
higher cache hit rate is not guaranteed.

Independent search does not fix missing reasoning in the main model's other
tool calls. Those calls still follow their provider's reasoning replay policy.

## Default-provider upgrade

New CLI defaults (`deepseek-flash`, `deepseek-pro`) and the Desktop `deepseek`
template use Chat Completions, with Flash selected, thinking enabled, high effort
and independent search enabled. Config version 9 migrates existing official
DeepSeek standard endpoints to Chat Completions, including renamed accounts,
Anthropic/Responses presets and standard request URL overrides, which are
cleared so the derived endpoint and its independent search still apply. Model
selection, key references, headers, extra body, effort, prices and search
settings survive.
Third-party gateways, nonstandard paths and URLs containing queries are excluded.
Later manual protocol edits remain authoritative. Project configs and session
files are not rewritten. Version 7 and 8 files receive a scalar-only edit preserving comments and unknown data;
earlier versions first run the existing config upgrades. Previous releases can
read the OpenAI route but lack independent search. An older binary with the old
startup migration may change minimal legacy provider entries again.

| Field / format | New reader of old data | Previous reader of new data | Conclusion |
| --- | --- | --- | --- |
| `kind` / `base_url` / request URLs | Official standard endpoints are migrated | Existing OpenAI values remain readable | No new protocol enum |
| `config_version` | One-time v9 upgrade; future versions remain untouched | Number remains readable; old automatic migrations have the limit above | Avoid mixing binaries with the retired migration |
| Models, pricing, effort, search switch | Preserved by this protocol migration | Unchanged field formats | User settings retained |
| Sessions | Unchanged files; existing adapter history projection | Unchanged file format | No session migration |

The exact beta ID `DeepSeek-V4.1-Flash-Expires-On-0910` has an explicit
Chat Completions wire alias `deepseek-v4.1-flash-expires-on-0910` on the official
host. Stored IDs and override ownership remain unchanged; arbitrary IDs are not
lowercased. This alias does not grant beta access or extend its availability.
Protocol changes can affect prefix-cache reuse on the first subsequent request.

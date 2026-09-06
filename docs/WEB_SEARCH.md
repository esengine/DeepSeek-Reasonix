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
and independent search enabled. Config version 8 restores historical built-in
Messages defaults to Chat Completions while preserving model selection, key
environment names, effort, prices and an explicit search disable.
Separate Anthropic/Responses entries, explicit preset identities and custom
transports or reasoning protocols are not migrated. Later manual protocol edits
remain authoritative. Project configs and session files are not rewritten.
Version 7 files receive a scalar-only edit preserving comments and unknown data;
earlier versions first run the existing config upgrades. Previous releases can
read the OpenAI route but lack independent search. An older binary with the old
startup migration may change minimal legacy provider entries again.

| Field / format | New reader of old data | Previous reader of new data | Conclusion |
| --- | --- | --- | --- |
| `kind` / `base_url` | Eligible historical defaults are migrated | Existing OpenAI values remain readable | No new protocol enum |
| `config_version` | One-time v8 upgrade; future versions remain untouched | Number remains readable; old automatic migrations have the limit above | Avoid mixing binaries with the retired migration |
| Models, pricing, effort, search switch | Preserved by this protocol migration | Unchanged field formats | User settings retained |
| Sessions | Unchanged files; existing adapter history projection | Unchanged file format | No session migration |

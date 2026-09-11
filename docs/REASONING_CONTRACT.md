# Adapter-owned reasoning controls

Each protocol adapter registers a pure `ReasoningForConfig` resolver alongside
its provider factory. Its returned options are ordered IDs with display names and
optional descriptions. Core does not impose a global effort vocabulary. Resolved
clients expose a detached capability snapshot through `ReasoningProvider`.

Configuration, the desktop effort menu, CLI completion, the local model catalog,
and request validation use these declarations. Model overrides must be resolved
before querying capabilities. Extension providers own their declared `Efforts`
list; selection and request overrides are validated before sidecar stream I/O.

Explicit selections must match a declared ID exactly. Unsupported choices return
`UNSUPPORTED_REASONING_EFFORT` before network I/O. Invalid declarations are also
rejected. No nearest-level mapping is performed. Binary protocols cannot acquire
a depth scale merely by listing depth values in `supported_efforts`.

`auto` remains the existing UI/CLI spelling for clearing an override; it is not an
adapter option and does not mean adaptive thinking. Request-level overrides use
an empty string to inherit configuration, not the literal `auto`. Existing load
normalization of retired stored `off` and letter case is retained. Existing valid
IDs and TOML field names remain unchanged. Saved DeepSeek `medium` and `xhigh`
values retain their historical `high` wire value when no explicit effort vocabulary
is declared; configuration storage is not rewritten. New explicit selections and
request overrides still reject undeclared aliases. Other unsupported aliases
produce an actionable error. Invalid configured
defaults remain visible for validation instead of falling back to another level.

| Boundary | Compatibility |
| --- | --- |
| Provider TOML | Same fields and valid IDs; no automatic file rewrite |
| Desktop `EffortInfo.options` | Optional additive metadata; `levels` remains for older clients |
| New frontend / older backend | Falls back to the older `levels` field |
| Remote model descriptors | Existing `Efforts` declarations remain authoritative |
| Provider-visible history | No prompt, tool schema, or reasoning-history rewrite |

Default requests retain existing serialization. Explicitly changing an effort can
change provider cache behavior; the contract itself does not add prompt bytes.
The experimental governor checks declared capability before applying its low
request override. This change does not introduce automatic cross-model effort
migration or copy Harness's request journal architecture.

The design is independently implemented for Reasonix, informed by
[DeepSeek Harness's adapter-owned reasoning contract](https://github.com/deepseek-ai/deepseek-harness/blob/d347e703908d0406b7a7ef80e3a0e594d86b2215/.agents/notes/implemented/architecture/2026-07-24-adapter-owned-reasoning-effort-capabilities.md).
No upstream implementation was copied.

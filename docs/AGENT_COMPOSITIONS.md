---
owner: "@SivanCola"
backup: "@esengine"
status: active
reviewed: 2026-10-18
---

# Session Agent compositions

Studio's **Agent presets** page manages a persona together with tools, skills,
MCP servers, extensions, hooks and custom commands. A composition is independent
of the model, reasoning effort and the existing balanced/delivery role setting.
It uses Reasonix's runtime; it does not load Harness's Cordis plugins.

## Choosing and editing

Choose the mode at the left of the composer status row before
the first prompt. One menu lists compositions with their sources and capabilities;
**Manage Agent presets…** opens the management page below the choices. Once a prompt is
accepted, the conversation keeps its binding.

The trigger shows only the mode name and arrow. Source and lock information stay
in the menu and accessible label; run status and token counts sit at the right.

Queued work, pending questions and
background jobs also prevent switching. The locked selector offers a new
conversation with another composition. Clearing or rewinding an existing
composition does not unlock it.

Two built-in cards sit above the separate custom-preset section:

| Mode | Behavior |
| --- | --- |
| Standard (`builtin:general`) | Inherit current configuration for everyday code, file and research work. The stable ID preserves existing defaults. |
| Minimal (`builtin:minimal`) | A concise system persona, terminal (`bash`) and image reading (`view_image`), with native context compaction. Requires a vision-capable model. |

Minimal loads no custom skills, commands, MCP, extensions, hooks or subagents.
It retains the host controls needed to answer and ask questions. Its terminal
can still modify files and access the network under existing permissions and
sandbox rules; Minimal is not a read-only mode.

Missing image capability or
disabled tools produce an explicit dependency error rather than another tool set.

Code review and Research are no longer built-in catalog entries. Their existing
conversations retain the original binding. An old default pointing to a retired
entry reports a diagnostic until an available default is explicitly chosen.

Copy a built-in to customize it. Each module group either inherits configuration
when a conversation binds, or uses only the selected IDs. An empty explicit
selection disables the group. The editor lists locally available modules;
imports preview missing dependencies without installing or launching them.

Saving creates a definition revision. Existing conversations retain their
resolved persona, parameters and resources. The page displays the definition
revision beside the conversation's bound revision and reports stale-save
conflicts while preserving the draft. Models remain independently selectable.

## Files and retained resources

| Item | Location |
| --- | --- |
| Personal definitions | `<state>/agent-presets/definitions/<id>.json` |
| Shared definitions | `<workspace>/.reasonix/agent-presets/definitions/<id>.json` |
| Definition revisions and explicit defaults | `<state>/agent-presets/` |
| Local authorization records and retained files | `<state>/trusted/agent-presets/` |

`<state>` is Reasonix's resolved state root, including relocation. IDs start
with `builtin:`, `user:` or `project:`. A project binding also records its
workspace. Matching display names coexist; one scope never shadows another.

Project files cannot select a default: workspace defaults are the user's
explicit choice, stored in personal state.

Definitions use `schemaVersion: 1`. Writes use atomic replacement and an
expected revision.

Exported definitions contain references rather than
credential values and never contain extension binaries. MCP supports `args`,
`url`, `env`, `headers`, `callTimeoutSeconds` and `startupTimeoutSeconds`;
extensions support `args`, `env` and `timeoutMillis`.

Parameter names are
validated. Environment/header overrides require `${NAME}` references.
Presets cannot replace an installed extension's executable declaration.

Inherited choices resolve once at binding. Retained skill and hook files and
extension code do not follow subsequent source edits.

Hooks retain referenced files and sibling bundles for common script formats.
Bundles must fit the resource limit and cannot contain credential files or
symbolic links.

Keep hook code in a dedicated directory; retain credentials
through environment references. External interpreters and services keep their
existing platform/authorization contracts.

Credential lookups remain
with the existing credential/configuration owner. Sessions use separate MCP
connections and process state, and extensions run from private copies. Existing
trust, revocation, authorization and sandbox checks still apply.

A binding is local transcript metadata and never enters provider messages.
Composition transcripts use native event-log schema 4; older readers refuse
them instead of continuing with another tool set. Legacy transcripts keep
their compatibility path and are not assigned today's default on restore.

Untrusted or unavailable bindings can be opened for history without launching
modules; continuation is blocked. Deleting a definition does not remove its
conversation's retained binding.

Native JSON exports use the `reasonix-native-session` version 2 envelope and
include the authoritative event-log binding and retained resources. Import
them under Settings → Storage. Import never creates local authorization records
or starts programs.

Configuration backup version 3 also includes personal
preset definitions and their default selection.

MCP tool contracts are pinned at binding. A changed tool catalog after refresh
or reconnect blocks execution instead of silently replacing schemas. Package
MCP processes and extension sidecars use private package copies; shutdown and
failed assembly release their private directories.

The nonempty legacy user persona migrates once to a personal default preset.
The original configuration remains. `/persona` reads and edits that personal
default rather than maintaining a second mutable persona. Migration retries
reuse an already written revision and preserve conflicting personal IDs.

## Entry points

CLI: `reasonix --agent-composition builtin:minimal`, or
`reasonix run --agent-composition user:my-agent "Review this change"`.
`/agent` lists compositions; `/agent user:my-agent` selects one in a blank
interactive conversation. `/preset` retains its balanced/delivery meaning.

ACP exposes an independent `agent_composition` configuration option and narrows
it to the current choice after the conversation starts. Remote sessions use the
remote host's catalog, credentials and retained resources.

| HTTP method and route | Request / result |
| --- | --- |
| `GET /agent-presets` | Catalog, effective default, module IDs and diagnostics |
| `GET /agent-presets/{id}` | Definition and revision |
| `POST /agent-presets` | `{definition, expectedRevision}` → saved version |
| `DELETE /agent-presets/{id}?revision=...` | Delete the matching declaration |
| `POST /agent-presets/default` | `{id, workspace}` |
| `GET /agent-presets/{id}/export` | Portable definition JSON |
| `POST /agent-presets/import` | `{content}` → `{definition, diagnostics}` preview |
| `GET /agent-composition` | Current binding summary, including lock/unavailability |
| `POST /agent-composition` | `{id}` → selected summary |
| `POST /runtimes` | Existing create fields plus optional `agentComposition` |
| `POST /tree/sessions/export` | `{path}` → `{name, content}`; native envelope for bound sessions |
| `POST /tree/sessions/import-native` | `{workspace, content}` → imported `{path, agentComposition}` |

`/status` and runtime listings include `agentComposition`. Refusals use stable
`agent_composition.invalid`, `.not_found`, `.read_only`, `.conflict`, `.locked`
and `.unavailable` codes. The Studio catalogs translate these into Chinese and
English.

Preset names, revisions and diagnostics do not enter the cached
provider prefix. First use of a different composition can have a cold cache;
editing a preset never rewrites a running conversation's prefix.

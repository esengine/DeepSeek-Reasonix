---
owner: "@SivanCola"
backup: "@esengine"
status: active
reviewed: 2026-10-20
---

# Session Agent compositions

Studio's **Agent presets** page manages a persona together with tools, skills,
MCP servers, extensions, hooks and custom commands. A composition is independent
of the model, reasoning effort and the existing balanced/delivery role setting.
It uses Reasonix's runtime; it does not load Harness's Cordis plugins.

Compositions are opt-in. No selection and Everyday partner use the ordinary configuration
without a binding, a new transcript schema or persona migration. Saved preset
preferences do not activate themselves. Explicitly choose another preset
to bind a version.

`--agent-composition default` explicitly chooses the saved preference.

## Choosing and editing

For **Selected modules only**, **Add modules** opens a checkbox menu. Select several
items without reopening it, search by ID or description, or select a whole source
group. Built-in and custom modules are grouped using catalog provenance; older
hosts without that metadata show an unclassified group.

**Select all** affects the current search results only. It preserves selections
outside the results and never selects future additions. **Clear** removes every
selected ID. **Done**, Escape and outside clicks close the menu while keeping the
draft; **Save preset** persists it. Inheritance remains a separate mode.

Existing module parameters survive bulk selection changes. Unlisted IDs remain
editable, and **Advanced: enter a module ID** retains manual entry without implying
that an ID installs a resource or grants authorization.

The mode picker is hidden by default. Enable **Show mode picker** on the Persona
management page to add it above the chat input in every pane. Settings search
also leads directly to this switch. Hiding it leaves the conversation's mode intact.

Everyday partner is the initial mode. Clicking a built-in card explicitly sets
this machine's new-task default, including local and remote tasks.

Saved server preferences alone do not
activate it. Showing the picker reads the catalog without binding a composition.
When hidden, it mounts no picker and makes no picker catalog reads; run status
and token counts remain visible.

An explicit mode in a new-task request overrides the card default. Restoring
history retains its original binding. Clicking a card leaves existing tasks
unchanged; pressing New may reuse an unused pane with the chosen default.

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

The catalog groups built-in, personal and project presets. New and duplicate
actions open a separate editor within Settings:

Built-ins use responsive cards with a new-task-default badge, canonical ID,
description, details, usage and copy actions. Personal/project entries remain
editable rows. Usage, details and copy do not change the new-task default.

| Mode | Behavior |
| --- | --- |
| Everyday partner (`builtin:general`) | Inherit current configuration for everyday code, file and research work. The stable ID preserves existing defaults. |
| Lightweight partner (`builtin:minimal`) | A concise system persona and terminal (`bash`), with native context compaction. Adds image reading (`view_image`) when the selected model reads images and the tool is enabled. |

The stable IDs preserve existing defaults. Retained conversations keep their
saved names, revisions and module requirements.

The persona reference is DeepSeek Harness commit
`687f282e8625c6b8d11ff58211a90524be2e4317`:
[standard][harness-standard] uses a short coding-agent identity, while
[minimal][harness-minimal] uses a short software-engineer identity and a reduced
tool set. Reasonix already uses that minimal opener.

Everyday partner inherits the user's persona; Lightweight
partner retains Reasonix's native system instructions rather than Harness's
complete-prompt replacement. This copy update changes no provider-visible prompt
or tool bytes.

[harness-standard]: https://github.com/deepseek-ai/deepseek-harness/blob/687f282e8625c6b8d11ff58211a90524be2e4317/packages/bundle/web-app/presets/standard.patch.yml

[harness-minimal]: https://github.com/deepseek-ai/deepseek-harness/blob/687f282e8625c6b8d11ff58211a90524be2e4317/packages/bundle/web-app/presets/minimal.patch.yml

Lightweight partner loads no custom skills, commands, MCP, extensions, hooks or subagents.
It retains the host controls needed to answer and ask questions. Its terminal
can still modify files and access the network under existing permissions and
sandbox rules; Lightweight partner is not a read-only mode.

Missing required tools produce an explicit dependency error. Lightweight partner
requires `bash`; its `view_image` member is optional, so a text-only model or a
configuration that disables image reading can still use the terminal.

Code review and Research are no longer built-in catalog entries. Their existing
conversations retain the original binding. An old default pointing to a retired
entry reports a diagnostic until an available default is explicitly chosen.

Copy a built-in to customize it. Each module group either follows current authorized
configuration, including resources enabled later, or uses only the selected IDs. An empty explicit
selection disables the group.

A selected tool member may declare `"optional": true`, for example
`{"id":"view_image","optional":true}`. It is admitted only if already available;
the declaration does not enable a disabled tool or grant image capability.

Omitting `optional` keeps the member required. Other module groups do not accept
optional members.

The editor lists locally available modules;
imports preview missing dependencies without installing or launching them.

Settings lists the page as Persona. The editor shows name, purpose and both
persona text fields directly, followed by the optional-runtime-context switch.

Tools and skills/extensions expand on demand. Storage scope and the generated
ID remain visible; both become read-only after saving.

A persistent footer offers Cancel and Save. Returning, closing Settings or
navigating elsewhere confirms unsaved changes; save errors retain the draft.

Everyday partner's user-config persona has a separate editor, accessible from Everyday partner
details or Settings search.

Saving creates a definition revision. Existing conversations retain their
resolved persona, parameters and permission rules. New conversations load current resources
on demand; existing snapshot conversations keep their saved resources.

The page displays the definition
revision beside the conversation's bound revision and reports stale-save
conflicts while preserving the draft. Models remain independently selectable.

## Files and retained resources

| Item | Location |
| --- | --- |
| Personal definitions | `<state>/agent-presets/definitions/<id>.json` |
| Shared definitions | `<workspace>/.reasonix/agent-presets/definitions/<id>.json` |
| Definition revisions and explicit defaults | `<state>/agent-presets/` |
| Studio new-task card choice | Browser-local `rx-new-task-agent-preset` |
| Local authorization records and retained files | `<state>/trusted/agent-presets/` |

Picker visibility reuses the original browser-local `rx-show-agent-composition`
key. Only `on` enables it; missing, `off`, unknown or unreadable values leave it
hidden. A failed write preserves the old value and reports an error.

The original reader follows the same `on`/`off` values; the intermediate
always-visible revision ignores this key. Other windows follow storage changes.
Visibility is independent of new-task card choices and existing bindings.

The new browser key accepts only the shipped Everyday partner/Lightweight partner identities.
It is separate from server preferences and never read by CLI or ACP.

| Stored choice | Current reader | Previous reader |
| --- | --- | --- |
| Missing, unreadable or unknown | Original Everyday partner behavior | Ignores the key |
| Everyday partner/Lightweight partner | Explicit choice on new Studio requests | Ignores the key |

An unsuccessful storage write retains the previous choice and reports an error.
Existing server selection/admission permissions still apply to new requests.

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

## Definition sources and authority

HTTP workspace selectors resolve to registered workspace values before native
session import probes the filesystem. Inherited dispatch bindings travel outside
the HTTP request DTO; the wire format is unchanged.

Personal declaration reads and retained-resource copies use directory handles.
Personal links may resolve inside their store, but not outside it. Generated
session-directory slugs and resource-relative paths are checked before joining
them to their owning directories.

Hook resource reads are bounded even if a file grows after inspection. Collection
capacity hints avoid unchecked size addition, and legacy text decoding checks
its expansion size before allocating.

Selecting a definition never grants permission. Built-ins are shipped, read-only
templates; user definitions are explicitly authored in personal state; project
definitions are untrusted repository files, even in a trusted workspace.

| Value | `builtin:` | `user:` | `project:` |
| --- | --- | --- | --- |
| Persona and tool/module selection | Shipped choices | User choices | Repository choices, only within existing user authorization |
| MCP `args`, `url`, `env`, `headers` | Shipped values only | Allowed; credential values remain references | Refused, including empty array/map overrides |
| MCP call/startup timeouts | Supported | Supported | Supported |
| Extension `args`, `env` | Shipped values only | Allowed; environment values remain references | Refused |
| Extension timeout | Supported | Supported | Supported |
| Extension executable, hook/command code or module installation | No new authority | Existing authorization still required | Cannot replace a launch or approve/install a program |

The current built-ins contain no MCP or extension overrides. Tools, skills,
hooks and commands do not accept member parameters in any scope.

Project definitions can select only resources already authorized by the user,
never widen capabilities or change a launch. Hook project-program approval and
its revocation checks still apply; selecting a preset is not an approval card.

Declaration reads, saves, imports, old revisions and executable binding restore
apply the same source policy before starting a module.

A previously retained project binding containing launch overrides remains
readable as history but cannot execute, even with an old local authorization
record. It is not silently rewritten. Valid timeout-only project bindings
retain their original version.

### On-demand conversations and existing snapshots

New bindings use `formatVersion: 2` and `resourceMode: "live"`. Selecting a preset
records permission rules, not resource file contents.

Skill discovery does not
capture template directories or complete every skill. Reading or invoking one
skill loads its body and the same references as an ordinary conversation.
Commands and non-blocking hooks use current authorized sources when invoked.

MCP directory reads do not connect an uncached server. An explicit capability
call connects after authorization; configured automatic connections remain
independent of selection. MCP and extension tools use the stable
`use_capability` entry, without changing the provider-visible schema.

Tool-only extension runtimes start on demand from their installed source, without
making a private package copy.

Providers, required runtimes and interceptors
initialize at the lifecycle stage that needs them. Changes to those startup
contracts require a new session or an explicit runtime reload; an existing
conversation never silently changes its system prompt.

Binding the preset does not approve a resource. Missing optional skills, MCP
servers and tool-only extensions do not prevent ordinary conversation; use of
that capability reports its failure. Revocation blocks subsequent calls.
Safety guards remain mandatory.

Bindings without the new fields retain snapshot behavior. Their inherited choices
resolved once at binding, and retained skill, non-blocking hook and extension
files do not follow subsequent source edits. They are not automatically converted.

Hooks retain referenced files and sibling bundles for common script formats.
Bundles must fit the resource limit and cannot contain credential files or
escaping or cyclic symbolic links. Contained source links are captured as regular
files; saved snapshot files must remain regular files.

Keep hook code in a dedicated directory; retain credentials
through environment references. External interpreters and services keep their
existing platform/authorization contracts.

Retention checks credentials at argument boundaries, preserving quoted Windows
file paths while rejecting literal credential arguments and unparseable shell launches.

Credential lookups remain
with the existing credential/configuration owner. Sessions use separate MCP
connections and process state. Existing snapshot extensions run from private copies. Existing
trust, revocation, authorization and sandbox checks still apply.

### Safety policy stays outside the composition

| Hook or rule | Can a composition deselect it? |
| --- | --- |
| `PreToolUse`, `UserPromptSubmit`, `PermissionRequest` hooks | No, in every scope, including imported Claude permission hooks |
| Permission `ask`/`deny` rules and remembered revocations | No; ordinary permission policy still judges the resolved call |
| Other Hook events | Yes; only selected non-blocking automation is retained |

Extensions selected by a bound composition are also tighten-only: neither a
permission interceptor nor a permission strategy may turn an `ask`/`deny` into
an allow. Ordinary unbound extension behavior keeps its existing contract.

Guards load from current authorized configuration at assembly and restore,
including for old bindings with an empty Hook selection. They also apply when
their installed package is outside the selected extension set.

Saving safety hooks updates the live shared runner, including child roles,
without replacing retained automation or the immutable composition digest.
An authorized policy edit may change a guard; selecting a composition cannot.

Project program approval and package activation still control which guards load.

A binding is local transcript metadata and never enters provider messages.
Snapshot compositions use native event-log schema 4; live compositions use schema 5.
Older readers refuse unsupported schemas instead of continuing with another tool set,
falling back to a stale checkpoint or rewriting the newer log.

Legacy transcripts keep
their compatibility path and are not assigned today's default on restore.

Untrusted or unavailable bindings can be opened for history without launching
modules; continuation is blocked. Deleting a definition does not remove its
conversation's retained binding.

Snapshot JSON exports use the `reasonix-native-session` version 2 envelope and
include the authoritative event-log binding and retained resources.

Live exports
use version 3 and contain history and permission rules, without local resource
files or credentials.

The current reader accepts both formats. Older clients
cannot continue a live-format conversation. Import
them under Settings → Storage. Import never creates local authorization records
or starts programs.

Configuration backup version 3 also includes personal
preset definitions and their default selection.

For snapshot conversations, MCP tool contracts are pinned at binding. A changed tool catalog after refresh
or reconnect blocks execution instead of silently replacing schemas.

Package
MCP processes and extension sidecars use private package copies; shutdown and
failed assembly release their private directories.

Live conversations instead
validate current authorization and server identity at dispatch.

ACP composition changes preserve the session's vetted additional directories.

A dispatched session inherits the exact binding instead of selecting today's
default. A binding scoped to one workspace cannot dispatch into another; the
request is refused before opening a runtime. Unbound Everyday partner dispatch keeps
the existing behavior.

A fresh dispatch never falls back to a history viewer
when its inherited binding cannot be assembled.

Moving a bound conversation to another project is refused before copying or
deleting it (`agent_composition.workspace_bound`). Start a new conversation in
the target project. Unbound conversations retain the ordinary move behavior.

The user persona is not migrated. `/persona` retains the user-config behavior
from #13588, and Everyday partner's settings reuse that Persona editor. Everyday partner reads
it on each runtime build. Explicit compositions
retain their selected persona; saving either configuration does not rewrite
an existing binding.

Prefix/suffix supplement first-party guidance; there is
no whole-prompt replacement switch.

## Entry points

CLI: `reasonix --agent-composition builtin:minimal`, or
`reasonix run --agent-composition user:my-agent "Review this change"`.
`/agent` lists compositions; `/agent user:my-agent` selects one in a blank
interactive conversation. `/preset` retains its balanced/delivery meaning.

ACP exposes an independent `agent_composition` configuration option and narrows
it to the current choice after the conversation starts.

Remote sessions use the
remote host's catalog, credentials and retained resources. New remote conversations
send the selected composition ID to that host; local executable resources are not uploaded.

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
| `POST /tree/sessions/import-native?workspace=...` | Original file as an `application/json` body → imported `{path, agentComposition}` |
| `POST /tree/sessions/move-to` | `{path, target}` → moved unbound conversation; bound conversations are refused |

The renderer uploads the archive as an opaque file and never reads or parses it.
The host uses the same native decoder and its 192 MiB / replay-record limits.

| Refusal | Code |
| --- | --- |
| Malformed | `session.archive_invalid` |
| Unsupported version | `session.archive_unsupported` |
| Byte or record limit | `session.archive_too_large` |

The original JSON `{workspace, content}` request remains supported on this same
route. Neither transport grants trust or starts programs.

The open conversation-import proposal
[#13728](https://github.com/esengine/DeepSeek-Reasonix/issues/13728) should reuse
this decoder and limits rather than introduce a second parser; this entry will
move to that common flow when it lands.

Preset save/delete/default/import and session composition selection require the
request's provider-edit grant, just like configuration writes. Without it they
return HTTP 403 `config.editing_disabled` before store access or selection.

Paired devices do not inherit the host's grant. Catalog/detail/export and current
binding reads remain available.

The remote-pane proxy refuses paired-device preset mutations before forwarding
under the host's remote credential. Reads and the host's own requests still reach
the remote kernel, which checks its grant independently.

`/status` and runtime listings include `agentComposition`. Refusals use stable
`agent_composition.invalid`, `.not_found`, `.read_only`, `.conflict`, `.locked`
and `.unavailable` codes. The Studio catalogs translate these into Chinese and
English.

Preset names, revisions and diagnostics do not enter the cached
provider prefix. First use of a different composition can have a cold cache;
editing a preset never rewrites a running conversation's prefix.

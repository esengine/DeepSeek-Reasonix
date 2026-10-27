---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-29
---

# Reasonix Extension Protocol v2

The Extension Protocol is the stable wire contract between Reasonix (the **host**) and code extensions running as out-of-process **sidecars**.

It is how an installed plugin with a `runtime` block intercepts runtime events, owns replacement strategies, contributes streaming model providers, and publishes structured UI — without ever linking into the host binary.

- Protocol ID: `reasonix.extension.v2`
- Machine-readable schema: `internal/ext/extension/protocol/schema.generated.json`
- Method/event/limit/error index: `docs/EXTENSION_PROTOCOL.generated.md`
  (generated, drift-checked in CI)
- Go SDK (implements everything below): `sdk/go`

This document is the prose companion to the generated index. Where they
disagree, the generated schema wins.

## Transport

- Strict JSON-RPC 2.0 over **NDJSON**: one complete JSON object per line on
  stdin/stdout. stderr belongs to the extension for diagnostics; the host
  captures a bounded, credential-redacted tail for errors.
- Frames are capped at **8 MiB** in both directions; oversized frames are a
  connection-fatal `frame_too_large` error.
- Request IDs are integers. `params` must be an object. Unknown members are
  tolerated at the frame level; DTO decoding is strict (unknown fields are
  rejected) so typos surface immediately.

## Lifecycle

1. The host spawns the sidecar (exec form, no shell) and sends `extension/initialize` first. The params carry the manifest expectation: the intercepts, replaces, providers, and UI actions the host will accept.
   - For one runtime generation, the host initializes at most four sidecars in parallel under one shared 30-second startup budget.
2. The sidecar answers with its declaration. The host validates it: exact
   protocol major version, and every subscription, replacement slot,
   provider, and UI action must be a **subset of the plugin manifest**.
   Anything beyond the manifest fails the handshake with
   `capability_not_declared`.
3. The host sends `extension/initialized`. Any extension-to-host traffic
   before this point poisons the connection.
4. Shutdown is bounded: `extension/shutdown` with a timeout, then stdin
   closes, then the process tree is killed if the sidecar does not exit.
5. Crashes: a sidecar that dies cancels all of its pending RPCs. If it owned
   the currently selected provider or a replacement slot, the current
   operation fails explicitly — the host never silently falls back to another
   model or strategy. A crashed sidecar is only restarted by an idle-time
   runtime reload.

## Content references

Payload fields marked externalizable that exceed **64 KiB** are offloaded into the host content store: the frame carries an `ExternalizedField` descriptor (JSON pointer, content ref, byte count, SHA-256) and a `null` placeholder.

The peer pages the bytes back with `host/content/read` in **256 KiB** chunks, verifying byte count and hash. A single content object is capped at **8 MiB**. Unknown or expired refs fail with `content_ref_expired`.

## Interception

Seventeen frozen hook points (see the generated index). `extension/intercept`
is blocking; `extension/event` is fire-and-forget observation of the same
points. Event delivery uses a bounded non-blocking writer queue: saturation
drops the observation with a warning instead of stalling the Agent.

- Ordinary interceptors run **sequentially** in a deterministic order:
  priority ascending (manifest `priority`, -1000..1000, default 0), then
  plugin ID, then registration order.
- Decisions per call: `continue` (pass the payload along), `block` (abort the operation with a user-visible reason), `replace` (substitute the payload — the host re-validates it against the point's DTO and schema before use), and `allow`/`deny` (only legal at `permission.decision`).
- A `block` at `tool.before` may carry an `answer`: the tool does not run and the text stands in for its output, labelled by the host as external data from the extension. An answer anywhere else is a contract violation.
- At `input.receive` a replacement may add `context` blocks (`label`, `text`). They follow the input on the turn's tail under a host-written header naming the plugin that added them, bounded by the user's ceilings; a later extension may add blocks but not change an earlier one's.
- A full-trust `allow` overrides a host deny and is audited.
- Replacement **strategy slots** (`system_prompt`, `context`, `provider_request`, `provider_response`, `compaction`, `session_policy`, `permission`, `frontend_events`, `tool:<name>`, `provider:<ref>`) have exactly one owner across all installed plugins.
- The chain runs first; the slot owner gets the final say. A strategy owner's timeout or error always fails the operation.
- Timeouts: input/tool/permission points default to 5s; the
  session/context/compaction/system-prompt family to 30s; a manifest may tune
  per-runtime up to a 60s ceiling. Optional observation-only extensions that
  time out are warned about once and skipped; required extensions and slot
  owners fail the operation.

## Streaming providers

An extension with the `providers` capability answers
`extension/provider/catalog` with descriptors equivalent to host providers
(models, context windows, pricing, vision, reasoning, effort) — never
credentials. Models appear as `plugin/<plugin>/<provider>/<model>`.

Streams follow `extension/provider/stream/open` → `stream/chunk` →
`stream/end`:

- Chunks carry a 1-based contiguous sequence number; `stream/end.lastSeq`
  freezes the terminal boundary. The host buffers out-of-order chunks,
  drops duplicates, and fails the stream as interrupted naming the missing
  sequence when a gap persists.
- Chunk types: `text`, `reasoning` (with `signature`), `tool_call_start`,
  `tool_call_args_delta`, `tool_call`, `usage` (including cache tokens),
  `done`, `error`. Provider errors must be redacted by the producer and are
  defensively redacted again by the host.
- Cancelling the stream context sends `stream/cancel`; the sidecar must stop
  producing chunks.
- The extension reads its own environment and credentials; the host never
  sends another provider's API keys or headers. A crashed provider never
  triggers fallback to a different model.

## Structured UI

Extensions with the `session.read` capability read the session (`host/session/read`: `usage` or `messages`, redacted and bounded by the user's ceilings) and those with `session.submit` queue a prompt (`host/prompt/submit`); neither implies the other.

A queued prompt is the host's message about the extension, labelled with its source, never the user's words; the host may refuse it with a `refused` identity in the result (`submit_per_turn`, `submit_bytes`, `no_session`).

Extensions with the `ui` capability publish `status`, `card`, `form`, `notification`, `panel`, `view` and `frame` payloads (`host/ui/publish`) and ask questions (`host/ui/request`: confirm, input, select, multiselect).

Protocol messages are **structured only**: no HTML, CSS, JavaScript, remote scripts, arbitrary frontend components, or uncontrolled URLs; Markdown renders through each frontend's existing safe renderer. A `frame` names a document the installed package ships; the markup never crosses the protocol (see Frames).

Every surface update carries the plugin ID, surface ID, session ID, and runtime generation; stale-generation updates are dropped so late results after a tab switch or reload can never overwrite current state.

A `panel` holds a place in the frontend's side rail rather than scrolling
past in the transcript: re-publishing the same surface id updates what is
already on screen. A `view` is composed rather than filled in: its payload is a
tree of host primitives, for a shape the fixed kinds do not have.

A `view` names where it would like to stand in `slot` (at most 64 bytes), or replaces a tool card's body through `anchor: "tool:<callId>"`, never both. Slot names are advisory, not an enum: a frontend maps the names it has, and every other name falls back to `rail`.

| Slot | Studio draws it as | Capacity in Studio |
| --- | --- | --- |
| `rail` | The workbench's Extensions tab, beside the extension panels | unbounded |
| `composer.above` | The band above the composer | 2 |
| `session.header` | The title bar after the session name; only `text`, `pip`, `meter` and `button` are drawn, any other node as its text | 3 |
| `pane.tab` | A workbench tab of its own, after every tab a person or the agent opened; closing it moves the view to `rail` | 4 per session |

1. Views beyond a slot's capacity stand in `rail`. Within a slot, views are ordered by plugin ID, then surface ID, so a republish never moves one.
2. A user's placement overrides the extension's request. Studio stores it in `desktop.surface_slots`.
3. `above_prompt` is an alias of `composer.above`. `settings.section` and `home.card` are reserved for app-scoped surfaces, which the protocol cannot publish yet, and fall back to `rail`.
4. ACP clients that negotiated `reasonix.extensionSurface` receive a view with its `slot` and place it themselves. Other ACP clients receive no text for a view.

Actions declared at initialize are namespaced `/<plugin>:<action>` and are
invoked via `extension/ui/action`; form submissions arrive via
`extension/ui/submit`.

### Frames

A frame is an HTML document the package declares under `contributes.frames` (`{id, entry, title}`) and the host draws in an isolated frame. Publishing one sends only JSON:

| Field | Rule |
| --- | --- |
| `frame` | A declared id, `^[a-z][a-z0-9-]{0,31}$`; an undeclared one fails with `capability_not_declared`. |
| `slot` | As for a view, except `session.header`, which falls back to `rail`. Frames take no `anchor`. |
| `title` | Required, at most 200 characters. |
| `fallback` | Required plain text, at most 600 bytes: what a frontend that cannot draw a frame shows instead. |
| `props` | Optional JSON object, at most 64 KiB. Every string in it is credential-redacted before it reaches a frontend. |

1. `HostCapabilities.uiFrames` is true when the host accepts frame surfaces; `ManifestExpectation.uiFrames` lists the declared ids.
2. A session keeps at most six live frame surfaces; admission and storage share one lock. Changing another surface kind to a frame consumes a slot; republishing an existing live frame does not.
3. The host serves the document at `GET /extensions/frames/{plugin}/{frame}?generation=N`, only for the current generation of a live plugin, from the package the runtime runs.
4. The response's CSP is `sandbox allow-scripts` with `default-src 'none'`, `connect-src 'none'`, `form-action 'none'` and `frame-ancestors 'self'`.
5. The document must be self-contained: inline scripts and styles, images and fonts as `data:` URIs, at most 2 MiB of UTF-8.
6. Refusals are typed: `ui.frame_stale`, `ui.frame_not_declared`, `ui.frame_missing`, `ui.frame_outside_root`, `ui.frame_too_large`, `ui.frame_not_utf8`.
7. ACP and terminal frontends show `title` and `fallback` as text.

The document talks to its host over `reasonix.frame.v1`. The host posts `{type: "connect", protocol}` with a `MessagePort`; every later message uses that port.

| Direction | Message | Limits |
| --- | --- | --- |
| host → frame | `init {protocol, props, theme {scheme, tokens}, locale, readOnly, state?, reason}` | once per load, after `ready`; `reason` is `publish` or `remount` |
| host → frame | `props {props}`, `theme {theme}` | the document is not reloaded |
| frame → host | `ready {}` | first message, accepted once per document load |
| frame → host | `size {height}` | ≤ 10/s, clamped to the place's range |
| frame → host | `action {actionId, args?}` | a declared action of the same plugin; `args` ≤ 16 string pairs of ≤ 1 KiB; ≤ 5/s |
| frame → host | `submit {values}` | delivered as `extension/ui/submit` for the frame's surface; ≤ 64 KiB; ≤ 1/s |
| frame → host | `state {value}` | ≤ 16 KiB, ≤ 10/s; kept within the owning pane/runtime and handed back on remount |

Any other message, an extra field or an over-limit value is dropped; three drops in ten seconds close the channel and the host shows the fallback. No message submits a prompt, answers an approval, changes a setting, reads the session or opens a URL.

Bridge values must be JSON, with at most 32 nested levels and 4096 visited values; byte limits use UTF-8. Cycles, BigInt and non-JSON objects are dropped.

Document identity includes the host, session, generation, plugin, surface and frame. Replacing it closes the old channel; retiring the surface clears its saved state. Moving a live surface within its pane preserves state.

桥接消息只接受 JSON，最多嵌套 32 层、访问 4096 个值，字节限制按 UTF-8 计算；循环引用、BigInt 和非 JSON 对象会被丢弃。

文档身份包含宿主、会话、代次、插件、界面和文档 ID；身份变化立即关闭旧通道，界面退役清除保存状态，同一窗格内移动仍保留状态。`ready` 每次加载只接受一次；六个界面的配额检查与写入原子执行，其他类型改成 frame 也占用配额。

Native validation: build the Studio host and frontend, then run `cd desktop/electron && pnpm frame-isolation-live`. It installs the SDK example in a disposable home and drives the real kernel, sandbox, CSP, action path, reload and malformed-message shutdown.

Set `REASONIX_FRAME_EVIDENCE` to an external directory to capture the running frame.

原生验证：构建 Studio 宿主与前端后执行 `cd desktop/electron && pnpm frame-isolation-live`。脚本使用临时数据目录安装 SDK 示例，验证真实内核、沙箱、CSP、动作调用、重载和非法消息关闭；可设置 `REASONIX_FRAME_EVIDENCE` 到仓库外目录保存截图。

## Tools

A runtime with the `tools` capability serves model-callable tools:

- **Declared in the manifest.** Each tool's name, description and input
  schema sit under `runtime.tools`, so the host knows them before the process
  starts and an install plan shows them.
- **Activated at initialize.** `InitializeResult.tools` names the declared
  tools this process serves; a name the manifest does not declare fails the
  handshake with `capability_not_declared`. `ManifestExpectation.tools`
  tells the extension which names the host will accept.
- **Called through the host.** The model sees each tool as
  `ext__<plugin>__<tool>` and reaches it through the capability catalog, as
  it does MCP tools, so installing a plugin never moves the cached prompt
  prefix. Every call passes the host's permission check first.
- **Answered by `extension/tool/call`.** Params carry the declared `name`,
  the model's `arguments` and a `timeoutMillis` budget (5 minutes today).
- **Two kinds of failure.** The result's `content` is what the model reads,
  and `isError` marks a failure the tool reports about its own work. A call
  the extension cannot run at all answers with a protocol error instead.

## Errors

Domain errors travel as JSON-RPC error code `-32000` with structured data
(reason, retryable, action); `protocol_error`, `unknown_method`,
`invalid_params`, and `internal` use the standard JSON-RPC codes. The frozen
reason table lives in the generated index.

## Stability contract

Within major version 2, the only permitted evolutions are: new optional fields, new enum values, and new methods. Existing required fields, directions, limits, error reasons, and semantics never change.

The canonical schema and its SHA-256 hash are produced by `cmd/extension-protocol-gen`; CI's `go test ./...` enforces this via the deterministic-generation test (`TestGeneratedArtifactsAreDeterministicAndCommitted`), so any drift — including an accidental semantic change — fails the build.

## Security model

A code extension is **full trust**: it runs outside the Reasonix sandbox with the unfiltered inherited environment, can read the full session and environment, can bypass permissions, and can operate the machine directly.

Installing, updating, replacing, or `--link`ing a plugin with a `runtime` block is the authorization — there is no second confirmation. Only plugins installed through the plugin flow (recorded in `plugin-packages.json`) can start a sidecar; project configuration can never declare one.

Before any sidecar diagnostics, structured UI, interceptor reasons, or provider errors reach the UI, logs, or error surfaces, the host runs its credential redaction pass. Ordinary provider/model content is preserved as product data.

The install preview, plugin details, and capability diagnostics always display the FULL TRUST block for runtime plugins.

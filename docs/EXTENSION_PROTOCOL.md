# Reasonix Extension Protocol v2

The Extension Protocol is the stable wire contract between Reasonix (the
**host**) and code extensions running as out-of-process **sidecars**. It is
how an installed plugin with a `runtime` block intercepts runtime events,
owns replacement strategies, contributes streaming model providers, and
publishes structured UI — without ever linking into the host binary.

- Protocol ID: `reasonix.extension.v2`
- Machine-readable schema: `internal/extension/protocol/schema.generated.json`
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

1. The host spawns the sidecar (exec form, no shell) and sends
   `extension/initialize` first. The params carry the manifest expectation:
   the intercepts, replaces, providers, and UI actions the host will accept.
   For one runtime generation, the host initializes at most four sidecars in
   parallel under one shared 30-second startup budget.
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

Payload fields marked externalizable that exceed **64 KiB** are offloaded
into the host content store: the frame carries an `ExternalizedField`
descriptor (JSON pointer, content ref, byte count, SHA-256) and a `null`
placeholder. The peer pages the bytes back with `host/content/read` in
**256 KiB** chunks, verifying byte count and hash. A single content object is
capped at **8 MiB**. Unknown or expired refs fail with `content_ref_expired`.

## Interception

Seventeen frozen hook points (see the generated index). `extension/intercept`
is blocking; `extension/event` is fire-and-forget observation of the same
points. Event delivery uses a bounded non-blocking writer queue: saturation
drops the observation with a warning instead of stalling the Agent.

- Ordinary interceptors run **sequentially** in a deterministic order:
  priority ascending (manifest `priority`, -1000..1000, default 0), then
  plugin ID, then registration order.
- Decisions per call: `continue` (pass the payload along), `block` (abort
  the operation with a user-visible reason), `replace` (substitute the
  payload — the host re-validates it against the point's DTO and schema
  before use), and `allow`/`deny` (only legal at `permission.decision`).
  A full-trust `allow` overrides a host deny and is audited.
- Replacement **strategy slots** (`system_prompt`, `context`,
  `provider_request`, `provider_response`, `compaction`, `session_policy`,
  `permission`, `frontend_events`, `tool:<name>`, `provider:<ref>`) have
  exactly one owner across all installed plugins. The chain runs first; the
  slot owner gets the final say. A strategy owner's timeout or error always
  fails the operation.
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

Extensions with the `ui` capability publish `status`, `card`, `form`, and
`notification` payloads (`host/ui/publish`) and ask questions
(`host/ui/request`: confirm, input, select, multiselect). Surfaces are
**structured only**: no HTML, CSS, JavaScript, remote scripts, arbitrary
frontend components, or uncontrolled URLs; Markdown renders through each
frontend's existing safe renderer. Every surface update carries the plugin
ID, surface ID, session ID, and runtime generation; stale-generation
updates are dropped so late results after a tab switch or reload can never
overwrite current state.

Actions declared at initialize are namespaced `/<plugin>:<action>` and are
invoked via `extension/ui/action`; form submissions arrive via
`extension/ui/submit`.

## Errors

Domain errors travel as JSON-RPC error code `-32000` with structured data
(reason, retryable, action); `protocol_error`, `unknown_method`,
`invalid_params`, and `internal` use the standard JSON-RPC codes. The frozen
reason table lives in the generated index.

## Stability contract

Within major version 2, the only permitted evolutions are: new optional
fields, new enum values, and new methods. Existing required fields,
directions, limits, error reasons, and semantics never change. The canonical
schema and its SHA-256 hash are produced by `cmd/extension-protocol-gen`;
CI's `go test ./...` enforces this via the deterministic-generation test
(`TestGeneratedArtifactsAreDeterministicAndCommitted`), so any drift —
including an accidental semantic change — fails the build.

## Security model

A code extension is **full trust**: it runs outside the Reasonix sandbox
with the unfiltered inherited environment, can read the full session and
environment, can bypass permissions, and can operate the machine directly.
Installing, updating, replacing, or `--link`ing a plugin with a `runtime`
block is the authorization — there is no second confirmation. Only plugins
installed through the plugin flow (recorded in `plugin-packages.json`) can
start a sidecar; project configuration can never declare one. Before any
sidecar diagnostics, structured UI, interceptor reasons, or provider errors
reach the UI, logs, or error surfaces, the host runs its credential redaction
pass. Ordinary provider/model content is preserved as product data. The
install preview, plugin details, and capability diagnostics always display the
FULL TRUST block for runtime plugins.

# ACP editor integration

<a href="../README.md">README</a>
&nbsp;·&nbsp;
<a href="./GUIDE.md">Guide</a>
&nbsp;·&nbsp;
<a href="https://agentclientprotocol.com/">ACP specification</a>

Reasonix implements Agent Client Protocol (ACP) v1 as an NDJSON JSON-RPC 2.0
agent over standard input and output. Editors and other ACP hosts launch the
process, open one or more workspace-scoped sessions, and receive streamed
messages, tool activity, plans, permission requests, and configuration updates.

Session status usage objects may include structured `costQuote` (original
currency, `originalTotals`, identity/official-table valuations,
`costComplete`, `displayComplete`, `displayStatus`, and `billingMode`) alongside legacy
`estimatedCost` / `currency` aliases that mirror the selected display valuation.
See [Billing](./BILLING.md).

## Start the agent

An ACP host should launch one of these commands:

```sh
reasonix acp
reasonix acp --model deepseek-pro
reasonix acp --preset delivery
```

`--model` selects the startup model when the client does not override it.
`--preset` sets the startup execution setting to `light`, `balanced`, or `delivery`
(default `balanced`). Legacy `--profile economy|balanced|delivery` still works
(`economy` → `light`). Both remain session-configurable after initialization.

Standard output is reserved for ACP messages. Reasonix sends diagnostics to
standard error, so hosts must not merge the two streams. Run `reasonix setup`
beforehand when no provider is configured; the initialize response also
advertises a terminal authentication method that launches `reasonix setup`.

## Initialize and negotiate capabilities

Clients should call `initialize` before opening a session. Reasonix advertises
the following capability shape (irrelevant fields omitted):

```json
{
  "protocolVersion": 1,
  "agentCapabilities": {
    "loadSession": true,
    "sessionCapabilities": {
      "list": {},
      "resume": {},
      "close": {},
      "delete": {},
      "additionalDirectories": {}
    },
    "promptCapabilities": {
      "image": true,
      "audio": false,
      "embeddedContext": true
    },
    "mcpCapabilities": {
      "http": true,
      "sse": false
    },
    "_meta": {
      "reasonix.io": {
        "sessionSteer": {
          "method": "_reasonix.io/session/steer"
        }
      }
    }
  }
}
```

When the client advertises `fs.readTextFile`, `fs.writeTextFile`, or
`terminal`, Reasonix routes eligible file operations through the editor's
unsaved buffers and eligible foreground commands through a client-owned
terminal. Every file tool takes part — reads, edits and writes alike — so an
edit applies to what the editor currently shows instead of to the last saved
copy on disk. A non-UTF-8 file is not eligible: the ACP file methods are
text-only, so it stays on the local encoding-preserving path and keeps its
original charset. Without those client capabilities, the normal workspace
tools run locally inside the Reasonix process.


### Authentication

`session/new` fails with the ACP "Authentication required" error (`-32000`) when
the selected model has no API key or no provider is configured, per the [ACP schema](https://agentclientprotocol.com/protocol/v1/schema) and
[authentication](https://agentclientprotocol.com/protocol/v1/authentication) pages:

- the advertised `reasonix-setup` method is of type `terminal`, so a client runs `reasonix setup` interactively and then reconnects; it must not send `authenticate` for it;
- provider keys are read from `<Reasonix home>/.env`, never from the host's environment variables, so an `env` block on the host side cannot supply one.

## Session lifecycle

Each ACP session owns an independent Reasonix controller, workspace root, model,
execution setting (agent preset), collaboration mode, approval mode, MCP set, and
persisted transcript. State does not leak between sessions.

| Method | Behavior |
| --- | --- |
| `session/new` | Opens a session for an absolute `cwd` and returns its configuration state. |
| `session/load` | Opens a persisted ACP session and replays its transcript as `session/update` notifications. |
| `session/resume` | Opens a persisted session without replaying the transcript. |
| `session/prompt` | Runs one turn and streams updates until it returns a stop reason. |
| `session/cancel` | Cancels the active turn; this is a notification. |
| `session/list` | Lists live and persisted ACP sessions, optionally filtered by absolute `cwd`. |
| `session/close` | Stops a live session and releases resources without deleting history. |
| `session/delete` | Stops the session and removes its persisted ACP history. |

`session/new`, `session/load`, and `session/resume` may include `mcpServers`.
Reasonix accepts stdio, Streamable HTTP, and legacy SSE servers. ACP's official `[{"name":"...","value":"..."}]`
shape is supported for stdio `env` and HTTP `headers`; the older object-map
shape remains accepted for compatibility.

`session/new`, `session/load`, and `session/resume` may also include
`additionalDirectories`: absolute folders that join the session's workspace as
write roots.

- They hold for that session only; nothing is written to the user's folder grants.
- Each folder gets the vetting a user-granted root gets (it must exist, belong to
  the user, hold no keys, not be a link to one, and not overlap another write root).
- One refused folder refuses the whole request with `-32602` and `data.code`
  naming the reason (for example `workspace_roots.protected`); nothing is skipped silently.
- At most 32 folders. An agent started to write only inside the cwd refuses them
  (`acp.additional_directories.disabled`).
- Resuming an already-open session with a different set is refused
  (`acp.additional_directories.session_open`); close it first.

## Session controls

Reasonix exposes independent controls instead of combining unrelated choices in
one mode selector:

| Control | Values | Wire surface |
| --- | --- | --- |
| Collaboration mode | `normal`, `plan`, `goal` | `modes` and `session/set_mode` |
| Model | Configured `provider/model` entries | `configOptions` with id `model` |
| Reasoning effort | Provider-supported levels or `auto` | `configOptions` with id `effort` |
| Execution setting | `light`, `balanced`, `delivery` | `configOptions` with id `agent_preset` (legacy id `work_mode` still accepted: `economy` → `light`) |
| Tool approval | `ask`, `auto`, `yolo`; also `readOnly`, `dontAsk` with `--permission-mode` | `configOptions` with id `tool_approval` |

Use `session/set_config_option` for model, effort, execution setting, and tool approval.
Its parameters are `sessionId`, `configId` and `value`, where `configId` is the
`id` of the option as advertised in `configOptions`:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/set_config_option",
  "params": {
    "sessionId": "session-id",
    "configId": "tool_approval",
    "value": "yolo"
  }
}
```

Note that the field is `configId`, not `optionId`. The result is the full
refreshed `configOptions` array. An unknown id returns `-32602 InvalidParams`.

Model and effort changes rebuild the session controller while preserving its
history and the other axes. Execution-setting (`agent_preset`) and tool-approval
changes update the gate in place without rebuilding the controller.

For older clients, `session/set_model` remains available. The legacy
`session/set_mode` values `default` and `auto` are also accepted as Normal + Ask
and Normal + Yolo respectively; new clients should use the independent
selectors above.

### Fixing the posture: `--permission-mode`

`reasonix acp --permission-mode <mode>` starts every session in that tool
approval posture and makes it the most permissive one a client can select.

- The modes are those of `reasonix run`: `read-only`, `dontAsk`, `manual`/`ask`,
  `auto`, `acceptEdits` (ask, with file edits allowed) and `bypassPermissions`.
  `plan` needs an interactive session and is refused.
- Without the flag nothing changes: sessions start in `ask` and the client may
  pick any of `ask`, `auto`, `yolo`.
- The order, lowest first, is `readOnly`, `dontAsk`, `ask`, `auto`, `yolo`. A client
  may move down it but not up.
- `session/set_config_option` and the legacy `session/set_mode` answer a raise with
  `-32602`, `data.code` `acp.tool_approval.above_ceiling` and `data.ceiling`; the
  posture stays as it was.
- A saved session whose file holds a higher posture opens at the ceiling. The
  file keeps the higher value until the client chooses a posture itself.
- The flag is read from the command line only, so a project file cannot loosen it.
- `bypassPermissions` as the ceiling is the same as no ceiling: nothing sits above `yolo`.
- A posture is not an OS sandbox. The `auto` and `yolo` ceilings do not themselves
  confine shell commands; macOS and Linux rely on the OS sandbox for that.
- Windows has no OS-level shell sandbox, so a shell command the posture lets through
  can write outside the workspace there. Do not rely on a posture for confinement on Windows.

The ceiling limits the posture, not the answers to approval cards. Under `ask`
the client can still approve a card; a host that must never approve (for example
DeepSeek Harness with `permission: reject`) is what keeps `ask` from writing.

## Prompts, updates, and approvals

`session/prompt` accepts text blocks, image blocks, and embedded resources.
Audio is not advertised.

- An image block or a binary (`blob`) resource is saved under
  `.reasonix/attachments/` in the session's workspace and referenced from the
  prompt, the same way a pasted attachment is in the other frontends.
- Data that is not base64, or an image that is not a supported image, fails
  the prompt with invalid params instead of being dropped.

During a turn, Reasonix may send:

- agent message and thought chunks;
- pending and completed tool-call updates;
- complete plan updates derived from `todo_write`;
- available slash commands;
- current-mode and configuration-option updates; and
- `session/request_permission` requests for permission-gated tools and user
  questions.

Hosts should keep the `session/prompt` request open until Reasonix returns its
stop reason, while continuing to process requests and notifications in both
directions.

- `agent_message_chunk` updates carry an opaque `messageId`.
  - Chunks of one sampling attempt share it; a retried attempt gets a new one,
    so a client never joins a discarded draft to the answer.
  - A chunk outside any attempt carries none.
- `tool_call` updates and permission requests carry the tool's own `name` beside
  its `title`. The name is informational: it grants nothing and is not changed
  later.
- `$/cancel_request` with `{"requestId": <id>}` cancels that one in-flight request
  without ending the session.
  - It is answered with the request's own result if it has one, else with error
    `-32800` (Request Cancelled).
  - An id that is unknown or already answered is ignored.
  - `session/cancel` still stops the active turn.
- An MCP server's form or external-page request is sent as `elicitation/create`
  when the client advertised `clientCapabilities.elicitation.form` or `.url`.
  - A mode counts only when its key is present and not null; otherwise the
    request is declined, as before.
  - A form is never offered for a request that needs a page, and a page is never
    offered to a client without `url`.
  - The `message` begins "MCP server <name> asks:", so the person can tell the
    server's words from the agent's. Control characters are removed and the text
    is capped.
  - The form's `requestedSchema` is a flat object: a string, an enum, or an array
    of an enum per question.
  - A value outside its enum, of the wrong shape or over the cap declines the
    whole form rather than reaching the server.
  - A page is an `http(s)` address the host already validated. The client should
    show it and ask before opening it.
  - `accept` for a page means the person agreed to open it, not that the
    interaction finished.

### Vendor notifications are opt-in

Reasonix pushes `_reasonix.io/session/status_update` and `_reasonix.io/session/mcpStatus_update`
only to a client that declared them in its `initialize` request:

```json
{"clientCapabilities": {"_meta": {"reasonix.io": {"statusUpdates": {"supported": true}}}}}
```

- without the declaration, those two notifications are never sent;
- the matching requests, `_reasonix.io/session/status` and `_reasonix.io/session/mcpStatus`, work either way;
- the on-disk transcript of a turn is reported as `_meta["reasonix.io"].transcriptPath` in the `session/prompt` result.

### When a turn fails

A turn that fails after it was admitted ends `session/prompt` with a JSON-RPC
error, not with a stop reason:

- the cases are an unreachable model endpoint or a provider that refuses the request;
- the [ACP prompt-turn specification](https://agentclientprotocol.com/protocol/prompt-turn)
  allows only `end_turn`, `max_tokens`, `max_turn_requests`, `refusal` and `cancelled` as stop reasons.

```json
{"error": {"code": -32603, "message": "<cause, secrets redacted>", "data": {"code": "turn_failed"}}}
```

`data.code` is the identity a client keys on; `message` is the host's
description of the cause, clipped to 2048 bytes.

### When the host pauses a turn

A turn that stops before delivery is verified (for example, files were written and no
verification command ran) is not a failure and not a completion:

- `session/prompt` returns `stopReason: "end_turn"`;
- the response carries the typed outcome in `_meta["reasonix.io"].turnOutcome`, e.g. `{"kind": "paused", "reason": "…"}`;
- the streamed text ends with a host-written `[paused] Not verified, …` line, so a client that reads only message text does not take the work for done.

## Mid-turn steering extension

Reasonix exposes mid-turn guidance as an ACP v1 vendor extension. It is not a
core ACP method, and it is not the still-unreleased ACP v2 `session/inject`
proposal.

### Discover support

Read the method name from:

```text
agentCapabilities._meta["reasonix.io"].sessionSteer.method
```

Do not assume the extension exists, and do not call the unnamespaced
`session/steer` name. ACP reserves non-underscore method names for the core
protocol.

### Send guidance

Call the advertised method while `session/prompt` is active:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "_reasonix.io/session/steer",
  "params": {
    "sessionId": "session-id",
    "prompt": [
      {"type": "text", "text": "use email instead of username"}
    ]
  }
}
```

A persistent session returns an item id and disposition:

```json
{"itemId":"inbox-item-id","disposition":"steer_accepted"}
```

Reasonix durably commits the guidance before returning. `steer_accepted` means
the active turn accepted it; `queued_followup` means that admission lost a race
or no turn was active, so the same item remains queued for a later turn. A
pathless compatibility session may omit `itemId` and still returns
`steer_accepted`. Applied guidance is persisted in normal history; transcript
replay shows the original user text, not Reasonix's internal steer marker.

| Condition | JSON-RPC result |
| --- | --- |
| Active prompt accepted durable guidance | `{"itemId":"...","disposition":"steer_accepted"}` |
| Guidance persisted but active admission was rejected | `{"itemId":"...","disposition":"queued_followup"}` |
| Unknown session or empty prompt | `-32602 InvalidParams` |
| Pathless compatibility session has no active prompt | `-32600 InvalidRequest` |
| Client calls `session/steer` | `-32601 MethodNotFound` |

On `InvalidRequest`, the compatibility session did not queue the guidance.

## Durable session inbox extension

Discover the versioned queue at
`agentCapabilities._meta["reasonix.io"].sessionInbox`. Schema version 1
advertises method names in its `methods` map; clients must use those advertised
names rather than constructing vendor method strings.

| Key | Purpose | Main parameters |
| --- | --- | --- |
| `enqueue` | Persist a follow-up or steer | `sessionId`, `text`, optional `intent`, `idempotencyKey` |
| `list` | Read metadata, capacity, pause and recovery state | `sessionId` |
| `get` | Read one full envelope on demand | `sessionId`, `itemId` |
| `update` / `delete` | Edit or delete pending work | `sessionId`, `itemId` |
| `move` | Reorder pending work | `sessionId`, `itemId`, zero-based `toIndex` |
| `setPaused` | Pause or resume dispatch | `sessionId`, `paused` |
| `retry` / `refresh` | Retry uncertain work or re-freeze references | `sessionId`, `itemId` |

`enqueue` returns `itemId`, `disposition`, `position`, `paused`, and
`idempotent`. List responses contain previews and byte counts, never prompt
bodies. A recovered inbox is paused; clients should let users inspect it before
calling `setPaused` with `false`.

### When an inbox call is refused

A refused inbox call answers with an implementation-defined JSON-RPC code and a
`data.code` a client branches on; `message` stays Reasonix's English, which
clients still show. The identities are the HTTP frontend's, so one vocabulary
names the queue's conditions on both surfaces.

```json
{"error": {"code": -32020, "message": "inbox item not found", "data": {"code": "inbox.not_found"}}}
```

`-32020` is "the queue refused this call" and `-32021` is "the work may already
have run". The class is the client's instruction:

- **terminal** — the same call cannot start succeeding; repair the client's view
  instead of retrying it;
- **retry-after-repair** — the same call succeeds once the named condition is
  repaired;
- **do not retry** — the work may already have run, so retrying could run it
  twice.

| `data.code` | JSON-RPC | Class | Refusal |
| --- | --- | --- | --- |
| `inbox.not_found` | `-32020` | terminal | No item with this id is in the queue. Read `list` and drop it. |
| `inbox.invalid_state` | `-32020` | terminal | This item's state does not allow this verb. Read `list` for the state, then pick a verb that fits. |
| `inbox.retry_may_repeat` | `-32021` | do not retry | Its block code is not `steer_unapplied`, so the turn may already have run. Retrying could run the work twice. |
| `inbox.item_too_large` | `-32020` | retry-after-repair | The body is over the single-item limit. Send a shorter one. |
| `inbox.capacity_items` | `-32020` | retry-after-repair | The queue holds its maximum count. Delete pending work, then retry. |
| `inbox.capacity_bytes` | `-32020` | retry-after-repair | The queue holds its maximum bytes. Delete pending work, then retry. |
| `inbox.paused` | `-32020` | retry-after-repair | The queue is paused. Resume it where the pause is one a client may lift; a host-set pause is for the person to inspect first, then retry. |
| `inbox.unrecovered` | `-32020` | retry-after-repair | The queue could not be read, so its contents are unknown. Inspect the queue directory before resuming. |
| `inbox.closed` | `-32020` | terminal | This queue is closed. Open the session again. |
| `inbox.schema_readonly` | `-32020` | terminal | A newer Reasonix wrote the queue and this build may read it only. Upgrade to write it. |
| `inbox.foreign_run` | `-32020` | terminal | Another run owns this item. Change it from the session that owns it. |
| `inbox.idempotency_conflict` | `-32020` | terminal | The key already belongs to different input. The same body still refuses; new input needs a new key. |
| `steer.already_applied` | `-32020` | terminal | The steer reached the turn before this call did. Nothing is lost and nothing is left to do. |
| `inbox.empty` | `-32602` | terminal | The request carries no text. Send a non-empty body. |

Three answers are not queue refusals:

- `-32602` with no `data.code`: the request cannot address a queue at all — an
  unknown `sessionId`, or params that do not parse;
- `-32600`: a malformed JSON-RPC frame (no method, or not an object);
- `-32603`: the queue, or the session's path to it, failed rather than refused.

## Goal pause and resume extension

Selecting `goal` with `session/set_mode` always drafts a **new** Goal: the next
prompt becomes its objective and its counters start at zero. To bring back a
Goal the session already holds, Reasonix advertises two vendor methods as keys
of `agentCapabilities._meta`, each with `{"schemaVersion": 1}`:

| Method | Params | Behavior |
| --- | --- | --- |
| `_reasonix.io/session/goal/resume` | `sessionId` | Re-enters a stopped, blocked, paused or cancelled Goal with its objective, counters, todos and delivery checkpoint. It does not start a turn: the next `session/prompt` continues the retained objective. Fails with `-32010` when the session has no resumable Goal (none, complete, or already running). |
| `_reasonix.io/session/goal/pause` | `sessionId` | Pauses a running Goal so it stops at the next continuation boundary; the turn in flight is left to `session/cancel`. Fails with `-32011` when no Goal is running. |

On success both methods:

- return `{"goal": …}` in the shape of the `goal` field of `_reasonix.io/session/status`;
- publish a `_reasonix.io/session/status_update` with event `goal`;
- send `current_mode_update` when the collaboration mode changes;
- replace a turn-outcome label the status was showing (`cancelled`, `failed`) with the Goal's own state.

The status `goal.status` value `failed` labels the last turn, not the Goal:
after a turn ends in an error the Goal is still running. A plain
`session/prompt` continues it, `resume` returns `-32010` because there is
nothing to resume, and `pause` stops it and reports the paused state.

## MCP server health extension

Reasonix advertises two versioned methods in `agentCapabilities._meta`:
`_reasonix.io/session/mcpStatus` and
`_reasonix.io/session/mcpStatus_update`, each with `{"schemaVersion": 1}`.
The first is a request with `{"sessionId": "…"}`. It returns a snapshot:

```json
{"schemaVersion":1,"sessionId":"…","servers":[{"name":"tools","status":"ready","tools":3}]}
```

The second is a notification with the same shape:

- Reasonix sends an initial snapshot after `session/new`, `session/load`, or
  `session/resume`, then another when a server starts connecting, connects,
  fails, or starts awaiting launch approval.
- Clients can request a fresh snapshot after reattaching.
- Status values are `ready`, `connecting`, `failed`, `pending`, `disabled`,
  `standby`, and `idle`. `standby` means cached tools are callable but the
  server has not connected yet. `pending` means project-server authorization
  is required, including after launch approval is revoked; it is not a
  connection failure.
- A status read does not start a server or probe its endpoint.
- Failed entries can include a sanitized `error` and an `httpStatus`; both
  fields are omitted when absent.

## Runtime reload and extension surface

Reasonix advertises two more extension points in
`agentCapabilities._meta["reasonix.io"]`:

- `sessionReloadExtensions` — the vendor method
  `_reasonix.io/session/reloadExtensions`. Calling it reloads the session's
  agent runtime (extensions, tools, skills, commands, hooks, providers) with
  the same fail-atomic semantics as the CLI `/reload` command: while a turn
  or rebuild is active exactly one reload is queued (`{"queued": true}`) and
  runs when the session goes idle; otherwise the runtime is rebuilt and
  swapped atomically, and a failed rebuild keeps the previous runtime. After
  a successful reload Reasonix pushes a fresh `available_commands_update`.
- `extensionSurface` — structured extension UI support. Clients that also
  advertise `reasonix.io.extensionSurface` in their initialize `_meta`
  receive structured extension surface payloads; clients without it receive
  equivalent text fallbacks (`agent_message_chunk` for cards and statuses,
  permission requests for extension forms), so no client-side handling is
  required to stay compatible.

Extension actions declared by installed plugins are exposed as
`/<plugin>:<action>` in `available_commands_update` and can be invoked like
any other slash command.

## Compatibility and cache behavior

| Surface | Older or non-Reasonix clients | Conclusion |
| --- | --- | --- |
| Existing ACP v1 methods | Their names and response shapes are unchanged. | Compatible |
| Capability `_meta` | Unknown metadata may be ignored. | Compatible |
| Persisted transcripts | Transcript schema is unchanged; the inbox is a versioned sidecar. | Compatible |
| CLI and Desktop steering | Rejected steers remain durable follow-ups. | Compatible |

Steering appends a user-requested message to normal conversation history. It
does not change the system prompt, tool schemas, tool order, or other stable
provider-prefix bytes. The next provider request necessarily misses the suffix
that did not previously exist, just like any normal new user message, while the
earlier prefix remains reusable.

## Client integration checklist

1. Launch `reasonix acp` with separate stdin, stdout, and stderr streams.
2. Call `initialize` and honor both standard and `_meta` capabilities.
3. Open sessions with absolute workspace paths and keep their ids isolated.
4. Process agent-to-client filesystem, terminal, and permission requests while
   a prompt is running.
5. Show steer UI only when the Reasonix capability is advertised and a prompt
   is active.
6. Branch on the steer `disposition`; both accepted steer and queued follow-up
   are durable, but only the former can affect the active turn.
7. Use `session/close` for resource cleanup and `session/delete` only when the
   user intends to remove persisted history.

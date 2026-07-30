# ACP editor integration

<a href="../README.md">README</a>
&nbsp;·&nbsp;
<a href="./ACP.zh-CN.md">简体中文</a>
&nbsp;·&nbsp;
<a href="./GUIDE.md">Guide</a>
&nbsp;·&nbsp;
<a href="https://agentclientprotocol.com/">ACP specification</a>

Reasonix implements Agent Client Protocol (ACP) v1 as an NDJSON JSON-RPC 2.0
agent over standard input and output. Editors and other ACP hosts launch the
process, open one or more workspace-scoped sessions, and receive streamed
messages, tool activity, plans, permission requests, and configuration updates.

## Start the agent

An ACP host should launch one of these commands:

```sh
reasonix acp
reasonix acp --model deepseek-pro
reasonix acp --profile delivery
```

`--model` selects the startup model when the client does not override it.
`--profile` sets the startup work mode to `economy`, `balanced`, or `delivery`.
Both remain session-configurable after initialization.

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
      "delete": {}
    },
    "promptCapabilities": {
      "image": false,
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
terminal. Without those client capabilities, the normal workspace tools run
locally inside the Reasonix process.

## Session lifecycle

Each ACP session owns an independent Reasonix controller, workspace root, model,
work mode, collaboration mode, approval mode, MCP set, and persisted transcript.
State does not leak between sessions.

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

## Session controls

Reasonix exposes independent controls instead of combining unrelated choices in
one mode selector:

| Control | Values | Wire surface |
| --- | --- | --- |
| Collaboration mode | `normal`, `plan`, `goal` | `modes` and `session/set_mode` |
| Model | Configured `provider/model` entries | `configOptions` with id `model` |
| Reasoning effort | Provider-supported levels or `auto` | `configOptions` with id `effort` |
| Work mode | `economy`, `balanced`, `delivery` | `configOptions` with id `work_mode` |
| Tool approval | `ask`, `auto`, `yolo` | `configOptions` with id `tool_approval` |

Use `session/set_config_option` for model, effort, work mode, and tool approval.
Model, effort, and work-mode changes rebuild the session controller while
preserving its history and the other axes. Tool-approval changes update the
gate without rebuilding the controller.

For older clients, `session/set_model` remains available. The legacy
`session/set_mode` values `default` and `auto` are also accepted as Normal + Ask
and Normal + Yolo respectively; new clients should use the independent
selectors above.

## Prompts, updates, and approvals

`session/prompt` accepts text blocks and embedded text resources. Images and
audio are not advertised. During a turn, Reasonix may send:

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

A successful `{}` result means the active turn accepted the guidance. Reasonix
adds it as a user message before the next safe model-call boundary, without
cancelling the turn or consuming an extra tool-step budget. The message is
persisted in normal history; transcript replay shows the original user text,
not Reasonix's internal steer marker.

| Condition | JSON-RPC result |
| --- | --- |
| Active prompt accepted the guidance | `{}` |
| Unknown session or empty prompt | `-32602 InvalidParams` |
| Session has no active prompt | `-32600 InvalidRequest` |
| Client calls `session/steer` | `-32601 MethodNotFound` |

On `InvalidRequest`, the guidance was not queued. A client may wait for the
active prompt to finish and offer the text as a normal new prompt, but it should
not silently report the failed steer as accepted.

## Compatibility and cache behavior

| Surface | Older or non-Reasonix clients | Conclusion |
| --- | --- | --- |
| Existing ACP v1 methods | Their names and response shapes are unchanged. | Compatible |
| Capability `_meta` | Unknown metadata may be ignored. | Compatible |
| Persisted transcripts | No new persisted schema is required. | Compatible |
| CLI, Desktop, and Bot steering | Their existing idle fallback remains unchanged. | Compatible |

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
6. Treat a successful steer response as queued guidance, not immediate model
   completion.
7. Use `session/close` for resource cleanup and `session/delete` only when the
   user intends to remove persisted history.

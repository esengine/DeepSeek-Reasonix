# Session runtime state

[简体中文](RUNTIME_STATE.zh-CN.md)

Desktop, Serve and remote sessions consume committed controller snapshots. Transcript events still carry content and completion results. Runtime reconciliation adds no model request or history reload and changes no session or inbox persistence format.

## Visible behavior

| State | Session and project indicator | Composer |
| --- | --- | --- |
| Executing | Thinking or streaming activity | Existing send and Stop semantics |
| Finishing | Static finishing label | Hide current-turn Stop; durably queue the next input |
| Waiting for confirmation | Confirmation indicator | Existing prompt and ownership guards |
| Cancelling | Remains cancelling until settled | Avoid duplicate cancellation |
| Background jobs | Count jobs across active and inactive sessions | Do not portray jobs as foreground model execution |
| Awaiting synchronization | Static uncertainty, retaining last known facts | Pause send/Stop until ownership and state are confirmed |

A successful finishing enqueue shows “Queued” and clears the draft. Failure preserves the draft and reports the error. Explicit retry of the same draft uses the same idempotency key. An uncertain remote POST only reads the receipt for its captured session and key; it never automatically replays the write. Remote queue snapshots validate the connection and session identity and update when the next turn starts.

Project activity includes every owned runtime and deduplicates the same remote session across tabs. Cancelled jobs retain their count and existing resource protection until they actually exit.

## Contract and ownership

`event.RuntimeStateSnapshot` schema version 1 explicitly includes:

- `runtimeEpoch` and `revision`: instance identity and monotonic version; one version has one immutable value.
- `phase`: `idle`, `executing`, `finishing` or `closed`.
- `running`, `turnId`, `turnStatus`, `turnEventSeq`.
- `pendingPrompt`, `cancelRequested`, `cancellable`, `backgroundJobs`, `activity`.

The legacy `Running()` execution/finishing protection remains intact. Controllers sample at commit boundaries and publish through bounded notifications outside owner locks. Jobs publish final counts after closing done. Runtime notifications bypass the WAL, transcript and provider messages.

Desktop `GetRuntimeStateSnapshot` and `runtime-state:changed` share a complete projection with epoch/revision, sessions and topics. Sampling reads controllers outside App locks and then revalidates the complete local binding set, including controller, tab, session generation, path and open/detached identity. The older project-tree interface adapts the same projection.

Serve `GET /runtime-states` reads foreground and detached controllers from memory; `/status` adds `runtimeState`. Session-specific reads resolve the controller owning that session. SSE `runtime_state` uses existing session tagging and cannot publish the new session state before its `session_changed` barrier. External takeover and read-only mirrors retain existing ownership rules.

The remote GET/SSE reducer preserves generation, client, selection and route fences. Older versions are discarded, identical versions are no-ops, and conflicting versions trigger coalesced resynchronization. Adopting a new epoch requires an authoritative read. A late GET cannot overwrite newer SSE state or a replacement binding.

## Synchronization and compatibility

The application subscribes before reading. Pushes apply immediately. One application owner checks every 30 seconds, with one read per Serve connection per pass. Attachment, focus and connection changes reconcile immediately; concurrent requests coalesce. Failures back off at 5, 10, 20 and 30 seconds without clearing known state.

With the new contract available, legacy per-session watchdogs no longer decide runtime truth. The ten-minute silence cleanup no longer determines project activity. A legacy Serve returning 404/501 is remembered for that connection generation and uses existing status interfaces. Missing fields are not zero values, and legacy payloads never imply finishing. Existing protocol fields remain supported.

Diagnostics report source, anonymous epoch, revision, phase, synchronization reason and stale/conflict/failure counts only. They exclude per-token logs, prompts, credentials and full paths.

## Validation

- Root module: `go test ./...`; `go test -race ./internal/control ./internal/jobs ./internal/event ./internal/serve`.
- Desktop module: `go test ./...`; `go test -race . -run 'RuntimeState|RemoteRuntime|ProjectTreeRuntime|SessionRuntime|RuntimeBinding'`.
- Frontend: runtime-state store, Composer inbox recovery and project-tree suites; `pnpm test:remote`, `pnpm test:app-lifecycle`, `pnpm build`.
- Browser: `node bench/runtime-state.mjs` runs real Chromium with controlled runtime frames for finishing, enqueue/retry, jobs, remote disconnection/recovery and switching. `pnpm test:app-browser` covers ordinary submission and application lifecycle. Frame fixtures supplement controller and HTTP/SSE integration tests.
- Native Desktop requires separate verification. An isolated macOS application with a loopback test model exercises real Wails submission, completion and sidebar settlement. Browser success does not establish Windows or Linux native behavior.

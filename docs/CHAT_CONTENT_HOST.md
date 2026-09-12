# Chat Content and Interaction Host

## Scope and source

The production chat path adopts the content layering, compact process rows, call inspection, and file-artifact interactions of DeepSeek Harness `c291e7961a515f6d7af9304e7fd1d257929aef26`, while retaining Reasonix session protocols, permissions, the built-in `present` tool, resource APIs, and Electron workspace.

```text
controller / history / LiveStream / remote events
                       ↓
          ChatSource turn projection
                       ↓
       prose / process summary / turn files
                       ↓
call drawer / workspace dock / browser / interaction host
```

The projection is rebuildable view data, not a second persistent conversation log. Cordis and the Harness Conversation framework are not dependencies.

## Shipped behavior

| Content | Default presentation | Interaction |
|---|---|---|
| User message | text, attachments, timestamp, copy | open images and files in the resource host |
| Assistant answer | complete Markdown | dedicated renderers for code, tables, math, images, and links |
| Reasoning | one-line summary and elapsed time | expand; large content uses the shared full-content loader |
| Tool call | compact semantic name, description, and status | inline disclosure plus Result, Parameters, Subcalls, and Raw record tabs |
| Presented file | cards from successful trusted `present` calls, first four visible | preview, source, browser, tree, path copy, save copy, and supported system actions |
| Modified file | compact rows from successful native file tools, first six visible | the same resource action entry point |
| Failure or unknown content | local status and safe fallback | inspect available raw content without failing the turn |
| Turn footer | copy, ordinary fork, usage, duration, and timestamp | existing session capability checks |

Read only, Workspace write, and Full access remain unchanged. The composer, stop/send, questions and approvals, project navigation, and model settings remain connected. No rating, transcript rewind, delivery acceptance, or display-mode switch was added.

## Stable projection and folding

`ChatSource` exposes independent order, node, and status subscriptions. It preserves snapshot references when structure or content has not changed. Streaming updates notify the target assistant node and affected turn only; the persisted completion replaces that node rather than inserting a second answer. Message and call IDs define identity, while generation and revision only fence stale asynchronous results.

A completed turn folds only when it has a confirmed answer and complete process boundary. Running, cancelled, interrupted, terminally failed, tool-only, and partial-history turns stay expanded. A recovered tool failure may fold with the successful turn, while its failure count remains visible. A manual expansion is not overridden by later streaming updates.

Folding unmounts Markdown, diagrams, terminal output, and detail subtrees. Reasonix intentionally does not retain Harness `hidden="until-found"` DOM, so browser find covers mounted content only.

## Trusted tool and file facts

Tool presentation matches exact trusted built-in identities before falling back to generic content. An MCP or plugin tool cannot gain native file behavior merely by including `write`, `edit`, or `present` in its name. Shell labels report PowerShell, Git Bash, Bash, Zsh, Shell, or Terminal from the actual executor metadata.

Presented and modified files are distinct facts:

- A file card requires a successful, error-free built-in call named exactly `present`.
- A modified-file row requires a successful native `write_file`, `edit_file`, `multi_edit`, `notebook_edit`, `delete_range`, `delete_symbol`, or `move_file` result.
- Reads, failures, denials, cancellations, `no changes made`, incomplete arguments, model prose, and shell output do not produce file facts.
- Presented paths suppress duplicate modified rows. Repeated presentation keeps first-seen ordering and applies the latest description.
- Successful file facts remain visible even when no final answer exists.

## Resource boundary

`FileResourceRef` carries source type, host, tab, source call, and path. Provenance does not grant arbitrary file access. UI capability snapshots control honest affordances, and every host operation revalidates the current session and policy.

```ts
openResource(ref, { view: "preview" | "source" | "browser" })
performResourceAction(ref, action)
resolveFileResourcePath(ref)
```

Browser navigation waits on a cancellable host subscription instead of polling animation frames. A newer request or session switch invalidates the old intent and revokes an unused preview URL.

Remote presented files still require `present-files-v1` and a trusted history declaration. Remote modified files are accepted only when the current tab, host, client, generation, session, successful native tool result, and workspace-confined path all match. Remote paths are never passed to local operating-system openers.

## Preview and lifecycle

The workspace dock remains the single host for Markdown, HTML, source, CSV, images, PDF, audio, and video. HTML retains the existing controlled media URL and `sandbox="allow-scripts"` iframe without same-origin, Node, application bridge, or top-level navigation privileges. Its dependency limits remain 32 MiB total, 64 resources, and 4 MiB per resource. The existing HTTPS CSP scope is unchanged; iframe isolation is not described as network isolation.

Full content, source pages, and dependency preparation share a four-request per-session scheduler. Closing content cancels requests without consumers; results that cannot be cancelled are discarded by generation. A session switch releases subscriptions, content tasks, drawers, active media, resource URLs, and pending host intents.

The natural document flow continues to use one `ChatScrollController` and one programmatic scroll writer. Stable node and viewport offsets restore reading position after prepend, folding, media changes, or workspace-dock layout changes. The turn navigator lists loaded turns only and retains the approved 1.38.7 colors.

The `present` schema, description, tool ordering, provider serialization, system prompt, and persisted history format were not changed.

## Validation on 2026-09-13

| Host | Scenario | Input P95 | Switch P95 | Max long task | Anchor drift |
|---|---:|---:|---:|---:|---:|
| Chromium 153 | 240 turns | 34.4 ms | 37.6 ms | 63 ms | 0 px |
| Chromium 153 | 1,000 turns | 105.7 ms | 37.6 ms | 243 ms | 0 px |
| WebKit 26.6 | 240 turns | 57 ms | 54 ms | unavailable | 0 px |
| WebKit 26.6 | 1,000 turns | 63 ms | 54 ms | unavailable | 0 px |
| Electron 44.2 | 240 turns | 30.1 ms | 24.6 ms | 72 ms | 0 px |
| Electron 44.2 | 1,000 turns | 43.2 ms | 24.6 ms | 272 ms | 0 px |

Chromium heap growth after forced collection and twenty session switches was 223,692 bytes, below the 20 MiB limit. History-prepend drift was 0.09375 px on all three hosts. WebKit has no Long Tasks API, so the result remains unavailable rather than being reported as zero.

Frontend type checks, lint, build, bundle budgets, the single-scroll-writer guard, transcript, stream, composer, workspace, remote, app-lifecycle, MCP, and projection-performance suites passed. Full Chromium, WebKit, and Electron chat replays and 42 Electron geometry scenarios passed. Root and Desktop Go tests plus relevant race suites passed.

Raw data and screenshots are under [evidence/chat-content-host-2026-09-13](evidence/chat-content-host-2026-09-13).

Not verified in this run: a live remote-server replay; native Windows and Linux openers, encoders, and IMEs; WebKit long-task metrics; a signed release build; and an hours-long macOS native-IME soak in an isolated graphics environment. Browser results are not substituted for those platform checks.

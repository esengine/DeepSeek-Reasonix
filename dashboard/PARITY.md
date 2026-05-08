# Dashboard ↔ TUI parity

Closing audit of #369. Inventories every slash command and how the
web dashboard handles it.

## Coverage legend

- **panel** — has a dedicated SPA panel or modal beyond just typing the
  slash in the chat box.
- **chat-box** — works by typing the slash into the web chat input.
  Result text shows in the dashboard scrollback as an info row. No
  dedicated UI; the chat box _is_ the UI.
- **tui-only** — keyboard binding or process-controlling action that
  has no useful web equivalent. Stays a TUI affordance by design.

## Counts

Roughly **20 commands have a dedicated panel**, **38 work via the chat
box**, and **`/exit` plus 4 raw keyboard shortcuts** stay TUI-only.
Aliases (e.g. `/sessions` and `/resume` share one picker) collapse
into one row.

## Core / observability

| Slash | Purpose | Coverage |
|---|---|---|
| `/help` | command reference | chat-box |
| `/keys` | keyboard shortcuts + prompt prefixes | chat-box |
| `/status` | model + flags + ctx + session | chat-box |
| `/context` | context-window breakdown (stacked bar) | chat-box · `ctxBreakdown` payload |
| `/cost` | last turn / next turn estimate | chat-box · usage card |
| `/stats` | cross-session cost dashboard | **panel** · Usage tab |
| `/think` | last R1 reasoning dump | chat-box |
| `/tool [N]` | dump full output of Nth tool call | chat-box |
| `/clear` | wipe visible scrollback | chat-box |
| `/new` (`/reset`) | wipe context + scrollback | chat-box |
| `/exit` (`/quit`, `/q`) | quit the TUI | **tui-only** |
| `/stop` | abort current model turn | chat-box |
| `/retry` | resend last user message | chat-box |
| `/compact` | fold older turns into summary | chat-box |
| `/update` | show current vs latest version | chat-box |
| `/doctor` | health check card | chat-box · doctor card |

## Model & compute

| Slash | Purpose | Coverage |
|---|---|---|
| `/preset` | model bundle (auto / flash / pro) | **panel** · Settings → Defaults |
| `/effort` | reasoning cap (high / max) | **panel** · Settings → Defaults |
| `/model` | active model | **panel** · Settings → Runtime (D-4 #437) |
| `/models` | list available models | chat-box |
| `/pro` | arm v4-pro for next turn | **panel** · Settings → Compute (D-2 #435) |
| `/budget` | session USD cap | **panel** · Settings → Budget + cockpit tile (D-3 #436) |
| `/loop` | auto-resubmit on interval | **panel** · Settings → Loop (D-5 #438) |

## Memory & project

| Slash | Purpose | Coverage |
|---|---|---|
| `/memory [list / show / forget / clear]` | manage pinned memory | **panel** · Memory tab |
| `/init` | synthesize baseline REASONIX.md | chat-box |
| `/semantic` | semantic-search index status | chat-box |
| `/search-engine` (`/se`) | switch web search backend | chat-box |
| `/language` (`/lang`) | runtime language | **panel** · Settings → Language |

## Sessions

| Slash | Purpose | Coverage |
|---|---|---|
| `/sessions` | list saved sessions | **panel** · SessionPicker modal (C-2 #423) |
| `/resume` | open a session | **panel** · same picker |
| `/rename` | rename current session | chat-box |
| `/forget` | delete current session | chat-box |
| `/plans` | active + archived plans | **panel** · Plans tab |
| `/replay [N]` | read-only plan archive | **panel** · Viewer modal (C-5 #427) |

## MCP

| Slash | Purpose | Coverage |
|---|---|---|
| `/mcp` (list) | bridged servers + tools | **panel** · MCP tab |
| `/mcp browse` | marketplace + install | **panel** · MCP marketplace picker (C-4 #426) |
| `/mcp disable` / `enable` / `reconnect` / `text` | server admin | chat-box |
| `/resource [uri]` | browse / read MCP resources | chat-box |
| `/prompt [name]` | browse / fetch MCP prompts | chat-box |

## Permissions & admin

| Slash | Purpose | Coverage |
|---|---|---|
| `/permissions [list / add / remove / clear]` | shell allowlist | **panel** · Permissions tab |
| `/hooks [reload]` | active hooks | **panel** · Hooks tab |
| `/dashboard [stop]` | embedded dashboard lifecycle | chat-box · *intentional — admin command for the surface you're typing in* |

## Code-mode only

| Slash | Purpose | Coverage |
|---|---|---|
| `/init [force]` | scan + synthesize REASONIX.md | chat-box |
| `/apply [N]` | commit pending edits | chat-box |
| `/discard [N]` | drop pending edits | chat-box |
| `/walk` | step through pending edits | **panel** · edit-review modal already covered web pre-#369 |
| `/undo` | roll back last edit batch | chat-box |
| `/history` | edit batch list | chat-box |
| `/show [id]` | dump stored edit diff | chat-box |
| `/commit "msg"` | git commit | chat-box |
| `/checkpoint [name / list / forget]` | snapshot touched files | chat-box |
| `/restore` | roll back to checkpoint | **panel** · CheckpointPicker modal (C-3 #425) |
| `/plan [on / off]` | read-only plan mode | chat-box |
| `/apply-plan` | force-approve pending plan | chat-box |
| `/mode [review / auto / yolo]` | edit gate | **panel** · Chat header pill |
| `/jobs` | list background jobs | chat-box |
| `/kill <id>` | stop background job | chat-box |
| `/logs <id> [lines]` | tail job output | chat-box |
| `/skill [list / show / new / <name>]` | skill management | **panel** · Skills tab |

## Keyboard / TTY-native

These don't have slashes. They ride alongside the slash surface and
stay TUI-only:

- `Esc` — abort current model turn (web equivalent: Abort button in chat)
- `Shift+Tab` — cycle edit mode (web equivalent: mode pill in chat header)
- `Ctrl-L`, `Ctrl-O`, `space`, `u` — TTY scroll / undo banner / pause hotkeys

## Done

Buckets covered:
- **C** (#416, closed): pickers — `/sessions`, `/restore`, `/mcp browse`, `/replay` + `/walk` (already)
- **D** (#428, closed): settings — `/preset`, `/effort`, `/model`, `/pro`, `/budget`, `/loop`, `/language`

Bucket A (text outputs that stay chat-box) and bucket B (structured
outputs that already have panels) need no further work — the panels
shipped in C/D plus the long-standing Memory / Permissions / Hooks /
Skills / Plans / Usage tabs already cover every command whose output
warranted a dedicated UI.

This file is the source of truth for the audit. Add a row when a
new slash lands.

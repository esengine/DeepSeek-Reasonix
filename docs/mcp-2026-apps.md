# MCP 2026 capability surface

Reasonix speaks the MCP `2026-07-28` protocol revision (multi-round-trip
requests, form/URL elicitation) and the stable MCP Apps `2026-01-26`
extension on Desktop. Users change nothing: servers keep their existing
enable/disable switches, and every new capability rides the frontend's
host profile.

## Host capability profiles

| Frontend | Profile | Declares |
|---|---|---|
| CLI headless (`-p`), bots | `core-v1` | legacy surface only (byte-identical to the old client) |
| CLI chat TUI, `serve` | `interactive-v1` | + form and URL elicitation |
| Desktop | `desktop-apps-2026-01-26-v1` | + elicitation and `io.modelcontextprotocol/ui` (`text/html;profile=mcp-app`) |

The profile is fixed when the host is created. If the Apps sandbox
listener cannot bind, Desktop degrades to `interactive-v1` before the
first connection — MCP Core and text results stay available and no
server is told this client renders apps.

`/mcp status` (TUI), the Desktop MCP panel, and `control.MCPCapabilityViews`
expose the four-layer matrix — **Protocol Connection / Core Host /
Interactive Host / Apps Host** — with `supported | negotiated | degraded |
unavailable` states derived from live sessions. There is no editable
switch.

## Elicitation (MRTR)

A server may pause a `tools/call` with input requests (form schema or a
credential-free URL). The broker travels with the per-call context, so
the answer always reaches the tab or terminal that started the call:

- **Desktop/serve/TUI**: a typed form (flat primitive schema: string,
  number, integer, boolean, enum, defaults, required, bounds) or a URL
  card showing the server and target origin; the browser opens only on
  an explicit click. Submit / refuse / close map to accept / decline /
  cancel.
- **Headless (no broker)**: the capability is not declared; a stray
  request answers cancel — the model never guesses.
- Frontend reconnects replay a pending elicitation; process exit cancels
  the underlying call.
- Form values and URL targets never enter logs or telemetry; decision
  receipts record only kind and action.

## MCP Apps (Desktop)

Tools may declare Apps metadata: `_meta.visibility` (`["model","app"]`
by default) and `_meta.ui.resourceUri` (nested key preferred, flat
`ui/resourceUri`/`resourceUri` accepted) with optional per-resource CSP.
App-only tools stay in a server-private catalog — invisible to the model
and to `use_capability list`.

Results from App-capable tools carry a bounded local presentation (one
aggregate 512 KiB cap including metadata and JSON framing; inline
audio/video and oversized nested base64 are stripped) that is persisted
for the Desktop card and stripped from every provider request.

Inline surfaces run in a double-iframe sandbox. A per-server loopback
origin relays AppBridge traffic in both directions to a sandboxed inner
frame, with parent/inner source checks, instance-nonce binding, an 8 MiB
UTF-8 frame cap, a 4 MiB `ui://` HTML cap, and deny-all CSP extended only
by exact declared origins. Opening a card validates its server, tool,
catalog generation, and resource URI, then freezes the resource in the
bounded live-instance registry. The SHA-256 digest is bound into the
resource request and response, so content cannot change within that App
instance. Reopening an older card creates a new validated snapshot of the
server's current resource; Reasonix does not persist executable App HTML
in the conversation.

After `ui/notifications/initialized`, Desktop sends the original tool
input followed by the full bounded `CallToolResult`; unmount waits up to
one second for `ui/resource-teardown`. App-initiated `tools/call`, link
opening, resource loading, and cleanup remain bound to the originating
tab even if the user switches tabs. External `http(s)` links require one
confirmation per App instance and origin, and are validated again by the
native host. App tool calls resolve through the instance registry (same
server, app visibility, current catalog generation) and record nested,
local-only events — visible in the transcript, never added to model
context.

## Cache and cross-version compatibility

Cache identity is the profile, never SDK version, time, or negotiation
results:

| Scenario | Behavior |
|---|---|
| New Desktop first reads an old core cache | miss; the enhanced profile handshakes its own catalog |
| New CLI/serve | keep the interactive profile's own cache; never read the Desktop cache |
| Old binary writes | only the legacy `<slug>.json`; enhanced files untouched |
| New session opened by an old binary | `mcp_app` ignored; text results intact |
| Old binary rewrites a session | interaction replay metadata may be lost; text and pairing survive |
| Both writing one session | unsupported (existing single-writer/session-lock boundary) |

`use_capability` keeps its name, schema, ordering, and lazy-connect
behavior byte-for-byte. App-only tools never enter provider requests,
and all cache files keep atomic writes with `0600` permissions.

# Capability gap matrix — Electron + `reasonix serve` PoC

Comparison of the **official Wails desktop** (~389 bridge methods, multi-tab)
versus this **PoC host** (single session, ~30 serve routes).

## Legend

| Symbol | Meaning |
| --- | --- |
| ✅ | Supported in PoC (HTTP/SSE or shell) |
| ⚠️ | Partial / degraded |
| ❌ | Out of PoC scope (capability-gated) |

## Core agent path (P0)

| Capability | Wails desktop | PoC (serve + Electron) | Notes |
| --- | --- | --- | --- |
| Submit text turn | ✅ | ✅ `POST /submit` | |
| Cancel | ✅ | ✅ `POST /cancel` | |
| Tool approval | ✅ | ✅ `POST /approve` | |
| Ask / answer | ✅ | ✅ `POST /answer` | |
| Event stream | ✅ Wails events | ✅ `GET /events` SSE | Mapped via `mapServeEventToWire` |
| History | ✅ | ✅ `GET /history` | Full list; no page API |
| Context panel | ✅ | ✅ `GET /context` | |
| Status / ready | ✅ | ✅ `GET /status` | |
| New session | ✅ | ✅ `POST /new` | |
| Compact | ✅ | ✅ `POST /compact` | |
| Rewind + checkpoints | ✅ | ✅ `/rewind` `/checkpoints` | Advanced preview/commit chain may differ |
| Fork / summarize | ✅ | ✅ | |
| Plan mode | ✅ | ✅ `POST /plan` | |
| Tool approval mode | ✅ | ✅ `/tool-approval-mode` | |
| Goal | ✅ | ✅ `POST /goal` | Subset of Goal FSM UI |
| Sessions list/delete/resume | ✅ | ✅ | |
| Models list / switch | ✅ | ✅ list; switch via `/model` submit intercept | |
| Todos / skills | ✅ | ✅ | |
| Provider setup | ✅ | ✅ `/provider-setup` (loopback) | |
| Extensions reload | ✅ | ✅ | |

## Desktop shell

| Capability | Wails | PoC |
| --- | --- | --- |
| Native window | ✅ | ✅ Electron |
| Single-instance | ✅ | ✅ |
| Workspace pick | ✅ in-process switch | ⚠️ dialog + **restart serve** in new cwd |
| Serve log open | n/a | ✅ |
| Crash restart serve once | n/a | ✅ |
| Production auto-update | ✅ | ❌ |

## Explicitly deferred (capability = false)

| Feature | Reason |
| --- | --- |
| `multiTab` | **Enabled (Phase 3):** serve `--multi-tab` + Electron `ListTabs`/`SubmitToTab`/`OpenProjectTab` via `/tabs`; sidebar topics/projects via `/desktop/*` |
| `projectTree` | **Enabled:** `GET /desktop/project-tree` + topic CRUD |
| `desktopSettings` | **Partial:** read-only startup/settings from user config; full Settings write remains Wails |
| `terminal` | No PTY in serve |
| `remote` | Separate remote bootstrap product path |
| `bot` | Bot bridge is desktop Go |
| `heartbeat` | Desktop-only panel |
| `themePackStore` | Wails theme system |
| `productionUpdater` | Not a release product |
| `steer` | No `/steer` route in serve today |
| `submitDisplay` / edit-replay / invocations | Submit body is plain `input` |
| `historyPagination` | Serve returns full history |

## Source of truth

- Runtime flags: `electron-poc/lib/capabilities.mjs`
- TS mirror: `desktop/frontend/src/lib/host/capabilities.ts`
- Routes: `electron-poc/lib/routes.mjs` ↔ `internal/serve` handlers

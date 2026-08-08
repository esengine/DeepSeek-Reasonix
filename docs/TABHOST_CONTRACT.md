# Tabhost contract (route C · MVP-B)

Transport-agnostic multi-Controller runtime shared by multi-tab `reasonix serve`
and the Electron shell. Tab semantics align with Wails desktop `WorkspaceTab` /
`TabMeta` (`desktop/tabs.go`).

**Status:** Phase 0–3 landed. `internal/tabhost` Host + `reasonix serve --multi-tab`
HTTP/SSE routes (`/tabs`, `/tabs/{id}/…`) + desktop sidebar metadata
(`internal/desktopsidebar` + `/desktop/*`) + Electron multi-tab UI (project open,
tab switch, per-tab transcript).

## Goals

| Goal | Notes |
| --- | --- |
| Multi-project concurrent agents | Distinct `workspaceRoot` per tab, independent `control.SessionAPI` |
| Multi-session concurrent agents | Distinct session paths; cancel/approve do not cross tabs |
| Event routing | Every wire event carries `tabId` (+ optional `runtimeEpoch`) |
| Electron UI | Existing `desktop/frontend` multi-tab reducer works without rewrite |
| Shared sidebar metadata | `desktop-projects.json` + topic title maps shared with Wails |

## Non-goals (MVP-B)

- Replacing official Wails packaging
- Full Terminal / Remote / Bot / theme pack / production updater parity
- Per-tab OS processes
- Moving all of `desktop` App onto tabhost

## TabMeta JSON (frozen for serve + Electron)

Aligned with `desktop.TabMeta` and `desktop/frontend/src/lib/types.ts`:

```json
{
  "id": "string",
  "scope": "project|global",
  "workspaceRoot": "string",
  "workspaceName": "string",
  "workspacePath": "string?",
  "topicId": "string",
  "topicTitle": "string",
  "sessionPath": "string?",
  "label": "string",
  "ready": "bool",
  "runtime": "SessionRuntimeView",
  "running": "bool",
  "cancellable": "bool",
  "mode": "string",
  "collaborationMode": "string",
  "toolApprovalMode": "string",
  "tokenMode": "string",
  "goal": "string?",
  "goalStatus": "string?",
  "active": "bool",
  "cwd": "string"
}
```

## HTTP routes

### Multi-tab (`--multi-tab`)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/tabs` | List tabs |
| POST | `/tabs` | Create tab |
| POST | `/tabs/{id}/activate` | Activate |
| POST | `/tabs/{id}/close` | Close |
| POST | `/tabs/{id}/submit` | Submit turn |
| POST | `/tabs/{id}/cancel` | Cancel |
| POST | `/tabs/{id}/approve` | Approve tool |
| POST | `/tabs/{id}/answer` | Answer ask |
| POST | `/tabs/{id}/plan` | Plan mode |
| POST | `/tabs/{id}/compact` | Compact |
| POST | `/tabs/{id}/new` | New session |
| POST | `/tabs/{id}/goal` | Goal |
| POST | `/tabs/{id}/tool-approval-mode` | Tool approval mode |
| GET | `/tabs/{id}/history` | History |
| GET | `/tabs/{id}/context` | Context usage |
| GET | `/tabs/{id}/status` | Runtime status |
| POST | `/tabs/open-project` | Open/focus project workspace tab |
| GET | `/events` | Multiplex SSE |

### Desktop sidebar / settings (always registered)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/desktop/project-tree` | Sidebar `ProjectNode[]` |
| POST | `/desktop/topics` | CreateTopic |
| POST | `/desktop/topics/rename` | RenameTopic |
| POST | `/desktop/topics/delete` | DeleteTopic |
| POST | `/desktop/topics/trash` | TrashTopic |
| POST | `/desktop/projects/remove` | RemoveWorkspace |
| POST | `/desktop/projects/rename` | RenameProject |
| POST | `/desktop/projects/reorder` | ReorderProjects |
| GET | `/desktop/startup-settings` | DesktopStartupSettings |
| GET | `/desktop/settings` | Lightweight Settings |

## Package / layering

- Package: `reasonix/internal/tabhost`
- Package: `reasonix/internal/desktopsidebar` (shared project/topic metadata)
- Frontend layer (may import `control`, `boot`, `event`, `eventwire`)
- Registered in `tools/repolint/layers.go` `frontends` for tabhost/serve

## Success criteria (MVP-B)

User can run **two workspace tabs** in Electron concurrently: streaming and approvals do not cross-talk; `GET /tabs` shows both; active switch does not stop the background turn; sidebar project tree and topic create work via `/desktop/*`.

## Still deferred

Terminal, Remote, Bot runtime, Heartbeat, Steer over HTTP, production updater, full Wails Settings write surface.

# Reasonix Electron PoC (loopback `reasonix serve`)

Experimental **Electron shell** that supervises a local **`reasonix serve`** on
`127.0.0.1` with **token-file auth**. This is **not** the official desktop
product (that remains the Wails app under `desktop/`).

## What this proves

1. Electron can start/stop a trusted `reasonix` binary with:
   - `--addr 127.0.0.1:0`
   - `--auth token`
   - `--token-file` / `--port-file` / `--pid-file` (token **not** on argv)
2. Core agent I/O works over **HTTP JSON + SSE** via `lib/httpSseHost.mjs`
   (P0 serve surface).
3. Unsupported Wails-only surfaces are **capability-gated** (see
   `docs/CAPABILITY_GAP.md`).

## Prerequisites

- Node.js ≥ 20
- A `reasonix` binary:
  - `export REASONIX_BIN=/path/to/reasonix`, or
  - `make build` in the repo root (`bin/reasonix`), or
  - `reasonix` on `PATH`
- Provider configured (`reasonix setup`) for live chat; `/status` works without a turn

## Quick start

```bash
cd electron-poc
npm install                 # installs deps
npm run ensure-electron     # download/extract Electron binary if postinstall was skipped
npm run build:ui            # build Wails-style desktop React UI → desktop-ui/
npm test                    # unit/integration tests (no display required)
npm start                   # opens Electron → desktop React UI over loopback serve
# npm run start:fast        # skip rebuild if desktop-ui/ already exists
```

If `npm install` blocked install scripts (npm allow-scripts policy), run
`npm run ensure-electron` once so `path.txt` + `Electron.app` Frameworks exist.

### UI mode

The window loads the **same React app as Wails desktop** (`desktop/frontend`),
wired through `HttpSseHost` + a same-origin reverse proxy (`lib/desktopShell.mjs`).
It is **not** the lightweight `reasonix serve` HTML client.

**Multi-tab (Phase 3):** the supervisor starts `reasonix serve --multi-tab`.
`ListTabs` / `SubmitToTab` / `OpenProjectTab` call `/tabs…`. Sidebar topics /
project tree use `/desktop/*` (`internal/desktopsidebar`, shared with Wails
`desktop-projects.json`). Use a binary built from this repo
(`REASONIX_BIN=./bin/reasonix`) so `--multi-tab` and `/desktop/*` exist.

Still deferred (not full Wails parity): terminal PTY, remote SSH, bot IM runtime,
heartbeat automation, theme pack store, production updater, full Settings write-back.

```bash
npm run smoke:dual-project   # two workspaces + project-tree + create topic
```

Environment:

| Variable | Meaning |
| --- | --- |
| `REASONIX_BIN` | Absolute path to reasonix binary |
| `REASONIX_WORKSPACE` | cwd for serve (default: process cwd) |
| `REASONIX_POC_CRASH_RESTART=0` | disable one-shot crash restart |
| `REASONIX_POC_SCRATCH` | directory for smoke log capture |
| `REASONIX_POC_SUBMIT=1` | allow live `/submit` in HTTP smoke |
| `REASONIX_POC_ELECTRON=1` | dual-launch script also spawns Electron |

## Smoke scripts

```bash
export REASONIX_POC_SCRATCH=/path/to/scratch
npm run smoke:http      # supervisor + HttpSseHost against real serve
npm run smoke:launch    # dual serial launches + GET /status
```

## Layout

```text
electron-poc/
  electron/main.mjs       # single-instance, window, workspace picker, quit→stop
  electron/preload.cjs    # contextIsolation whitelist
  lib/serveSupervisor.mjs # token/port/pid lifecycle (testable without Electron)
  lib/httpSseHost.mjs     # P0 HTTP+SSE host
  lib/capabilities.mjs    # feature gates
  docs/CAPABILITY_GAP.md
  docs/GO_NO_GO.md
```

Frontend host mirror (Wails default unchanged):

- `desktop/frontend/src/lib/host/*` — TypeScript `HttpSseHost` + capabilities

## Security defaults

- Loopback bind only
- Token in `0600` file; never `--token` on argv under supervision
- Renderer: `contextIsolation: true`, `nodeIntegration: false`, `sandbox: true`
- Trusted binary basename must be `reasonix` / `reasonix.exe`

## Menu / shell actions (IPC)

Via `window.reasonixPoc` (preload):

- `getEndpoint()` — baseUrl, token, log path, capabilities
- `pickWorkspace()` — dialog + restart serve in chosen directory
- `openLog()` — open serve log in OS
- `restartServe()` — manual restart

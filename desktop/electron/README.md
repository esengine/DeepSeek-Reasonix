# Reasonix Desktop shell (Electron)

The Electron process that hosts the React UI and supervises the Go desktop
service. The wire contract between the two is
[`docs/DESKTOP_HOST_PROTOCOL.md`](../../docs/DESKTOP_HOST_PROTOCOL.md); this
package implements the shell side of it and nothing else. Business logic stays
in Go, the UI stays in `../frontend`.

```text
renderer (reasonix://app) ──preload (window.reasonixDesktop)──▶ main process ──NDJSON JSON-RPC over stdio──▶ reasonix-desktop --host-rpc
```

## Layout

| Path | Concern |
| --- | --- |
| `src/main/index.ts` | bootstrap: data home, single instance, privileged scheme, wiring |
| `src/main/service.ts` | Go service supervisor: spawn, stderr log, restart budget, shutdown |
| `src/main/rpc.ts` | NDJSON JSON-RPC 2.0 client (64 MiB frames, timeouts, reverse requests) |
| `src/main/handshake.ts` | `desktop/hello` params, result validation, failure descriptions |
| `src/main/window.ts` | main `BrowserWindow`, `host/window.*`, close and crash handling |
| `src/main/protocol.ts` | `reasonix://app` file serving and resource-origin forwarding |
| `src/main/ipc.ts` | renderer IPC: sender check, contract allowlist, native calls |
| `src/main/hostCalls.ts` | `host/*` dispatch table |
| `src/main/lifecycle.ts` | quit sequencing (`beforeClose` → `shutdown` → stdin close → exit) |
| `src/main/menu.ts`, `tray.ts`, `dialogs.ts`, `remoteWindows.ts` | native surfaces |
| `src/main/browser/` | in-app browser: website views, snapshots, actions, downloads, grants |
| `src/preload/index.ts` | the single `window.reasonixDesktop` object |
| `src/shared/ipc.ts` | channel names and types shared by main and preload |

## Browser surface

## Hardware acceleration recovery

The desktop UI exposes **Settings → General → System → Hardware acceleration**.
The preference is stored in the Electron shell profile and only takes effect
after a full application restart. If rendering fails before Settings can open,
fully quit Reasonix and start it once with `REASONIX_DISABLE_GPU=1`; this is a
temporary override and does not change the saved preference. The override is
supported on Windows, macOS, and Linux.

The shell can host real websites next to the app UI (contract:
[`docs/DESKTOP_BROWSER.md`](../../docs/DESKTOP_BROWSER.md)). Every tab is a
sandboxed `WebContentsView` managed by `browser/surfaceManager.ts`; the React
panel drives it through `reasonixDesktop.browser.*` (user surface, no grant),
and Go drives it through the `host/browser.*` host calls
(`browser/hostCalls.ts`), which require a per-task grant that dies with the
service generation.

| Module | Concern |
| --- | --- |
| `guestView.ts`, `electronGuestViews.ts` | the `WebContentsView` behind injected interfaces; tests use fakes |
| `surfaceManager.ts` | tabs, layout/overlay visibility, take-over and crash recovery |
| `grants.ts`, `errors.ts` | per-task grants and the `-32010/-32011/-32012` contract codes |
| `snapshotScript.ts`, `snapshot.ts`, `pageScripts.ts` | serialised page walkers: aria-style snapshot, ref resolve/locate/select |
| `documents.ts`, `refResolver.ts` | document tokens; a navigation or take-over stales every earlier ref |
| `actions.ts`, `keys.ts`, `upload.ts` | trusted input dispatch: click, type, press, scroll, select, upload |
| `screenshot.ts` | element/full-page captures into the task scratch directory |
| `downloads.ts` | `will-download` routing, progress events, per-tab waits |
| `guestPreload.ts` | website-view preload; only reports user input for take-over |
| `fakeGuestViews.ts` | in-memory views so all of the above runs under plain `node --test` |

User input in a website view flips the tab to human mode (take-over), bumps
its epoch and is reported to Go as `browser.takeover`; `browser.resume` hands
it back. Agent-dispatched input is marked so its echo is not a take-over.
Downloads land in the task's scratch directory when one is registered by a
`browser.act`/`browser.screenshot` call, otherwise in
`userData/downloads/<taskId>`; the renderer hears about them through
`reasonixDesktop.browser.onDownload`.

## Build

Prerequisites: Node 24+, pnpm 10, Go. Install from the workspace root once:

```sh
cd desktop
pnpm install
```

`pnpm install` also downloads the Electron binary (`allowBuilds: electron` in
`pnpm-workspace.yaml`). If `node_modules/electron/dist` is missing afterwards,
run `node node_modules/electron/install.js` inside `desktop/electron`.

Build the Go service and the UI, then the shell:

```sh
cd desktop
go build -o build/bin/reasonix-desktop-service .     # accepts --host-rpc
go run . -emit-contract frontend/src/generated       # desktopContract.generated.{ts,json}
pnpm --filter reasonix-desktop-frontend build        # frontend/dist
pnpm --filter reasonix-desktop-shell build           # electron/dist/{main,preload}.cjs + desktopContract.json
```

The shell build reads `frontend/src/generated/desktopContract.generated.json`,
recomputes its digest the way `hostrpc.Contract.Canonical` defines it
(sorted keys, compact, no HTML escaping), checks it against the
`DESKTOP_CONTRACT_DIGEST` the generator emitted, and writes the contract plus
`digest` to `dist/desktopContract.json`. A missing contract fails the build;
set `REASONIX_ELECTRON_ALLOW_MISSING_CONTRACT=1` to build without it (every
`desktop/invoke` is then rejected and the hello digest is empty).

Packaged shells read the full version tag, channel and commit from
`resources/build.json` for `desktop/hello`. `app.getVersion()` and
`package.json.version` are numeric native metadata and must not identify the
RPC build. The packaged startup smoke runs without development overrides and
requires the renderer's `Version` command to match that manifest; the service
used by CI must also be linked with the same non-development version.

## Run

```sh
cd desktop/electron
pnpm start                     # electron . against ../build/bin/reasonix-desktop-service
REASONIX_DESKTOP_SERVICE=/path/to/binary pnpm start
```

Development against the Vite dev server instead of the packaged UI:

```sh
cd desktop/frontend && pnpm dev                      # http://127.0.0.1:5173
cd desktop/electron && pnpm dev                      # REASONIX_DEV=1, loads REASONIX_ELECTRON_DEV_URL
```

Environment:

| Variable | Effect |
| --- | --- |
| `REASONIX_DESKTOP_SERVICE` | path of the Go service binary (packaged default: `resources/service/reasonix-desktop[.exe]`) |
| `REASONIX_HOME` | data home, resolved exactly like `internal/config.ReasonixHomeDir` and sent in `hello.instance.home` |
| `REASONIX_DEV` | skips the single-instance lock and marks the instance as `dev` |
| `REASONIX_ELECTRON_DEV_URL` | loads this URL instead of `reasonix://app/index.html` |
| `REASONIX_FRONTEND_DIST` | overrides the directory served under `reasonix://app/` |
| `REASONIX_CHANNEL`, `REASONIX_COMMIT` | build identity in `hello.build` (default `dev`) |

Logs live under `<home>/desktop-shell/logs/`: `shell.log` (main process) and
`service.log` (the Go service's stderr), each rotating at 5 MB. In dev both
are echoed to the terminal.

## Verify

```sh
pnpm typecheck     # main + preload tsconfigs
pnpm test          # node --test; pure modules only, Electron is injected through interfaces
```

## Security boundaries

- The application window runs with `sandbox: true`, `contextIsolation: true`,
  `nodeIntegration: false`, no spellcheck, and loads only `reasonix://app`.
  Every navigation away from the app origin is blocked; popups are denied;
  `<webview>` is refused.
- The preload exposes exactly one object, `window.reasonixDesktop`, shaped as
  the protocol document's `ReasonixDesktopHost`. IPC replies are envelopes, so
  a Go error reaches the renderer as `Error(<Go message>)` with no Electron
  prefix.
- `ipcMain` handlers accept calls only from the main window's top frame
  (`event.sender` and `event.senderFrame` are both checked); any other sender
  is rejected and logged.
- `desktop/invoke` names are validated against the embedded contract before
  they reach Go; unknown names fail with a `-32601` error.
- `reasonix://app` serves files strictly under the frontend dist (no `..`,
  no absolute escapes, no directory index fallback except `/`). Only the
  three resource prefixes are forwarded to the loopback origin, and the bearer
  token is attached in the main process; it never reaches any renderer.
- Remote Serve windows use their own `persist:remote-<hostKey>` session, no
  preload, sandbox on, popups denied, navigation pinned to the page origin.
- Website views are sandboxed `WebContentsView`s on the `persist:browser`
  partition (`temp:<id>` for temporary tabs) with a preload that only reports
  user input. `host/browser.*` calls need a grant scoped to one task and one
  service generation; reads and writes refuse a tab in human mode.
- `shell.openExternal` from the renderer accepts `http:`, `https:` and
  `mailto:` only.
- The service is restarted automatically at most three times per five
  minutes after an unexpected exit; afterwards the failure page offers a
  manual restart, the logs folder, and quit. There is no mock fallback.

Packaging (`electron-builder`) is intentionally not part of this package yet.

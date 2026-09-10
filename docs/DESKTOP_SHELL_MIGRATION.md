# Desktop shell migration: Wails to Electron

[简体中文](DESKTOP_SHELL_MIGRATION.zh-CN.md)

This is the architecture decision record and the working plan for replacing
the Wails desktop shell with Electron while keeping the Go kernel, the Go
desktop business layer and the React UI. It is the reference for the migration
branch until the final phase closes; the wire contract lives in
[the host protocol](DESKTOP_HOST_PROTOCOL.md) and the generated entry-point
inventory in `docs/desktop-migration/INVENTORY.md`.

## Decision

Reasonix Desktop moves from Wails v2 (WebKit on macOS, WebView2 on Windows,
WebKitGTK on Linux) to Electron with a Chromium renderer, because the product
needs a native browser that the user and the agent operate together, and no
system webview offers a second, isolated, scriptable web surface with a stable
engine across all four release targets. The Go desktop layer becomes a
standalone service process joined to the shell by one private JSON-RPC
connection. The development branch replaces Wails directly; no dual-shell
product is maintained, and the branch is not released before every acceptance
gate in this document passes.

Alternatives considered and rejected:

- **Keep Wails and embed a browser through CDP to a system Chrome.** Depends on
  an external browser install, cannot share a login partition safely, and gives
  no control over the surface geometry inside the app window.
- **Wails v3 multi-window.** Still one system engine per platform, no
  `WebContentsView` equivalent, and the WebKitGTK/WebView2 rough edges that
  motivated the recovery code stay.
- **Rewrite the desktop layer in TypeScript.** Discards the controller, lease,
  recovery and remote logic that the CLI, Serve and bot frontends share.

Consequences accepted: a larger fixed memory and package footprint, measured
across the complete process tree and reported honestly; two runtimes to keep in
one version unit; Chromium sandbox requirements on Linux.

## Baseline

The migration baseline is `main-v2` at `7717f3eeab47f66560ea85cc7dbe27426c3adf47`,
frozen when the branch was cut. The prototype work at `e2298bd78` (isolated
Electron + Go browser experiment and the ACP MCP-interaction forwarding) is
carried on the branch. The fixes between the two commits (session recovery
visible in welcome layouts, global new-session workspace targeting, settings
search/save-bar overlap) are part of the baseline and must survive.

Wails metrics are captured with `scripts/desktop-shell-metrics.sh` on the same
machine and stored under `docs/desktop-migration/baseline/`. The Electron build
is measured with the same script so the comparison is like for like.

## Architecture

```text
React UI ──typed IPC via preload──▶ Electron main ──stdio JSON-RPC──▶ Go desktop service
                                        │                                │
                                        ├─ WebContentsView (websites)    └─ control.Controller, sessions,
                                        ├─ remote Serve windows              tools, leases, recovery, billing
                                        └─ menu, tray, dialogs, clipboard
Remote Reasonix agent ◀── restricted host RPC over the existing SSH channel ──▶ Go desktop service
```

| Layer | Owns |
| --- | --- |
| React UI | rendering, intent, layout, state projection; no Electron or Go globals |
| Electron main | windows, browser views, menu, tray, dialogs, clipboard, notifications, native lifecycle |
| Go desktop service | every desktop business command, controller ownership, approvals, settings, terminals, SSH, extensions, update coordination |
| Go kernel | unchanged agent, provider, tool, persistence, lease, recovery and billing semantics |
| Remote adapter | forwards session-authorised host capabilities; no second browser implementation |

Contracts (see the protocol document for the wire shapes):

- `DesktopContract`: the reflected command registry over the Go `App` value,
  generated into a TypeScript command table and DTO declarations with a digest
  the handshake verifies.
- `DesktopEvent`: one envelope (`seq`, `generation`, `name`, `args`) carrying
  the existing event payloads unchanged.
- `NativeHost`: the Go interface that replaced direct shell-toolkit calls;
  implemented by the Electron host over `host/*` requests.
- `BrowserExecutor`: the local and remote browser read/act/capture/file
  interface (phase D).
- `HostCapabilityRegistry`: host capability discovery, version negotiation
  and per-session grants; browser tools register through the existing
  capability and tool registry.
- `DesktopLifecycle`: start, ready, hide, restore, quit and update hand-off
  states shared by both processes.

## Phases and status

Status values: `implemented` (code on the branch), `locally tested` (tests or
manual checks on the development machine), `externally verified` (CI or
another platform), `blocked` (with the reason). A phase closes only when its
exit condition is met on every release target.

### A. Freeze the baseline and inventory every entry point

- Branch `feature/electron-desktop-shell` from the frozen baseline with the
  prototype and ACP work carried over: implemented.
- `tools/desktopinventory` generates the inventory of commands, native calls,
  events, frontend bridge uses, CSS markers, persisted files, shell-only Go
  files, release artifacts and CI jobs, each with exactly one class; `-check`
  fails on drift or an unclassified entry: implemented, locally tested.
- Wails baseline metrics: see `docs/desktop-migration/baseline/`.
- This record, the protocol document and the inventory in English and
  Chinese: implemented.

Exit condition: every existing entry has a destination and an acceptance
case. Met for the inventory; acceptance cases are listed under gates below.

### B. Extract the desktop service and the unified bridge

- `nativeHost` interface with the Wails implementation behind it; Go business
  code no longer calls the shell toolkit directly: implemented, locally tested
  (`desktop/native_host*.go`, `go test -short .` green).
- `desktop/internal/hostrpc`: reflection registry, contract digest, TypeScript
  emitter, strict JSON-RPC server over `rpcwire`, event envelope, reverse host
  requests: implemented, locally tested; all 575 commands accepted by the
  registry.
- `reasonix-desktop --host-rpc`: one Go service process for all sessions and
  tabs; `-emit-contract` writes the generated TypeScript and JSON; the RPC
  native host, tray and quit hooks run over the shell connection:
  implemented, locally tested.
- One pnpm workspace under `desktop/` for the frontend and the shell:
  implemented.
- The root Go module stays static-only; the desktop module keeps its own build.

Exit condition: the service starts and is tested without Wails; every command
is mapped by the contract; business code has no direct shell calls.

### C. Electron hosts the complete existing desktop

Main window, trusted preload, error recovery page, service supervisor,
`reasonix://app` asset scheme with forwarded authorised media, window state,
theme, title bar drag, shortcuts, file drop, clipboard, dialogs, remote Serve
windows, menu, tray, background close and restore. The transcript kernel,
stable message identity and single scroll writer are untouched.

Status: the shell (`desktop/electron`), the frontend host adapter
(`src/lib/desktopHost.ts`, boundary gate, one stylesheet with the drag-region
rewrite) and the host-mode routes for tray, remote windows and relaunch are
implemented and locally tested on macOS arm64: `pnpm --dir electron smoke`
boots the real service in a disposable home and passes 12/12 (handshake,
invoke, unknown-command rejection, window bounds, no Node or Wails globals in
the renderer, clean exit of both processes); the full desktop Go lane and the
frontend gates are green. Measured against the Wails baseline with the same
script (`docs/desktop-migration/baseline/README.md`): time to a healthy
frontend is unchanged within noise, process-tree memory is about 280 MiB
higher, and SIGTERM now quits cleanly. Windows and Linux runs of the shell
are external verification items.

Exit condition: the whole existing desktop flow works in Electron with no
mock fallback, no dead controls and no missing events; rapid session
switching never cross-talks.

### D. Production browser with one local/remote executor

Browser panel in the right workspace (tabs per task, address bar, history,
reload, zoom, load errors, downloads, DevTools) managed by a
`BrowserSurfaceManager`; agent capabilities (structure snapshot, screenshot,
navigate, click, type, keys, scroll, tabs, files) through the existing
capability, approval, cancellation and evidence system; user take-over
revokes pending actions; writes record an operation identity before
execution and report executed / not executed / unknown; remote agents reach
the same executor through the SSH-carried host RPC with generation-bound
grants.

Status: implemented, locally tested on macOS arm64. The Electron browser
surface (`desktop/electron/src/main/browser/`: WebContentsView surfaces,
snapshot/refs, trusted actions, generation-bound grants with the
stale/taken-over/no-grant error codes, downloads, screenshots) passes 81/81
unit tests and the shell smoke now opens example.com and verifies the tab
title end to end (15/15). The renderer browser API is fixed at
`window.reasonixDesktop.browser`. The frontend browser panel
(`BrowserPanel`, dock tab, address bar, zoom, DevTools, downloads,
take-over banner, overlay gating) ships as one lazy chunk with the initial
bundle budget ratcheted by measurement (2408.2 → 2408.8 KiB raw, zero
initial-chunk leakage proven by token-level diff). Remote agents reach the
same executor through a 127.0.0.1 loopback broker
(`desktop/browser_broker.go`): per-host generation tokens that die on
reconnect, session-scoped routing with cross-session `no_grant` rejection,
screenshot/download relay over SFTP, and serve capability negotiation so
older remotes keep working; covered by `-race` tests including a real SFTP
round trip. Open items: the remote end-to-end run against a real SSH host,
`browser_upload` reverse staging (the wire passes `files` through; the
broker does not stage remote-to-desktop uploads yet), and the remote
browser acceptance rows in the gate table.

Exit condition: local and remote agents complete real web tasks through the
same tools with identical take-over, approval, file ownership and recovery
behaviour.

### E. Platform features, installation and updates

Electron menu, tray, notifications, file associations, window restore,
single-instance presentation; unchanged product name, install locations,
shortcuts, uninstall identity, data directory and artifact names; Electron
packaging feeding the existing NSIS, nfpm and signing steps; the Go update
coordinator keeps version resolution, signature checks, layout and recovery
with Electron providing prepare-quit and restart; one version unit for shell,
service, assets and helpers; macOS universal, notarised; Linux Chromium
sandbox without `--no-sandbox`; minisign and digest checks unchanged.

Status: implemented, locally tested where the development machine allows.
The install layout members, payload schema 2, shell bootstrap and macOS
hand-off below are on the branch with the desktop module suite green and
Windows/Linux cross-builds passing. The release pipeline now packages the
Electron shell end to end: `desktop/packaging/` assembles the `app/` tree
with @electron/packager (+ universal on macOS), `scripts/desktop-build.sh`
runs the contract drift check and drives packaging without `wails build`,
NSIS installs the tree via `File /r`, the deb ships `/usr/lib/reasonix/app`
with a root-owned 4755 `chrome-sandbox`, the SignPath configurations cover
the tree's PE set with two-stage installer signing kept, and the CI/release
workflows run `packaging/smoke.mjs` against the packaged shell (the
`desktop-linux-webkit41` job is removed; the pinned contract tests were
rewritten to the new entry points with negative guards against
`wails build`). Open items: the four-platform install/upgrade matrix,
real-code-signing and notarisation runs, the SignPath preflight
re-attestation (the artifact-configuration fingerprint changed), and
Windows/Linux runner verification.

Exit condition: all four artifacts install, start and uninstall, and the
Wails→Electron upgrade, Electron→Electron upgrade and failed-install recovery
tests pass.

Design notes for the versioned install layout (Windows and Linux): the
`installlayout` activator whitelists flat regular files inside
`versions/<v>/`. The Electron payload adds one tree member, `app/`, holding
the Electron bundle; the Windows payload manifest moves to schema 2 and lists
every file under `app/` with its digest so the activator validates the tree
before `current.json` moves. `reasonix-desktop(.exe)` stays the active desktop
executable the thin launcher starts: without `--host-rpc` it bootstraps
`app/Reasonix(.exe)` and exits, and Electron spawns the same binary with
`--host-rpc` as the service. Launcher, `current.json`, single-instance
identity and relaunch logic therefore keep their current shape. On macOS the
bundle's main executable is Electron and the Go service lives in
`Contents/MacOS/`; the `.app` swap path is unchanged. Implementation notes:
`installlayout.Member` names are forward-slash paths under the version
directory, either a whitelisted base name or `app/...` (no `..`, absolute
paths, backslashes or symlinks); manifest readers accept schema 1 (flat list)
and schema 2 (flat list plus `app/`); the migration window's
`REASONIX_DESKTOP_SHELL=wails` in-process fallback left with phase F; under the
shell the
macOS hand-off waits for the Electron process (the service's parent, passed
as `-owner-pid`) and reopens the swapped bundle with `open -n` while the shell
only quits.

#### First upgrade from Wails

The first Electron release requires a **manual full-package installation**
when upgrading from v1.38.x. Published clients copy and execute
their existing update helper, which cannot transfer the new `app/` tree.
Release assets therefore carry `install_layout: "electron-v1"`: existing
v1.38.x manifest validation rejects that unknown layout before downloading or
replacing files. The old installation remains usable; its update error view
retains the official download-page link. Quit the old app and install the
complete Windows installer, macOS app, or Linux package from that page.
For a portable archive, extract the complete archive into a new directory;
do not replace only the Go executable. Configuration, sessions and the data
home retain their existing names and formats. macOS also uses this one-time
manual transition because old clients validate the whole platform manifest.

After that transition, Electron clients accept `electron-v1` and publish the
Go service, CLI and complete shell resources as one version before moving
`current.json`. Linux native packages remain owned by the package manager.
Windows update completion waits for the Electron owner to exit and verifies
the new Go service through a data-home-specific named pipe whose server PID
is provided by Windows; Wails endpoint lookup remains for old running apps.
This boundary must be retained on all mirrors and release manifests; changing
the field back to `versioned-v1` would re-enable unsafe legacy automatic updates.

### F. Full-matrix acceptance and removal of the old shell

CI on the new build, contract generation and native test entry points; Wails
entry, dependencies, generated bindings, WebView2 recovery and shell patches
removed; prototype fault cases promoted into real tests; migration aliases,
duplicate DTOs and temporary adapters deleted.

Status: the removal is implemented and locally tested. The Wails entry
(`wails.Run`, `native_host_wails.go`, `wails.json`, the generated `wailsjs`
bindings, the in-process remote-window child processes) is gone, and with it
the WebView2/WebKitGTK recovery coordinators, diagnostics observers, native
smoke harnesses (`cmd/transcript-native-smoke`, `cmd/transcript-selection-smoke`),
the vendored go-webview2 fork, the `webkit2_41` build tag and the CI WebKitGTK
toolchain steps. The desktop module's `go list -m all` is Wails-free; the
frontend reaches only `window.reasonixDesktop` (enforced by
`check-desktop-host-boundary.mjs`) and the test seam is an Electron host stub.
`REASONIX_DESKTOP_SHELL=wails` no longer exists: a plain launch without an
installed shell exits with an install hint. The prototype's crash fault cases
(renderer crash before dispatch cancels the act; crash after dispatch settles
executed without replay; recovery keeps the login partition) run as real tests
in `desktop/electron/src/main/browser/`. Kept on purpose: the fyne systray
in-process fallback behind `startNativeShellSupport` (unreachable under the
shell but still the bare-service path), the legacy crash-report decode fields,
the `com.wails.reasonix-desktop` bundle identity, and the update helper's
`wails-app-` single-instance lookup (upgrade-from-Wails detection). Open: the
four-platform acceptance matrix, the interaction p95 comparison against the
Wails baseline, and CI runner verification on Windows/Linux.

Exit condition: no Wails in the final build graph; no old bridge globals in
business code; every matrix item and gate closed.

## Capability matrix

The generated inventory lists every entry point. This table is the
product-level view the acceptance run follows; each row maps to inventory
classes and to a gate below.

| Capability | Today (Wails) | Target (Electron) | Class |
| --- | --- | --- | --- |
| Sessions: send, stop, model/effort switch, history, recovery, leases | `App` methods over Wails bindings | same methods over `desktop/invoke` | keep-business |
| Projects, worktrees, file preview, workspace watch | Go + asset middleware | Go + `reasonix://app` forwarding to the resource origin | keep-business |
| Terminal | Go PTY/ConPTY, events | unchanged over `desktop/event` | keep-business |
| Settings, MCP, MCP Apps, skills, plugins | Go | unchanged; MCP Apps keep their loopback origins | keep-business |
| Remote workspaces and remote Serve windows | SSH manager + child Wails process per window | SSH manager unchanged; `BrowserWindow` per host with isolated partition | migrate-host |
| Window geometry, theme, drag regions, shortcuts, zoom | Wails runtime | `host/window.*`, preload window API, `-webkit-app-region` | migrate-host |
| File drop, clipboard, external links, dialogs | Wails runtime | preload native API and `host/dialog.*` | migrate-host |
| Menu, tray, background close, second instance | Wails menu, fyne systray, Wails lock | Electron menu, `Tray`, `requestSingleInstanceLock` keyed by canonical home | migrate-host |
| Updater | Go coordinator + Wails relaunch | Go coordinator + `host/app.relaunch` | migrate-host |
| Renderer recovery (WebView2/WebKitGTK) | Go recovery coordinators | Electron `render-process-gone` handling | delete-shell |
| Native browser for the agent | prototype only | `WebContentsView` panel + `BrowserExecutor` | new |

## Data compatibility

- Session, configuration, project, task, billing and lease formats are
  unchanged; the transcript schema is not modified.
- Browser metadata and operation logs are new, versioned files that the old
  shell never reads.
- Website logins live in Chromium persistent partitions; cookie values never
  enter configuration, logs or model context.
- Restored browser tabs keep safe navigation entries only; no passwords, form
  state or replayable submissions are persisted.
- File-backed settings win over old webview-local preferences. The only
  allowed resets are renderer-local appearance preferences (font family, text
  size, panel widths, typography) that lived in the old webview's storage;
  old webview data is left in place and listed in the migration notes.
- Downgrade: stop the Electron build, run the previous Wails build; new
  browser state must not break its session and configuration reads.

## Acceptance gates

| Area | Required scenarios |
| --- | --- |
| Contract | Go/TS signature parity, empty arrays, optional fields, error mapping, cancellation, out-of-order replies, protocol mismatch, large resources |
| Sessions and ownership | send, stop, model/effort switch, rapid project and session switching, background reattach, lease conflicts, failed controller replacement keeps the old session |
| Event recovery | renderer reload, event backlog, subscription loss and re-snapshot; no duplicates, no old-generation writes |
| Desktop capabilities | terminal I/O and resize, file drop, media preview, MCP Apps, settings, automation, remote connections and windows |
| Browser | iframes, dynamic DOM, controlled inputs, popups, upload/download, history, temporary partitions, shared vs isolated logins |
| Take-over and unknown writes | take-over before approval, after approval before dispatch, lost receipt after execution, restart after crash, duplicate operation IDs |
| Remote browser | SSH drop, reconnect generation change, stale token, cross-session misrouting, remote upload/download, remote process recovery |
| Native experience | real CJK IME, focus, selection and copy, shortcuts, title bar, split panes, cross-screen DPI, tray restore on macOS, Windows and Linux |
| Install and upgrade | upgrade while the old build runs, coexisting data homes, relative data home, corrupt signature, interrupted install, failed restart and rollback |
| Isolation | websites and iframes have no bridge; forged IPC, expired resource tokens, out-of-bounds file requests and external protocol calls are handled |

Real-task acceptance: authenticated GitHub PR review draft with sources;
cross-page documentation search saved locally; controlled test-site form
submit, upload and download through approval and take-over; the same tasks
from a remote workspace with the browser local and results owned by the
remote task; interrupted submit with unknown receipt proving no automatic
resubmission after recovery.

Resource and performance sampling follows `scripts/desktop-shell-metrics.sh`
(full process tree; startup, idle, 1/5 tabs, long session, streaming, one
hour) plus 30 open/close cycles for tabs and sessions proving process,
listener, `WebContents` and session resources are released. Interaction p95
(session switch, stop feedback, input latency) must stay within
`max(1.2 × baseline, baseline + 50 ms)` of the Wails baseline on the same
machine. Package size, startup and memory deltas are published as measured;
fixed overhead alone is not a failure, a sustained leak is.

Final evidence is bound to one candidate SHA: root and desktop module tests,
race tests for changed concurrent paths, the complete frontend CI suite, and
native acceptance for the four artifacts.

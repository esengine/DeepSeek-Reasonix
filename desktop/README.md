# Reasonix Studio

A desktop window around the Reasonix Go kernel. The window hosts a React SPA,
and every request the SPA makes reaches the kernel's own HTTP server
(`internal/frontend/serve`) over loopback, so Studio, `reasonix serve` and the remote UI
all answer the same routes.

```
┌──────────────────────────────────────────────────────────────┐
│  renderer (React + TS, Vite)  —  desktop/frontend-next        │
│    fetch("/…") · EventSource("/events")                       │
└───────────────▲────────────────────────────┬─────────────────┘
                │ HTTP + SSE over 127.0.0.1  │
┌───────────────┴────────────────────────────▼─────────────────┐
│  desktop/electron  main process                               │
│    windows, menus, dialogs, tray, the platform opener         │
│    owns OS capability and no business state                   │
└───────────────▲────────────────────────────┬─────────────────┘
                │ spawns, holds the lease    │
┌───────────────┴────────────────────────────▼─────────────────┐
│  cmd/reasonix-studio-host  (main module, CGO-free)            │
│    internal/assembly/boot.Build → internal/session/control.Controller          │
│    (same assembly the CLI uses: providers, tools, gate, …)    │
└───────────────────────────────────────────────────────────────┘
```

The renderer never learns which shell it is in: anything a page cannot do it
asks `HostPort` (`frontend-next/src/port/host.ts`) for, and the main process
answers over IPC that carries OS capability only. The boundaries this runs on
are written down in [`docs/STUDIO_SHELL_BOUNDARIES.md`](../docs/STUDIO_SHELL_BOUNDARIES.md).

## Layout

| Path | What it is |
| --- | --- |
| `electron/` | the shell: main process, preload bridge, packaging |
| `frontend-next/` | the SPA it serves |
| `internal/platform/update/` | manifest, download, verify, apply — shared by the shell and the helper |
| `cmd/update-helper/` | the elevated half of an update (dpkg on Linux) |
| `cmd/studio-manifest/`, `cmd/sign/` | release-side tools |
| `internal/winuninstall/` | taking a previous Windows install over |

The host binary itself lives in the main module at `cmd/reasonix-studio-host`,
because the updater it carries has to stay in the kernel's language.

## Why a nested module

`desktop/` is its own Go module (`module reasonix/desktop`, `replace reasonix =>
../`). The parent module's `go build / vet / test ./...` skip this directory
while the import path stays under `reasonix/`, so it can still import the
`reasonix/internal/*` kernel. The module is CGO-free, so no platform carries
build dependencies of its own.

## Prerequisites

- Go (matches the parent module).
- Node 24+ and **pnpm 10** (`npm install -g pnpm@10`).

## Running it

```sh
make studio          # build the SPA + kernel and launch the Electron shell
```

One launch path on every platform. The dev build is already an .app, so macOS
treats it as a real GUI app and native panels — "Add a folder…" — open normally.

For frontend iteration, run the kernel and point Vite at it:

```sh
go run ./cmd/reasonix serve                       # from the repository root
cd desktop/frontend-next && pnpm install && pnpm dev   # :5273, proxies API paths
```

## Messages sent during a run

**Settings → Appearance → Keyboard shortcuts → Messages sent while running** chooses the default:

- **Steer at the next tool boundary** (default) adds guidance to the current run.
- **Send after the whole run** queues a new turn until the active run finishes.

The choice is local to this device's browser profile, applies across projects,
and survives reloads. It does not change idle sends or messages already queued.
Skill invocations continue to use follow-up turns.

The queue still supports hold/resume, editing, and taking a message back into
the composer. **Send now** overrides the wait for a selected message by stopping
the current response and starting that message as a new turn; it is unavailable
while the queue is held.

Guidance not delivered before a run ends still becomes a follow-up through the
kernel's existing queue.

## Global show/hide shortcut

In the desktop shell, **Settings → Appearance → Global show / hide shortcut**
offers one opt-in binding, disabled by default.

1. Click **Record shortcut**, then press a modifier and key combination.
   Escape cancels recording.
2. Review the platform-native label, then **Apply shortcut** to register it.
3. **Clear shortcut** disables it immediately.

You can also edit the Electron accelerator directly (up to 128 characters).
The shell requires Control, Command, Alt or Super, even through IPC or when
restored from disk. Shift may accompany one of them but cannot qualify alone.

The shortcut hides a focused Studio window; otherwise it restores, shows and
focuses the existing window. It does not change in-app shortcuts or tray settings.

The primary Electron instance owns the binding and stores it in its local shell
profile. Changes apply immediately; invalid or unavailable bindings leave the
previous saved and registered binding intact.

Startup registration failures are shown in settings rather than reported as
success. An unavailable binding may be occupied, reserved by the OS, denied
permission, or unsupported by the desktop.

Native Wayland environments may not support registration with this Electron
backend; no portal authorization or cross-platform success is implied.

The binding exists only while Studio is running. It cannot launch a stopped
application and no external daemon is installed. Browser and network viewers
cannot configure a remote machine's global shortcut.

## Building and testing

```sh
make studio-test                                  # go test ./... for this module
cd frontend-next && pnpm typecheck && pnpm test && pnpm build
```

The Linux/X11 and macOS shortcut smoke drives the real Studio window, chord
capture, and OS key events. After building the host and frontend, run
`pnpm shortcut-live` in `electron/`. macOS uses System Events and requires
Accessibility/Automation permission for the launching process.

Use isolated `REASONIX_HOME` and `XDG_CONFIG_HOME` directories; the smoke seeds
a test provider at an unreachable loopback port.

- On Linux/X11, `REASONIX_XDOTOOL` names `xdotool`.
- On Linux/X11, `REASONIX_XMESSAGE` optionally names `xmessage` for the separate-app focus check.
- `REASONIX_SCREENSHOTS` optionally names the screenshot output directory.

This test requires a working display. Windows and Wayland are not covered by
this native smoke; successful builds do not establish native shortcut behavior.

Packaging runs through electron-builder; `release-studio.yml` drives it on all
three platforms. Linux ships a `.deb` because that is what makes Studio
self-updating there. See `docs/STUDIO_RELEASE.md`.

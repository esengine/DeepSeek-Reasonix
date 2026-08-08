# Go / No-Go decision artifact — Electron serve PoC

**Date:** 2026-08-08  
**Scope:** Phases 0–3 of 方案 A (loopback `reasonix serve` + Electron shell +
HttpSseHost + multi-tab UI + `/desktop/*` sidebar metadata)  
**Official desktop:** remains **Wails** (`desktop/`). This tree is **additive/experimental**.

## Decision

### **GO — experimental shell only**

Ship and maintain `electron-poc/` as a **community/experimental** path for:

- validating dual-process supervision (`token-file` / `port-file` / `pid-file`)
- validating HTTP+SSE host against the P0 serve surface
- optional local use when Chromium/Electron is preferred for debugging

### **NO-GO — product replacement**

Do **not**:

- replace Wails as the official desktop release line
- promise multi-tab / terminal / remote / bot parity via Electron
- add Electron to the official release matrix, signing, or auto-update channel
- rewrite agent/provider/tools in Node

## Evidence used

| Check | Result |
| --- | --- |
| Supervisor args loopback + token-file only | Covered by unit tests + smoke |
| HttpSseHost P0 routes + JSON CSRF + SSE wire map | Unit/integration tests against real HTTP |
| Dual launch GET `/status` | `scripts/dual-launch-smoke.mjs` |
| Capability gap written | `docs/CAPABILITY_GAP.md` |
| Wails path unchanged as default product | No removal of `desktop/` Wails bindings; host is opt-in |

## Revisit triggers (would re-open product discussion)

1. Serve gains first-class multi-session API and Desktop parity closes gap to &lt;20% of bridge surface.
2. Wails/WebView becomes an unfixable support burden on target platforms.
3. Explicit product requirement for an Electron distribution with staffing for dual-shell CI.

## Recommendation to maintainers

Keep investing in **Wails desktop + shared `control.Controller`**. Treat Electron+serve as a **thin host proof**, reusing Remote’s supervision contract, not as a second full desktop.

**Status: GO for experimental PoC · NO-GO for replacing Wails.**

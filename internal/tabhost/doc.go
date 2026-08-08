// Package tabhost is a transport-agnostic multi-Controller session host.
//
// It owns many independent control.SessionAPI instances (one per Tab), stamps
// tabId/runtimeEpoch onto eventwire payloads, and is the shared runtime behind
// multi-tab reasonix serve and the Electron desktop shell.
//
// Design constraints (see docs/TABHOST_CONTRACT.md):
//
//   - Tab semantics align with desktop WorkspaceTab / TabMeta, not a new slang.
//   - This package is a frontend-layer package (may import control); leaves must not.
//   - Wails desktop is not required to migrate onto tabhost in MVP-B.
//   - Single-session serve remains the default; multi-tab is opt-in.
//
// Layering: listed in tools/repolint/layers.go frontends.
package tabhost

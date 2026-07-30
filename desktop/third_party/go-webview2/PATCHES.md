# Reasonix WebView2 Patch

This directory contains the Windows packages used by Wails v2 from
`github.com/wailsapp/go-webview2` v1.0.23 (commit
`e08e9397f04ad0b1050d7e5bdc8e27faddc87987`). The original MIT license is in
`LICENSE`.

Reasonix carries one behavioral patch in `pkg/edge/chromium.go`: WebView2
monitor-scale detection remains enabled while raw-pixel bounds mode is active.
Without it, a frameless window minimized on a mixed-DPI Windows desktop can
adopt another monitor's rasterization scale and restore with stale WebView
bounds.

The diagnosis and upstream implementation are documented in:

- https://github.com/esengine/DeepSeek-Reasonix/issues/5862
- https://github.com/wailsapp/wails/issues/5544
- https://github.com/wailsapp/wails/pull/5734

Remove this replacement when Wails v2 publishes an equivalent fix. Until then,
keep the copied packages aligned with the version required by `desktop/go.mod`
and retain `TestWebView2OwnsMonitorScaleDetection`.

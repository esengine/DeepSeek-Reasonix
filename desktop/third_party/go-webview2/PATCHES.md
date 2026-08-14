# Reasonix WebView2 compatibility patch

This directory is rebased on Wails WebView2 v1.0.28, upstream commit
`5539733d1b61b7584f9665d72f0256ffd52096ff`, from the `webview2/` module in
`github.com/wailsapp/wails`. The upstream MIT license is retained in `LICENSE`.

Wails v2.13 still imports `github.com/wailsapp/go-webview2/pkg/edge`, while
v1.0.28 moved to `github.com/wailsapp/wails/webview2`. The vendored module and
its self-imports therefore keep the legacy module path as a compatibility
adapter; all other upstream v1.0.28 files are copied without API redesign.

The v1.0.28 upstream implementation owns monitor-scale detection. Reasonix does
not call `PutRasterizationScale` or maintain a DPI scaling fork.

Reasonix carries only these behavioural additions in `pkg/edge`:

- process-failure diagnostics, including reason, exit code and source module;
- one native renderer `Reload`, bound to its exact navigation ID;
- recovery observer events consumed by the desktop restart coordinator;
- `--no-proxy-server` for the embedded/loopback UI (network clients remain in
  the Go host).

Obsolete pre-native-loader DLLs from v1.0.23 were removed. Update this file,
the upstream commit, and the Windows cross-compile test together on each rebase.

Upstream references:

- https://github.com/wailsapp/wails/releases/tag/webview2%2Fv1.0.28
- https://github.com/wailsapp/wails/pull/5734
- https://github.com/esengine/DeepSeek-Reasonix/issues/5862
- https://github.com/wailsapp/wails/issues/5544

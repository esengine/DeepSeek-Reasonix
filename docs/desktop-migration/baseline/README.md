# Wails baseline metrics

Captured with `scripts/desktop-shell-metrics.sh` on the frozen baseline build
(`wails build -platform darwin/arm64`, Apple silicon, macOS 26.6), three runs,
fresh disposable data home each run, `REASONIX_DEV=1`. Medians below; the raw
runs are the JSON files beside this file. Electron builds are measured with the
same script and compared only against these figures.

| Metric | Median of 3 |
| --- | ---: |
| Go `startup` → lifecycle `ready` | 332 ms |
| Go `startup` → frontend `healthy` (React mounted, bridge heartbeat) | 3692 ms |
| Process-tree RSS, healthy + 2 s | 389 MiB |
| Process-tree RSS, healthy + 10 s | 385 MiB |
| Process-tree RSS, healthy + 30 s | 412 MiB |
| Processes in the tree | 4 (com.apple.WebKit.GPU, com.apple.WebKit.Networking, com.apple.WebKit.WebContent, reasonix-desktop) |
| SIGTERM honoured within 10 s | no (the Wails shell ignores SIGTERM; the script had to SIGKILL it) |

Run 1 includes first-launch work in a cold data home; runs 2 and 3 reuse the
warm OS file cache. Startup here starts at Go `main`, not at process creation,
and `healthy` is the point where the React app has rendered and the bridge
heartbeat succeeded. Interaction latency (session switch, stop feedback, input)
is captured separately by the frontend benchmarks under `desktop/frontend/bench`.

## Electron, same script, same machine

Development build of the shell (`desktop/electron`, Electron 44.2.0) started
through `desktop/electron/scripts/metrics-launch.sh` with a Go service built
with `-X main.version=v0.0.0-electron`, three runs, fresh data home each run.
Startup starts at the Electron `main` process launch (Chromium initialisation
included), so "ready" is later than the Wails figure, which started at Go
`main`.

| Metric | Wails (median of 3) | Electron (median of 3) | Delta |
| --- | ---: | ---: | ---: |
| launch → Go lifecycle `ready` | 332 ms | 448 ms | +116 ms |
| launch → frontend `healthy` | 3692 ms | 3742 ms | +50 ms |
| Process-tree RSS, healthy + 2 s | 389 MiB | 665 MiB | +276 MiB |
| Process-tree RSS, healthy + 10 s | 385 MiB | 668 MiB | +283 MiB |
| Process-tree RSS, healthy + 30 s | 412 MiB | 697 MiB | +285 MiB |
| Processes in the tree | 4 | 5 (Electron, Electron Helper, Electron Helper (Renderer), reasonix-desktop-service-versioned) | |
| SIGTERM honoured within 10 s | no | yes (247 ms) | |

The fixed cost is the Chromium runtime and a second renderer-class process;
time to a healthy frontend is unchanged within noise. The unpackaged shell
loads the UI from `frontend/dist` through the `reasonix://app` handler, the
same path the packaged build uses. Idle growth between 2 s and 30 s is the
same order for both shells and is re-measured over an hour in the phase F
acceptance run.

The phase F acceptance run on the packaged macOS artifact (startup/idle,
1/5 browser tabs, 35-cycle tab/session leak loops, one-hour sustained use,
interaction p95 evidence and gaps) is recorded in `PHASE_F_ACCEPTANCE.md`
beside this file.

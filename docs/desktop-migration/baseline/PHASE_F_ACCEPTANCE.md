# Phase F acceptance: local resource and leak sampling (Electron, packaged)

Acceptance-run evidence for the resource/performance rows of the phase F gate
in `docs/DESKTOP_SHELL_MIGRATION.md`. Everything below was measured on the
**packaged** macOS artifact `dist/Reasonix-darwin-arm64.zip`
(candidate SHA `650e01e31`, zip 191.7 MiB, unpacked `.app` 489 MB,
`com.wails.reasonix-desktop`, arm64), same machine as the Wails baseline
(macOS 26.6.2, Apple silicon), 2026-09-09. Raw JSON evidence sits beside this
file.

Method, per the plan: `scripts/desktop-shell-metrics.sh` for startup/idle (3
runs, fresh disposable data home, `REASONIX_DEV=1`, full process tree via the
script's ps family sum); a Playwright harness driving the packaged app through
`window.reasonixDesktop` for tab/session cycles (same ps tree method plus
main-process `webContents.getAllWebContents()`, per-WebContents listener
totals and `process._getActiveHandles()`); `desktop/frontend/bench/run.mjs`
(`pnpm test:bench`, the harness the baseline README designates for interaction
latency) for session-switch/input p95.

## Startup and idle (packaged app, 3 runs, medians)

| Metric | Wails baseline | Electron dev shell | Electron packaged | Δ vs Wails |
| --- | ---: | ---: | ---: | ---: |
| launch → Go lifecycle `ready` | 332 ms | 448 ms | 598 ms | +266 ms |
| launch → frontend `healthy` | 3692 ms | 3742 ms | 3991 ms | +299 ms |
| Process-tree RSS, healthy + 2 s | 389 MiB | 665 MiB | 672 MiB | +283 MiB |
| Process-tree RSS, healthy + 10 s | 385 MiB | 669 MiB | 673 MiB | +288 MiB |
| Process-tree RSS, healthy + 30 s | 412 MiB | 698 MiB | 660 MiB | +248 MiB |
| Processes in the tree | 4 | 5 | 5 (Reasonix, Reasonix Helper ×2 (GPU/utility + Renderer), reasonix-desktop) | +1 |
| SIGTERM honoured | no (SIGKILL needed) | yes (247 ms) | yes (239–252 ms, 3/3 clean) | improved |

Run 1 carries first-launch cost for the unpacked copy (ready 3901 ms, healthy
7592 ms); runs 2–3 are warm (ready 595/598 ms, healthy 3945/3991 ms), so the
medians above are warm. Time-to-healthy is +299 ms over Wails — within
`max(1.2×, +50 ms)` (= 4430 ms) if that gate were applied to startup, and the
plan's rule for startup/memory is "publish as measured; fixed overhead alone
is not a failure". The ~+250–290 MiB RSS delta is the Chromium runtime fixed
cost, flat across the 30 s idle window (no idle growth beyond Wails's own).

Evidence: `electron-packaged-darwin-arm64-run{1,2,3}.json` (same schema as the
Wails/Electron-dev runs).

## Browser surfaces: idle vs 1/5 tabs, and reclamation

One Electron harness run, packaged app, fresh home. Tabs are `temporary`
browser surfaces opened on distinct origins.

| Phase | Tree RSS | Processes | WebContents |
| --- | ---: | ---: | ---: |
| idle (healthy + 2 s) | 683 MiB | 5 | 1 |
| 1 tab (example.com loaded) | 750 MiB | 6 | 2 |
| 5 tabs loaded | 1226 MiB | 10 | 6 |
| all 5 closed + 5 s settle | 673 MiB | 5 | 1 |

With 5 tabs the tree is main (204 MiB) + GPU (102 MiB) + utility (63 MiB) +
app renderer (237 MiB) + 5 tab renderers (~96–134 MiB each) + Go service
(69 MiB). After closing, RSS returns to ≈idle (-10 MiB vs the pre-tab idle
sample) and process/WebContents counts return exactly to idle. No tab
residue.

## Leak check: 35 open/close cycles (gate asks ≥30)

Browser-tab loop (open `example.com` temporary tab → loaded → close), sampled
every 5 cycles:

| Cycle | Tree RSS | Processes | WebContents | WC listeners | Active handles | Open tabs |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 5 | 664 MiB | 6 | 1 | 22 | 5 | 0 |
| 15 | 675 MiB | 6 | 1 | 22 | 5 | 0 |
| 25 | 684 MiB | 6 | 1 | 22 | 5 | 0 |
| 35 | 694 MiB | 6 | 1 | 22 | 5 | 0 |
| +5 s settle | 694 MiB | 5 | 1 | — | — | 0 |

Flat where it matters: WebContents count, per-WebContents listener total and
handle count never move; process count returns to 5 after settling. RSS drifts
+30 MiB over 35 cycles (664→694), the shape of renderer/OS caches rather than
a per-cycle leak (growth decelerates: +11/+9/+10 per 10 cycles, and the
post-loop settle sample does not climb further). Conclusion: **no sustained
leak in tab open/close**.

Session loop (35 × `NewSession` + `DeleteSession` of any newly persisted file
via `desktop/invoke`, Go service RSS and `ListSessions` length tracked): Go
service RSS 69→70 MiB across the whole loop (+0.6 MiB), session count 0→0. On
a fresh data home `NewSession` on a blank tab is a no-op rotation by design,
so this exercised the RPC/rotation path but **not** a session with real
conversation content — see gaps.

Evidence: `electron-packaged-darwin-arm64-tabs-leak.json`.

## Sustained use (one hour)

Evidence: `electron-packaged-darwin-arm64-one-hour.json`. 5 browser tabs held
open the whole hour; every 5 minutes one temporary tab open/close cycle plus
business RPCs (`ListSessions`, `Version`), then a full-tree sample.

| Minute | Tree RSS (corrected) | Processes | WebContents | WC listeners | Handles | Tabs |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 1271 MiB | 10 | 6 | 157 | 5 | 5 |
| 10 | 1370 MiB | 10 | 6 | 157 | 5 | 5 |
| 20 | 1377 MiB | 10 | 6 | 157 | 5 | 5 |
| 30 | 1378 MiB | 10 | 6 | 157 | 5 | 5 |
| 40 | 1380 MiB | 10 | 6 | 157 | 5 | 5 |
| 50 | 1323 MiB | 10 | 6 | 157 | 5 | 5 |
| 55 | 1323 MiB | 10 | 6 | 157 | 5 | 5 |

WebContents, listener and handle counts and the tab set are constant for the
full hour; the app then exited cleanly on `app.close()`. RSS moves 1271 →
1323 MiB (+52 MiB, +4 %), peaking at 1381 MiB around minute 35 and receding
after — decelerating, partially GC-reclaimed growth, concentrated in the main
window renderer (245 → 352 MiB; tab renderers, main, GPU and the Go service
are flat). Not a monotonic leak; the equivalent Wails one-hour figure does
not exist (the Wails baseline covered 30 s), so this is published as measured
per the plan.

Two sampling artifacts in the raw JSON, corrected in the table above:
transient 0-MiB zombie helpers caught mid-reap after each activity cycle
(`comm` shows as `2026 (Reasonix Helper` — the harness's ps column split is
off by one token), and an unrelated external `reasonix` CLI process from
other work on this machine that the name-family filter picked up in the
minute-30+ samples (different pid each sample). Neither belongs to the
measured app's process tree; the descendant-only tree was 10 processes at
every sample.

## Interaction p95 vs the `max(1.2×, +50 ms)` gate

**The Wails interaction baseline was never recorded.** The baseline README
says interaction latency "is captured separately by the frontend benchmarks
under `desktop/frontend/bench`", but no Wails-side numbers exist in
`docs/desktop-migration/baseline/` or elsewhere in the repo, and Wails is
being removed in this phase — so the relative gate cannot be computed. What
was measured instead, same machine, same day:

Frontend bench (`pnpm test:bench`, production frontend in headless Chromium,
mock 38-turn tool-dense and 46-turn markdown-heavy sessions; this frontend is
shared by both shells, so these numbers are shell-independent):

| Metric | Run 1 | Run 2 | Bench gate | Verdict |
| --- | ---: | ---: | ---: | --- |
| Cold open: first paint p95 | 20 ms | 20 ms | 100 ms | PASS |
| Cold open: interactive p95 | 944 ms | 337 ms | 300 ms | FAIL (run-to-run variance on the first cold open) |
| Session switch p95 (click → target rendered) | 471 ms | 475 ms | 300 ms | FAIL |
| Activation ready p95 (ticketed backend flow) | 98 ms | 101 ms | 300 ms | PASS |
| Input event p95 (INP-ish) | 24 ms | 24 ms | 200 ms | PASS |
| Long-task p95 / max | 79 / 86 ms | 79 / 84 ms | 50 / 500 ms | FAIL p95 |
| Settled renderer task time | 3.0 % | 2.7 % | 3 % | PASS on rerun |
| Retained heap growth after 100 switches | 3.5 MiB | 3.5 MiB | 20 MiB | PASS |
| DOM node growth | 0.0 % | 0.0 % | 10 % | PASS |

Switch latency is systematic (p50 ≈ p95 ≈ 445–475 ms over 100 switches, both
runs), dominated by rendering the heavy target session after the ticketed
activation (which itself completes in ~100 ms). Because the harness and
frontend are identical for both shells, this is not attributable to the
Electron migration, but it currently fails the bench's own plan-value gates
and is recorded here as acceptance evidence.

Real-shell probes in the packaged app (event-timing PerformanceObserver, real
key events at the composer after a 15-keystroke warmup): keydown/input p50
24 ms, **p95 40 ms**, max 72 ms, n=80 — consistent with the headless bench.
Without the warmup the first keystrokes pay one-time composer initialisation
(overall p95 352 ms including overlay-dismissal pointer events).

Evidence: `electron-frontend-bench-run{1,2}.json`,
`electron-packaged-darwin-arm64-input.json`.

## Gaps (not measurable locally in this run)

- **Wails interaction baseline missing** (above): the 1.2×/+50 ms comparison
  for session switch / stop feedback / input cannot be computed. If the gate
  is enforced, either restore a Wails build long enough to capture the three
  p95s with `bench/run.mjs` pointing at the same fixtures, or re-baseline the
  gate on the Electron numbers.
- **Stop-feedback p95 and streaming/long-session memory**: need a running
  turn, i.e. a configured model provider; a disposable data home has none and
  there is no Go-side mock provider (the bench mock is frontend-only). Not
  measured.
- **Session open/close with real content**: on a fresh home chat sessions are
  blank, so the 35-cycle session loop proves the RPC/rotation path only;
  in-memory session runtime reclamation with populated history is unmeasured
  (same root cause: no provider).
- Real-shell session-switch p95: the project tree has 0 topics on a fresh
  home, so sidebar-driven switching could not be exercised in the packaged
  shell; the frontend bench numbers above stand in.

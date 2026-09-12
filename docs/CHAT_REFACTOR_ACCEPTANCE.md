# Reasonix chat refactor: delivery and acceptance

[简体中文](CHAT_REFACTOR_ACCEPTANCE.zh-CN.md) · [Architecture and capability decisions](TRANSCRIPT_ARCHITECTURE.md) · [Environment](evidence/chat-refactor/environment.json)

> This report covers the 2026-09-12 natural-flow refactor. The subsequent content-host, trusted file-fact, unified resource, and call-detail implementation is documented in [Chat Content and Interaction Host](CHAT_CONTENT_HOST.md), with fresh 2026-09-13 Chromium, WebKit, and Electron evidence.

Date: 2026-09-12. Worktree: `fix/harness-chat-transcript`, based on `0e5319ad145b2c40dd2601ac0d7be0782556114b`; Harness reference: `c291e7961a`. Verification includes uncommitted changes. Build commit fields identify the base, not a newly created delivery commit.

## Delivered behavior

Local and remote sessions now share natural document flow, accumulated loaded history, ChatSource node subscriptions, a single ChatScrollController, turn-process disclosure and an overlay tool drawer. The virtual chat/window/ledger paths, logical selection overlay, old question navigator, message editing and old reasoning panel have been removed.

Answers keep their complete source and existing safe Markdown, math, diagrams, images and citations. Thoughts/tools preview 8,000 characters; code initially shows 200 lines and copies the full source. Chat actions retain copy, eligible ordinary conversation forks and genuine recovery. Rewind, worktree-fork and delivery/acceptance workflows no longer appear in chat. Composer, approvals, questions, model controls and the workbench remain connected.

Full tool reads bypass archived previews and resolve only the selected call. Concurrent answer/thought resolution merges individual fields, including batched remote React commits. No backend API, stored history shape, permission policy, model input or prompt-cache bytes change. See the [capability table and architecture](TRANSCRIPT_ARCHITECTURE.md); the product has one renderer and no legacy toggle.

Legacy history no longer prefetches whole-page full content outside the four-request budget. Legacy tools resolve call-specific references; stale body references cannot be copied as though their preview were complete.

## Regression results

| Check | Result |
| --- | --- |
| Full frontend discovery | 337 suites passed; subsequent changes received targeted reruns. [Log](evidence/chat-refactor/unit-discovery.log) |
| Transcript, store, Markdown and concurrency | Passed: 120 isolated node updates, stable order/final identity, native selection, chunked content, four-request concurrency, independent fields and stale drawer targets. [Log](evidence/chat-refactor/transcript.log) |
| Original #185 class of failure | Real Transcript sustained 60 content/geometry commits; browsers separately replayed 60 size changes and streaming |
| Stream, composer, remote, lifecycle and motion | Applicable suites passed; remote/lifecycle rerun after final wiring. [Remote](evidence/chat-refactor/remote.log), [lifecycle](evidence/chat-refactor/app-lifecycle.log), [motion](evidence/chat-refactor/motion.log) |
| Reading/input | Real keys, PageUp, browser touch input, wheel, composer wrap/manual resize and composition-event replay passed, with 0 px reader drift. [Log](evidence/chat-refactor/composer-browser.log) |
| Application composition | Model/layout/file/terminal changes, local/remote navigation, remote new session, send and stop preserve Composer/workspace identity. [App](evidence/chat-refactor/app-browser.log), [files](evidence/chat-refactor/dock-browser.log) |
| Remote recovery | Disconnect, unknown runtime, reconnect, missing turn_done reconciliation and local ownership passed. [Log](evidence/chat-refactor/runtime-browser.log) |
| Layout | Chromium: 39 scenarios; actual Electron: 42, including 0.8/1/1.25 zoom. [Chromium](evidence/chat-refactor/layout-chromium/layout.json), [Electron](evidence/chat-refactor/layout-electron/layout.json) |
| Types, lint, build and bundle | Passed with existing budgets, single-writer and module-boundary gates. [Build](evidence/chat-refactor/build.log), [test types](evidence/chat-refactor/typecheck.log) |

Browser replay checks maximum-update-depth failures, ResizeObserver loops and page errors. Exceptions were not swallowed, #185 did not trigger automatic remounting, and thresholds were not relaxed to pass.

## Measured performance

Darwin arm64, Node 24.19.0; Chromium 153.0.8010.12, Playwright WebKit 26.6 and actual Electron 44.2.0. The production fixture uses real Transcript, Markdown and Composer, accumulates 60-turn pages and collects at least 30 real input/paint samples per size. It is not a live-backend end-to-end test.

| Engine | 240-turn input P95 | 1,000-turn input P95 | 1,000-turn longest task | 20-switch P95 | Maximum absolute stream/prepend anchor drift |
| --- | ---: | ---: | ---: | ---: | ---: |
| Chromium | 40.0 ms | 92.2 ms | 102 ms | 32.8 ms | 0.281 px |
| WebKit | 45.0 ms | 99.0 ms | API unavailable | 30.0 ms | 0.719 px |
| Electron | 37.7 ms | 73.1 ms | 100 ms | 15.3 ms | 0.219 px |
| Retained gate | ≤200 ms | ≤200 ms | ≤500 ms | ≤300 ms | ≤2 px |

Raw samples, page timings, tasks and DOM counts: [Chromium](evidence/chat-refactor/chromium-results.json), [WebKit](evidence/chat-refactor/webkit-results.json), [Electron](evidence/chat-refactor/electron-results.json). Zero means no observed Long Tasks API entries; WebKit reports `null`, which cannot establish that its long-task gate passed.

The separate complete-workbench benchmark ran five cold opens and 100 alternating heavy-session switches through real application composition and existing mock histories. Every existing gate passed:

- Cold first-paint P95: 44 ms; interactive P95: 238.2 ms.
- Click-to-target-rendered P95: 295.8 ms; activation-ready P95: 18.9 ms.
- Input-event P95: 16 ms; observed maximum long task: 0 ms; maximum Worker parse: 67.9 ms.
- Settled renderer CPU: approximately 0.6%; retained heap growth: 3.6 MiB against the 20 MiB gate.
- Body/Markdown cache budgets passed; DOM growth versus warmup was 0%.

See [raw workbench results](evidence/chat-refactor/workbench-results.json) and [gate output](evidence/chat-refactor/workbench.log). The smaller chat-only 20-switch forced-GC test grew by 88,856 bytes; these are different workloads and should not be conflated.

## Continuous replay, expansion and convergence

Chromium completed 1,306 cycles over 60.076 seconds on 240 accumulated turns, continuously streaming and typing/deleting, with periodic scrolling, disclosure and session replacement. No input loss, duplication or browser errors occurred.

The separate 1,000-turn expansion workload opened every process and thought, navigated every 20 turns to activate visible formatting, expanded formatted long code and loaded one tool's full result. It took 7.936 seconds, reached 64,489 DOM elements and recorded a 128 ms task. Browser-reported heap was approximately 109,000,000 bytes, without forced GC; this is not a leak measurement. The product holds one tool drawer at a time, so this does not claim simultaneous retention of 1,000 full tool results or fixed cost for extreme expansion.

Convergence requires zero pending Worker work and 250 ms without layout writes within a five-second deadline, followed by one second with no additional write sequence. IntersectionObserver may admit a final parse after a transient zero-pending snapshot; that snapshot alone is not completion. A continuous layout loop cannot pass this bounded check.

Development failures are retained: [one Electron input-P95 failure](evidence/chat-refactor/electron-input-initial-failure.log), [premature idle sampling](evidence/chat-refactor/electron-idle-initial-failure.log), and [raw samples](evidence/chat-refactor/electron-initial-results.json). The isolated input failure does not establish an environmental root cause. Final reruns are reported above with unchanged thresholds.

## Screenshots

![Chat body, process summary, turn actions and existing Composer](evidence/chat-refactor/chromium-chat.png)

![Tool overlay leaves the background column width unchanged](evidence/chat-refactor/chromium-details.png)

[WebKit chat](evidence/chat-refactor/webkit-chat.png) · [Electron chat](evidence/chat-refactor/electron-chat.png) · [Electron layout](evidence/chat-refactor/layout-electron/layout.png)

## Explicit verification limits

- Actual Electron was tested in an isolated hidden host with production chat components. A packaged full-app native-input soak and long macOS system-IME test in an isolated graphical environment were not run. Browser composition replay does not replace them.
- Windows/Linux native desktop suites were not run. The Linux/Xvfb native-scrollbar-drag variant remains wired but was not run here. Chromium touch testing uses browser input protocol, not physical hardware; dedicated WebKit touch testing was not run.
- An additional hidden-Electron scrollbar attempt timed out: the native gutter measured 0, so the pointer selected document text. Reader intent correctly released follow, but no scrollbar was dragged. The test now requires an exposed native gutter to prevent a false pass; native-thumb acceptance remains unverified. [Diagnostics](evidence/chat-refactor/native-thumb-electron/electron-results.json), [screenshot](evidence/chat-refactor/native-thumb-electron/electron-native-thumb.png), [failure log](evidence/chat-refactor/native-thumb-electron/run.log).
- The repository now uses Electron across desktop platforms; WebView2/WebKitGTK are retired. Playwright WebKit is not evidence for those hosts.
- WebKit does not expose Long Tasks API, so its long-task gate is unverified. Electron/WebKit did not receive separate forced-GC heap measurements; Chromium chat/workbench tests establish the measured heap gate.
- No live remote-server network end-to-end run was performed. Remote evidence comes from protocol/connection tests and real-app mock browser scenarios. A 60-second replay is not an hour-long native soak.
- Very old records containing several ID-less calls may have ambiguous chunk references. In that case details preserve the preview and report an error instead of copying another call's data.

Implementation and executed automated checks are delivered; this is not a claim that every platform acceptance item is complete. No automatic merge, release or closure of other PRs was performed.

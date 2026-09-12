# Chat presentation design QA

## Harness source port, 2026-09-12

- Source provenance and host adaptations: `desktop/frontend/src/components/harness-chat/README.md`.
- Ported disclosure, reasoning, turn-process, context-record, tool-row, terminal,
  diff, web-source, status and folding primitives; copied scoped Markdown styling.
- Preserved Reasonix's safe-link host, Markdown workers, composer, approvals,
  fork capability checks and content API. No feedback voting controls.
- Removed Settings → Conversation experience → Standard/Deep and its search
  summary. Kept the approval control and persisted compatibility field.
- Completed tool errors can fold with a successful final answer; a visible
  failed-call count remains in the process summary. Interrupted/terminal
  failure records stay visible. Permission text is disclosed on demand.
- In real Electron, opened the user's existing weather turn, expanded and
  collapsed its seven tools, and verified the two failed calls remained visible
  in the summary. Inspected the settings screen and confirmed there are only
  the three default approval choices in that section.
- Production ZIP: SHA256 `3409b930ca85eebc7dae8e266821300e2358518827736394602769c4ddf303ce`.
  Build `v0.0.0-dev`, commit label `0e5319ad145b` plus current worktree changes,
  build time `2026-09-12T12:54:05Z`, macOS arm64 / Electron 44.2.0.
- Ad-hoc signature verification and packaged smoke passed: 3.1s handshake,
  real renderer `Version` call, normal shell and service exit. The user-facing
  launch uses the isolated `Reasonix-Test-ChatRefactor` profile.
- The final font-only correction uses a root-owned token for portaled usage/time
  dialogs; the typography contract passed all 165 assertions. Long-history
  replay numbers above were captured before that portal-only correction.
- Browser evidence: `docs/evidence/harness-chat-port/`. Synthetic weather
  screenshots use fixture data, not a fresh weather query. Each tool header is
  24px high; collapsed processes do not mount tool bodies.
- WebKit 26.6: 1,000 turns input P95 103ms, switch P95 40ms. Its Long Tasks
  API is unavailable, so no maximum-long-task claim is made for WebKit.
- Chromium 153: 1,000 turns input P95 120.7ms, max long task 138ms,
  switch P95 32.8ms, released heap growth 125,396 bytes over 20 switches.
- Electron 44.2.0 replay: 1,000 turns input P95 43.4ms, max long task 142ms,
  switch P95 20.3ms. Stream anchor drift 0px; prepend drift 0.094px.
- Native WebView hosts on other platforms, native IME soak and exposed native
  scrollbar dragging were not rerun in this pass. This is a local test build,
  not a published or notarized release.

## Earlier footer verification

## Reference

- DeepSeek Harness commit: `c291e7961a`
- Source components:
  - `<deepseek-harness>/packages/client/ui-chat/src/client/chat/TurnUsagePanel.tsx`
  - `<deepseek-harness>/packages/client/ui-chat/src/client/chat/MessageIconActions.tsx`
- Visual references supplied by the user:
  - Reference screenshot: assistant footer actions and turn metrics.
  - Reference screenshot: token usage detail popover.
  - Reference screenshot: elapsed-time detail popover.

## Implemented target

- Assistant turn footer contains copy, branch-in-new-chat, usage, duration, and timestamp.
- Like and dislike actions are intentionally omitted.
- Usage popover shows exact total tokens, provider/model route, cache hit rate, uncached input, cache-read input, output, and reasoning output.
- Duration popover shows total turn time and live throughput when the active turn exposes it.
- Compact values use the same `K tok` treatment as Harness while popovers retain exact values.
- Long provider/model routes remain on one line with ellipsis and expose the full route as a native title.
- Branch action uses the existing checkpoint and capability checks and creates a real conversation fork.
- Popovers dismiss with Escape or outside interaction and return focus to their trigger.

## Native verification

- App: `~/Applications/Reasonix-Canary/Reasonix.app`
- Platform: macOS arm64, Electron 44.2.0
- Data directory: isolated `Reasonix-Test-ChatRefactor` profile
- Compared the Harness reference and the running app in one visual comparison input.
- Verified a real newly completed turn displayed `用量 7.4K tok`, `用时 2秒`, and a timestamp.
- Verified its usage details displayed 7,370 total tokens, 96.7% cache hit, 238 uncached input, 7,040 cache-read input, 92 output tokens, and 25 reasoning tokens.
- Verified the duration dialog, usage dialog, copy action, branch action, keyboard dismissal, and normal transcript scrolling.
- Found and fixed a queued-turn timing bug that could reuse the preceding turn start time; the new regression test covers this event order.

## Result

PASS. The footer hierarchy, spacing, subdued metadata treatment, popover content, and interaction match the supplied Harness target within the existing Reasonix theme. No like or dislike control is present.

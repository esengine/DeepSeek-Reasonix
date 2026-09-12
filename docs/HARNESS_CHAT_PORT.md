# Harness chat presentation port

Reference: DeepSeek Harness `c291e7961a515f6d7af9304e7fd1d257929aef26`.
The source inventory and license are in
[`harness-chat/README.md`](../desktop/frontend/src/components/harness-chat/README.md).

## Delivered behavior

- One natural-flow conversation surface, with compact process rows and a
  standalone final answer. Completed processes show call/message counts and
  retain a failed-call count when recovery succeeded. Interrupted or terminally
  failed turns remain visible.
- Tools expand inline into the Harness terminal, diff, source-list or generic
  input/output presentation. An inspection action opens the existing isolated
  detail drawer. Long content loads on request; malformed results preserve a
  retryable preview instead of enabling copy of an incomplete result.
- Thinking uses the first nonempty line after completion and the latest line
  while running. Expanded thinking does not duplicate its text in the header.
- System and permission receipts are compact disclosures. Uninformative empty
  completion summaries are omitted. Markdown headings, tables and spacing use
  the adapted Harness styles; Reasonix's parsing and security remain in place.
- Copy, conversation fork, usage breakdown, duration and timestamps remain.
  Voting controls are omitted. Missing historical usage is not fabricated.
- Settings no longer expose Standard/Deep conversation presentation. New
  sessions default to Workspace write. Persisted legacy fields are retained only
  for compatibility and do not select a second chat implementation.

The port retains Reasonix theme/font settings, composer, approvals, host-safe
links, Markdown workers, history APIs and backend ownership. It does not import
Cordis or the Harness server. Tool result shapes that cannot be mapped reliably
use the generic card. This is not a claim that the entire Harness application
or every plugin-specific renderer is embedded in Reasonix.

## Verification on macOS arm64

Frontend build passed hooks lint, typecheck, CSS/theme checks, the single scroll
writer contract and unchanged bundle budgets (render-blocking CSS 3.8 KiB against
4.0 KiB). Transcript, settings, stream, app lifecycle, remote and composer suites
passed. The full-content race test also covers invalid results and retry.

Production-path fixture replay results:

| Runtime | 240 turns input P95 | 1,000 turns input P95 | Max long task at 1,000 | Switch P95 |
| --- | ---: | ---: | ---: | ---: |
| Chromium 153 | 56.3 ms | 120.7 ms | 138 ms | 32.8 ms |
| WebKit 26.6 | 46 ms | 103 ms | Unavailable | 40 ms |
| Electron 44.2.0 | 58.4 ms | 43.4 ms | 142 ms | 20.3 ms |

All three exercised 60 consecutive streaming size changes, manual upward
scrolling, prepend, process disclosure, drawer focus restoration and queue
convergence. Stream anchor drift was 0 px; prepend drift was 0.094 px. Chromium
released heap growth across 20 switches was 125,396 bytes. The weather fixture
verifies 24 px tool rows, collapsed permission bodies and no mounted tool
subtrees in a folded process. WebKit does not expose the Long Tasks API.

Raw results and screenshots: [`evidence/harness-chat-port`](evidence/harness-chat-port).
Fixtures contain synthetic weather data. Native verification additionally
opened the user's existing weather turn and the settings screen in the final
packaged app. Package identity and native smoke details are in
[`design-qa.md`](../design-qa.md).

The test build is installed at `~/Applications/Reasonix-Canary/Reasonix.app`
with the isolated `Reasonix-Test-ChatRefactor` profile. Native WebView hosts on
other platforms, exposed native scrollbar dragging, extended native IME soak
and fully expanded 1,000-turn stress were not rerun for this presentation pass.
No release was published or PR merged.
- The right-side turn rail replaces the top dropdown. Harness fixed-pitch
  marks, active state, hover/focus previews and independent overflow scrolling
  are adapted to the existing ChatScrollController. Pointer/keyboard activation
  enters reading mode; return-to-latest restores follow. Only loaded turns are
  listed and prepend extends the rail. Preview subscribes only to its user and
  answer nodes. Navigation stays available in narrow workbenches.

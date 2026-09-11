# Transcript cutover acceptance

[简体中文](TRANSCRIPT_ACCEPTANCE_9777.zh-CN.md)

The renderer slice of #9777 replaces the legacy engine atomically. It includes
mounted-block ResizeObserver ownership, generation-fenced cold measurements,
retirement of consumed native wheel travel, and one before-paint commit for
prefix geometry and anchor correction. Old queued geometry cannot overwrite
that commit. Native gestures keep authority; no second scroll writer is added.

Acceptance covers selection, question navigation, history prepend, async
content growth, process/reasoning disclosure, A-to-B-to-A replacement,
streaming tail reachability and manual-reading ownership. Windowed rendering
retains the mounted-block cap. Sustained native wheel input and its release
are checked for reverse displacement, painted overlap and final tail distance.
Existing correctness thresholds must not be relaxed to qualify the cutover.

The local production replay exercises Chromium and Playwright WebKit; the
platform workflow additionally exercises actual macOS WKWebView, Windows
WebView2 and Linux WebKitGTK. Browser emulation cannot replace these native
checks. Each child PR must pass its own current-head checks and integrate
its settings and pure-model parents before merge.

App source-bound commands and memory qualification are separate slices.
Renderer acceptance does not certify whole-App heap retention or the entire
original integration PR. Live child PR checks are authoritative for delivery
status; this document records the contract rather than a permanent green
CI claim.

Native finish is geometry-driven: after the sustained input phase, hosts send
batches of eight native events until two observations confirm the physical tail.
There is no fixed finishing-event count that assumes estimated height stayed
constant. The original GTK 45-second total watchdog, WKWebView 225-second
interaction watchdog, WebView2 60-second interaction budget, 4px displacement
and tail limits, zero blank frames, and bounded mounts remain unchanged.

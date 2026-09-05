# PR #9777 review closure and acceptance

The later App-layer implementation status is tracked in
[App lifecycle checkpoint](APP_LIFECYCLE_9777.md) and its
[Chinese version](APP_LIFECYCLE_9777.zh-CN.md). Earlier measurements below are
historical evidence, not final-head qualification.

## Reviewable slices

1. `9cc1f4ef7`: source/generation/interaction ownership, terminal navigation, cancellable clocks and deterministic async behavior tests.
2. `ff559421b`: atomic covered window geometry, total completed-block budget, shared presentation and safety, resident retirement, native selection/focus replay, and committed command authority.
3. Scope and evidence: restore independent API-key behavior to current main, document compatibility and residual qualification, and retain existing budget ratchets.

The base was rechecked at `2c65a55abbca638c58e1bd8c5a21602e455f99a0`; the original PR head was `de1295df6d911cce20d498f294d5cce1d141c13c`. It already contained this base. Existing history is preserved, with no force push, merge or release authorization.

## Behavior evidence

| Invariant | Executable evidence |
| --- | --- |
| A → B, A → B → A, newer same-turn request, old finally | `transcript-question-jump.test.ts`, `transcript-question-nav-integration.test.ts`; old data may finish, old UI/positioning cannot resume |
| Wheel/touch/thumb/selection during pending navigation | Same request-owner tests plus actual Transcript dispatch; zero accepted non-user writes |
| Queued disconnected observer and cancelled frame/timer | Kernel races, actual viewport and surface-token tests; old generation cannot advance new geometry or confirm old paint |
| 100 → 101 → 135 growth | Actual same-surface DOM test must remain windowed, total completed mounts ≤40; full fallback is not accepted |
| 1,000/10,000 completion and resident exit | Deterministic keyed native rectangles, stable completed active host, visible coverage and tail error ≤4px |
| Missing range/invalid prefix/invalid native size | Explicit unavailable commit, before-paint all-paged presentation, second observation locks full, healthy geometry cannot unlock the generation |
| Safety × tail/reader/focus/native drag selection | Chromium production paged replay; host identity preserved, focus and selection usable, drift/tail error ≤4px, no held-gesture write |
| Commands across commit/suspension | Stable identity dispatches to current committed state; suspended work does not replace authority |

New ownership tests reproduced eight failures before the interaction fence. The overscan/growth tests reproduced budget fallback before re-budgeting. Materializing a concrete prefix fixed the real lazy-TanStack-Proxy failure (sparse array methods skipped virtual entries). Browser assertions count actual `accepted` writes, not no-op terminal records; drift and mount budgets were not relaxed. New races use controlled frames, timers, observers and deferred Promises, not elapsed sleeps or source strings. Obsolete string assertions for composer/mouse wiring were removed in favor of behavior replays.

## Local verification and performance

The frontend production build, Transcript suite, Chromium/WebKit reader replay,
Chromium selection/scroll/composer replay, single-writer gate and repolint were
run during these slices. The full frontend suite is part of final-head
qualification, not inferred from a focused pass. Config tests and the desktop
Go package tests also passed locally. Platform coverage is separately defined
below; local Chromium is not a native WebView substitute.

On the current macOS performance environment, production history conversion
with archived/lazy tool bodies produced 10,000 turns / 30,000 items:

| Operation | Measured time | Ceiling |
| --- | ---: | ---: |
| Full turn-model/block projection | 77.41ms | 1,000ms |
| Immutable prefix and covered-range commit | 0.89ms | 1,000ms |
| Completed mounted range including two residents | 40 blocks | 40 blocks |

The browser safety fixture uses the production TranscriptStore paging path:
120 loaded turns out of a 1,000-turn session, not 1,000 fully expanded bodies.
Normal mode mounts 40 completed blocks; safety mounts the resident 120, retains
trusted prefix coordinates, and does not fetch the other 880. In the 24-cycle
profiled run, normal navigation/ready cost was 98–466ms; full safety paint and
retained focus took 64–126ms. Heap-snapshot instrumentation increases latency;
these are measured environment values, not portable timing guarantees. The
replay logs every sample and enforces the 1s safety-interactivity ceiling.

### Memory finding and qualification limit

The first nine-cycle profile exposed persistent Transcript render-context
retention: 1,960 additional `resolveText` closures. Binding commands outside the
render lexical scope removed that retention; a repeated snapshot comparison
kept exactly 11 such closures before and after. This is a root-level lifetime
fix, not cache eviction or a forced-GC production workaround.

**Whole-application heap stability is not established.** A subsequent 24-cycle
profile still grew from 35,333,924 to 38,372,324 collected JS bytes. Retainers
include App-shell callback contexts and compiled code; a no-fault session-switch
control also grows. Most released samples have 6,462 DOM nodes and 489 event
listeners, with transient 590-listener samples. The transcript selection graph
no longer accumulates, but these observations do not prove the entire App is
leak-free or establish the remaining growth's pre-PR baseline. The executable
replay records a 12-switch no-fault control and 24 safety cycles for comparison.
No arbitrary cross-platform byte allowance or claim of a plateau is used.

Resolving/attributing remaining App-shell retention requires separate inspection
of the session-shell callback lifecycle and a main-v2 control; this PR does not
silently broaden into workspace/Provider rewrites. Until that qualification and
the final remote checks are resolved, this report is **not a merge-ready signoff**.

## Scope and overlapping PRs

API-key saving had gained an independent await/disabled-input behavior in
`410876875` (`fix: repair model picker interactions`). That behavior is restored
to current main-v2: it is not part of session experience. The remaining Provider
fixture capability adjustment follows the merged model-capability contract;
snapshot normalization tests guard preservation of mainline fields during
authoritative settings refresh. No Provider production redesign is included.

| Related PR | Covered here | Not imported / independent capability |
| --- | --- | --- |
| [#9754](https://github.com/esengine/DeepSeek-Reasonix/pull/9754) | Stable prepend/jump ownership, native-scroll fencing, shared Markdown, content-free geometry evidence | Its Virtuoso transaction/controller strategy and approximately 16px jump alignment are not retained. Empty/whitespace fenced-code suppression already exists in shared `markdownComponents`; it remains independently valuable, not credited to the new kernel. Old platform evidence is not reused for this head. |
| [#9673](https://github.com/esengine/DeepSeek-Reasonix/pull/9673) | Long-history window bounds, reader geometry, resident tail, collapse/prepend authority | Its 1,000-row static threshold and Virtuoso branch are replaced by the 100-completed-turn rule. Natural-height pending Markdown remains a separate shared-pipeline behavior, covered by Markdown/typography tests. |

Neither related PR is closed, merged or changed by this work.

## Final remote qualification

Use the exact head and [PR check suite](https://github.com/esengine/DeepSeek-Reasonix/pull/9777/checks), not an earlier green head. Required evidence includes Linux WebKitGTK 4.0 and 4.1 native smoke, isolated macOS WKWebView, Windows Chromium replay and real WebView2 scroll/selection/Wails startup, Go tests/race/lint and CodeQL. PR Windows smoke coverage is not the full main-v2 push/release sweep. Final check-run URLs and results belong in the PR's acceptance update after those jobs finish.

No bundle, file-size, complexity, mounted-block or drift budget is raised.
Legacy fields/setters/events/localStorage mirrors remain for one complete
release as described in [Session experience](SESSION_EXPERIENCE.md); they may be
removed only after that downgrade-compatibility window ends. Rollback is a PR
revert, not a hidden old renderer feature flag.

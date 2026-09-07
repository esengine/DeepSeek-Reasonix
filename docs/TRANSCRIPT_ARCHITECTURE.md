# Transcript architecture

The desktop Transcript has one projection and one scrolling authority:

```text
TranscriptStore / ControllerLiveStore
                ↓
       TimelineProjection
                ↓
        TranscriptKernel
                ↓
  Full DOM / TanStack Window Adapter
                ↓
   TranscriptViewportWriter
                ↓
       native scroll container
```

## Projection and rendering

`TimelineProjection` is pure. One complete turn is a `TimelineBlock`, keyed from stable backend entry/user identity. History prepend, stream completion, and unrelated content patches must not rename an existing block. The active turn never enters the window size ledger.

Up to 100 completed turns use full DOM. At 101 turns the adapter windows cold completed history with `@tanstack/react-virtual`; the active turn and at least the two most recent completed turns remain ordinary DOM. A former resident turn is eligible for the cold window only after it is at least one viewport above view and contains no logical anchor, selection endpoint, or focused element. Every contiguous eligible prefix is measured and published as one ledger snapshot before React transfers it out of ordinary flow, so resident-to-cold movement preserves the native extent. TanStack supplies prefix sizes and mounted ranges only: stable `getItemKey` is mandatory, automatic size-change scroll correction is disabled, and its scroll callback performs no native write.

The Window Adapter applies a range commit protocol instead of painting every asynchronous TanStack candidate. A committed range must cover the current native viewport. Native viewport geometry is consumed as an immutable external-store snapshot, allowing React to reject a concurrent render if the compositor offset advances before commit. The mounted items, total window extent, and scroll margin form one immutable adapter snapshot: retaining an old range while publishing a new extent is forbidden because that mixes measurement generations and can move or uncover content at an unchanged native `scrollTop`. Window items are positioned with absolute layout `top`, not transforms, so the item range and native scroll position cannot be split into independently committed WebView compositor transactions. The bounded adapter budget is directional: resident turns consume the shared 40-completed-block budget first, four cold blocks remain behind current motion as a reversal cushion when capacity permits, and the remaining cold capacity is mounted ahead. A stale candidate therefore cannot replace a previously covering range; a native jump that invalidates both ranges is reconstructed synchronously from TanStack's prefix-size ledger with the same directional budget, including every protected anchor, selection, focus, and jump block. If candidate, retained, and reconstructed ranges are all uncovered—or required protected/resident ownership cannot fit the window budget—the adapter fails closed through the shared full-DOM safety renderer before paint. It never exposes a blank range while waiting for the later anomaly probe. While native input owns an unchanged viewport, measurement-only notifications retain the complete painted geometry snapshot. The adapter records whether the range came from a candidate, retention, reconstruction, or an unavailable fail-closed state, but none of these paths may write scroll position.

DOM measurement uses the same commit boundary. The adapter owns an immutable, block-keyed Reasonix measurement ledger; TanStack's item ResizeObserver path is not connected. Measurements enter a staging ledger first. In reader intent, the whole painted viewport is immutable, rather than only the first logical anchor. Both the pre-measurement prefix range at the immutable native `scrollTop` and the mounted DOM must identify a block beyond the current publication frontier before any staged size may publish; the Kernel's logical anchor may only move that boundary later. A settled reader's frontier is the viewport end. During bounded wheel input the lazy measurement ledger advances it by the accumulated absolute pixel-mode native wheel steps observed in the current Kernel lease plus one full viewport, so a WebView compositor cannot carry a size publication into view before React commits it. The accumulated lead is retained until that wheel lease ends. Non-pixel wheel input, touch, selection, keyboard jumps, native thumb drag, and nested handoff without a bounded delta remain unbounded and therefore stage every cold measurement until ownership ends. This avoids both failure modes: publishing too close to a moving compositor reflows visible content, while freezing the whole forward runway during a long bounded wheel stream leaves estimate gaps uncalibrated until they become visible. Together these rules guard every divergence direction: a stale native listener, an underestimated prefix, an earlier lazy block growing into view, sequential visible blocks whose remeasurement would otherwise shift by an increasing amount, and compositor motion outrunning a React commit. TanStack's `scrollMargin` is measured in the native scroller's coordinate space, including Transcript padding and any prefix surface. Measurements before or inside the frontier remain staged, while safe forward overscan is refined before the reader reaches it. Tail intent does not refine invisible cold history: its physical geometry comes from the exact resident tail, avoiding an unrelated prefix rebuild and extra tail write. Each publication first commits one immutable Reasonix ledger snapshot, then transfers only that safe suffix into TanStack's keyed size cache synchronously in the same browser task. Calling TanStack `measure()` is forbidden because it clears the keyed cache and rebuilds the whole prefix, which can reintroduce older off-screen measurement deltas ahead of the reader. The full-DOM adapter follows the same will-change/commit handshake. This keeps rendering, prefix sums, and native scroll ownership on one ordered state transition instead of allowing asynchronous measurements or partially updated item sizes to move visible content behind the kernel.

Development, test, preview, and canary builds may use the non-persistent `?transcriptRenderMode=full|windowed` diagnostic override. Stable builds ignore it.

## Kernel state machine

Source paging and navigation have different ownership. `TranscriptHistoryRequest`
deduplicates one request within its source generation; identity-matched cleanup
cannot release a newer request. `TranscriptNavigation` additionally captures the
Kernel interaction revision. Its pending → locating → terminal lifecycle spans
paging, mounting and the actual jump transaction; failed/retry retains that same
ownership. User takeover invalidates navigation immediately but may allow the
source data request to complete. Replacement, unmount and a newer jump invalidate
all old UI effects. The question controller loads with the question rail; paging
remains outside that lazy boundary so history and auto-fill share one owner.

Event commands bind to the latest committed presentation through
`useTranscriptCommand`. A stable callback must not retain a per-render controller
result or a chain of older sibling callbacks: those contexts can keep obsolete
selection rows alive even after all DOM and observers have been released. The
binding lives in a separate lexical scope and publishes only in a layout commit;
a suspended render does not acquire command authority.

Persistent viewport intent is either `tail` or `reader`. The logical anchor is the tail or a stable block key plus the viewport offset inside that block. Native `scrollHeight`, `scrollTop`, and `clientHeight` are the only bottom truth.

Every structural action is a generation-bound transaction. Async history paging and unloaded question navigation additionally acquire a surface token before their first `await`; replacement invalidates those workflows before they can create a transaction or mutate replacement-session UI:

- user input and selection
- question jump
- display change, prepend, restore, and composer resize
- tail follow

That order is also the preemption order. Every transaction terminates as committed, cancelled, or expired; the default deadline is 1000 ms. A session or surface replacement increments `generation`, so old animation frames, timers, measurements, and commands are rejected. Structural writes use `behavior: auto`, with at most one correction per geometry revision and one recomputation from the latest anchor.

`TranscriptKernel` receives an injectable clock. Correctness tests use fake animation frames and timers; real sleeps are not a correctness mechanism.

## Single writer and gestures

`TranscriptViewportWriter` is the only production module that may assign the native Transcript `scrollTop`. Question navigation, history prepend, Markdown block-window compensation, selection edge scrolling, the Creation scrollbar, and nested-scroll handoff all route through the kernel and writer. A request that has already landed commits with a `no-op` terminal write outcome and performs no DOM assignment. The static `check:scroll-writer` gate rejects bypasses, while runtime diagnostics record only session identity, generation, transaction, owner, intent, geometry revision, numeric offsets, and terminal outcome—never message content.

Viewport actions that can be activated during a geometry commit keep stable DOM identity. Their visibility changes on the mounted host instead of conditionally unmounting it, so a pointer or native automation target acquired before a React commit cannot become a detached no-op. The action still delegates every physical scroll to the Kernel and single writer.

Wheel, touch, scrolling keys, pointer selection, and native scrollbar drag immediately take reader ownership and cancel lower-priority work. Native thumb drag freezes program writes but never browser scrolling. The native gesture lease and post-gesture paint callbacks use the Kernel's injectable clock and are invalidated on surface-generation replacement. A physical writer offset remains pending until its matching native `scroll` event is consumed or a different offset proves real user movement, even when gesture ownership has already begun. Only native-owned scroll events update the gesture's logical anchor, and top-edge pagination additionally requires upward movement; measurement-only layout changes, delayed writer events, movement away from the history boundary, and gesture completion cannot invent a new reader position or history request. When native ownership ends, deferred structural work may resume from that observed anchor. Reduced motion affects decorative animation only.

## Geometry and safe mode

`commitTranscriptWindowGeometry` commits range, complete prefix, margin and extent
together. It concretely materializes TanStack's lazy measurement Proxy: spreading
the array reference or calling a sparse-array method is not a snapshot. Candidate
and retained ranges are re-budgeted against current residents and protection;
optional overscan cannot alone cause safety fallback. The commit returns explicit
coverage, which feeds the same coalesced Kernel geometry entry as full DOM.

`TranscriptProjectionView` supplies one keyed host and observer lifecycle for all
three presentations. An unavailable range immediately mounts every currently
paged completed block before paint. Safety retains trusted cold prefix coordinates
and disables eviction/measurement publication instead of reflowing estimates into
natural flow. Thus selection/focus hosts and reader coordinates survive even while
native input prohibits writes. Ordinary short full DOM remains natural flow. Two
fault observations lock this all-mounted presentation until generation replacement;
healthy geometry alone cannot flip it back. This trades extra mounted DOM for
continuity; it does not eagerly fetch unloaded pages or lazy tool/Markdown bodies.

The Kernel clock owns geometry coalescing, observer notifications, surface-ready
callbacks and auto-fill. Observer registration and queued callbacks both capture
generation; cancellation also guards callbacks delivered after disconnect. Health
validation and one structural correction share the geometry frame. One subsequent
clock observation can confirm a fault, not an open-ended scroll retry loop.

Streaming active-block ResizeObserver reports are coalesced by the kernel to at most one tail write per animation frame. Reader intent receives no tail write. Prepend and display changes restore the same logical block offset after the new projection is measured. Composer resize preserves the reader's native top and performs one tail correction only when tail owns the viewport.

Two consecutive blank-viewport, invalid-geometry, or unrecoverable-anchor events without an intervening healthy frame in one generation switch that session to full DOM until the next surface generation. Safe mode mounts only the pages currently resident in `TranscriptStore`; unloaded history and large Markdown bodies remain lazy. It reuses the same projection, components, selection model, and writer—there is no legacy renderer fallback.

## Required verification

Changes to this path must keep deterministic Kernel sequences, 100/101 rendering boundaries, active/resident ownership, stable prepend identity, stale-generation zero-write behavior, Markdown parity, selection retention, and browser/native platform replays green. Production must contain one native Transcript write point and no alternate scrolling controller.

See [review closure and acceptance evidence](TRANSCRIPT_ACCEPTANCE_9777.md) for
the measured paged safety costs, remaining qualification limits, related PR
boundaries, and the final-head CI requirement. This architecture does not assert
that every frontend issue since 1.23.0 has been eliminated.

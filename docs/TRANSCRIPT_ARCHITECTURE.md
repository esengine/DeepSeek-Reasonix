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

DOM measurement distinguishes first materialization from later changes. A new native block host publishes its actual size with the complete prefix before its first paint, even during input. It cannot paint an estimated allocation that overlaps the next natural block and leave that discrepancy for input release. Geometry is acknowledged only after all measurements needed by that materialization have committed. The generation-bound host set also recognizes cache-backed remounts and safety mounts; a replacement surface starts a new set.

When new blocks refine the prefix above an already-visible block during native input, the Window Adapter retains one coordinate origin for the entire window. DOM positions and extent include that origin; range lookup subtracts it from native scroll position, and publication-frontier checks use translated positions. Common visible blocks retain their positions while new adjacent blocks use real sizes. No native scroll write occurs. The origin is consumed continuously as native travel approaches the leading edge, so it cannot hide the first block or disappear abruptly at zero. When input ends, the previously committed prefix supplies the coordinate-conversion anchor; clearing the origin and the Kernel correction form one prepaint commit. This is a coordinate mapping, not a second input lease or a queue of per-row size debts.

Subsequent measurements enter the immutable, block-keyed staging ledger. TanStack's automatic measurement publication and native scroll correction remain disconnected. During input, both translated painted geometry and fresh DOM geometry must identify a suffix beyond the viewport plus one viewport of runway before a later size can publish. The Kernel anchor may only move that frontier later. Wheel deltas are never accumulated into a publication barrier. After input ends, later sizes publish under the input-captured Kernel anchor in the same prepaint transaction. A preceding block that grows into the viewport cannot replace that anchor through its newly changed DOM bounds. Actual content growth or explicit disclosure can reposition following blocks; their old tops must not be frozen into overlaps. Mounted absolute blocks have generation-fenced ResizeObservers scheduled by the Kernel clock.

Window materialization preloads its history presentation. Complete answers up to 8,000 source characters and 24 Markdown blocks format synchronously, so their first measured DOM is already formatted. Larger sources retain the worker and bounded block window. Ready output is cached separately from displayed output; a complete answer fitting the block window waits only for active input to end, not for a stationary reader to return to the bottom. Its layout effect asks the Window to measure before paint. Long block-window replacements retain their existing visible-source protection and can commit after leaving the viewport or returning to the tail. Full-DOM rendering keeps its existing worker path. Formatting a genuinely different content layout is not claimed to preserve every following block's old position.

The ledger owns sizes only, and the Kernel owns all input leases and writes. First materialization establishes real mounted geometry in both reader and tail intent; later invisible cold-history changes do not refine the tail's prefix. Every approved batch first commits one immutable Reasonix snapshot, then transfers that exact batch into TanStack's keyed size cache synchronously. Calling TanStack `measure()` remains forbidden because it discards that cache and rebuilds the protected prefix. The full-DOM adapter continues to share the will-change/commit handshake and the same native writer.

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

An approved measurement batch also owns its next geometry commit. A layout-effect state update completes that commit before paint rather than relying on TanStack notification scheduling. The commit installs the complete published prefix and either its covering candidate or a range reconstructed from that same prefix. Retaining the older prefix would defer already-approved offscreen growth until native scrolling brings it into view. Unsolicited stale range notifications still retain the last covering snapshot.

Input ownership also gates intent changes: an unowned scroll event may be a layout clamp or a delayed writer notification, so it cannot change the logical reading anchor or cancel tail follow. Structural transactions use the existing logical anchor. Touch momentum and native thumb release retain the same renewable native-input lease; a jump-bottom command explicitly ends the older lease. Lease renewal does not synthesize a scroll observation, and no-op writes do not erase pending writer provenance.

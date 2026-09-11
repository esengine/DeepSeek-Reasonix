# Desktop agent notes

## Transcript scroll discipline

The transcript (`frontend/src/components/Transcript.tsx`) is governed by
`TimelineProjection` and the generation-aware `TranscriptKernel`. Keep these
contracts when touching anything that can move the transcript viewport.

- **Stable identity**: one complete turn is the projection, anchor, and
  virtualization unit. Block keys come from backend entry/user identity, never
  array position. Prepend and content patches must not rename mounted blocks.
- **Stable viewport actions**: controls that can be activated while range or
  geometry state changes keep the same DOM identity. Visibility is state on a
  mounted action host; do not conditionally remount it across viewport commits.
- **Generation fence**: session/surface replacement increments the kernel
  generation. Every delayed measurement, timer, animation-frame callback, and
  write request carries that generation; stale work performs zero writes.
  Async paging owns a source-session request identity; question navigation owns
  generation plus interaction revision from request through positioned terminal
  state. Native takeover cancels navigation, not a valid source data load. Old
  completion/finally callbacks may release only their identical request.
- **Single writer**: only `frontend/src/lib/transcriptViewportWriter.ts` may
  mutate the transcript's native scroll position. Full-DOM, TanStack window,
  Markdown, selection, question navigation, prepend, composer resize, and tail
  follow all submit transactions to `TranscriptKernel`. The static gate in
  `frontend/scripts/check-single-scroll-writer.mjs` must reject any bypass.
- **Scroll provenance**: a physical writer offset remains pending until its
  matching native `scroll` event is consumed or a different offset proves
  input-owned user movement. No-op writes and input-lease renewal must not
  consume pending writer provenance. Layout scrolls without an input owner
  preserve logical intent, as do all structural geometry transactions. Touch
  momentum and native thumb release retain the existing quiet-period lease
  until their final native progress. Starting a gesture must not relabel a delayed writer event as
  native input, and top-edge pagination reacts only to native-owned upward
  movement, never a writer event or a reader moving away from the boundary.
- **Explicit terminal state**: every transaction ends committed, cancelled, or
  expired. User input and selection preempt lower-priority work; question jumps
  outrank display/prepend/restore/resize, which outrank tail follow.
- **Native geometry is authoritative**: bottom means
  `scrollHeight - scrollTop - clientHeight <= 4`. TanStack computes prefix
  sizes and mounted ranges only; its measurement compensation is disabled and
  its scroll callback must never bypass the writer.
- **Covered-range commit**: the Window Adapter may paint a TanStack candidate
  only when it covers the current native viewport. Retain the last covering
  geometry snapshot when a candidate is stale: mounted items, complete prefix, total extent,
  and scroll margin are one atomic value and must never come from different
  measurement generations. If a native jump invalidates both, rebuild once
  from the prefix-size ledger while preserving every protected block.
  Spend the bounded mount budget directionally: keep a reverse cushion, then
  allocate the remaining runway ahead of current native motion so asynchronous
  WebView scrolling cannot outrun the mounted range. Never exceed the completed
  block mount cap to hide coverage races. The cap belongs to the whole adapter,
  not only cold history: resident and protected blocks consume the same budget.
  Retire measured resident prefixes and trim optional overscan before declaring
  budget failure. Materialize third-party lazy cache views with `Array.from`;
  a Proxy over mutable typed-array sizes is not an immutable prefix snapshot.
  If no candidate, retained snapshot, or ledger reconstruction covers the
  viewport, fail closed through the shared full-DOM safety renderer before
  paint; never commit an uncovered range and detect the blank afterward.
  Unsolicited measurement notifications cannot replace the painted range while
  native input owns an unchanged viewport. An adapter-approved measurement batch
  must instead commit its complete prefix and covering range before paint; it
  cannot retain an older prefix and expose that growth during later native travel. Native viewport geometry is an
  external store: range renders must use its immutable snapshot so React
  cannot commit a range calculated before a newer compositor scroll offset.
  Window items use absolute layout `top`, not transforms that can put range
  position and native scroll state into independently committed compositor
  transactions.
- **Anchor-safe measurement commit**: DOM measurements enter a block-keyed
  staging ledger before they can change TanStack's prefix sizes. First
  materialization is distinct: each new native host must publish its real size
  and complete prefix before its first paint, not wait for gesture release.
  One generation-bound window origin can preserve common visible positions
  while new preceding blocks are measured. Translate positions, extent, range
  lookup and publication-frontier checks together; consume the origin
  continuously toward the native leading edge, never reset it abruptly at zero.
  Clear it using the committed prefix anchor in one prepaint commit after
  input ends; ordinary growth retains the input-captured Kernel anchor. Acknowledge geometry only after materialization finishes. For
  subsequent size changes, while native input owns reader intent, the entire painted viewport is immutable: both the pre-measurement
  prefix range and mounted DOM must place a block after the viewport before it
  becomes a publish boundary. The logical Kernel anchor may only move that
  boundary later. This prevents stale listeners, underestimated ranges, or
  lazy blocks from reflowing any content the reader can see. TanStack's
  `scrollMargin` is measured in the native scroller's coordinate space,
  including Transcript padding and any prefix. Earlier and visible sizes remain
  staged during native ownership; only post-viewport overscan may publish later
  changes. Newly materialized natural sizes follow the prepaint rule above.
  After ownership ends, publish staged DOM sizes under a Kernel logical-anchor
  restore transaction. Prefix layout and anchor correction must complete in
  one before-paint commit, cancelling any queued older geometry work.
  Preserve the first reading anchor, while allowing later
  blocks to move with actual content growth; freezing every old top would
  overlap expanded content. Observe mounted absolute blocks as well as the
  projection root, since local folds do not change the root extent. Tail intent
  does not refine subsequent invisible cold-history changes; first materialization
  still establishes real mounted sizes. The measurement ledger owns sizes only.
  Input leases belong to the Kernel and must not be duplicated in the window
  adapter. Re-read physical viewport geometry at measurement admission; both
  painted prefix and measured DOM must place the publication boundary beyond
  the viewport plus one viewport of runway. This reserve is not a bound on
  compositor travel. Never integrate wheel intent into a pending-distance
  barrier: a transient native backlog can exceed the mounted window and freeze
  every future measurement until release. Apply the same geometry boundary to
  wheel, touch, selection, keyboard and native-thumb ownership; the Kernel
  still rejects programmatic reader writes throughout those leases.
  Publish one immutable Reasonix snapshot, then transfer that exact published
  batch into TanStack's keyed size cache in the same browser task. Close the
  batch with a layout-effect state update and acknowledge that publication in
  the geometry commit; TanStack notification scheduling alone is insufficient. Never call
  TanStack `measure()` for a measurement publish: it clears the keyed cache and
  rebuilds the protected prefix. Never base correctness on an idle timeout,
  enable TanStack-owned ResizeObserver publication, or add platform-specific
  scroll compensation.
- **Resident active tail**: the active turn and at least the two newest
  completed turns stay in ordinary DOM. A resident block may enter windowed
  history only after it is a viewport away and owns no anchor, focus, or
  selection endpoint. Measure every contiguous leaving prefix into one ledger
  snapshot before changing the resident boundary; estimated-size migration is
  forbidden. Stream growth in reader intent performs zero writes.
- **Bounded safe mode**: two blank/invalid/correction anomalies without an
  intervening healthy frame in one generation switch that session to full DOM
  until the next surface generation. Do not add a second rendering stack or a
  persistent user flag. Normal full, windowed, and safety use one presentation
  and observer lifecycle. Immediate safety mounts all currently paged blocks
  with the last trusted cold prefix coordinates, retaining native keyed hosts,
  focus, selection, and reader offset without a structural write. It stops
  window eviction; it does not force offscreen history or large bodies to load.
- **One geometry scheduler**: full/windowed layout, ResizeObserver, surface
  paint, and auto-fill use cancellable Kernel frames. Observers bind generation
  at registration and reject queued notifications even after disconnection.
  Coalesce health validation and correction at one geometry entry; only one
  clock-owned re-observation may precede the second-fault safety latch.
- **Systemic fixes**: repair ownership, projection, measurement, or presentation
  invariants in their common layer. Do not accumulate scenario-specific retry
  branches, platform compensation, or progressively relaxed acceptance gates.
- **Deterministic clocks**: new scroll logic must go through the same
  injectable clock used by `TranscriptKernel` (`requestAnimationFrame`,
  `Date.now`, timer functions). No real sleeps or hidden retry clocks.
- **No redundant physical writes**: a transaction whose requested offset has
  already landed may commit as a no-op, but must not assign `scrollTop` again.
- **Race tests are mandatory**: any scroll-behavior change ships with a
  deterministic event sequence in `frontend/src/__tests__/transcript-kernel.test.ts`
  and, when relevant, a viewport/projection case. Run `pnpm test:transcript`
  before committing transcript changes.

# Transcript scroll and history contract

[中文](TRANSCRIPT_SCROLL_CONTRACT.zh-CN.md)

## Scope

The transcript (`../desktop/frontend/src/components/Transcript.tsx`) renders
through `ChatSource` and `ChatScrollController` in natural document flow. There
is one implementation for local and remote sessions. The retired window
adapter, measurement ledger, geometry-revision loop and logical selection are
not coming back: do not reintroduce a second rendering stack, a nested virtual
vertical scroller, or a platform-specific scroll compensation.

Keep these contracts when touching anything that can move the transcript
viewport or change which history is resident.

## Identity and rendering

- **Stable node keys**: node and block keys derive from message, turn and tool
  identities, never array positions. Prepend, settlement and content patches
  must not rename a mounted node.
- **Unchanged nodes keep their object**: streaming and settlement update the
  same assistant host; an unchanged node or order snapshot retains its
  reference so React does not remount it.
- **Markdown block identity** comes from the parse: each block carries a key
  (top-level index within one parse) and a content fingerprint stamped by the
  parse that produced it. The render path keeps the previous AST object when
  both match, which is what preserves native selection and code disclosure
  across stream publications. Do not compare serialized trees on the render
  path — that cost is what the fingerprint replaced.
- **Natural flow**: Markdown, tables and loaded history use document flow.
  Parsing may be lazy and content may be fetched on demand, but the transcript
  must not create a nested virtual vertical scroller. Collapsed process/tool
  bodies are mounted on demand.
- **Business state lives in its owner**: the controller and the history stores
  own state; `ChatSource` is a reconstructable view projection. Structural
  changes batch in microtasks.

## Single writer

- Only `../desktop/frontend/src/lib/transcriptViewportWriter.ts` may mutate the
  transcript's native scroll position. `ChatScrollController` owns programmatic
  follow, reader anchoring and navigation; everything else submits to it.
  `../desktop/frontend/scripts/check-single-scroll-writer.mjs` must reject any
  bypass.
- Native input is never synthesized or prevented to keep the tail pinned. A
  small upward reader movement releases follow, including inside the bottom
  threshold.
- Prepend, resize and page replacement preserve a stable node plus a viewport
  offset.

## Bounded reading window

History is a bounded window, not an ever-growing list.

- The resident store keeps a small number of adjacent pages per session
  (`windowMaxPages`, default 3, over 32-message pages) **including the active
  session**. Paging past that budget reclaims a page from the end the reader is
  moving away from and reports the item ids the caller must drop; a caller that
  ignores them renders rows the store has already released.
- **Pins protect a session's identity and its live edge, not an unbounded
  record set.** A running or visible session still cannot be evicted, but its
  history is subject to the same page budget as any other.
- Reclaiming is not deletion. The persisted session stays authoritative and the
  reclaimed direction stays reachable through its cursor, so every message is
  still findable, searchable and exportable. Do not treat "all history is
  mounted" as a correctness property; assert reachability and bounded residency
  instead.
- Paging is bidirectional (`loadOlder` / `loadNewer`). A binding that reports no
  newer cursor keeps its forward paging rather than being asked to simulate one
  through full downloads.
- Window cursors pin a fixed snapshot. Appends keep a cursor valid; a storage
  replacement or projection rebuild answers the typed `stale_cursor`, and a
  cursor the server cannot read is that same typed answer rather than a
  transport error. A client re-anchors at most once and keeps its current page
  with a retry affordance after a second failure.
- Anchor jumps (search hit, turn navigation) resolve through the history index
  and request the page around the target. They never walk pages from the newest
  position.

## Routing

- History reads route by the tab's **binding identity**, resolved before the
  request from the tab metadata the controller already loads. A failed local
  call must never be answered by a remote service holding a different session.
- Chat requires negotiated `transcript-v2` on Desktop and Serve. An older
  service receives an upgrade error, without legacy chat fallback. Permission,
  corruption and network errors do not trigger a protocol downgrade.

## Generation fence

- Session or surface replacement increments the generation. Every delayed
  measurement, timer, animation-frame callback and write request carries that
  generation; stale work performs zero writes.
- Async paging owns a source-session request identity; navigation owns the
  generation plus its interaction revision from request through the positioned
  terminal state. Native takeover cancels navigation, not a valid source data
  load. An old completion or `finally` may release only its identical request.
- A response from a replaced session must not advance coverage or mutate
  another tab's state.

## Budgets

- Per-renderer history body cache 32 MiB and parsed-markdown cache 16 MiB are
  admission budgets for rebuildable data. They are not a bound on the whole
  Electron process or on model-execution memory.
- String sizes are counted as resident representation; media is counted by
  decoded size. Network bytes are not heap bytes.
- Reclaiming a page withdraws the body requests, parse tasks, DOM and object
  URLs that belong to it.
- Text length and element counts that exceed a preview budget degrade to a
  bounded preview with an explicit detail path. Do not silently truncate a
  copy or export: an explicit full-content action or a streamed file export
  carries the whole value.

## Deterministic behaviour

- Scroll logic goes through the same injectable clock the controller uses
  (`requestAnimationFrame`, `Date.now`, timer functions). No real sleeps and no
  hidden retry clocks.
- A transaction whose requested offset has already landed may commit as a
  no-op, but must not assign `scrollTop` again.
- **Race tests are mandatory**: any scroll or paging behaviour change ships
  with a deterministic event sequence in
  `../desktop/frontend/src/__tests__/`, and `pnpm test:transcript` runs before
  committing transcript changes.

# Desktop agent notes

Desktop Go is a separate module; root Go tests do not cover it.

For changes affecting transcript viewport, scrolling, virtualization,
measurement, or delayed geometry work, read the
[transcript scroll contract](../docs/TRANSCRIPT_SCROLL_CONTRACT.md)
([中文](../docs/TRANSCRIPT_SCROLL_CONTRACT.zh-CN.md)).
It preserves single-writer ownership, generation isolation, reader intent,
bounded rendering, and deterministic regression requirements.

Other Desktop work does not require the scroll-specific procedure.

## Natural-flow chat

The transcript uses ChatSource and ChatScrollController. It has one natural-flow
implementation for local and remote sessions. Do not restore the retired
window adapter, measurement ledger, geometry revision loop or logical selection.

- Stable node keys derive from message/call identities, never array positions.
  Streaming and settlement update the same assistant host; unchanged node and
  order snapshots retain their references.
- Business state remains in the controller/history owners. ChatSource is a
  reconstructable view projection. Structural changes batch in microtasks;
  existing controller frame batching owns stream publication.
- Only frontend/src/lib/transcriptViewportWriter.ts writes the chat viewport.
  ChatScrollController owns programmatic follow, reader anchoring and navigation.
  Native input is never synthesized or prevented to keep the tail pinned.
- A small upward reader movement releases follow even inside the 24px bottom
  threshold. Prepend and resize preserve a stable node plus viewport offset.
  Old observers, requests and callbacks cannot act on a replaced session.
- Markdown, tables and loaded history use document flow. Parsing may be lazy,
  but must not remove loaded text or create a nested virtual vertical scroller.
  Collapsed process/tool bodies are mounted on demand.
- No geometry snapshots are stored in React state. Layout observers must
  converge without a render/measurement feedback loop.
- Native selection is browser-owned. No cross-window selection overlay or
  clipboard interception belongs to the chat.
- Keep draft input, approvals, questions, model controls and the session bridge
  outside the presentation refactor. Do not change persisted/provider bytes.
- Run pnpm test:transcript and the applicable browser suite. The primary cases
  are small reader gestures, stream growth, prepend, disclosure, session change,
  stale callbacks and unchanged-node render isolation. Do not weaken performance
  gates to hide regressions.

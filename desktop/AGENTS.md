# Desktop agent notes

## Transcript scroll discipline

The transcript (`frontend/src/components/Transcript.tsx`, react-virtuoso) has
one structural rule set, earned across #8657/#8688 and the follow-up refactors.
Keep to it when touching anything that can move the transcript viewport.

- **Single writer**: only the scroll arbiter — `frontend/src/lib/useTranscriptScrollArbiter.ts`
  and its extracted controllers (`transcriptTailSettle.ts`,
  `transcriptAnchorCompensation.ts`) — may call
  `virtuosoRef.current.scrollTo/scrollBy/scrollToIndex`, and raw
  `scroller.scrollTop` assignments on transcript surfaces are equally
  off-limits (route through the arbiter's `SCROLL_TO_OFFSET` channel, e.g.
  owner `"anchor-compensation"` / `"block-window-prepend"`). Everything else
  (jumps, tail-follow, selection edge scrolls, layout recovery) submits
  requests to the arbiter. `frontend/scripts/check-single-scroll-writer.mjs`
  enforces it statically; `lib/transcriptScrollProbe.ts` observes it at
  runtime. Never add a second writer — extend the arbiter's reducer
  (`lib/transcriptScrollArbiter.ts`) with an explicit transition instead.
- **Preemption is explicit**: user intent (wheel/touch/key/pointer), selection,
  and programmatic writers preempt an in-flight recovery through reducer
  events that end it in a terminal state (done / cancelled / expired), each
  reported to `noteTranscriptRecoveryTerminal`. No silent exits.
- **Native geometry is authoritative**: Virtuoso's `atBottomStateChange` is a
  delivery signal, not the bottom truth. Derive bottom ownership from the live
  scroller's `scrollHeight - scrollTop - clientHeight`. Browser clamps may not
  leave manual reading; only a delivered scroll with explicit reader intent
  may re-enter tail-follow. Tail-follow persists across later measurements and
  layout growth until explicit user intent releases it.
- **No keyed remounts on content patches**: patches flow through `data` only;
  Virtuoso re-measures mounted rows itself. Remounts happen only on surface
  switches and blank-watchdog rebuilds, and restore from the measured-size
  cache (`lib/transcriptMeasuredSizes.ts`) plus a state snapshot
  (`lib/transcriptStateSnapshot.ts`) instead of static estimates.
- **Deterministic clocks**: new scroll logic must go through the same
  injectable patterns as the existing code (global `requestAnimationFrame`,
  `Date.now`, `window.setTimeout`) so the fake-clock harness can drive it —
  no `performance.now`-only budgets or ad-hoc timers.
- **Race tests are mandatory**: any scroll-behavior change ships with a
  deterministic case in `frontend/src/__tests__/transcript-recovery-race.test.tsx`
  (JSDOM + fake rAF/clock harness, stubbed `VirtuosoHandle`). Run
  `pnpm test:transcript` before committing transcript changes.

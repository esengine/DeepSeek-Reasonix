import { useEffect, useMemo } from "react";
import type { WireEvent } from "../port/wire";
import { usePageVisible } from "./shown";

export const BACKGROUND_FLUSH_MS = 1000;

const DELTAS = new Set<WireEvent["kind"]>(["reasoning", "text"]);

export interface Pacer {
  push: (ev: WireEvent) => void;
  show: (shown: boolean) => void;
  drop: () => void;
}

// A pane nobody is looking at still has to learn every event, but not at the
// wire's rate: each delta re-renders the whole pane. Only a delta continuing
// the same stream is held, so the first token of each stream and every other
// kind of event land at once, in order, and stamp the moment they really came.
export function createPacer(deliver: (ev: WireEvent) => void, flushMs = BACKGROUND_FLUSH_MS): Pacer {
  let held: WireEvent[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;
  let shown = true;
  let last: WireEvent["kind"] | "" = "";

  const flush = () => {
    clearTimeout(timer);
    timer = undefined;
    const batch = held;
    held = [];
    batch.forEach(deliver);
  };

  return {
    push(ev) {
      const continues = DELTAS.has(ev.kind) && ev.kind === last;
      last = ev.kind;
      if (!shown && continues) {
        held.push(ev);
        timer ??= setTimeout(flush, flushMs);
        return;
      }
      flush();
      deliver(ev);
    },
    show(next) {
      shown = next;
      if (next) flush();
    },
    drop() {
      clearTimeout(timer);
      timer = undefined;
      held = [];
      last = "";
    },
  };
}

type Sink = (ev: WireEvent) => void;

export function useBackgroundDeltas(visible: boolean, state: Sink, trajectory: Sink) {
  const pageVisible = usePageVisible();
  const shown = visible && pageVisible;
  const pacer = useMemo(
    () => createPacer((ev) => {
      state(ev);
      trajectory(ev);
    }),
    [state, trajectory],
  );
  useEffect(() => pacer.show(shown), [pacer, shown]);
  useEffect(() => pacer.drop, [pacer]);
  return { pacer, shown };
}

import { registerTerminalOutputSink } from "./terminalEvents";

export type TerminalSinkSubscription = {
  setActive: (active: boolean) => void;
  dispose: () => void;
};

export function registerTerminalSink(
  id: string,
  sink: (data: Uint8Array) => void,
  initiallyActive = true,
): TerminalSinkSubscription {
  let active = initiallyActive;
  let disposed = false;
  let cursor = 0;
  const [unregister, history] = registerTerminalOutputSink(id, (bytes, sequence) => {
    if (!active || disposed) return;
    if (cursor === sequence) {
      sink(bytes);
      cursor = sequence + 1;
      return;
    }
    flush();
  });
  const initialHistory = history();
  cursor = initialHistory[1] - initialHistory[0].length;
  const flush = () => {
    if (!active || disposed) return;
    const [chunks, nextSequence] = history();
    const firstSequence = nextSequence - chunks.length;
    const first = Math.max(cursor, firstSequence);
    for (let sequence = first; sequence < nextSequence; sequence += 1) {
      const bytes = chunks[sequence - firstSequence];
      if (bytes) sink(bytes);
    }
    // Output larger than the retained limit is intentionally skipped; advance
    // the cursor so the next live chunk can continue without replaying old data.
    cursor = nextSequence;
  };
  flush();
  return {
    setActive(nextActive) {
      if (disposed || active === nextActive) return;
      active = nextActive;
      if (active) flush();
    },
    dispose() {
      disposed = true;
      unregister();
    },
  };
}

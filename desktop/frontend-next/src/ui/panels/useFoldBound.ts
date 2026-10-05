import { useCallback, useEffect, useState } from "react";
import type { AgentPort, CompactionSettings, ContextBreakdown } from "../../port/port";
import { foldIntent, foldModeOf, foldStep } from "../foldbound";
import { reason } from "../../i18n/kernel";

/** The fold bound as something the mark on the capacity bar can move.
 *
 *  A hook rather than a component because the handle is not a control placed
 *  near the bar — it is the bar's own mark. Owning the track would mean
 *  rebuilding it whenever the editor opens, and a bar that is torn down and
 *  remounted animates its width from zero every time, which reads as the
 *  session's usage jumping.
 *
 *  What it must never become is a percentage. This bound is an absolute input
 *  size and that is its whole point: the same 16% is 160k against one window
 *  and 20k against another, so a track measured in shares would rewrite the
 *  setting every time the model changed. The track spans the window because
 *  that is the only span worth drawing; the number stays tokens. */
export function useFoldBound(port: AgentPort | undefined, window: number, onCtx?: (next: ContextBreakdown) => void) {
  const [box, setBox] = useState<CompactionSettings | null>(null);
  const [draft, setDraft] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!port) return;
    let alive = true;
    port
      .compaction()
      .then((s) => alive && setBox(s))
      .catch((e) => alive && setError(reason(e)));
    return () => {
      alive = false;
    };
  }, [port]);

  const capacity = box ? Math.round(window * box.ratio) || window : 0;
  const step = foldStep(window);
  // Where a dragged position lands as an intent: the far end retires the bound
  // rather than setting a very large one, and the default's own position is a
  // detent, because "the default" and "a number equal to today's default" stop
  // meaning the same thing the moment the default moves.
  const intent = useCallback(
    (value: number) => (box ? foldIntent(value, capacity) : value),
    [box, capacity],
  );

  const commit = useCallback(
    async (value: number) => {
      setDraft(null);
      if (!box || !port || busy) return;
      const stored = intent(value);
      if (stored === box.soft_limit_tokens) return;
      setBusy(true);
      setError("");
      try {
        setBox(await port.saveCompaction(stored));
        // The write rebuilds the runtime, so the gauge handed back is the one
        // the session folds against — never this reply's arithmetic.
        onCtx?.(await port.context());
      } catch (e) {
        setError(reason(e));
      } finally {
        setBusy(false);
      }
    },
    [box, busy, intent, onCtx, port],
  );

  return {
    ready: box !== null,
    mode: box ? foldModeOf(box.soft_limit_tokens) : "capacity",
    stored: box?.soft_limit_tokens ?? 0,
    capacity,
    step,
    busy,
    error,
    // The raw position, which only the input itself uses: a controlled range
    // clamped mid-drag pulls its own thumb back under the pointer and the
    // handle stops following the finger.
    preview: draft,
    // What that position means, which is what the panel shows. Past capacity
    // the session still folds at capacity, so a reading of 1M would name a
    // point nothing folds at.
    reading: draft === null ? null : Math.min(draft, capacity),
    // Whether letting go here writes the default back rather than a number
    // that merely equals it today.
    move: setDraft,
    commit,
  };
}

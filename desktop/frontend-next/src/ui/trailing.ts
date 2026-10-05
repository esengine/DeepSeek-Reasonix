// trailing.ts — one write in flight, and only the newest value survives the wait.

/** trailing wraps a save so the caller can write as fast as a pointer moves. A
 *  drag is a stream of values, not of decisions: a write per tick rewrites the
 *  whole config twenty times, and two answers landing out of order put the thumb
 *  back two frames. The first goes out at once, the rest collapses into one
 *  follow-up, and it paces on the round trip rather than on a timer constant. */
export function trailing<T>(
  save: (v: T) => Promise<T>,
  adopt: (v: T) => void,
  failed: (e: unknown) => void,
): (v: T) => void {
  // Boxed, so a T that is itself null stays apart from "nothing waiting".
  let pending: { v: T } | null = null;
  let flying = false;

  const flush = () => {
    if (flying || !pending) return;
    const { v } = pending;
    pending = null;
    flying = true;
    save(v)
      .then((got) => {
        // The answer wins because the kernel clamps what was sent — but only
        // while it is still about the current value; a stale one is the snap-back.
        if (!pending) adopt(got);
      })
      .catch(failed)
      .finally(() => {
        flying = false;
        flush();
      });
  };

  return (v: T) => {
    pending = { v };
    flush();
  };
}

import { useEffect } from "react";

const STATUS_POLL_MS = 250;

// The next read is scheduled when the previous one settles, never on a clock: a
// kernel slower than the cadence would otherwise be asked again before it had
// answered, and the queue of unanswered reads is what starves the window's other
// requests of connections.
export function useStatusPoll(on: boolean, read: () => Promise<unknown>) {
  useEffect(() => {
    if (!on) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = () => {
      const next = () => {
        if (!stopped) timer = setTimeout(tick, STATUS_POLL_MS);
      };
      void read().then(next, next);
    };
    tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [on, read]);
}

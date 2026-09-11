import { act } from "react";

export function installTranscriptRaceClock(targetWindow: Window) {
  let clockNow = 10_000;
  let nextTimer = 1;
  const timers = new Map<number, { dueAt: number; run: () => void }>();
  const originalDateNow = Date.now;
  const originalSetTimeout = targetWindow.setTimeout;
  const originalClearTimeout = targetWindow.clearTimeout;
  Date.now = () => clockNow;
  targetWindow.setTimeout = ((handler: TimerHandler, timeout = 0, ...args: unknown[]) => {
    const id = nextTimer++;
    const run = typeof handler === "function"
      ? () => handler(...args)
      : () => { throw new Error("string timer handlers are unsupported in this test"); };
    timers.set(id, { dueAt: clockNow + Math.max(0, timeout), run });
    return id;
  }) as typeof targetWindow.setTimeout;
  targetWindow.clearTimeout = ((id: number | undefined) => {
    if (id !== undefined) timers.delete(id);
  }) as typeof targetWindow.clearTimeout;

  const advanceClock = async (milliseconds: number) => {
    await act(async () => {
      const target = clockNow + milliseconds;
      while (true) {
        const next = [...timers.entries()]
          .filter(([, timer]) => timer.dueAt <= target)
          .sort(([leftID, left], [rightID, right]) => left.dueAt - right.dueAt || leftID - rightID)[0];
        if (!next) break;
        const [id, timer] = next;
        timers.delete(id);
        clockNow = timer.dueAt;
        timer.run();
      }
      clockNow = target;
    });
  };
  const restore = () => {
    Date.now = originalDateNow;
    targetWindow.setTimeout = originalSetTimeout;
    targetWindow.clearTimeout = originalClearTimeout;
  };
  return { advanceClock, restore };
}

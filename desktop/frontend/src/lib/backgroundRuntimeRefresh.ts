import type { BackgroundRuntimeView } from "./types";

export const BACKGROUND_RUNTIME_ACTIVE_REFRESH_MS = 5_000;
export const BACKGROUND_RUNTIME_ERROR_RETRY_BASE_MS = 5_000;
export const BACKGROUND_RUNTIME_ERROR_RETRY_MAX_MS = 60_000;

export function sameBackgroundRuntimeLists(
  current: readonly BackgroundRuntimeView[],
  next: readonly BackgroundRuntimeView[],
): boolean {
  if (current === next) return true;
  if (current.length !== next.length) return false;
  for (let index = 0; index < current.length; index += 1) {
    if (JSON.stringify(current[index]) !== JSON.stringify(next[index])) return false;
  }
  return true;
}

type RefreshOptions = { afterMutation?: boolean };

export function createBackgroundRuntimeRefreshCoordinator(
  load: () => Promise<BackgroundRuntimeView[]>,
  apply: (runtimes: BackgroundRuntimeView[]) => void,
  schedule: (callback: () => void, delayMs: number) => number = window.setTimeout,
  clear: (timer: number) => void = window.clearTimeout,
) {
  let disposed = false;
  let inFlight: Promise<BackgroundRuntimeView[]> | null = null;
  let trailing = false;
  let timer: number | null = null;
  let failureStreak = 0;

  const clearTimer = () => {
    if (timer === null) return;
    clear(timer);
    timer = null;
  };

  const scheduleNext = (active: boolean, retry = false) => {
    clearTimer();
    if (disposed || (!active && !retry)) return;
    const delay = retry
      ? Math.min(BACKGROUND_RUNTIME_ERROR_RETRY_MAX_MS, BACKGROUND_RUNTIME_ERROR_RETRY_BASE_MS * 2 ** Math.min(failureStreak - 1, 3))
      : BACKGROUND_RUNTIME_ACTIVE_REFRESH_MS;
    timer = schedule(() => {
      timer = null;
      void refresh().catch(() => undefined);
    }, delay);
  };

  const start = (): Promise<BackgroundRuntimeView[]> => {
    const request = Promise.resolve().then(load);
    const settled = request.then(
      (runtimes) => {
        failureStreak = 0;
        if (!disposed) apply(runtimes);
        scheduleNext(runtimes.length > 0);
        return runtimes;
      },
      (reason) => {
        failureStreak += 1;
        scheduleNext(false, true);
        throw reason;
      },
    ).finally(() => {
      if (inFlight === settled) inFlight = null;
      if (!disposed && trailing) {
        trailing = false;
        clearTimer();
        void start().catch(() => undefined);
      }
    });
    inFlight = settled;
    return settled;
  };

  const refresh = (options?: RefreshOptions): Promise<BackgroundRuntimeView[]> => {
    if (disposed) return Promise.resolve([]);
    clearTimer();
    if (inFlight) {
      if (options?.afterMutation) trailing = true;
      return inFlight;
    }
    return start();
  };

  return {
    refresh,
    dispose() {
      disposed = true;
      trailing = false;
      clearTimer();
    },
  };
}

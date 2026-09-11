export interface WorkspaceRefreshTimer {
  schedule(callback: () => void, delayMs: number): unknown;
  cancel(handle: unknown): void;
}

export interface WorkspaceRefreshScheduler {
  trigger(run: () => Promise<void> | void): void;
  cancel(): void;
}

const browserTimer: WorkspaceRefreshTimer = {
  schedule: (callback, delayMs) => window.setTimeout(callback, delayMs),
  cancel: (handle) => window.clearTimeout(handle as number),
};

/**
 * Creates a trailing quiet-window scheduler with at most one refresh in flight.
 * A trigger while a refresh is running is retained and executes once the quiet
 * window has elapsed and the prior refresh settles.
 */
export function createWorkspaceRefreshScheduler(delayMs: number, timer: WorkspaceRefreshTimer = browserTimer): WorkspaceRefreshScheduler {
  let handle: unknown = null;
  let inFlight = false;
  let trailing = false;
  let latest: (() => Promise<void> | void) | null = null;
  let cancelled = false;

  const start = () => {
    const run = latest;
    if (!run || cancelled) return;
    if (inFlight) {
      trailing = true;
      return;
    }
    inFlight = true;
    void Promise.resolve().then(run).finally(() => {
      inFlight = false;
      if (cancelled || !trailing) return;
      trailing = false;
      start();
    });
  };

  return {
    trigger(run: () => Promise<void> | void): void {
      cancelled = false;
      latest = run;
      trailing = false;
      if (handle !== null) timer.cancel(handle);
      handle = timer.schedule(() => {
        handle = null;
        start();
      }, delayMs);
    },
    cancel(): void {
      cancelled = true;
      trailing = false;
      latest = null;
      if (handle !== null) timer.cancel(handle);
      handle = null;
    },
  };
}

export interface WorkspaceTreePersistenceClock {
  requestFrame(callback: () => void): number;
  cancelFrame(handle: number): void;
  setTimer(callback: () => void, delayMs: number): ReturnType<typeof setTimeout>;
  clearTimer(handle: ReturnType<typeof setTimeout>): void;
}

export interface WorkspaceTreePersistenceScheduler {
  schedule(memoryKey: string): void;
  flush(): void;
  cancel(): void;
}

const browserClock: WorkspaceTreePersistenceClock = {
  requestFrame(callback) {
    if (typeof requestAnimationFrame === "function") return requestAnimationFrame(callback);
    return setTimeout(callback, 0) as unknown as number;
  },
  cancelFrame(handle) {
    if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(handle);
    else clearTimeout(handle);
  },
  setTimer(callback, delayMs) {
    return setTimeout(callback, delayMs);
  },
  clearTimer(handle) {
    clearTimeout(handle);
  },
};

export function createWorkspaceTreePersistenceScheduler(
  persist: (memoryKey: string) => void,
  delayMs = 200,
  clock: WorkspaceTreePersistenceClock = browserClock,
): WorkspaceTreePersistenceScheduler {
  let frameHandle: number | null = null;
  let timerHandle: ReturnType<typeof setTimeout> | null = null;
  let pendingKey = "";
  let dirty = false;

  const clearPendingHandles = () => {
    if (frameHandle != null) {
      clock.cancelFrame(frameHandle);
      frameHandle = null;
    }
    if (timerHandle != null) {
      clock.clearTimer(timerHandle);
      timerHandle = null;
    }
  };

  const write = () => {
    timerHandle = null;
    if (!dirty || !pendingKey) return;
    dirty = false;
    persist(pendingKey);
  };

  return {
    schedule(memoryKey) {
      pendingKey = memoryKey;
      dirty = true;
      if (frameHandle != null) return;
      frameHandle = clock.requestFrame(() => {
        frameHandle = null;
        if (timerHandle != null) clock.clearTimer(timerHandle);
        timerHandle = clock.setTimer(write, delayMs);
      });
    },
    flush() {
      clearPendingHandles();
      write();
    },
    cancel() {
      clearPendingHandles();
      dirty = false;
      pendingKey = "";
    },
  };
}

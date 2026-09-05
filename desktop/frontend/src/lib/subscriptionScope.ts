type ListenerSlot<Args extends unknown[]> = { listener?: (...args: Args) => void };

function bindListener<Args extends unknown[]>(slot: ListenerSlot<Args>, lifecycle: { disposed: boolean }) {
  return (...args: Args) => { if (!lifecycle.disposed) slot.listener?.(...args); };
}

/** A disposed subscription is inert even if its source already queued delivery. */
export function createSubscriptionScope(track: (delta: 1 | -1) => void = () => {}) {
  const cleanups = new Set<() => void>();
  const lifecycle = { disposed: false };
  return {
    listen<Args extends unknown[]>(register: (listener: (...args: Args) => void) => () => void,
      listener: (...args: Args) => void): void {
      if (lifecycle.disposed) return;
      const slot: ListenerSlot<Args> = { listener };
      let unsubscribe: () => void;
      try { unsubscribe = register(bindListener(slot, lifecycle)); }
      catch (error) { slot.listener = undefined; throw error; }
      track(1);
      const cleanup = () => {
        slot.listener = undefined;
        try { unsubscribe(); } finally { track(-1); }
      };
      if (lifecycle.disposed) cleanup();
      else cleanups.add(cleanup);
    },
    dispose(): void {
      if (lifecycle.disposed) return;
      lifecycle.disposed = true;
      const errors: unknown[] = [];
      for (const cleanup of cleanups) {
        try { cleanup(); } catch (error) { errors.push(error); }
      }
      cleanups.clear();
      if (errors.length) throw errors[0];
    },
    get size() { return cleanups.size; },
  };
}

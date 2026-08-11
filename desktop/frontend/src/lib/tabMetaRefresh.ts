import type { TabMeta } from "./types";

export const TAB_META_VISIBLE_FALLBACK_MS = 15_000;
export const TAB_META_HIDDEN_FALLBACK_MS = 60_000;
export const TAB_META_MAX_IN_FLIGHT = 2;

export type BoundedRefreshResult<T> = {
  value: T;
  latest: boolean;
  coalesced: boolean;
};

export type BoundedRefreshOptions = {
  /**
   * Mutation-sensitive refresh: never treat a pre-mutation in-flight request
   * as authoritative. When saturated, queue a trailing load that starts after
   * a slot frees instead of applying the joined pre-mutation snapshot.
   */
  invalidate?: boolean;
};

export function createBoundedRefreshCoordinator<T>(maxInFlight: number) {
  if (!Number.isInteger(maxInFlight) || maxInFlight < 1) {
    throw new Error("maxInFlight must be a positive integer");
  }
  let sequence = 0;
  let generation = 0;
  const inFlight: Array<{ sequence: number; generation: number; promise: Promise<T> }> = [];
  let trailing: {
    load: () => Promise<T>;
    waiters: Array<{
      resolve: (result: BoundedRefreshResult<T>) => void;
      reject: (reason?: unknown) => void;
    }>;
  } | null = null;

  const settleEntry = (entry: { sequence: number; generation: number; promise: Promise<T> }) => {
    const index = inFlight.indexOf(entry);
    if (index >= 0) inFlight.splice(index, 1);
    pumpTrailing();
  };

  const startEntry = (load: () => Promise<T>) => {
    const entry = {
      sequence: ++sequence,
      generation,
      promise: Promise.resolve().then(load),
    };
    inFlight.push(entry);
    void entry.promise.then(
      () => settleEntry(entry),
      () => settleEntry(entry),
    );
    return entry;
  };

  const resultFor = (
    entry: { sequence: number; generation: number },
    value: T,
    coalesced: boolean,
  ): BoundedRefreshResult<T> => ({
    value,
    latest: entry.sequence === sequence && entry.generation === generation,
    coalesced,
  });

  const queueTrailing = (load: () => Promise<T>): Promise<BoundedRefreshResult<T>> => {
    if (!trailing) {
      trailing = { load, waiters: [] };
    } else {
      trailing.load = load;
    }
    return new Promise<BoundedRefreshResult<T>>((resolve, reject) => {
      trailing!.waiters.push({ resolve, reject });
      pumpTrailing();
    });
  };

  function pumpTrailing() {
    if (!trailing || inFlight.length >= maxInFlight) return;
    const job = trailing;
    trailing = null;
    const entry = startEntry(job.load);
    void entry.promise.then(
      (value) => {
        const result = resultFor(entry, value, false);
        for (const waiter of job.waiters) waiter.resolve(result);
      },
      (reason) => {
        for (const waiter of job.waiters) waiter.reject(reason);
      },
    );
  }

  return {
    run(load: () => Promise<T>, options?: BoundedRefreshOptions): Promise<BoundedRefreshResult<T>> {
      if (options?.invalidate) {
        generation += 1;
        // Mutation-sensitive callers must not join a pre-mutation request.
        if (inFlight.length >= maxInFlight) {
          return queueTrailing(load);
        }
        const entry = startEntry(load);
        return entry.promise.then((value) => resultFor(entry, value, false));
      }

      let entry = inFlight.length >= maxInFlight ? inFlight[inFlight.length - 1] : undefined;
      const coalesced = entry !== undefined;
      if (!entry) {
        entry = startEntry(load);
      }
      const selected = entry;
      return selected.promise.then((value) => resultFor(selected, value, coalesced));
    },
  };
}

const TAB_META_EVENT_KINDS = new Set([
  "turn_started",
  "turn_done",
  "retrying",
  "approval_request",
  "ask_request",
]);

export function tabMetaFallbackDelay(visibility: DocumentVisibilityState): number {
  return visibility === "hidden" ? TAB_META_HIDDEN_FALLBACK_MS : TAB_META_VISIBLE_FALLBACK_MS;
}

export function shouldRefreshTabMetaForEvent(kind: string): boolean {
  return TAB_META_EVENT_KINDS.has(kind);
}

export function sameTabMetaLists(current: readonly TabMeta[], next: readonly TabMeta[]): boolean {
  if (current === next) return true;
  if (current.length !== next.length) return false;
  for (let index = 0; index < current.length; index += 1) {
    if (JSON.stringify(current[index]) !== JSON.stringify(next[index])) return false;
  }
  return true;
}

/** Authoritative running-id set — cards derive `running` from `has(id)` instead of trusting end-event delivery. Loop adds on dispatch entry, deletes in `finally` so every exit path cleans up. */

export type InflightSubscriber = () => void;

export class InflightSet {
  private readonly _set = new Set<string>();
  private readonly _listeners = new Set<InflightSubscriber>();

  add(id: string): void {
    if (this._set.has(id)) return;
    this._set.add(id);
    this._notify();
  }

  delete(id: string): void {
    if (this._set.delete(id)) this._notify();
  }

  has(id: string): boolean {
    return this._set.has(id);
  }

  /** Snapshot for diagnostics / tests; live view, do not mutate. */
  get size(): number {
    return this._set.size;
  }

  /** Subscribe to add/delete; returns the unsubscribe function. */
  subscribe(fn: InflightSubscriber): () => void {
    this._listeners.add(fn);
    return () => {
      this._listeners.delete(fn);
    };
  }

  /** Drop everything — only use at session reset. Notifies once. */
  clear(): void {
    if (this._set.size === 0) return;
    this._set.clear();
    this._notify();
  }

  private _notify(): void {
    for (const fn of this._listeners) {
      try {
        fn();
      } catch {
        /* listener errors must not break the gate */
      }
    }
  }
}

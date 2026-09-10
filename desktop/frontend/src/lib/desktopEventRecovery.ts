/** Read-side recovery only: callers retain their mounted UI and unsent drafts. */
export interface SnapshotRecoveryPorts<Scope, Snapshot> {
  subscribe(callback: () => void): () => void;
  capture(): Scope;
  isCurrent(scope: Scope): boolean;
  read(scope: Scope): Promise<Snapshot>;
  apply(snapshot: Snapshot, scope: Scope): void;
  failed?(error: unknown): void;
  timer(callback: () => void, delay: number): unknown;
  clearTimer(timer: unknown): void;
}

export function startDesktopEventRecovery<Scope, Snapshot>(ports: SnapshotRecoveryPorts<Scope, Snapshot>): () => void {
  let disposed = false, inFlight = false, requested = 0, failures = 0;
  let timer: unknown;
  const repair = async () => {
    if (disposed || inFlight) return;
    ports.clearTimer(timer);
    timer = undefined;
    inFlight = true;
    const version = requested;
    const scope = ports.capture();
    let retry = false;
    try {
      const snapshot = await ports.read(scope);
      if (disposed || version !== requested) return;
      if (!ports.isCurrent(scope)) { retry = true; return; }
      ports.apply(snapshot, scope);
      failures = 0;
    } catch (error) {
      if (!disposed && version === requested) { failures++; retry = true; ports.failed?.(error); }
    } finally {
      inFlight = false;
      if (!disposed && version !== requested) void repair();
      else if (!disposed && retry) timer = ports.timer(() => { void repair(); }, Math.min(30000, 1000 * 2 ** Math.min(failures, 5)));
    }
  };
  const off = ports.subscribe(() => { requested++; void repair(); });
  return () => { disposed = true; off(); ports.clearTimer(timer); };
}

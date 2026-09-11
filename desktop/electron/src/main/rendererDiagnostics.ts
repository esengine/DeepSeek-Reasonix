export interface CpuProfile {
  nodes: { id: number; callFrame: { functionName: string; url: string; lineNumber: number; columnNumber?: number } }[];
  samples?: number[];
  timeDeltas?: number[];
  startTime: number;
  endTime: number;
}
export interface ProfileFrame { label: string; samples: number; selfMs: number }
export interface RendererProfileResult {
  status: "captured" | "inactive" | "busy" | "cooldown" | "limit" | "unavailable" | "cancelled" | "failed";
  durationMs?: number;
  frames?: ProfileFrame[];
}
export interface ProfileTarget {
  debugger: {
    isAttached(): boolean;
    attach(): void;
    detach(): void;
    sendCommand(method: string, params?: Record<string, unknown>): Promise<any>;
    on(event: "detach", listener: () => void): unknown;
    removeListener(event: "detach", listener: () => void): unknown;
  };
  isDestroyed(): boolean;
  isDevToolsOpened(): boolean;
}
interface Dependencies {
  target(): ProfileTarget | null;
  isForeground(): boolean;
  onInvalidated(listener: () => void): () => void;
  analyse(profile: CpuProfile): Promise<ProfileFrame[]>;
  now?: () => number;
  durationMs?: number;
  cooldownMs?: number;
  maxCaptures?: number;
  commandTimeoutMs?: number;
}

// Own only the trusted renderer's debugger connection. Never share or steal a
// connection from DevTools or another caller; cancellation retains ownership
// until stop/detach has completed, so a late reply cannot affect a new capture.
export class RendererDiagnostics {
  private active: { requestId?: string; cancel(): void } | undefined;
  private lastAttempt = -Infinity;
  private captures = 0;
  private disposed = false;
  constructor(private readonly deps: Dependencies) {}
  get busy(): boolean { return this.active !== undefined; }

  cancel(requestId?: string): void {
    if (requestId !== undefined && this.active?.requestId !== requestId) return;
    this.active?.cancel();
  }
  dispose(): void { this.disposed = true; this.cancel(); }

  async capture(requestId?: string): Promise<RendererProfileResult> {
    if (this.disposed) return { status: "unavailable" };
    if (this.active) return { status: "busy" };
    if (!this.deps.isForeground()) return { status: "inactive" };
    const now = (this.deps.now ?? (() => performance.now()))();
    if (this.captures >= (this.deps.maxCaptures ?? 3)) return { status: "limit" };
    if (now - this.lastAttempt < (this.deps.cooldownMs ?? 600_000)) return { status: "cooldown" };
    const target = this.deps.target();
    if (!target || target.isDestroyed() || target.isDevToolsOpened() || target.debugger.isAttached()) return { status: "unavailable" };
    this.lastAttempt = now;
    this.captures++;
    let cancelled = false;
    let ownsConnection = false;
    let wake!: () => void;
    const interrupted = new Promise<void>((resolve) => { wake = resolve; });
    const owner = { requestId, cancel: () => { cancelled = true; wake(); } };
    this.active = owner;
    const onDetach = () => { ownsConnection = false; owner.cancel(); };
    let removeInvalidated = () => {};
    const inspector = target.debugger;
    const command = <T>(method: string, params?: Record<string, unknown>) => deadline<T>(
      inspector.sendCommand(method, params), this.deps.commandTimeoutMs ?? 1500,
    );
    let timer: ReturnType<typeof setTimeout> | undefined;
    const startedAt = (this.deps.now ?? (() => performance.now()))();
    try {
      removeInvalidated = this.deps.onInvalidated(owner.cancel);
      if (cancelled) return { status: "cancelled" };
      inspector.attach();
      ownsConnection = true;
      inspector.on("detach", onDetach);
      await command("Profiler.enable");
      if (cancelled) return { status: "cancelled" };
      await command("Profiler.setSamplingInterval", { interval: 10_000 });
      if (cancelled) return { status: "cancelled" };
      await command("Profiler.start");
      await Promise.race([
        interrupted,
        new Promise<void>((resolve) => { timer = setTimeout(resolve, this.deps.durationMs ?? 5000); }),
      ]);
      if (!ownsConnection || target.isDestroyed()) return { status: "cancelled" };
      const result = await command<{ profile: CpuProfile }>("Profiler.stop");
      if (cancelled) return { status: "cancelled" };
      // The raw profile never enters the renderer or its report payload.
      const frames = await this.deps.analyse(result.profile);
      if (cancelled) return { status: "cancelled" };
      return { status: "captured", durationMs: (this.deps.now ?? (() => performance.now()))() - startedAt, frames };
    } catch {
      return { status: cancelled ? "cancelled" : "failed" };
    } finally {
      clearTimeout(timer);
      try { removeInvalidated(); } catch { /* Cleanup cannot strand the lease. */ }
      if (ownsConnection) {
        try { inspector.detach(); } catch { /* Renderer exit also ends the session. */ }
      }
      try { inspector.removeListener("detach", onDetach); } catch { /* A destroyed target may reject cleanup. */ }
      if (this.active === owner) this.active = undefined;
    }
  }
}

function deadline<T>(work: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("diagnostic command timeout")), ms);
    work.then(resolve, reject).finally(() => clearTimeout(timer));
  });
}

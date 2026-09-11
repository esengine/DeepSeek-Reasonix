export interface ProcessDiagnosticsSnapshot {
  scope: "electron";
  growth?: { pid: number; type: string; metric: "private" | "working-set"; baselineMb: number; currentMb: number; durationMs: number }[];
  samples: {
    ageMs: number;
    intervalMs: number | null;
    truncated?: boolean;
    processes: { pid: number; type: string; cpuPercent: number | null; workingSetMb: number | null; privateMb: number | null }[];
  }[];
}

export interface RendererProfileResult {
  status: "recording" | "captured" | "inactive" | "busy" | "cooldown" | "limit" | "unavailable" | "cancelled" | "failed";
  durationMs?: number;
  frames?: { label: string; samples: number; selfMs: number }[];
}
export interface NativePerformanceActions {
  captureRendererProfile?(requestId?: string): Promise<RendererProfileResult>;
  cancelRendererProfile?(requestId?: string): Promise<unknown>;
  exportHeapSnapshot?(): Promise<{ status: "saved" | "cancelled" | "busy" | "unavailable" | "failed" }>;
}

// Reporting must still work with an older shell, a rejected IPC or a hung host.
export async function boundedDiagnostics<T>(read: () => Promise<T>, timeoutMs = 750): Promise<T | undefined> {
  let cancel = () => {};
  try {
    return await Promise.race([
      Promise.resolve().then(read).catch(() => undefined),
      new Promise<undefined>((resolve) => { const timer = setTimeout(resolve, timeoutMs); cancel = () => clearTimeout(timer); }),
    ]);
  } finally { cancel(); }
}

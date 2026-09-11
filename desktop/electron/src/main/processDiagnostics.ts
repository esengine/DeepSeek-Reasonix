import type { ProcessMetric } from "electron";

interface ProcessSample {
  pid: number; type: string; creationTime: number;
  cpuPercent: number | null; workingSetMb: number | null; privateMb: number | null;
}
interface Sample {
  atMs: number; intervalMs: number | null; truncated: boolean; processes: ProcessSample[];
}
export interface MemoryGrowth {
  pid: number; type: string; metric: "private" | "working-set";
  baselineMb: number; currentMb: number; durationMs: number;
}

export function detectMemoryGrowth(samples: readonly Sample[]): MemoryGrowth[] {
  if (samples.length < 5) return [];
  const last = samples[samples.length - 1];
  if (last.atMs - samples[0].atMs < 120_000) return [];
  const growth: MemoryGrowth[] = [];
  for (const process of last.processes) {
    if (!Number.isFinite(process.creationTime)) continue;
    const rows = samples.map((s) => s.processes.find((p) => p.pid === process.pid && p.creationTime === process.creationTime));
    // Require one continuous process identity and a settled two-reading baseline.
    if (rows.some((row) => !row)) continue;
    const metric = rows.every((row) => row!.privateMb !== null) ? "private" : "working-set";
    const values = rows.map((row) => metric === "private" ? row!.privateMb : row!.workingSetMb);
    if (values.some((v) => v === null)) continue;
    const baselineMb = Math.max(values[0]!, values[1]!);
    const threshold = baselineMb + Math.max(256, baselineMb * 0.5);
    if (!values.slice(-3).every((value) => value! >= threshold)) continue;
    growth.push({ pid: process.pid, type: process.type, metric, baselineMb, currentMb: values[values.length - 1]!, durationMs: last.atMs - samples[0].atMs });
  }
  return growth.sort((a, b) => (b.currentMb - b.baselineMb) - (a.currentMb - a.baselineMb)).slice(0, 3);
}

// Numeric process metrics only. This does not include the Go service and must
// never be described as a leak detector or the app's exclusive physical memory.
export class ProcessDiagnostics {
  private samples: Sample[] = [];
  private previousAt: number | undefined;
  private previousProcesses = new Set<string>();
  constructor(
    private readonly read: () => ProcessMetric[],
    private readonly now = () => performance.now(),
    private readonly foreground = () => true,
  ) {}

  sample(): void {
    const atMs = this.now();
    const interval = this.foreground() ? 30_000 : 60_000;
    if (this.previousAt !== undefined && atMs - this.previousAt < interval) return;
    try {
      const metrics = this.read();
      const intervalMs = this.previousAt === undefined ? null : atMs - this.previousAt;
      const finite = (n: number | undefined) => typeof n === "number" && Number.isFinite(n) && n >= 0 ? n : null;
      const mb = (n: number | undefined) => { const value = finite(n); return value === null ? null : value / 1024; };
      const types = new Set(["Browser", "Tab", "GPU", "Utility", "Zygote", "Sandbox helper", "Pepper Plugin", "Pepper Plugin Broker"]);
      const processes = metrics.slice(0, 128).map((metric) => ({
        pid: metric.pid,
        type: types.has(metric.type) ? metric.type : "Other",
        creationTime: metric.creationTime,
        cpuPercent: intervalMs === null || !this.previousProcesses.has(`${metric.pid}:${metric.creationTime}`) ? null : finite(metric.cpu?.percentCPUUsage),
        workingSetMb: mb(metric.memory?.workingSetSize),
        privateMb: mb(metric.memory?.privateBytes),
      }));
      this.previousAt = atMs;
      this.previousProcesses = new Set(processes.map((p) => `${p.pid}:${p.creationTime}`));
      this.samples = [...this.samples.filter((s) => atMs - s.atMs <= 300_000), { atMs, intervalMs, truncated: metrics.length > 128, processes }].slice(-12);
    } catch { /* A failed diagnostic sample must never affect the app. */ }
  }

  snapshot() {
    this.sample();
    const now = this.now();
    const samples = this.samples.filter((s) => now - s.atMs <= 300_000);
    return {
      scope: "electron" as const,
      growth: detectMemoryGrowth(samples),
      samples: samples.map(({ atMs, ...sample }) => ({ ageMs: now - atMs, ...sample })),
    };
  }
}

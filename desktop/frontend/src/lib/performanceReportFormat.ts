import type { PerformanceSnapshot } from "./crash";

export function fmtNumber(n: number, digits = 0): string {
  return Number.isFinite(n) ? n.toFixed(digits) : "0";
}

export function fmtMb(n: number): string {
  return `${fmtNumber(n, 1)} MB`;
}

export function formatPerformanceContext(snapshot: PerformanceSnapshot): string {
  const lines = [
    `reason: ${snapshot.reason}`,
    `uptime: ${fmtNumber(snapshot.uptimeMs / 1000, 1)}s`,
    `visibility: ${snapshot.visibility || "unknown"}`,
    `focused: ${snapshot.focused ? "true" : "false"}`,
    `online: ${snapshot.online ? "true" : "false"}`,
    `hardware concurrency: ${snapshot.hardwareConcurrency || "unknown"}`,
  ];
  if (snapshot.deviceMemoryGb) lines.push(`device memory: ${snapshot.deviceMemoryGb} GB`);
  if (snapshot.jsHeap) {
    const pct =
      snapshot.jsHeap.usagePercent !== undefined ? `, ${fmtNumber(snapshot.jsHeap.usagePercent)}% of limit` : "";
    lines.push(
      `js heap: ${fmtMb(snapshot.jsHeap.usedMb)} used, ${fmtMb(snapshot.jsHeap.totalMb)} allocated, ${fmtMb(snapshot.jsHeap.limitMb)} limit${pct}`,
    );
  }
  if (snapshot.eventLoopLag) {
    lines.push(
      `event loop lag: current ${fmtNumber(snapshot.eventLoopLag.currentMs)}ms, max ${fmtNumber(snapshot.eventLoopLag.maxMs)}ms, avg ${fmtNumber(snapshot.eventLoopLag.avgMs)}ms over ${snapshot.eventLoopLag.samples} samples`,
    );
  }
  if (snapshot.longTasks) {
    const recent = snapshot.longTasks.recent
      .map(
        (t) =>
          `${fmtNumber(t.durationMs)}ms @ ${fmtNumber(t.startMs / 1000, 1)}s${t.attribution ? ` (${t.attribution})` : ""}`,
      )
      .join("; ");
    lines.push(
      `long tasks: ${snapshot.longTasks.count} in the last 60s, max ${fmtNumber(snapshot.longTasks.maxMs)}ms, total ${fmtNumber(snapshot.longTasks.totalMs)}ms`,
    );
    if (recent) lines.push(`recent long tasks: ${recent}`);
  }
  if (snapshot.longTaskFrames?.length) {
    lines.push("long task top frames (sampled):");
    for (const frame of snapshot.longTaskFrames) lines.push(`  ${frame.samples}x ${frame.label}`);
  }
  if (snapshot.cpuProfile) {
    const profile = snapshot.cpuProfile;
    lines.push(`CPU profile after trigger: ${profile.status}${profile.durationMs === undefined ? "" : `, ${fmtNumber(profile.durationMs)}ms`}; does not reconstruct the original long task`);
    for (const frame of profile.frames ?? []) lines.push(`  ${fmtNumber(frame.selfMs, 1)}ms / ${frame.samples} samples: ${frame.label}`);
  }
  if (snapshot.processes) {
    lines.push("process samples: Electron only (Go service excluded); working sets may share pages; CPU is interval average");
    const samples = snapshot.processes.samples;
    for (const sample of samples) {
      const total = sample.processes.every((p) => p.workingSetMb !== null)
        ? fmtMb(sample.processes.reduce((sum, p) => sum + (p.workingSetMb ?? 0), 0)) : "unavailable";
      lines.push(`  ${fmtNumber(sample.ageMs)}ms ago: ${sample.processes.length} processes${sample.truncated ? " (truncated)" : ""}, summed working set ${total}`);
    }
    for (const growth of snapshot.processes.growth ?? []) {
      lines.push(`sustained memory growth (not proof of a leak): PID ${growth.pid} ${growth.type}, ${growth.metric} ${fmtMb(growth.baselineMb)} → ${fmtMb(growth.currentMb)} over ${fmtNumber(growth.durationMs / 1000)}s`);
    }
    const latest = samples[samples.length - 1];
    if (latest) {
      lines.push(`latest process CPU interval: ${latest.intervalMs === null ? "baseline unavailable" : `${fmtNumber(latest.intervalMs)}ms`}`);
      for (const p of latest.processes) {
        lines.push(`  PID ${p.pid} ${p.type}: CPU ${p.cpuPercent === null ? "unavailable" : `${fmtNumber(p.cpuPercent, 1)}%`}, working set ${p.workingSetMb === null ? "unavailable" : fmtMb(p.workingSetMb)}, private ${p.privateMb === null ? "unavailable" : fmtMb(p.privateMb)}`);
      }
    }
  }
  if (snapshot.connection) {
    const parts = [
      snapshot.connection.effectiveType,
      snapshot.connection.rttMs !== undefined ? `${snapshot.connection.rttMs}ms rtt` : "",
      snapshot.connection.downlinkMbps !== undefined ? `${snapshot.connection.downlinkMbps} Mbps` : "",
      snapshot.connection.saveData !== undefined ? `saveData ${snapshot.connection.saveData ? "true" : "false"}` : "",
    ].filter(Boolean);
    if (parts.length) lines.push(`connection: ${parts.join(", ")}`);
  }
  const pipeline = snapshot.sessionPipeline;
  if (pipeline?.activation) {
    const a = pipeline.activation;
    const parts = [`request ${a.requestId}`];
    if (a.tabId) parts.push(`tab ${a.tabId}`);
    if (a.ticketToStartingMs !== undefined) parts.push(`ticket→starting ${fmtNumber(a.ticketToStartingMs)}ms`);
    if (a.startingToReadyMs !== undefined) parts.push(`starting→ready ${fmtNumber(a.startingToReadyMs)}ms`);
    if (a.totalMs !== undefined) parts.push(`total ${fmtNumber(a.totalMs)}ms`);
    if (a.outcome) parts.push(`outcome ${a.outcome}`);
    if (a.failureClass) parts.push(`failure ${a.failureClass}`);
    lines.push(`activation: ${parts.join(", ")}`);
  }
  if (pipeline?.history) {
    const h = pipeline.history;
    lines.push(
      `history page: ${h.entries} entries, ${fmtNumber(h.inlineBytes / 1024, 1)} KiB inline, ${fmtNumber(h.durationMs)}ms, source ${h.source || "unknown"}${h.stale ? ", stale" : ""} ` +
        `(pages ${h.pages}, stale ${h.staleCount}, index hits ${h.indexHits}, misses ${h.indexMisses})`,
    );
  }
  if (pipeline?.mountedRows) {
    lines.push(`mounted rows: ${pipeline.mountedRows.mounted} of ${pipeline.mountedRows.total}`);
  }
  if (pipeline?.markdownWorker) {
    const w = pipeline.markdownWorker;
    lines.push(
      `markdown worker: ${w.pending} pending, ${w.completed} parsed, avg ${fmtNumber(w.avgParseMs, 1)}ms, max ${fmtNumber(w.maxParseMs)}ms` +
        `${w.fallbackActive ? ", fallback active" : ""}${w.workerFailures > 0 ? `, ${w.workerFailures} worker failures` : ""}`,
    );
  }
  if (pipeline?.transcriptCache) {
    const c = pipeline.transcriptCache;
    lines.push(
      `transcript cache: ${c.residentSessions}/${c.maxResidentSessions} resident sessions, ` +
        `bodies ${fmtMb(c.bodyBytes / 1048576)} of ${fmtMb(c.bodyBudgetBytes / 1048576)}, ` +
        `markdown ${fmtMb(c.markdownBytes / 1048576)} of ${fmtMb(c.markdownBudgetBytes / 1048576)}, ` +
        `evictions ${c.historyEvictions} history + ${c.markdownEvictions} markdown`,
    );
  }
  return lines.join("\n");
}

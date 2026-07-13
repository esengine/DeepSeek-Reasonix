// Last-resort crash surface: a React render error with no boundary unmounts the
// whole tree (blank window), and global errors/rejections leave no trace either.

import { addBreadcrumb, dumpBreadcrumbs, snapshotBreadcrumbs, type Breadcrumb } from "./breadcrumbs";
import { t } from "./i18n";

declare const __BUILD_COMMIT__: string;
declare const __BUILD_CHANNEL__: string;

export type CrashKind = "crash" | "exception" | "feedback" | "performance" | "bot";

export type PerformanceSnapshot = {
  reason: string;
  uptimeMs: number;
  visibility: string;
  focused: boolean;
  online: boolean;
  hardwareConcurrency: number;
  deviceMemoryGb?: number;
  jsHeap?: {
    usedMb: number;
    totalMb: number;
    limitMb: number;
    usagePercent?: number;
  };
  eventLoopLag?: {
    currentMs: number;
    maxMs: number;
    avgMs: number;
    samples: number;
  };
  longTasks?: {
    count: number;
    totalMs: number;
    maxMs: number;
    recent: { startMs: number; durationMs: number; attribution?: string }[];
  };
  longTaskFrames?: { label: string; samples: number }[];
  connection?: {
    effectiveType?: string;
    downlinkMbps?: number;
    rttMs?: number;
    saveData?: boolean;
  };
};

export type CrashPayload = {
  schemaVersion: 2;
  source: "frontend" | "frontend.react" | "frontend.global" | "frontend.performance" | "bot.runtime";
  kind: CrashKind;
  label: string;
  message: string;
  errorType: string;
  errorMessage: string;
  stack?: string;
  componentStack?: string;
  topFrame?: string;
  buildCommit: string;
  channel: string;
  language: string;
  view: string;
  breadcrumbs: Breadcrumb[];
  occurredAt: string;
};

type NormalizedError = {
  errorType: string;
  errorMessage: string;
  stack?: string;
};

type LongTaskSample = {
  startMs: number;
  durationMs: number;
  attribution?: string;
};

// WICG JS Self-Profiling API (https://wicg.github.io/js-self-profiling/), available
// in Chromium WebViews when the document is served with `Document-Policy: js-profiling`.
export type ProfilerTrace = {
  resources?: string[];
  frames?: { name?: string; resourceId?: number; line?: number; column?: number }[];
  stacks?: { frameId: number; parentId?: number }[];
  samples?: { timestamp: number; stackId?: number }[];
};

type ProfilerLike = {
  stop(): Promise<ProfilerTrace>;
  addEventListener?: (type: string, listener: () => void) => void;
};

type ProfilerConstructor = new (options: { sampleInterval: number; maxBufferSize: number }) => ProfilerLike;

type BrowserPerformanceMemory = {
  usedJSHeapSize?: number;
  totalJSHeapSize?: number;
  jsHeapSizeLimit?: number;
};

type BrowserNavigator = Navigator & {
  deviceMemory?: number;
  connection?: {
    effectiveType?: string;
    downlink?: number;
    rtt?: number;
    saveData?: boolean;
  };
};

const LONG_TASK_WINDOW_MS = 60_000;
const LONG_TASK_PROMPT_MS = 800;
// Streaming renders routinely accumulate ~1.5s of 70-240ms tasks per minute without
// user-visible jank, so the cumulative prompt only fires past half of that budget spent blocked.
const LONG_TASK_TOTAL_PROMPT_MS = 3_000;
const EVENT_LOOP_LAG_PROMPT_MS = 1_200;
const STARTUP_GRACE_MS = 15_000;
const PROMPT_COOLDOWN_MS = 10 * 60_000;
const MAX_LAG_SAMPLES = 60;
const VISIBILITY_RESUME_GRACE_MS = 5_000;

const longTasks: LongTaskSample[] = [];
const lagSamples: number[] = [];
let performanceMonitorInstalled = false;
let lastPerformancePromptAt = 0;

// Rolling self-profiling sampler (Chromium WebViews only; requires the asset server
// to send `Document-Policy: js-profiling`, see jsProfilingMiddleware on the Go side).
// ~10ms native sampling; the buffer covers the same 60s window as longTasks.
const PROFILER_SAMPLE_INTERVAL_MS = 10;
const PROFILER_MAX_BUFFER_SAMPLES = LONG_TASK_WINDOW_MS / PROFILER_SAMPLE_INTERVAL_MS;
let activeProfiler: ProfilerLike | null = null;

function startLongTaskProfiler(): void {
  const ProfilerCtor = (globalThis as { Profiler?: ProfilerConstructor }).Profiler;
  if (!ProfilerCtor) return;
  try {
    const profiler = new ProfilerCtor({
      sampleInterval: PROFILER_SAMPLE_INTERVAL_MS,
      maxBufferSize: PROFILER_MAX_BUFFER_SAMPLES,
    });
    // A full buffer stops sampling silently; drop the stale trace and roll over.
    profiler.addEventListener?.("samplebufferfull", () => {
      if (activeProfiler !== profiler) return;
      activeProfiler = null;
      void profiler.stop().catch(() => {});
      startLongTaskProfiler();
    });
    activeProfiler = profiler;
  } catch {
    // Document policy missing or the API is disabled in this WebView.
    activeProfiler = null;
  }
}

async function collectLongTaskFrames(
  windows: { startMs: number; durationMs: number }[],
): Promise<{ label: string; samples: number }[]> {
  const profiler = activeProfiler;
  if (!profiler) return [];
  activeProfiler = null;
  try {
    const trace = await profiler.stop();
    return aggregateLongTaskProfile(trace, windows);
  } catch {
    return [];
  } finally {
    startLongTaskProfiler();
  }
}

const PERF_REPORTED_STORAGE_KEY = "reasonix:perf-reported";

// Idempotent per pressure label: once a category is reported (persisted per build) or
// dismissed (session only), stop re-surfacing it so a steady slowdown can't spam prompts.
const dismissedPerfLabels = new Set<string>();
let reportedPerfLabels: Set<string> | null = null;

function currentBuildCommit(): string {
  return typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
}

export function parseReportedPerf(raw: string | null, build: string): Set<string> {
  if (!raw) return new Set();
  try {
    const parsed = JSON.parse(raw) as { build?: string; labels?: unknown };
    if (parsed.build !== build || !Array.isArray(parsed.labels)) return new Set();
    return new Set(parsed.labels.filter((label): label is string => typeof label === "string"));
  } catch {
    return new Set();
  }
}

export function serializeReportedPerf(labels: ReadonlySet<string>, build: string): string {
  return JSON.stringify({ build, labels: [...labels] });
}

function getReportedPerfLabels(): Set<string> {
  if (reportedPerfLabels) return reportedPerfLabels;
  let raw: string | null = null;
  try {
    raw = typeof localStorage !== "undefined" ? localStorage.getItem(PERF_REPORTED_STORAGE_KEY) : null;
  } catch {
    raw = null;
  }
  reportedPerfLabels = parseReportedPerf(raw, currentBuildCommit());
  return reportedPerfLabels;
}

function markPerfReported(label: string): void {
  const set = getReportedPerfLabels();
  if (set.has(label)) return;
  set.add(label);
  try {
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(PERF_REPORTED_STORAGE_KEY, serializeReportedPerf(set, currentBuildCommit()));
    }
  } catch {
    // localStorage can throw (private mode / quota); the session-level set still dedups.
  }
}

function clip(s: string, n: number): string {
  return s.length > n ? s.slice(0, n) : s;
}

function safeStringify(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

export function normalizeCrashError(err: unknown): NormalizedError {
  if (err instanceof Error) {
    return {
      errorType: err.name || "Error",
      errorMessage: err.message || String(err),
      stack: err.stack,
    };
  }
  if (typeof err === "string") {
    return { errorType: "string", errorMessage: err };
  }
  if (err && typeof err === "object") {
    const obj = err as { name?: unknown; message?: unknown; stack?: unknown; constructor?: { name?: string } };
    const errorType = typeof obj.name === "string" && obj.name ? obj.name : obj.constructor?.name || "object";
    const errorMessage =
      typeof obj.message === "string" && obj.message ? obj.message : clip(safeStringify(err), 1000);
    return {
      errorType,
      errorMessage,
      stack: typeof obj.stack === "string" ? obj.stack : undefined,
    };
  }
  return { errorType: typeof err, errorMessage: String(err) };
}

export function topFrameFromStack(stack?: string): string {
  if (!stack) return "";
  const lines = stack
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
  return lines.find((l) => /\b(src|assets|wails|frontend)\b|\.tsx?:|\.jsx?:/.test(l)) ?? lines[1] ?? lines[0] ?? "";
}

function currentView(): string {
  if (typeof window === "undefined") return "";
  const { protocol, host, pathname, hash } = window.location;
  const safeHash = hash && hash.length < 80 ? hash : "";
  return clip(`${protocol}//${host}${pathname}${safeHash}`, 180);
}

function kindForLabel(label: string): CrashKind {
  return label === "unhandledrejection" ? "exception" : "crash";
}

function sourceForLabel(label: string): CrashPayload["source"] {
  if (label === "react") return "frontend.react";
  if (label === "window.error" || label === "unhandledrejection") return "frontend.global";
  return "frontend";
}

function formatText(label: string, normalized: NormalizedError, extra?: string): string {
  const detail = normalized.stack || normalized.errorMessage;
  const crumbs = dumpBreadcrumbs();
  const buildCommit = typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
  return [`[${label}]`, detail, extra?.trim(), crumbs && `--- breadcrumbs ---\n${crumbs}`, `build ${buildCommit}`]
    .filter(Boolean)
    .join("\n\n");
}

function fmtNumber(n: number, digits = 0): string {
  return Number.isFinite(n) ? n.toFixed(digits) : "0";
}

function fmtMb(n: number): string {
  return `${fmtNumber(n, 1)} MB`;
}

function readHeapSnapshot(): PerformanceSnapshot["jsHeap"] | undefined {
  if (typeof performance === "undefined") return undefined;
  const memory = (performance as Performance & { memory?: BrowserPerformanceMemory }).memory;
  if (!memory?.usedJSHeapSize || !memory.totalJSHeapSize || !memory.jsHeapSizeLimit) return undefined;
  const usedMb = memory.usedJSHeapSize / 1024 / 1024;
  const totalMb = memory.totalJSHeapSize / 1024 / 1024;
  const limitMb = memory.jsHeapSizeLimit / 1024 / 1024;
  return {
    usedMb,
    totalMb,
    limitMb,
    usagePercent: limitMb > 0 ? (usedMb / limitMb) * 100 : undefined,
  };
}

function pruneLongTasks(now = performance.now()): void {
  while (longTasks.length && now - longTasks[0].startMs > LONG_TASK_WINDOW_MS) longTasks.shift();
}

function longTaskSummary(now = performance.now()): PerformanceSnapshot["longTasks"] {
  pruneLongTasks(now);
  if (!longTasks.length) return undefined;
  const totalMs = longTasks.reduce((sum, t) => sum + t.durationMs, 0);
  const maxMs = Math.max(...longTasks.map((t) => t.durationMs));
  return {
    count: longTasks.length,
    totalMs,
    maxMs,
    recent: longTasks.slice(-5),
  };
}

function eventLoopLagSummary(currentMs = 0): PerformanceSnapshot["eventLoopLag"] {
  const samples = lagSamples.filter((n) => n > 0);
  if (!samples.length && currentMs <= 0) return undefined;
  const all = currentMs > 0 ? [...samples, currentMs] : samples;
  const total = all.reduce((sum, n) => sum + n, 0);
  return {
    currentMs,
    maxMs: Math.max(...all),
    avgMs: total / all.length,
    samples: all.length,
  };
}

function networkSnapshot(): PerformanceSnapshot["connection"] {
  if (typeof navigator === "undefined") return undefined;
  const connection = (navigator as BrowserNavigator).connection;
  if (!connection) return undefined;
  return {
    effectiveType: connection.effectiveType,
    downlinkMbps: connection.downlink,
    rttMs: connection.rtt,
    saveData: connection.saveData,
  };
}

function performanceSnapshot(reason: string, currentLagMs = 0): PerformanceSnapshot {
  const nav = typeof navigator === "undefined" ? undefined : (navigator as BrowserNavigator);
  const doc = typeof document === "undefined" ? undefined : document;
  return {
    reason,
    uptimeMs: typeof performance !== "undefined" ? performance.now() : 0,
    visibility: doc?.visibilityState ?? "",
    focused: doc?.hasFocus?.() ?? false,
    online: nav?.onLine ?? true,
    hardwareConcurrency: nav?.hardwareConcurrency ?? 0,
    deviceMemoryGb: nav?.deviceMemory,
    jsHeap: readHeapSnapshot(),
    eventLoopLag: eventLoopLagSummary(currentLagMs),
    longTasks: typeof performance !== "undefined" ? longTaskSummary() : undefined,
    connection: networkSnapshot(),
  };
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
  if (snapshot.connection) {
    const parts = [
      snapshot.connection.effectiveType,
      snapshot.connection.rttMs !== undefined ? `${snapshot.connection.rttMs}ms rtt` : "",
      snapshot.connection.downlinkMbps !== undefined ? `${snapshot.connection.downlinkMbps} Mbps` : "",
      snapshot.connection.saveData !== undefined ? `saveData ${snapshot.connection.saveData ? "true" : "false"}` : "",
    ].filter(Boolean);
    if (parts.length) lines.push(`connection: ${parts.join(", ")}`);
  }
  return lines.join("\n");
}

export function performanceLabelForReason(reason: string): string {
  const normalized = reason.trim().toLowerCase();
  if (normalized.startsWith("event loop lag")) return "performance.lag";
  if (normalized.startsWith("long task")) return "performance.longtask";
  if (normalized.startsWith("js heap")) return "performance.heap";
  return "performance.pressure";
}

export function shouldRecordLongTaskSample(
  startMs: number,
  durationMs: number,
  graceUntilMs: number,
  visibilityHidden = false,
  visibleSinceMs = 0,
  focused = true,
): boolean {
  if (!focused) return false;
  if (visibilityHidden) return false;
  return durationMs >= 50 && startMs >= graceUntilMs && startMs - visibleSinceMs >= VISIBILITY_RESUME_GRACE_MS;
}

export function shouldPromptForLongTasks(summary: { count: number; totalMs: number; maxMs: number }): boolean {
  return summary.maxMs >= LONG_TASK_PROMPT_MS || (summary.count >= 3 && summary.totalMs >= LONG_TASK_TOTAL_PROMPT_MS);
}

type TaskAttributionLike = {
  containerType?: string;
  containerName?: string;
  containerId?: string;
  containerSrc?: string;
};

// Longtask entries carry no stacks, only a culprit descriptor ("self", "same-origin",
// iframe container, ...). "self" and "unknown" are the expected no-signal cases, so
// only anomalies (cross-context culprits, named containers) make it into the report.
export function formatLongTaskAttribution(entryName?: string, attribution?: TaskAttributionLike[]): string {
  const parts: string[] = [];
  if (entryName && entryName !== "unknown" && entryName !== "self") parts.push(entryName);
  const culprit = attribution?.[0];
  if (culprit) {
    const container = culprit.containerName || culprit.containerId || culprit.containerSrc || "";
    const containerType = culprit.containerType && culprit.containerType !== "window" ? culprit.containerType : "";
    const detail = [containerType, container].filter(Boolean).join(":");
    if (detail) parts.push(detail);
  }
  return parts.join(" ");
}

// Self-time view of a self-profiling trace: count each sample that landed inside a
// long-task window against its leaf frame, so the report names the code that was
// actually on-CPU while the UI was blocked.
export function aggregateLongTaskProfile(
  trace: ProfilerTrace,
  windows: { startMs: number; durationMs: number }[],
  maxFrames = 8,
): { label: string; samples: number }[] {
  if (!windows.length) return [];
  const counts = new Map<number, number>();
  for (const sample of trace.samples ?? []) {
    if (sample.stackId === undefined) continue;
    const inWindow = windows.some(
      (w) => sample.timestamp >= w.startMs && sample.timestamp <= w.startMs + w.durationMs,
    );
    if (!inWindow) continue;
    const stack = trace.stacks?.[sample.stackId];
    if (!stack) continue;
    counts.set(stack.frameId, (counts.get(stack.frameId) ?? 0) + 1);
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, maxFrames)
    .map(([frameId, samples]) => ({ label: formatProfilerFrame(trace, frameId), samples }));
}

function formatProfilerFrame(trace: ProfilerTrace, frameId: number): string {
  const frame = trace.frames?.[frameId];
  if (!frame) return `frame#${frameId}`;
  const name = frame.name || "(anonymous)";
  const resource = frame.resourceId !== undefined ? trace.resources?.[frame.resourceId] : undefined;
  if (!resource) return name;
  const line = frame.line !== undefined ? `:${frame.line}${frame.column !== undefined ? `:${frame.column}` : ""}` : "";
  return `${name} (${resource}${line})`;
}

export function shouldRecordEventLoopLagSample(
  visibilityHidden: boolean,
  msSinceVisible: number,
  focused = true,
): boolean {
  if (!focused) return false;
  if (visibilityHidden) return false;
  return msSinceVisible >= VISIBILITY_RESUME_GRACE_MS;
}

export function buildPerformancePayload(snapshot: PerformanceSnapshot): CrashPayload {
  const buildCommit = typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
  const context = formatPerformanceContext(snapshot);
  const crumbs = dumpBreadcrumbs();
  const label = performanceLabelForReason(snapshot.reason);
  const errorMessage = "UI responsiveness degraded because the app observed long tasks, event-loop lag, or high JS heap pressure.";
  return {
    schemaVersion: 2,
    source: "frontend.performance",
    kind: "performance",
    label,
    message: [
      `[${label}]`,
      errorMessage,
      `--- performance context ---\n${context}`,
      crumbs && `--- breadcrumbs ---\n${crumbs}`,
      `build ${buildCommit}`,
    ]
      .filter(Boolean)
      .join("\n\n"),
    errorType: "PerformancePressure",
    errorMessage,
    topFrame: "frontend.performance",
    buildCommit,
    channel: typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "",
    language: typeof navigator !== "undefined" ? navigator.language || "" : "",
    view: currentView(),
    breadcrumbs: snapshotBreadcrumbs(),
    occurredAt: new Date().toISOString(),
  };
}

export function buildCrashPayload(label: string, err: unknown, extra?: string): CrashPayload {
  const normalized = normalizeCrashError(err);
  const buildCommit = typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
  return {
    schemaVersion: 2,
    source: sourceForLabel(label),
    kind: kindForLabel(label),
    label,
    message: formatText(label, normalized, extra),
    errorType: normalized.errorType,
    errorMessage: normalized.errorMessage,
    stack: normalized.stack,
    componentStack: extra?.trim() || undefined,
    topFrame: topFrameFromStack(normalized.stack || extra),
    buildCommit,
    channel: typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "",
    language: typeof navigator !== "undefined" ? navigator.language || "" : "",
    view: currentView(),
    breadcrumbs: snapshotBreadcrumbs(),
    occurredAt: new Date().toISOString(),
  };
}

function sendButton(
  payload: CrashPayload,
  className = "crash-overlay__send",
  onSent?: () => void,
): HTMLButtonElement | null {
  // Resolved at click time via window.go, not the bridge module: this overlay must
  // stay usable even when the rest of the app (and its imports) is broken.
  const report = window.go?.main?.App?.ReportCrash;
  if (!report) return null;
  const send = document.createElement("button");
  send.className = className;
  send.textContent = t("crash.send");
  send.onclick = async () => {
    send.disabled = true;
    send.textContent = t("crash.sending");
    try {
      await report(payload.kind, JSON.stringify(payload));
      send.textContent = t("crash.sent");
      onSent?.();
    } catch {
      send.textContent = t("crash.sendFailed");
    }
  };
  return send;
}

function paintPerformancePrompt(payload: CrashPayload, snapshot: PerformanceSnapshot) {
  if (typeof document === "undefined") return;
  let host = document.getElementById("performance-report-prompt");
  if (!host) {
    host = document.createElement("div");
    host.id = "performance-report-prompt";
    document.body.appendChild(host);
  }
  const title = document.createElement("div");
  title.className = "performance-report__title";
  title.textContent = t("performanceReport.title");
  const body = document.createElement("pre");
  body.className = "performance-report__body";
  body.textContent = formatPerformanceContext(snapshot);
  const actions = document.createElement("div");
  actions.className = "performance-report__actions";
  const send = sendButton(payload, "performance-report__send", () => markPerfReported(payload.label));
  const copy = document.createElement("button");
  copy.className = "performance-report__copy";
  copy.textContent = t("crash.copy");
  copy.onclick = () => void navigator.clipboard?.writeText(payload.message);
  const dismiss = document.createElement("button");
  dismiss.className = "performance-report__dismiss";
  dismiss.textContent = t("performanceReport.dismiss");
  dismiss.onclick = () => {
    dismissedPerfLabels.add(payload.label);
    host?.remove();
  };
  if (send) actions.append(send);
  actions.append(copy, dismiss);
  const note = document.createElement("div");
  note.className = "performance-report__note";
  note.textContent = t("performanceReport.privacyNote");
  host.replaceChildren(title, body, actions, note);
}

function paint(payload: CrashPayload) {
  let host = document.getElementById("crash-overlay");
  if (!host) {
    host = document.createElement("div");
    host.id = "crash-overlay";
    document.body.appendChild(host);
  }
  const title = document.createElement("div");
  title.className = "crash-overlay__title";
  title.textContent = t("crash.title");
  const body = document.createElement("pre");
  body.className = "crash-overlay__body";
  body.textContent = payload.message;
  const copy = document.createElement("button");
  copy.className = "crash-overlay__copy";
  copy.textContent = t("crash.copy");
  copy.onclick = () => void navigator.clipboard?.writeText(payload.message);
  const actions = document.createElement("div");
  actions.className = "crash-overlay__actions";
  const send = sendButton(payload);
  if (send) actions.append(send);
  actions.append(copy);
  const note = document.createElement("div");
  note.className = "crash-overlay__note";
  note.textContent = t("crash.privacyNote");
  host.replaceChildren(title, body, actions, ...(send ? [note] : []));
}

export function reportCrash(label: string, err: unknown, extra?: string) {
  paint(buildCrashPayload(label, err, extra));
}

type GlobalCrashEventLike = Pick<Event, "defaultPrevented"> & {
  message?: unknown;
  error?: unknown;
  filename?: unknown;
  lineno?: unknown;
  colno?: unknown;
};

const RESIZE_OBSERVER_LOOP_MESSAGE_RE =
  /^ResizeObserver loop (?:limit exceeded|completed with undelivered notifications\.?)$/;
const OPAQUE_SCRIPT_ERROR_MESSAGE = "Script error.";

function globalCrashEventMessages(e: GlobalCrashEventLike): string[] {
  const messages: string[] = [];
  const pushMessage = (message: string) => {
    const trimmed = message.trim();
    if (trimmed) messages.push(trimmed);
  };
  if (typeof e.message === "string") pushMessage(e.message);
  const error = e.error;
  if (typeof error === "string") pushMessage(error);
  if (error && typeof error === "object" && "message" in error) {
    const msg = (error as { message?: unknown }).message;
    if (typeof msg === "string") pushMessage(msg);
  }
  return messages;
}

export function shouldReportGlobalCrashEvent(e: GlobalCrashEventLike): boolean {
  if (e.defaultPrevented) return false;
  if (globalCrashEventMessages(e).some((message) => RESIZE_OBSERVER_LOOP_MESSAGE_RE.test(message))) return false;
  return true;
}

function globalScriptErrorLocation(e: GlobalCrashEventLike): string {
  const parts: string[] = [];
  if (typeof e.filename === "string" && e.filename.trim()) parts.push(`filename=${e.filename.trim()}`);
  if (typeof e.lineno === "number" && Number.isFinite(e.lineno) && e.lineno > 0) parts.push(`lineno=${e.lineno}`);
  if (typeof e.colno === "number" && Number.isFinite(e.colno) && e.colno > 0) parts.push(`colno=${e.colno}`);
  return parts.join(" ");
}

export function globalCrashReportReason(e: GlobalCrashEventLike): unknown {
  if (e.error !== undefined && e.error !== null) return e.error;
  const message = typeof e.message === "string" ? e.message.trim() : e.message;
  if (message === OPAQUE_SCRIPT_ERROR_MESSAGE) {
    const location = globalScriptErrorLocation(e);
    if (location) return `${OPAQUE_SCRIPT_ERROR_MESSAGE}\n${location}`;
  }
  return e.message;
}

export function shouldPromptForPerformanceLabel(
  alreadyHandled: boolean,
  msSinceLastPrompt: number,
  visibilityHidden: boolean,
  focused = true,
): boolean {
  if (alreadyHandled) return false;
  if (msSinceLastPrompt < PROMPT_COOLDOWN_MS) return false;
  if (visibilityHidden) return false;
  if (!focused) return false;
  return true;
}

function isPerfLabelHandled(label: string): boolean {
  return dismissedPerfLabels.has(label) || getReportedPerfLabels().has(label);
}

function shouldPromptForPerformance(now: number, label: string): boolean {
  const hidden = typeof document !== "undefined" && document.visibilityState === "hidden";
  const focused = typeof document === "undefined" || document.hasFocus?.() !== false;
  return shouldPromptForPerformanceLabel(isPerfLabelHandled(label), now - lastPerformancePromptAt, hidden, focused);
}

function promptPerformanceReport(reason: string, currentLagMs = 0): void {
  const now = Date.now();
  const label = performanceLabelForReason(reason);
  if (!shouldPromptForPerformance(now, label)) return;
  lastPerformancePromptAt = now;
  addBreadcrumb("performance", reason);
  const snapshot = performanceSnapshot(reason, currentLagMs);
  if (!activeProfiler) {
    paintPerformancePrompt(buildPerformancePayload(snapshot), snapshot);
    return;
  }
  // Attribute samples to the blocked spans: every recorded long task, plus the lag
  // spike itself for event-loop reports (profiler timestamps share performance.now()'s origin).
  const windows = [...longTasks];
  if (currentLagMs > 0) {
    const nowMs = performance.now();
    windows.push({ startMs: Math.max(0, nowMs - currentLagMs), durationMs: currentLagMs });
  }
  void collectLongTaskFrames(windows).then((frames) => {
    if (frames.length) snapshot.longTaskFrames = frames;
    paintPerformancePrompt(buildPerformancePayload(snapshot), snapshot);
  });
}

function maybePromptForHeapPressure(): void {
  const heap = readHeapSnapshot();
  if (!heap?.usagePercent) return;
  if (heap.usedMb >= 512 && heap.usagePercent >= 85) {
    promptPerformanceReport(`js heap ${fmtNumber(heap.usagePercent)}% of limit`);
  }
}

export function installPerformancePressureMonitor() {
  if (performanceMonitorInstalled || typeof window === "undefined" || typeof performance === "undefined") return;
  if (!window.runtime) return;
  performanceMonitorInstalled = true;
  const startedAt = performance.now();
  const graceUntil = startedAt + STARTUP_GRACE_MS;
  const isHidden = () => typeof document !== "undefined" && document.visibilityState === "hidden";
  const isFocused = () => typeof document === "undefined" || document.hasFocus?.() !== false;
  let visibleSince = isHidden() ? Number.POSITIVE_INFINITY : startedAt;
  let expected = performance.now() + 1000;
  let eventLoopLagPrimed = false;

  const pastGrace = () => performance.now() >= graceUntil;
  const inspectLongTasks = () => {
    if (!pastGrace()) return;
    const summary = longTaskSummary();
    if (!summary) return;
    if (shouldPromptForLongTasks(summary)) {
      promptPerformanceReport(`long task ${fmtNumber(summary.maxMs)}ms`);
    }
  };

  startLongTaskProfiler();

  if (typeof document !== "undefined") {
    document.addEventListener("visibilitychange", () => {
      longTasks.length = 0;
      lagSamples.length = 0;
      expected = performance.now() + 1000;
      eventLoopLagPrimed = false;
      if (!isHidden()) visibleSince = performance.now();
    });
  }

  if (typeof PerformanceObserver !== "undefined") {
    try {
      const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          if (!shouldRecordLongTaskSample(entry.startTime, entry.duration, graceUntil, isHidden(), visibleSince, isFocused())) continue;
          const attribution = formatLongTaskAttribution(
            entry.name,
            (entry as PerformanceEntry & { attribution?: TaskAttributionLike[] }).attribution,
          );
          longTasks.push({
            startMs: Math.round(entry.startTime),
            durationMs: Math.round(entry.duration),
            ...(attribution ? { attribution } : {}),
          });
        }
        pruneLongTasks();
        inspectLongTasks();
      });
      observer.observe({ entryTypes: ["longtask"] });
    } catch {
      // Some WebViews expose PerformanceObserver without the longtask entry type.
    }
  }

  window.setInterval(() => {
    const now = performance.now();
    if (!pastGrace()) {
      expected = now + 1000;
      return;
    }
    if (!eventLoopLagPrimed) {
      expected = now + 1000;
      eventLoopLagPrimed = true;
      return;
    }
    const lagMs = Math.max(0, now - expected);
    expected = now + 1000;
    if (!shouldRecordEventLoopLagSample(isHidden(), now - visibleSince, isFocused())) return;
    lagSamples.push(lagMs);
    if (lagSamples.length > MAX_LAG_SAMPLES) lagSamples.shift();
    if (lagMs >= EVENT_LOOP_LAG_PROMPT_MS) promptPerformanceReport(`event loop lag ${fmtNumber(lagMs)}ms`, lagMs);
    maybePromptForHeapPressure();
  }, 1000);
}

export function installGlobalCrashHandlers() {
  window.addEventListener("error", (e) => {
    if (shouldReportGlobalCrashEvent(e)) reportCrash("window.error", globalCrashReportReason(e));
  });
  window.addEventListener("unhandledrejection", (e) => {
    if (shouldReportGlobalCrashEvent(e)) reportCrash("unhandledrejection", e.reason);
  });
}

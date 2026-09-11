// Last-resort crash surface: a React render error with no boundary unmounts the
// whole tree (blank window), and global errors/rejections leave no trace either.

import { addBreadcrumb, dumpBreadcrumbs, snapshotBreadcrumbs, type Breadcrumb } from "./breadcrumbs";
import { writeClipboardText } from "./clipboard";
import { desktopHost } from "./desktopHost";
import { fmtNumber, formatPerformanceContext } from "./performanceReportFormat";
export { formatPerformanceContext } from "./performanceReportFormat";
import { boundedDiagnostics, type ProcessDiagnosticsSnapshot, type RendererProfileResult } from "./processDiagnostics";
import { t } from "./i18n";
import { sessionPipelineDiagnostics, type SessionPipelineDiagnostics } from "./sessionDiagnostics";
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
  cpuProfile?: RendererProfileResult;
  processes?: ProcessDiagnosticsSnapshot;
  connection?: {
    effectiveType?: string;
    downlinkMbps?: number;
    rttMs?: number;
    saveData?: boolean;
  };
  // Session-switch/history pipeline diagnostics (Phase F): last activation
  // timings, last HistorySlice page stats with index hit/miss, virtual mounted
  // rows, markdown worker counters, transcript cache weights. All optional —
  // absent before the first switch/page or when a provider never registered.
  sessionPipeline?: SessionPipelineDiagnostics;
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
  // Optional, non-display grouping context for otherwise opaque WebView errors.
  // It is deliberately restricted to build/view/breadcrumb categories and never
  // contains breadcrumb messages, tab IDs, paths, or user content.
  fingerprintHint?: string;
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
const EVENT_LOOP_LAG_CONSECUTIVE_SAMPLES = 2;
const STARTUP_GRACE_MS = 15_000;
const PROMPT_COOLDOWN_MS = 10 * 60_000;
const MAX_LAG_SAMPLES = 60;
const VISIBILITY_RESUME_GRACE_MS = 5_000;

const longTasks: LongTaskSample[] = [];
const lagSamples: number[] = [];
let performanceMonitorInstalled = false;
let lastPerformancePromptAt = 0;
let heapSnapshotInProgress = false;
let diagnosticQuietUntil = 0;
let activeCaptureId: string | undefined;

function cancelCapture(requestId = activeCaptureId): void {
  if (!requestId || activeCaptureId !== requestId) return;
  void desktopHost().native.cancelRendererProfile?.(requestId).catch(() => {});
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
  return lines.find((l) => /\b(src|assets|frontend)\b|\.tsx?:|\.jsx?:/.test(l)) ?? lines[1] ?? lines[0] ?? "";
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
  const pipeline = sessionPipelineDiagnostics();
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
    sessionPipeline: Object.keys(pipeline).length > 0 ? pipeline : undefined,
  };
}


export function performanceLabelForReason(reason: string): string {
  const normalized = reason.trim().toLowerCase();
  if (normalized.startsWith("event loop lag")) return "performance.lag";
  if (normalized.startsWith("long task")) return "performance.longtask";
  if (normalized.startsWith("js heap")) return "performance.heap";
  if (normalized.startsWith("process memory")) return "performance.memory";
  return "performance.pressure";
}

export function performanceFingerprintHintForReason(reason: string): string | undefined {
  const normalized = reason.trim().toLowerCase();
  if (!normalized.startsWith("js heap")) return undefined;
  const match = normalized.match(/(\d+(?:\.\d+)?)%/);
  const percent = match ? Number(match[1]) : Number.NaN;
  if (!Number.isFinite(percent)) return "frontend.performance.heap.unknown";
  return percent >= 95
    ? "frontend.performance.heap.critical"
    : "frontend.performance.heap.high";
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

export function shouldPromptForEventLoopLag(
  samples: readonly number[],
  longTask?: { count: number; totalMs: number; maxMs: number },
): boolean {
  const recent = samples.slice(-EVENT_LOOP_LAG_CONSECUTIVE_SAMPLES);
  const sustained =
    recent.length === EVENT_LOOP_LAG_CONSECUTIVE_SAMPLES &&
    recent.every((sample) => sample >= EVENT_LOOP_LAG_PROMPT_MS);
  const current = samples.length ? samples[samples.length - 1] : 0;
  const corroborated = current >= EVENT_LOOP_LAG_PROMPT_MS && Boolean(longTask && shouldPromptForLongTasks(longTask));
  return sustained || corroborated;
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


export function shouldRecordEventLoopLagSample(
  visibilityHidden: boolean,
  msSinceVisible: number,
  focused = true,
  msSinceFocused = msSinceVisible,
): boolean {
  if (!focused) return false;
  if (visibilityHidden) return false;
  return msSinceVisible >= VISIBILITY_RESUME_GRACE_MS && msSinceFocused >= VISIBILITY_RESUME_GRACE_MS;
}

export function buildPerformancePayload(snapshot: PerformanceSnapshot): CrashPayload {
  const buildCommit = typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev";
  const context = formatPerformanceContext(snapshot);
  const crumbs = dumpBreadcrumbs();
  const label = performanceLabelForReason(snapshot.reason);
  const errorMessage = label === "performance.memory"
    ? "App process memory remained elevated across multiple samples; this does not establish a leak."
    : "UI responsiveness degraded because the app observed long tasks, event-loop lag, or high JS heap pressure.";
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
    fingerprintHint: performanceFingerprintHintForReason(snapshot.reason),
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

export function opaqueScriptFingerprintHint(
  rawView = currentView(),
  breadcrumbs = snapshotBreadcrumbs(),
  buildCommit = currentBuildCommit(),
): string {
  const view = rawView
    .replace(/[?#].*$/, "")
    .replace(/\b[0-9a-f]{8,}\b/gi, "_")
    .replace(/\/\d+(?=\/|$)/g, "/_");
  const categories = breadcrumbs
    .slice(-8)
    .map((crumb) => crumb.cat?.trim().toLowerCase().replace(/[^a-z0-9_.-]+/g, "_") ?? "")
    .filter(Boolean)
    .join(">");
  return clip(`build:${buildCommit.slice(0, 16)}|view:${view}|cats:${categories || "none"}`, 300);
}

function sendButton(
  payload: CrashPayload | (() => CrashPayload),
  className = "crash-overlay__send",
  onSent?: () => void,
): HTMLButtonElement | null {
  // Resolved at click time through the host adapter, not the bridge module: this
  // overlay must stay usable even when the rest of the app (and its imports) is broken.
  const report = desktopHost().app?.ReportCrash;
  if (!report) return null;
  const send = document.createElement("button");
  send.className = className;
  send.textContent = t("crash.send");
  send.onclick = async () => {
    send.disabled = true;
    send.textContent = t("crash.sending");
    try {
      const current = typeof payload === "function" ? payload() : payload;
      await report(current.kind, JSON.stringify(current));
      send.textContent = t("crash.sent");
      onSent?.();
    } catch (err) {
      send.textContent = t("crash.sendFailed");
      send.title = err instanceof Error ? err.message : String(err);
      send.disabled = false;
    }
  };
  return send;
}

const COPY_FEEDBACK_MS = 2_000;

function copyButton(text: string | (() => string), className: string): HTMLButtonElement {
  const copy = document.createElement("button");
  copy.className = className;
  copy.textContent = t("crash.copy");
  copy.onclick = async () => {
    copy.disabled = true;
    let copied = false;
    // The crash overlay is the last-resort surface, so the button must re-enable
    // even if the clipboard path throws unexpectedly — a stuck disabled Copy is
    // exactly the #6388 unresponsive symptom. Catch so a rejection can't escape as
    // an unhandledrejection into the global crash handler either.
    try {
      copied = await writeClipboardText(typeof text === "function" ? text() : text);
    } catch {
      copied = false;
    } finally {
      copy.textContent = copied ? t("crash.copied") : t("crash.copyFailed");
      copy.disabled = false;
      window.setTimeout(() => {
        copy.textContent = t("crash.copy");
      }, COPY_FEEDBACK_MS);
    }
  };
  return copy;
}

let performancePromptGeneration = 0;
function paintPerformancePrompt(payload: CrashPayload, snapshot: PerformanceSnapshot, captureId?: string) {
  if (typeof document === "undefined") return;
  const generation = ++performancePromptGeneration;
  let currentPayload = payload;
  let host = document.getElementById("performance-report-prompt");
  if (!host) {
    host = document.createElement("div");
    host.id = "performance-report-prompt";
    document.body.appendChild(host);
  }
  const title = document.createElement("div");
  title.className = "performance-report__title";
  title.textContent = t(payload.label === "performance.memory" ? "performanceReport.memoryTitle" : "performanceReport.title");
  const body = document.createElement("pre");
  body.className = "performance-report__body";
  body.textContent = formatPerformanceContext(snapshot);
  const actions = document.createElement("div");
  actions.className = "performance-report__actions";
  const send = sendButton(() => currentPayload, "performance-report__send", () => markPerfReported(payload.label));
  const copy = copyButton(() => currentPayload.message, "performance-report__copy");
  const dismiss = document.createElement("button");
  dismiss.className = "performance-report__dismiss";
  dismiss.textContent = t("performanceReport.dismiss");
  dismiss.onclick = () => {
    if (generation !== performancePromptGeneration) return;
    dismissedPerfLabels.add(payload.label);
    performancePromptGeneration++;
    if (captureId) cancelCapture(captureId);
    host?.remove();
  };
  if (send) actions.append(send);
  actions.append(copy, dismiss);
  const exportHeap = desktopHost().native.exportHeapSnapshot;
  if (exportHeap) {
    const heap = document.createElement("button");
    heap.className = "performance-report__copy";
    heap.textContent = t("performanceReport.saveHeap");
    heap.onclick = async () => {
      if (generation !== performancePromptGeneration || !host?.isConnected) return;
      heap.disabled = true;
      heapSnapshotInProgress = true;
      try {
        const result = await exportHeap();
        heap.textContent = result.status === "saved" ? t("performanceReport.heapSaved") : result.status === "busy" ? t("performanceReport.diagnosticBusy") : result.status === "failed" ? t("performanceReport.heapFailed") : t("performanceReport.saveHeap");
      } catch { heap.textContent = t("performanceReport.heapFailed"); }
      finally {
        heap.disabled = false;
        heapSnapshotInProgress = false;
        diagnosticQuietUntil = performance.now() + VISIBILITY_RESUME_GRACE_MS;
      }
    };
    actions.append(heap);
  }
  const note = document.createElement("div");
  note.className = "performance-report__note";
  note.textContent = t("performanceReport.privacyNote");
  host.replaceChildren(title, body, actions, note);
  return () => {
    if (generation !== performancePromptGeneration || !host?.isConnected) return;
    currentPayload = buildPerformancePayload(snapshot);
    body.textContent = formatPerformanceContext(snapshot);
  };
}

export function paintCrashOverlay(payload: CrashPayload) {
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
  const copy = copyButton(payload.message, "crash-overlay__copy");
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
  paintCrashOverlay(buildCrashPayload(label, err, extra));
}

type GlobalCrashEventLike = Pick<Event, "defaultPrevented"> & {
  message?: unknown;
  error?: unknown;
  reason?: unknown;
  filename?: unknown;
  lineno?: unknown;
  colno?: unknown;
};

const RESIZE_OBSERVER_LOOP_MESSAGE_RE = /^ResizeObserver loop (?:limit exceeded|completed with undelivered notifications\.?)$/;
const OPAQUE_SCRIPT_ERROR_MESSAGE = "Script error.";
function globalCrashEventMessages(e: GlobalCrashEventLike): string[] {
  const messages: string[] = [];
  const pushMessage = (message: string) => {
    const trimmed = message.trim();
    if (trimmed) messages.push(trimmed);
  };
  if (typeof e.message === "string") pushMessage(e.message);
  const error = e.error ?? e.reason;
  if (typeof error === "string") pushMessage(error);
  if (error && typeof error === "object" && "message" in error) {
    const msg = (error as { message?: unknown }).message;
    if (typeof msg === "string") pushMessage(msg);
  }
  return messages;
}

export function shouldReportGlobalCrashEvent(e: GlobalCrashEventLike): boolean {
  if (e.defaultPrevented) return false;
  if (globalCrashEventMessages(e).some((message) => RESIZE_OBSERVER_LOOP_MESSAGE_RE.test(message) ||
    /Minified React error #520\b/.test(message) || message.includes("status was superseded by"))) return false;
  return true;
}

export function isOpaqueScriptErrorEvent(e: GlobalCrashEventLike): boolean {
  return (
    (e.error === undefined || e.error === null) &&
    typeof e.message === "string" &&
    e.message.trim() === OPAQUE_SCRIPT_ERROR_MESSAGE &&
    globalScriptErrorLocation(e) === ""
  );
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

function promptPerformanceReport(reason: string, currentLagMs = 0, processes?: ProcessDiagnosticsSnapshot): void {
  if (heapSnapshotInProgress || performance.now() < diagnosticQuietUntil) return;
  const now = Date.now();
  const label = performanceLabelForReason(reason);
  if (!shouldPromptForPerformance(now, label)) return;
  lastPerformancePromptAt = now;
  addBreadcrumb("performance", reason);
  const snapshot = performanceSnapshot(reason, currentLagMs);
  snapshot.processes = processes;
  const native = desktopHost().native;
  const capture = label === "performance.longtask" || label === "performance.lag";
  const requestId = capture && native.captureRendererProfile
    ? globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}` : undefined;
  if (requestId) activeCaptureId = requestId;
  if (capture) snapshot.cpuProfile = { status: native.captureRendererProfile ? "recording" : "unavailable" };
  // Show the original evidence immediately. Slow diagnostics only enrich this
  // same prompt; they cannot recreate a dismissed/replaced report.
  const update = paintPerformancePrompt(buildPerformancePayload(snapshot), snapshot, requestId);
  if (!processes && native.processDiagnostics) {
    void boundedDiagnostics(() => native.processDiagnostics!()).then((sample) => {
      if (sample) { snapshot.processes = sample; update?.(); }
    });
  }
  if (requestId && native.captureRendererProfile) {
    void boundedDiagnostics(() => native.captureRendererProfile!(requestId), 12_000).then((result) => {
      snapshot.cpuProfile = result ?? { status: "failed" };
      if (!result) cancelCapture(requestId);
      if (activeCaptureId === requestId) activeCaptureId = undefined;
      update?.();
    });
  }
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
  if (desktopHost().kind === "none") return;
  performanceMonitorInstalled = true;
  const startedAt = performance.now();
  const graceUntil = startedAt + STARTUP_GRACE_MS;
  const isHidden = () => typeof document !== "undefined" && document.visibilityState === "hidden";
  const isFocused = () => typeof document === "undefined" || document.hasFocus?.() !== false;
  let visibleSince = isHidden() ? Number.POSITIVE_INFINITY : startedAt;
  let focusedSince = isFocused() ? startedAt : Number.POSITIVE_INFINITY;
  let expected = performance.now() + 1000;
  let eventLoopLagPrimed = false;
  // When the view is shown or focused again, overdue timer callbacks can run before
  // the queued visibilitychange/focus task, so visibleSince/focusedSince may still
  // describe the previous settled period at that point. The sampler tracks hidden and
  // unfocused observations itself and restarts both windows on the first settled tick
  // instead of trusting the listener-maintained timestamps.
  let pendingResume = isHidden() || !isFocused();

  const pastGrace = () => performance.now() >= graceUntil;
  const inspectLongTasks = () => {
    if (!pastGrace()) return;
    const summary = longTaskSummary();
    if (!summary) return;
    if (shouldPromptForLongTasks(summary)) {
      promptPerformanceReport(`long task ${fmtNumber(summary.maxMs)}ms`);
    }
  };

  // Blur/hide park the timestamps at +Infinity so a stale read before the matching
  // resume listener has run can never satisfy the grace windows.
  const resetSamples = () => {
    const now = performance.now();
    longTasks.length = 0;
    lagSamples.length = 0;
    expected = now + 1000;
    eventLoopLagPrimed = false;
    visibleSince = isHidden() ? Number.POSITIVE_INFINITY : now;
    focusedSince = isFocused() ? now : Number.POSITIVE_INFINITY;
    pendingResume = isHidden() || !isFocused();
    if (pendingResume) cancelCapture();
  };

  if (typeof document !== "undefined") {
    document.addEventListener("visibilitychange", resetSamples);
  }
  window.addEventListener("focus", resetSamples);
  window.addEventListener("blur", resetSamples);

  if (typeof PerformanceObserver !== "undefined") {
    try {
      const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          if (heapSnapshotInProgress || entry.startTime < diagnosticQuietUntil) continue;
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

  let processSampleAt = performance.now();
  let processSamplePending = false;
  window.setInterval(() => {
    const now = performance.now();
    if (heapSnapshotInProgress || now < diagnosticQuietUntil) {
      longTasks.length = 0;
      lagSamples.length = 0;
      expected = now + 1000;
      eventLoopLagPrimed = false;
      return;
    }
    if (isHidden() || !isFocused()) {
      pendingResume = true;
    } else if (pendingResume) {
      pendingResume = false;
      visibleSince = now;
      focusedSince = now;
      longTasks.length = 0;
      lagSamples.length = 0;
      expected = now + 1000;
      eventLoopLagPrimed = false;
      return;
    }
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
    if (!shouldRecordEventLoopLagSample(isHidden(), now - visibleSince, isFocused(), now - focusedSince)) return;
    lagSamples.push(lagMs);
    if (lagSamples.length > MAX_LAG_SAMPLES) lagSamples.shift();
    if (shouldPromptForEventLoopLag(lagSamples, longTaskSummary(now))) {
      promptPerformanceReport(`event loop lag ${fmtNumber(lagMs)}ms`, lagMs);
    }
    maybePromptForHeapPressure();
    const readProcesses = desktopHost().native.processDiagnostics;
    if (readProcesses && !processSamplePending && now - processSampleAt >= 30_000) {
      processSampleAt = now;
      processSamplePending = true;
      void boundedDiagnostics(readProcesses).then((sample) => {
        if (sample?.growth?.length && !isHidden() && isFocused()) promptPerformanceReport("process memory growth", 0, sample);
      }).finally(() => { processSamplePending = false; });
    }
  }, 1000);
}

// sessionDiagnostics — content-free counters and last-event timings for the
// session-switch/history pipeline (Phase F of the session-switch/history
// refactor). Everything recorded here is bounded metadata (ids, phase
// durations, byte/entry counts, closed-class labels) — never message text —
// matching the crash/metrics privacy posture. crash.ts folds a snapshot into
// the performance-report context; the bench harness reads the same state via
// the window.__reasonixPerf hook (see installPerfDebugHook).
//
// This module must stay dependency-free: it sits at the bottom of the import
// graph so crash.ts (eager) and the lazy markdown-worker chunk can both use
// it without pulling each other in. Heavy/live state (transcript store cache
// weights, markdown worker counters) reaches it through registered providers.

export type ActivationOutcome = "ready" | "failed" | "cancelled";

export interface ActivationDiagnostic {
  requestId: string;
  tabId: string;
  /** performance.now() clock; durations are differences on the same clock. */
  requestedAtMs: number;
  startedAtMs?: number;
  settledAtMs?: number;
  outcome?: ActivationOutcome;
  failureClass?: string;
}

export interface HistoryPageDiagnostic {
  entries: number;
  inlineBytes: number;
  durationMs: number;
  stale: boolean;
  source: string; // index|scan|live-index|live-fallback|"" (unknown)
}

export interface MarkdownWorkerDiagnostic {
  pending: number;
  completed: number;
  avgParseMs: number;
  maxParseMs: number;
  fallbackActive: boolean;
  workerFailures: number;
}

export interface TranscriptCacheDiagnostic {
  residentSessions: number;
  maxResidentSessions: number;
  bodyBytes: number;
  bodyBudgetBytes: number;
  markdownBytes: number;
  markdownBudgetBytes: number;
  historyEvictions: number;
  markdownEvictions: number;
}

export interface MountedRowsDiagnostic {
  mounted: number;
  total: number;
}

// activationFailureClass maps an activation error onto a closed label set so
// reports never carry the error text itself (which can echo session state).
export function activationFailureClass(error: string | undefined): string {
  const low = (error ?? "").trim().toLowerCase();
  if (!low) return "unknown";
  if (low.includes("timeout") || low.includes("deadline")) return "timeout";
  if (low.includes("cancel")) return "cancelled";
  if (low.includes("stale") || low.includes("superseded")) return "stale";
  if (low.includes("not found") || low.includes("no such") || low.includes("missing")) return "missing";
  return "other";
}

const MAX_ACTIVATION_LOG = 128;

const activations = new Map<string, ActivationDiagnostic>();
const activationOrder: string[] = [];
let lastActivationKey: string | null = null;

let lastHistoryPage: HistoryPageDiagnostic | null = null;
let historyPages = 0;
let historyStalePages = 0;
let historyIndexHits = 0;
let historyIndexMisses = 0;

let mountedRows: MountedRowsDiagnostic = { mounted: 0, total: 0 };

type MarkdownWorkerProvider = () => MarkdownWorkerDiagnostic;
type TranscriptCacheProvider = () => TranscriptCacheDiagnostic;

let markdownWorkerProvider: MarkdownWorkerProvider | null = null;
let transcriptCacheProvider: TranscriptCacheProvider | null = null;

function now(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

function trimActivationLog(): void {
  while (activationOrder.length > MAX_ACTIVATION_LOG) {
    const oldest = activationOrder.shift();
    if (oldest) activations.delete(oldest);
  }
}

/** A new ticketed activation (activateTopic) or tab switch was requested. */
export function noteActivationRequested(requestId: string): void {
  if (!requestId || activations.has(requestId)) return;
  activations.set(requestId, { requestId, tabId: "", requestedAtMs: now() });
  activationOrder.push(requestId);
  lastActivationKey = requestId;
  trimActivationLog();
}

/** The backend echoed a different requestId than the provisional one: keep
 *  the original request timestamp under the canonical id. */
export function aliasActivationRequest(fromId: string, toId: string): void {
  if (!fromId || !toId || fromId === toId) return;
  const entry = activations.get(fromId);
  if (!entry || activations.has(toId)) return;
  activations.delete(fromId);
  entry.requestId = toId;
  activations.set(toId, entry);
  const index = activationOrder.indexOf(fromId);
  if (index >= 0) activationOrder[index] = toId;
  if (lastActivationKey === fromId) lastActivationKey = toId;
}

/** The activation's "starting" phase was observed (or the switch applied). */
export function noteActivationStarted(requestId: string, tabId: string): void {
  const entry = activations.get(requestId);
  if (!entry || entry.startedAtMs !== undefined) return;
  entry.startedAtMs = now();
  if (tabId) entry.tabId = tabId;
}

/** The activation reached a terminal phase (ready / failed / cancelled). */
export function noteActivationSettled(requestId: string, outcome: ActivationOutcome, error?: string): void {
  const entry = activations.get(requestId);
  if (!entry || entry.outcome) return;
  entry.settledAtMs = now();
  entry.outcome = outcome;
  if (outcome === "failed") entry.failureClass = activationFailureClass(error);
}

/** One HistorySliceForTab page response (stale retries recorded too). */
export function noteHistoryPage(page: HistoryPageDiagnostic): void {
  historyPages += 1;
  if (page.stale) historyStalePages += 1;
  if (page.source === "index" || page.source === "live-index") historyIndexHits += 1;
  else if (page.source === "scan" || page.source === "live-fallback") historyIndexMisses += 1;
  lastHistoryPage = page;
}

/** Current virtual-mounted vs total transcript row counts (Transcript.tsx). */
export function noteTranscriptRowCounts(mounted: number, total: number): void {
  mountedRows = { mounted, total };
}

/** Registered by the lazy markdown-worker chunk at module load. */
export function registerMarkdownWorkerDiagnostics(provider: MarkdownWorkerProvider): void {
  markdownWorkerProvider = provider;
}

/** Registered by transcriptStore at module load. */
export function registerTranscriptCacheDiagnostics(provider: TranscriptCacheProvider): void {
  transcriptCacheProvider = provider;
}

export interface SessionPipelineDiagnostics {
  activation?: ActivationDiagnostic & {
    ticketToStartingMs?: number;
    startingToReadyMs?: number;
    totalMs?: number;
  };
  history?: HistoryPageDiagnostic & {
    pages: number;
    staleCount: number;
    indexHits: number;
    indexMisses: number;
  };
  mountedRows?: MountedRowsDiagnostic;
  markdownWorker?: MarkdownWorkerDiagnostic;
  transcriptCache?: TranscriptCacheDiagnostic;
}

function deriveActivation(entry: ActivationDiagnostic): SessionPipelineDiagnostics["activation"] {
  const out: SessionPipelineDiagnostics["activation"] = { ...entry };
  if (entry.startedAtMs !== undefined) out.ticketToStartingMs = entry.startedAtMs - entry.requestedAtMs;
  if (entry.settledAtMs !== undefined) {
    out.totalMs = entry.settledAtMs - entry.requestedAtMs;
    if (entry.outcome === "ready" && entry.startedAtMs !== undefined) {
      out.startingToReadyMs = entry.settledAtMs - entry.startedAtMs;
    }
  }
  return out;
}

/** Point-in-time snapshot for the crash/performance report context. */
export function sessionPipelineDiagnostics(): SessionPipelineDiagnostics {
  const out: SessionPipelineDiagnostics = {};
  const activation = lastActivationKey ? activations.get(lastActivationKey) : undefined;
  if (activation) out.activation = deriveActivation(activation);
  if (lastHistoryPage) {
    out.history = {
      ...lastHistoryPage,
      pages: historyPages,
      staleCount: historyStalePages,
      indexHits: historyIndexHits,
      indexMisses: historyIndexMisses,
    };
  }
  if (mountedRows.mounted > 0 || mountedRows.total > 0) out.mountedRows = { ...mountedRows };
  if (markdownWorkerProvider) {
    try {
      out.markdownWorker = markdownWorkerProvider();
    } catch {
      // A broken provider must never break crash reporting.
    }
  }
  if (transcriptCacheProvider) {
    try {
      out.transcriptCache = transcriptCacheProvider();
    } catch {
      // Same: diagnostics are best-effort.
    }
  }
  return out;
}

/** Recent activation records, oldest first (bench harness introspection). */
export function activationLog(): ActivationDiagnostic[] {
  const out: ActivationDiagnostic[] = [];
  for (const requestId of activationOrder) {
    const entry = activations.get(requestId);
    if (entry) out.push({ ...entry });
  }
  return out;
}

/** Test/bench reset. */
export function resetSessionDiagnostics(): void {
  activations.clear();
  activationOrder.length = 0;
  lastActivationKey = null;
  lastHistoryPage = null;
  historyPages = 0;
  historyStalePages = 0;
  historyIndexHits = 0;
  historyIndexMisses = 0;
  mountedRows = { mounted: 0, total: 0 };
}

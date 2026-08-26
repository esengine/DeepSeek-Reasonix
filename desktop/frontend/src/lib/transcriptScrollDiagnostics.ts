import { setTranscriptScrollDiagnosticSink } from "./transcriptScrollProbe";
import { getProcessFoldPreference } from "./processFoldPreference";
import { getReasoningDisplayMode } from "./reasoningDisplayPreference";

export const TRANSCRIPT_SCROLL_DIAGNOSTIC_SCHEMA_VERSION = 2;
export const MAX_TRANSCRIPT_SCROLL_DIAGNOSTIC_EVENTS = 4_096;
export const TRANSCRIPT_SCROLL_DIAGNOSTIC_DURATION_MS = 90_000;

export type TranscriptScrollDiagnosticStatus = "idle" | "recording" | "stopped";

export type TranscriptScrollDiagnosticEventType =
  | "start"
  | "stop"
  | "mark"
  | "sample"
  | "wheel"
  | "scroll"
  | "scroll-write"
  | "items-rendered"
  | "list-height"
  | "row-measure"
  | "geometry-contract-violation"
  | "scroll-state"
  | "blank-check"
  | "blank-reset"
  | "recovery";

export type TranscriptScrollDiagnosticEvent = {
  t: number;
  type: TranscriptScrollDiagnosticEventType;
  scrollTop?: number;
  scrollHeight?: number;
  clientHeight?: number;
  bottomDistance?: number;
  mountedRows?: number;
  totalRows?: number;
  firstVisibleIndex?: number;
  firstVisibleTop?: number;
  deltaY?: number;
  targetTop?: number;
  targetIndex?: number | "LAST";
  listHeight?: number;
  rowIndex?: number;
  estimatedSize?: number;
  previousSize?: number;
  measuredSize?: number;
  sizeDelta?: number;
  contentRevision?: number;
  disclosureCount?: number;
  settleFrame?: number;
  offBottomFrames?: number;
  stagnantFrames?: number;
  mode?: "tail-follow" | "manual" | "user-resize" | "selection" | "restoring" | "unknown";
  previousMode?: "tail-follow" | "manual" | "user-resize" | "selection" | "restoring" | "unknown";
  owner?: "tail-follow" | "jump" | "rewind" | "jump-bottom" | "custom-scrollbar" | "selection-edge-scroll" | "recovery" | "other";
  writeKind?: "scrollTo" | "scrollBy" | "scrollToIndex";
  source?: "reset" | "user-scroll-intent" | "manual-reading" | "reader-intent-ended" | "scroll-delivered"
    | "tail-content-changed" | "content-shrank" | "layout-height-changed" | "viewport-resized"
    | "user-resize-begin" | "user-resize-end" | "selection-begin" | "selection-end"
    | "programmatic-begin" | "programmatic-end" | "jump-bottom" | "jump-index" | "scroll-offset"
    | "recovery-begin" | "recovery-end" | "native-scrollbar-release" | "other";
  phase?: "initial" | "settle";
  rowKind?: "older-history" | "user" | "process-header" | "reasoning" | "tool" | "tool-batch"
    | "tool-group" | "phase" | "process-notice" | "notice" | "compaction" | "answer" | "extension"
    | "turn-actions";
  layoutVariant?: "reasoning-summary" | "reasoning-heading-only" | "reasoning-expanded"
    | "tool-collapsed" | "tool-expanded" | "tool-batch-collapsed" | "tool-batch-expanded"
    | "tool-group-collapsed" | "tool-group-expanded" | "compaction-collapsed"
    | "compaction-expanded" | "static" | "text-flow";
  estimateSource?: "exact" | "calibrated" | "static";
  relativeError?: number;
  foldState?: "none" | "open" | "closed" | "mixed";
  state?: "begin" | "suspend" | "retry" | "done" | "cancelled" | "expired";
  reason?: "user-takeover" | "surface-switch" | "superseded" | "viewport-blank" | "other";
  atBottom?: boolean;
  scrollable?: boolean;
  blank?: boolean;
  readerIntent?: boolean;
  canClaimTail?: boolean;
  substantial?: boolean;
  tailCommand?: boolean;
};

export type TranscriptScrollDiagnosticEnvironment = {
  buildCommit: string;
  buildChannel: string;
  platform: "windows" | "macos" | "linux" | "other";
  userAgent: string;
  devicePixelRatio: number;
  viewportWidth: number;
  viewportHeight: number;
  reducedMotion: boolean;
  transcriptWidth: number;
  contentWidth: number;
  fontSize: number;
  lineHeight: number;
  processFoldPreference: "auto" | "expanded";
  reasoningDisplayMode: "hidden" | "summary" | "auto" | "expanded" | "legacy-collapsed" | "pending";
};

export type TranscriptScrollDiagnosticPayload = {
  schemaVersion: typeof TRANSCRIPT_SCROLL_DIAGNOSTIC_SCHEMA_VERSION;
  manifest: TranscriptScrollDiagnosticEnvironment & {
    reportId: string;
    createdAt: string;
  };
  summary: {
    durationMs: number;
    eventCount: number;
    droppedEventCount: number;
    markerCount: number;
  };
  events: TranscriptScrollDiagnosticEvent[];
};

export type TranscriptScrollDiagnosticSnapshot = {
  status: TranscriptScrollDiagnosticStatus;
  durationMs: number;
  eventCount: number;
  droppedEventCount: number;
  markerCount: number;
  reportId: string;
};

type EventFields = Record<string, unknown>;
type Sample = EventFields | null;
type Listener = () => void;

type RecorderOptions = {
  maxEvents?: number;
  maxDurationMs?: number;
  now?: () => number;
  randomID?: () => string;
  environment?: () => TranscriptScrollDiagnosticEnvironment;
};

const EVENT_TYPES = new Set<TranscriptScrollDiagnosticEventType>([
  "start", "stop", "mark", "sample", "wheel", "scroll", "scroll-write",
  "items-rendered", "list-height", "row-measure", "geometry-contract-violation", "scroll-state", "blank-check", "blank-reset", "recovery",
]);
const MODES = new Set(["tail-follow", "manual", "user-resize", "selection", "restoring", "unknown"]);
const OWNERS = new Set(["tail-follow", "jump", "rewind", "jump-bottom", "custom-scrollbar", "selection-edge-scroll", "recovery", "other"]);
const WRITE_KINDS = new Set(["scrollTo", "scrollBy", "scrollToIndex"]);
const SOURCES = new Set([
  "reset", "user-scroll-intent", "manual-reading", "reader-intent-ended", "scroll-delivered",
  "tail-content-changed", "content-shrank", "layout-height-changed", "viewport-resized",
  "user-resize-begin", "user-resize-end", "selection-begin", "selection-end",
  "programmatic-begin", "programmatic-end", "jump-bottom", "jump-index", "scroll-offset",
  "recovery-begin", "recovery-end", "native-scrollbar-release", "other",
]);
const PHASES = new Set(["initial", "settle"]);
const ROW_KINDS = new Set([
  "older-history", "user", "process-header", "reasoning", "tool", "tool-batch", "tool-group", "phase",
  "process-notice", "notice", "compaction", "answer", "extension", "turn-actions",
]);
const LAYOUT_VARIANTS = new Set([
  "reasoning-summary", "reasoning-heading-only", "reasoning-expanded",
  "tool-collapsed", "tool-expanded", "tool-batch-collapsed", "tool-batch-expanded",
  "tool-group-collapsed", "tool-group-expanded", "compaction-collapsed", "compaction-expanded",
  "static", "text-flow",
]);
const ESTIMATE_SOURCES = new Set(["exact", "calibrated", "static"]);
const FOLD_STATES = new Set(["none", "open", "closed", "mixed"]);
const STATES = new Set(["begin", "suspend", "retry", "done", "cancelled", "expired"]);
const REASONS = new Set(["user-takeover", "surface-switch", "superseded", "viewport-blank", "other"]);
const NUMBER_FIELDS = [
  "scrollTop", "scrollHeight", "clientHeight", "bottomDistance", "mountedRows", "totalRows",
  "firstVisibleIndex", "firstVisibleTop", "deltaY", "targetTop", "listHeight", "rowIndex",
  "estimatedSize", "previousSize", "measuredSize", "sizeDelta", "contentRevision", "disclosureCount",
  "settleFrame", "offBottomFrames", "stagnantFrames", "relativeError",
] as const;
const BOOLEAN_FIELDS = ["atBottom", "scrollable", "blank", "readerIntent", "canClaimTail", "substantial", "tailCommand"] as const;

function finiteNumber(value: unknown): number | undefined {
  if (typeof value !== "number" || !Number.isFinite(value)) return undefined;
  return Math.round(value * 100) / 100;
}

function sanitizeEvent(
  t: number,
  type: TranscriptScrollDiagnosticEventType,
  input: EventFields = {},
): TranscriptScrollDiagnosticEvent {
  const event: TranscriptScrollDiagnosticEvent = { t: Math.max(0, Math.round(t)), type };
  const target = event as Record<string, unknown>;
  for (const field of NUMBER_FIELDS) {
    const value = finiteNumber(input[field]);
    if (value !== undefined) target[field] = value;
  }
  for (const field of BOOLEAN_FIELDS) {
    if (typeof input[field] === "boolean") target[field] = input[field];
  }
  if (typeof input.targetIndex === "number" && Number.isFinite(input.targetIndex)) {
    event.targetIndex = Math.max(0, Math.round(input.targetIndex));
  } else if (input.targetIndex === "LAST") {
    event.targetIndex = "LAST";
  }
  if (typeof input.mode === "string" && MODES.has(input.mode)) event.mode = input.mode as TranscriptScrollDiagnosticEvent["mode"];
  if (typeof input.previousMode === "string" && MODES.has(input.previousMode)) event.previousMode = input.previousMode as TranscriptScrollDiagnosticEvent["previousMode"];
  if (typeof input.owner === "string") event.owner = (OWNERS.has(input.owner) ? input.owner : "other") as TranscriptScrollDiagnosticEvent["owner"];
  if (typeof input.writeKind === "string" && WRITE_KINDS.has(input.writeKind)) event.writeKind = input.writeKind as TranscriptScrollDiagnosticEvent["writeKind"];
  if (typeof input.source === "string") event.source = (SOURCES.has(input.source) ? input.source : "other") as TranscriptScrollDiagnosticEvent["source"];
  if (typeof input.phase === "string" && PHASES.has(input.phase)) event.phase = input.phase as TranscriptScrollDiagnosticEvent["phase"];
  if (typeof input.rowKind === "string" && ROW_KINDS.has(input.rowKind)) event.rowKind = input.rowKind as TranscriptScrollDiagnosticEvent["rowKind"];
  if (typeof input.layoutVariant === "string" && LAYOUT_VARIANTS.has(input.layoutVariant)) {
    event.layoutVariant = input.layoutVariant as TranscriptScrollDiagnosticEvent["layoutVariant"];
  }
  if (typeof input.estimateSource === "string" && ESTIMATE_SOURCES.has(input.estimateSource)) {
    event.estimateSource = input.estimateSource as TranscriptScrollDiagnosticEvent["estimateSource"];
  }
  if (typeof input.foldState === "string" && FOLD_STATES.has(input.foldState)) event.foldState = input.foldState as TranscriptScrollDiagnosticEvent["foldState"];
  if (typeof input.state === "string" && STATES.has(input.state)) event.state = input.state as TranscriptScrollDiagnosticEvent["state"];
  if (typeof input.reason === "string") event.reason = (REASONS.has(input.reason) ? input.reason : "other") as TranscriptScrollDiagnosticEvent["reason"];
  return event;
}

function defaultRandomID(): string {
  const bytes = new Uint8Array(16);
  if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
    crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i += 1) bytes[i] = Math.floor(Math.random() * 256);
  }
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function normalizedPlatform(): TranscriptScrollDiagnosticEnvironment["platform"] {
  if (typeof navigator === "undefined") return "other";
  const source = `${navigator.userAgent} ${navigator.platform}`.toLowerCase();
  if (source.includes("win")) return "windows";
  if (source.includes("mac")) return "macos";
  if (source.includes("linux")) return "linux";
  return "other";
}

function defaultEnvironment(): TranscriptScrollDiagnosticEnvironment {
  const transcript = typeof document === "undefined" ? null : document.querySelector<HTMLElement>(".transcript");
  const style = transcript ? getComputedStyle(transcript) : null;
  const rootStyle = typeof document === "undefined" ? null : getComputedStyle(document.documentElement);
  const transcriptWidth = transcript?.clientWidth ?? 0;
  const inlinePadding = Number.parseFloat(style?.getPropertyValue("--transcript-inline-pad") ?? "") || 0;
  const maxContentWidth = Number.parseFloat(rootStyle?.getPropertyValue("--maxw") ?? "") || transcriptWidth;
  const contentWidth = Math.max(0, Math.min(maxContentWidth, transcriptWidth - inlinePadding * 2));
  return {
    buildCommit: typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev",
    buildChannel: typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "development",
    platform: normalizedPlatform(),
    userAgent: typeof navigator === "undefined" ? "unknown" : navigator.userAgent.slice(0, 512),
    devicePixelRatio: finiteNumber(globalThis.devicePixelRatio) ?? 1,
    viewportWidth: typeof window === "undefined" ? 0 : Math.max(0, Math.round(window.innerWidth)),
    viewportHeight: typeof window === "undefined" ? 0 : Math.max(0, Math.round(window.innerHeight)),
    reducedMotion: typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true,
    transcriptWidth: finiteNumber(transcriptWidth) ?? 0,
    contentWidth: finiteNumber(contentWidth) ?? 0,
    fontSize: finiteNumber(Number.parseFloat(style?.fontSize ?? "")) ?? 0,
    lineHeight: finiteNumber(Number.parseFloat(style?.lineHeight ?? "")) ?? 0,
    processFoldPreference: getProcessFoldPreference(),
    reasoningDisplayMode: getReasoningDisplayMode(),
  };
}

export function createTranscriptScrollDiagnostics(options: RecorderOptions = {}) {
  const maxEvents = Math.max(4, Math.min(MAX_TRANSCRIPT_SCROLL_DIAGNOSTIC_EVENTS, Math.round(options.maxEvents ?? MAX_TRANSCRIPT_SCROLL_DIAGNOSTIC_EVENTS)));
  const maxDurationMs = Math.max(1_000, Math.min(TRANSCRIPT_SCROLL_DIAGNOSTIC_DURATION_MS, Math.round(options.maxDurationMs ?? TRANSCRIPT_SCROLL_DIAGNOSTIC_DURATION_MS)));
  const now = options.now ?? (() => performance.now());
  const randomID = options.randomID ?? defaultRandomID;
  const environment = options.environment ?? defaultEnvironment;
  const listeners = new Set<Listener>();

  let status: TranscriptScrollDiagnosticStatus = "idle";
  let startedAt = 0;
  let stoppedAt = 0;
  let reportId = "";
  let createdAt = "";
  let manifest: TranscriptScrollDiagnosticEnvironment | null = null;
  let events: TranscriptScrollDiagnosticEvent[] = [];
  let droppedEventCount = 0;
  let markerCount = 0;
  let stopTimer: ReturnType<typeof setTimeout> | null = null;
  let sampleFrame: number | null = null;
  let sampler: (() => Sample) | undefined;
  let lastSampleAt = -Infinity;
  let lastSampleSignature = "";

  const emit = () => listeners.forEach((listener) => listener());

  const append = (type: TranscriptScrollDiagnosticEventType, fields: EventFields = {}) => {
    if (status !== "recording" || !EVENT_TYPES.has(type)) return;
    events.push(sanitizeEvent(now() - startedAt, type, fields));
    if (events.length > maxEvents) {
      const removed = events.splice(0, events.length - maxEvents);
      markerCount -= removed.filter((event) => event.type === "mark").length;
      droppedEventCount += removed.length;
    }
  };

  const cancelAsync = () => {
    if (stopTimer !== null) clearTimeout(stopTimer);
    stopTimer = null;
    if (sampleFrame !== null && typeof cancelAnimationFrame === "function") cancelAnimationFrame(sampleFrame);
    sampleFrame = null;
  };

  const scheduleSample = () => {
    if (!sampler || typeof requestAnimationFrame !== "function") return;
    const tick = () => {
      sampleFrame = null;
      if (status !== "recording" || !sampler) return;
      const elapsed = now() - startedAt;
      if (elapsed - lastSampleAt >= 50) {
        const sample = sampler();
        if (sample) {
          const sanitized = sanitizeEvent(elapsed, "sample", sample);
          const signature = JSON.stringify({ ...sanitized, t: 0 });
          if (signature !== lastSampleSignature || elapsed - lastSampleAt >= 250) {
            append("sample", sample);
            lastSampleSignature = signature;
            lastSampleAt = elapsed;
          }
        }
      }
      sampleFrame = requestAnimationFrame(tick);
    };
    sampleFrame = requestAnimationFrame(tick);
  };

  const snapshot = (): TranscriptScrollDiagnosticSnapshot => ({
    status,
    durationMs: Math.max(0, Math.round((status === "recording" ? now() : stoppedAt) - startedAt)),
    eventCount: events.length,
    droppedEventCount,
    markerCount,
    reportId,
  });

  const stop = (): TranscriptScrollDiagnosticPayload => {
    if (status === "idle" || !manifest) throw new Error("scroll diagnostics are not active");
    if (status === "recording") {
      append("stop");
      stoppedAt = now();
      status = "stopped";
      cancelAsync();
      emit();
    }
    return {
      schemaVersion: TRANSCRIPT_SCROLL_DIAGNOSTIC_SCHEMA_VERSION,
      manifest: { ...manifest, reportId, createdAt },
      summary: {
        durationMs: Math.max(0, Math.round(stoppedAt - startedAt)),
        eventCount: events.length,
        droppedEventCount,
        markerCount,
      },
      events: events.map((event) => ({ ...event })),
    };
  };

  return {
    start(nextSampler?: () => Sample) {
      cancelAsync();
      status = "recording";
      startedAt = now();
      stoppedAt = startedAt;
      reportId = randomID();
      createdAt = new Date().toISOString();
      manifest = environment();
      events = [];
      droppedEventCount = 0;
      markerCount = 0;
      sampler = nextSampler;
      lastSampleAt = -Infinity;
      lastSampleSignature = "";
      append("start");
      stopTimer = setTimeout(() => stop(), maxDurationMs);
      scheduleSample();
      emit();
    },
    stop,
    reset() {
      cancelAsync();
      status = "idle";
      startedAt = 0;
      stoppedAt = 0;
      reportId = "";
      createdAt = "";
      manifest = null;
      events = [];
      droppedEventCount = 0;
      markerCount = 0;
      sampler = undefined;
      emit();
    },
    mark() {
      if (status !== "recording") return;
      markerCount += 1;
      append("mark");
      emit();
    },
    record(type: TranscriptScrollDiagnosticEventType, fields: EventFields = {}) {
      append(type, fields);
    },
    getSnapshot: snapshot,
    subscribe(listener: Listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
}

export const transcriptScrollDiagnostics = createTranscriptScrollDiagnostics();
setTranscriptScrollDiagnosticSink((type, fields) => {
  transcriptScrollDiagnostics.record(type as TranscriptScrollDiagnosticEventType, fields);
});

export { isTranscriptScrollDiagnosticsBuild } from "./transcriptScrollProbe";

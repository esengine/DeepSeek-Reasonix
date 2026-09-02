/**
 * Privacy-preserving frontend interaction recorder.
 *
 * This is deliberately a local, opt-in recorder. It captures timing and
 * geometry, not user text, transcript content, paths, stable ids, or input
 * values. The recorder is inert until a test-build user starts it, so the
 * ordinary desktop path has no listeners, timers, or allocation overhead.
 */

import { notifyFrontendDiagnosticStart, setFrontendDiagnosticSink } from "./frontendDiagnosticBridge";
import type { FrontendDiagnosticFields } from "./frontendDiagnosticBridge";

export { isFrontendDiagnosticsBuild } from "./frontendDiagnosticsBuild";

export const FRONTEND_DIAGNOSTIC_SCHEMA_VERSION = 2;
export const MAX_FRONTEND_DIAGNOSTIC_EVENTS = 16_384;
export const FRONTEND_DIAGNOSTIC_DURATION_MS = 120_000;

/** Geometry replay fixtures produced by v1 remain valid inputs. New captures
 * always write v2. */
export function isSupportedFrontendDiagnosticSchemaVersion(value: unknown): value is 1 | 2 {
  return value === 1 || value === FRONTEND_DIAGNOSTIC_SCHEMA_VERSION;
}

export type FrontendDiagnosticStatus = "idle" | "recording" | "stopped";

export type FrontendDiagnosticEvent = {
  t: number;
  type: string;
  source?: string;
  eventSource?: string;
  action?: string;
  target?: string;
  targetRole?: string;
  targetTag?: string;
  keyClass?: string;
  pointerType?: string;
  inputType?: string;
  visibility?: string;
  phase?: string;
  reason?: string;
  rejectedReason?: string;
  result?: string;
  status?: string;
  mode?: string;
  previousMode?: string;
  owner?: string;
  writeKind?: string;
  rowKind?: string;
  layoutVersion?: string;
  layoutVariant?: string;
  estimateSource?: string;
  foldState?: string;
  state?: string;
  errorName?: string;
  errorCode?: string;
  width?: number;
  height?: number;
  x?: number;
  y?: number;
  deltaX?: number;
  deltaY?: number;
  targetTop?: number;
  targetIndex?: number | "LAST";
  listHeight?: number;
  durationMs?: number;
  scrollTop?: number;
  scrollHeight?: number;
  clientHeight?: number;
  bottomDistance?: number;
  mountedRows?: number;
  totalRows?: number;
  firstVisibleIndex?: number;
  firstVisibleTop?: number;
  rowIndex?: number;
  estimatedSize?: number;
  previousSize?: number;
  measuredSize?: number;
  sizeDelta?: number;
  relativeError?: number;
  sequence?: number;
  generation?: number;
  surfaceGeneration?: number;
  ownershipEpoch?: number;
  geometryRevision?: number;
  transactionId?: number;
  footerHeight?: number;
  viewport?: number;
  mounted?: number;
  total?: number;
  reverseDisplacement?: number;
  extentDelta?: number;
  stableFrames?: number;
  direction?: number;
  disclosureCount?: number;
  contentRevision?: number;
  tabCount?: number;
  patchCount?: number;
  workspaceSessions?: number;
  visibleSessions?: number;
  hiddenSessions?: number;
  hiddenByFilter?: number;
  hiddenByCollapsed?: number;
  hiddenByTruncation?: number;
  runtimeSessions?: number;
  runtimeOnlySessions?: number;
  recoveryOnlySessions?: number;
  recoveryCopySessions?: number;
  recoveryCopies?: number;
  runningSessions?: number;
  unreadSessions?: number;
  pinnedSessions?: number;
  activeSessions?: number;
  activeVisibleSessions?: number;
  folderCount?: number;
  expandedFolders?: number;
  showAllFolders?: number;
  catalogRevision?: number;
  catalogIndexed?: number;
  catalogTotal?: number;
  repairPending?: number;
  treeRevision?: number;
  organizationRevision?: number;
  unloadedSessions?: number;
  deltaWorkspaceSessions?: number;
  deltaVisibleSessions?: number;
  deltaHiddenSessions?: number;
  deltaRecoveryCopies?: number;
  deltaRuntimeOnlySessions?: number;
  hasActiveTab?: boolean;
  ready?: boolean;
  running?: boolean;
  hydrating?: boolean;
  runtimeTransitioning?: boolean;
  atBottom?: boolean;
  scrollable?: boolean;
  blank?: boolean;
  readerIntent?: boolean;
  canClaimTail?: boolean;
  substantial?: boolean;
  transient?: boolean;
  tailCommand?: boolean;
  isTrusted?: boolean;
  queryActive?: boolean;
  timeFilterActive?: boolean;
  catalogPartial?: boolean;
  catalogRebuilding?: boolean;
  directoryState?: string;
  changeReason?: string;
  outcome?: string;
  trigger?: string;
  scope?: string;
  variant?: string;
  timeFilter?: string;
  button?: number;
  modifiers?: number;
  intent?: number;
  sources?: Array<"footer-resize" | "row-measure" | "data-change" | "viewport-resize" | "fold-change" | "typography-change" | "items-rendered">;
};

export type FrontendDiagnosticAnomaly = {
  code: "settle-before-paint-ready" | "viewport-older-without-user-input" | "navigation-session-count-changed" | "unknown-scroll-writer" | "target-empty-sequence";
  intent?: number;
  count?: number;
};

export type FrontendDiagnosticEnvironment = {
  buildCommit: string;
  buildChannel: string;
  platform: "windows" | "macos" | "linux" | "other";
  userAgent: string;
  devicePixelRatio: number;
  viewportWidth: number;
  viewportHeight: number;
  language: string;
  reducedMotion: boolean;
};

export type FrontendDiagnosticPayload = {
  schemaVersion: typeof FRONTEND_DIAGNOSTIC_SCHEMA_VERSION;
  manifest: FrontendDiagnosticEnvironment & { reportId: string; createdAt: string };
  summary: {
    durationMs: number;
    eventCount: number;
    droppedEventCount: number;
    markerCount: number;
    anomalies: FrontendDiagnosticAnomaly[];
  };
  events: FrontendDiagnosticEvent[];
};

export type FrontendDiagnosticSnapshot = {
  status: FrontendDiagnosticStatus;
  durationMs: number;
  eventCount: number;
  droppedEventCount: number;
  markerCount: number;
  reportId: string;
};

type EventFields = FrontendDiagnosticFields;
type Sample = EventFields | null;
type Listener = () => void;
type EventTargetListener = { target: EventTarget; type: string; listener: EventListener; options?: AddEventListenerOptions };

const NUMBER_FIELDS = [
  "width", "height", "x", "y", "deltaX", "deltaY", "targetTop", "listHeight", "durationMs", "scrollTop", "scrollHeight",
  "clientHeight", "bottomDistance", "mountedRows", "totalRows", "firstVisibleIndex", "firstVisibleTop",
  "rowIndex", "estimatedSize", "previousSize", "measuredSize", "sizeDelta", "relativeError", "disclosureCount", "contentRevision", "tabCount", "patchCount", "button", "modifiers", "intent",
  "sequence", "generation", "surfaceGeneration", "ownershipEpoch", "geometryRevision", "transactionId", "footerHeight", "viewport", "mounted", "total", "reverseDisplacement", "extentDelta", "stableFrames", "direction",
  "workspaceSessions", "visibleSessions", "hiddenSessions", "hiddenByFilter", "hiddenByCollapsed", "hiddenByTruncation", "runtimeSessions", "runtimeOnlySessions", "recoveryOnlySessions", "recoveryCopySessions", "recoveryCopies", "runningSessions", "unreadSessions", "pinnedSessions", "activeSessions", "activeVisibleSessions", "folderCount", "expandedFolders", "showAllFolders", "catalogRevision", "catalogIndexed", "catalogTotal", "repairPending", "treeRevision", "organizationRevision", "unloadedSessions", "deltaWorkspaceSessions", "deltaVisibleSessions", "deltaHiddenSessions", "deltaRecoveryCopies", "deltaRuntimeOnlySessions",
] as const;
const BOOLEAN_FIELDS = [
  "hasActiveTab", "ready", "running", "hydrating", "runtimeTransitioning", "atBottom", "scrollable", "blank", "readerIntent", "canClaimTail", "substantial", "tailCommand", "isTrusted",
  "queryActive", "timeFilterActive", "catalogPartial", "catalogRebuilding", "transient",
] as const;
const STRING_FIELDS = [
  "source", "eventSource", "action", "target", "targetRole", "targetTag", "keyClass", "pointerType", "inputType", "visibility", "phase",
  "reason", "rejectedReason", "result", "status", "mode", "previousMode", "owner", "writeKind", "rowKind", "layoutVersion", "layoutVariant", "estimateSource", "foldState", "state", "errorName", "errorCode",
  "directoryState", "changeReason", "outcome", "trigger", "scope", "variant", "timeFilter",
] as const;
const GEOMETRY_SOURCES = new Set([
  "footer-resize", "row-measure", "data-change", "viewport-resize", "fold-change", "typography-change", "items-rendered",
]);

export function analyzeFrontendDiagnosticAnomalies(events: readonly FrontendDiagnosticEvent[]): FrontendDiagnosticAnomaly[] {
  const anomalies: FrontendDiagnosticAnomaly[] = [];
  const painted = new Set<number>();
  const navigation = new Map<number, { sessionCount?: number; counts: Set<number>; rows: number[] }>();
  const knownScrollWriters = new Set([
    "tail-follow", "recovery", "reader-stability", "jump", "rewind", "jump-bottom", "custom-scrollbar",
    "selection-edge-scroll", "anchor-compensation", "block-window-prepend",
  ]);
  let viewportPermit = 0;
  let unknownWriters = 0;
  let unpermittedOlder = 0;
  for (const event of events) {
    if (event.type === "history.viewport-permit") viewportPermit = 1;
    if (event.type === "navigation.begin" && event.intent !== undefined) {
      navigation.set(event.intent, { counts: new Set(), rows: [] });
    }
    if (event.type === "navigation.paint-ready" && event.intent !== undefined) painted.add(event.intent);
    if (event.type === "navigation.settle" && event.intent !== undefined && event.outcome !== "failed" &&
      event.outcome !== "cancelled" && event.outcome !== "superseded" && !painted.has(event.intent)) {
      anomalies.push({ code: "settle-before-paint-ready", intent: event.intent });
    }
    if (event.type === "history.older-request" && event.trigger === "viewport-user") {
      if (viewportPermit > 0) viewportPermit -= 1;
      else unpermittedOlder += 1;
    }
    if (event.type === "transcript.scroll-write" && event.owner && !knownScrollWriters.has(event.owner)) unknownWriters += 1;
    for (const [intent, state] of navigation) {
      if (painted.has(intent)) continue;
      if (event.workspaceSessions !== undefined) state.counts.add(event.workspaceSessions);
      if (event.type === "transcript.surface" && event.totalRows !== undefined) state.rows.push(event.totalRows);
    }
  }
  for (const [intent, state] of navigation) {
    if (state.counts.size > 1) anomalies.push({ code: "navigation-session-count-changed", intent, count: state.counts.size });
    const sequence = state.rows;
    let sawEmpty = false;
    let sawNonEmptyAfterEmpty = false;
    let exposed = false;
    for (const rows of sequence) {
      if (rows === 0) {
        if (sawNonEmptyAfterEmpty) exposed = true;
        sawEmpty = true;
      } else if (sawEmpty) {
        sawNonEmptyAfterEmpty = true;
      }
    }
    if (exposed) anomalies.push({ code: "target-empty-sequence", intent });
  }
  if (unpermittedOlder > 0) anomalies.push({ code: "viewport-older-without-user-input", count: unpermittedOlder });
  if (unknownWriters > 0) anomalies.push({ code: "unknown-scroll-writer", count: unknownWriters });
  return anomalies;
}

function finiteNumber(value: unknown): number | undefined {
  if (typeof value !== "number" || !Number.isFinite(value)) return undefined;
  return Math.round(value * 100) / 100;
}

function safeToken(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const token = value.trim();
  if (!/^[a-zA-Z0-9._:-]{1,64}$/.test(token)) return undefined;
  return token;
}

function safeEventType(value: unknown): string {
  return safeToken(value) ?? "other";
}

function normalizedPlatform(): FrontendDiagnosticEnvironment["platform"] {
  if (typeof navigator === "undefined") return "other";
  const marker = `${navigator.userAgent} ${navigator.platform}`.toLowerCase();
  if (marker.includes("win")) return "windows";
  if (marker.includes("mac")) return "macos";
  if (marker.includes("linux")) return "linux";
  return "other";
}

function defaultEnvironment(): FrontendDiagnosticEnvironment {
  return {
    buildCommit: typeof __BUILD_COMMIT__ === "string" ? __BUILD_COMMIT__ : "dev",
    buildChannel: typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "development",
    platform: normalizedPlatform(),
    userAgent: typeof navigator === "undefined" ? "unknown" : navigator.userAgent.slice(0, 512),
    devicePixelRatio: finiteNumber(globalThis.devicePixelRatio) ?? 1,
    viewportWidth: typeof window === "undefined" ? 0 : Math.max(0, Math.round(window.innerWidth)),
    viewportHeight: typeof window === "undefined" ? 0 : Math.max(0, Math.round(window.innerHeight)),
    language: typeof navigator === "undefined" ? "unknown" : navigator.language.slice(0, 32),
    reducedMotion: typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true,
  };
}

function sanitizeEvent(t: number, type: string, fields: EventFields): FrontendDiagnosticEvent {
  const event: FrontendDiagnosticEvent = { t: Math.max(0, Math.round(t)), type: safeEventType(type) };
  const target = event as Record<string, unknown>;
  for (const field of NUMBER_FIELDS) {
    const value = finiteNumber(fields[field]);
    if (value !== undefined) target[field] = value;
  }
  const targetIndex = fields.targetIndex;
  if (typeof targetIndex === "number" && Number.isFinite(targetIndex)) target.targetIndex = Math.round(targetIndex);
  else if (targetIndex === "LAST") target.targetIndex = targetIndex;
  for (const field of BOOLEAN_FIELDS) {
    if (typeof fields[field] === "boolean") target[field] = fields[field];
  }
  for (const field of STRING_FIELDS) {
    const value = safeToken(fields[field]);
    if (value !== undefined) target[field] = value;
  }
  if (Array.isArray(fields.sources)) {
    const sources = [...new Set(fields.sources.filter((value): value is FrontendDiagnosticEvent["sources"] extends Array<infer Item> ? Item : never => (
      typeof value === "string" && GEOMETRY_SOURCES.has(value)
    )))];
    if (sources.length > 0) event.sources = sources;
  }
  return event;
}

function targetElement(value: EventTarget | null): Element | null {
  if (value instanceof Element) return value;
  return null;
}

function classifyTarget(value: EventTarget | null): Pick<FrontendDiagnosticEvent, "target" | "targetRole" | "targetTag"> {
  const element = targetElement(value);
  if (!element) return { target: "window" };
  const control = element.closest("button,input,textarea,select,[role],[contenteditable='true']") ?? element;
  const tag = control.tagName.toLowerCase();
  const role = control.getAttribute("role");
  const target = tag === "button" ? "button" : tag === "input" ? "input" : tag === "textarea" ? "textarea" : tag === "select" ? "select" : role ? `role-${role}` : tag;
  return { target: safeToken(target), targetRole: safeToken(role ?? undefined), targetTag: safeToken(tag) };
}

function keyClass(key: string): string {
  if (/^Arrow(?:Up|Down|Left|Right)$/.test(key)) return "arrow";
  if (key === "Enter" || key === "Escape" || key === "Tab" || key === "Backspace" || key === "Delete" || key === "Home" || key === "End" || key === "PageUp" || key === "PageDown") return key.toLowerCase();
  if (key.length === 1) return "character";
  return "other";
}

function transcriptGeometry(element: HTMLElement | null): EventFields {
  if (!element) return {};
  const bottomDistance = element.scrollHeight - element.scrollTop - element.clientHeight;
  return {
    scrollTop: element.scrollTop,
    scrollHeight: element.scrollHeight,
    clientHeight: element.clientHeight,
    bottomDistance,
    scrollable: element.scrollHeight - element.clientHeight > 4,
    atBottom: bottomDistance <= 4,
  };
}

function eventModifiers(event: MouseEvent | KeyboardEvent | WheelEvent): number {
  return (event.ctrlKey ? 1 : 0) | (event.shiftKey ? 2 : 0) | (event.altKey ? 4 : 0) | (event.metaKey ? 8 : 0);
}

export function createFrontendDiagnostics(options: {
  maxEvents?: number;
  maxDurationMs?: number;
  now?: () => number;
  randomID?: () => string;
  environment?: () => FrontendDiagnosticEnvironment;
} = {}) {
  const maxEvents = Math.max(32, Math.min(MAX_FRONTEND_DIAGNOSTIC_EVENTS, Math.round(options.maxEvents ?? MAX_FRONTEND_DIAGNOSTIC_EVENTS)));
  const maxDurationMs = Math.max(1_000, Math.min(FRONTEND_DIAGNOSTIC_DURATION_MS, Math.round(options.maxDurationMs ?? FRONTEND_DIAGNOSTIC_DURATION_MS)));
  const now = options.now ?? (() => performance.now());
  const randomID = options.randomID ?? (() => {
    const bytes = new Uint8Array(16);
    if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") crypto.getRandomValues(bytes);
    else for (let index = 0; index < bytes.length; index += 1) bytes[index] = Math.floor(Math.random() * 256);
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  });
  const environment = options.environment ?? defaultEnvironment;
  const listeners = new Set<Listener>();
  let status: FrontendDiagnosticStatus = "idle";
  let startedAt = 0;
  let stoppedAt = 0;
  let reportId = "";
  let createdAt = "";
  let manifest: FrontendDiagnosticEnvironment | null = null;
  let events: FrontendDiagnosticEvent[] = [];
  let droppedEventCount = 0;
  let markerCount = 0;
  let stopTimer: ReturnType<typeof setTimeout> | null = null;
  let sampleFrame: number | null = null;
  let sampler: (() => Sample) | undefined;
  let lastSampleAt = -Infinity;
  let lastSampleSignature = "";
  let eventListeners: EventTargetListener[] = [];

  const emit = () => listeners.forEach((listener) => listener());

  const append = (type: string, fields: EventFields = {}) => {
    if (status !== "recording") return;
    events.push(sanitizeEvent(now() - startedAt, type, fields));
    if (events.length > maxEvents) {
      const removed = events.splice(0, events.length - maxEvents);
      markerCount -= removed.filter((event) => event.type === "marker").length;
      droppedEventCount += removed.length;
    }
  };

  const listen = (target: EventTarget, type: string, listener: EventListener, options?: AddEventListenerOptions) => {
    target.addEventListener(type, listener, options);
    eventListeners.push({ target, type, listener, options });
  };

  const removeListeners = () => {
    for (const entry of eventListeners) entry.target.removeEventListener(entry.type, entry.listener, entry.options);
    eventListeners = [];
  };

  const recordPointer = (type: string, event: PointerEvent) => {
    const target = classifyTarget(event.target);
    append(type, { ...target, pointerType: event.pointerType, button: event.button, modifiers: eventModifiers(event), x: event.clientX, y: event.clientY, isTrusted: event.isTrusted });
  };

  const recordWheel = (event: WheelEvent) => {
    const target = classifyTarget(event.target);
    const transcript = targetElement(event.target)?.closest<HTMLElement>(".transcript") ?? document.querySelector<HTMLElement>(".transcript");
    append("wheel", { ...target, deltaX: event.deltaX, deltaY: event.deltaY, modifiers: eventModifiers(event), isTrusted: event.isTrusted, ...transcriptGeometry(transcript) });
  };

  const recordScroll = (event: Event) => {
    const element = targetElement(event.target)?.closest<HTMLElement>(".transcript");
    if (!element) return;
    append("scroll", { ...classifyTarget(event.target), ...transcriptGeometry(element), isTrusted: event.isTrusted });
  };

  const attachBrowserListeners = () => {
    if (typeof window === "undefined" || typeof document === "undefined") return;
    listen(document, "pointerdown", (event) => recordPointer("pointerdown", event as PointerEvent), { capture: true, passive: true });
    listen(document, "pointerup", (event) => recordPointer("pointerup", event as PointerEvent), { capture: true, passive: true });
    listen(document, "click", (event) => recordPointer("click", event as PointerEvent), { capture: true, passive: true });
    listen(document, "wheel", (event) => recordWheel(event as WheelEvent), { capture: true, passive: true });
    listen(document, "scroll", recordScroll, { capture: true, passive: true });
    listen(document, "keydown", (event) => {
      const keyboard = event as KeyboardEvent;
      append("keydown", { ...classifyTarget(keyboard.target), keyClass: keyClass(keyboard.key), modifiers: eventModifiers(keyboard), isTrusted: keyboard.isTrusted });
    }, { capture: true, passive: true });
    listen(document, "keyup", (event) => {
      const keyboard = event as KeyboardEvent;
      append("keyup", { ...classifyTarget(keyboard.target), keyClass: keyClass(keyboard.key), modifiers: eventModifiers(keyboard), isTrusted: keyboard.isTrusted });
    }, { capture: true, passive: true });
    listen(document, "input", (event) => {
      const input = event as InputEvent;
      append("input", { ...classifyTarget(input.target), inputType: input.inputType, isTrusted: input.isTrusted });
    }, { capture: true, passive: true });
    listen(document, "focusin", (event) => append("focusin", classifyTarget(event.target)), { capture: true, passive: true });
    listen(document, "focusout", (event) => append("focusout", classifyTarget(event.target)), { capture: true, passive: true });
    listen(document, "selectionchange", () => append("selectionchange"));
    listen(document, "visibilitychange", () => append("visibility", { visibility: document.visibilityState }));
    listen(window, "resize", () => append("resize", { width: window.innerWidth, height: window.innerHeight }));
    listen(window, "online", () => append("network", { status: "online" }));
    listen(window, "offline", () => append("network", { status: "offline" }));
    listen(window, "error", () => append("error", { errorName: "window-error" }));
    listen(window, "unhandledrejection", () => append("error", { errorName: "unhandled-rejection" }));
    listen(window, "pageshow", () => append("lifecycle", { action: "pageshow" }));
    listen(window, "pagehide", () => append("lifecycle", { action: "pagehide" }));
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

  const snapshot = (): FrontendDiagnosticSnapshot => ({
    status,
    durationMs: Math.max(0, Math.round((status === "recording" ? now() : stoppedAt) - startedAt)),
    eventCount: events.length,
    droppedEventCount,
    markerCount,
    reportId,
  });

  const stop = (): FrontendDiagnosticPayload => {
    if (status === "idle" || !manifest) throw new Error("frontend diagnostics are not active");
    if (status === "recording") {
      append("stop");
      stoppedAt = now();
      status = "stopped";
      cancelAsync();
      removeListeners();
      emit();
    }
    return {
      schemaVersion: FRONTEND_DIAGNOSTIC_SCHEMA_VERSION,
      manifest: { ...manifest, reportId, createdAt },
      summary: {
        durationMs: Math.max(0, Math.round(stoppedAt - startedAt)),
        eventCount: events.length,
        droppedEventCount,
        markerCount,
        anomalies: analyzeFrontendDiagnosticAnomalies(events),
      },
      events: events.map((event) => ({ ...event })),
    };
  };

  return {
    start(nextSampler?: () => Sample) {
      cancelAsync();
      removeListeners();
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
      append("start", { source: "recorder" });
      attachBrowserListeners();
      notifyFrontendDiagnosticStart();
      stopTimer = setTimeout(() => stop(), maxDurationMs);
      scheduleSample();
      emit();
    },
    stop,
    reset() {
      cancelAsync();
      removeListeners();
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
      append("marker", { source: "user" });
      emit();
    },
    record(source: string, type: string, fields: EventFields = {}) {
      const { source: eventSource, ...rest } = fields;
      append(type, { ...rest, source, eventSource });
    },
    getSnapshot: snapshot,
    subscribe(listener: Listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
}

export const frontendDiagnostics = createFrontendDiagnostics();

export function frontendDiagnosticSample(element: HTMLElement | null, totalRows?: number): EventFields | null {
  if (!element) return null;
  const viewport = element.getBoundingClientRect();
  const rows = element.querySelectorAll<HTMLElement>(".transcript__row");
  let firstVisibleIndex: number | undefined;
  let firstVisibleTop: number | undefined;
  for (const row of rows) {
    const rect = row.getBoundingClientRect();
    if (rect.bottom <= viewport.top || rect.top >= viewport.bottom) continue;
    const index = Number(row.dataset.logicalIndex ?? row.dataset.index);
    firstVisibleIndex = Number.isFinite(index) ? index : undefined;
    firstVisibleTop = rect.top - viewport.top;
    break;
  }
  return {
    ...transcriptGeometry(element),
    mountedRows: rows.length,
    totalRows: totalRows ?? Number(element.dataset.transcriptRowCount ?? 0),
    firstVisibleIndex,
    firstVisibleTop,
    mode: element.dataset.scrollMode,
  };
}

setFrontendDiagnosticSink((source, type, fields) => frontendDiagnostics.record(source, type, fields));

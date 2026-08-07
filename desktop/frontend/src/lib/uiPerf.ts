// UI latency telemetry: turn-scoped, content-free counters and percentiles for
// the streaming render pipeline. Everything reported is a bounded (signal,
// bucket) pair — no message text, no timings tied to content — matching the
// existing desktop metrics privacy posture.
//
// The targets below are performance budgets, not hard standards; recalibrate
// them against the supported machine population before treating a breach as a
// regression gate.
export const UI_PERF_BUDGETS = {
  bridgeEventsPerSec: 120, // Go→WebView stream event rate; ideally < 60
  stateCommitsPerSec: 60, // stream store updates; at most display FPS
  streamPaintP95Ms: 50, // token dispatch → next frame
  frameP95Ms: 16.7,
  slowFramePct: 1, // frames > 33ms
  inputLatencyP95Ms: 100, // typing while streaming
  markdownRenderP95Ms: 10,
  longTasks: 0, // main-thread tasks > 50ms per turn
} as const;

export interface UIPerfSummary {
  turnMs: number;
  bridgeEvents: number;
  stateCommits: number;
  streamPaintP95Ms?: number;
  frameP95Ms?: number;
  slowFramePct?: number;
  inputLatencyP95Ms?: number;
  markdownRenderP95Ms?: number;
  longTasks: number;
  domNodes?: number;
  jsHeapMB?: number;
}

export function percentile(values: number[], p: number): number | undefined {
  if (values.length === 0) return undefined;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1);
  return sorted[Math.max(0, index)];
}

function rateBucket(perSec: number): string {
  if (perSec < 30) return "lt30";
  if (perSec < 60) return "lt60";
  if (perSec < 120) return "lt120";
  return "ge120";
}

function msBucket(ms: number, edges: number[]): string {
  for (const edge of edges) if (ms < edge) return `lt${edge}`;
  return `ge${edges[edges.length - 1]}`;
}

// uiPerfSignals maps a turn summary onto the closed (signal, bucket) set the
// Go side allowlists. Signals without data (feature-detection gaps, no
// samples) are omitted rather than reported as zero.
export function uiPerfSignals(s: UIPerfSummary): Record<string, string> {
  const out: Record<string, string> = {};
  const seconds = s.turnMs / 1000;
  if (seconds > 1) {
    out.ui_bridge_events_rate = rateBucket(s.bridgeEvents / seconds);
    out.ui_state_commit_rate = rateBucket(s.stateCommits / seconds);
  }
  if (s.streamPaintP95Ms !== undefined) out.ui_stream_paint_p95 = msBucket(s.streamPaintP95Ms, [25, 50, 100]);
  if (s.frameP95Ms !== undefined) out.ui_frame_p95 = msBucket(s.frameP95Ms, [17, 33]);
  if (s.slowFramePct !== undefined) {
    out.ui_slow_frames = s.slowFramePct === 0 ? "zero" : s.slowFramePct < 1 ? "lt1" : s.slowFramePct < 5 ? "lt5" : "ge5";
  }
  if (s.inputLatencyP95Ms !== undefined) out.ui_input_latency_p95 = msBucket(s.inputLatencyP95Ms, [50, 100]);
  if (s.markdownRenderP95Ms !== undefined) out.ui_markdown_p95 = msBucket(s.markdownRenderP95Ms, [10, 50]);
  out.ui_long_tasks = s.longTasks === 0 ? "zero" : s.longTasks < 3 ? "lt3" : "ge3";
  if (s.domNodes !== undefined) {
    out.ui_dom_nodes = s.domNodes < 5_000 ? "lt5k" : s.domNodes < 20_000 ? "lt20k" : "ge20k";
  }
  if (s.jsHeapMB !== undefined) {
    out.ui_js_heap = s.jsHeapMB < 200 ? "lt200" : s.jsHeapMB < 500 ? "lt500" : "ge500";
  }
  return out;
}

interface PerfEnv {
  now(): number;
  raf(cb: (ts: number) => void): number;
  caf(handle: number): void;
  observe(type: string, cb: (durationsMs: number[]) => void): (() => void) | undefined;
  domNodes(): number | undefined;
  jsHeapMB(): number | undefined;
}

function defaultEnv(): PerfEnv {
  return {
    now: () => performance.now(),
    raf: (cb) => requestAnimationFrame(cb),
    caf: (handle) => cancelAnimationFrame(handle),
    observe(type, cb) {
      if (typeof PerformanceObserver === "undefined") return undefined;
      try {
        const observer = new PerformanceObserver((list) => {
          const durations: number[] = [];
          for (const entry of list.getEntries()) {
            if (type === "measure" && !entry.name.startsWith("reasonix:markdown")) continue;
            if (type === "event") {
              const timing = entry as PerformanceEntry & { processingStart?: number };
              durations.push((timing.processingStart ?? entry.startTime + entry.duration) - entry.startTime);
              continue;
            }
            durations.push(entry.duration);
          }
          if (durations.length > 0) cb(durations);
        });
        observer.observe(type === "event" ? { type, durationThreshold: 16 } as PerformanceObserverInit : { type });
        return () => observer.disconnect();
      } catch {
        return undefined;
      }
    },
    domNodes: () => (typeof document === "undefined" ? undefined : document.getElementsByTagName("*").length),
    jsHeapMB() {
      const memory = (performance as Performance & { memory?: { usedJSHeapSize: number } }).memory;
      return memory ? Math.round(memory.usedJSHeapSize / (1 << 20)) : undefined;
    },
  };
}

const MAX_SAMPLES = 512;

function push(samples: number[], value: number) {
  if (samples.length < MAX_SAMPLES) samples.push(value);
}

// UIPerfTurnCollector samples one turn: a rAF loop measures frame durations
// and dispatch→frame latency while it runs; observers count long tasks and
// input/markdown timings. All sampling stops at finish().
export class UIPerfTurnCollector {
  private env: PerfEnv;
  private startedAt: number;
  private bridgeEvents = 0;
  private stateCommits = 0;
  private longTasks = 0;
  private frames: number[] = [];
  private slowFrames = 0;
  private streamPaint: number[] = [];
  private inputLatency: number[] = [];
  private markdown: number[] = [];
  private pendingDispatchAt: number | null = null;
  private lastFrameAt: number | null = null;
  private frameHandle: number | null = null;
  private disconnects: Array<() => void> = [];
  private done = false;

  constructor(env: PerfEnv = defaultEnv()) {
    this.env = env;
    this.startedAt = env.now();
    const longTask = env.observe("longtask", (durations) => {
      this.longTasks += durations.length;
    });
    if (longTask) this.disconnects.push(longTask);
    const input = env.observe("event", (durations) => {
      for (const d of durations) push(this.inputLatency, d);
    });
    if (input) this.disconnects.push(input);
    const markdown = env.observe("measure", (durations) => {
      for (const d of durations) push(this.markdown, d);
    });
    if (markdown) this.disconnects.push(markdown);
    this.scheduleFrame();
  }

  private scheduleFrame() {
    this.frameHandle = this.env.raf((ts) => {
      if (this.done) return;
      if (this.lastFrameAt !== null) {
        const duration = ts - this.lastFrameAt;
        push(this.frames, duration);
        if (duration > 33.4) this.slowFrames += 1;
      }
      this.lastFrameAt = ts;
      if (this.pendingDispatchAt !== null) {
        push(this.streamPaint, Math.max(0, ts - this.pendingDispatchAt));
        this.pendingDispatchAt = null;
      }
      this.scheduleFrame();
    });
  }

  noteBridgeEvent() {
    this.bridgeEvents += 1;
  }

  noteStateCommit() {
    this.stateCommits += 1;
  }

  noteStreamDispatch() {
    if (this.pendingDispatchAt === null) this.pendingDispatchAt = this.env.now();
  }

  finish(): UIPerfSummary {
    this.done = true;
    if (this.frameHandle !== null) this.env.caf(this.frameHandle);
    for (const disconnect of this.disconnects) disconnect();
    this.disconnects = [];
    return {
      turnMs: this.env.now() - this.startedAt,
      bridgeEvents: this.bridgeEvents,
      stateCommits: this.stateCommits,
      streamPaintP95Ms: percentile(this.streamPaint, 95),
      frameP95Ms: percentile(this.frames, 95),
      slowFramePct: this.frames.length > 0 ? (this.slowFrames / this.frames.length) * 100 : undefined,
      inputLatencyP95Ms: percentile(this.inputLatency, 95),
      markdownRenderP95Ms: percentile(this.markdown, 95),
      longTasks: this.longTasks,
      domNodes: this.env.domNodes(),
      jsHeapMB: this.env.jsHeapMB(),
    };
  }
}

// uiPerfTracker is the app-wide instance; useController feeds it wire events,
// state commits, and stream dispatches.
export const uiPerfTracker: UIPerfTracker = createUIPerfTracker((signals) => {
  void import("./bridge").then(({ app }) => app.RecordUIPerf(signals)).catch(() => {});
});

export interface UIPerfTracker {
  onWireEvent(tabId: string, kind: string): void;
  onStateCommit(): void;
  onStreamDispatch(): void;
}

// createUIPerfTracker owns per-tab collectors keyed on turn lifecycle events
// and reports each finished turn's bucketed signals through `report`.
export function createUIPerfTracker(
  report: (signals: Record<string, string>) => void,
  makeCollector: () => UIPerfTurnCollector = () => new UIPerfTurnCollector(),
): UIPerfTracker {
  const collectors = new Map<string, UIPerfTurnCollector>();
  return {
    onWireEvent(tabId, kind) {
      if (kind === "turn_started" && !collectors.has(tabId)) {
        collectors.set(tabId, makeCollector());
        return;
      }
      const collector = collectors.get(tabId);
      if (!collector) return;
      collector.noteBridgeEvent();
      if (kind === "turn_done") {
        collectors.delete(tabId);
        const signals = uiPerfSignals(collector.finish());
        if (Object.keys(signals).length > 0) report(signals);
      }
    },
    onStateCommit() {
      for (const collector of collectors.values()) collector.noteStateCommit();
    },
    onStreamDispatch() {
      for (const collector of collectors.values()) collector.noteStreamDispatch();
    },
  };
}

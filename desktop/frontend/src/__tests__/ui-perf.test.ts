// Run: tsx src/__tests__/ui-perf.test.ts
//
// UI latency telemetry: percentiles, bounded (signal, bucket) mapping, the
// turn collector's frame/dispatch sampling, and the tracker's turn lifecycle.
// Everything reported must stay content-free.

import { createUIPerfTracker, percentile, UIPerfTurnCollector, uiPerfSignals } from "../lib/uiPerf";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function fakeEnv() {
  let now = 0;
  const frames: Array<(ts: number) => void> = [];
  return {
    env: {
      now: () => now,
      raf: (cb: (ts: number) => void) => {
        frames.push(cb);
        return frames.length;
      },
      caf: () => {},
      observe: () => undefined,
      domNodes: () => 1_234,
      jsHeapMB: () => 100,
    },
    advance(ms: number) {
      now += ms;
    },
    frame() {
      frames.shift()?.(now);
    },
  };
}

// --- percentile ---
{
  eq(percentile([], 95), undefined, "empty sample set has no percentile");
  eq(percentile([7], 95), 7, "single sample is its own percentile");
  const values = [10, 20, 30, 40, 50, 60, 70, 80, 90, 100];
  eq(percentile(values, 50), 50, "p50 of 10 evenly spread samples");
  eq(percentile(values, 95), 100, "p95 of 10 samples takes the top sample");
}

// --- signal mapping: bounded buckets, absent data omitted ---
{
  const signals = uiPerfSignals({
    turnMs: 10_000,
    bridgeEvents: 500, // 50/s
    stateCommits: 250, // 25/s
    streamPaintP95Ms: 30,
    frameP95Ms: 12,
    slowFramePct: 0.4,
    inputLatencyP95Ms: 120,
    markdownRenderP95Ms: 9,
    longTasks: 0,
    domNodes: 12_000,
    jsHeapMB: 250,
  });
  eq(signals.ui_bridge_events_rate, "lt60", "bridge event rate buckets by per-second rate");
  eq(signals.ui_state_commit_rate, "lt30", "state commit rate buckets by per-second rate");
  eq(signals.ui_stream_paint_p95, "lt50", "stream→paint p95 buckets in ms");
  eq(signals.ui_frame_p95, "lt17", "frame p95 under one display frame");
  eq(signals.ui_slow_frames, "lt1", "slow frame percentage buckets");
  eq(signals.ui_input_latency_p95, "ge100", "input latency over budget lands in the top bucket");
  eq(signals.ui_markdown_p95, "lt10", "markdown render p95 within budget");
  eq(signals.ui_long_tasks, "zero", "no long tasks reports zero");
  eq(signals.ui_dom_nodes, "lt20k", "DOM size buckets");
  eq(signals.ui_js_heap, "lt500", "JS heap buckets");

  const sparse = uiPerfSignals({ turnMs: 500, bridgeEvents: 3, stateCommits: 2, longTasks: 1 });
  eq(sparse.ui_bridge_events_rate, undefined, "sub-second turns report no rates");
  eq(sparse.ui_stream_paint_p95, undefined, "missing samples are omitted, not reported as zero");
  eq(sparse.ui_long_tasks, "lt3", "long task count still buckets");
}

// --- collector: frames, slow frames, dispatch→frame latency ---
{
  const f = fakeEnv();
  const c = new UIPerfTurnCollector(f.env);
  c.noteBridgeEvent();
  c.noteBridgeEvent();
  c.noteStateCommit();

  f.advance(100);
  f.frame(); // first frame: baseline only
  c.noteStreamDispatch();
  f.advance(40);
  f.frame(); // 40ms frame → slow, and dispatch→frame sample of 40ms
  f.advance(10);
  f.frame(); // 10ms frame
  f.advance(1_900);

  const summary = c.finish();
  eq(summary.bridgeEvents, 2, "bridge events counted");
  eq(summary.stateCommits, 1, "state commits counted");
  eq(summary.turnMs, 2_050, "turn duration from injected clock");
  eq(summary.frameP95Ms, 40, "frame p95 from sampled frame durations");
  eq(summary.slowFramePct, 50, "one of two frames over 33ms");
  eq(summary.streamPaintP95Ms, 40, "dispatch→frame latency sampled on the next frame");
  eq(summary.domNodes, 1_234, "DOM size snapshot at finish");
  eq(summary.jsHeapMB, 100, "heap snapshot at finish");
}

// --- tracker: per-tab turn lifecycle drives collection and reporting ---
{
  const reports: Array<Record<string, string>> = [];
  const f = fakeEnv();
  const tracker = createUIPerfTracker(
    (signals) => reports.push(signals),
    () => new UIPerfTurnCollector(f.env),
  );

  tracker.onWireEvent("tab-b", "text"); // no turn: ignored
  tracker.onWireEvent("tab-a", "turn_started");
  tracker.onWireEvent("tab-a", "text");
  tracker.onWireEvent("tab-a", "text");
  tracker.onStateCommit();
  tracker.onStreamDispatch();
  f.advance(2_000);
  tracker.onWireEvent("tab-a", "turn_done");

  eq(reports.length, 1, "turn_done reports exactly once");
  eq(reports[0].ui_bridge_events_rate, "lt30", "reported signals come from the turn's own counters");
  tracker.onStateCommit(); // after turn end: no active collector, no crash
  eq(reports.length, 1, "no reporting outside a turn");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);

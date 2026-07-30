// Run: tsx src/__tests__/crash-reporting.test.ts

import {
  aggregateLongTaskProfile,
  buildCrashPayload,
  buildPerformancePayload,
  formatLongTaskAttribution,
  formatPerformanceContext,
  globalCrashReportReason,
  isOpaqueScriptErrorEvent,
  installPerformancePressureMonitor,
  normalizeCrashError,
  opaqueScriptFingerprintHint,
  parseReportedPerf,
  performanceLabelForReason,
  serializeReportedPerf,
  shouldPromptForLongTasks,
  shouldPromptForEventLoopLag,
  shouldRecordEventLoopLagSample,
  shouldPromptForPerformanceLabel,
  shouldReportGlobalCrashEvent,
  shouldRecordLongTaskSample,
  topFrameFromStack,
  type PerformanceSnapshot,
  type ProfilerTrace,
} from "../lib/crash";
import { writeClipboardText } from "../lib/clipboard";
import { installObjectHasOwnPolyfill } from "../lib/compat";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ncrash reporting");

const testDir = dirname(fileURLToPath(import.meta.url));
const mainSource = readFileSync(resolve(testDir, "../main.tsx"), "utf8");
eq(mainSource.startsWith('import "./lib/compat";'), true, "installs WebKit compatibility before application imports");
const legacyObject = function LegacyObject() {} as unknown as ObjectConstructor & {
  hasOwn?: (value: object, property: PropertyKey) => boolean;
};
installObjectHasOwnPolyfill(legacyObject);
eq(legacyObject.hasOwn?.({ own: true }, "own"), true, "Object.hasOwn polyfill accepts own properties");
eq(legacyObject.hasOwn?.(Object.create({ inherited: true }), "inherited"), false, "Object.hasOwn polyfill rejects inherited properties");
for (const file of ["../components/VirtualMenu.tsx", "../components/WorkspacePanel.tsx", "../components/editors/HljsDiff.tsx"]) {
  const source = readFileSync(resolve(testDir, file), "utf8");
  const fileParts = file.split("/");
  const label = fileParts[fileParts.length - 1];
  eq(
    source.includes("directDomUpdates: true") &&
      source.includes("ref={virtualizer.containerRef}") &&
      !source.includes("transform: `translateY(${row.start}px)`"),
    true,
    `${label} avoids measurement-triggered React update loops`,
  );
}

const err = new TypeError("invalid argument");
err.stack = "TypeError: invalid argument\n    at submit (src/App.tsx:12:3)";
const payload = buildCrashPayload("unhandledrejection", err, "component stack");

eq(normalizeCrashError("boom"), { errorType: "string", errorMessage: "boom" }, "normalizes string reasons");
eq(topFrameFromStack(err.stack), "at submit (src/App.tsx:12:3)", "extracts top app frame");
eq(payload.kind, "exception", "unhandled rejection is a nonfatal exception kind");
eq(payload.source, "frontend.global", "global handler payload identifies source");
eq(payload.errorType, "TypeError", "captures error type");
eq(payload.componentStack, "component stack", "captures component stack");
eq(payload.message.includes("[unhandledrejection]"), true, "keeps human-readable message");
eq(shouldReportGlobalCrashEvent({ defaultPrevented: false }), true, "reports unhandled global events by default");
eq(shouldReportGlobalCrashEvent({ defaultPrevented: true }), false, "ignores global events already handled by a filter");
eq(
  shouldReportGlobalCrashEvent({ defaultPrevented: false, message: "ResizeObserver loop limit exceeded" }),
  false,
  "ignores Chromium ResizeObserver loop limit notices",
);
eq(
  shouldReportGlobalCrashEvent({ defaultPrevented: false, message: "Minified React error #520; recovered synchronously" }),
  false,
  "suppresses React's recoverable concurrent-render diagnostic",
);
eq(
  shouldReportGlobalCrashEvent({
    defaultPrevented: false,
    message: "ResizeObserver loop completed with undelivered notifications.",
  }),
  false,
  "ignores Chromium ResizeObserver undelivered notification notices",
);
eq(isOpaqueScriptErrorEvent({ defaultPrevented: false, message: "Script error." }), true, "identifies locationless opaque script errors");
eq(
  isOpaqueScriptErrorEvent({ defaultPrevented: false, message: "Script error.", filename: "wails://wails/assets/index.js" }),
  false,
  "keeps located script errors out of opaque grouping",
);
const opaqueHint = opaqueScriptFingerprintHint(
  "wails://wails.localhost/tabs/123456789?token=private#abcdef123456",
  [{ t: 1, cat: "tab hydration", msg: "private path /Users/alice/project" }],
  "0123456789abcdefdeadbeef",
);
eq(opaqueHint, "build:0123456789abcdef|view:wails://wails.localhost/tabs/_|cats:tab_hydration", "opaque grouping uses stable safe context");
eq(opaqueHint.includes("alice"), false, "opaque grouping never includes breadcrumb messages");
eq(
  shouldReportGlobalCrashEvent({ defaultPrevented: false, error: new Error("ResizeObserver loop limit exceeded") }),
  false,
  "ignores ResizeObserver notices delivered through ErrorEvent.error",
);
eq(
  shouldReportGlobalCrashEvent({
    defaultPrevented: false,
    message: "",
    error: new Error("ResizeObserver loop limit exceeded"),
  }),
  false,
  "checks ErrorEvent.error when ErrorEvent.message is empty",
);
eq(
  shouldReportGlobalCrashEvent({
    defaultPrevented: false,
    message: "Uncaught Error",
    error: new Error("ResizeObserver loop limit exceeded"),
  }),
  false,
  "checks ErrorEvent.error when ErrorEvent.message is a wrapper",
);
eq(
  globalCrashReportReason({
    defaultPrevented: false,
    message: "Script error.",
    filename: "wails://wails/assets/index-abc123.js",
    lineno: 42,
    colno: 7,
  }),
  "Script error.\nfilename=wails://wails/assets/index-abc123.js lineno=42 colno=7",
  "adds script location to opaque window.error messages",
);
eq(
  globalCrashReportReason({
    defaultPrevented: false,
    message: "Script error.",
  }),
  "Script error.",
  "keeps opaque script errors bare when WebView provides no location",
);

const perf: PerformanceSnapshot = {
  reason: "event loop lag 1300ms",
  uptimeMs: 42_000,
  visibility: "visible",
  focused: true,
  online: true,
  hardwareConcurrency: 10,
  deviceMemoryGb: 16,
  jsHeap: { usedMb: 700, totalMb: 780, limitMb: 900, usagePercent: 77.7 },
  eventLoopLag: { currentMs: 1300, maxMs: 1300, avgMs: 220, samples: 6 },
  longTasks: {
    count: 3,
    totalMs: 1800,
    maxMs: 900,
    recent: [
      { startMs: 40_000, durationMs: 900 },
      { startMs: 41_000, durationMs: 500 },
    ],
  },
  connection: { effectiveType: "4g", rttMs: 50, downlinkMbps: 20, saveData: false },
};
const perfPayload = buildPerformancePayload(perf);
eq(perfPayload.kind, "performance", "performance pressure reports use performance kind");
eq(perfPayload.source, "frontend.performance", "performance pressure reports identify source");
eq(perfPayload.label, "performance.lag", "performance pressure reports partition by stable pressure label");
eq(perfPayload.errorType, "PerformancePressure", "performance pressure reports use a stable error type");
eq(perfPayload.errorMessage.includes("1300"), false, "performance fingerprint message avoids dynamic durations");
eq(perfPayload.label.includes("1300"), false, "performance fingerprint label avoids dynamic durations");
eq(formatPerformanceContext(perf).includes("long tasks: 3"), true, "formats long task context");
eq(perfPayload.message.includes("event loop lag 1300ms"), true, "payload message keeps lag context");
eq(performanceLabelForReason("long task 900ms"), "performance.longtask", "labels long task pressure");
eq(performanceLabelForReason("js heap 87% of limit"), "performance.heap", "labels heap pressure");
eq(shouldRecordLongTaskSample(14_000, 900, 15_000), false, "ignores startup long tasks before grace ends");
eq(shouldRecordLongTaskSample(16_000, 40, 15_000), false, "ignores short long-task observer entries");
eq(shouldRecordLongTaskSample(16_000, 900, 15_000), true, "records post-grace long tasks");
eq(shouldRecordLongTaskSample(60_000, 900, 15_000, true, 20_000), false, "ignores long tasks while the window is hidden");
eq(shouldRecordLongTaskSample(23_000, 900, 15_000, false, 20_000), false, "ignores long tasks immediately after visibility resumes");
eq(shouldRecordLongTaskSample(26_000, 900, 15_000, false, 20_000), true, "records long tasks after the visibility resume grace period");
eq(shouldRecordLongTaskSample(570_000, 92, 15_000, false, 20_000, false), false, "ignores long tasks while unfocused");
eq(shouldPromptForLongTasks({ count: 1, totalMs: 850, maxMs: 850 }), true, "prompts on a single 800ms+ long task");
eq(
  shouldPromptForLongTasks({ count: 16, totalMs: 1_584, maxMs: 237 }),
  false,
  "tolerates streaming-render bursts below the 3s cumulative budget",
);
eq(shouldPromptForLongTasks({ count: 16, totalMs: 3_100, maxMs: 237 }), true, "prompts past the 3s cumulative budget");
eq(shouldPromptForLongTasks({ count: 2, totalMs: 3_100, maxMs: 790 }), false, "cumulative path needs at least 3 tasks");
eq(shouldPromptForEventLoopLag([6_007]), false, "ignores an isolated lag spike without long-task evidence");
eq(shouldPromptForEventLoopLag([1_350, 1_420]), true, "prompts on consecutive lag samples");
eq(
  shouldPromptForEventLoopLag([1_350], { count: 1, totalMs: 900, maxMs: 900 }),
  true,
  "prompts on a lag spike corroborated by a blocking long task",
);
eq(
  shouldPromptForEventLoopLag([1_350], { count: 2, totalMs: 300, maxMs: 180 }),
  false,
  "does not treat unrelated short long tasks as lag corroboration",
);

eq(formatLongTaskAttribution("self", [{ containerType: "window" }]), "", "hides the no-signal self/window attribution");
eq(formatLongTaskAttribution("unknown", undefined), "", "hides unknown attribution");
eq(
  formatLongTaskAttribution("cross-origin-descendant", [{ containerType: "iframe", containerSrc: "https://embed.example" }]),
  "cross-origin-descendant iframe:https://embed.example",
  "surfaces cross-context culprits with their container",
);

const trace: ProfilerTrace = {
  resources: ["wails://wails/assets/vendor-markdown.js"],
  frames: [
    { name: "post", resourceId: 0, line: 1, column: 130216 },
    { name: "tick", resourceId: 0, line: 9 },
    { name: "" },
  ],
  stacks: [{ frameId: 0 }, { frameId: 1, parentId: 0 }, { frameId: 2 }],
  samples: [
    { timestamp: 1_000, stackId: 0 },
    { timestamp: 1_010, stackId: 0 },
    { timestamp: 1_020, stackId: 1 },
    { timestamp: 5_000, stackId: 0 }, // outside every long-task window
    { timestamp: 1_030 }, // idle sample without a stack
    { timestamp: 1_040, stackId: 2 },
  ],
};
eq(
  aggregateLongTaskProfile(trace, [{ startMs: 990, durationMs: 100 }]),
  [
    { label: "post (wails://wails/assets/vendor-markdown.js:1:130216)", samples: 2 },
    { label: "tick (wails://wails/assets/vendor-markdown.js:9)", samples: 1 },
    { label: "(anonymous)", samples: 1 },
  ],
  "counts leaf frames for samples inside long-task windows",
);
eq(aggregateLongTaskProfile(trace, []), [], "returns nothing without long-task windows");
eq(
  aggregateLongTaskProfile(trace, [{ startMs: 990, durationMs: 100 }], 1),
  [{ label: "post (wails://wails/assets/vendor-markdown.js:1:130216)", samples: 2 }],
  "caps the frame list at maxFrames",
);

const framesSnapshot: PerformanceSnapshot = {
  ...perf,
  longTasks: {
    count: 1,
    totalMs: 900,
    maxMs: 900,
    recent: [{ startMs: 40_000, durationMs: 900, attribution: "cross-origin-descendant" }],
  },
  longTaskFrames: [{ label: "post (vendor-markdown.js:1)", samples: 42 }],
};
eq(
  formatPerformanceContext(framesSnapshot).includes("900ms @ 40.0s (cross-origin-descendant)"),
  true,
  "recent long tasks carry their attribution",
);
eq(
  formatPerformanceContext(framesSnapshot).includes("long task top frames (sampled):\n  42x post (vendor-markdown.js:1)"),
  true,
  "formats sampled top frames into the report context",
);
eq(
  formatPerformanceContext(perf).includes("long task top frames"),
  false,
  "omits the frames section when no profile was captured",
);

eq(shouldRecordEventLoopLagSample(true, 60_000), false, "ignores event-loop lag while the window is hidden");
eq(shouldRecordEventLoopLagSample(false, 3_000), false, "ignores event-loop lag immediately after visibility resumes");
eq(shouldRecordEventLoopLagSample(false, 6_000), true, "records event-loop lag after the visibility resume grace period");
eq(shouldRecordEventLoopLagSample(false, 60_000, false), false, "ignores event-loop lag while unfocused");
eq(
  shouldRecordEventLoopLagSample(false, 60_000, true, 3_000),
  false,
  "ignores event-loop lag immediately after focus resumes",
);
eq(shouldRecordEventLoopLagSample(false, 60_000, true, 6_000), true, "records event-loop lag once both resume grace windows pass");

eq(shouldPromptForPerformanceLabel(false, 11 * 60_000, false), true, "prompts an unhandled label past cooldown while visible");
eq(shouldPromptForPerformanceLabel(true, 11 * 60_000, false), false, "suppresses an already reported or dismissed label");
eq(shouldPromptForPerformanceLabel(false, 5 * 60_000, false), false, "respects the prompt cooldown window");
eq(shouldPromptForPerformanceLabel(false, 11 * 60_000, true), false, "never prompts while the window is hidden");
eq(shouldPromptForPerformanceLabel(false, 11 * 60_000, false, false), false, "never prompts while unfocused");

{
  let interval: (() => void) | undefined;
  let now = 0;
  let focused = true;
  let promptPainted = false;
  const previousWindow = (globalThis as any).window;
  const previousDocument = (globalThis as any).document;
  const previousPerformance = (globalThis as any).performance;
  const previousPerformanceObserver = (globalThis as any).PerformanceObserver;
  (globalThis as any).performance = { now: () => now };
  // Listeners are intentionally no-ops: this exercises the sampler's own
  // hidden/unfocused self-observation, i.e. the case where a throttled tick
  // runs before the visibilitychange/focus task is delivered (the race behind
  // the field reports #6419/#5909).
  (globalThis as any).window = {
    runtime: {},
    location: { protocol: "app:", host: "test", pathname: "/", hash: "" },
    addEventListener: () => {},
    setInterval: (cb: () => void) => {
      interval = cb;
      return 1;
    },
  };
  (globalThis as any).document = {
    visibilityState: "visible",
    hasFocus: () => focused,
    addEventListener: () => {},
    getElementById: () => {
      promptPainted = true;
      return null;
    },
  };
  (globalThis as any).PerformanceObserver = undefined;
  installPerformancePressureMonitor();
  now = 26_000;
  interval?.();
  eq(promptPainted, false, "first post-grace event-loop tick primes without reporting startup backlog");

  // Hidden-view timer throttling defers ticks; when the view is shown again the
  // overdue tick can run before any visibilitychange handler. The accumulated
  // delay must read as suspension, not as an event-loop lag report.
  now = 27_000;
  interval?.(); // records a 0ms sample in the steady visible state
  (globalThis as any).document.visibilityState = "hidden";
  now = 47_000;
  interval?.(); // hidden tick: observed hidden, sample dropped (visibilitychange never delivered)
  (globalThis as any).document.visibilityState = "visible";
  now = 49_500;
  try {
    interval?.(); // resume-boundary tick, 1.5s overdue, visibilitychange still not delivered
  } catch {
    // a regressed sampler paints into the stubbed DOM and throws; the eq below reports it
  }
  eq(promptPainted, false, "resume-boundary tick does not report suspended-timer delay as event-loop lag");

  now = 50_500;
  interval?.(); // re-primes after the restart
  now = 51_500;
  interval?.();
  now = 52_500;
  interval?.();
  now = 53_500;
  interval?.();
  now = 54_500;
  interval?.(); // grace over, steady 0ms samples resume

  // Focus-only cycle, self-observed: the window loses focus (a throttled tick
  // observes it before any blur task), the app naps, and on refocus the overdue
  // tick runs before the focus task. Without focus tracking this reads as a
  // multi-second lag spike and prompts (the #6138 path #6424 must absorb).
  focused = false;
  now = 59_000;
  interval?.(); // unfocused tick: arms the resume restart, records nothing
  focused = true;
  now = 61_500;
  try {
    interval?.(); // refocus-boundary tick, 1.5s overdue, focus task not yet delivered
  } catch {
    // a regressed sampler paints into the stubbed DOM and throws; the eq below reports it
  }
  eq(promptPainted, false, "refocus-boundary tick does not report napped-timer delay as event-loop lag");

  now = 62_600;
  interval?.(); // re-primes
  now = 63_600;
  interval?.();
  now = 64_600;
  interval?.();
  now = 65_600;
  interval?.();
  now = 66_600;
  interval?.(); // both grace windows over, steady samples resume

  // A single delayed callback can still be a timer discontinuity. It is held
  // until the next delayed sample (or a long-task entry) corroborates the freeze.
  let promptAttempted = false;
  (globalThis as any).document.getElementById = () => {
    promptAttempted = true;
    throw new Error("stop before painting into the stubbed DOM");
  };
  now = 112_600;
  try {
    interval?.(); // isolated 45s delay: not enough evidence by itself
  } catch {
    // paint intentionally stopped at getElementById
  }
  eq(promptAttempted, false, "an isolated settled-state timer discontinuity does not prompt");
  now = 115_100;
  try {
    interval?.(); // a second consecutive 1.5s delay corroborates sustained lag
  } catch {
    // paint intentionally stopped at getElementById
  }
  eq(promptAttempted, true, "consecutive settled-state lag still prompts");
  (globalThis as any).window = previousWindow;
  (globalThis as any).document = previousDocument;
  (globalThis as any).performance = previousPerformance;
  (globalThis as any).PerformanceObserver = previousPerformanceObserver;
}

const reportedPerf = serializeReportedPerf(new Set(["performance.lag"]), "abc123");
eq([...parseReportedPerf(reportedPerf, "abc123")], ["performance.lag"], "round-trips reported labels for the same build");
eq([...parseReportedPerf(reportedPerf, "def456")], [], "re-surfaces reported labels on a new build");
eq([...parseReportedPerf(null, "abc123")], [], "tolerates missing storage");
eq([...parseReportedPerf("{not json", "abc123")], [], "tolerates corrupt storage");

{
  const originalNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  const previousWindow = (globalThis as any).window;
  const previousHTMLElement = (globalThis as any).HTMLElement;
  const setNavigator = (value: unknown) =>
    Object.defineProperty(globalThis, "navigator", { value, configurable: true });

  setNavigator({ clipboard: { writeText: async () => {} } });
  eq(await writeClipboardText("report"), true, "copy reports success through the async clipboard API");

  const rejectingClipboard = {
    clipboard: {
      writeText: async () => {
        throw new Error("denied");
      },
    },
  };
  setNavigator(rejectingClipboard);
  let bridgeCalls = 0;
  (globalThis as any).window = {
    runtime: {
      ClipboardSetText: async (value: string) => {
        bridgeCalls += 1;
        return value.length > 0;
      },
    },
  };
  eq(await writeClipboardText("report"), true, "copy falls back to the Wails native clipboard bridge when the clipboard API rejects");
  eq(bridgeCalls, 1, "the rejected clipboard write goes through the native bridge exactly once");

  setNavigator(rejectingClipboard);
  const execCommands: string[] = [];
  (globalThis as any).window = {};
  (globalThis as any).HTMLElement = class {};
  (globalThis as any).document = {
    activeElement: undefined,
    getSelection: () => null,
    createElement: () => ({ value: "", style: {}, setAttribute: () => {}, select: () => {}, remove: () => {} }),
    body: { appendChild: () => {} },
    execCommand: (command: string) => {
      execCommands.push(command);
      return true;
    },
  };
  eq(await writeClipboardText("report"), true, "copy falls back to execCommand when both the clipboard API and bridge are unavailable");
  eq(execCommands, ["copy"], "the last-resort path drives the execCommand copy");

  // Some WebViews reject execCommand("copy") with NotAllowedError. It must
  // surface as a resolved `false`, never a thrown rejection — otherwise the crash
  // overlay's Copy button stays disabled (the #6388 unresponsive symptom).
  let removed = false;
  (globalThis as any).document = {
    activeElement: undefined,
    getSelection: () => null,
    createElement: () => ({ value: "", style: {}, setAttribute: () => {}, select: () => {}, remove: () => { removed = true; } }),
    body: { appendChild: () => {} },
    execCommand: () => {
      throw new DOMException("not allowed", "NotAllowedError");
    },
  };
  let threw = false;
  let result: boolean | undefined;
  try {
    result = await writeClipboardText("report");
  } catch {
    threw = true;
  }
  eq(threw, false, "writeClipboardText never rejects when execCommand throws");
  eq(result, false, "an execCommand that throws resolves to a failed copy");
  eq(removed, true, "the hidden textarea is still cleaned up when execCommand throws");
  delete (globalThis as any).document;

  (globalThis as any).window = previousWindow;
  if (previousHTMLElement === undefined) delete (globalThis as any).HTMLElement;
  else (globalThis as any).HTMLElement = previousHTMLElement;
  if (originalNavigator) Object.defineProperty(globalThis, "navigator", originalNavigator);
  else delete (globalThis as any).navigator;
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

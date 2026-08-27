// Run: pnpm exec tsx src/__tests__/transcript-native-scrollbar.test.ts

import { deepEqual, equal } from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  hasPendingTranscriptGeometry,
  isNativeVerticalScrollbarPointer,
  measureTranscriptVirtuosoItem,
} from "../lib/transcriptNativeScrollbar";
import { noteTranscriptRowMeasurement, setTranscriptScrollDiagnosticSink } from "../lib/transcriptScrollProbe";
import { advanceViewportPagePermit, grantsNativeScrollbarPagePermit } from "../lib/useTranscriptNavigationSurface";

let passed = 0;
function check(actual: unknown, expected: unknown, label: string) {
  equal(actual, expected, label);
  process.stdout.write(`  PASS  ${label}\n`);
  passed += 1;
}

const dom = new JSDOM('<div class="transcript"><div id="row" data-index="44" data-item-index="1000044" data-logical-index="44" data-row-kind="answer" data-estimated-size="1800" data-known-size="160" data-content-revision="3"><button aria-expanded="false"></button></div></div>');
const transcript = dom.window.document.querySelector<HTMLElement>(".transcript")!;
const row = dom.window.document.querySelector<HTMLElement>("#row")!;
Object.defineProperties(transcript, {
  clientHeight: { configurable: true, value: 600 },
  scrollHeight: { configurable: true, value: 6000 },
  offsetWidth: { configurable: true, value: 1000 },
  clientWidth: { configurable: true, value: 980 },
  clientLeft: { configurable: true, value: 10 },
});
transcript.getBoundingClientRect = () => ({
  x: 100,
  y: 0,
  width: 1000,
  height: 600,
  top: 0,
  right: 1100,
  bottom: 600,
  left: 100,
  toJSON: () => ({}),
});

process.stdout.write("\ntranscript native scrollbar\n");
check(isNativeVerticalScrollbarPointer(transcript, { button: 0, clientX: 1095 }), true, "left-button in the right native gutter starts the lock");
check(isNativeVerticalScrollbarPointer(transcript, { button: 0, clientX: 1085 }), false, "left-button in chat content does not start the lock");
check(isNativeVerticalScrollbarPointer(transcript, { button: 1, clientX: 1095 }), false, "middle-button autoscroll is not classified as thumb dragging");
check(grantsNativeScrollbarPagePermit(400, 300, true, false), true, "an observed upward native drag grants one page permit");
check(grantsNativeScrollbarPagePermit(300, 200, true, true), false, "the same native drag cannot grant a second page permit");
check(grantsNativeScrollbarPagePermit(400, 300, false, false), false, "scroll movement without a native drag grants no permit");
let viewportPermit = advanceViewportPagePermit(0, 0);
check(viewportPermit, 1, "one upward gesture grants a viewport page permit");
viewportPermit = advanceViewportPagePermit(viewportPermit, 1);
check(viewportPermit, 2, "startReached consumes the granted viewport page permit");
check(advanceViewportPagePermit(viewportPermit, 0), 2, "wheel burst events cannot queue another page while the request is active");
viewportPermit = advanceViewportPagePermit(viewportPermit, 2);
check(advanceViewportPagePermit(viewportPermit, 1), 0, "request completion clears burst permits before prepend reaches the start");
check(advanceViewportPagePermit(advanceViewportPagePermit(viewportPermit, 0), 1), 2, "a later user gesture can request one new page");

Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 600 });
check(isNativeVerticalScrollbarPointer(transcript, { button: 0, clientX: 1095 }), false, "an empty native gutter without overflow does not start the lock");

row.getBoundingClientRect = () => ({
  x: 0,
  y: 0,
  width: 800,
  height: 640,
  top: 0,
  right: 800,
  bottom: 640,
  left: 0,
  toJSON: () => ({}),
});
check(measureTranscriptVirtuosoItem(row, "offsetHeight", false), 640, "ordinary wheel path keeps real dynamic measurement");
check(measureTranscriptVirtuosoItem(row, "offsetHeight", true), 640, "completed rows keep their measured height during native thumb drag");
row.dataset.transcriptEstimate = "180";
check(measureTranscriptVirtuosoItem(row, "offsetHeight", true), 640, "completed rows never regress to a logical estimate");
delete row.dataset.knownSize;
check(measureTranscriptVirtuosoItem(row, "offsetHeight", true), 640, "completed rows do not fall back to an estimate when known size is absent");
row.dataset.knownSize = "160";
delete row.dataset.transcriptEstimate;
check(measureTranscriptVirtuosoItem(row, "offsetHeight", false), 640, "real measurement resumes after thumb release");

const pendingMarkdown = dom.window.document.createElement("div");
pendingMarkdown.dataset.transcriptGeometryPending = "true";
row.dataset.staticEstimate = "157";
row.appendChild(pendingMarkdown);
check(hasPendingTranscriptGeometry(row), true, "a lazy Markdown fallback marks transient row geometry");
check(measureTranscriptVirtuosoItem(row, "offsetHeight", true), 160, "native thumb drag freezes only pending row geometry at its last measured size");
check(measureTranscriptVirtuosoItem(row, "offsetHeight", false), 157, "pending Markdown keeps the state-aware initial seed");
pendingMarkdown.remove();
check(hasPendingTranscriptGeometry(row), false, "resolved Markdown releases transient geometry");
check(measureTranscriptVirtuosoItem(row, "offsetHeight", false), 640, "resolved Markdown resumes browser measurement");

const measurementEvents: Array<{ type: string; fields: Record<string, unknown> }> = [];
setTranscriptScrollDiagnosticSink((type, fields) => measurementEvents.push({ type, fields }));
noteTranscriptRowMeasurement(row, "offsetHeight", 640);
deepEqual(measurementEvents, [{
  type: "row-measure",
  fields: {
    rowIndex: 44,
    rowKind: "answer",
    estimatedSize: 1800,
    previousSize: 160,
    measuredSize: 640,
    sizeDelta: 480,
    contentRevision: 3,
    foldState: "closed",
    disclosureCount: 1,
  },
}], "row measurement records only geometry and fixed classifications");
passed += 1;
delete row.dataset.knownSize;
noteTranscriptRowMeasurement(row, "offsetHeight", 420);
deepEqual(measurementEvents[measurementEvents.length - 1], {
  type: "row-measure",
  fields: {
    rowIndex: 44,
    rowKind: "answer",
    estimatedSize: 1800,
    previousSize: undefined,
    measuredSize: 420,
    sizeDelta: -1380,
    contentRevision: 3,
    foldState: "closed",
    disclosureCount: 1,
  },
}, "first real measurement records its estimate delta with the logical row index");
passed += 1;
row.dataset.knownSize = "160";
noteTranscriptRowMeasurement(row, "offsetHeight", 160);
check(measurementEvents.length, 2, "unchanged row size emits no diagnostic event");
noteTranscriptRowMeasurement(row, "offsetWidth", 800);
check(measurementEvents.length, 2, "horizontal measurements emit no row-height diagnostic event");

process.stdout.write(`\n${passed} passed\n`);

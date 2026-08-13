// Run: pnpm exec tsx src/__tests__/transcript-native-scrollbar.test.ts

import { equal } from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  isNativeVerticalScrollbarPointer,
  measureTranscriptVirtuosoItem,
} from "../lib/transcriptNativeScrollbar";

let passed = 0;
function check(actual: unknown, expected: unknown, label: string) {
  equal(actual, expected, label);
  process.stdout.write(`  PASS  ${label}\n`);
  passed += 1;
}

const dom = new JSDOM('<div class="transcript"><div id="row" data-known-size="160"></div></div>');
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
check(measureTranscriptVirtuosoItem(row, "offsetHeight", true), 160, "native thumb drag keeps the existing Virtuoso size");
check(measureTranscriptVirtuosoItem(row, "offsetHeight", false), 640, "real measurement resumes after thumb release");

process.stdout.write(`\n${passed} passed\n`);

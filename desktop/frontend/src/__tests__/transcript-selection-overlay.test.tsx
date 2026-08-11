// Run: node --import tsx src/__tests__/transcript-selection-overlay.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { TranscriptSelectionOverlay } from "../components/TranscriptSelectionOverlay";
import { transcriptSelectionStore, type TranscriptSelectableRow } from "../lib/transcriptSelectionStore";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Range = dom.window.Range;
globalThis.MutationObserver = dom.window.MutationObserver;

let nextFrame = 1;
const frames = new Map<number, FrameRequestCallback>();
globalThis.requestAnimationFrame = (callback) => {
  const id = nextFrame;
  nextFrame += 1;
  frames.set(id, callback);
  return id;
};
globalThis.cancelAnimationFrame = (id) => {
  frames.delete(id);
};

async function flushFramesOnce() {
  const pending = Array.from(frames.values());
  frames.clear();
  await act(async () => {
    for (const callback of pending) callback(performance.now());
  });
}

async function drainFrames() {
  while (frames.size > 0) await flushFramesOnce();
}

const rect = {
  left: 20,
  right: 100,
  top: 30,
  bottom: 50,
  width: 80,
  height: 20,
  x: 20,
  y: 30,
  toJSON: () => ({}),
} as DOMRect;
let layoutReady = false;
Object.defineProperty(dom.window.Range.prototype, "getClientRects", {
  configurable: true,
  value: () => layoutReady ? [rect] : [],
});
Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
  configurable: true,
  value: () => rect,
});

const rows: TranscriptSelectableRow[] = [
  { rowKey: "a", sourceText: "alpha", contentRevision: 1, resolveText: async () => "alpha" },
  { rowKey: "b", sourceText: "bravo", contentRevision: 1, resolveText: async () => "bravo" },
];
transcriptSelectionStore.clear("test-reset");
transcriptSelectionStore.beginNative("tab-overlay");
transcriptSelectionStore.promoteToLogical(
  "tab-overlay",
  { rowKey: "a", textOffset: 0, affinity: "forward" },
  { rowKey: "b", textOffset: 5, affinity: "forward" },
  rows,
);

console.log("\ntranscript logical selection overlay");

const root = createRoot(document.getElementById("root")!);
await act(async () => {
  root.render(
    <div className="transcript__virtual-sizer">
      <TranscriptSelectionOverlay tabId="tab-overlay" scrollElement={null} virtualRevision="a:0|b:20" />
      <div className="transcript__row" data-row-key="a"><div data-transcript-selectable="message">alpha</div></div>
      <div className="transcript__row" data-row-key="b"><div data-transcript-selectable="message">bravo</div></div>
    </div>,
  );
});

await flushFramesOnce();
ok(document.querySelectorAll(".transcript-selection-overlay__rect").length === 0, "a pre-layout range does not draw zero-sized rectangles");
ok(frames.size > 0, "a selected mounted range schedules a stabilization paint");

layoutReady = true;
await drainFrames();
ok(document.querySelectorAll(".transcript-selection-overlay__rect").length > 0, "the stabilization paint restores logical selection highlighting");

await act(async () => root.unmount());
transcriptSelectionStore.clear("test-cleanup");
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

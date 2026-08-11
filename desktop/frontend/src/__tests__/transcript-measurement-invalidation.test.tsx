// Run: tsx src/__tests__/transcript-measurement-invalidation.test.tsx

import { JSDOM } from "jsdom";
import React, { useRef } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { useTranscriptMeasurementInvalidation } from "../lib/useTranscriptMeasurementInvalidation";
import { EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT } from "../lib/transcriptHeightCache";

let passed = 0;
let failed = 0;
function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\ntranscript measurement invalidation");
const dom = new JSDOM("<!doctype html><html><body><div id='root'></div></body></html>", { pretendToBeVisual: true });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.MutationObserver = dom.window.MutationObserver;
globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window);
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

let allowMeasure = false;
let measureCalls = 0;
let reconcileCalls = 0;
let idleListener: (() => void) | null = null;
const virtualizer = { measure: () => { measureCalls += 1; } };

function Harness() {
  const scrollRef = useRef<HTMLDivElement>(null);
  const layoutSnapshotRef = useRef(EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT);
  useTranscriptMeasurementInvalidation({
    scrollRef,
    layoutSnapshotRef,
    virtualizer: virtualizer as never,
    selectionActive: false,
    canMeasure: () => allowMeasure,
    onMeasureIdle: (listener) => {
      idleListener = listener;
      return () => { idleListener = null; };
    },
    captureViewportAnchor: () => ({ rowKey: "tab:row", viewportOffset: 12, generation: 1 }),
    reconcileViewportAnchor: () => {
      reconcileCalls += 1;
      return true;
    },
  });
  return <div ref={scrollRef} style={{ width: 600, fontSize: 14 }} />;
}

const root = createRoot(document.getElementById("root")!);
await act(async () => { root.render(<Harness />); });
eq(measureCalls, 0, "layout invalidation waits while a user gesture owns scrolling");

allowMeasure = true;
await act(async () => {
  idleListener?.();
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
});
eq(measureCalls, 1, "idle flush measures one pending layout invalidation");
eq(reconcileCalls, 1, "idle flush restores the captured viewport anchor");

await act(async () => {
  idleListener?.();
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
});
eq(measureCalls, 1, "ordinary gesture idle does not trigger an unconditional full measure");

await act(async () => { root.unmount(); });
dom.window.close();
console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

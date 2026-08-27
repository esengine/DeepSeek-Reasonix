// Run: tsx src/__tests__/transcript-list-geometry-observer.test.tsx

import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useTranscriptListGeometryObserver } from "../lib/useTranscriptListGeometryObserver";

const dom = new JSDOM(`<!doctype html><html><body>
  <div id="root"></div>
  <div id="scroll"><div class="transcript__virtual-sizer"><div class="transcript__row"></div></div></div>
</body></html>`, { pretendToBeVisual: true, url: "http://localhost/" });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Element = dom.window.Element;
globalThis.Node = dom.window.Node;

class TestResizeObserver {
  static current: TestResizeObserver | null = null;
  observed: Element | null = null;
  disconnected = false;

  constructor(private readonly callback: ResizeObserverCallback) {
    TestResizeObserver.current = this;
  }

  observe(element: Element) { this.observed = element; }
  unobserve() {}
  disconnect() { this.disconnected = true; }
  trigger() { this.callback([], this as unknown as ResizeObserver); }
}
globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver;

const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
const sizer = scrollElement.querySelector<HTMLElement>(".transcript__virtual-sizer")!;
const row = scrollElement.querySelector<HTMLElement>(".transcript__row")!;
let nativeScrollHeight = 2_000;
Object.defineProperty(scrollElement, "scrollHeight", {
  configurable: true,
  get: () => nativeScrollHeight,
});
let height = 1_000;
sizer.getBoundingClientRect = () => ({
  x: 0, y: 0, top: 0, left: 0, right: 800, bottom: height,
  width: 800, height, toJSON: () => ({}),
});
let readerObservations = 0;
let geometryRevisions = 0;

function Probe() {
  useTranscriptListGeometryObserver({
    scrollElement,
    enabled: true,
    surfaceKey: "surface-a",
    noteGeometryChange: () => { geometryRevisions += 1; },
    observeReaderExtent: () => { readerObservations += 1; return false; },
  });
  return null;
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe />));
assert.equal(TestResizeObserver.current?.observed, sizer, "the rendered Virtuoso sizer is observed");

height = 1_143;
await act(async () => TestResizeObserver.current?.trigger());
assert.equal(readerObservations, 1, "a laid-out height change rechecks the reader anchor before paint");
assert.equal(geometryRevisions, 1, "the same height change publishes one geometry revision");

await act(async () => TestResizeObserver.current?.trigger());
assert.equal(readerObservations, 1, "an unchanged ResizeObserver delivery does not recheck the reader anchor");
assert.equal(geometryRevisions, 1, "an unchanged delivery does not publish another revision");

row.style.transform = "translateY(427px)";
await act(async () => { await Promise.resolve(); });
assert.equal(readerObservations, 2, "a range style mutation rechecks visual reader displacement");
assert.equal(geometryRevisions, 1, "a range mutation with the same native extent emits no geometry revision");

nativeScrollHeight = 3_280;
row.style.transform = "translateY(1707px)";
await act(async () => { await Promise.resolve(); });
assert.equal(readerObservations, 3, "a translated range extent change still rechecks the reader anchor");
assert.equal(geometryRevisions, 2,
  "a native extent change with the same mounted-list height publishes one deferred geometry revision");

await act(async () => root.unmount());
assert.equal(TestResizeObserver.current?.disconnected, true, "the list observer disconnects on cleanup");
dom.window.close();
console.log("transcript list geometry observer tests passed");

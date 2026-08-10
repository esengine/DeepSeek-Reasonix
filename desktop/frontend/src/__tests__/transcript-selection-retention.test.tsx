// Run: node --import tsx src/__tests__/transcript-selection-retention.test.tsx

import { JSDOM } from "jsdom";
import React, { useEffect } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { Range as VirtualRange } from "@tanstack/react-virtual";
import { useTranscriptSelectionRetention } from "../lib/useTranscriptSelectionRetention";
import type { TranscriptScrollMode } from "../lib/transcriptScrollController";

type RetentionApi = ReturnType<typeof useTranscriptSelectionRetention>;

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
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
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;

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

async function drainFrames() {
  while (frames.size > 0) {
    const pending = Array.from(frames.entries());
    frames.clear();
    await act(async () => {
      for (const [, callback] of pending) callback(performance.now());
    });
  }
}

const rowIndexByKey = new Map([
  ["row-a", 0],
  ["tool", 1],
  ["row-b", 2],
]);

function Harness({
  tabId,
  onReady,
  setMode,
}: {
  tabId: string;
  onReady: (api: RetentionApi) => void;
  setMode: (mode: TranscriptScrollMode, reason?: string) => void;
}) {
  // Transcript resets its scroll generation before the selection hook's own
  // tab-reset effect runs. The selection reset must not overwrite this mode.
  useEffect(() => setMode("tail-follow", "generation-reset"), [setMode, tabId]);
  const retention = useTranscriptSelectionRetention({
    tabId,
    revealSignal: 0,
    rowIndexByKey,
    setScrollMode: setMode,
    cancelStreamingScroll: () => {},
    captureViewportAnchor: () => ({ rowKey: "row-a", viewportOffset: 0, generation: 0 }),
    reconcileViewportAnchor: () => true,
  });
  useEffect(() => onReady(retention), [onReady, retention]);
  return (
    <div onPointerDownCapture={retention.onPointerDownCapture}>
      <div className="transcript__row" data-row-key="row-a"><div data-transcript-selectable="message">alpha</div></div>
      <div className="transcript__row" data-row-key="tool">tool</div>
      <div className="transcript__row" data-row-key="row-b"><div data-transcript-selectable="reasoning">bravo</div></div>
    </div>
  );
}

async function selectAcrossRows() {
  const first = document.querySelector<HTMLElement>("[data-row-key='row-a'] [data-transcript-selectable]")!;
  const last = document.querySelector<HTMLElement>("[data-row-key='row-b'] [data-transcript-selectable]")!;
  await act(async () => {
    first.dispatchEvent(new window.MouseEvent("pointerdown", { bubbles: true, button: 0 }));
    const range = document.createRange();
    range.setStart(first.firstChild!, 0);
    range.setEnd(last.firstChild!, 3);
    const selection = document.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new window.Event("selectionchange"));
    document.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0 }));
  });
}

console.log("\ntranscript selection retention");

const root = createRoot(document.getElementById("root")!);
let api: RetentionApi | null = null;
let mode: TranscriptScrollMode = "tail-follow";
const onReady = (next: RetentionApi) => { api = next; };
const setMode = (next: TranscriptScrollMode) => { mode = next; };

await act(async () => {
  root.render(<Harness tabId="tab-a" onReady={onReady} setMode={setMode} />);
});
await selectAcrossRows();
eq(mode, "native-selecting", "cross-row pointer selection owns scrolling while dragging");

await act(async () => {
  root.render(<Harness tabId="tab-b" onReady={onReady} setMode={setMode} />);
});
await drainFrames();
eq(mode, "tail-follow", "tab reset rejects delayed selection settle callbacks");

await selectAcrossRows();
await drainFrames();
const virtualRange: VirtualRange = { startIndex: 0, endIndex: 0, overscan: 0, count: 3 };
eq(api?.rangeExtractor(virtualRange), [0, 1, 2], "settled native selection retains its continuous row interval");
await act(async () => {
  document.dispatchEvent(new window.Event("copy", { bubbles: true }));
});
await drainFrames();
eq(document.getSelection()?.isCollapsed, true, "keyboard copy releases the native browser range after the copy event");
eq(api?.rangeExtractor(virtualRange), [0], "keyboard copy releases selection-only virtual rows");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

// Run: tsx src/__tests__/transcript-recovery-race.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { VirtuosoHandle } from "react-virtuoso";
import { useTranscriptVirtuosoRecovery } from "../lib/useTranscriptVirtuosoRecovery";
import type { TranscriptRow } from "../lib/transcriptRows";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function check(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript recovery races");

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div><div id="scroll"><div class="transcript__row" data-row-key="row-a"></div></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Element = dom.window.Element;
globalThis.Node = dom.window.Node;

let nextFrame = 1;
const frames = new Map<number, FrameRequestCallback>();
const requestFrame = (callback: FrameRequestCallback) => {
  const id = nextFrame;
  nextFrame += 1;
  frames.set(id, callback);
  return id;
};
const cancelFrame = (id: number) => void frames.delete(id);
globalThis.requestAnimationFrame = requestFrame;
globalThis.cancelAnimationFrame = cancelFrame;
dom.window.requestAnimationFrame = requestFrame;
dom.window.cancelAnimationFrame = cancelFrame;

async function flushFrames() {
  const pending = [...frames.entries()];
  frames.clear();
  await act(async () => pending.forEach(([, callback]) => callback(performance.now())));
}

const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
const rowElement = scrollElement.querySelector<HTMLElement>(".transcript__row")!;
scrollElement.getBoundingClientRect = () => ({ top: 0, bottom: 100, height: 100, left: 0, right: 800, width: 800, x: 0, y: 0, toJSON: () => ({}) });
rowElement.getBoundingClientRect = () => ({ top: 200, bottom: 300, height: 100, left: 0, right: 800, width: 800, x: 0, y: 200, toJSON: () => ({}) });
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, value: 500 });

const item: Item = { kind: "assistant", id: "a", text: "answer", reasoning: "", streaming: false };
const rows: TranscriptRow[] = [{ kind: "answer", key: "row-a", item }];
const scrollRef = { current: scrollElement };
const pinnedRef = { current: false };
const readyRef = { current: true };
let scrollByCalls = 0;
const virtuosoRef = {
  current: {
    scrollBy: () => { scrollByCalls += 1; },
    scrollToIndex: () => undefined,
  } as unknown as VirtuosoHandle,
};
let recovery: ReturnType<typeof useTranscriptVirtuosoRecovery> | undefined;

function Probe({ surfaceKey }: { surfaceKey: string }) {
  recovery = useTranscriptVirtuosoRecovery({
    surfaceKey,
    historyLayoutRevision: 0,
    rows,
    rowIndexByKey: new Map([["row-a", 0]]),
    scrollRef,
    pinnedRef,
    virtuosoRef,
    readyRef,
    scrollToBottom: () => undefined,
  });
  return null;
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe surfaceKey="surface-a" />));
await act(async () => recovery?.scheduleBlankViewportCheck());
await act(async () => root.render(<Probe surfaceKey="surface-b" />));
await flushFrames();
check(recovery?.resetKey === "surface-b:0", "surface switch cancels the previous blank-viewport watchdog");

await act(async () => recovery?.scheduleBlankViewportCheck());
await flushFrames();
await flushFrames();
check(recovery?.resetKey === "surface-b:1", "blank viewport schedules a controlled size-tree rebuild");
await act(async () => recovery?.handleItemsRendered(1));
await act(async () => root.render(<Probe surfaceKey="surface-c" />));
await flushFrames();
check(scrollByCalls === 0, "stale anchor correction cannot scroll the newly selected surface");

await act(async () => root.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript recovery race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript recovery race tests passed.`);

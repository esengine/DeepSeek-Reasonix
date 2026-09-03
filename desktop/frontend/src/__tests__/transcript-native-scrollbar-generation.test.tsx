// Run: tsx src/__tests__/transcript-native-scrollbar-generation.test.tsx
//
// A native-thumb transaction is bound to the surface generation it started
// in. Callers that advance the generation are expected to cancel it first,
// but the hook must not depend on that ordering: a late observe/finish from
// a previous generation must clean up silently instead of reusing the stale
// element or claiming the tail.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { TranscriptScrollEvent, TranscriptScrollMode } from "../lib/transcriptScrollArbiter";
import type { TranscriptTailSettle } from "../lib/transcriptTailSettle";
import { useTranscriptNativeScrollbarOwnership } from "../lib/useTranscriptNativeScrollbarOwnership";

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

console.log("\ntranscript native scrollbar generation fence");

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div><div id="scroll"></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Element = dom.window.Element;
globalThis.Node = dom.window.Node;

const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 600 });
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, value: 6_000 });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 1_000 });

const scrollRef = { current: scrollElement as HTMLDivElement | null };
const modeRef = { current: "manual" as TranscriptScrollMode };
const generationRef = { current: 7 };
const events: TranscriptScrollEvent[] = [];
let readerCancels = 0;
let deliveries = 0;
let tailSchedules = 0;
const tailSettle = {
  schedule: () => { tailSchedules += 1; },
  cancel: () => {},
} as unknown as TranscriptTailSettle;

let ownership: ReturnType<typeof useTranscriptNativeScrollbarOwnership> | undefined;
function Probe() {
  ownership = useTranscriptNativeScrollbarOwnership({
    scrollRef,
    modeRef,
    cancelReaderTransaction: () => { readerCancels += 1; },
    deliverScroll: () => { deliveries += 1; },
    dispatch: (event) => { events.push(event); return undefined; },
    tailSettle,
    generationRef,
  });
  return null;
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe />));

// Same generation: the transaction observes progress and finishes normally.
await act(async () => ownership!.begin(1, scrollElement));
check(readerCancels === 1, "beginning a thumb drag ends the reader transaction");
check(events.at(-1)?.type === "NATIVE_SCROLLBAR_BEGIN", "beginning a thumb drag dispatches NATIVE_SCROLLBAR_BEGIN");
check(scrollElement.dataset.nativeScrollbarDrag === "true", "the element carries the drag marker");
check(ownership!.isActive() === true, "the transaction is active within its generation");
scrollElement.scrollTop = 5_400;
await act(async () => ownership!.observe(scrollElement));
events.length = 0;
await act(async () => { ownership!.finish(1); });
check(deliveries === 1, "a same-generation finish delivers the final scroll sample");
check(events.some((event) => event.type === "NATIVE_SCROLLBAR_END" && event.claimTail === true),
  "forward progress to the bottom lets the same-generation finish claim the tail");
check(scrollElement.dataset.nativeScrollbarDrag === undefined, "a same-generation finish clears the drag marker");

// Generation advances underneath the transaction without a cancel.
events.length = 0;
deliveries = 0;
tailSchedules = 0;
scrollElement.scrollTop = 1_000;
await act(async () => ownership!.begin(2, scrollElement));
generationRef.current += 1;
events.length = 0;
check(ownership!.isActive() === false, "a transaction from a previous generation is no longer active");
scrollElement.scrollTop = 5_400;
await act(async () => ownership!.observe(scrollElement));
check(scrollElement.dataset.nativeScrollbarDrag === undefined,
  "a stale-generation observe clears the drag marker instead of recording progress");
check(events.length === 1 && events[0].type === "NATIVE_SCROLLBAR_END" && events[0].claimTail === false,
  "a stale-generation observe ends native ownership without claiming the tail");
let finished: boolean | undefined;
await act(async () => { finished = ownership!.finish(2); });
check(finished === false, "a stale-generation finish reports no transaction");
check(deliveries === 0 && tailSchedules === 0,
  "a stale-generation finish neither delivers a sample nor schedules tail settle");

// Finish before any observe on a stale generation: still silent cleanup.
events.length = 0;
scrollElement.scrollTop = 1_000;
await act(async () => ownership!.begin(3, scrollElement));
generationRef.current += 1;
await act(async () => { finished = ownership!.finish(3); });
check(finished === false && scrollElement.dataset.nativeScrollbarDrag === undefined,
  "a stale-generation finish without observe clears the marker and reports no transaction");
check(events.every((event) => event.type !== "NATIVE_SCROLLBAR_END" || event.claimTail === false),
  "no stale-generation path can claim the tail");

await act(async () => root.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript native scrollbar generation test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript native scrollbar generation tests passed.`);

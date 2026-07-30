// Run: tsx src/__tests__/scroll-manager.test.tsx

import { JSDOM } from "jsdom";
import React, { useEffect } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { useScrollManager } from "../lib/useScrollManager";

type ScrollManagerApi = ReturnType<typeof useScrollManager>;

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
  }
}

function Harness({ onReady }: { onReady: (api: ScrollManagerApi) => void }) {
  const manager = useScrollManager();
  useEffect(() => onReady(manager), [manager, onReady]);
  return (
    <div
      ref={manager.scrollRef}
      data-testid="transcript"
      onScroll={manager.onScroll}
      onWheelCapture={manager.onWheelIntent}
      onKeyDownCapture={manager.onKeyScrollIntent}
    />
  );
}

console.log("\nscroll manager manual intent");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.WheelEvent = dom.window.WheelEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);
let api: ScrollManagerApi | null = null;

await act(async () => {
  root.render(<Harness onReady={(next) => { api = next; }} />);
});

if (!api) throw new Error("scroll manager did not mount");
const transcript = document.querySelector<HTMLElement>("[data-testid='transcript']");
if (!transcript) throw new Error("transcript did not render");

let scrollTop = 900;
Object.defineProperty(transcript, "clientHeight", { configurable: true, value: 100 });
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
Object.defineProperty(transcript, "scrollTop", {
  configurable: true,
  get: () => scrollTop,
  set: (value) => { scrollTop = value; },
});

await act(async () => {
  api?.onScroll();
});
eq(api.stick.current, true, "manager starts pinned when the transcript is at the bottom");

await act(async () => {
  api?.onWheelIntent({ deltaX: 0, deltaY: 48 } as React.WheelEvent<HTMLElement>);
});
eq(api.stick.current, true, "wheel-down at the bottom keeps tail-follow enabled");

await act(async () => {
  const released = api?.onWheelIntent({ deltaX: 0, deltaY: -48 } as React.WheelEvent<HTMLElement>);
  eq(released, true, "wheel-up at the bottom releases auto-scroll immediately");
});
eq(api.stick.current, false, "manual wheel intent breaks the bottom pin before the native scroll event");

if (api.stick.current) {
  transcript.scrollTop = transcript.scrollHeight;
}
eq(scrollTop, 900, "a queued streaming auto-scroll would not yank after manual wheel intent");

scrollTop = 900;
await act(async () => {
  api!.stick.current = true;
  api?.onWheelIntent({ deltaX: 40, deltaY: 4 } as React.WheelEvent<HTMLElement>);
});
eq(api.stick.current, true, "horizontal-dominant wheel gestures do not break vertical tail-follow");

Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 100 });
scrollTop = 0;
await act(async () => {
  api!.stick.current = true;
  const released = api?.onWheelIntent({ deltaX: 0, deltaY: -48 } as React.WheelEvent<HTMLElement>);
  eq(released, false, "wheel intent is ignored when the transcript is not scrollable");
});
eq(api.stick.current, true, "short transcripts stay pinned after ignored wheel intent");

Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
scrollTop = 900;
await act(async () => {
  api!.stick.current = true;
  const released = api?.onWheelIntent({ deltaX: 0, deltaY: -48, ctrlKey: true } as React.WheelEvent<HTMLElement>);
  eq(released, false, "ctrl+wheel (trackpad pinch-zoom) is ignored, not treated as scroll intent");
});
eq(api.stick.current, true, "pinch-zoom gesture does not release tail-follow");

const editTextarea = document.createElement("textarea");
await act(async () => {
  api!.stick.current = true;
  const released = api?.onKeyScrollIntent({ key: "Home", target: editTextarea } as unknown as React.KeyboardEvent<HTMLElement>);
  eq(released, false, "Home pressed while editing a message textarea is not treated as scroll intent");
});
eq(api.stick.current, true, "editing an earlier message does not release the streaming tail-follow");

const plainDiv = document.createElement("div");
await act(async () => {
  api!.stick.current = true;
  const released = api?.onKeyScrollIntent({ key: "Home", target: plainDiv } as unknown as React.KeyboardEvent<HTMLElement>);
  eq(released, true, "Home pressed on a non-editable target still releases tail-follow");
});
eq(api.stick.current, false, "keyboard scroll intent from outside an editable field still breaks the bottom pin");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

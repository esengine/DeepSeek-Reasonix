// Run: tsx src/__tests__/resize-drag.test.ts

import { JSDOM } from "jsdom";
import { createPointerResizeLifecycle, createRafResizeUpdater } from "../lib/resizeDrag";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nresizeDrag");

const frames: FrameRequestCallback[] = [];
const originalRequestAnimationFrame = globalThis.requestAnimationFrame;
const originalCancelAnimationFrame = globalThis.cancelAnimationFrame;
globalThis.requestAnimationFrame = ((callback: FrameRequestCallback) => {
  frames.push(callback);
  return frames.length;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = ((id: number) => {
  frames[id - 1] = () => undefined;
}) as typeof cancelAnimationFrame;

try {
  const applied: Array<[string, string]> = [];
  const attrs: Record<string, string> = {};
  const updater = createRafResizeUpdater({
    target: {
      style: {
        setProperty(name: string, value: string) {
          applied.push([name, value]);
        },
      },
    },
    separator: {
      setAttribute(name: string, value: string) {
        attrs[name] = value;
      },
    },
    cssVar: "--pane-width",
  });

  updater.schedule(101.2);
  updater.schedule(148.7);
  eq(applied.length, 0, "live resize waits for animation frame");
  eq(frames.length, 1, "multiple pointer moves share one animation frame");
  frames[0](0);
  eq(applied.length, 1, "one frame applies one live resize");
  eq(applied[0]?.[0], "--pane-width", "live resize writes the configured CSS variable");
  eq(applied[0]?.[1], "149px", "live resize applies the latest rounded pixel value");
  eq(attrs["aria-valuenow"], "149", "live resize keeps separator ARIA in sync");

  frames.length = 0;
  const flushed: Array<[string, string]> = [];
  const flushUpdater = createRafResizeUpdater({
    target: {
      style: {
        setProperty(name: string, value: string) {
          flushed.push([name, value]);
        },
      },
    },
    cssVar: "--pane-width",
  });
  flushUpdater.schedule(600);
  frames[0](0);
  flushUpdater.schedule(420);
  eq(flushed[flushed.length - 1]?.[1], "600px", "pending final resize waits for animation frame before flush");
  flushUpdater.flush();
  eq(flushed[flushed.length - 1]?.[1], "420px", "flush applies the final resize before React state commits");

  frames.length = 0;
  const appliedValues: number[] = [];
  const callbackUpdater = createRafResizeUpdater({
    target: {
      style: {
        setProperty() {
          // CSS write is covered above; this block checks the synchronized callback contract.
        },
      },
    },
    cssVar: "--pane-width",
    onApply(value) {
      appliedValues.push(value);
    },
  });
  callbackUpdater.schedule(279.7);
  callbackUpdater.schedule(300.2);
  eq(appliedValues.length, 0, "live resize callback waits for animation frame");
  frames[0](0);
  eq(appliedValues.join(","), "300", "live resize callback receives the applied rounded value");

  const dom = new JSDOM('<!doctype html><button id="separator"></button>');
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  const separator = dom.window.document.getElementById("separator") as HTMLButtonElement;
  let capturedPointer: number | null = null;
  let finished = 0;
  let moved = 0;
  separator.setPointerCapture = (pointerId) => { capturedPointer = pointerId; };
  separator.hasPointerCapture = (pointerId) => capturedPointer === pointerId;
  separator.releasePointerCapture = (pointerId) => {
    capturedPointer = null;
    const lost = new dom.window.Event("lostpointercapture") as Event & { pointerId: number };
    Object.defineProperty(lost, "pointerId", { value: pointerId });
    separator.dispatchEvent(lost);
  };
  const pointerEvent = (type: string, pointerId: number) => {
    const event = new dom.window.Event(type) as Event & { pointerId: number };
    Object.defineProperty(event, "pointerId", { value: pointerId });
    return event;
  };
  const lifecycle = createPointerResizeLifecycle({
    separator,
    pointerId: 7,
    onMove: () => { moved += 1; },
    onFinish: () => { finished += 1; },
  });
  dom.window.dispatchEvent(pointerEvent("pointermove", 8));
  dom.window.dispatchEvent(pointerEvent("pointermove", 7));
  eq(moved, 1, "resize lifecycle ignores other pointers and delivers the active pointer");
  dom.window.dispatchEvent(pointerEvent("pointercancel", 7));
  eq(finished, 1, "pointer cancellation finishes the resize once");
  lifecycle.finish();
  separator.dispatchEvent(pointerEvent("lostpointercapture", 7));
  dom.window.dispatchEvent(new dom.window.Event("blur"));
  eq(finished, 1, "capture loss, blur, and component cleanup cannot finish a gesture twice");
  eq(capturedPointer, null, "resize finish releases active pointer capture");
  dom.window.close();
} finally {
  globalThis.requestAnimationFrame = originalRequestAnimationFrame;
  globalThis.cancelAnimationFrame = originalCancelAnimationFrame;
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

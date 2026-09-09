import assert from "node:assert/strict";
import React, { act, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { TranscriptKernelClockContext, useTranscriptKernel } from "../lib/useTranscriptKernel";
import { TranscriptTestClock } from "./transcript-test-clock";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document,
  HTMLElement: dom.window.HTMLElement, Element: dom.window.Element, IS_REACT_ACT_ENVIRONMENT: true });
const clock = new TranscriptTestClock();
const scroller = document.createElement("div");
const block = document.createElement("div");
block.dataset.transcriptBlockKey = "anchor";
scroller.dataset.transcriptBlockCount = "1";
scroller.append(block);
let scrollTop = 200, anchorTop = 180;
const writes: number[] = [];
Object.defineProperties(scroller, {
  scrollTop: { get: () => scrollTop, set: (value: number) => { scrollTop = value; writes.push(value); } },
  scrollHeight: { get: () => 2000 }, clientHeight: { get: () => 500 },
});
const rect = (top: number, height: number) => ({ top, bottom: top + height, left: 0, right: 800,
  width: 800, height, x: 0, y: top, toJSON: () => ({}) });
scroller.getBoundingClientRect = () => rect(0, 500);
block.getBoundingClientRect = () => rect(anchorTop - scrollTop, 100);
let current!: ReturnType<typeof useTranscriptKernel>;
function Probe() {
  current = useTranscriptKernel({ sessionKey: "source", geometryRevision: 1 });
  const attach = current.setScroller;
  useLayoutEffect(() => { attach(scroller); return () => attach(null); }, [attach]);
  return null;
}
const root = createRoot(document.getElementById("root")!);
try {
  await act(async () => root.render(React.createElement(TranscriptKernelClockContext.Provider,
    { value: clock }, React.createElement(Probe))));
  await act(async () => { current.beginGesture(); current.endGesture(); });
  writes.length = 0;
  await act(async () => {
    current.beginAnchorRestore();
    current.commitViewportGeometry(true);
    assert(clock.frames.size > 0, "ordinary geometry work is scheduled");
    anchorTop += 200;
    current.commitViewportGeometry(true, true);
    assert.equal(scrollTop, 400, "measured prefix and reader correction commit before the next paint");
    assert.equal(clock.frames.size, 0, "the synchronous commit revokes its pending older geometry frame");
  });
  await act(async () => clock.flushFrames());
  assert.deepEqual(writes, [400], "one measured commit emits exactly one physical correction");
  await act(async () => {
    current.beginAnchorRestore();
    current.beginGesture();
    anchorTop += 100;
    current.commitViewportGeometry(true, true);
  });
  assert.deepEqual(writes, [400], "native takeover forbids correction even at the before-paint boundary");
  await act(async () => {
    current.endGesture();
    current.beginAnchorRestore();
    current.commitViewportGeometry(true);
  });
  const stale = [...clock.frames.values()];
  await act(async () => root.unmount());
  stale.forEach(callback => callback(clock.time));
  assert.deepEqual(writes, [400], "queued geometry cannot write after its surface detaches");
  const inputRoot = createRoot(document.getElementById("root")!);
  scrollTop = 200; anchorTop = 180;
  await act(async () => inputRoot.render(React.createElement(TranscriptKernelClockContext.Provider,
    { value: clock }, React.createElement(Probe))));
  await act(async () => { current.onPointerDownCapture({ clientX: 100, pointerType: "touch" }); current.onTouchStartCapture(); scrollTop = 260; anchorTop = 240; current.onScroll(); window.dispatchEvent(new window.MouseEvent("pointerup")); current.onTouchEndCapture(); clock.flushFrames(); });
  assert.equal(current.kernel.userGestureActive, true, "touch release preserves ownership for momentum");
  await act(async () => clock.advance(319));
  await act(async () => { scrollTop = 320; anchorTop = 300; current.onScroll(); });
  await act(async () => clock.advance(319));
  assert.equal(current.kernel.userGestureActive, true, "momentum renews the existing input lease");
  await act(async () => clock.advance(1));
  assert.equal(current.kernel.userGestureActive, false, "ownership ends only after native progress becomes idle");
  const ownedAnchor = current.kernel.anchor;
  await act(async () => { scrollTop = 325; anchorTop = 600; current.onScroll(); });
  assert.deepEqual(current.kernel.anchor, ownedAnchor, "an idle layout scroll cannot overwrite the observed reading anchor");
  await act(async () => { current.onWheelCapture(); current.scrollToBottom(); });
  assert.equal(current.kernel.intent, "tail", "explicit jump-bottom supersedes the older input lease");
  assert.equal(current.kernel.userGestureActive, false);
  await act(async () => {
    scrollTop -= 54;
    const transaction = current.beginStructural("display-change");
    assert.equal(current.kernel.intent, "tail", "structural geometry cannot reinterpret a new bottom gap as reader intent");
    assert.equal(transaction?.status, "active");
  });
  await act(async () => {
    current.onPointerDownCapture({ clientX: 100, pointerType: "mouse" });
    window.dispatchEvent(new window.MouseEvent("pointerup"));
    current.onWheelCapture();
    clock.flushFrames();
  });
  assert.equal(current.kernel.userGestureActive, true, "an older pointer-release frame cannot end a newer wheel lease");
  await act(async () => {
    current.endGesture();
    current.onPointerDownCapture({ clientX: 799, pointerType: "mouse" });
    window.dispatchEvent(new window.MouseEvent("pointerup"));
    current.scrollToBottom();
    clock.flushFrames();
  });
  assert.equal(current.kernel.intent, "tail", "a delayed thumb release cannot re-enter reader intent after jump-bottom");
  assert.equal(current.kernel.userGestureActive, false);
  await act(async () => inputRoot.unmount());
  console.log("geometry commit: atomic paint, queued-work revocation, native takeover and disposal passed");
} finally { dom.window.close(); }

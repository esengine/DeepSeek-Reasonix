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
transcript.scrollTo = ((options: ScrollToOptions) => {
  if (typeof options.top === "number") {
    scrollTop = Math.max(0, Math.min(options.top, transcript.scrollHeight - transcript.clientHeight));
  }
}) as typeof transcript.scrollTo;

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

// A short upward move remains inside the ordinary near-bottom threshold. It
// must still latch manual mode after the gesture settles; otherwise the next
// streaming token can pull the reader back to the bottom.
scrollTop = 860;
await act(async () => {
  api!.onScroll();
});
eq(api!.stick.current, false, "short upward scroll keeps tail-follow disengaged inside the bottom threshold");
eq(api!.modeRef.current, "manual", "short upward scroll preserves manual mode");
await act(async () => {
  api!.gestureLastActivityRef.current -= 100;
  api!.onScrollEnd();
  api!.scrollToBottom(false, "stream");
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
});
eq(scrollTop, 860, "streaming cannot re-pin after a settled short upward gesture");

// User-owned downward scrolling may deliberately opt back into tail-follow,
// but only after reaching the physical bottom rather than merely its 80px
// proximity band.
scrollTop = 899;
await act(async () => {
  api!.onScroll();
});
eq(api!.stick.current, false, "manual latch remains one pixel above the physical bottom");
scrollTop = 900;
await act(async () => {
  api!.onScroll();
});
eq(api!.stick.current, true, "user scroll to the physical bottom restores tail-follow");
eq(api!.modeRef.current, "tail-follow", "physical bottom clears manual mode");

// Nested tables/code blocks hand their edge wheel to the transcript. A
// downward handoff at the physical bottom is harmless: it still owns the
// gesture (so compensating writers stay frozen), but must not detach the
// transcript from streaming tail-follow.
await act(async () => {
  const released = api!.onNestedScrollIntent(48);
  eq(released, false, "nested wheel-down at the bottom does not release tail-follow");
});
eq(api!.stick.current, true, "nested wheel-down at the bottom preserves the bottom pin");
eq(api!.modeRef.current, "tail-follow", "nested wheel-down at the bottom preserves tail-follow mode");

// If the final token grows the transcript while that harmless gesture lock is
// active, the rejected passive write must be replayed once at gesture idle.
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1040 });
await act(async () => {
  const wrote = api!.writeOffset("stream", transcript.scrollHeight);
  eq(wrote, false, "stream write is deferred while the harmless nested gesture is active");
  api!.gestureLastActivityRef.current -= 100;
  api!.onScrollEnd();
});
eq(scrollTop, 940, "deferred tail-follow replays to the current bottom after gesture idle");

// Row measurements use the same deferred path. This covers async Markdown,
// image and code-block growth that lands after the token frame itself.
await act(async () => {
  api!.onWheelIntent({ deltaX: 0, deltaY: 48 } as React.WheelEvent<HTMLElement>);
});
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1080 });
await act(async () => {
  api!.scheduleRepinIfWasPinned(0, "row-size");
  await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
});
eq(scrollTop, 940, "row-size repin stays silent during the active gesture");
await act(async () => {
  api!.gestureLastActivityRef.current -= 100;
  api!.onScrollEnd();
});
eq(scrollTop, 980, "row-size repin replays once after gesture idle");

// A real upward intent after a deferred passive write cancels the replay. The
// reader must remain in control even when no later token arrives.
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
scrollTop = 900;
await act(async () => {
  api!.onWheelIntent({ deltaX: 0, deltaY: 48 } as React.WheelEvent<HTMLElement>);
});
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1040 });
await act(async () => {
  eq(api!.writeOffset("stream", transcript.scrollHeight), false, "passive tail write queues during downward gesture");
  api!.onWheelIntent({ deltaX: 0, deltaY: -48 } as React.WheelEvent<HTMLElement>);
  api!.gestureLastActivityRef.current -= 100;
  api!.onScrollEnd();
});
eq(scrollTop, 900, "upward intent cancels a queued tail-follow replay");
eq(api!.modeRef.current, "manual", "upward intent keeps manual ownership after idle");

// Deferred work is scoped to the transcript generation. Switching tabs must
// discard the previous tab's pending replay even if its idle callback arrives.
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
scrollTop = 900;
await act(async () => {
  api!.setMode("tail-follow", "deferred-generation-test");
  api!.stick.current = true;
  api!.onWheelIntent({ deltaX: 0, deltaY: 48 } as React.WheelEvent<HTMLElement>);
});
Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1040 });
await act(async () => {
  eq(api!.writeOffset("stream", transcript.scrollHeight), false, "old generation queues a passive tail write");
  api!.resetGeneration("next-tab", 2);
});
eq(scrollTop, 900, "tab generation reset discards the previous transcript's replay");

Object.defineProperty(transcript, "scrollHeight", { configurable: true, value: 1000 });
scrollTop = 900;

// Scrollbar drags do not emit wheel intent. The first unowned scroll away from
// the physical bottom must establish the same manual latch for mouse users.
scrollTop = 870;
await act(async () => {
  api!.onScroll();
});
eq(api!.stick.current, false, "short native scrollbar drag disengages tail-follow without a wheel event");
eq(api!.modeRef.current, "manual", "native scrollbar drag enters manual mode");
scrollTop = 900;
await act(async () => {
  api!.onScroll();
});
eq(api!.stick.current, true, "scrollbar drag back to the physical bottom restores tail-follow");

// Creation's custom scrollbar writes through the controller, so its scroll
// events are intentionally classified as programmatic. Finishing that drag
// must still apply the same user-intent latch.
await act(async () => {
  api!.setMode("programmatic", "custom-scrollbar-test");
  api!.writeOffset("custom-scrollbar", 870);
  api!.onScroll();
  api!.finishProgrammaticScroll();
});
eq(api!.stick.current, false, "custom scrollbar near-bottom drag remains manual after pointerup");
eq(api!.modeRef.current, "manual", "custom scrollbar finish preserves the manual latch");
await act(async () => {
  api!.setMode("programmatic", "custom-scrollbar-bottom-test");
  api!.writeOffset("custom-scrollbar", 900);
  api!.onScroll();
  api!.finishProgrammaticScroll();
});
eq(api!.stick.current, true, "custom scrollbar drag to the physical bottom restores tail-follow");

// Gesture lock: virtualizer/stream must not rewrite scrollTop mid-gesture.
const writes: Array<[string, number]> = [];
window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (owner, top) => {
  writes.push([owner, top]);
};
scrollTop = 400;
await act(async () => {
  api!.stick.current = false;
  api!.setMode("manual", "test");
  api!.markUserGesture();
  const virtualizerWrote = api!.writeOffset("virtualizer", 120);
  const streamWrote = api!.writeOffset("stream", 900);
  eq(virtualizerWrote, false, "virtualizer write is blocked during user gesture");
  eq(streamWrote, false, "stream write is blocked during user gesture");
  eq(api!.canVirtualizerAdjust(), false, "virtualizer adjust freezes during user gesture");
});
eq(scrollTop, 400, "scrollTop stays put when compensating owners fire mid-gesture");
eq(writes.length, 0, "no compensating scroll writes are emitted mid-gesture");
window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;

// scrollend deterministically releases ownership and notifies idle subscribers.
let idleFires = 0;
const stopIdle = api!.onGestureIdle(() => {
  idleFires += 1;
});
await act(async () => {
  api!.markUserGesture();
});
eq(idleFires, 0, "gesture idle does not fire while the hold is active");
await act(async () => {
  api!.gestureLastActivityRef.current -= 100;
  api!.onScrollEnd();
});
eq(idleFires, 1, "scrollend releases the gesture exactly once");
eq(api!.canVirtualizerAdjust(), true, "virtualizer resumes immediately after scrollend");
stopIdle();

// A late scrollend from a controller write or an already-settled gesture must
// not manufacture another idle notification.
let strayIdleFires = 0;
const stopStrayIdle = api!.onGestureIdle(() => {
  strayIdleFires += 1;
});
await act(async () => {
  api!.onScrollEnd();
  await new Promise((resolve) => setTimeout(resolve, 24));
});
eq(strayIdleFires, 0, "stray scrollend does not create a synthetic idle cycle");
stopStrayIdle();

// Windows middle-button auto-scroll has no wheel samples after activation.
scrollTop = 900;
await act(async () => {
  api!.stick.current = true;
  const captured = api!.onPointerDownIntent({ button: 1 } as React.PointerEvent<HTMLElement>);
  eq(captured, true, "middle-button pointerdown starts a native scroll session");
});
eq(api!.stick.current, false, "middle-button auto-scroll releases tail-follow");
eq(api!.canVirtualizerAdjust(), false, "middle-button session freezes virtualizer compensation");
api!.gestureLastActivityRef.current -= 100;
api!.onScrollEnd();

// Unowned native scroll events cover scrollbar drags and continued auto-scroll.
scrollTop = 600;
api!.gestureUntilRef.current = 0;
await act(async () => {
  api!.setMode("manual", "native-scroll-test");
  api!.onScroll();
});
eq(api!.gestureSourceRef.current, "native-scroll", "unowned scroll starts a native gesture session");
eq(api!.canVirtualizerAdjust(), false, "native scroll freezes compensating writers");
api!.gestureLastActivityRef.current -= 100;
api!.onScrollEnd();

// Controller writes consume their own scroll event instead of masquerading as user input.
await act(async () => {
  api!.setMode("manual", "programmatic-scroll-test");
  const wrote = api!.writeOffset("virtualizer", 500);
  eq(wrote, true, "virtualizer writes when no user gesture is active");
  api!.onScroll();
});
eq(api!.gestureSourceRef.current, null, "owned scroll event does not create a user session");
eq(api!.canVirtualizerAdjust(), true, "owned scroll event keeps virtualizer writes enabled");

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

scrollTop = 400;
await act(async () => {
  api!.setMode("native-selecting", "test-selection");
  api!.scrollToBottom(true, "jump-bottom");
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
});
eq(api.modeRef.current, "native-selecting", "force-scroll cannot steal ownership from an active selection");
eq(scrollTop, 400, "force-scroll cannot move the viewport during an active selection");

const jumpTarget = document.createElement("div");
jumpTarget.getBoundingClientRect = () => ({
  x: 0,
  y: 240,
  top: 240,
  right: 100,
  bottom: 260,
  left: 0,
  width: 100,
  height: 20,
  toJSON: () => ({}),
});
await act(async () => {
  api!.setMode("manual", "prepare-jump");
  api!.smoothScrollTo(jumpTarget);
  api!.resetGeneration("next-tab", 1);
  await new Promise((resolve) => setTimeout(resolve, 300));
});
eq(api.modeRef.current, "tail-follow", "tab reset rejects the previous session's smooth-scroll completion timer");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

// Run: tsx src/__tests__/native-motion.test.tsx

import { JSDOM } from "jsdom";
import React, { useRef } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { CSS_EASE_OUT } from "../lib/motion";
import { useCollapseAnimation } from "../lib/useCollapseAnimation";
import { transcriptEntranceResetKey, useEntranceAnimation } from "../lib/useEntranceAnimation";
import { useMountTransition } from "../lib/useMountTransition";

let passed = 0;
let failed = 0;

type ControllableAnimation = {
  onfinish: (() => void) | null;
  oncancel: (() => void) | null;
  cancelCalls: number;
  cancel: () => void;
};

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
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function controllableAnimation(): ControllableAnimation {
  const animation: ControllableAnimation = {
    onfinish: null,
    oncancel: null,
    cancelCalls: 0,
    cancel() {
      animation.cancelCalls += 1;
      animation.oncancel?.();
    },
  };
  return animation;
}

function installDom(reducedMotion = false) {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  dom.window.matchMedia = () => ({
    matches: reducedMotion,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
  return dom;
}

function mockNativeAnimate(
  dom: JSDOM,
  implementation: (
    this: Element,
    frames: Keyframe[] | PropertyIndexedKeyframes | null,
    options: KeyframeAnimationOptions,
  ) => ControllableAnimation,
) {
  Object.defineProperty(dom.window.Element.prototype, "animate", {
    configurable: true,
    value: function (frames: Keyframe[] | PropertyIndexedKeyframes | null, options: number | KeyframeAnimationOptions) {
      if (typeof options === "number") throw new TypeError("expected keyframe animation options");
      return implementation.call(this, frames, options) as unknown as Animation;
    },
  });
}

async function cleanup(root: Root, dom: JSDOM) {
  await act(async () => root.unmount());
  dom.window.close();
}

function CollapseHarness({
  open,
  onOpen,
  onClose,
}: {
  open: boolean;
  onOpen?: () => void;
  onClose?: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useCollapseAnimation(ref, open, { onOpenComplete: onOpen, onCloseComplete: onClose });
  return <div id="collapse" ref={ref}>content</div>;
}

async function renderCollapse(root: Root, open: boolean, onOpen?: () => void, onClose?: () => void) {
  await act(async () => {
    root.render(<CollapseHarness open={open} onOpen={onOpen} onClose={onClose} />);
  });
}

function EntranceHarness({ resetKey, ids }: { resetKey: string; ids: string[] }) {
  const ref = useEntranceAnimation<HTMLDivElement>(resetKey, ids.length, "[data-entrance]", ids);
  return (
    <div ref={ref}>
      {ids.map((id) => <div key={id} id={`entry-${id}`} data-entrance={id} />)}
    </div>
  );
}

function DeferredEntranceHarness({
  resetKey,
  seedIds,
  visibleIds,
}: {
  resetKey: string;
  seedIds: string[];
  visibleIds: string[];
}) {
  const ref = useEntranceAnimation<HTMLDivElement>(resetKey, visibleIds.length, "[data-entrance]", seedIds);
  return (
    <div ref={ref}>
      {visibleIds.map((id) => <div key={id} id={`entry-${id}`} data-entrance={id} />)}
    </div>
  );
}

function MountHarness({ open, duration }: { open: boolean; duration: number }) {
  const { mounted } = useMountTransition(open, duration);
  return mounted ? <div id="mounted" /> : null;
}

console.log("\nnative motion fallbacks");

// Transcript tail appends keep one entrance generation; real surface changes
// and history prepends reset it before virtual rows mount.
{
  const base = [{ id: "u1" }, { id: "a1" }];
  const appended = [...base, { id: "u2" }];
  const prepended = [{ id: "old-u" }, ...base];
  const initialKey = transcriptEntranceResetKey("tab-a", 0, base);
  eq(transcriptEntranceResetKey("tab-a", 0, appended), initialKey, "tail append preserves transcript entrance reset key");
  ok(transcriptEntranceResetKey("tab-a", 0, prepended) !== initialKey, "history prepend changes transcript entrance reset key");
  ok(transcriptEntranceResetKey("tab-b", 0, base) !== initialKey, "tab switch changes transcript entrance reset key");
  ok(transcriptEntranceResetKey("tab-a", 1, base) !== initialKey, "reveal signal changes transcript entrance reset key");
}

// Valid native motion waits for completion and settles exactly once.
{
  const dom = installDom();
  const animations: ControllableAnimation[] = [];
  let easing: string | undefined;
  mockNativeAnimate(dom, (_frames, options) => {
    easing = options.easing;
    const animation = controllableAnimation();
    animations.push(animation);
    return animation;
  });
  const root = createRoot(document.getElementById("root")!);
  let opened = 0;
  await renderCollapse(root, false, () => { opened += 1; });
  await renderCollapse(root, true, () => { opened += 1; });

  eq(easing, CSS_EASE_OUT, "collapse passes a CSS easing to Element.animate");
  eq(opened, 0, "collapse waits for native completion");
  await act(async () => {
    animations[0].onfinish?.();
    animations[0].oncancel?.();
  });
  eq(opened, 1, "finish and late cancel settle collapse only once");
  eq((document.getElementById("collapse") as HTMLElement).style.height, "auto", "finished open state uses auto height");
  await cleanup(root, dom);
}

// A synchronously rejecting WebView must expose the requested final state.
{
  const dom = installDom();
  mockNativeAnimate(dom, () => {
    throw new TypeError("WebView rejected height animation");
  });
  const root = createRoot(document.getElementById("root")!);
  let opened = 0;
  await renderCollapse(root, false, () => { opened += 1; });
  await renderCollapse(root, true, () => { opened += 1; });
  eq(opened, 1, "rejected collapse animation runs its completion fallback");
  eq((document.getElementById("collapse") as HTMLElement).style.height, "auto", "rejected collapse animation exposes content");
  await cleanup(root, dom);
}

// Direction reversal cancels the old animation without firing its stale callback.
{
  const dom = installDom();
  const animations: ControllableAnimation[] = [];
  mockNativeAnimate(dom, () => {
    const animation = controllableAnimation();
    animations.push(animation);
    return animation;
  });
  const root = createRoot(document.getElementById("root")!);
  let opened = 0;
  let closed = 0;
  await renderCollapse(root, false, () => { opened += 1; }, () => { closed += 1; });
  await renderCollapse(root, true, () => { opened += 1; }, () => { closed += 1; });
  await renderCollapse(root, false, () => { opened += 1; }, () => { closed += 1; });
  eq(animations[0].cancelCalls, 1, "direction reversal cancels the previous animation");
  eq(opened, 0, "superseded open callback is suppressed");
  await act(async () => animations[1].oncancel?.());
  eq(closed, 1, "unexpected cancellation settles the active direction");
  eq((document.getElementById("collapse") as HTMLElement).style.height, "0px", "cancelled close reaches zero height");
  await cleanup(root, dom);
}

// A stable reset key animates appends; a real reset pre-seeds restored rows.
{
  const dom = installDom();
  const calls: { options: KeyframeAnimationOptions; animation: ControllableAnimation }[] = [];
  mockNativeAnimate(dom, (_frames, options) => {
    const animation = controllableAnimation();
    calls.push({ options, animation });
    return animation;
  });
  const root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<EntranceHarness resetKey="tab-a" ids={["a"]} />));
  await act(async () => root.render(<EntranceHarness resetKey="tab-a" ids={["a", "b"]} />));
  await act(async () => { await flushTimers(25); });
  eq(calls.length, 1, "stable entrance reset key animates an appended row");
  eq(calls[0].options.easing, CSS_EASE_OUT, "entrance animation uses the shared CSS easing");
  await act(async () => calls[0].animation.onfinish?.());
  eq((document.getElementById("entry-b") as HTMLElement).style.opacity, "1", "finished entrance exposes the row");

  await act(async () => root.render(<EntranceHarness resetKey="tab-b" ids={["a", "b", "c"]} />));
  await act(async () => { await flushTimers(25); });
  eq(calls.length, 1, "changed reset key pre-seeds restored rows without animation");
  await act(async () => root.render(<EntranceHarness resetKey="tab-b" ids={["a", "b", "c", "d"]} />));
  await act(async () => { await flushTimers(25); });
  eq(calls.length, 2, "append after a reset animates normally");
  await cleanup(root, dom);
}

// Virtual rows can mount after the initial scan. Model-backed seed IDs keep
// restored rows inert while still allowing a genuinely appended row to enter.
{
  const dom = installDom();
  const calls: string[] = [];
  mockNativeAnimate(dom, function () {
    calls.push((this as unknown as Element).getAttribute?.("data-entrance") ?? "");
    return controllableAnimation();
  });
  const root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(
    <DeferredEntranceHarness resetKey="tab-a" seedIds={["history"]} visibleIds={[]} />,
  ));
  await act(async () => root.render(
    <DeferredEntranceHarness resetKey="tab-a" seedIds={["history", "new"]} visibleIds={["history", "new"]} />,
  ));
  await act(async () => { await flushTimers(25); });
  eq(calls.length, 1, "deferred virtual history does not animate with a new append");
  eq(calls[0], "new", "only the genuinely appended virtual row animates");
  await cleanup(root, dom);
}

// One rejected entrance must fail open without aborting the timer callback.
{
  const dom = installDom();
  let attempts = 0;
  mockNativeAnimate(dom, () => {
    attempts += 1;
    throw new TypeError("WebView rejected entrance animation");
  });
  const root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<EntranceHarness resetKey="tab-a" ids={["a"]} />));
  await act(async () => root.render(<EntranceHarness resetKey="tab-a" ids={["a", "b", "c"]} />));
  await act(async () => { await flushTimers(25); });
  eq(attempts, 2, "a rejected entrance does not prevent later entries from attempting motion");
  eq((document.getElementById("entry-b") as HTMLElement).style.opacity, "1", "rejected entrance exposes the first row");
  eq((document.getElementById("entry-c") as HTMLElement).style.opacity, "1", "rejected entrance exposes later rows");
  await cleanup(root, dom);
}

// Terminal-style conditional content unmounts on a bounded timer even if no
// transitionend event is delivered.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<MountHarness open={true} duration={20} />));
  ok(Boolean(document.getElementById("mounted")), "transition content mounts while open");
  await act(async () => root.render(<MountHarness open={false} duration={20} />));
  ok(Boolean(document.getElementById("mounted")), "transition content remains mounted during close delay");
  await act(async () => { await flushTimers(30); });
  ok(!document.getElementById("mounted"), "transition content unmounts without transitionend");
  await cleanup(root, dom);
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

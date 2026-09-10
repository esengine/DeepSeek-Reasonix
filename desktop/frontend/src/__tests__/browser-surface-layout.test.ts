// Run: tsx src/__tests__/browser-surface-layout.test.ts
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import type { BrowserLayoutRect } from "../lib/browserHost";
import { attachAppOverlayGate, attachBrowserSurfaceLayout } from "../lib/useBrowserSurfaceLayout";

const dom = new JSDOM("<!doctype html><html><body><main id='scroller'><div id='surface'></div></main></body></html>", { url: "http://localhost" });
const view = dom.window as unknown as Window & typeof globalThis;
const frames: FrameRequestCallback[] = [];
let cancelled = 0;
view.requestAnimationFrame = (callback) => { frames.push(callback); return frames.length; };
view.cancelAnimationFrame = (id) => { cancelled += 1; frames[id - 1] = () => {}; };
const flush = () => { for (const callback of frames.splice(0)) callback(0); };

class FakeResizeObserver {
  static instances: FakeResizeObserver[] = [];
  observed: Element[] = [];
  constructor(readonly callback: ResizeObserverCallback) { FakeResizeObserver.instances.push(this); }
  observe(target: Element) { this.observed.push(target); }
  unobserve() {}
  disconnect() { this.observed = []; }
  trigger() { this.callback([], this as unknown as ResizeObserver); }
}
(view as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;

const surface = view.document.getElementById("surface")!;
const scroller = view.document.getElementById("scroller")!;
let box = { left: 10.4, top: 20.6, width: 300.2, height: 199.5 };
surface.getBoundingClientRect = () => ({ ...box, right: box.left + box.width, bottom: box.top + box.height, x: box.left, y: box.top, toJSON: () => box }) as DOMRect;

const layouts: (BrowserLayoutRect | null)[] = [];
const overlays: boolean[] = [];
const host = { setLayout: (rect: BrowserLayoutRect | null) => { layouts.push(rect); }, setOverlay: (active: boolean) => { overlays.push(active); } };
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

console.log("\nbrowser surface layout");

const detachLayout = attachBrowserSurfaceLayout(host, surface, view);
assert.deepEqual(layouts, [{ x: 10, y: 21, width: 300, height: 200 }], "the surface rect is reported immediately, rounded to integers");
const observer = FakeResizeObserver.instances[0];
assert.deepEqual(observer.observed, [surface], "the surface is observed for size changes");
observer.trigger();
observer.trigger();
assert.equal(frames.length, 1, "notifications coalesce into one animation frame");
flush();
assert.equal(layouts.length, 1, "an unchanged rect is not resent");
box = { ...box, top: 60 };
scroller.dispatchEvent(new view.Event("scroll"));
flush();
assert.deepEqual(layouts[layouts.length - 1], { x: 10, y: 60, width: 300, height: 200 }, "scrolling an ancestor re-measures");
box = { ...box, width: 420 };
view.dispatchEvent(new view.Event("resize"));
flush();
assert.deepEqual(layouts[layouts.length - 1], { x: 10, y: 60, width: 420, height: 200 }, "window resize re-measures");
box = { ...box, left: 5 };
observer.trigger();
detachLayout();
assert.equal(cancelled, 1, "a pending frame is cancelled on detach");
assert.equal(layouts[layouts.length - 1], null, "detach hides the native view");
assert.deepEqual(observer.observed, [], "detach disconnects the observer");
const count = layouts.length;
scroller.dispatchEvent(new view.Event("scroll"));
view.dispatchEvent(new view.Event("resize"));
flush();
assert.equal(layouts.length, count, "listeners are removed on detach");
console.log("  PASS  rect math, dedupe, ancestor scroll, resize and detach");

const detachOverlay = attachAppOverlayGate(host, view);
assert.deepEqual(overlays, [false], "the initial overlay state is reported");
const modal = view.document.createElement("div");
modal.setAttribute("data-app-overlay", "");
view.document.body.appendChild(modal);
await settle();
flush();
assert.deepEqual(overlays, [false, true], "a portaled overlay root hides the native views");
const wrapper = view.document.createElement("div");
wrapper.innerHTML = "<section><div data-app-overlay=''></div></section>";
view.document.body.appendChild(wrapper);
await settle();
flush();
assert.deepEqual(overlays, [false, true], "a second overlay does not repeat the state");
modal.remove();
await settle();
flush();
assert.deepEqual(overlays, [false, true], "one remaining nested overlay keeps the state");
wrapper.remove();
await settle();
flush();
assert.deepEqual(overlays, [false, true, false], "removing the last overlay restores the views");
const plain = view.document.createElement("div");
view.document.body.appendChild(plain);
await settle();
flush();
assert.equal(overlays.length, 3, "unrelated mutations do not report");
plain.setAttribute("data-app-overlay", "");
await settle();
flush();
assert.deepEqual(overlays[overlays.length - 1], true, "toggling the attribute on an existing element counts");
detachOverlay();
assert.deepEqual(overlays[overlays.length - 1], false, "detach clears an active overlay state");
plain.remove();
await settle();
flush();
assert.equal(overlays.length, 5, "the observer is disconnected on detach");
console.log("  PASS  overlay gate follows [data-app-overlay] presence");
console.log("browser surface layout: all checks passed");

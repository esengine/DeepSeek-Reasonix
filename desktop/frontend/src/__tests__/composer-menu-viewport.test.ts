// Run: tsx src/__tests__/composer-menu-viewport.test.ts

import { JSDOM } from "jsdom";
import {
  COMPOSER_MENU_AVAILABLE_HEIGHT,
  composerMenuAvailableHeight,
  observeComposerMenuViewport,
} from "../lib/composerMenuViewport";

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
  ok(actual === expected, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function rect(top: number): DOMRect {
  return {
    left: 0,
    top,
    right: 600,
    bottom: top + 120,
    width: 600,
    height: 120,
    x: 0,
    y: top,
    toJSON: () => ({}),
  } as DOMRect;
}

class TestResizeObserver {
  static instance: TestResizeObserver | null = null;
  readonly observed = new Set<Element>();
  disconnected = false;

  constructor(private readonly callback: ResizeObserverCallback) {
    TestResizeObserver.instance = this;
  }

  observe(target: Element) {
    this.observed.add(target);
  }

  unobserve(target: Element) {
    this.observed.delete(target);
  }

  disconnect() {
    this.disconnected = true;
    this.observed.clear();
  }

  trigger() {
    this.callback([], this as unknown as ResizeObserver);
  }
}

console.log("\ncomposer menu viewport geometry");

const dom = new JSDOM(`<!doctype html><html><body>
  <div id="layout">
    <main id="chat"></main>
    <footer id="footer"><div id="anchor"><div class="slashmenu"></div></div></footer>
    <section id="terminal"></section>
  </div>
</body></html>`, { pretendToBeVisual: true });

globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver;

let nextFrameId = 1;
const frames = new Map<number, FrameRequestCallback>();
const requestFrame = (callback: FrameRequestCallback) => {
  const id = nextFrameId++;
  frames.set(id, callback);
  return id;
};
const cancelFrame = (id: number) => frames.delete(id);
dom.window.requestAnimationFrame = requestFrame;
dom.window.cancelAnimationFrame = cancelFrame;
globalThis.requestAnimationFrame = requestFrame;
globalThis.cancelAnimationFrame = cancelFrame;

function flushFrames() {
  const queued = [...frames.entries()];
  frames.clear();
  for (const [, callback] of queued) callback(0);
}

const anchor = document.getElementById("anchor");
const terminal = document.getElementById("terminal");
if (!(anchor instanceof HTMLElement) || !(terminal instanceof HTMLElement)) {
  throw new Error("missing geometry test elements");
}

let anchorTop = 567;
anchor.getBoundingClientRect = () => rect(anchorTop);

eq(composerMenuAvailableHeight(anchorTop), 553, "normal layout exposes the full 360px CSS cap");
eq(composerMenuAvailableHeight(10), 0, "tiny viewports never produce a negative max height");

const stop = observeComposerMenuViewport(anchor);
eq(anchor.style.getPropertyValue(COMPOSER_MENU_AVAILABLE_HEIGHT), "553px", "initial layout is measured synchronously");

const observer = TestResizeObserver.instance;
if (!observer) throw new Error("ResizeObserver was not installed");
ok(observer.observed.has(terminal), "terminal sibling is observed because it can move the composer anchor");

// Reproduce the constrained layout from the review: terminal + tall composer
// move the anchor to y=207. The menu has 207 - 6 (anchor gap) - 8 (safe edge)
// = 193px, so its first rows remain on-screen and the rest scroll internally.
anchorTop = 207;
observer.trigger();
flushFrames();
eq(anchor.style.getPropertyValue(COMPOSER_MENU_AVAILABLE_HEIGHT), "193px", "terminal expansion uses real space above the composer");

anchorTop = 307;
window.dispatchEvent(new Event("resize"));
flushFrames();
eq(anchor.style.getPropertyValue(COMPOSER_MENU_AVAILABLE_HEIGHT), "293px", "viewport resize recomputes the geometry");

observer.trigger();
stop();
eq(frames.size, 0, "cleanup cancels a queued geometry frame");
eq(anchor.style.getPropertyValue(COMPOSER_MENU_AVAILABLE_HEIGHT), "", "cleanup removes the temporary CSS variable");
ok(observer.disconnected, "cleanup disconnects layout observers");

dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

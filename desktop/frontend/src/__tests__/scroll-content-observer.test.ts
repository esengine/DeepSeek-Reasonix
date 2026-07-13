// Run: tsx src/__tests__/scroll-content-observer.test.ts

import { JSDOM } from "jsdom";
import { observeScrollContentSize } from "../lib/scrollContentObserver";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nscroll content observer");

const dom = new JSDOM("<!doctype html><html><body><div id=scroll><div id=first></div></div></body></html>");
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;

class FakeResizeObserver {
  static current: FakeResizeObserver | null = null;
  readonly observed = new Set<Element>();
  disconnected = false;

  constructor(private readonly callback: ResizeObserverCallback) {
    FakeResizeObserver.current = this;
  }

  observe(target: Element) { this.observed.add(target); }
  unobserve(target: Element) { this.observed.delete(target); }
  disconnect() { this.disconnected = true; this.observed.clear(); }
  trigger() { this.callback([], this as unknown as ResizeObserver); }
}

class FakeMutationObserver {
  static current: FakeMutationObserver | null = null;
  disconnected = false;

  constructor(private readonly callback: MutationCallback) {
    FakeMutationObserver.current = this;
  }

  observe() {}
  disconnect() { this.disconnected = true; }
  takeRecords(): MutationRecord[] { return []; }
  trigger() { this.callback([], this as unknown as MutationObserver); }
}

globalThis.ResizeObserver = FakeResizeObserver as unknown as typeof ResizeObserver;
globalThis.MutationObserver = FakeMutationObserver as unknown as typeof MutationObserver;

const scroll = document.getElementById("scroll");
const first = document.getElementById("first");
if (!scroll || !first) throw new Error("missing test elements");

let changes = 0;
const stop = observeScrollContentSize(scroll, () => { changes += 1; });
eq(FakeResizeObserver.current?.observed.has(first), true, "observes existing transcript children");

FakeResizeObserver.current?.trigger();
eq(changes, 1, "content child resize refreshes scrollbar metrics");

const second = document.createElement("div");
scroll.appendChild(second);
FakeMutationObserver.current?.trigger();
eq(FakeResizeObserver.current?.observed.has(second), true, "observes children added after mount");
eq(changes, 2, "child-list changes refresh scrollbar metrics");

first.remove();
FakeMutationObserver.current?.trigger();
eq(FakeResizeObserver.current?.observed.has(first), false, "stops observing removed transcript children");

stop();
eq(FakeResizeObserver.current?.disconnected, true, "disconnects the resize observer on cleanup");
eq(FakeMutationObserver.current?.disconnected, true, "disconnects the mutation observer on cleanup");

dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

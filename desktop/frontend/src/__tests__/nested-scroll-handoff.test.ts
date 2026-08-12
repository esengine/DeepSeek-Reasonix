// Run: tsx src/__tests__/nested-scroll-handoff.test.ts

import { JSDOM } from "jsdom";
import {
  attachNestedScrollHandoff,
  canElementScrollVertically,
  normalizeWheelDelta,
  shouldHandoffVerticalWheel,
} from "../lib/nestedScrollHandoff";

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
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

console.log("\nnested scroll handoff");

const dom = new JSDOM(`<!doctype html><html><body>
  <div id="parent" style="height:200px;overflow-y:auto">
    <div id="nested" data-nested-scroll style="height:100px;overflow-y:auto">
      <div id="inner" style="height:400px">cell</div>
    </div>
    <div id="table-wrap" class="md-table-scroll" style="height:80px;overflow-x:auto;overflow-y:hidden">
      <div id="table-cell">wide table cell</div>
    </div>
  </div>
</body></html>`, { pretendToBeVisual: true });

globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Element = dom.window.Element;
globalThis.WheelEvent = dom.window.WheelEvent;

const parent = document.getElementById("parent") as HTMLElement;
const nested = document.getElementById("nested") as HTMLElement;
const inner = document.getElementById("inner") as HTMLElement;
const tableWrap = document.getElementById("table-wrap") as HTMLElement;
const tableCell = document.getElementById("table-cell") as HTMLElement;

Object.defineProperty(nested, "clientHeight", { configurable: true, value: 100 });
Object.defineProperty(nested, "scrollHeight", { configurable: true, value: 400 });
let nestedTop = 0;
Object.defineProperty(nested, "scrollTop", {
  configurable: true,
  get: () => nestedTop,
  set: (v: number) => { nestedTop = v; },
});

Object.defineProperty(tableWrap, "clientHeight", { configurable: true, value: 80 });
Object.defineProperty(tableWrap, "scrollHeight", { configurable: true, value: 80 });
Object.defineProperty(tableWrap, "scrollTop", { configurable: true, value: 0 });

Object.defineProperty(parent, "clientHeight", { configurable: true, value: 200 });
Object.defineProperty(parent, "scrollHeight", { configurable: true, value: 2000 });
let parentTop = 100;
Object.defineProperty(parent, "scrollTop", {
  configurable: true,
  get: () => parentTop,
  set: (v: number) => { parentTop = v; },
});

// Mock computed styles for overflow detection.
const styleMap = new Map<Element, { overflowX: string; overflowY: string }>([
  [nested, { overflowX: "auto", overflowY: "auto" }],
  [tableWrap, { overflowX: "auto", overflowY: "hidden" }],
  [parent, { overflowX: "hidden", overflowY: "auto" }],
  [inner, { overflowX: "visible", overflowY: "visible" }],
  [tableCell, { overflowX: "visible", overflowY: "visible" }],
]);
dom.window.getComputedStyle = ((el: Element) => {
  const s = styleMap.get(el) ?? { overflowX: "visible", overflowY: "visible" };
  return s as unknown as CSSStyleDeclaration;
}) as typeof getComputedStyle;
globalThis.getComputedStyle = dom.window.getComputedStyle;

nestedTop = 50;
ok(canElementScrollVertically(nested, -20), "nested can scroll up from mid-body");
ok(canElementScrollVertically(nested, 20), "nested can scroll down from mid-body");
nestedTop = 0;
ok(!canElementScrollVertically(nested, -20), "nested cannot scroll up at top");
nestedTop = 300;
ok(!canElementScrollVertically(nested, 20), "nested cannot scroll down at bottom");
ok(!canElementScrollVertically(tableWrap, 20), "horizontal-only wrapper cannot scroll vertically");

nestedTop = 0;
ok(
  shouldHandoffVerticalWheel(inner, parent, -40),
  "wheel-up at nested top should handoff to parent",
);
nestedTop = 50;
ok(
  !shouldHandoffVerticalWheel(inner, parent, -40),
  "wheel-up mid nested should not handoff",
);
ok(
  shouldHandoffVerticalWheel(tableCell, parent, 40),
  "wheel over horizontal-only table wrapper handoffs vertical to parent",
);

// Integration: attach listener and dispatch wheels.
nestedTop = 0;
parentTop = 100;
let intents = 0;
const handedOffDeltas: number[] = [];
let clock = 1_000;
const handoff = attachNestedScrollHandoff({
  parent,
  onParentScrollIntent: (deltaY) => {
    intents += 1;
    handedOffDeltas.push(deltaY);
  },
  latchHoldMs: 200,
  now: () => clock,
});

const wheelAt = (target: Element, deltaY: number, deltaMode: number = dom.window.WheelEvent.DOM_DELTA_PIXEL) => {
  const event = new dom.window.WheelEvent("wheel", {
    deltaY,
    deltaX: 0,
    deltaMode,
    bubbles: true,
    cancelable: true,
  });
  target.dispatchEvent(event);
  return event;
};

const edgeUp = wheelAt(inner, -40);
ok(edgeUp.defaultPrevented, "edge wheel is preventDefaulted");
eq(parentTop, 60, "edge wheel promotes deltaY onto parent scrollTop");
ok(intents >= 1, "edge handoff notifies parent scroll intent");
eq(handedOffDeltas[0], -40, "edge handoff reports normalized direction to the parent controller");

// After an edge handoff, latch keeps driving the parent even if nested could scroll.
clock += 10; // still inside latchHoldMs
const afterLatch = parentTop;
nestedTop = 50;
const latched = wheelAt(inner, -30);
ok(latched.defaultPrevented, "latched gesture continues on parent");
eq(parentTop, afterLatch - 30, "latched wheel keeps applying to parent");
eq(handedOffDeltas[1], -30, "latched handoff keeps reporting normalized direction");

// Once the latch expires, mid-body nested scroll is left to the browser.
clock += 500;
const midTopBefore = parentTop;
nestedTop = 50;
const mid = wheelAt(inner, -20);
ok(!mid.defaultPrevented, "mid nested wheel is left to the browser after latch expires");
eq(parentTop, midTopBefore, "mid nested wheel does not move parent after latch expires");

const lineDelta = normalizeWheelDelta({ deltaX: 0, deltaY: 2, deltaMode: dom.window.WheelEvent.DOM_DELTA_LINE }, parent);
eq(lineDelta.y, 32, "line-mode wheel deltas use a stable pixel fallback");
const pageDelta = normalizeWheelDelta({ deltaX: 0, deltaY: 1, deltaMode: dom.window.WheelEvent.DOM_DELTA_PAGE }, parent);
eq(pageDelta.y, 200, "page-mode wheel deltas use the transcript viewport height");

clock += 500;
nestedTop = 0;
parentTop = 500;
wheelAt(inner, -2, dom.window.WheelEvent.DOM_DELTA_LINE);
eq(parentTop, 468, "line-mode handoff applies normalized pixels instead of raw lines");

clock += 500;
parentTop = 500;
wheelAt(inner, -1, dom.window.WheelEvent.DOM_DELTA_PAGE);
eq(parentTop, 300, "page-mode handoff applies one viewport page");

handoff.detach();
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

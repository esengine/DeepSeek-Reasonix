// Run: tsx src/__tests__/reasoning-scroll-follow.test.tsx

import { JSDOM } from "jsdom";
import { readFileSync } from "node:fs";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useReasoningScrollFollow } from "../lib/useReasoningScrollFollow";

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

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", { pretendToBeVisual: true });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;

function Harness({ content }: { content: string }) {
  const [elementRef, onScroll] = useReasoningScrollFollow(content, true);
  return <div ref={elementRef} data-nested-scroll onScroll={onScroll}>{content}</div>;
}

function RefLessHarness({ content }: { content: string }) {
  useReasoningScrollFollow(content, true);
  return null;
}

console.log("\nreasoning inner-scroll follow");

const styles = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
eq(styles.includes("max-height: min(40vh, 480px);"), true, "expanded reasoning uses the responsive height cap");
eq(styles.includes("overflow-y: auto;"), true, "expanded reasoning keeps full content internally scrollable");
eq(styles.includes(".reasoning--loading { height: 58px; }"), true, "lazy reasoning reserves collapsed history geometry");
eq(styles.includes(".reasoning--loading[data-expanded] { height: min(40vh, 480px); }"), true, "lazy expanded reasoning reserves bounded geometry");

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => root.render(<Harness key="a:turn:0" content="start" />));
const pane = rootElement.firstElementChild as HTMLElement;
let scrollHeight = 1_000;
let scrollTop = 0;
Object.defineProperty(pane, "clientHeight", { configurable: true, value: 200 });
Object.defineProperty(pane, "scrollHeight", { configurable: true, get: () => scrollHeight });
Object.defineProperty(pane, "scrollTop", {
  configurable: true,
  get: () => scrollTop,
  set: (value: number) => { scrollTop = value; },
});

await act(async () => root.render(<Harness key="a:turn:0" content="start next token" />));
eq(scrollTop, 1_000, "new reasoning tokens follow the inner tail while armed");
eq(pane.hasAttribute("data-nested-scroll"), true, "reasoning pane participates in nested-scroll handoff");

scrollTop = 300;
pane.dispatchEvent(new dom.window.Event("scroll", { bubbles: true }));
scrollHeight = 1_200;
await act(async () => root.render(<Harness key="a:turn:0" content="start next token more" />));
eq(scrollTop, 300, "manual upward reading releases automatic inner follow");

scrollTop = 1_000;
pane.dispatchEvent(new dom.window.Event("scroll", { bubbles: true }));
scrollHeight = 1_300;
await act(async () => root.render(<Harness key="a:turn:0" content="start next token more final" />));
eq(scrollTop, 1_300, "returning within eight pixels of the bottom rearms follow");

scrollTop = 100;
pane.dispatchEvent(new dom.window.Event("scroll", { bubbles: true }));
await act(async () => root.render(<Harness key="a:turn:1" content="new segment" />));
const nextPane = rootElement.firstElementChild as HTMLElement;
let nextScrollTop = 0;
Object.defineProperty(nextPane, "clientHeight", { configurable: true, value: 200 });
Object.defineProperty(nextPane, "scrollHeight", { configurable: true, value: 1_400 });
Object.defineProperty(nextPane, "scrollTop", {
  configurable: true,
  get: () => nextScrollTop,
  set: (value: number) => { nextScrollTop = value; },
});
await act(async () => root.render(<Harness key="a:turn:1" content="new segment token" />));
eq(nextScrollTop, 1_400, "a new assistant segment starts with inner follow armed");

await act(async () => root.render(<RefLessHarness content="unmounted before layout effect" />));
eq(rootElement.firstElementChild, null, "an active follow tolerates a reasoning body that is not mounted");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

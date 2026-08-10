// Run: tsx src/__tests__/markdown-idle-highlight.test.tsx
//
// Oversized code blocks (≥ IDLE_HIGHLIGHT_MIN_BYTES, under the skip caps)
// mount the COMPLETE plain text immediately and swap in highlighted HTML from
// an idle callback; the skip caps remain the plain-forever policy. Small
// blocks keep synchronous highlighting. Text content is identical before and
// after the swap — nothing is ever truncated.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import HljsCode from "../components/editors/HljsCode";
import { IDLE_HIGHLIGHT_MIN_BYTES, MAX_HIGHLIGHT_BYTES } from "../lib/highlight";
import { LocaleProvider } from "../lib/i18n";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
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

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;

let pendingIdle: Array<() => void> = [];
Object.defineProperty(dom.window, "requestIdleCallback", {
  configurable: true,
  value: (callback: () => void) => {
    pendingIdle.push(callback);
    return pendingIdle.length;
  },
});
Object.defineProperty(dom.window, "cancelIdleCallback", {
  configurable: true,
  value: () => undefined,
});

async function runIdle() {
  const callbacks = pendingIdle;
  pendingIdle = [];
  await act(async () => {
    for (const callback of callbacks) callback();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");

function pre() {
  return rootEl.querySelector("pre.code.hljs");
}

console.log("\nmarkdown idle highlight");

// ── oversized-but-highlightable block: plain first, highlight at idle ───────
{
  const line = "const resultValue = computeSomething(argumentOne, argumentTwo); // comment\n";
  const value = line.repeat(Math.ceil((IDLE_HIGHLIGHT_MIN_BYTES + 4096) / line.length));
  ok(value.length > IDLE_HIGHLIGHT_MIN_BYTES, "fixture exceeds the idle-highlight threshold");
  ok(value.length < MAX_HIGHLIGHT_BYTES, "fixture stays under the highlight skip cap");

  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <HljsCode value={value} language="javascript" />
      </LocaleProvider>,
    );
  });
  eq(pre()?.getAttribute("data-highlight-mode"), "plain", "oversized block mounts as plain text");
  eq(pre()?.textContent, value, "plain first paint carries the COMPLETE source");
  ok(!pre()?.querySelector("span"), "plain first paint has no highlight markup");
  ok(pendingIdle.length > 0, "highlight is scheduled for idle time");

  await runIdle();
  eq(pre()?.getAttribute("data-highlight-mode"), "syntax", "idle callback swaps in highlighted HTML");
  ok(pre()?.querySelector("span"), "highlighted output contains token spans");
  eq(pre()?.textContent, value, "text content is identical before and after highlighting");
  await act(async () => root.unmount());
}

// ── small blocks keep synchronous highlighting ───────────────────────────────
{
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <HljsCode value="const small = true;" language="javascript" />
      </LocaleProvider>,
    );
  });
  eq(pre()?.getAttribute("data-highlight-mode"), "syntax", "small blocks highlight synchronously");
  ok(pre()?.querySelector("span"), "small blocks render token spans immediately");
  eq(pendingIdle.length, 0, "small blocks schedule no idle work");
  await act(async () => root.unmount());
}

// ── over the skip caps: plain forever, still complete ────────────────────────
{
  const value = "x = 1\n".repeat(Math.ceil((MAX_HIGHLIGHT_BYTES + 1024) / 6));
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <HljsCode value={value} language="python" />
      </LocaleProvider>,
    );
  });
  eq(pre()?.getAttribute("data-highlight-mode"), "plain", "over-cap blocks stay plain");
  eq(pre()?.textContent, value, "over-cap blocks still show the complete source");
  eq(pendingIdle.length, 0, "over-cap blocks schedule no idle highlight");
  await runIdle();
  eq(pre()?.getAttribute("data-highlight-mode"), "plain", "over-cap blocks remain plain after idle");
  await act(async () => root.unmount());
}

dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

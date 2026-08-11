// Run: tsx src/__tests__/context-panel-capacity.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextPanel } from "../components/ContextPanel";
import { LocaleProvider } from "../lib/i18n";
import type { ContextPanelInfo } from "../lib/types";

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

function wait(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.ResizeObserver = TestResizeObserver;
  return dom;
}

function emptyPanelInfo(): ContextPanelInfo {
  return {
    usedTokens: 0,
    windowTokens: 0,
    promptTokens: 0,
    completionTokens: 0,
    totalTokens: 0,
    reasoningTokens: 0,
    cacheHitTokens: 0,
    cacheMissTokens: 0,
    sessionCacheHitTokens: 0,
    sessionCacheMissTokens: 0,
    sessionCompletionTokens: 0,
    requestCount: 0,
    elapsedMs: 0,
    sessionCost: 0,
    readFiles: [],
    changedFiles: [],
  };
}

console.log("\ncontext panel capacity");

const dom = installDom();
(window as unknown as { go: { main: { App: { ContextPanel: () => Promise<ContextPanelInfo> } } } }).go = {
  main: { App: { ContextPanel: async () => emptyPanelInfo() } },
};
const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <ContextPanel
        tabId="tab-capacity"
        context={{ used: 1_001, window: 1_000, sessionTokens: 1_001, compactRatio: 0.8 }}
      />
    </LocaleProvider>,
  );
  await wait();
});

const capacity = document.querySelector(".context-panel__capacity-card");
const meter = capacity?.querySelector(".context-panel__capacity-meter");
const fill = meter?.querySelector(".context-panel__progress-fill") as HTMLElement | null;
const usedPin = meter?.querySelector(".context-panel__capacity-pin--used");
const compactMarker = meter?.querySelector(".context-panel__compact-marker") as HTMLElement | null;

eq(capacity?.querySelector(".context-panel__capacity-status")?.textContent, "Over context limit", "over-limit status is explicit");
eq(usedPin?.textContent, "101%", "just-over-limit percentage pin remains visibly over 100 percent");
eq(fill?.style.width, "100%", "capacity fill is capped at the physical track width");
eq(meter?.querySelectorAll(".context-panel__progress-segment").length, 0, "capacity meter does not mix token composition into its fill");
eq(compactMarker?.style.left, "80%", "compression threshold marker stays at the configured ratio");
ok(meter?.getAttribute("aria-label")?.includes("101% used") === true, "accessible summary reports the over-limit ratio");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

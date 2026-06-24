// Run: tsx src/__tests__/context-panel-tooltip.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextPanel } from "../components/ContextPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
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
  if (actual === expected) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
  }
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(check: () => boolean, iterations = 20): Promise<void> {
  for (let index = 0; index < iterations; index += 1) {
    if (check()) return;
    await act(async () => {
      await flush();
    });
  }
  throw new Error("condition not met in time");
}

function panelInfo(elapsedMs: number): ContextPanelInfo {
  return {
    usedTokens: 42124,
    windowTokens: 128000,
    promptTokens: 22134,
    completionTokens: 12345,
    totalTokens: 34479,
    reasoningTokens: 7521,
    cacheHitTokens: 87000,
    cacheMissTokens: 13000,
    requestCount: 3,
    elapsedMs,
    sessionCost: 0.1,
    sessionCurrency: "CNY",
    sessionCostUsd: 0.1,
    sources: {},
    mock: true,
    readFiles: [],
    changedFiles: [],
  };
}

console.log("\ncontext panel tooltip");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "zh-CN" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
Object.defineProperty(window, "innerHeight", { configurable: true, value: 800 });

const contextPanelCalls: string[] = [];
window.go = {
  main: {
    App: {
      ContextPanel: async (tabID: string) => {
        contextPanelCalls.push(tabID);
        if (tabID === "short") return panelInfo(42_000);
        if (tabID === "long") return panelInfo((19 * 60 * 60 + 49 * 60 + 1) * 1000);
        return panelInfo((2 * 24 * 60 * 60 + 5 * 60 * 60) * 1000);
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <ContextPanel tabId="short" refreshKey={1} />
    </LocaleProvider>,
  );
  await flush();
});

await waitFor(() => Boolean(document.querySelector(".context-panel__metric-tooltip-target[aria-label='耗时: 42秒']")));

const shortCard = document.querySelector<HTMLElement>(".context-panel__metric-tooltip-target[aria-label='耗时: 42秒']");
ok(Boolean(shortCard), "seconds-only time card still renders as a tooltip target");

await act(async () => {
  shortCard?.focus();
  await flush();
});

let tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
eq(tooltip?.textContent?.trim(), "耗时: 42秒", "hovering a short duration reveals the tooltip");

await act(async () => {
  root.render(
    <LocaleProvider>
      <ContextPanel tabId="long" refreshKey={2} />
    </LocaleProvider>,
  );
  await flush();
});

await waitFor(() => Array.from(document.querySelectorAll(".context-panel__metric strong")).some((node) => node.textContent?.trim() === "19时49分"));

const longLabels = Array.from(document.querySelectorAll(".context-panel__metric strong")).map((node) => node.textContent?.trim());
ok(longLabels.includes("19时49分"), "time card body keeps only the largest two units");

const longCard = document.querySelector<HTMLElement>(".context-panel__metric-tooltip-target[aria-label='耗时: 19时49分1秒']");
ok(Boolean(longCard), "long duration time card exposes full detail in aria label");

await act(async () => {
  longCard?.focus();
  await flush();
});

tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
eq(tooltip?.textContent?.trim(), "耗时: 19时49分1秒", "hovering a long duration reveals full segmented detail");
ok(contextPanelCalls.includes("short") && contextPanelCalls.includes("long"), "component refreshes panel data for each tab scenario");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

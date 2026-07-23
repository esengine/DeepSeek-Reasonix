// Run: tsx src/__tests__/context-window-ring.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextWindowRing } from "../components/ContextWindowRing";
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
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.ResizeObserver = TestResizeObserver;
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  });
  return dom;
}

function contextPanelInfo(requestCount: number): ContextPanelInfo {
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
    requestCount,
    elapsedMs: 0,
    sessionCost: 0,
    sessionCurrency: "",
    readFiles: [],
    changedFiles: [],
  };
}

function installContextPanelMock(fn: (tabId: string) => Promise<ContextPanelInfo>) {
  (window as unknown as { go: { main: { App: { ContextPanel: typeof fn } } } }).go = {
    main: {
      App: {
        ContextPanel: fn,
      },
    },
  };
}

async function renderRing(props: Partial<Parameters<typeof ContextWindowRing>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  let currentProps: Parameters<typeof ContextWindowRing>[0] = {
    enabled: true,
    tabId: "tab-a",
    context: { used: 10, window: 100, compactRatio: 0.8 },
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof ContextWindowRing>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ContextWindowRing {...currentProps} />
        </LocaleProvider>,
      );
      await wait();
    });
  };
  await paint();
  return { root, rerender: paint };
}

console.log("\ncontext window ring");

{
  const dom = installDom();
  const calls: string[] = [];
  installContextPanelMock(async (tabId) => {
    calls.push(tabId);
    return contextPanelInfo(1);
  });

  const { root } = await renderRing({ enabled: false });

  eq(document.querySelector(".context-ring"), null, "disabled ring renders nothing");
  eq(calls.length, 0, "disabled ring does not request context panel data");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const calls: string[] = [];
  const resolvers = new Map<string, (value: ContextPanelInfo) => void>();
  installContextPanelMock((tabId) => {
    calls.push(tabId);
    return new Promise<ContextPanelInfo>((resolve) => {
      resolvers.set(tabId, resolve);
    });
  });

  const { root, rerender } = await renderRing({ tabId: "old-tab" });
  const button = document.querySelector(".context-ring") as HTMLButtonElement | null;
  if (!button) throw new Error("missing context ring button");
  await act(async () => {
    button.dispatchEvent(new MouseEvent("mouseover", { bubbles: true, relatedTarget: null }));
    await wait();
  });

  await rerender({ tabId: "new-tab" });
  const nextButton = document.querySelector(".context-ring") as HTMLButtonElement | null;
  if (!nextButton) throw new Error("missing context ring button after tab switch");
  await act(async () => {
    nextButton.dispatchEvent(new MouseEvent("mouseover", { bubbles: true, relatedTarget: null }));
    await wait();
  });

  await act(async () => {
    resolvers.get("new-tab")?.(contextPanelInfo(2));
    await wait();
  });
  await act(async () => {
    resolvers.get("old-tab")?.(contextPanelInfo(1));
    await wait();
  });
  await act(async () => {
    nextButton.dispatchEvent(new MouseEvent("mouseover", { bubbles: true, relatedTarget: null }));
    await wait(220);
  });

  eq(calls[0], "old-tab", "old tab request starts first");
  eq(calls[1], "new-tab", "new tab request starts after tab switch");
  const requestRow = [...document.querySelectorAll(".context-ring-popover__row")]
    .find((row) => row.querySelector(".context-ring-popover__label")?.textContent === "Requests");
  eq(
    requestRow?.querySelector(".context-ring-popover__value")?.textContent,
    "2",
    "stale old-tab response cannot overwrite the new tab info",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

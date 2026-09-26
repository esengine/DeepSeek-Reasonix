import assert from "node:assert/strict";
import { mock } from "node:test";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextPanel } from "../components/ContextPanel";
import { LocaleProvider } from "../lib/i18n";
import { installDesktopHostStub } from "./desktopHostStub";
import type { ContextPanelInfo } from "../lib/types";

const dom = new JSDOM('<html><body><div id="root"></div></body></html>', { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
  HTMLElement: dom.window.HTMLElement, Event: dom.window.Event, IS_REACT_ACT_ENVIRONMENT: true,
  ResizeObserver: class { observe() {} unobserve() {} disconnect() {} } });
Object.defineProperty(globalThis, "navigator", { value: { language: "en-US" }, configurable: true });
const start = 1_000_000;
mock.timers.enable({ apis: ["Date", "setTimeout", "setInterval"], now: start });

let info: ContextPanelInfo = {
  usedTokens: 100, windowTokens: 1000, promptTokens: 100, completionTokens: 10,
  totalTokens: 1000, reasoningTokens: 0, cacheHitTokens: 0, cacheMissTokens: 100,
  sessionCacheHitTokens: 0, sessionCacheMissTokens: 100, sessionCompletionTokens: 10,
  requestCount: 1, elapsedMs: 10_000, activeTurnStartedAt: start - 5_000, sessionCost: 1,
  readFiles: [], changedFiles: [],
};
let calls = 0;
installDesktopHostStub({ ContextPanel: () => { calls++; return Promise.resolve(info); } });
const root = createRoot(document.getElementById("root")!);
let refreshKey = 0;
async function render() {
  await act(async () => {
    root.render(<LocaleProvider><ContextPanel tabId="tab-a" sessionGen={1} refreshKey={refreshKey}
      context={{ used: 100, window: 1000, sessionTokens: 1000 }} /></LocaleProvider>);
  });
}
async function tick(ms: number) { await act(async () => { mock.timers.tick(ms); }); }
const runtime = () => document.querySelectorAll(".context-panel__summary-rows .context-panel__mini-stat strong")[2]?.textContent;

try {
  await render();
  assert.equal(runtime(), "15s", "completed turns plus the running turn so far");
  const fetched = calls;
  // Tool execution: no usage event arrives, so no snapshot is refetched.
  await tick(60_000);
  assert.equal(calls, fetched, "the runtime advances without polling the host");
  assert.equal(runtime(), "1m 15s", "a running turn keeps counting between model requests");

  info = { ...info, elapsedMs: 80_000, activeTurnStartedAt: undefined };
  refreshKey++;
  await render();
  await tick(30_000);
  assert.equal(runtime(), "1m 20s", "an idle session holds its accumulated runtime");
  console.log("context panel runtime: running turn ticks between requests, idle session holds");
} finally {
  await act(async () => root.unmount());
  mock.timers.reset();
  dom.window.close();
}

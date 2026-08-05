// Run: tsx src/__tests__/usage-stats-panel.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { UsageStatsRange, UsageStatsRequest } from "../lib/types";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

let failed = 0;
function ok(value: boolean, label: string) {
  if (value) process.stdout.write(`  PASS  ${label}\n`);
  else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function emptyStats(overrides: Partial<UsageStatsRange> = {}): UsageStatsRange {
  return {
    from: "", to: "", tokens: 1, requests: 1, turns: 1,
    cacheHit: 0, cacheMiss: 1, activeDays: 1,
    topModel: "deepseek/model", topProvider: "deepseek",
    daily: [], models: [], providers: [], ...overrides,
  };
}

function day(offset: number): string {
  const d = new Date();
  d.setDate(d.getDate() + offset);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
Object.defineProperty(globalThis.HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 720 });
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver;

let rejectCLIHeat: ((error: Error) => void) | undefined;
let rejectCLIMain: ((error: Error) => void) | undefined;
let resolveDesktopMain: ((stats: UsageStatsRange) => void) | undefined;
const usageStats = (req: UsageStatsRequest): Promise<UsageStatsRange> => {
  if (req.range !== "custom") {
    if (req.source === "cli") {
      return new Promise((_, reject) => { rejectCLIMain = reject; });
    }
    if (req.source === "desktop") {
      return new Promise((resolve) => { resolveDesktopMain = resolve; });
    }
    return Promise.resolve(emptyStats({
      tokens: 21,
      daily: Array.from({ length: 400 }, (_, index) => ({
        day: day(index - 399),
        total: 21,
        byModel: Object.fromEntries(Array.from({ length: 21 }, (_, modelIndex) => [`deepseek/model-${modelIndex + 1}`, 1])),
        byProvider: { deepseek: 21 },
        requests: 1,
        turns: 1,
        cacheHit: 0,
        cacheMiss: 1,
      })),
      models: Array.from({ length: 21 }, (_, index) => ({
        model: `deepseek/model-${index + 1}`,
        tokens: 1,
        percent: 100 / 21,
      })),
    }));
  }
  if (req.source === "cli") {
    return new Promise((_, reject) => { rejectCLIHeat = reject; });
  }
  return Promise.resolve(emptyStats({
    from: day(-279),
    to: day(0),
    daily: [{ day: day(0), total: 100, byModel: { "deepseek/model": 100 }, byProvider: { deepseek: 100 }, requests: 1, turns: 1, cacheHit: 10, cacheMiss: 10 }],
  }));
};
(window as unknown as { go: unknown }).go = { main: { App: { UsageStats: usageStats } } };

const [{ LocaleProvider }, { UsageStatsPanel }] = await Promise.all([
  import("../lib/i18n"),
  import("../components/UsageStatsPanel"),
]);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);
await act(async () => {
  root.render(<LocaleProvider><UsageStatsPanel /></LocaleProvider>);
  await new Promise((resolve) => setTimeout(resolve, 0));
});

for (let i = 0; i < 20 && rootEl.querySelectorAll("rect.usage-stats__heat-cell--5").length === 0; i += 1) {
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
}
ok(rootEl.querySelectorAll("rect.usage-stats__heat-cell--5").length === 1, "initial source heat data is rendered");
ok(rootEl.textContent?.includes("Completed turns") === true && !rootEl.textContent?.includes("Sessions"), "completed turns are not mislabeled as sessions");
ok(rootEl.querySelectorAll("rect.usage-stats__bar-hit").length === 180, "long custom trends render at most 180 interactive columns");
ok(rootEl.textContent?.includes("Showing the latest 180 days") === true, "bounded trends disclose the visible window");
  const modelSegments = rootEl.querySelectorAll("circle.usage-stats__donut-seg");
  ok(modelSegments.length === 6, "21 models collapse into five steps plus Other");
  // Model colours map to the fixed --chart-1..5 series (rank order) plus the
  // gray --chart-other step; each series colour is mixed toward --bg-elev
  // like the heatmap levels (light/dark hex variants live in styles.css), so
  // the assertions only check the rank-to-variable mapping.
  const seriesVar = (color: string) => color.match(/var\(--chart-\d+\)/)?.[0] ?? "";
  const primaryColour = modelSegments[0]?.getAttribute("stroke") ?? "";
  ok(seriesVar(primaryColour) === "var(--chart-1)" && primaryColour.includes("72%") && primaryColour.includes("var(--bg-elev)"), "most-used model keeps the first series colour softened toward the card background");
  const lastTopColour = modelSegments[4]?.getAttribute("stroke") ?? "";
  ok(seriesVar(lastTopColour) === "var(--chart-5)", "fifth model gets the fifth series colour");
  const otherColour = modelSegments[modelSegments.length - 1]?.getAttribute("stroke") ?? "";
  ok(otherColour === "var(--chart-other)", "models beyond the top five collapse into the gray Other step");
  const series = [...modelSegments].slice(0, 5).map((seg) => seriesVar(seg.getAttribute("stroke") ?? ""));
  ok(new Set(series).size === 5, "the five model steps each get a distinct series colour");

const otherToggle = rootEl.querySelector<HTMLButtonElement>("button.usage-stats__model-row--expandable");
ok(otherToggle instanceof dom.window.HTMLButtonElement, "Other uses one native button for the whole expandable row");
ok(otherToggle?.querySelector("button") === null, "Other does not nest an interactive button inside another button");
ok(otherToggle?.getAttribute("aria-expanded") === "false", "Other exposes its collapsed state on the row button");
await act(async () => {
  otherToggle?.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(otherToggle?.getAttribute("aria-expanded") === "true", "clicking the Other row exposes its expanded state");
ok(rootEl.querySelector(".usage-stats__model-other-wrap--open") !== null, "clicking the Other row opens the detail list");

await act(async () => {
  modelSegments[modelSegments.length - 1]?.dispatchEvent(new dom.window.MouseEvent("mouseover", { bubbles: true, clientX: 120, clientY: 120 }));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(rootEl.querySelectorAll(".usage-stats__tip-breakdown .usage-stats__tip-row--other").length === 5, "donut tooltip caps the visible Other breakdown");
ok(rootEl.querySelector(".usage-stats__tip-breakdown .usage-stats__tip-more") !== null, "donut tooltip discloses omitted Other models");
await act(async () => {
  modelSegments[modelSegments.length - 1]?.dispatchEvent(new dom.window.MouseEvent("mouseout", { bubbles: true }));
  rootEl.querySelector("rect.usage-stats__bar-hit")?.dispatchEvent(new dom.window.MouseEvent("mouseover", { bubbles: true, clientX: 120, clientY: 120 }));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const trendTip = rootEl.querySelector(".usage-stats__tip--screen");
ok(trendTip?.querySelectorAll(".usage-stats__tip-row--other").length === 5, "daily tooltip caps the visible Other breakdown");
ok(trendTip?.querySelector(".usage-stats__tip-more") !== null, "daily tooltip discloses omitted Other models");

const cliButton = [...rootEl.querySelectorAll("button")].find((button) => button.textContent === "CLI");
if (!cliButton) throw new Error("missing CLI source button");
await act(async () => {
  cliButton.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(rootEl.querySelectorAll("rect.usage-stats__heat-cell--5").length === 0, "source switch clears previous heat data while loading");

await act(async () => {
  rejectCLIHeat?.(new Error("expected test failure"));
  rejectCLIMain?.(new Error("expected main test failure"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(rootEl.querySelectorAll("rect.usage-stats__heat-cell--5").length === 0, "failed current-source fetch cannot revive previous heat data");
ok(rootEl.querySelectorAll("circle.usage-stats__donut-seg").length === 0, "failed current-source fetch cannot retain previous main stats");

const desktopButton = [...rootEl.querySelectorAll("button")].find((button) => button.textContent === "Desktop");
if (!desktopButton) throw new Error("missing Desktop source button");
await act(async () => {
  desktopButton.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const customButton = [...rootEl.querySelectorAll("button")].find((button) => button.textContent === "Custom");
if (!customButton) throw new Error("missing Custom range button");
await act(async () => {
  customButton.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  resolveDesktopMain?.(emptyStats({ tokens: 777 }));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(!rootEl.textContent?.includes("777"), "incomplete custom range invalidates an older main response");

await act(async () => root.unmount());
dom.window.close();
if (failed > 0) process.exit(1);

// Run: tsx src/__tests__/subagent-progress-card.test.tsx
//
// Verifies the ToolCard rendering of the sub-agent progress chip (phase +
// elapsed + recent activity) and the expanded preview body (reasoning /
// response preview / notices), including the terminal phase visuals.

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ToolCard } from "../components/ToolCard";
import { LocaleProvider } from "../lib/i18n";
import { setReasoningSummaryEnabled } from "../lib/reasoningSummaryPreference";
import { hydrateSessionExperience } from "../lib/sessionExperience";
import type { Item, SubagentProgress } from "../lib/useController";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

type ToolItem = Extract<Item, { kind: "tool" }>;

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  dom.window.matchMedia = () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
  return dom;
}

function makeItem(phase: SubagentProgress["phase"], over: Partial<SubagentProgress> = {}): ToolItem {
  const now = Date.now();
  return {
    kind: "tool",
    id: `task-${phase}`,
    name: "task",
    args: "{}",
    readOnly: true,
    status: phase === "completed" || phase === "failed" ? "done" : phase === "cancelled" ? "stopped" : "running",
    subagentProgress: {
      phase,
      reasoning: "**thinking** step by step\n\n- inspect\n- verify",
      text: "draft answer preview",
      notice: "heads up",
      lastActivityAt: now - 3_000,
      startedAt: now - 12_000,
      truncated: false,
      ...over,
    },
  };
}

console.log("\nsubagent progress card");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  hydrateSessionExperience("standard");
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: makeItem("reasoning") })));
    for (let i = 0; i < 50; i += 1) {
      await flushTimers();
      if (document.querySelector(".tool__subagent-preview .md")) break;
    }
  });
  ok(!!document.querySelector(".tool__subagent-preview .md"), "standard mode expands reasoning when the card mounts mid-stream");

  const responding = makeItem("responding");
  responding.id = "task-reasoning";
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: responding })));
    await flushTimers();
  });
  ok(!!document.querySelector(".tool__subagent-preview"), "standard mode keeps the subagent card open after reasoning starts responding");
  ok(!!document.querySelector(".tool__subagent-preview .md"), "standard mode keeps completed subagent reasoning expanded while the task runs");

  const completed = makeItem("completed");
  completed.id = "task-reasoning";
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: completed })));
    await flushTimers();
  });
  ok(!document.querySelector(".tool__subagent-preview"), "standard mode collapses the untouched subagent card after the task settles");
  await act(async () => root.unmount());
  dom.window.close();
  hydrateSessionExperience("standard");
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  hydrateSessionExperience("deep");
  const running = makeItem("reasoning");
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: running })));
    for (let i = 0; i < 50; i += 1) {
      await flushTimers();
      if (document.querySelector(".tool__subagent-preview .md")) break;
    }
  });
  ok(!!document.querySelector(".tool__subagent-preview .md"), "deep mode opens live sub-agent reasoning");

  const completed = makeItem("completed", { durationMs: 42_000 });
  completed.id = running.id;
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: completed })));
    await flushTimers();
  });
  ok(!!document.querySelector(".tool__subagent-preview .md"), "deep mode keeps completed sub-agent reasoning visible");

  await act(async () => {
    document.querySelector<HTMLButtonElement>(".tool__head")?.click();
    await flushTimers();
  });
  ok(!document.querySelector(".tool__subagent-preview"), "manual card collapse still wins in deep mode");

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { key: "completed-history", item: completed })));
    for (let i = 0; i < 50; i += 1) {
      await flushTimers();
      if (document.querySelector(".tool__subagent-preview .md")) break;
    }
  });
  ok(!!document.querySelector(".tool__subagent-preview .md"), "deep mode opens completed sub-agent reasoning restored from history");

  await act(async () => root.unmount());
  dom.window.close();
  hydrateSessionExperience("standard");
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  // Running card: chip shows phase, live elapsed and recent activity.
  const running = makeItem("reasoning");
  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: running })),
    );
    await flushTimers();
  });
  const chip = document.querySelector(".tool__subagent-chip");
  ok(!!chip, "running card renders the progress chip");
  ok(chip?.textContent?.includes("reasoning"), "chip shows the phase label");
  ok(chip?.textContent?.includes("12s"), "chip shows the running elapsed");
  ok(chip?.textContent?.includes("3s ago"), "chip shows recent activity");
  ok(chip?.getAttribute("data-phase") === "reasoning", "chip carries the phase attribute");

  // Standard keeps active process work reachable without an extra card click.
  const head = document.querySelector(".tool__head") as HTMLButtonElement | null;
  ok(!!head, "card head renders");
  ok(!!document.querySelector(".tool__subagent-preview"), "active standard card renders the preview block");
  ok(document.querySelector(".tool__subagent-preview-label")?.textContent === "Reasoning", "reasoning section label");
  ok(!document.querySelector(".tool__subagent-preview .reasoning-summary"), "active standard reasoning is not replaced by a summary");
  ok(!!document.querySelector(".tool__subagent-preview .md"), "active standard reasoning renders full Markdown");
  ok(document.body.textContent?.includes("draft answer preview"), "response preview text visible");
  ok(document.body.textContent?.includes("heads up"), "notice preview text visible");
  ok(document.body.textContent?.includes("thinking step by step"), "reasoning preview text visible after expanding");
  ok(document.querySelector(".tool__subagent-preview-text strong")?.textContent === "thinking", "reasoning preview renders Markdown emphasis");
  ok(document.querySelectorAll(".tool__subagent-preview-text li").length === 2, "reasoning preview renders Markdown lists");

  // The section label toggles back to the summary and re-expands.
  const reasoningLabel = document.querySelector(".tool__subagent-preview-label") as HTMLButtonElement | null;
  await act(async () => {
    reasoningLabel?.click();
    await flushTimers();
  });
  ok(!document.querySelector(".tool__subagent-preview .md"), "clicking the reasoning label collapses back to the summary");
  ok(document.querySelector(".tool__subagent-preview .reasoning-summary")?.textContent === "- verify", "collapsed reasoning section shows the summary again");
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".tool__subagent-preview-label")?.click();
    for (let i = 0; i < 50; i += 1) {
      await flushTimers();
      if (document.querySelector(".tool__subagent-preview .md strong")) break;
    }
  });
  ok(!!document.querySelector(".tool__subagent-preview .md strong"), "clicking the reasoning label expands the full Markdown");

  await act(async () => {
    setReasoningSummaryEnabled(false);
    root.render(
      React.createElement(LocaleProvider, null, React.createElement(ToolCard, { key: "summary-off", item: running })),
    );
    await flushTimers();
  });
  ok(!document.querySelector(".tool__subagent-preview .reasoning-summary"), "legacy summary-off cannot collapse active Standard reasoning");
  ok(!!document.querySelector(".tool__subagent-preview .md"), "legacy summary-off keeps active process Markdown reachable");
  await act(async () => {
    setReasoningSummaryEnabled(true);
    root.render(
      React.createElement(LocaleProvider, null, React.createElement(ToolCard, { key: "summary-on", item: running })),
    );
    await flushTimers();
  });
  ok(!!document.querySelector(".tool__subagent-preview .md"), "legacy summary-on leaves the canonical active preview intact");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  // Terminal chips: completed/failed/cancelled show the final duration, no
  // recent-activity suffix, and the existing status visuals.
  const completed = makeItem("completed", { durationMs: 42_000 });
  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: completed })),
    );
    await flushTimers();
  });
  const chip = document.querySelector(".tool__subagent-chip");
  ok(chip?.textContent?.includes("completed"), "completed chip label");
  ok(chip?.textContent?.includes("42s"), "completed chip shows the terminal duration");
  ok(!chip?.textContent?.includes("ago"), "terminal chip drops the recent-activity suffix");
  ok(!!document.querySelector(".tool__status-icon--ok"), "completed card shows the done icon");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  const cancelled = makeItem("cancelled", { durationMs: 500 });
  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: cancelled })),
    );
    await flushTimers();
  });
  ok(document.querySelector(".tool__subagent-chip")?.textContent?.includes("cancelled"), "cancelled chip label");
  ok(!!document.querySelector(".tool__status-icon--stopped"), "cancelled card shows the stopped icon");
  ok(document.querySelector(".tool__subagent-chip")?.classList.contains("tool__subagent-chip--cancelled"), "chip carries the cancelled modifier class");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const live = makeItem("partial");
  live.status = "error";
  live.subagentOutcome = ["sa_live", "partial", "completion_uncertain", true];

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item: live })));
    await flushTimers();
  });
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".tool__head")?.click();
    for (let i = 0; i < 50; i += 1) {
      await flushTimers();
      if (document.querySelector(".tool__subagent-outcome")) break;
    }
  });
  ok(document.querySelector(".tool__subagent-outcome")?.textContent?.includes("partially complete"), "live outcome tuple renders through the lazy card boundary");
  ok(document.querySelector(".tool__subagent-outcome")?.textContent?.includes("sa_live"), "live outcome keeps the stable subagent reference");
  ok(document.querySelector(".tool__subagent-outcome")?.textContent?.includes("completion_uncertain"), "live outcome exposes the bounded error code");

  const history: ToolItem = {
    kind: "tool",
    id: "task-history-outcome",
    name: "task",
    args: "{}",
    readOnly: true,
    status: "error",
    output: "Subagent reference (failed): sa_history\nSubagent outcome: status=failed retryable=false error_code=provider_error",
  };
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { key: history.id, item: history })));
    await flushTimers();
  });
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".tool__head")?.click();
    for (let i = 0; i < 50; i += 1) {
      await flushTimers();
      if (document.querySelector(".tool__subagent-outcome code")?.textContent === "sa_history") break;
    }
  });
  ok(document.querySelector(".tool__subagent-outcome code")?.textContent === "sa_history", "history outcome is parsed only when the card is opened");
  ok(document.querySelector(".tool__subagent-outcome")?.textContent?.includes("failed"), "history outcome uses the same localized status projection");

  await act(async () => root.unmount());
  dom.window.close();
}

console.log(`\nsubagent progress card: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

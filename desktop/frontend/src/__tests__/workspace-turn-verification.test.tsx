// Run: tsx src/__tests__/workspace-turn-verification.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { WORKSPACE_TURN_VERIFICATION_ID, WorkspacePanel } from "../components/WorkspacePanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { WireCompletionSummary } from "../lib/types";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

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

const flushPromises = () => new Promise((resolve) => setTimeout(resolve, 0));

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function installDom() {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
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
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.ResizeObserver = TestResizeObserver;
  dom.window.ResizeObserver = TestResizeObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  (dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void }).attachEvent = () => {};
  (dom.window.HTMLElement.prototype as unknown as { detachEvent: () => void }).detachEvent = () => {};
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", { configurable: true, get: () => 320 });
  Object.defineProperty(dom.window.HTMLElement.prototype, "offsetHeight", { configurable: true, get: () => 300 });
  Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
    configurable: true,
    value: () => ({ x: 0, y: 0, top: 0, left: 0, right: 320, bottom: 300, width: 320, height: 300, toJSON: () => ({}) }) as DOMRect,
  });
  return dom;
}

type WorkspaceProps = Parameters<typeof WorkspacePanel>[0];

async function createHarness(props: Partial<WorkspaceProps>) {
  const dom = installDom();
  window.go = {
    main: {
      App: {
        ListDirForTab: async () => [],
        SearchFileRefsForTab: async () => [],
        WorkspaceGitHistory: async () => [],
        WorkspaceChanges: async () => ({ files: [], gitAvailable: true }),
        WorkspaceChangeDetail: async () => ({}),
        ReadFileForTab: async (_tabID, path) => ({ path, body: "", size: 0, truncated: false, binary: false }),
      } as Partial<AppBindings> as AppBindings,
    },
  };
  const root = createRoot(document.getElementById("root")!);
  let currentProps: WorkspaceProps = {
    open: true,
    tabId: "tab-a",
    cwd: "/repo",
    maximized: false,
    onClose: () => {},
    onToggleMaximized: () => {},
    ...props,
  };
  const rerender = async (next: Partial<WorkspaceProps> = {}) => {
    currentProps = { ...currentProps, ...next };
    await act(async () => {
      root.render(<LocaleProvider><WorkspacePanel {...currentProps} /></LocaleProvider>);
      await flushPromises();
    });
  };
  await rerender();
  return { dom, root, rerender };
}

async function closeHarness(dom: JSDOM, root: Root) {
  await act(async () => root.unmount());
  dom.window.close();
}

function summary(mutations: number, checksFailed = 1): WireCompletionSummary {
  return {
    preset: "balanced",
    verdict: "partial",
    mutations,
    checks_passed: 2,
    checks_failed: checksFailed,
    checks_suppressed: 1,
    review: "passed",
    gap_kinds: ["stale_check", "future_internal_value"],
    constraint_degraded: true,
  };
}

console.log("\nworkspace turn verification");

{
  const current = summary(3);
  const { dom, root } = await createHarness({ initialViewMode: "changed", completionSummary: current });
  await waitFor("turn verification summary", () => document.getElementById(WORKSPACE_TURN_VERIFICATION_ID) !== null);
  const text = document.querySelector(".workspace-completion-summary")?.textContent ?? "";
  ok(text.includes("Partially complete") && text.includes("3 changes"), "summary renders localized verdict and metrics");
  ok(text.includes("stale checks") && text.includes("Other"), "summary safely labels known and unknown gaps");
  ok(text.includes("Turn verification limited"), "summary explains constrained verification");
  ok(!text.includes("balanced") && !text.includes("partial") && !text.includes("stale_check"), "summary exposes no raw enum values");
  const title = document.getElementById(`${WORKSPACE_TURN_VERIFICATION_ID}-title`);
  ok(title?.tagName === "H3", "turn verification title is a heading, not a button");
  ok(document.querySelector(`#${WORKSPACE_TURN_VERIFICATION_ID} button`) === null, "summary does not expose a clickable control");
  await closeHarness(dom, root);
}

{
  const legacyDeliverySummary: WireCompletionSummary = {
    preset: "balanced",
    verdict: "partial",
    mutations: 1,
    checks_passed: 0,
    checks_failed: 0,
    checks_suppressed: 0,
    review: "passed",
    gap_kinds: ["unverified_change"],
    constraint_degraded: false,
  };
  const { dom, root, rerender } = await createHarness({
    initialViewMode: "changed",
    completionSummary: legacyDeliverySummary,
    qualityFloor: "delivery",
  });
  await waitFor("delivery attention styling", () => document.querySelector(".workspace-completion-summary") !== null);
  ok(document.querySelector(".workspace-completion-summary")?.classList.contains("workspace-completion-summary--attention"), "legacy delivery summary uses delivery-floor attention styling");
  await rerender({ qualityFloor: "standard" });
  ok(!document.querySelector(".workspace-completion-summary")?.classList.contains("workspace-completion-summary--attention"), "legacy standard summary remains neutral");
  await closeHarness(dom, root);
}

{
  const current = summary(2);
  const historical = summary(7);
  const { dom, root, rerender } = await createHarness({ initialViewMode: "changed", completionSummary: current });
  await waitFor("current summary", () => document.body.textContent?.includes("2 changes") === true);
  let scrolled = 0;
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => { scrolled += 1; } });
  const request = { id: 1, summary: historical, tabId: "tab-a", turnStartAt: 100, currentSummary: current };
  await rerender({ verificationRevealRequest: request, turnStartAt: 100 } as Partial<WorkspaceProps>);
  await waitFor("same-view scroll", () => scrolled > 0);
  ok(document.body.textContent?.includes("7 changes"), "same-view reveal displays the requested historical summary");
  ok(scrolled === 1, "same-view reveal scrolls exactly once");

  await rerender({ completionSummary: undefined, turnStartAt: 200 } as Partial<WorkspaceProps>);
  await waitFor("stale summary cleared", () => document.getElementById(WORKSPACE_TURN_VERIFICATION_ID) === null);
  ok(!document.body.textContent?.includes("7 changes"), "a new turn clears the historical reveal");
  await closeHarness(dom, root);
}

{
  const current = summary(4);
  const historical = summary(9);
  const { dom, root, rerender } = await createHarness({ initialViewMode: "files", completionSummary: current });
  let scrolled = 0;
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => { scrolled += 1; } });
  const request = { id: 2, summary: historical, tabId: "tab-a", turnStartAt: 300, currentSummary: current };
  await rerender({ initialViewMode: "changed", verificationRevealRequest: request, turnStartAt: 300 } as Partial<WorkspaceProps>);
  await waitFor("navigation reveal", () => document.body.textContent?.includes("9 changes") === true && scrolled > 0);
  ok(document.getElementById(WORKSPACE_TURN_VERIFICATION_ID) !== null, "reveal navigates from Files to the change overview");
  ok(scrolled === 1, "navigation reveal scrolls after the overview mounts");
  await closeHarness(dom, root);
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

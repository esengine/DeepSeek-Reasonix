// Run: tsx src/__tests__/workspace-turn-verification.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { WORKSPACE_TURN_VERIFICATION_ID, WorkspacePanel } from "../components/WorkspacePanel";
import { WorkspaceTurnResult } from "../components/WorkspaceTurnResult";
import { TurnCheckDetails } from "../components/TurnCheckDetails";
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
        WorkspaceTurnChanges: async () => ({ turn: 0, coverage: "unknown", files: [], added: 0, removed: 0, reasons: [] }),
        WorkspaceTurnChangeDetail: async () => null,
        TurnCheckLog: async () => null,
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
  ok(document.querySelector(".workspace-turn-result") === null, "workspace overview does not imply the current turn covers all changes");
  await closeHarness(dom, root);
}

{
  const current = summary(2);
  const historical = { ...summary(7), receipt: { verdict: "partial", verifications: [{ command: "go test ./...", passed: false, exitCode: 1, toolCallId: "check-old", stale: true }] } };
  const request = { id: 1, summary: historical, tabId: "tab-a", turnStartAt: 100, currentSummary: current, sessionPath: "/old.json", view: "checks" as const };
  const { dom, root, rerender } = await createHarness({ initialViewMode: "changed", completionSummary: current, sessionPath: "/old.json", verificationRevealRequest: request, turnStartAt: 100 });
  await waitFor("historical check", () => document.body.textContent?.includes("go test ./...") === true);
  const text = document.body.textContent ?? "";
  ok(text.includes("Exit code: 1"), "actual exit code appears with the historical command");
  ok(!text.includes("7 files") && !text.includes("7 changes"), "mutation receipts are not presented as a diff inventory");
  ok(text.includes("stale") || text.includes("Stale"), "later changes mark checks stale");
  ok(!Array.from(document.querySelectorAll("button")).some(b => /Run|Retry|Continue verification/.test(b.textContent ?? "")), "result panel has only view actions");
  await rerender({ sessionPath: "/new.json" });
  ok(document.querySelector(".workspace-turn-result") === null, "session switch immediately fences a historical request");
  await closeHarness(dom, root);
}

{
  const current = summary(0);
  const historical = { ...summary(9), turnId: "turn-old", receipt: { verdict: "complete", diff: { id: "0:42", turn: 0, coverage: "complete" as const, files: [{ path: "src/old.ts", kind: "modify", added: 2, removed: 1 }], added: 2, removed: 1, reasons: [] }, verifications: [] } };
  const request = { id: 2, summary: historical, tabId: "tab-a", turnStartAt: 300, currentSummary: current, sessionPath: "/history.json", view: "changes" as const };
  const { dom, root, rerender } = await createHarness({ initialViewMode: "changed", completionSummary: current, verificationRevealRequest: request, sessionPath: "/history.json", turnStartAt: 300 });
  await waitFor("frozen result", () => document.body.textContent?.includes("src/old.ts") === true);
  ok(document.body.textContent?.includes("+2"), "historical counts come from the frozen receipt");
  await rerender({ completionSummary: summary(42) });
  ok(document.body.textContent?.includes("src/old.ts"), "a current summary refresh preserves the selected historical result");
  await rerender({ turnStartAt: 301 });
  ok(document.querySelector(".workspace-turn-result") === null, "a new turn fences the old reveal");
  await closeHarness(dom, root);
}

{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const pending = new Map<string, (value: unknown) => void>();
  const deferred = (key: string) => new Promise(resolve => pending.set(key, resolve));
  window.go = { main: { App: {
    WorkspaceTurnChanges: (_tab, session) => deferred(`files:${session}`),
    WorkspaceTurnChangeDetail: (_tab, session) => deferred(`detail:${session}`),
    TurnCheckLog: (_tab, session) => deferred(`log:${session}`),
  } as Partial<AppBindings> as AppBindings } };
  const diff = { id: "frozen", turn: 0, coverage: "complete" as const, files: [{ path: "f.ts", kind: "modify", added: 1, removed: 1 }], added: 1, removed: 1, reasons: [] };
  const result = { ...summary(1), receipt: { verdict: "partial", diff, verifications: [{ command: "test", passed: false, toolCallId: "check", toolResultId: "entry" }] } };
  const paint = (session: string) => act(async () => root.render(<LocaleProvider><WorkspaceTurnResult summary={result} tabId="a" sessionPath={session} initialView="changes" onAllChanges={() => {}} /></LocaleProvider>));
  await paint("old");
  await paint("new");
  await act(async () => pending.get("files:old")!(diff));
  ok(document.querySelector<HTMLButtonElement>(".turn-file-list__entry")?.disabled, "old session response cannot enable the new session file list");
  await act(async () => pending.get("files:new")!(diff));
  await act(async () => document.querySelector<HTMLButtonElement>(".turn-file-list__entry")!.click());
  await paint("replacement");
  await act(async () => pending.get("detail:new")!({ ...diff.files[0], patch: "stale private patch" }));
  ok(!document.body.textContent?.includes("stale private patch"), "late diff response is discarded after session replacement");
  const logs = (session: string) => act(async () => root.render(<LocaleProvider><TurnCheckDetails summary={result} tabId="a" sessionPath={session} /></LocaleProvider>));
  await logs("old");
  await act(async () => { const details = document.querySelector<HTMLDetailsElement>("details")!; details.open = true; details.dispatchEvent(new Event("toggle")); });
  await waitFor("old log request", () => pending.has("log:old"));
  await logs("new");
  await waitFor("new log request", () => pending.has("log:new"));
  await act(async () => pending.get("log:old")!({ output: "stale private log" }));
  ok(!document.body.textContent?.includes("stale private log"), "late log response is discarded after session replacement");
  await act(async () => pending.get("log:new")!(null));
  ok(document.body.textContent?.includes("Logs have not arrived, were cleared, or cannot be linked."), "cleared logs remain explicitly unavailable");
  await closeHarness(dom, root);
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

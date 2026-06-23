// Run: tsx src/__tests__/tab-switch-hydration.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { useController } from "../lib/useController";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, JobView, Meta, TabMeta } from "../lib/types";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function tabMeta(id: string, overrides: Partial<TabMeta> = {}): TabMeta {
  const workspaceRoot = `/repo/${id}`;
  return {
    id,
    scope: "project",
    workspaceRoot,
    workspaceName: id,
    workspacePath: workspaceRoot,
    gitBranch: "main",
    topicId: `topic-${id}`,
    topicTitle: id,
    label: `model-${id}`,
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: false,
    cwd: workspaceRoot,
    ...overrides,
  };
}

function metaFor(tab: TabMeta): Meta {
  return {
    label: tab.label,
    ready: tab.ready,
    startupErr: tab.startupErr,
    eventChannel: "agent:event",
    cwd: tab.cwd || tab.workspaceRoot,
    workspaceRoot: tab.workspaceRoot,
    workspaceName: tab.workspaceName,
    workspacePath: tab.workspacePath,
    gitBranch: tab.gitBranch,
    autoApproveTools: false,
    bypass: false,
    collaborationMode: tab.collaborationMode ?? "normal",
    toolApprovalMode: tab.toolApprovalMode ?? "ask",
    tokenMode: tab.tokenMode ?? "full",
    goal: "",
    goalStatus: "stopped",
  };
}

function userMessage(content: string): HistoryMessage {
  return { role: "user", content };
}

console.log("\ntab switch hydration");

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
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const context: ContextInfo = { used: 12, window: 100, sessionTokens: 12 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];
const tabA = tabMeta("tab-a", { active: true });
const tabB = tabMeta("tab-b");
const tabC = tabMeta("tab-c");
let backendActiveId = "tab-a";
const historyB = deferred<HistoryMessage[]>();
const setActiveBGate = deferred<void>();
const historyCalls: string[] = [];
let setActiveCalls = 0;
let newSessionCalls = 0;
const runningTabs = new Set<string>();
const tabsById = new Map([tabA, tabB, tabC].map((tab) => [tab.id, tab]));

function currentTabs(): TabMeta[] {
  return [tabA, tabB, tabC].map((tab) => ({ ...tab, active: tab.id === backendActiveId, running: runningTabs.has(tab.id) }));
}

window.runtime = {
  EventsOn: () => () => {},
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => currentTabs(),
      MetaForTab: async (tabID: string) => metaFor(tabsById.get(tabID) ?? tabA),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (tabID: string) => {
        historyCalls.push(tabID);
        if (tabID === "tab-b") return historyB.promise;
        return [userMessage("cached A")];
      },
      NewSession: async () => {
        newSessionCalls += 1;
      },
      ReplayPendingPrompts: async () => {},
      SetActiveTab: async (tabID: string) => {
        setActiveCalls += 1;
        if (tabID === "tab-b") await setActiveBGate.promise;
        backendActiveId = tabID;
      },
      SubmitToTab: async (tabID: string) => {
        runningTabs.add(tabID);
      },
      SubmitDisplayToTab: async (tabID: string) => {
        runningTabs.add(tabID);
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;

function Probe() {
  controller = useController();
  return null;
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});
await waitFor("initial active tab", () => controller?.activeTabId === "tab-a" && controller.state.items.length === 1);

let switchToB: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  switchToB = controller?.switchTab("tab-b", tabB);
  await flushPromises();
});

eq(setActiveCalls, 1, "SetActiveTab is called for the selected tab");
eq(controller?.activeTabId, "tab-b", "switchTab updates the active tab before backend activation resolves");
eq(controller?.state.meta?.label, "model-tab-b", "switchTab applies optimistic tab metadata immediately");
eq(controller?.state.items.length, 0, "uncached target tab does not keep the previous transcript visible");
eq(controller?.state.hydrating, true, "target tab shows lightweight hydration state while backend activation is pending");
eq(controller?.state.backendActivationPending, true, "target tab gates unscoped actions while backend activation is pending");
ok(!historyCalls.includes("tab-b"), "HistoryForTab is not requested before SetActiveTab completes");

let newSessionWhileSwitching: Promise<void> | undefined;
await act(async () => {
  newSessionWhileSwitching = controller?.newSession();
  await flushPromises();
});
eq(newSessionCalls, 0, "newSession waits for backend activation before using the unscoped binding");

await act(async () => {
  setActiveBGate.resolve();
  await switchToB;
  await newSessionWhileSwitching;
  await flushPromises();
});
eq(newSessionCalls, 1, "newSession runs after the selected tab is active in the backend");
await waitFor("tab-b history request", () => historyCalls.includes("tab-b"));

await act(async () => {
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await waitFor("tab-a restored", () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"));

await act(async () => {
  historyB.resolve([userMessage("late B")]);
  await historyB.promise;
  await flushPromises();
});

eq(controller?.activeTabId, "tab-a", "late history for another tab does not change the active tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "cached A") ?? false, "late history for another tab does not overwrite the active transcript");
ok(!(controller?.state.items.some((item) => item.kind === "user" && item.text === "late B") ?? false), "late history stays scoped to its tab state");

await act(async () => {
  controller?.sendToTab("tab-c", "streaming C");
  await flushPromises();
});
await act(async () => {
  await controller?.switchTab("tab-c", tabC);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-c", "switching to a cached running tab still updates the active tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "streaming C") ?? false, "cached running tab keeps its optimistic transcript");
ok(!historyCalls.includes("tab-c"), "cached running tab skips history hydration");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

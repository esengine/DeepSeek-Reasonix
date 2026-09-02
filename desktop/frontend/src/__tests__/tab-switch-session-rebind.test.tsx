// Run: tsx src/__tests__/tab-switch-session-rebind.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { useController } from "../lib/useController";
import { historySliceFromMessages } from "./mockHistorySlice";
import type {
  BalanceInfo,
  CheckpointMeta,
  ContextInfo,
  EffortInfo,
  HistoryMessage,
  HistorySliceRequest,
  JobView,
  Meta,
  TabMeta,
} from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1;
  else failed += 1;
}

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, actual === expected ? label : `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => { await flushPromises(); });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function tabMeta(id: string, sessionGeneration: number, active = false): TabMeta {
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
    sessionPath: `${workspaceRoot}/sessions/${id}.jsonl`,
    sessionGeneration,
    label: `model-${id}`,
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active,
    cwd: workspaceRoot,
  };
}

function metaFor(tab: TabMeta): Meta {
  return {
    label: tab.label,
    ready: tab.ready,
    eventChannel: "agent:event",
    cwd: tab.cwd || tab.workspaceRoot,
    workspaceRoot: tab.workspaceRoot,
    workspaceName: tab.workspaceName,
    workspacePath: tab.workspacePath,
    sessionPath: tab.sessionPath,
    sessionGeneration: tab.sessionGeneration,
    gitBranch: tab.gitBranch,
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
  };
}

function userMessage(content: string): HistoryMessage {
  return { role: "user", content };
}

console.log("\ntab switch session rebind");

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

const context: ContextInfo = { used: 0, window: 100, sessionTokens: 0 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];
const tabA = tabMeta("tab-a", 1, true);
const tabO = tabMeta("tab-o", 1);
const tabsById = new Map([tabA, tabO].map((tab) => [tab.id, tab]));
let backendActiveId = "tab-a";
let generationTwoHistory = deferred<HistoryMessage[]>();
let heldTabOHistory: Promise<HistoryMessage[]> | null = null;
let heldListTabs: Promise<TabMeta[]> | null = null;
let heldListTabsStarted = false;

function currentTabs(): TabMeta[] {
  return Array.from(tabsById.values()).map((tab) => ({ ...tab, active: tab.id === backendActiveId }));
}

window.runtime = {
  EventsOn: () => () => {},
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => {
        if (heldListTabs) {
          const promise = heldListTabs;
          heldListTabs = null;
          heldListTabsStarted = true;
          return promise;
        }
        return currentTabs();
      },
      MetaForTab: async (tabID: string) => metaFor(tabsById.get(tabID) ?? tabA),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (tabID: string) => {
        if (tabID === "tab-o" && heldTabOHistory) {
          const promise = heldTabOHistory;
          heldTabOHistory = null;
          return promise;
        }
        const generation = tabsById.get(tabID)?.sessionGeneration ?? 0;
        return [userMessage(tabID === "tab-o" ? `history O generation ${generation}` : "history A")];
      },
      HistorySliceForTab: async (tabID: string, request: HistorySliceRequest) => {
        const messages = await window.go.main.App.HistoryForTab(tabID);
        return historySliceFromMessages(tabID, messages, request);
      },
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      SetActiveTab: async (tabID: string) => { backendActiveId = tabID; },
      OpenProjectTab: async (workspaceRoot: string) => {
        const target = Array.from(tabsById.values()).find((tab) => tab.workspaceRoot === workspaceRoot) ?? tabA;
        backendActiveId = target.id;
        return { ...target, active: true };
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;
function Probe() { controller = useController(); return null; }

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

await act(async () => { root.render(<Probe />); await flushPromises(); });
await waitFor("initial session", () => controller?.state.items.some((item) => item.kind === "user" && item.text === "history A") ?? false);
await act(async () => { await controller?.openProjectTab(tabO.workspaceRoot, tabO.topicId || ""); await flushPromises(); });
await waitFor("generation one", () => controller?.state.items.some((item) => item.kind === "user" && item.text === "history O generation 1") ?? false);
await act(async () => { await controller?.openProjectTab(tabA.workspaceRoot, tabA.topicId || ""); await flushPromises(); });
await waitFor("source restored", () => controller?.activeTabId === "tab-a");

const reboundTabO = { ...tabO, sessionGeneration: 2 };
tabsById.set("tab-o", reboundTabO);
generationTwoHistory = deferred<HistoryMessage[]>();
heldTabOHistory = generationTwoHistory.promise;
let generationTwoSwitch: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  generationTwoSwitch = controller?.switchTab("tab-o", reboundTabO);
  await flushPromises();
});

eq(controller?.activeTabId, "tab-o", "generation-rebound tab becomes the selected target");
eq(controller?.state.items.length, 0, "generation-rebound tab clears its prior session before history settles");
eq(controller?.state.hydratePlaceholderItems?.length ?? 0, 0, "generation-rebound tab never exposes prior-session placeholders");
eq(controller?.state.hydrating, true, "generation-rebound tab remains in target hydration after the App mask hands off");

await act(async () => {
  generationTwoHistory.reject(new Error("generation 2 history failed"));
  await Promise.all([generationTwoHistory.promise.catch(() => undefined), generationTwoSwitch]);
  await flushPromises();
});
await waitFor("source restored after target history failure", () => controller?.activeTabId === "tab-a");
eq(backendActiveId, "tab-a", "target history failure rebinds backend focus to the retained source session");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "history A") ?? false, "target history failure restores the retained source transcript");
ok(!(controller?.state.items.some((item) => item.kind === "user" && item.text === "history O generation 1") ?? false), "target history failure never restores the prior target generation");

await act(async () => {
  await controller?.openProjectTab(reboundTabO.workspaceRoot, reboundTabO.topicId || "");
  await flushPromises();
});
await waitFor("generation two retry", () => controller?.state.items.some((item) => item.kind === "user" && item.text === "history O generation 2") ?? false);

// A mount/ready sync can start before a same-tab session rebind and resolve
// afterwards. Its tab id still matches, so the navigation generation — not the
// id — must fence the stale snapshot before it can rewrite optimistic meta.
const staleGenerationTwoTabs = currentTabs();
const staleListTabs = deferred<TabMeta[]>();
heldListTabs = staleListTabs.promise;
heldListTabsStarted = false;
let staleSync: Promise<string | undefined> | undefined;
await act(async () => {
  staleSync = controller?.syncActiveTab(false);
  await flushPromises();
});
ok(heldListTabsStarted, "backend sync is held before the newer same-tab navigation");

const reboundTabOGenerationThree = { ...tabO, sessionGeneration: 3, active: true };
tabsById.set("tab-o", reboundTabOGenerationThree);
backendActiveId = "tab-o";
const generationThreeHistory = deferred<HistoryMessage[]>();
heldTabOHistory = generationThreeHistory.promise;
let generationThreeNavigation: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  generationThreeNavigation = controller?.openProjectTab(reboundTabOGenerationThree.workspaceRoot, reboundTabOGenerationThree.topicId || "");
  await flushPromises();
});
eq(controller?.state.meta?.sessionGeneration, 3, "newer same-tab navigation installs generation three identity");
eq(controller?.state.hydrating, true, "generation three history remains pending");

await act(async () => {
  staleListTabs.resolve(staleGenerationTwoTabs);
  await flushPromises();
});
eq(controller?.state.meta?.sessionGeneration, 3, "stale same-tab sync cannot restore generation two metadata");
eq(controller?.state.hydrating, true, "stale same-tab sync cannot cancel generation three hydration");

await act(async () => {
  generationThreeHistory.resolve([userMessage("history O generation 3")]);
  await Promise.all([generationThreeHistory.promise, generationThreeNavigation, staleSync]);
  await flushPromises();
});
await waitFor("generation three history", () => controller?.state.items.some((item) => item.kind === "user" && item.text === "history O generation 3") ?? false);
eq(controller?.state.meta?.sessionGeneration, 3, "generation three remains the settled session identity");

await act(async () => { root.unmount(); });
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

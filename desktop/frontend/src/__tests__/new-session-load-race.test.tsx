// Run: tsx src/__tests__/new-session-load-race.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { initialState, reducer, useController, type Item } from "../lib/useController";
import type { AppBindings } from "../lib/bridge";
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
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function tabMeta(overrides: Partial<TabMeta> = {}): TabMeta {
  return {
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    topicId: "topic-a",
    topicTitle: "General",
    label: "model",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
    ...overrides,
  };
}

function meta(overrides: Partial<Meta> = {}): Meta {
  return {
    label: "model",
    ready: true,
    eventChannel: "agent:event",
    cwd: "/repo",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    ...overrides,
  };
}

console.log("\nnew session load race");

const resetSourceItems: Item[] = [{ kind: "user", id: "old-user", text: "old prompt" }];
const resetPlaceholderItems: Item[] = [{ kind: "user", id: "placeholder-user", text: "placeholder prompt" }];
const resetState = reducer(
  {
    ...initialState,
    items: resetSourceItems,
    hydrating: true,
    hydrateReason: "open-topic",
    hydratePlaceholderItems: resetPlaceholderItems,
  },
  { type: "reset" },
);
eq(resetState.items.length, 0, "reset clears real transcript items");
eq(resetState.hydratePlaceholderItems?.length, 1, "reset preserves hydration placeholder separately");

const emptyHistoryState = reducer(resetState, { type: "history", messages: [] });
eq(emptyHistoryState.items.length, 0, "empty history keeps the real transcript empty");
eq(emptyHistoryState.hydrateHistoryLoaded, true, "empty history marks transcript hydration loaded");
eq(emptyHistoryState.hydratePlaceholderItems?.length ?? 0, 0, "empty history clears hydration placeholder items");

const hydrateDoneState = reducer(emptyHistoryState, { type: "hydrate_done" });
eq(Boolean(hydrateDoneState.hydrateHistoryLoaded), false, "hydrate_done clears the history-loaded marker");

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

const staleHistory = deferred<HistoryMessage[]>();
let newSessionCalls = 0;
const context: ContextInfo = { used: 12, window: 100, sessionTokens: 12 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];

window.runtime = {
  EventsOn: () => () => {},
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => [tabMeta()],
      MetaForTab: async () => meta(),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async () => staleHistory.promise,
      HistoryPageForTab: async () => {
        const messages = await staleHistory.promise;
        return { messages, startTurn: 0, endTurn: messages.filter((message) => message.role === "user").length, totalTurns: messages.filter((message) => message.role === "user").length, hasOlder: false };
      },
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      NewSession: async () => {
        newSessionCalls += 1;
      },
      NewSessionForTab: async (tabID: string) => {
        if (tabID !== "tab-a") throw new Error(`unexpected new-session target ${tabID}`);
        newSessionCalls += 1;
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
await waitFor("active tab", () => controller?.activeTabId === "tab-a");

await act(async () => {
  await controller?.newSession();
  await flushPromises();
});
eq(newSessionCalls, 1, "tab-scoped NewSession is called once");
eq(controller?.state.items.length, 0, "new session clears the visible transcript");

await act(async () => {
  staleHistory.resolve([{ role: "user", content: "old prompt" }]);
  await staleHistory.promise;
  await flushPromises();
});

eq(controller?.state.items.length, 0, "stale history load cannot repopulate a new blank session");

await act(async () => {
  root.unmount();
});

const guardedStartupTabs = deferred<TabMeta[]>();
const staleProjectA = "/repo/project-a";
const targetProjectB = "/repo/project-b";
const ensureBlankSurfaceCalls: Array<{ scope: string; workspaceRoot: string }> = [];
window.go.main.App = {
  ListTabs: async () => guardedStartupTabs.promise,
  MetaForTab: async (tabID: string) => tabID === "tab-new"
    ? meta({ cwd: targetProjectB, workspaceRoot: targetProjectB, workspaceName: "project-b", workspacePath: targetProjectB })
    : meta({ cwd: staleProjectA, workspaceRoot: staleProjectA, workspaceName: "project-a", workspacePath: staleProjectA }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryForTab: async () => [],
  HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  EnsureBlankSurface: async (scope: string, workspaceRoot: string) => {
    ensureBlankSurfaceCalls.push({ scope, workspaceRoot });
    return tabMeta({
      id: "tab-new",
      topicId: "topic-new",
      topicTitle: "New session",
      workspaceRoot: targetProjectB,
      workspaceName: "project-b",
      workspacePath: targetProjectB,
      cwd: targetProjectB,
    });
  },
} as Partial<AppBindings> as AppBindings;

controller = undefined;
const guardRoot = createRoot(rootEl);

await act(async () => {
  guardRoot.render(<Probe />);
  await flushPromises();
});

await act(async () => {
  await controller?.ensureBlankSurface("project", targetProjectB);
  await flushPromises();
});

eq(ensureBlankSurfaceCalls.length, 1, "EnsureBlankSurface is called once");
eq(ensureBlankSurfaceCalls[0]?.workspaceRoot, targetProjectB, "EnsureBlankSurface keeps the requested project root");
eq(controller?.activeTabId, "tab-new", "blank surface becomes active before startup sync resolves");
eq(controller?.state.meta?.workspaceRoot, targetProjectB, "blank surface exposes the new project root");

await act(async () => {
  guardedStartupTabs.resolve([tabMeta({
    id: "tab-old",
    topicId: "topic-old",
    topicTitle: "Old session",
    workspaceRoot: staleProjectA,
    workspaceName: "project-a",
    workspacePath: staleProjectA,
    cwd: staleProjectA,
  })]);
  await guardedStartupTabs.promise;
  await flushPromises();
});

eq(controller?.activeTabId, "tab-new", "guarded startup sync cannot restore an older active tab");
eq(controller?.state.meta?.workspaceRoot, targetProjectB, "guarded startup sync cannot restore the old project root");

await act(async () => {
  guardRoot.unmount();
});

const remoteOld = tabMeta({
  id: "remote-old",
  targetKind: "remote",
  workspaceRoot: "workspace-opaque",
  workspaceName: "Remote workspace",
  workspacePath: "/host/workspace",
  cwd: "/host/workspace",
  topicId: "topic-opaque",
  topicTitle: "Remote topic",
  sessionPath: "session-token-old",
});
const remoteReplacement = tabMeta({
  ...remoteOld,
  id: "remote-new",
  sessionPath: "session-token-new",
});
const remoteCleared = tabMeta({
  ...remoteOld,
  id: "remote-cleared",
  sessionPath: "session-token-cleared",
});
const remoteBackground = tabMeta({
  ...remoteOld,
  id: "remote-background",
  topicId: "topic-background",
  topicTitle: "Background",
  sessionPath: "session-token-background",
  active: false,
});
let remoteTabs = [remoteOld];
const remoteReadyHandlers: Array<(tabId?: string) => void> = [];
const remoteRebuiltHandlers: Array<(tabId?: string) => void> = [];
const remoteNewGate = deferred<void>();
const remoteClearGate = deferred<void>();
const remoteNewTargets: string[] = [];
const remoteClearTargets: string[] = [];

window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "agent:ready") remoteReadyHandlers.push(cb as (tabId?: string) => void);
    if (name === "runtime:rebuilt") remoteRebuiltHandlers.push(cb as (tabId?: string) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go.main.App = {
  ListTabs: async () => remoteTabs,
  MetaForTab: async (tabID: string) => meta({
    cwd: "/host/workspace",
    workspaceRoot: "workspace-opaque",
    workspaceName: "Remote workspace",
    workspacePath: "/host/workspace",
    sessionPath: remoteTabs.find((tab) => tab.id === tabID)?.sessionPath,
  }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryForTab: async () => [],
  HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  NewSessionForTab: async (tabID: string) => {
    remoteNewTargets.push(tabID);
    await remoteNewGate.promise;
  },
  ClearSessionForTab: async (tabID: string) => {
    remoteClearTargets.push(tabID);
    await remoteClearGate.promise;
  },
} as Partial<AppBindings> as AppBindings;

controller = undefined;
const remoteRoot = createRoot(rootEl);
await act(async () => {
  remoteRoot.render(<Probe />);
  await flushPromises();
});
await waitFor("Remote old active tab", () => controller?.activeTabId === remoteOld.id);

remoteTabs = [{ ...remoteOld, active: true }, remoteBackground];
await act(async () => {
  for (const handler of remoteReadyHandlers) handler(remoteBackground.id);
  for (const handler of remoteRebuiltHandlers) handler(remoteBackground.id);
  await flushPromises();
});
eq(controller?.activeTabId, remoteOld.id, "background ready/rebuilt cannot steal the active Remote tab while it remains authoritative");

let remoteNewPromise: Promise<void> | undefined;
await act(async () => {
  remoteNewPromise = controller?.newSession();
  await flushPromises();
});
await waitFor("Remote NewSession RPC", () => remoteNewTargets.length === 1);
eq(remoteNewTargets[0], remoteOld.id, "Remote NewSession targets the pre-replacement tab");

remoteTabs = [{ ...remoteReplacement, active: true }, remoteBackground];
await act(async () => {
  for (const handler of remoteReadyHandlers) handler(remoteReplacement.id);
  await flushPromises();
});
await waitFor("Remote replacement adoption", () => controller?.activeTabId === remoteReplacement.id);
eq(controller?.state.meta?.sessionPath, remoteReplacement.sessionPath, "ready event adopts the authoritative Remote replacement Session");

await act(async () => {
  remoteNewGate.resolve();
  await remoteNewPromise;
  await flushPromises();
});
eq(controller?.activeTabId, remoteReplacement.id, "late NewSession RPC continuation cannot recreate the retired tab");
eq(controller?.state.meta?.sessionPath, remoteReplacement.sessionPath, "late NewSession RPC continuation keeps replacement metadata");

let remoteClearPromise: Promise<void> | undefined;
await act(async () => {
  remoteClearPromise = controller?.clearSession();
  await flushPromises();
});
await waitFor("Remote ClearSession RPC", () => remoteClearTargets.length === 1);
eq(remoteClearTargets[0], remoteReplacement.id, "Remote ClearSession targets the current replacement tab");

await act(async () => {
  remoteClearGate.resolve();
  await remoteClearPromise;
  await flushPromises();
});
eq(controller?.activeTabId, remoteReplacement.id, "ClearSession RPC completion keeps the current tab until SnapshotUpdate arrives");

remoteTabs = [{ ...remoteCleared, active: true }, remoteBackground];
await act(async () => {
  for (const handler of remoteRebuiltHandlers) handler(remoteCleared.id);
  await flushPromises();
});
await waitFor("Remote clear replacement adoption", () => controller?.activeTabId === remoteCleared.id);
eq(controller?.state.meta?.sessionPath, remoteCleared.sessionPath, "rebuilt event adopts the authoritative Remote clear replacement");
eq(controller?.state.items.length, 0, "Remote replacement hydrate exposes the new blank Session");

await act(async () => {
  remoteRoot.unmount();
});

const localA = tabMeta({ id: "local-a", targetKind: "local", sessionPath: "/sessions/local-a.jsonl" });
const localB = tabMeta({
  id: "local-b",
  targetKind: "local",
  topicId: "topic-b",
  topicTitle: "Local B",
  sessionPath: "/sessions/local-b.jsonl",
  active: false,
});
let localBackendActive = localA.id;
let localANew = false;
const localNewGate = deferred<void>();
const localNewTargets: string[] = [];
const localHistoryCalls: string[] = [];
const currentLocalTabs = () => [localA, localB].map((tab) => ({ ...tab, active: tab.id === localBackendActive }));

window.runtime = {
  EventsOn: () => () => {},
  BrowserOpenURL: () => {},
};
window.go.main.App = {
  ListTabs: async () => currentLocalTabs(),
  MetaForTab: async (tabID: string) => meta({ sessionPath: tabID === localA.id ? localA.sessionPath : localB.sessionPath }),
  ContextUsageForTab: async () => context,
  EffortForTab: async () => effort,
  BalanceForTab: async () => balance,
  JobsForTab: async () => jobs,
  CheckpointsForTab: async () => checkpoints,
  HistoryForTab: async (tabID: string) => {
    localHistoryCalls.push(tabID);
    if (tabID === localA.id) return localANew ? [] : [{ role: "user", content: "Local A old" }];
    return [{ role: "user", content: "Local B" }];
  },
  HistoryPageForTab: async (tabID: string) => {
    const messages = await window.go.main.App.HistoryForTab(tabID);
    return { messages, startTurn: 0, endTurn: messages.length, totalTurns: messages.length, hasOlder: false };
  },
  HistoryCheckpointTurnsForTab: async () => [],
  ReplayPendingPrompts: async () => {},
  SetActiveTab: async (tabID: string) => {
    localBackendActive = tabID;
  },
  NewSessionForTab: async (tabID: string) => {
    localNewTargets.push(tabID);
    await localNewGate.promise;
    localANew = true;
  },
} as Partial<AppBindings> as AppBindings;

controller = undefined;
const localRoot = createRoot(rootEl);
await act(async () => {
  localRoot.render(<Probe />);
  await flushPromises();
});
await waitFor("Local A initial history", () => controller?.activeTabId === localA.id && controller.state.items.length === 1);

let localNewPromise: Promise<void> | undefined;
await act(async () => {
  localNewPromise = controller?.newSession();
  await flushPromises();
});
await waitFor("Local NewSession RPC", () => localNewTargets.length === 1);
eq(localNewTargets[0], localA.id, "Local NewSession keeps its explicit source tab");

await act(async () => {
  await controller?.switchTab(localB.id, localB);
  await flushPromises();
});
await waitFor("Local B active", () => controller?.activeTabId === localB.id && controller.state.items.some((item) => item.kind === "user" && item.text === "Local B"));

await act(async () => {
  localNewGate.resolve();
  await localNewPromise;
  await flushPromises();
});
eq(controller?.activeTabId, localB.id, "Local NewSession completion does not steal focus from tab B");

await act(async () => {
  await controller?.switchTab(localA.id, localA);
  await flushPromises();
});
await waitFor("Local A blank completion", () => controller?.activeTabId === localA.id && !controller.state.hydrating);
eq(controller?.state.items.length, 0, "Local NewSession completes and clears tab A while focus was on tab B");
ok(localHistoryCalls.filter((tabID) => tabID === localA.id).length >= 1, "Local A session state remains loadable after tab-scoped completion");

await act(async () => {
  localRoot.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

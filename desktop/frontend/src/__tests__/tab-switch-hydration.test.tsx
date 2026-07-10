// Run: tsx src/__tests__/tab-switch-hydration.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { useController } from "../lib/useController";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, JobView, Meta, TabMeta, WireEvent } from "../lib/types";

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
    sessionPath: `${workspaceRoot}/sessions/${id}.jsonl`,
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
const tabD = tabMeta("tab-d");
const tabE = tabMeta("tab-e");
const tabF = tabMeta("tab-f");
const tabG = tabMeta("tab-g");
const tabH = tabMeta("tab-h");
const tabI = tabMeta("tab-i", { running: true, pendingPrompt: true, cancellable: true });
const tabJ = tabMeta("tab-j");
let backendActiveId = "tab-a";
const historyB = deferred<HistoryMessage[]>();
const historyD = deferred<HistoryMessage[]>();
let metaH = deferred<Meta>();
let historyH = deferred<HistoryMessage[]>();
const contextDGate = deferred<ContextInfo>();
const setActiveBGate = deferred<void>();
const setActiveEGate = deferred<void>();
const setActiveFGate = deferred<void>();
const submitTabCGate = deferred<void>();
const forkResultGate = deferred<void>();
const historyCalls: string[] = [];
const cancelCalls: string[] = [];
let contextDCalls = 0;
let metaHCalls = 0;
let holdNextContextForD = false;
let holdNextMetaForH = false;
let holdNextHistoryForH = false;
let setActiveCalls = 0;
let newSessionCalls = 0;
const newSessionTargets: string[] = [];
let replayPendingPromptCalls = 0;
let failSetActiveFor = "";
let holdNextForkResult = false;
let forkStarted = false;
const runningTabs = new Set<string>();
const tabsById = new Map([tabA, tabB, tabC, tabD, tabE, tabF, tabG, tabH, tabI].map((tab) => [tab.id, tab]));
const eventHandlers: Array<(e: WireEvent) => void> = [];
const readyHandlers: Array<(tabId?: string) => void> = [];

function currentTabs(): TabMeta[] {
  return Array.from(tabsById.values()).map((tab) => {
    const running = runningTabs.has(tab.id);
    return { ...tab, active: tab.id === backendActiveId, running, cancellable: running };
  });
}

window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "agent:event") eventHandlers.push(cb as (e: WireEvent) => void);
    if (name === "agent:ready") readyHandlers.push(cb as (tabId?: string) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => currentTabs(),
      MetaForTab: async (tabID: string) => {
        if (tabID === "tab-h" && holdNextMetaForH) {
          metaHCalls += 1;
          holdNextMetaForH = false;
          return metaH.promise;
        }
        return metaFor(tabsById.get(tabID) ?? tabA);
      },
      ContextUsageForTab: async (tabID: string) => {
        if (tabID === "tab-d" && holdNextContextForD) {
          contextDCalls += 1;
          holdNextContextForD = false;
          return contextDGate.promise;
        }
        if (tabID === "tab-d") contextDCalls += 1;
        return context;
      },
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (tabID: string) => {
        historyCalls.push(tabID);
        if (tabID === "tab-b") return historyB.promise;
        if (tabID === "tab-d") return historyD.promise;
        if (tabID === "tab-e") return [userMessage("fork E")];
        if (tabID === "tab-g") return [userMessage("history G")];
        if (tabID === "tab-h" && holdNextHistoryForH) {
          holdNextHistoryForH = false;
          return historyH.promise;
        }
        if (tabID === "tab-h") return [userMessage("history H")];
        if (tabID === "tab-i") return [userMessage("fork I")];
        if (tabID === "tab-j") return [userMessage("fork J")];
        return [userMessage("cached A")];
      },
      HistoryPageForTab: async (tabID: string) => {
        const messages = await window.go.main.App.HistoryForTab(tabID);
        return { messages, startTurn: 0, endTurn: messages.filter((message) => message.role === "user").length, totalTurns: messages.filter((message) => message.role === "user").length, hasOlder: false };
      },
      HistoryCheckpointTurnsForTab: async () => [],
      OpenProjectTab: async (workspaceRoot: string, topicId: string) => {
        const target = Array.from(tabsById.values()).find((tab) => tab.workspaceRoot === workspaceRoot && tab.topicId === topicId) ?? tabD;
        backendActiveId = target.id;
        return { ...target, active: true };
      },
      ActivateTopic: async (_scope: string, workspaceRoot: string, topicId: string) => {
        const target = Array.from(tabsById.values()).find((tab) => tab.workspaceRoot === workspaceRoot && tab.topicId === topicId) ?? tabG;
        backendActiveId = target.id;
        return { ...target, active: true };
      },
      NewSession: async () => {
        newSessionCalls += 1;
      },
      NewSessionForTab: async (tabID: string) => {
        newSessionCalls += 1;
        newSessionTargets.push(tabID);
      },
      Fork: async () => {
        tabsById.set("tab-e", tabE);
        backendActiveId = "tab-e";
        runningTabs.add("tab-e");
        return { ...tabE, active: true, running: true };
      },
      ForkForTab: async () => {
        const fork = holdNextForkResult ? tabJ : tabE;
        tabsById.set(fork.id, fork);
        backendActiveId = fork.id;
        runningTabs.add(fork.id);
        if (holdNextForkResult) {
          holdNextForkResult = false;
          forkStarted = true;
          await forkResultGate.promise;
        }
        return { ...fork, active: true, running: true };
      },
      ReplayPendingPrompts: async () => {
        replayPendingPromptCalls += 1;
        const active = tabsById.get(backendActiveId);
        if (!active?.pendingPrompt) return;
        for (const handler of eventHandlers) {
          handler({
            kind: "approval_request",
            tabId: backendActiveId,
            approval: { id: `pending-${backendActiveId}`, tool: "bash", subject: `pending ${backendActiveId}` },
          });
        }
      },
      SetActiveTab: async (tabID: string) => {
        setActiveCalls += 1;
        if (tabID === "tab-b") await setActiveBGate.promise;
        if (tabID === "tab-e") await setActiveEGate.promise;
        if (tabID === "tab-f") await setActiveFGate.promise;
        if (tabID === failSetActiveFor) throw new Error("persist failed");
        backendActiveId = tabID;
      },
      CancelTab: async (tabID: string) => {
        cancelCalls.push(tabID);
        runningTabs.delete(tabID);
      },
      SubmitToTab: async (tabID: string) => {
        runningTabs.add(tabID);
        if (tabID === "tab-c") await submitTabCGate.promise;
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

await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "approval_request", tabId: "tab-b", approval: { id: "stale-tab-b", tool: "bash", subject: "stale tab B" } });
  }
  await flushPromises();
});

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
eq(controller?.state.approval?.id, undefined, "tab activation clears a stale approval already stored on the target tab");
eq(controller?.state.running, false, "tab activation clears the stale prompt lifecycle before backend status arrives");

await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "approval_request", approval: { id: "old-backend-approval", tool: "bash", subject: "old backend approval" } });
  }
  await flushPromises();
});
eq(controller?.state.approval?.id, undefined, "tab-less events stay with the confirmed backend tab during optimistic activation");
eq(controller?.state.running, false, "tab-less old-backend prompts cannot lock the optimistic target tab");

let newSessionWhileSwitching: Promise<void> | undefined;
await act(async () => {
  newSessionWhileSwitching = controller?.newSession();
  await flushPromises();
});
eq(newSessionCalls, 1, "newSession can target the selected tab before backend focus activation settles");
eq(newSessionTargets.join(","), "tab-b", "newSession keeps the selected tab as its explicit target");

await act(async () => {
  setActiveBGate.resolve();
  await switchToB;
  await newSessionWhileSwitching;
  await flushPromises();
});
eq(newSessionCalls, 1, "backend focus completion does not duplicate the scoped new-session action");
await waitFor("tab-b history request", () => historyCalls.includes("tab-b"));

const historyCallsBeforeReturnToA = historyCalls.length;
await act(async () => {
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await waitFor("tab-a restored", () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"));
eq(historyCalls.length, historyCallsBeforeReturnToA, "cached idle tab skips history hydration when reselected");

await act(async () => {
  historyB.resolve([userMessage("late B")]);
  await historyB.promise;
  await flushPromises();
});

eq(controller?.activeTabId, "tab-a", "late history for another tab does not change the active tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "cached A") ?? false, "late history for another tab does not overwrite the active transcript");
ok(!(controller?.state.items.some((item) => item.kind === "user" && item.text === "late B") ?? false), "late history stays scoped to its tab state");

const historyCallsBeforeFallbackSync = historyCalls.length;
await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "approval_request", tabId: "tab-b", approval: { id: "stale-fallback-approval", tool: "bash", subject: "stale fallback approval" } });
  await flushPromises();
});
backendActiveId = "tab-b";
await act(async () => {
  await controller?.syncActiveTab(false);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-b", "backend fallback sync activates the backend-selected cached tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "late B") ?? false, "backend fallback sync keeps the cached transcript");
eq(historyCalls.length, historyCallsBeforeFallbackSync, "backend fallback sync preserves cached history instead of reloading it");
eq(controller?.state.approval?.id, undefined, "backend fallback sync reconciles stale approval state");
eq(controller?.state.running, false, "backend fallback sync reconciles stale running state");
await act(async () => {
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await waitFor("tab-a restored after fallback sync", () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"));

runningTabs.add("tab-e");
let switchToE: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  switchToE = controller?.switchTab("tab-e", { ...tabE, running: true, cancellable: true });
  await flushPromises();
});
eq(controller?.activeTabId, "tab-e", "switching to a backend-running tab updates the active tab immediately");
eq(controller?.state.running, true, "backend-running tab restores the stop state before backend activation settles");
eq(controller?.state.cancellable, true, "backend-running tab remains cancellable before backend activation settles");
await act(async () => {
  controller?.cancel();
  await flushPromises();
});
eq(cancelCalls.join(","), "tab-e", "cancel targets the backend-running tab while activation is pending");
await act(async () => {
  setActiveEGate.resolve();
  await switchToE;
  await flushPromises();
});
eq(controller?.state.running, false, "cancelled backend-running tab reconciles to idle after activation");
await act(async () => {
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await waitFor("tab-a restored after backend-running switch", () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"));

runningTabs.add("tab-i");
const replayCallsBeforePendingSwitch = replayPendingPromptCalls;
await act(async () => {
  await controller?.switchTab("tab-i", tabI);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-i", "switching to a prompt-blocked tab activates the requested tab");
ok(replayPendingPromptCalls > replayCallsBeforePendingSwitch, "pending backend prompts are replayed after tab activation");
eq(controller?.state.approval?.id, "pending-tab-i", "a genuine pending approval survives the later hydration start");
eq(controller?.state.running, true, "a genuine pending approval keeps the target tab running");
tabsById.set("tab-i", { ...tabI, pendingPrompt: false, running: false, cancellable: false });
runningTabs.delete("tab-i");
await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_done", tabId: "tab-i" });
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await waitFor("tab-a restored after pending-prompt switch", () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"));

let switchToF: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  switchToF = controller?.switchTab("tab-f", tabF);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-f", "first rapid switch activates the slow target optimistically");
let switchToG: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  switchToG = controller?.switchTab("tab-g", tabG);
  await switchToG;
  await flushPromises();
});
eq(controller?.activeTabId, "tab-g", "second rapid switch wins immediately");
eq(backendActiveId, "tab-g", "second rapid switch activates the backend");
await act(async () => {
  setActiveFGate.resolve();
  await switchToF;
  await flushPromises();
});
eq(controller?.activeTabId, "tab-g", "late completion from the first rapid switch does not replace the visible tab");
eq(backendActiveId, "tab-g", "late completion from the first rapid switch reasserts the last-clicked backend tab");
ok(!historyCalls.includes("tab-f"), "late completion from the first rapid switch does not hydrate the stale target");

await act(async () => {
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await waitFor("tab-a restored after rapid switch", () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"));

failSetActiveFor = "tab-b";
const historyCallsBeforeFailedSwitch = historyCalls.length;
await act(async () => {
  await controller?.switchTab("tab-b", tabB);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-a", "failed backend tab switch reverts to the previous active tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "cached A") ?? false, "failed backend tab switch keeps the previous transcript visible");
eq(historyCalls.length, historyCallsBeforeFailedSwitch, "failed backend tab switch does not hydrate the rejected target");
failSetActiveFor = "";

await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "phase", text: "Planner is thinking", tabId: "tab-a" });
  for (const handler of eventHandlers) handler({ kind: "message", text: "Planner kept", reasoning: "Planner notes", tabId: "tab-a" });
  await flushPromises();
});
await waitFor("cached planner transcript", () =>
  controller?.state.items.some((item) => item.kind === "assistant" && item.text === "Planner kept" && item.reasoning === "Planner notes") ?? false
);
const historyCallsBeforeReady = historyCalls.length;
await act(async () => {
  for (const handler of readyHandlers) handler();
  await flushPromises();
});
await waitFor("ready hydration settled", () => controller?.state.hydrating === false);
eq(historyCalls.length, historyCallsBeforeReady, "agent ready with cached transcript skips executor-only history hydration");
ok(controller?.state.items.some((item) => item.kind === "phase" && item.text === "Planner is thinking") ?? false, "agent ready keeps cached planner phase");
ok(controller?.state.items.some((item) => item.kind === "assistant" && item.text === "Planner kept" && item.reasoning === "Planner notes") ?? false, "agent ready keeps cached planner answer");

let tabCSendResolved = false;
await act(async () => {
  const sendPromise = controller?.sendToTab("tab-c", "streaming C");
  sendPromise?.then(() => {
    tabCSendResolved = true;
  });
  await flushPromises();
});
eq(tabCSendResolved, true, "sendToTab resolves after optimistic dispatch before backend submit completes");
await act(async () => {
  await controller?.switchTab("tab-c", tabC);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-c", "switching to a cached running tab still updates the active tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "streaming C") ?? false, "cached running tab keeps its optimistic transcript");
ok(!historyCalls.includes("tab-c"), "cached running tab skips history hydration");
await act(async () => {
  submitTabCGate.resolve();
  await submitTabCGate.promise;
  await flushPromises();
});

holdNextContextForD = true;
await act(async () => {
  await controller?.openProjectTab(tabD.workspaceRoot, tabD.topicId || "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-d", "openProjectTab activates the opened tab");
eq(controller?.state.items.length, 0, "open topic keeps the new tab transcript empty while hydrating");
ok(controller?.state.hydratePlaceholderItems?.some((item) => item.kind === "user" && item.text === "streaming C") ?? false, "open topic stores previous transcript only as a hydration placeholder");

await act(async () => {
  historyD.resolve([userMessage("history D")]);
  await historyD.promise;
  await flushPromises();
});
eq(controller?.state.hydrating, false, "topic history clears visible hydration before ancillary phase 2 settles");
await waitFor("open topic phase 2 started", () => contextDCalls === 1);
const contextCallsBeforeReadyD = contextDCalls;
const historyCallsBeforeReadyD = historyCalls.length;
await act(async () => {
  for (const handler of readyHandlers) {
    handler("tab-b");
    handler("tab-d");
    handler();
  }
  await flushPromises();
});
eq(contextDCalls, contextCallsBeforeReadyD, "agent ready reuses in-flight open-topic hydration for the active tab");
eq(historyCalls.length, historyCallsBeforeReadyD, "background ready events do not hydrate the active tab");
await act(async () => {
  contextDGate.resolve(context);
  await contextDGate.promise;
  await flushPromises();
});
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "history D") ?? false, "topic history replaces the hydration placeholder");
eq(controller?.state.hydratePlaceholderItems?.length ?? 0, 0, "topic history clears the hydration placeholder");

const historyCallsBeforeReopenD = historyCalls.length;
await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "approval_request", tabId: "tab-d", approval: { id: "stale-approval", tool: "bash", subject: "stale approval" } });
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await act(async () => {
  await controller?.openProjectTab(tabD.workspaceRoot, tabD.topicId || "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-d", "reopening an already hydrated topic keeps it active");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "history D") ?? false, "reopened cached topic keeps its transcript");
eq(historyCalls.length, historyCallsBeforeReopenD, "reopening an already hydrated topic skips history hydration");
eq(controller?.state.approval?.id, undefined, "reopening a topic reconciles stale approval state");
eq(controller?.state.running, false, "reopening a topic reconciles stale running state");

await act(async () => {
  await controller?.rewind(0, "fork");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-e", "fork activates the forked tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "fork E") ?? false, "fork loads the forked transcript");
eq(controller?.state.running, true, "fork reconciles backend running state after reset hydration");
runningTabs.delete("tab-e");

const contextCallsBeforeInactiveD = contextDCalls;
await act(async () => {
  await controller?.openProjectTab(tabD.workspaceRoot, tabD.topicId || "");
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
await act(async () => {
  await flushPromises();
  await flushPromises();
});
eq(contextDCalls, contextCallsBeforeInactiveD, "inactive topic skips ancillary hydration after quick tab switch");

holdNextForkResult = true;
let delayedFork: Promise<boolean> | undefined;
await act(async () => {
  delayedFork = controller?.rewind(0, "fork");
  await flushPromises();
});
await waitFor("delayed fork result", () => forkStarted && backendActiveId === "tab-j");
await act(async () => {
  await controller?.switchTab("tab-d", tabD);
  await controller?.switchTab("tab-a", tabA);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-a", "later A→D→A navigation returns to the source tab before fork completion");
eq(backendActiveId, "tab-a", "later A→D→A navigation owns backend focus before fork completion");
await act(async () => {
  forkResultGate.resolve();
  await delayedFork;
  await flushPromises();
});
eq(controller?.activeTabId, "tab-a", "late fork completion does not override newer ABA navigation");
eq(backendActiveId, "tab-a", "late fork completion reasserts the latest backend tab");
ok(!historyCalls.includes("tab-j"), "stale fork result is not hydrated as the visible tab");
runningTabs.delete("tab-j");

tabsById.set("tab-d", { ...tabD, sessionPath: `${tabD.workspaceRoot}/sessions/next-tab-d.jsonl` });
const historyCallsBeforeReboundD = historyCalls.length;
await act(async () => {
  await controller?.openProjectTab(tabD.workspaceRoot, tabD.topicId || "");
  await flushPromises();
});
eq(historyCalls.length, historyCallsBeforeReboundD + 1, "rebound topic reloads history when session path changes");

metaH = deferred<Meta>();
holdNextMetaForH = true;
const historyCallsBeforeSlowMeta = historyCalls.length;
await act(async () => {
  await controller?.openProjectTab(tabH.workspaceRoot, tabH.topicId || "");
  await flushPromises();
});
await waitFor("slow meta tab hydrates history", () =>
  controller?.activeTabId === "tab-h" &&
  controller.state.hydrating === false &&
  (controller.state.items.some((item) => item.kind === "user" && item.text === "history H") ?? false) &&
  metaHCalls === 1
);
eq(historyCalls.length, historyCallsBeforeSlowMeta + 1, "slow MetaForTab does not delay the history request");
eq(controller?.state.meta?.label, "model-tab-h", "slow MetaForTab leaves optimistic metadata visible while history hydrates");
await act(async () => {
  metaH.resolve({ ...metaFor(tabH), label: "fresh-model-tab-h" });
  await metaH.promise;
  await flushPromises();
});
await waitFor("slow meta refresh applies", () => controller?.state.meta?.label === "fresh-model-tab-h");

metaH = deferred<Meta>();
const metaHCallsBeforeStale = metaHCalls;
holdNextMetaForH = true;
await act(async () => {
  await controller?.openProjectTab(tabH.workspaceRoot, tabH.topicId || "");
  await flushPromises();
});
await waitFor("slow meta pending before single-surface navigation", () => metaHCalls === metaHCallsBeforeStale + 1);
await act(async () => {
  await controller?.activateTopic("project", tabG.workspaceRoot, tabG.topicId || "");
  await flushPromises();
});
await waitFor("single-surface activation replaces visible tab", () =>
  controller?.activeTabId === "tab-g" &&
  (controller.state.items.some((item) => item.kind === "user" && item.text === "history G") ?? false)
);
await act(async () => {
  metaH.resolve({ ...metaFor(tabH), label: "stale-model-tab-h" });
  await metaH.promise;
  await flushPromises();
});
eq(controller?.activeTabId, "tab-g", "late meta from a replaced tab does not switch the visible tab");
ok(controller?.state.meta?.label !== "stale-model-tab-h", "late meta from a replaced tab does not overwrite visible metadata");
historyH = deferred<HistoryMessage[]>();
holdNextHistoryForH = true;
await act(async () => {
  await controller?.openProjectTab(tabH.workspaceRoot, tabH.topicId || "");
  await flushPromises();
});
await waitFor("reopened tab-h treats stale late meta as discarded", () =>
  controller?.activeTabId === "tab-h" &&
  controller.state.hydrating === true &&
  (controller.state.hydratePlaceholderItems?.some((item) => item.kind === "user" && item.text === "history G") ?? false)
);
await act(async () => {
  historyH.resolve([userMessage("history H after stale meta")]);
  await historyH.promise;
  await flushPromises();
});
await waitFor("reopened tab-h finishes after stale meta discard", () =>
  controller?.state.hydrating === false &&
  (controller.state.items.some((item) => item.kind === "user" && item.text === "history H after stale meta") ?? false)
);

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

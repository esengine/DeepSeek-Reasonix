// Run: tsx src/__tests__/activate-topic-stale.test.tsx
//
// Locks in last-click-wins for single-surface topic activation (#6607): when
// a newer navigation starts while app.StartTopicActivation is still in
// flight, the stale completion must neither flip the visible tab away from
// the user's last click nor delete the newer surface's cached state (the
// single-surface prune removes every other tab state, blanking the visible
// transcript).

import { readFileSync } from "node:fs";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { enqueueNavigationRequest, type NavigationCoalescingRefs } from "../lib/openTopicCoalescing";
import { useController } from "../lib/useController";
import { historySliceFromMessages } from "./mockHistorySlice";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, HistorySliceRequest, JobView, Meta, TabMeta, TopicActivationEvent, TopicActivationRequest, WireEvent } from "../lib/types";

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
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
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

console.log("\nactivate topic stale completion");

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
const tabX = tabMeta("tab-x");
const tabY = tabMeta("tab-y");
let backendActiveId = "tab-a";
// Per-tab holds so any activation can be stalled mid-flight and released.
const activationHolds = new Map<string, Promise<void>>();
const tabsById = new Map([tabA, tabX, tabY].map((tab) => [tab.id, tab]));
const topicActivationHandlers: Array<(e: TopicActivationEvent) => void> = [];
const eventHandlers: Array<(e: WireEvent) => void> = [];
const replayTargets: string[] = [];
// The pending ticketed activation backend-side: a newer StartTopicActivation
// supersedes it (cancelled), exactly like the real generation protocol.
let mockPendingActivation: { requestId: string; tabId: string } | undefined;

function emitTopicActivation(event: TopicActivationEvent): void {
  for (const handler of topicActivationHandlers) handler(event);
}

function currentTabs(): TabMeta[] {
  return Array.from(tabsById.values()).map((tab) => ({ ...tab, active: tab.id === backendActiveId }));
}

window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "agent:event") eventHandlers.push(cb as (e: WireEvent) => void);
    if (name === "topic:activation") topicActivationHandlers.push(cb as (e: TopicActivationEvent) => void);
    return () => {};
  },
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
        if (tabID === "tab-x") return [userMessage("history X")];
        if (tabID === "tab-y") return [userMessage("history Y")];
        return [userMessage("history A")];
      },
      HistoryPageForTab: async (tabID: string) => {
        const messages = await window.go.main.App.HistoryForTab(tabID);
        return { messages, startTurn: 0, endTurn: messages.length, totalTurns: messages.length, hasOlder: false };
      },
      HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) =>
        historySliceFromMessages(tabID, await window.go.main.App.HistoryForTab(tabID), req),
      HistoryCheckpointTurnsForTab: async () => [],
      ActivateTopic: async (_scope: string, workspaceRoot: string, topicId: string) => {
        const target = Array.from(tabsById.values()).find((tab) => tab.workspaceRoot === workspaceRoot && tab.topicId === topicId) ?? tabA;
        const hold = activationHolds.get(target.id);
        if (hold) await hold;
        backendActiveId = target.id;
        return { ...target, active: true };
      },
      StartTopicActivation: async (req: TopicActivationRequest) => {
        const target = Array.from(tabsById.values()).find((tab) => tab.workspaceRoot === req.workspaceRoot && tab.topicId === req.topicId) ?? tabA;
        const hold = activationHolds.get(target.id);
        if (hold) await hold;
        backendActiveId = target.id;
        const requestId = req.requestId || `mock-activation-${target.id}`;
        const previous = mockPendingActivation;
        mockPendingActivation = { requestId, tabId: target.id };
        if (previous && previous.requestId !== requestId) {
          emitTopicActivation({ requestId: previous.requestId, tabId: previous.tabId, phase: "cancelled" });
        }
        emitTopicActivation({ requestId, tabId: target.id, phase: "starting" });
        window.setTimeout(() => {
          if (mockPendingActivation?.requestId !== requestId) return;
          mockPendingActivation = undefined;
          emitTopicActivation({ requestId, tabId: target.id, phase: "ready" });
        }, 0);
        return { requestId, tabId: target.id, meta: { ...target, active: true } };
      },
      SetActiveTab: async (tabID: string) => {
        const hold = activationHolds.get(tabID);
        if (hold) await hold;
        backendActiveId = tabID;
      },
      ReplayPendingPrompts: async () => {},
      ReplayPendingPromptsForTab: async (tabID: string) => {
        replayTargets.push(tabID);
        if (!tabsById.get(tabID)?.pendingPrompt) return;
        for (const handler of eventHandlers) {
          handler({
            kind: "ask_request",
            tabId: tabID,
            ask: { id: `pending-${tabID}`, questions: [{ id: "choice", prompt: "Keep me through A-X-A", options: [] }] },
          });
        }
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
await waitFor("initial active tab", () => controller?.activeTabId === "tab-a");

// Click topic X: the backend call hangs (slow prune / disk).
const activateXGate = deferred<void>();
activationHolds.set("tab-x", activateXGate.promise);
let activateX: Promise<TabMeta> | undefined;
await act(async () => {
  activateX = controller?.activateTopic("project", tabX.workspaceRoot, tabX.topicId ?? "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-a", "held activation does not flip the tab early");

// The user clicks topic Y before X's backend call returns; Y resolves first.
await act(async () => {
  await controller?.activateTopic("project", tabY.workspaceRoot, tabY.topicId ?? "");
  await flushPromises();
});
await waitFor("Y is active with its history", () =>
  controller?.activeTabId === "tab-y" && controller.state.items.some((item) => item.kind === "user" && item.text === "history Y"));

// X's stale completion lands after Y applied. Last click must win.
await act(async () => {
  activateXGate.resolve();
  activationHolds.delete("tab-x");
  await activateX;
  await flushPromises();
});
await act(async () => {
  await flushPromises();
});
eq(controller?.activeTabId, "tab-y", "stale activation must not flip the visible tab");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "history Y") === true,
  "stale activation must not delete the visible tab's cached state");

// A fresh activation afterwards still applies normally (guard is not sticky).
await act(async () => {
  await controller?.activateTopic("project", tabX.workspaceRoot, tabX.topicId ?? "");
  await flushPromises();
});
await waitFor("X activates cleanly on a fresh click", () => controller?.activeTabId === "tab-x");

// --- Through the REAL production navigation queue (#6613 review P1) ---
//
// App.enqueueNavigation serializes clicks: a click made while another request
// runs only becomes a pending queue entry — it does NOT run activateTopic, so
// the controller epoch does not advance by itself. The App wiring must bump
// the epoch at ENQUEUE time (noteNavigationIntent), otherwise the running
// stale activation passes the guard, flips the tab, and prunes cached state.
type NavInput = { workspaceRoot: string; topicId: string };
const navRefs: NavigationCoalescingRefs<NavInput> = {
  seqRef: { current: 0 },
  runningRef: { current: false },
  pendingRef: { current: null },
};
const enqueueNav = (workspaceRoot: string, topicId: string): Promise<void> => {
  controller?.noteNavigationIntent(); // the App.tsx wiring under test
  return enqueueNavigationRequest(navRefs, { workspaceRoot, topicId }, async (request) => {
    await controller?.activateTopic("project", request.workspaceRoot, request.topicId);
  });
};

const gateY = deferred<void>();
activationHolds.set("tab-y", gateY.promise);
const gateA = deferred<void>();
activationHolds.set("tab-a", gateA.promise);

let queuedFirst: Promise<void> | undefined;
let queuedSecond: Promise<void> | undefined;
await act(async () => {
  queuedFirst = enqueueNav(tabY.workspaceRoot, tabY.topicId ?? ""); // runs, held mid-flight
  await flushPromises();
});
await act(async () => {
  queuedSecond = enqueueNav(tabA.workspaceRoot, tabA.topicId ?? ""); // queued, does not run yet
  await flushPromises();
});

// The first (now stale) activation resolves while the second is still queued.
await act(async () => {
  gateY.resolve();
  activationHolds.delete("tab-y");
  await queuedFirst;
  await flushPromises();
});
eq(controller?.activeTabId, "tab-x", "queued click invalidates the running activation (no flip to tab-y)");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "history X") === true,
  "queued click keeps the visible tab's cached state intact");

// The queued request then runs and lands on the user's last click.
await act(async () => {
  gateA.resolve();
  activationHolds.delete("tab-a");
  await queuedSecond;
  await flushPromises();
});
await waitFor("queued last click applies once it runs", () => controller?.activeTabId === "tab-a");

// A prompt can arrive on A before a slow A→X→A switch completes. The final A
// activation must preserve its card, and X's stale completion must reassert A
// without clearing or replaying a sibling tab's prompt.
const promptBlockedA = { ...tabA, running: true, pendingPrompt: true, cancellable: true };
tabsById.set(tabA.id, promptBlockedA);
await act(async () => {
  for (const handler of eventHandlers) {
    handler({
      kind: "ask_request",
      tabId: tabA.id,
      ask: { id: "pending-tab-a", questions: [{ id: "choice", prompt: "Keep me through A-X-A", options: [] }] },
    });
  }
  await flushPromises();
});
eq(controller?.state.ask?.id, "pending-tab-a", "A starts the rapid switch with a visible ask");

const slowSwitchGate = deferred<void>();
activationHolds.set(tabX.id, slowSwitchGate.promise);
let slowSwitch: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  slowSwitch = controller?.switchTab(tabX.id, tabX);
  await flushPromises();
});
eq(controller?.activeTabId, tabX.id, "A→X renders the slow target optimistically");

await act(async () => {
  await controller?.switchTab(tabA.id, promptBlockedA);
  await flushPromises();
});
eq(controller?.activeTabId, tabA.id, "A→X→A returns to the prompt owner");
eq(controller?.state.ask?.id, "pending-tab-a", "returning to A preserves its ask");
ok(replayTargets.includes(tabA.id), "post-activation replay is scoped to A");

await act(async () => {
  slowSwitchGate.resolve();
  activationHolds.delete(tabX.id);
  await slowSwitch;
  await flushPromises();
});
eq(controller?.activeTabId, tabA.id, "late X activation cannot replace A");
eq(backendActiveId, tabA.id, "late X activation reasserts A as backend owner");
eq(controller?.state.ask?.id, "pending-tab-a", "late X completion cannot clear A's ask");

// Wiring lock: App.enqueueNavigation must invalidate in-flight activations at
// enqueue time — the queue-based scenario above only proves the mechanism.
const appSource = readFileSync(new URL("../App.tsx", import.meta.url), "utf8");
ok(
  /const enqueueNavigation = useCallback\(\(input: DesktopNavigationIntent\)[\s\S]{0,900}?const navigationIntentSeq = noteNavigationIntent\(\);[\s\S]{0,900}?enqueueNavigationWithIntent\(input, navigationIntentSeq\)/.test(appSource),
  "App.enqueueNavigation captures a shared navigation intent before handing the request to the queue",
);
ok(
  /const enqueueNavigationWithIntent = useCallback\([\s\S]{0,900}?enqueueNavigationRequest\([\s\S]{0,900}?\{ \.\.\.input, navigationIntentSeq \}/.test(appSource),
  "App.enqueueNavigationWithIntent forwards the captured intent into enqueueNavigationRequest",
);
ok(
  /const enqueueTabSwitch = useCallback\([\s\S]{0,1400}?const navigationIntentSeq = noteNavigationIntent\(\);[\s\S]{0,1400}?switchTab\(request\.tabId, request\.optimisticTab, request\.navigationIntentSeq\)/.test(appSource),
  "App.enqueueTabSwitch invalidates older navigation at enqueue time and forwards the shared intent",
);
ok(
  /const latest = \(\) => request\.seq === navigationSeqRef\.current && isNavigationIntentCurrent\(request\.navigationIntentSeq\)/.test(appSource),
  "App navigation results require both queue ownership and the shared navigation intent",
);

// useController owns periodic runtime metadata refreshes. Unmount explicitly
// so the suite verifies their cleanup and does not keep the discovery runner
// alive after all assertions have passed.
await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

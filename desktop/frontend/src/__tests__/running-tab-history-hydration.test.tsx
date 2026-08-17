// Run: tsx src/__tests__/running-tab-history-hydration.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { useController } from "../lib/useController";
import { historySliceFromMessages } from "./mockHistorySlice";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, HistorySliceRequest, JobView, Meta, TabMeta, WireEvent } from "../lib/types";

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
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
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

async function settle() {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
  }
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
    sessionPath: tab.sessionPath,
    sessionRevision: tab.sessionRevision,
    sessionDigest: tab.sessionDigest,
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

console.log("\nrunning tab history hydration");

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
// The session the user clicks into: it has been running for a while, so the
// backend reports it running before hydration finishes.
const tabR = tabMeta("tab-r");
const tabsById = new Map([tabA, tabR].map((tab) => [tab.id, tab]));
const runningTabs = new Set<string>(["tab-r"]);
let backendActiveId = "tab-a";
const eventHandlers: Array<(e: WireEvent) => void> = [];
const readyHandlers: Array<(tabId?: string) => void> = [];

function currentTabs(): TabMeta[] {
  return Array.from(tabsById.values()).map((tab) => {
    const running = runningTabs.has(tab.id);
    return { ...tab, active: tab.id === backendActiveId, running, cancellable: running };
  });
}

function historyFor(tabID: string): HistoryMessage[] {
  if (tabID === "tab-r") return [userMessage("history R 1"), userMessage("history R 2")];
  if (tabID === "tab-s") return [userMessage("history S")];
  return [userMessage("cached A")];
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
      MetaForTab: async (tabID: string) => metaFor(tabsById.get(tabID) ?? tabA),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (tabID: string) => historyFor(tabID),
      HistoryPageForTab: async (tabID: string) => {
        const messages = historyFor(tabID);
        const turns = messages.filter((message) => message.role === "user").length;
        return { messages, startTurn: 0, endTurn: turns, totalTurns: turns, hasOlder: false };
      },
      HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) =>
        historySliceFromMessages(tabID, historyFor(tabID), req),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      SetActiveTab: async (tabID: string) => {
        backendActiveId = tabID;
      },
      CancelTab: async (tabID: string) => {
        runningTabs.delete(tabID);
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

await waitFor(
  "initial active tab hydrated",
  () => controller?.activeTabId === "tab-a" && controller.state.items.some((item) => item.kind === "user" && item.text === "cached A"),
);

// Clicking a session that the backend already reports as running: the tab has
// no cached transcript, so hydration is the only thing that can put the
// conversation on screen.
await act(async () => {
  void controller?.switchTab("tab-r", { ...tabR, running: true, cancellable: true });
  await flushPromises();
});
await settle();

eq(controller?.activeTabId, "tab-r", "switching to the running session activates its tab");
eq(controller?.state.running, true, "the running session keeps its live status");
ok(
  controller?.state.items.some((item) => item.kind === "user" && item.text === "history R 1") ?? false,
  "an uncached running session still hydrates its persisted history",
);
ok(
  controller?.state.items.some((item) => item.kind === "user" && item.text === "history R 2") ?? false,
  "the whole history page lands, not just the newest turn",
);

// Second door: a session that is mid-stream when it is opened. Its live text
// makes the transcript look cached, which used to skip the history fetch
// outright and leave the streaming turn floating over an empty transcript.
tabsById.set("tab-s", tabMeta("tab-s"));
runningTabs.add("tab-s");
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "turn_started", tabId: "tab-s" } as WireEvent);
    handler({ kind: "text", tabId: "tab-s", text: "streaming S" } as WireEvent);
  }
  await flushPromises();
});
await act(async () => {
  void controller?.switchTab("tab-s", { ...tabsById.get("tab-s")!, running: true, cancellable: true });
  await flushPromises();
});
await settle();

eq(controller?.activeTabId, "tab-s", "switching to the streaming session activates its tab");
ok(
  controller?.state.items.some((item) => item.kind === "user" && item.text === "history S") ?? false,
  "a mid-stream session still fetches and installs its history",
);
ok(controller?.state.live !== undefined, "installing history leaves the live stream alone");
eq(controller?.state.items[0]?.kind, "user", "the persisted page lands in front of the streaming turn");
eq(controller?.state.items[controller.state.items.length - 1]?.kind, "assistant", "the streaming turn stays at the tail");

await act(async () => {
  root.unmount();
});
dom.window.close();

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);

// Run: tsx src/__tests__/topic-activation.test.tsx
//
// Ticketed topic activation (StartTopicActivation + "topic:activation"
// events): rapid A→B→C navigation with out-of-order lifecycle events hydrates
// only the last click; a terminal "failed" surfaces the hydrate-error UI; a
// terminal event that beats the ticket resolution is stashed and replayed;
// the legacy agent:ready flow and the tab:meta refresh push still work.

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
  TabMetaRefreshEvent,
  TopicActivationEvent,
  TopicActivationRequest,
  WireEvent,
} from "../lib/types";

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

function metaFor(tab: TabMeta, overrides: Partial<Meta> = {}): Meta {
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
    gitBranch: tab.gitBranch,
    autoApproveTools: false,
    bypass: false,
    collaborationMode: tab.collaborationMode ?? "normal",
    toolApprovalMode: tab.toolApprovalMode ?? "ask",
    tokenMode: tab.tokenMode ?? "full",
    goal: "",
    goalStatus: "stopped",
    ...overrides,
  };
}

console.log("\ntopic activation (ticketed)");

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
const tabR = tabMeta("tab-r", { running: true, cancellable: true });
let backendActiveId = "tab-a";
const tabsById = new Map([tabA, tabB, tabC, tabR].map((tab) => [tab.id, tab]));
const eventHandlers: Array<(e: WireEvent) => void> = [];
const readyHandlers: Array<(tabId?: string) => void> = [];
const topicActivationHandlers: Array<(e: TopicActivationEvent) => void> = [];
const tabMetaHandlers: Array<(e: TabMetaRefreshEvent) => void> = [];
// requestId the controller issued per tab, recorded by the mock.
const requestIdByTab = new Map<string, string>();
// When true, the mock emits starting+ready synchronously BEFORE returning the
// ticket (exercises the terminal-event stash path).
let eagerActivationEvents = false;
let failedHistoryTabId = "";
let failSetActiveTabId = "";
let restoredTabSeq = 0;

function emitActivation(event: TopicActivationEvent): void {
  for (const handler of topicActivationHandlers) handler(event);
}

function historyFor(tabID: string): HistoryMessage[] {
  if (tabID === "tab-r") return [{ role: "user", content: "帮我查询，现在我链接的 deep" }];
  if (tabID.startsWith("tab-c-restored-")) return [{ role: "user", content: "history tab-c" }];
  return [{ role: "user", content: `history ${tabID}` }];
}

function hasHistory(tabID: string): boolean {
  return controller?.state.items.some((item) => item.kind === "user" && item.text === `history ${tabID}`) ?? false;
}

window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "agent:event") eventHandlers.push(cb as (e: WireEvent) => void);
    if (name === "agent:ready") readyHandlers.push(cb as (tabId?: string) => void);
    if (name === "topic:activation") topicActivationHandlers.push(cb as (e: TopicActivationEvent) => void);
    if (name === "tab:meta") tabMetaHandlers.push(cb as (e: TabMetaRefreshEvent) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => Array.from(tabsById.values()).map((tab) => ({ ...tab, active: tab.id === backendActiveId })),
      MetaForTab: async (tabID: string) => metaFor(tabsById.get(tabID) ?? tabA),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (tabID: string) => historyFor(tabID),
      HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) => {
        if (tabID === failedHistoryTabId) throw new Error(`/private/${tabID}/history.jsonl could not be read`);
        return historySliceFromMessages(tabID, historyFor(tabID), req);
      },
      HistoryCheckpointTurnsForTab: async () => [],
      StartTopicActivation: async (req: TopicActivationRequest) => {
        const target = Array.from(tabsById.values()).find((tab) => tab.workspaceRoot === req.workspaceRoot && tab.topicId === req.topicId) ?? tabA;
        backendActiveId = target.id;
        const requestId = req.requestId || `mock-activation-${target.id}`;
        requestIdByTab.set(target.id, requestId);
        if (eagerActivationEvents) {
          emitActivation({ requestId, tabId: target.id, phase: "starting" });
          emitActivation({ requestId, tabId: target.id, phase: "ready" });
        }
        return { requestId, tabId: target.id, meta: { ...target, active: true } };
      },
      SetActiveTab: async (tabID: string) => {
        if (tabID === failSetActiveTabId) throw new Error("source tab was already pruned");
        backendActiveId = tabID;
      },
      OpenTopicSession: async (_scope: string, _workspaceRoot: string, _topicID: string, sessionPath: string) => {
        const source = Array.from(tabsById.values()).find((tab) => tab.sessionPath === sessionPath) ?? tabA;
        const restored = tabMeta(`${source.id}-restored-${++restoredTabSeq}`, {
          ...source,
          id: `${source.id}-restored-${restoredTabSeq}`,
          active: true,
        });
        tabsById.set(restored.id, restored);
        backendActiveId = restored.id;
        return restored;
      },
      ReplayPendingPrompts: async () => {},
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
await waitFor("initial tab", () => controller?.activeTabId === "tab-a" && hasHistory("tab-a"));

// ── rapid A→B→C with out-of-order events: only C hydrates ──────────────────
await act(async () => {
  await controller?.activateTopic("project", tabB.workspaceRoot, tabB.topicId ?? "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-b", "B applies optimistically on its ticket");
eq(controller?.state.hydrating, true, "B shows the hydrating surface until its terminal event");

await act(async () => {
  await controller?.activateTopic("project", tabC.workspaceRoot, tabC.topicId ?? "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-c", "C applies optimistically over B");

// Out of order: B's terminal events arrive (superseded) before C's ready.
await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-b") ?? "", tabId: "tab-b", phase: "cancelled" });
  emitActivation({ requestId: requestIdByTab.get("tab-b") ?? "", tabId: "tab-b", phase: "ready" });
  emitActivation({ requestId: requestIdByTab.get("tab-a") ?? "", tabId: "tab-a", phase: "ready" });
  await flushPromises();
});
eq(controller?.activeTabId, "tab-c", "superseded terminal events do not flip the visible tab");
ok(!hasHistory("tab-b") && !hasHistory("tab-a"), "superseded terminal events never hydrate");
eq(controller?.state.hydrating, true, "C still waits for its own terminal event");

await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-c") ?? "", tabId: "tab-c", phase: "starting" });
  await flushPromises();
});
eq(controller?.state.hydrating, true, "starting does not hydrate");
await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-c") ?? "", tabId: "tab-c", phase: "ready" });
  await flushPromises();
});
await waitFor("C hydrates on its ready", () => hasHistory("tab-c") && controller?.state.hydrating === false);
ok(!hasHistory("tab-b"), "only the last click's history is visible");

// ── terminal failure restores the committed source surface ─────────────────
await act(async () => {
  await controller?.activateTopic("project", tabA.workspaceRoot, tabA.topicId ?? "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-a", "A applies optimistically");
await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-a") ?? "", tabId: "tab-a", phase: "failed", error: "session failed to start" });
  await flushPromises();
});
eq(controller?.activeTabId, "tab-c", "failed activation restores the previously committed source tab");
eq(controller?.state.hydrating, false, "restored source is immediately usable");
ok(hasHistory("tab-c"), "failed activation retains the source transcript");
const failureNotice = controller?.state.items.findLast((item) => item.kind === "notice");
ok(Boolean(failureNotice && failureNotice.kind === "notice" && !failureNotice.text.includes("session failed to start")), "failure notice is sanitized before it reaches the restored source");

// ── activation succeeds but target history fails: source still wins ─────────
failedHistoryTabId = "tab-a";
failSetActiveTabId = "tab-c";
await act(async () => {
  await controller?.activateTopic("project", tabA.workspaceRoot, tabA.topicId ?? "");
  await flushPromises();
  emitActivation({ requestId: requestIdByTab.get("tab-a") ?? "", tabId: "tab-a", phase: "ready" });
  await flushPromises();
});
await waitFor("history failure rebinds the pruned committed source", () => controller?.activeTabId?.startsWith("tab-c-restored-") ?? false);
ok(hasHistory("tab-c"), "target history failure retains the source controller transcript");
const historyFailureNotice = controller?.state.items.findLast((item) => item.kind === "notice");
ok(!(historyFailureNotice?.kind === "notice" && historyFailureNotice.text.includes("/private/")), "target history failure does not expose a local path");
failedHistoryTabId = "";
failSetActiveTabId = "";

// ── a terminal event that beats the ticket is stashed and replayed ──────────
eagerActivationEvents = true;
await act(async () => {
  await controller?.activateTopic("project", tabB.workspaceRoot, tabB.topicId ?? "");
  await flushPromises();
});
await waitFor("stashed ready hydrates after the ticket lands", () => controller?.activeTabId === "tab-b" && hasHistory("tab-b"));
eagerActivationEvents = false;

// ── legacy agent:ready flow still works for non-ticketed tabs ───────────────
await act(async () => {
  for (const handler of readyHandlers) handler("tab-b");
  await flushPromises();
});
await waitFor("legacy ready keeps the session", () => controller?.activeTabId === "tab-b" && hasHistory("tab-b"));

// ── tab:meta refresh push merges; wrong session is fenced out ───────────────
await act(async () => {
  for (const handler of tabMetaHandlers) handler({ tabId: "tab-b", meta: metaFor(tabB, { gitBranch: "feature/x", imageInputEnabled: true }) });
  await flushPromises();
});
eq(controller?.state.meta?.gitBranch, "feature/x", "tab:meta merges the refreshed git branch");
eq(controller?.state.meta?.imageInputEnabled, true, "tab:meta merges the refreshed image-input capability");
await act(async () => {
  for (const handler of tabMetaHandlers) handler({ tabId: "tab-b", meta: metaFor(tabB, { gitBranch: "stale", sessionPath: "/elsewhere/other.jsonl" }) });
  await flushPromises();
});
eq(controller?.state.meta?.gitBranch, "feature/x", "tab:meta for a different session binding is discarded");

// ── switch away from a thinking session and back: keep its transcript ──────
await act(async () => {
  await controller?.activateTopic("project", tabR.workspaceRoot, tabR.topicId ?? "");
  await flushPromises();
});
await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-r") ?? "", tabId: "tab-r", phase: "ready" });
  await flushPromises();
});
await waitFor("running session hydrates on first open", () =>
  controller?.activeTabId === "tab-r" &&
  controller.state.running === true &&
  controller.state.hydrating === false &&
  (controller.state.items.some((item) => item.kind === "user" && item.text === "帮我查询，现在我链接的 deep") ?? false),
);
eq(controller?.state.running, true, "reattached thinking session stays marked running");
eq(controller?.state.hydrating, false, "running-session hydrate settles instead of leaving Welcome");

await act(async () => {
  await controller?.activateTopic("project", tabB.workspaceRoot, tabB.topicId ?? "");
  await flushPromises();
});
await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-b") ?? "", tabId: "tab-b", phase: "ready" });
  await flushPromises();
});
await waitFor("idle session replaces the thinking surface", () => hasHistory("tab-b") && controller?.state.hydrating === false);

await act(async () => {
  await controller?.activateTopic("project", tabR.workspaceRoot, tabR.topicId ?? "");
  await flushPromises();
});
eq(controller?.activeTabId, "tab-r", "switch-back selects the thinking session");
eq(
  controller?.state.hydratePlaceholderItems?.some((item) => item.kind === "user" && item.text === "history tab-b") ?? false,
  false,
  "switch-back does not present the other session as a hydration placeholder",
);
await act(async () => {
  emitActivation({ requestId: requestIdByTab.get("tab-r") ?? "", tabId: "tab-r", phase: "ready" });
  await flushPromises();
});
await waitFor("switch-back restores the thinking transcript", () =>
  controller?.activeTabId === "tab-r" &&
  controller.state.running === true &&
  controller.state.hydrating === false &&
  (controller.state.items.some((item) => item.kind === "user" && item.text === "帮我查询，现在我链接的 deep") ?? false),
);
eq(controller?.state.running, true, "switch-back keeps the composer in the live turn");
eq(controller?.state.items.some((item) => item.kind === "user" && item.text === "history tab-b") ?? false, false, "switch-back does not keep the other session as the visible transcript");
eq(controller?.state.hydratePlaceholderItems?.length ?? 0, 0, "switch-back clears the foreign placeholder after live history lands");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

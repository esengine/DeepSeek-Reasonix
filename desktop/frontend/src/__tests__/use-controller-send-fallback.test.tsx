// Run: tsx src/__tests__/use-controller-send-fallback.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider, preloadLocale, useI18n } from "../lib/i18n";
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
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function tabMeta(overrides: Partial<TabMeta> = {}): TabMeta {
  return {
    id: "tab-send",
    scope: "project",
    workspaceRoot: "/repo/send",
    workspaceName: "send",
    workspacePath: "/repo/send",
    gitBranch: "main",
    topicId: "topic-send",
    topicTitle: "Send",
    label: "model-send",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo/send",
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

console.log("\nuse controller send fallback");

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

let backendTab = tabMeta({ backgroundJobs: 2 });
const context: ContextInfo = { used: 0, window: 100, sessionTokens: 0 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];
let tabsAvailable = false;
let submitCalls = 0;
let rejectSubmit = false;
let rejectAnswer = false;
let rejectListTabs = false;
let listTabsCalls = 0;
const exactAnswerCalls: Array<{ tabId: string; turnId: string; promptId: string; answers: unknown[] }> = [];
const legacyAnswerCalls: string[] = [];
const eventHandlers: Array<(event: WireEvent) => void> = [];

window.runtime = {
  EventsOn: (name: string, callback: (...data: unknown[]) => void) => {
    if (name === "agent:event") eventHandlers.push(callback as (event: WireEvent) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => {
        listTabsCalls += 1;
        if (rejectListTabs) throw new Error("runtime status unavailable");
        return tabsAvailable ? [backendTab] : [];
      },
      MetaForTab: async () => metaFor(backendTab),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (): Promise<HistoryMessage[]> => [],
      HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      ReplayPendingPromptsForTab: async () => {},
      SubmitToTab: async (tabId: string) => {
        submitCalls += tabId === "tab-send" ? 1 : 0;
      },
      SubmitToTabWithID: async (tabId: string) => {
        submitCalls += tabId === "tab-send" ? 1 : 0;
        if (rejectSubmit) throw new Error("turn already running");
      },
      AnswerQuestionForTab: async (_tabId: string, promptId: string) => { legacyAnswerCalls.push(promptId); },
      AnswerPromptForTab: async (tabId: string, turnId: string, promptId: string, answers: unknown[]) => {
        exactAnswerCalls.push({ tabId, turnId, promptId, answers });
        if (rejectAnswer) throw new Error("prompt write failed");
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;
let setLocale: ReturnType<typeof useI18n>["setPref"] | undefined;

function Probe() {
  setLocale = useI18n().setPref;
  controller = useController();
  return null;
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<LocaleProvider><Probe /></LocaleProvider>);
  await flushPromises();
});
eq(controller?.activeTabId, undefined, "startup has no active tab when backend has no tabs");

tabsAvailable = true;
await act(async () => {
  await controller?.send("hello from fallback");
  await flushPromises();
});

eq(controller?.activeTabId, "tab-send", "send fallback activates the backend-selected tab");
eq(controller?.state.backgroundJobs, 2, "send fallback reconciles backend runtime metadata");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "hello from fallback") ?? false, "send fallback keeps the optimistic user turn");
eq(submitCalls, 1, "send fallback submits to the activated tab");

await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_done", tabId: "tab-send" } as WireEvent);
  await flushPromises();
});

backendTab = tabMeta({ running: true, pendingPrompt: true, turnId: "turn-authoritative" });
await act(async () => {
  for (const handler of eventHandlers) handler({
    kind: "ask_request",
    tabId: "tab-send",
    ask: { id: "ask-fallback", questions: [{ id: "q1", prompt: "Proceed?", options: [{ label: "yes" }] }] },
  } as WireEvent);
  await flushPromises();
});
eq(controller?.state.activeTurnId, undefined, "Ask fixture starts without a local turn id");
const beforeAnswerListCalls = listTabsCalls;
await act(async () => {
  await controller?.answerQuestion("ask-fallback", [{ questionId: "q1", selected: ["yes"] }]);
  await flushPromises();
});
eq(listTabsCalls, beforeAnswerListCalls + 1, "Ask answer resolves one authoritative ListTabs fallback");
eq(exactAnswerCalls.at(-1)?.turnId, "turn-authoritative", "Ask answer uses the authoritative turn fence");
eq(legacyAnswerCalls.length, 0, "Ask answer never falls back to the unfenced endpoint");
eq(controller?.state.ask, undefined, "successful exact answer clears the matching Ask without replay");

await act(async () => {
  for (const handler of eventHandlers) handler({
    kind: "ask_request",
    tabId: "tab-send",
    turnId: "turn-authoritative",
    ask: { id: "ask-retry", questions: [{ id: "q2", prompt: "Retry?", options: [{ label: "yes" }] }] },
  } as WireEvent);
  await flushPromises();
});
rejectAnswer = true;
let answerRejected = false;
await preloadLocale("zh");
await act(async () => {
  setLocale?.("zh");
  await flushPromises();
});
await act(async () => {
  try {
    await controller?.answerQuestion("ask-retry", [{ questionId: "q2", selected: ["yes"] }]);
  } catch {
    answerRejected = true;
  }
  await flushPromises();
});
eq(answerRejected, true, "failed exact answer propagates to AskCard");
eq(controller?.state.ask?.id, "ask-retry", "failed exact answer preserves the pending Ask");
eq(controller?.state.pendingPrompt, true, "failed exact answer keeps the prompt gate active");
eq(controller?.state.items.find((item) => item.kind === "notice" && item.text.includes("prompt write failed"))?.text, "提交回答失败：prompt write failed", "failed Ask answer uses the active locale");
rejectAnswer = false;

rejectSubmit = true;
await act(async () => {
  await controller?.send("continue while prompt is pending");
  await flushPromises();
  await flushPromises();
});
eq(controller?.state.items.some((item) => item.kind === "user" && item.text === "continue while prompt is pending" && item.failed), true, "colliding submit marks its optimistic bubble failed");
eq(controller?.state.ask?.id, "ask-retry", "colliding submit preserves the pending Ask");
eq(controller?.state.running, true, "active backend snapshot keeps the composer blocked after rejection");
eq(controller?.state.pendingPrompt, true, "active backend snapshot restores the prompt gate after rejection");
eq(controller?.state.activeTurnId, "turn-authoritative", "active backend snapshot restores the authoritative turn id");

await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_done", tabId: "tab-send", turnId: "turn-authoritative" } as WireEvent);
  backendTab = tabMeta({ running: false, pendingPrompt: false, turnId: undefined });
  await flushPromises();
  await controller?.send("retry against an idle backend");
  await flushPromises();
  await flushPromises();
});
eq(controller?.state.running, false, "authoritative idle snapshot releases a rejected submit");
eq(controller?.state.pendingPrompt, false, "authoritative idle snapshot leaves no prompt gate");

rejectListTabs = true;
const beforeFailedReconcileCalls = listTabsCalls;
await act(async () => {
  await controller?.send("retry while runtime status is unavailable");
  await new Promise((resolve) => setTimeout(resolve, 1_500));
});
eq(listTabsCalls - beforeFailedReconcileCalls, 4, "rejected submit retries failed ListTabs reads at every bounded delay");
eq(controller?.state.running, true, "exhausted status reads leave the composer conservatively blocked");
rejectListTabs = false;

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

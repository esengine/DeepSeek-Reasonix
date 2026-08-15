// Run: tsx src/__tests__/use-controller-stale-turn-watchdog.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import type { HistoryMessage, Meta, TabMeta, WireEvent } from "../lib/types";
import { useController } from "../lib/useController";
import { historySliceFromMessages } from "./mockHistorySlice";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => globalThis.setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => { await flushPromises(); });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

console.log("\nstale turn watchdog");

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
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const originalNow = Date.now;
let fakeNow = 1_000;
Date.now = () => fakeNow;
let nextTimerID = 1;
const watchdogTimers = new Map<number, () => void>();
window.setTimeout = ((handler: TimerHandler, delay?: number) => {
  const id = nextTimerID++;
  if ((delay ?? 0) >= 29_000 && typeof handler === "function") watchdogTimers.set(id, handler);
  else globalThis.setTimeout(handler, delay);
  return id;
}) as typeof window.setTimeout;
window.clearTimeout = ((id?: number) => {
  if (id !== undefined) watchdogTimers.delete(id);
}) as typeof window.clearTimeout;

const tabID = "tab-watchdog";
const sessionPath = "/repo/sessions/watchdog.jsonl";
let backendRunning = false;
let revision = 1;
let history: HistoryMessage[] = [];
let listTabsCalls = 0;
let historyCalls = 0;
const agentEventHandlers: Array<(event: WireEvent) => void> = [];

function tabMeta(): TabMeta {
  return {
    id: tabID,
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    topicId: "topic-watchdog",
    topicTitle: "Watchdog",
    sessionPath,
    sessionRevision: revision,
    sessionDigest: `digest-${revision}`,
    label: "model",
    ready: true,
    running: backendRunning,
    cancellable: backendRunning,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
  };
}

function meta(): Meta {
  return {
    label: "model",
    ready: true,
    eventChannel: "agent:event",
    sessionPath,
    sessionRevision: revision,
    sessionDigest: `digest-${revision}`,
    cwd: "/repo",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
  };
}

window.runtime = {
  EventsOn: (name, handler) => {
    if (name === "agent:event") agentEventHandlers.push(handler as (event: WireEvent) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => {
        listTabsCalls += 1;
        return [tabMeta()];
      },
      MetaForTab: async () => meta(),
      ContextUsageForTab: async () => ({ used: 0, window: 1000, sessionTokens: 0 }),
      EffortForTab: async () => ({ supported: true, current: "auto", default: "auto", levels: ["auto"] }),
      BalanceForTab: async () => ({ available: false, display: "" }),
      JobsForTab: async () => [],
      CheckpointsForTab: async () => [],
      HistorySliceForTab: async (_id, request) => {
        historyCalls += 1;
        return historySliceFromMessages(tabID, history, request, { revision, digest: `digest-${revision}` });
      },
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      SubmitToTabWithID: async () => {
        backendRunning = true;
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

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});
await waitFor("initial session", () => controller?.activeTabId === tabID && historyCalls > 0);

await act(async () => {
  for (const handler of agentEventHandlers) {
    handler({ kind: "turn_started", tabId: tabID });
    handler({ kind: "turn_done", tabId: tabID });
  }
  await flushPromises();
});
fakeNow += 60_000;
await act(async () => {
  await controller?.send("hello");
  await flushPromises();
});
ok(controller?.state.running ?? false, "optimistic submit enters running state without any agent events");
ok(controller?.state.turnActive === false, "missing turn_started leaves no live turn evidence");
ok(watchdogTimers.size === 1, "optimistic submit arms the stale-turn watchdog");

const firstTimer = watchdogTimers.entries().next().value as [number, () => void] | undefined;
if (firstTimer) {
  watchdogTimers.delete(firstTimer[0]);
  fakeNow += 30_000;
  await act(async () => {
    firstTimer[1]();
    await flushPromises();
  });
}
ok(listTabsCalls >= 2, "first quiet-period probe reconciles backend runtime state");
ok(controller?.state.running ?? false, "a genuinely running backend remains running");
ok(watchdogTimers.size === 1, "still-running probe re-arms instead of stopping after one check");

backendRunning = false;
revision = 2;
history = [
  { role: "user", content: "hello" },
  { role: "assistant", content: "recovered answer" },
];
const secondTimer = watchdogTimers.entries().next().value as [number, () => void] | undefined;
if (secondTimer) {
  watchdogTimers.delete(secondTimer[0]);
  fakeNow += 30_000;
  await act(async () => {
    secondTimer[1]();
    await flushPromises();
  });
}
await waitFor("missed answer hydration", () => controller?.state.items.some(
  (item) => item.kind === "assistant" && item.text === "recovered answer",
) ?? false);
ok(controller?.state.running === false, "idle backend settles the spinner without switching tabs");
ok(controller?.state.items.some((item) => item.kind === "assistant" && item.text === "recovered answer") ?? false, "watchdog hydrates the persisted missed answer");
ok(watchdogTimers.size === 0, "settled turn leaves no watchdog timer behind");

await act(async () => { root.unmount(); });
Date.now = originalNow;
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

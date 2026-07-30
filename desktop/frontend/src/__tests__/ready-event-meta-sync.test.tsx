// Run: tsx src/__tests__/ready-event-meta-sync.test.tsx

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

function tabMeta(ready: boolean): TabMeta {
  return {
    id: "tab-ready",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    topicId: "topic-ready",
    topicTitle: "Ready race",
    sessionPath: "/repo/sessions/ready.jsonl",
    label: "model",
    ready,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
  };
}

function meta(ready: boolean): Meta {
  return {
    label: "model",
    ready,
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
  };
}

console.log("\nready event meta sync");

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
const readyHandlers: Array<(tabId?: string) => void> = [];
const historyGate = deferred<HistoryMessage[]>();
let backendReady = false;
let listTabsCalls = 0;
let historyCalls = 0;
let metaCalls = 0;

window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "agent:ready") readyHandlers.push(cb as (tabId?: string) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => {
        listTabsCalls += 1;
        return [tabMeta(backendReady)];
      },
      MetaForTab: async () => {
        metaCalls += 1;
        return meta(backendReady);
      },
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async () => historyGate.promise,
      HistoryPageForTab: async () => {
        historyCalls += 1;
        const messages = await historyGate.promise;
        return { messages, startTurn: 0, endTurn: messages.filter((message) => message.role === "user").length, totalTurns: messages.filter((message) => message.role === "user").length, hasOlder: false };
      },
      HistoryCheckpointTurnsForTab: async () => [],
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

await waitFor("initial not-ready metadata", () => controller?.activeTabId === "tab-ready" && controller.state.meta?.ready === false);
eq(historyCalls, 1, "startup begins one history hydration");
eq(metaCalls, 0, "startup history remains in flight before ancillary meta");

backendReady = true;
await act(async () => {
  for (const handler of readyHandlers) handler("tab-ready");
  await flushPromises();
});
await waitFor("ready metadata refreshed before history settles", () => controller?.state.meta?.ready === true);

eq(listTabsCalls >= 2, true, "ready event refreshes active tab metadata from ListTabs");
eq(historyCalls, 1, "ready event joins the in-flight hydrate instead of reloading history");
eq(metaCalls, 0, "ready event does not wait for ancillary MetaForTab before unlocking send");

await act(async () => {
  historyGate.resolve([{ role: "user", content: "hello" }]);
  await historyGate.promise;
  await flushPromises();
});
await waitFor("history finishes", () => controller?.state.hydrating === false);
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "hello") ?? false, "history still hydrates after the ready metadata sync");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

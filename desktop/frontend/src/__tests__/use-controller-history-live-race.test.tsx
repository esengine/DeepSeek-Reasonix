// Run: tsx src/__tests__/use-controller-history-live-race.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { useController } from "../lib/useController";
import { historySliceFromMessages } from "./mockHistorySlice";
import type { HistorySlice, Meta, TabMeta, WireEvent } from "../lib/types";

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

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => { await flushPromises(); });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

console.log("\nuse controller history/live race");

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

const tab: TabMeta = {
  id: "tab-live",
  scope: "project",
  workspaceRoot: "/repo",
  workspaceName: "repo",
  workspacePath: "/repo",
  topicId: "topic-live",
  topicTitle: "General",
  sessionPath: "/repo/sessions/live.jsonl",
  sessionRevision: 1,
  sessionDigest: "digest-v1",
  label: "model",
  ready: true,
  running: false,
  mode: "normal",
  toolApprovalMode: "ask",
  tokenMode: "full",
  active: true,
  cwd: "/repo",
};
const meta: Meta = {
  label: "model",
  ready: true,
  eventChannel: "agent:event",
  sessionPath: tab.sessionPath,
  sessionRevision: tab.sessionRevision,
  sessionDigest: tab.sessionDigest,
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
const historyGate = deferred<HistorySlice>();
const eventHandlers: Array<(event: WireEvent) => void> = [];
let historyStarted = false;

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
      ListTabs: async () => [tab],
      MetaForTab: async () => meta,
      ContextUsageForTab: async () => ({ used: 0, window: 100, sessionTokens: 0 }),
      EffortForTab: async () => ({ supported: true, current: "auto", default: "auto", levels: ["auto"] }),
      BalanceForTab: async () => ({ available: false, display: "" }),
      JobsForTab: async () => [],
      CheckpointsForTab: async () => [],
      HistorySliceForTab: async () => {
        historyStarted = true;
        return historyGate.promise;
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

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});
await waitFor("history request", () => historyStarted && eventHandlers.length > 0);

await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_started", tabId: tab.id });
  await flushPromises();
});
ok(controller?.state.items.some((item) => item.kind === "assistant" && item.streaming) ?? false, "turn starts while history is pending");

historyGate.resolve(historySliceFromMessages(
  tab.id,
  [{ role: "user", content: "stale durable history" }],
  { cursor: "", turns: 12 },
  { revision: 1, digest: "digest-v1" },
));
await waitFor("hydration completion", () => controller?.state.hydrating === false);

ok(controller?.state.running ?? false, "late history keeps the live turn running");
ok(controller?.state.items.some((item) => item.kind === "assistant" && item.streaming) ?? false, "late history keeps the live assistant stream");
// The page read before the turn started is this session's history, not a
// competing version of it: it goes in front of the live turn instead of
// replacing it, so the turn never streams over a blank transcript.
ok(controller?.state.items[0]?.kind === "user", "late history lands in front of the live turn");
ok(controller?.state.items.at(-1)?.kind === "assistant", "late history leaves the live turn at the tail");

await act(async () => { root.unmount(); });
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

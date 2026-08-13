// Run: tsx src/__tests__/use-controller-cancel-reconcile.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useController } from "../lib/useController";
import type { AppBindings } from "../lib/bridge";
import type { ContextInfo, EffortInfo, HistorySlice, HistorySliceRequest, Meta, TabMeta, WireEvent } from "../lib/types";
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

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
	for (let attempt = 0; attempt < 50; attempt += 1) {
		await act(async () => {
			await flushPromises(20);
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
    topicId: "topic-a",
    topicTitle: "General",
    label: "model",
    ready: true,
    running: false,
    cancellable: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
    ...overrides,
  };
}

function meta(): Meta {
  return {
    label: "model",
    ready: true,
    eventChannel: "agent:event",
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

console.log("\nuse controller cancel reconcile");

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

const eventHandlers: Array<(e: WireEvent) => void> = [];
let backendRunning = false;
let cancelCalls = 0;
let cancelInboxCalls = 0;
let cancelInboxError: Error | null = null;
let effortCalls = 0;
let checkpointHistoryCalls = 0;
let historyLoads = 0;
let checkpointLoads = 0;
let historyShouldFail = false;
let deferredHistory = false;
let resolveDeferredHistory: ((page: HistorySlice) => void) | undefined;
const context: ContextInfo = { used: 0, window: 100, sessionTokens: 0 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };

window.runtime = {
  EventsOn: (name: string, cb: (payload: unknown) => void) => {
    if (name === "agent:event") eventHandlers.push(cb as (e: WireEvent) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => [tabMeta({ running: backendRunning, cancellable: backendRunning })],
      MetaForTab: async () => meta(),
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      SetEffortForTab: async () => {
        effortCalls += 1;
        throw new Error("finish or cancel the current turn, answer pending prompts, and stop background jobs before changing effort");
      },
      BalanceForTab: async () => ({ available: false, display: "" }),
      JobsForTab: async () => [],
      CheckpointsForTab: async () => {
        checkpointLoads += 1;
        return [{ turn: 0, prompt: "hello", files: [], time: Date.now(), canConversation: true }];
      },
      HistoryForTab: async () => [],
      HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) => {
        historyLoads += 1;
        if (historyShouldFail) throw new Error("history unavailable");
        if (deferredHistory) {
          return await new Promise<HistorySlice>((resolve) => {
            resolveDeferredHistory = resolve;
          });
        }
        return historySliceFromMessages(
          tabID,
          [{ role: "user", content: "hello", createdAt: Date.now(), checkpointTurn: 0 }],
          req,
        );
      },
      HistoryCheckpointTurnsForTab: async () => {
        checkpointHistoryCalls += 1;
        return [];
      },
      ReplayPendingPrompts: async () => {},
      SubmitToTab: async () => {},
      SubmitToTabWithID: async () => {},
      CancelTab: async () => {
        cancelCalls += 1;
        backendRunning = false;
      },
      CancelTabWithInboxItems: async () => {
        cancelInboxCalls += 1;
        if (cancelInboxError) throw cancelInboxError;
        backendRunning = false;
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
  await flushPromises(50);
});
historyLoads = 0;
checkpointLoads = 0;

backendRunning = true;
await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_started", tabId: "tab-a" });
  await flushPromises();
});
eq(controller?.state.running, true, "turn_started marks the tab running");

await act(async () => {
  controller?.cancel();
  await flushPromises();
  await flushPromises();
});

for (let attempt = 0; attempt < 20 && controller?.state.running; attempt += 1) {
  await act(async () => {
    await flushPromises(50);
  });
}

eq(controller?.state.running, false, "cancel reconciliation clears the running state");
eq(cancelCalls, 1, "CancelTab is called once");
eq(controller?.state.cancelRequested, false, "cancel reconciliation clears cancelRequested");
await waitFor(
  "cancelled transcript reload",
  () => historyLoads > 0 && checkpointLoads > 0 && Boolean(
    controller?.state.items.some((item) => item.kind === "user" && item.text === "hello") &&
    controller.state.checkpoints.some((checkpoint) => checkpoint.turn === 0 && checkpoint.canConversation),
  ),
);
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "hello"), "cancelled prompt is restored from the authoritative transcript");
ok(controller?.state.checkpoints.some((checkpoint) => checkpoint.turn === 0 && checkpoint.canConversation), "cancelled prompt keeps its conversation checkpoint");

// The screenshot repro stops before turn_started, while the backend has
// already accepted the prompt and will persist it during cancellation cleanup.
historyLoads = 0;
checkpointLoads = 0;
backendRunning = true;
await act(async () => {
  await controller?.send("hello");
  await flushPromises();
});
ok(
  controller?.state.items.some((item) => item.kind === "user" && item.text === "hello"),
  "an immediate stop still has the optimistic prompt item",
);
await act(async () => {
  controller?.cancel();
  await flushPromises();
});
await waitFor(
  "immediate cancelled transcript reload",
  () => !controller?.state.running && historyLoads > 0 && checkpointLoads > 0 && Boolean(
    controller?.state.items.some((item) => item.kind === "user" && item.text === "hello") &&
    controller.state.checkpoints.some((checkpoint) => checkpoint.turn === 0 && checkpoint.canConversation),
  ),
);
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "hello"), "immediate cancellation restores the persisted prompt bubble");
ok(controller?.state.checkpoints.some((checkpoint) => checkpoint.turn === 0 && checkpoint.canConversation), "immediate cancellation restores the rewind checkpoint");

// A new submission must invalidate a cancellation hydrate that is still
// waiting on authoritative history; otherwise its replace dispatch can erase
// the new optimistic turn.
historyLoads = 0;
deferredHistory = true;
backendRunning = true;
await act(async () => {
  await controller?.send("accidental");
  controller?.cancel();
  await flushPromises();
});
await waitFor("deferred cancellation history", () => historyLoads > 0 && resolveDeferredHistory !== undefined);
await act(async () => {
  await controller?.send("corrected");
  resolveDeferredHistory?.(historySliceFromMessages(
    "tab-a",
    [{ role: "user", content: "stale cancellation history", createdAt: Date.now(), checkpointTurn: 0 }],
    { cursor: "" },
  ));
  resolveDeferredHistory = undefined;
  deferredHistory = false;
  await flushPromises();
});
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "corrected"), "resubmission survives a stale cancellation hydrate");
ok(!controller?.state.items.some((item) => item.kind === "user" && item.text === "stale cancellation history"), "stale cancellation history cannot replace a resubmitted turn");

// A failed cancellation history read must preserve the transcript that was
// already visible instead of resetting it to an empty state.
historyLoads = 0;
historyShouldFail = true;
backendRunning = true;
await act(async () => {
  controller?.cancel();
  await flushPromises();
});
await waitFor("failed cancellation history", () => !controller?.state.running && historyLoads > 0);
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "corrected"), "history failure preserves the visible transcript");
historyShouldFail = false;

await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_done", tabId: "tab-a", checkpointTurn: 0 });
  await flushPromises();
});
eq(checkpointHistoryCalls, 0, "TurnDone does not request the full checkpoint-turn history");

await act(async () => {
  await controller?.setEffort("max");
  await flushPromises();
});

const effortNotice = controller?.state.items.find((item) => item.kind === "notice" && item.text.includes("cannot change yet"));
eq(effortCalls, 1, "SetEffortForTab is called once");
ok(Boolean(effortNotice), "busy effort switch surfaces a non-failure warning notice");

cancelInboxError = new Error("reasonix_error:inbox_invalid_state");
await act(async () => {
  controller?.cancel(["queued-guidance"]);
  await flushPromises();
});
const inboxCancelNotice = controller?.state.items.find((item) =>
  item.kind === "notice" && item.text.includes("Cancel failed: This inbox instruction cannot be changed"),
);
eq(cancelInboxCalls, 1, "CancelTabWithInboxItems is called for durable guidance");
ok(Boolean(inboxCancelNotice), "cancel failure formats the stable inbox code for the active locale");
ok(inboxCancelNotice?.kind === "notice" && !inboxCancelNotice.text.includes("reasonix_error:"), "cancel failure never renders the stable transport code");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

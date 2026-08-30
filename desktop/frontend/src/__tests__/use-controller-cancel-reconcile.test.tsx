// Run: tsx src/__tests__/use-controller-cancel-reconcile.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useController } from "../lib/useController";
import type { AppBindings } from "../lib/bridge";
import type { ContextInfo, EffortInfo, HistorySliceRequest, Meta, TabMeta, WireEvent } from "../lib/types";
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
let cancelDiscardedItemIDs: string[] = [];
let effortCalls = 0;
let checkpointHistoryCalls = 0;
let historyLoads = 0;
let checkpointLoads = 0;
let turnReplayCalls = 0;
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
      TurnEventsForTab: async (_tabID: string, afterSeq: number) => {
        turnReplayCalls += 1;
        const events = afterSeq !== 1 ? [] : [
          {
            turnId: "turn-gap",
            seq: 2,
            status: "in_progress",
            event: { kind: "turn_started", turnId: "turn-gap", seq: 2, status: "in_progress" },
          },
          {
            turnId: "turn-gap",
            seq: 3,
            status: "waiting_user",
            event: { kind: "turn_status", turnId: "turn-gap", seq: 3, status: "waiting_user" },
          },
        ];
        return {
          events,
          floorSeq: 1,
          latestSeq: 3,
          nextAfterSeq: events.length > 0 ? 3 : afterSeq,
          hasMore: false,
          resetRequired: false,
        };
      },
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
      CancelTabWithInboxItemsResult: async () => {
        cancelInboxCalls += 1;
        if (cancelInboxError) throw cancelInboxError;
        backendRunning = false;
        return { discardedItemIds: [...cancelDiscardedItemIDs] };
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

// A future event must pause projection until the missing durable prefix has
// been replayed. This interleaving is driven only by resolved promises (no
// timing sleeps), then a duplicate seq=3 is ignored idempotently.
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "turn_status", tabId: "tab-a", turnId: "turn-gap", seq: 1, status: "queued" });
    handler({ kind: "turn_status", tabId: "tab-a", turnId: "turn-gap", seq: 3, status: "waiting_user" });
  }
  for (let step = 0; step < 20; step += 1) await Promise.resolve();
});
eq(turnReplayCalls, 1, "sequence gap replays the durable missing prefix once");
eq(controller?.state.items.find((item) => item.kind === "assistant")?.id, "a:turn-gap:0", "gap replay keeps the stable sampling-segment item id");
eq(controller?.state.pendingPrompt, true, "future event projects only after the missing prefix");
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "turn_status", tabId: "tab-a", turnId: "turn-gap", seq: 3, status: "in_progress" });
  }
  await Promise.resolve();
});
eq(controller?.state.pendingPrompt, true, "duplicate sequence is ignored idempotently");
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "turn_done", tabId: "tab-a", turnId: "turn-gap", seq: 4, status: "completed" });
  }
  await Promise.resolve();
});

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
await waitFor("cancelled checkpoint refresh", () => checkpointLoads > 0);
eq(historyLoads, 0, "cancellation never reloads or replaces the transcript");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "hello"), "cancelled prompt stays in the projected transcript");
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
await waitFor("immediate cancellation", () => !controller?.state.running && checkpointLoads > 0);
eq(historyLoads, 0, "immediate cancellation does not schedule a transcript hydrate");
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "hello"), "immediate cancellation keeps the optimistic prompt bubble");
ok(controller?.state.checkpoints.some((checkpoint) => checkpoint.turn === 0 && checkpoint.canConversation), "immediate cancellation keeps the rewind checkpoint");

// A new submission after the interrupted terminal boundary must remain in the
// reducer because cancellation has no whole-history replacement path anymore.
historyLoads = 0;
await act(async () => {
  await controller?.send("corrected");
  await flushPromises();
});
ok(controller?.state.items.some((item) => item.kind === "user" && item.text === "corrected"), "resubmission survives the completed cancellation cleanup");
eq(historyLoads, 0, "resubmission cannot race a stale cancellation history response");

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

cancelDiscardedItemIDs = ["withdrawn-guidance"];
let cancelOutcome: Awaited<ReturnType<NonNullable<typeof controller>["cancel"]>> | undefined;
await act(async () => {
  cancelOutcome = await controller?.cancel(["withdrawn-guidance", "delivered-guidance"]);
  await flushPromises();
});
eq(cancelOutcome?.discardedItemIds.join(","), "withdrawn-guidance", "cancel returns only backend-confirmed withdrawn IDs");

cancelInboxError = new Error("reasonix_error:inbox_invalid_state");
await act(async () => {
  await controller?.cancel(["queued-guidance"]);
  await flushPromises();
});
const inboxCancelNotice = controller?.state.items.find((item) =>
  item.kind === "notice" && item.text.includes("Cancel failed: This inbox instruction cannot be changed"),
);
eq(cancelInboxCalls, 2, "receipt-capable cancellation is called for durable guidance");
ok(Boolean(inboxCancelNotice), "cancel failure formats the stable inbox code for the active locale");
ok(inboxCancelNotice?.kind === "notice" && !inboxCancelNotice.text.includes("reasonix_error:"), "cancel failure never renders the stable transport code");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

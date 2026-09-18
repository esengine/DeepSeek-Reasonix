// Run: tsx src/__tests__/use-controller-cancel-reconcile.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useController } from "../lib/useController";
import type { AppBindings } from "../lib/bridge";
import type { ContextInfo, EffortInfo, HistoryMessage, HistorySliceRequest, Meta, TabMeta, WireEvent } from "../lib/types";
import { historySliceFromMessages } from "./mockHistorySlice";
import { installDesktopHostStub } from "./desktopHostStub";

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
    session: { hostId: "local", sessionId: "canonical-a" },
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
    session: { hostId: "local", sessionId: "canonical-a" },
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

let backendRunning = false;
const backendHistory: HistoryMessage[] = [{
  role: "user",
  content: "hello",
  messageId: "initial-user",
  createdAt: 1000,
  checkpointTurn: 0,
  attachments: [{ kind: "image", digest: "a".repeat(64), name: "photo.png", mime: "image/png", width: 1, height: 1, bytes: 68 }],
}];
let cancelCalls = 0;
let cancelInboxCalls = 0;
let cancelInboxError: Error | null = null;
let cancelDiscardedItemIDs: string[] = [];
let interruptCalls = 0;
let interruptError: Error | null = null;
let effortCalls = 0;
let checkpointHistoryCalls = 0;
let historyLoads = 0;
let checkpointLoads = 0;
let turnReplayCalls = 0;
const context: ContextInfo = { used: 0, window: 100, sessionTokens: 0 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };

const desktopStub = installDesktopHostStub(({
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
      ForkTargetsForTab: async () => ({ targets: [], verifiable: false }),
      HistoryForTab: async () => [],
      HistorySliceForTab: async (tabID: string, req: HistorySliceRequest) => {
        historyLoads += 1;
        return historySliceFromMessages(
          tabID,
          backendHistory,
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
      SubmitToTabWithID: async (tabId: string, text: string, submissionId: string) => {
        const messageId = `user-${submissionId}`;
        const createdAt = Date.now();
        backendHistory.push({ role: "user", content: text, messageId, submissionId, createdAt });
        desktopStub.emit("agent:event", { kind: "user_message", tabId, text, messageId, submissionId, createdAt });
      },
      CancelTab: async () => {
        cancelCalls += 1;
        backendRunning = false;
        desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a", status: "cancelled" });
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
        desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a", status: "cancelled" });
        return { discardedItemIds: [...cancelDiscardedItemIDs] };
      },
      InterruptTurnForTab: async () => {
        interruptCalls += 1;
        if (interruptError) throw interruptError;
        backendRunning = false;
        desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a", status: "cancelled" });
      },
    } as Partial<AppBindings> as AppBindings,
  },
}).main.App);

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
  desktopStub.emit("agent:event", { kind: "turn_status", tabId: "tab-a", turnId: "turn-gap", seq: 1, status: "queued" });
  desktopStub.emit("agent:event", { kind: "turn_status", tabId: "tab-a", turnId: "turn-gap", seq: 3, status: "waiting_user" });
  for (let step = 0; step < 20; step += 1) await Promise.resolve();
});
eq(turnReplayCalls, 0, "v2 does not consult the retired ledger sequence space");
eq(controller?.state.pendingPrompt, true, "future event projects only after the missing prefix");
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_status", tabId: "tab-a", turnId: "turn-gap", seq: 3, status: "in_progress" });
  await Promise.resolve();
});
eq(controller?.state.pendingPrompt, false, "ordered Follow revision is authoritative despite legacy sequence values");
const historyLoadsBeforeSettlement = historyLoads;
const historyMutationBeforeSettlement = controller?.state.historyMutation.seq ?? 0;
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a", turnId: "turn-gap", seq: 4, status: "completed" });
  await Promise.resolve();
});
// A terminal turn is folded into the bounded durable window independently of
// cancellation. Let that expected read settle before measuring the later
// cancellation path, whose invariant is still that it schedules no reload.
await act(async () => { await flushPromises(); });
eq(historyLoads, historyLoadsBeforeSettlement, "completion never rebases from history");
ok((controller?.state.historyMutation.seq ?? 0) >= historyMutationBeforeSettlement, "completion retains the installed transcript");
historyLoads = 0;

backendRunning = true;
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_started", tabId: "tab-a" });
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
ok(Object.values(controller?.state.localSubmissions ?? {}).some((submission) => submission.text === "corrected"),
  "resubmission keeps its local echo through completed cancellation cleanup");
eq(historyLoads, 0, "resubmission cannot race a stale cancellation history response");

await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_done", tabId: "tab-a", checkpointTurn: 0 });
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

// Stop is a session-level request: an exact-turn fence rejection (stale or
// replaced turn id) must fall back to the unconditional cancel instead of
// leaving the user with a "Cancel failed" notice and a running turn.
const noticesBefore = controller?.state.items.filter((item) => item.kind === "notice").length ?? 0;
const cancelCallsBefore = cancelCalls;
backendRunning = true;
interruptError = new Error('turn "turn-live" is not the active turn for tab "tab-a"');
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_started", tabId: "tab-a", turnId: "turn-live" });
  await flushPromises();
});
eq(controller?.state.activeTurnId, "turn-live", "turn_started with a turn id records the active turn");
await act(async () => {
  await controller?.cancel();
  await flushPromises();
});
eq(interruptCalls, 1, "exact-turn stop is attempted first");
eq(cancelCalls, cancelCallsBefore + 1, "fence rejection falls back to the unconditional CancelTab");
eq(controller?.state.items.filter((item) => item.kind === "notice").length, noticesBefore, "fence rejection does not surface a Cancel failed notice");
await waitFor("fallback cancel reconciliation", () => controller?.state.running === false);

// An idle backend answers with a stable code; the UI reconciles quietly.
backendRunning = false;
interruptError = new Error("reasonix_error:turn_not_running");
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_started", tabId: "tab-a", turnId: "turn-idle" });
  await flushPromises();
});
await act(async () => {
  await controller?.cancel();
  await flushPromises();
});
eq(interruptCalls, 2, "idle stop still asks the backend once");
eq(cancelCalls, cancelCallsBefore + 1, "idle stop does not retry through CancelTab");
eq(controller?.state.items.filter((item) => item.kind === "notice").length, noticesBefore, "idle stop does not surface a Cancel failed notice");
await waitFor("idle stop reconciliation", () => controller?.state.running === false);

// A transport gap can hide the final event entirely: the resnapshot must
// settle core state while retaining the mounted transcript and never cancel.
backendRunning = true;
await act(async () => {
  desktopStub.emit("agent:event", { kind: "turn_started", tabId: "tab-a", turnId: "turn-gap-final" });
  await flushPromises();
});
const gapCancelCalls = cancelCalls;
const projectedUsers = controller?.state.items.filter(item => item.kind === "user") ?? [];
const gapHistoryLoads = historyLoads;
backendRunning = false;
await act(async () => {
  desktopStub.emit("desktop:resync", { generation: "g-current", reason: "gap", expectedSeq: 2, actualSeq: 4 });
  await flushPromises();
});
await waitFor("event gap runtime snapshot", () => controller?.state.running === false);
eq(cancelCalls, gapCancelCalls, "event gap recovery never replays a state-changing command");
ok(projectedUsers.every(item => controller?.state.items.includes(item)), "event gap recovery retains projected user item identities");
eq(historyLoads, gapHistoryLoads + 1, "transport gap obtains one consistent Follow snapshot");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

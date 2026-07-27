// Run: tsx src/__tests__/send-failed.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { acceptsRuntimeEventEpoch, initialState, reducer, replayPendingPromptsForActiveTab, runtimeReadyForSubmit } from "../lib/useController";
import { continueDelivery } from "../lib/deliveryContinue";
import type { WireEvent } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nsend failure feedback");

eq(runtimeReadyForSubmit({ label: "", ready: false, eventChannel: "", cwd: "", runtime: { phase: "starting", epoch: "e1" } }), false, "starting runtime cannot submit");
eq(runtimeReadyForSubmit({ label: "", ready: false, eventChannel: "", cwd: "", runtime: { phase: "lease_blocked", epoch: "e1" } }), false, "lease-blocked runtime cannot submit");
eq(runtimeReadyForSubmit({ label: "", ready: false, eventChannel: "", cwd: "", runtime: { phase: "failed", epoch: "e1" } }), false, "failed runtime cannot submit");
eq(runtimeReadyForSubmit({ label: "", ready: true, eventChannel: "", cwd: "", runtime: { phase: "ready", epoch: "e1" } }), true, "ready runtime can submit");
eq(acceptsRuntimeEventEpoch("e2", "e1"), false, "old runtime epoch is rejected");
eq(acceptsRuntimeEventEpoch("e2", "e2"), true, "current runtime epoch is accepted");
eq(acceptsRuntimeEventEpoch(undefined, "e1"), true, "first runtime epoch can establish the fence");
eq(acceptsRuntimeEventEpoch("e2", undefined), true, "legacy events remain compatible");

const sent = reducer({ ...initialState }, { type: "user", text: "hello", seq: 0 });
eq(sent.items.length, 1, "submit appends the user bubble immediately");
eq(sent.items[0].kind === "user" && sent.items[0].text, "hello", "bubble carries the submitted text");
eq(sent.running, true, "submit marks the turn running");
eq(sent.pendingUser, "hello", "submit tracks the optimistic bubble");

const hiddenSubmit = reducer({ ...initialState }, { type: "user", text: "display prompt", submitText: "hidden context\ndisplay prompt", seq: 0 });
eq(
  hiddenSubmit.items[0].kind === "user" && hiddenSubmit.items[0].submitText,
  "hidden context\ndisplay prompt",
  "optimistic user bubble preserves submit-only context",
);

const confirmed = reducer(sent, { type: "event", e: { kind: "text", text: "hi" } as WireEvent });
eq(confirmed.items.filter((it) => it.kind === "user").length, 1, "first backend event confirms without duplicating");
eq(confirmed.pendingUser, undefined, "confirmation clears the pending marker");

const memoryCitationMessage = {
  kind: "message",
  memoryCitations: [{ kind: "memory_reference", source: "MEMORY.md", note: "reasonix workflow" }],
} as WireEvent;
const started = reducer(sent, { type: "event", e: { kind: "turn_started" } as WireEvent });
const citationOnlyFinal = reducer(started, { type: "event", e: memoryCitationMessage });
eq(citationOnlyFinal.items.length, 1, "memory citations alone do not leave an empty assistant bubble");
eq(citationOnlyFinal.items.some((it) => it.kind === "assistant"), false, "memory citations alone stay hidden from the transcript");
const textThenCitationFinal = reducer(reducer(started, { type: "event", e: { kind: "text", text: "done" } as WireEvent }), { type: "event", e: memoryCitationMessage });
const citedAssistant = textThenCitationFinal.items.find((it) => it.kind === "assistant");
eq(citedAssistant?.kind === "assistant" && citedAssistant.text, "done", "memory citations preserve existing assistant text");
eq(citedAssistant?.kind === "assistant" && citedAssistant.memoryCitations?.length, 1, "memory citations attach to real assistant content");

const failedState = reducer(sent, { type: "send_failed", error: "Send failed: bridge unavailable" });
const failedBubble = failedState.items.find((it) => it.kind === "user");
eq(failedBubble?.kind === "user" && failedBubble.failed, true, "send_failed marks the bubble failed");
const notice = failedState.items[failedState.items.length - 1];
eq(notice.kind, "notice", "send_failed appends a notice");
eq(notice.kind === "notice" && notice.level, "warn", "the notice is a warning");
eq(failedState.running, false, "send_failed stops the running indicator");
eq(failedState.pendingUser, undefined, "send_failed clears the pending marker");

const readinessStarted = reducer(sent, { type: "event", e: { kind: "turn_started" } as WireEvent });
const readinessState = reducer(readinessStarted, {
  type: "event",
  e: {
    kind: "turn_done",
		outcome: "final_readiness",
		err: "final-answer readiness failed 3 times: missing verification",
		readiness: { attempts: 3, missing: ["verification", "review"] },
  } as WireEvent,
});
const readinessNotice = readinessState.items[readinessState.items.length - 1];
eq(readinessNotice.kind, "notice", "final readiness appends a notice");
eq(readinessNotice.kind === "notice" && readinessNotice.level, "info", "final readiness uses informational severity");
eq(readinessNotice.kind === "notice" && readinessNotice.variant, "delivery", "final readiness uses the delivery status treatment");
eq(readinessNotice.kind === "notice" && readinessNotice.title, "Delivery checks are not complete", "final readiness uses localized product copy");
eq(readinessNotice.kind === "notice" && readinessNotice.detail, "Still needed: verification, change review", "structured requirements produce localized detail");
eq(readinessNotice.kind === "notice" && readinessNotice.action, "continue_delivery", "final readiness offers a recovery action");
const readinessUser = readinessState.items.find((it) => it.kind === "user");
eq(readinessUser?.kind === "user" && Boolean(readinessUser.failed), false, "final readiness does not mark the delivered user message as failed");

const recovering = reducer(readinessState, { type: "user", text: "Continue checks", seq: readinessState.seq, deliveryRecovery: true });
const recovered = reducer(recovering, { type: "event", e: { kind: "turn_done" } as WireEvent });
eq(recovered.items.some((it) => it.kind === "notice" && it.variant === "delivery"), false, "successful explicit recovery removes the stale delivery card");

const ordinaryTurnError = reducer(readinessStarted, {
  type: "event",
  e: { kind: "turn_done", err: "provider failed" } as WireEvent,
});
const ordinaryTurnNotice = ordinaryTurnError.items[ordinaryTurnError.items.length - 1];
eq(ordinaryTurnNotice.kind === "notice" && ordinaryTurnNotice.level, "warn", "ordinary turn errors remain warnings");
eq(ordinaryTurnNotice.kind === "notice" && ordinaryTurnNotice.text, "provider failed", "ordinary turn errors keep their diagnostic text");

const recoveryPaused = reducer(readinessStarted, {
  type: "event",
  e: {
    kind: "turn_done",
    outcome: "recovery_paused",
    err: "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction.",
  } as WireEvent,
});
const recoveryNotice = recoveryPaused.items[recoveryPaused.items.length - 1];
eq(recoveryNotice.kind === "notice" && recoveryNotice.level, "info", "recovery_paused uses informational severity");
eq(recoveryNotice.kind === "notice" && Boolean(recoveryNotice.title), true, "recovery_paused shows a product title");
eq(
  recoveryNotice.kind === "notice" && recoveryNotice.text,
  "Reasonix stopped repeated attempts and kept completed work. Send “Continue” to start a fresh attempt, or add instructions to change direction.",
  "recovery_paused uses the localized product copy",
);
eq(
  recoveryNotice.kind === "notice" && Boolean(recoveryNotice.detail),
  false,
  "recovery_paused does not repeat the backend English fallback as localized detail",
);
const recoveryUser = recoveryPaused.items.find((it) => it.kind === "user");
eq(recoveryUser?.kind === "user" && Boolean(recoveryUser.failed), false, "recovery_paused does not mark the user message as failed");
eq(recoveryPaused.running, false, "recovery_paused frees the composer");

const shellSent = reducer({ ...initialState }, { type: "user", text: "!ls", seq: 0 });
const shellFailed = reducer(shellSent, { type: "send_failed", error: "Command failed: workspace is still starting" });
const shellNotice = shellFailed.items[shellFailed.items.length - 1];
eq(shellNotice.kind, "notice", "rejected shell command appends a visible notice");
eq(shellNotice.kind === "notice" && shellNotice.text.includes("workspace is still starting"), true, "shell rejection notice includes the backend error");

const lateFailure = reducer(confirmed, { type: "send_failed", error: "Send failed: late" });
eq(lateFailure, confirmed, "send_failed after backend confirmation is a no-op");

const beforeMcpReady = { ...initialState };
const mcpReady = reducer(beforeMcpReady, { type: "event", e: { kind: "mcp_surface_ready" } as WireEvent });
eq(mcpReady, beforeMcpReady, "mcp_surface_ready is accepted as a deliberate no-op");
const pendingMcpReady = reducer(sent, { type: "event", e: { kind: "mcp_surface_ready" } as WireEvent });
eq(pendingMcpReady, sent, "mcp_surface_ready does not confirm a pending submit");
const failedAfterMcpReady = reducer(pendingMcpReady, { type: "send_failed", error: "Send failed: bridge unavailable" });
const failedAfterMcpReadyBubble = failedAfterMcpReady.items.find((it) => it.kind === "user");
eq(
  failedAfterMcpReadyBubble?.kind === "user" && failedAfterMcpReadyBubble.failed,
  true,
  "send_failed still marks a pending submit after mcp readiness",
);

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const typesSource = readFileSync(resolve(here, "../lib/types.ts"), "utf8");
const controllerSource = readFileSync(resolve(here, "../lib/useController.ts"), "utf8");
eq(typesSource.includes('"mcp_surface_ready"'), true, "TypeScript EventKind declares mcp_surface_ready");
eq(controllerSource.includes('e.kind === "mcp_surface_ready"'), true, "reducer handles mcp_surface_ready before optimistic confirmation");
eq(
  /state\.approval!\.tool === "exit_plan_mode" && allow\) await applyCollaborationMode\("normal"\);/.test(appSource),
  true,
  "plan approval clears the remembered plan restore intent before execution",
);
eq(
  /onExitPlan=\{async \(\) => \{\s*await applyCollaborationMode\("normal"\);\s*approve\(state\.approval!\.id, false, false, false\);\s*\}\}/.test(appSource),
  true,
  "exit-without-executing switches to Normal before rejecting the pending plan",
);
eq(
  !/exit_plan_mode[\s\S]{0,240}rememberUserIntent:\s*false/.test(appSource),
  true,
  "plan approval must not preserve stale plan restore intent",
);
eq(
  !appSource.includes("rememberUserIntent"),
  true,
  "collaboration mode changes always reconcile the remembered plan restore intent",
);
eq(
  appSource.includes("runtimeTransitionTabsRef.current.has(tabId)"),
  true,
  "runtime profile transitions reject rapid duplicate switches for one tab",
);
eq(
  appSource.includes("delete pending.tokenMode") && appSource.includes("tokenMode: previous"),
  true,
  "failed runtime profile transitions roll back the optimistic token mode",
);
eq(
  appSource.includes("!state.backendActivationPending &&") && appSource.includes("!runtimeTransitioning"),
  true,
  "runtime profile transitions keep submit behind the controller-ready gate",
);
eq(
  /await continueDelivery\(\{[\s\S]{0,240}goal: state\.meta\?\.goal,[\s\S]{0,240}resumeGoal: resumeControllerGoalForTab,/.test(appSource),
  true,
  "delivery recovery routes through continueDelivery with the backend Goal state",
);
eq(
  /await applyGoal\(trimmed\);[\s\S]{0,120}await commitThenSendRef\.current\(sourceTabId/.test(appSource),
  true,
  "the first Goal turn waits for the controller Goal before submitting",
);

const unsent = reducer(sent, { type: "unsend" });
eq(unsent.pendingUser, undefined, "unsend clears the pending marker");
eq(unsent.discardTurn, true, "unsend discards the in-flight turn");

const planApprovalFirst = reducer(
  { ...initialState },
  { type: "event", e: { kind: "approval_request", approval: { id: "plan-1", tool: "exit_plan_mode", subject: "Approve plan" } } as WireEvent },
);
const planTurnDoneAfter = reducer(planApprovalFirst, { type: "event", e: { kind: "turn_done" } as WireEvent });
eq(
  planTurnDoneAfter.approval?.id,
  "plan-1",
  "turn_done preserves out-of-order plan approval",
);
eq(planTurnDoneAfter.running, true, "preserved plan approval keeps the tab running");
eq(planTurnDoneAfter.pendingPrompt, true, "preserved plan approval keeps the prompt gate active");

let replayCalls = 0;
replayPendingPromptsForActiveTab(undefined, () => {
  replayCalls += 1;
  return Promise.resolve();
});
eq(replayCalls, 0, "no active tab does not replay pending prompts");

replayPendingPromptsForActiveTab("tab-a", () => {
  replayCalls += 1;
  return Promise.resolve();
});
eq(replayCalls, 1, "active tab switch replays pending prompts");

replayPendingPromptsForActiveTab("tab-b", () => {
  replayCalls += 1;
  return Promise.reject(new Error("bridge unavailable"));
});
await new Promise((resolve) => setTimeout(resolve, 0));
eq(replayCalls, 2, "replay bridge failures are swallowed by the tab-switch effect");

console.log("\ndelivery recovery continuation");

interface ContinueCalls {
  resumes: string[];
  sends: string[];
}

async function runContinueDelivery(opts: {
  goal: string | undefined;
  resumed?: boolean;
  ready?: boolean;
  tabId?: string | null;
  tabAfterResume?: string;
}): Promise<ContinueCalls> {
  const calls: ContinueCalls = { resumes: [], sends: [] };
  await continueDelivery({
    tabId: opts.tabId === undefined ? "tab-a" : opts.tabId,
    ready: opts.ready ?? true,
    goal: opts.goal,
    activeTabId: () => opts.tabAfterResume ?? "tab-a",
    resumeGoal: (tabId) => {
      calls.resumes.push(tabId);
      return Promise.resolve(opts.resumed ?? true);
    },
    send: (tabId) => {
      calls.sends.push(tabId);
      return Promise.resolve();
    },
  });
  return calls;
}

const noGoal = await runContinueDelivery({ goal: undefined });
eq(noGoal.resumes.length, 0, "delivery recovery without a Goal skips the resume call");
eq(noGoal.sends.join(","), "tab-a", "delivery recovery without a Goal submits the continuation directly");

const blankGoal = await runContinueDelivery({ goal: "   " });
eq(blankGoal.resumes.length, 0, "delivery recovery treats a blank Goal as absent");
eq(blankGoal.sends.join(","), "tab-a", "delivery recovery with a blank Goal still submits the continuation");

const goalResumed = await runContinueDelivery({ goal: "ship it", resumed: true });
eq(goalResumed.resumes.join(","), "tab-a", "delivery recovery with a Goal resumes it first");
eq(goalResumed.sends.join(","), "tab-a", "delivery recovery submits after the Goal resumes");

const goalRefused = await runContinueDelivery({ goal: "ship it", resumed: false });
eq(goalRefused.resumes.join(","), "tab-a", "an unresumable Goal is still offered the resume");
eq(goalRefused.sends.length, 0, "an unresumable (completed) Goal does not submit the continuation");

const tabSwitched = await runContinueDelivery({ goal: "ship it", resumed: true, tabAfterResume: "tab-b" });
eq(tabSwitched.sends.length, 0, "a tab switch during resume drops the continuation");

const notReady = await runContinueDelivery({ goal: undefined, ready: false });
eq(notReady.sends.length, 0, "delivery recovery waits for controller readiness");

const noTab = await runContinueDelivery({ goal: undefined, tabId: null });
eq(noTab.sends.length, 0, "delivery recovery without an active tab is a no-op");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import { useSessionPromptCommands } from "../app-runtime/useSessionPromptCommands";
import type { PromptPorts } from "../app-runtime/sessionPromptExecutor";
import { CommandCancelled } from "../lib/commandOutcome";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
function deferred() { let resolve!: () => void; const promise = new Promise<void>(done => { resolve = done; }); return { promise, resolve }; }
let entered = deferred();
let gate = deferred();
let prompt = "approval-A";
const calls: string[] = [];
const ports: PromptPorts = {
  isPromptCurrentForTab: target => target.tabId === "A" && target.promptId === prompt,
  approveForTab: target => { calls.push(`approve:${target.tabId}`); },
  resolvePlanForTab: target => { calls.push(`resolve:${target.tabId}:${target.promptId}`); },
  resolveRecoveryForTab: target => { calls.push(`recover:${target.tabId}`); },
  answerQuestionForTab: async target => { calls.push(`question:${target.tabId}`); },
  answerMCPForTab: target => { calls.push(`mcp:${target.tabId}`); },
  setCollaborationModeForTab: async tab => { calls.push(`mode:${tab}`); },
  clearGoalForTab: async tab => { calls.push(`clear:${tab}`); entered.resolve(); await gate.promise; },
  setRemoteComposerProfile: async tab => { calls.push(`remote:${tab}`); entered.resolve(); await gate.promise; return [prompt]; },
  patchComposerProfile: tab => { calls.push(`patch:${tab}`); },
  notePlanMode: tab => { calls.push(`remember:${tab}`); },
  drainRemoteApprovals: tab => { calls.push(`drain:${tab}`); },
  rememberRevision: tab => { calls.push(`revision:${tab}`); },
};
let commands!: ReturnType<typeof useSessionPromptCommands>;
function Probe({ tab, generation = "1", remote = false }: { tab: string; generation?: string; remote?: boolean }) {
  const target = { tabId: tab, sessionKey: tab + generation };
  const operations = useSessionOperations({ visible: target, resources: ["A", "B"].map(tabId => ({ tabId, sessionKey: tabId + generation })) });
  commands = useSessionPromptCommands({ target, session: { hostId: "local", sessionId: `session-${tab}` }, sessionGeneration: Number(generation), approval: { id: prompt, tool: "exit_plan_mode", subject: "plan", turnId: `turn-${tab}`, runtimeEpoch: `runtime-${tab}` },
    remote, goal: "fixture", toolApprovalMode: "ask", ports, operations, reportError: error => { throw error; } });
  return null;
}
async function paint(tab: string, generation = "1", remote = false) {
  await act(async () => root.render(<Probe tab={tab} generation={generation} remote={remote} />));
}
function reset() { calls.length = 0; entered = deferred(); gate = deferred(); prompt = "approval-A"; }
try {
  await paint("A");
  let pending = commands.handleApprovalAnswer(true, false, false);
  await entered.promise;
  await paint("B");
  gate.resolve(); await pending;
  assert.deepEqual(calls, ["clear:A", "mode:A", "remember:A", "patch:A", "resolve:A:approval-A"]);

  reset(); await paint("A");
  pending = commands.handleExitPlan(); await entered.promise;
  prompt = "replacement";
  gate.resolve(); await assert.rejects(pending, CommandCancelled);
  assert.deepEqual(calls, ["clear:A"], "replacement prompt revokes the entire continuation, including mode changes");

  reset(); await paint("A");
  pending = commands.handleExitPlan(); await entered.promise;
  await paint("A", "2"); gate.resolve(); await assert.rejects(pending, CommandCancelled);
  assert.deepEqual(calls, ["clear:A"], "same tab with a different session cannot resolve an old approval");

  reset(); await paint("A", "1", true);
  pending = commands.handleExitPlan(); await entered.promise;
  await paint("B", "1", true); await paint("A", "1", true);
  gate.resolve(); await pending;
  assert.deepEqual(calls, ["remote:A", "remember:A", "patch:A", "resolve:A:approval-A"], "ABA never drains the new surface's approvals");

  reset(); await paint("A");
  pending = commands.handleExitPlan(); await entered.promise;
  await commands.handleApprovalAnswer(false, false, false);
  gate.resolve(); await assert.rejects(pending, CommandCancelled);
  assert.deepEqual(calls, ["clear:A", "resolve:A:approval-A"], "new decision supersedes the older mode/approval chain");

  reset(); await paint("A");
  pending = commands.handleExitPlan(); await entered.promise;
  await act(async () => root.unmount());
  gate.resolve(); await assert.rejects(pending, CommandCancelled);
  await assert.rejects(commands.handleRecoveryAnswer("stop"), CommandCancelled);
  assert.deepEqual(calls, ["clear:A"], "unmount revokes the stable entry and every in-flight continuation");
  console.log("session prompts: source, prompt identity, replacement, ABA, supersession and disposal passed");
} finally { dom.window.close(); }

import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import { useSessionPromptCommands } from "../app-runtime/useSessionPromptCommands";
import type { PromptPorts } from "../app-runtime/sessionPromptExecutor";
import { stateOwnsInteraction } from "../lib/interactionOwnership";
import { sessionIdentityStableKey } from "../lib/sessionIdentity";
import { CommandCancelled } from "../lib/commandOutcome";
import { resolvePromptForSession } from "../lib/exactPromptSubmit";
import { sessionObservationDiagnostics } from "../lib/sessionObservationDiagnostics";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const meta = { label: "test", ready: true, eventChannel: "test", cwd: "/workspace",
  session: { hostId: "local", sessionId: "session-A" }, sessionGeneration: 0 };
const approval = { id: "1", tool: "write_file", subject: "temporary render script", kind: "write_access",
  turnId: "turn-A", runtimeEpoch: "runtime-A", generation: 1, permissionRevision: 1,
  write_access: { directories: ["/tmp/render-tools"] } };
const target = { tabId: "A", sessionKey: sessionIdentityStableKey(meta) };
let commands!: ReturnType<typeof useSessionPromptCommands>;
let calls = 0;
let submit: () => void | Promise<void> = () => { calls++; };
const ports: PromptPorts = {
  isPromptCurrentForTab: candidate => stateOwnsInteraction({ meta, approval }, candidate),
  approveForTab: candidate => resolvePromptForSession({
    ResolvePromptForSession: async received => {
      assert.equal(received.sessionGeneration, 0, "fresh binding generation reaches the host unchanged");
      await submit();
    },
    ResolvePromptForTab: async () => { throw new Error("unexpected legacy route"); },
    PendingPromptIdentitiesForTab: async () => [],
  }, candidate, { allow: true }),
  resolvePlanForTab: () => submit(), resolveRecoveryForTab: () => submit(),
  answerQuestionForTab: async () => {}, answerMCPForTab: () => {},
  setCollaborationModeForTab: async () => {}, clearGoalForTab: async () => {},
  setRemoteComposerProfile: async () => [], patchComposerProfile: () => {}, notePlanMode: () => {},
  drainRemoteApprovals: () => {}, rememberRevision: () => {},
};
function Probe({ ready, resourceKey }: { ready: boolean; resourceKey: string }) {
  const operations = useSessionOperations({ visible: target, resources: [{ ...target, sessionKey: resourceKey }] });
  commands = useSessionPromptCommands({ target, session: ready ? meta.session : undefined,
    sessionGeneration: ready ? meta.sessionGeneration : undefined, approval,
    remote: false, goal: "", toolApprovalMode: "workspace-write", ports, operations,
    reportError: error => { throw error; } });
  return null;
}
async function paint(ready = true, resourceKey = target.sessionKey) {
  await act(async () => root.render(<Probe ready={ready} resourceKey={resourceKey} />));
}
try {
  await paint(false);
  await assert.rejects(commands.handleApprovalAnswer(true, false, false), CommandCancelled,
    "missing session binding must not masquerade as an accepted approval");
  assert.equal(calls, 0);
  await paint(true, "replaced-session");
  await assert.rejects(commands.handleApprovalAnswer(true, false, false), CommandCancelled,
    "a cancelled operation must release the card's submission lock");
  assert.equal(calls, 0, "a stale card never authorizes the replacement session");
  await paint();
  await commands.handleApprovalAnswer(true, false, false);
  assert.equal(calls, 1, "retry after the authoritative binding arrives submits once");

  let reject!: (error: Error) => void;
  let entered!: () => void;
  const started = new Promise<void>(resolve => { entered = resolve; });
  const pending = new Promise<void>((_resolve, fail) => { reject = fail; });
  submit = () => { entered(); return pending; };
  let settled = false;
  const result = commands.handleApprovalAnswer(true, false, false);
  const checked = assert.rejects(result, /transport unavailable/);
  void result.then(() => { settled = true; }, () => { settled = true; });
  await started;
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(settled, false, "the card waits for the actual approval transport outcome");
  reject(new Error("transport unavailable"));
  await checked;
  const trace = sessionObservationDiagnostics("session-id:session-A", "A");
  assert.ok(trace.events.some(e => e.action === "prompt-transport" && e.status === "accepted" && e.prompt?.bindingGeneration === 0));
  assert.ok(trace.events.some(e => e.action === "prompt-transport" && e.status === "rejected:unavailable"));
  assert.ok(!JSON.stringify(trace).includes("transport unavailable"), "diagnostics keep closed error classes, not raw errors");
  assert.equal(sessionObservationDiagnostics("session-id:session-A", "B").events.length, 0, "diagnostics stay with the submitting tab");
  console.log("approval outcomes: missing/stale binding, safe retry and transport failure passed");
} finally {
  await act(async () => root.unmount());
  dom.window.close();
}

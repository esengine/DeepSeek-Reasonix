import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import { useComposerModeActions } from "../lib/useComposerModeActions";
import { executeComposerMode, type ComposerModePorts } from "../app-runtime/composerModeOwner";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const effects: string[] = [];
let release!: () => void;
let gate = new Promise<void>(resolve => { release = resolve; });
const resetGate = () => { effects.length = 0; gate = new Promise<void>(resolve => { release = resolve; }); };
const planIntentsRef = { current: {} };
const yoloRestoreRef = { current: {} };
// The hook owns rememberPlan/rememberApproval through these refs; the owner
// still accepts them as ports, exercised directly below.
const ports: Omit<ComposerModePorts, "rememberPlan" | "rememberApproval"> = {
  setMode: async id => { effects.push(`mode:${id}`); },
  setCollaboration: async id => { effects.push(`collaboration:${id}`); },
  setApproval: async id => { effects.push(`approval:${id}`); },
  clearGoal: async id => { effects.push(`clear:${id}`); await gate; },
  setRemote: async id => { effects.push(`remote:${id}`); await gate; return ["approval-A"]; },
  drainRemote: id => { effects.push(`drain:${id}`); },
  patch: id => { effects.push(`patch:${id}`); },
};
let commands!: ReturnType<typeof useComposerModeActions>;
let operations!: ReturnType<typeof useSessionOperations>;
function Probe({ id, remote = false, generation = "" }: { id: string; remote?: boolean; generation?: string }) {
  operations = useSessionOperations({ visible: { tabId: id, sessionKey: id + generation }, resources: ["A", "B"].map(tabId => ({ tabId, sessionKey: tabId + generation })) });
  commands = useComposerModeActions({
    remote, collaborationMode: "goal", toolApprovalMode: "ask", goal: "task",
    target: { tabId: id, sessionKey: id + generation }, operations, ports,
    planIntentsRef, yoloRestoreRef,
    showError: message => effects.push(`error:${message}`),
  });
  return null;
}
async function paint(id: string, remote = false, generation = "") {
  await act(async () => root.render(<Probe id={id} remote={remote} generation={generation} />));
}
try {
  await paint("A");
  const pending = commands.applyCollaborationMode("normal");
  assert.deepEqual(effects, ["clear:A"]);
  await paint("B");
  release();
  await act(async () => { await pending; });
  assert.deepEqual(effects, ["clear:A", "collaboration:A", "patch:A"], "every continuation mutates the captured source, never B");
  assert.equal(planIntentsRef.current["A"], undefined, "normal mode records no plan intent for the source tab");
  resetGate();
  await paint("A", true);
  const remote = commands.applyCollaborationMode("normal");
  await paint("B", true);
  await paint("A", true);
  release();
  await act(async () => { await remote; });
  assert.deepEqual(effects, ["remote:A", "patch:A"], "A→B→A preserves source data but never revives approval-drain UI ownership");

  resetGate();
  await paint("A");
  const replaced = commands.applyCollaborationMode("normal");
  await paint("A", false, ":new");
  release();
  await act(async () => { await replaced; });
  assert.deepEqual(effects, ["clear:A"], "reused tab with a new session identity blocks every stale continuation");

  resetGate();
  await paint("A");
  const rerendered = commands.applyCollaborationMode("normal");
  const stop = await operations({ tabId: "A", sessionKey: "A" }, "stop", "A", async (id, authority) => {
    authority.checkpoint(); effects.push(`stop:${id}`);
  });
  assert.equal(stop.status, "completed", "waiting profile does not block stop");
  await paint("A");
  release();
  await act(async () => { await rerendered; });
  assert.deepEqual(effects, ["clear:A", "stop:A", "collaboration:A", "patch:A"], "ordinary commit does not cancel an in-flight source request");

  resetGate();
  const stale = commands.applyCollaborationMode("normal");
  const releaseStale = release;
  gate = new Promise<void>(resolve => { release = resolve; });
  const latest = commands.applyCollaborationMode("plan");
  releaseStale();
  await act(async () => { await stale; });
  assert.deepEqual(effects, ["clear:A", "clear:A"], "superseded continuation has zero side effects");
  release();
  await act(async () => { await latest; });
  assert.deepEqual(effects, ["clear:A", "clear:A", "collaboration:A", "patch:A"], "old finally cannot release the new request");

  resetGate();
  const disposed = commands.applyCollaborationMode("normal");
  const oldEntry = commands;
  await act(async () => root.unmount());
  release();
  await disposed;
  oldEntry.applyMode("normal");
  assert.deepEqual(effects, ["clear:A"], "unmount synchronously revokes commands and pending continuations");
  const writes: unknown[][] = [];
  const remotePorts: ComposerModePorts = { ...ports,
    rememberPlan: id => { effects.push(`plan:${id}`); },
    rememberApproval: id => { effects.push(`remember:${id}`); },
    setRemote: async (...args) => { writes.push(args); return []; },
    clearGoal: async () => { throw new Error("atomic remote transition cannot use local goal clearing"); },
    setCollaboration: async () => { throw new Error("atomic remote transition cannot use local mode changes"); },
    setMode: () => { throw new Error("remote mode cannot use local mode changes"); },
    setApproval: () => { throw new Error("remote approval cannot use local mode changes"); },
  };
  for (const request of [{ kind: "collaboration", mode: "normal" }, { kind: "approval", mode: "yolo" }] as const) {
    await executeComposerMode({ target: { tabId: "A", sessionKey: "A" }, request,
      remote: true, collaborationMode: "goal", toolApprovalMode: "ask", goal: "task", ports: remotePorts,
    }, { checkpoint() {}, ownsUI: () => true });
  }
  assert.deepEqual(writes, [["A", "normal", "ask", ""], ["A", "goal", "yolo", "task"]], "each remote change sends every axis in one atomic profile transaction");
  console.log("composer source operations: source isolation, ABA, identity replacement, lanes, finally and disposal passed");
} finally {
  if (document.getElementById("root")?.hasChildNodes()) await act(async () => root.unmount());
  dom.window.close();
}

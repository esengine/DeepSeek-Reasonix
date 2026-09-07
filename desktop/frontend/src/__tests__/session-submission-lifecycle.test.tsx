import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionSubmission } from "../lib/useSessionSubmission";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import type { SubmissionPorts, SubmissionResource } from "../app-runtime/sessionSubmissionOwner";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const { createSubmissionPorts } = await import("../app-runtime/desktopSubmissionAdapter");
function deferred() {
  let resolve!: () => void; let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let goalGate: ReturnType<typeof deferred> | undefined, profileGate: ReturnType<typeof deferred> | undefined;
const calls: unknown[][] = [];
const ports: SubmissionPorts = {
  send: async (...args) => { calls.push(["send", ...args]); },
  clearUndo: tab => { calls.push(["undo", tab]); },
  setGoal: async (...args) => { calls.push(["goal", ...args]); await goalGate?.promise; },
  patchGoal: (...args) => { calls.push(["patch", ...args]); },
  profile: async tab => { calls.push(["profile", tab]); await profileGate?.promise; return true; },
};
let commands!: ReturnType<typeof useSessionSubmission>;
function Probe({ tab, gen, draft, readOnly }: { tab: string; gen: number; draft: boolean; readOnly: boolean }) {
  const resources: SubmissionResource[] = ["A", "B"].map(tabId => ({ target: { tabId, sessionKey: tabId + gen },
    ready: true, remote: false, unavailable: readOnly ? "read-only" : "", goalDraft: draft, collaboration: "normal", approval: "ask" }));
  const target = resources.find(source => source.target.tabId === tab)!.target;
  const operations = useSessionOperations({ visible: target, resources: resources.map(source => source.target) });
  commands = useSessionSubmission({ target, resources, operations, ports, missingSource: "missing" });
  return null;
}
const paint = (tab = "A", gen = 1, draft = false, readOnly = false) => act(async () => root.render(<Probe tab={tab} gen={gen} draft={draft} readOnly={readOnly} />));
try {
  const adapterCalls: unknown[][] = [];
  const adapter = createSubmissionPorts({
    send: async (...args) => { adapterCalls.push(["send", ...args]); },
    setGoal: async (...args) => { adapterCalls.push(["set", ...args]); },
    clearGoal: async (...args) => { adapterCalls.push(["clear", ...args]); },
    clearUndo: () => {}, patchGoal: () => {}, profile: async () => true,
  });
  await adapter.setGoal("B", "goal", false); await adapter.setGoal("A", "", false);
  await adapter.send("B", "display", " raw bytes ", undefined, { goal: "goal", collaborationMode: "normal", toolApprovalMode: "ask" });
  assert.deepEqual(adapterCalls, [["set", "B", "goal"], ["clear", "A"], ["send", "B", "display", " raw bytes ", undefined, undefined,
    { goal: "goal", collaborationMode: "normal", toolApprovalMode: "ask" }]], "runtime adapter preserves explicit Controller targets, original-text slot and atomic Goal payload");
  await paint(); profileGate = deferred();
  const first = commands.submit("A", " display ", " provider bytes ");
  await paint("B"); profileGate.resolve(); await first;
  assert.deepEqual(calls, [["profile", "A"], ["undo", "A"], ["send", "A", "display", "provider bytes", undefined, undefined]], "ordinary continuation uses the source resource and preserves established trim semantics");

  calls.length = 0; profileGate = deferred(); await paint();
  const replaced = commands.submit("A", "stale");
  await paint("A", 2); profileGate.resolve(); await replaced;
  assert.deepEqual(calls, [["profile", "A"]], "replacement receives no stale undo invalidation or submit");

  calls.length = 0; profileGate = undefined; goalGate = deferred(); await paint();
  const goal = commands.submit("A", "/goal source goal");
  await paint("A", 2); goalGate.resolve(); await goal;
  assert.deepEqual(calls, [["goal", "A", "source goal", false]], "old Goal completion cannot patch or submit to a replacement");

  calls.length = 0; goalGate = deferred(); await paint();
  const rejected = commands.applyGoal("bad goal");
  goalGate.reject(Error("activation failed")); await assert.rejects(rejected, /activation failed/);
  assert.deepEqual(calls, [["goal", "A", "bad goal", false]], "failed activation leaves UI profile and undo untouched");

  calls.length = 0; goalGate = undefined; await paint("A", 1, true);
  const structured = { display: "skill", input: "/skill input", invocations: [{ name: "skill", kind: "skill" as const, offset: 0 }] };
  await commands.submit("A", " goal text ", " /skill input ", structured);
  assert.deepEqual(calls, [["undo", "A"], ["send", "A", "goal text", "/skill input", structured,
    { goal: "goal text", collaborationMode: "normal", toolApprovalMode: "ask" }], ["patch", "A", "goal text"]], "structured Goal uses one atomic source send and unchanged invocation bytes");
  calls.length = 0;
  await commands.submit("A", " goal text ", " task bytes ");
  assert.equal(calls[1][3], "/goal task bytes", "ordinary first Goal retains its existing prefix");

  calls.length = 0; await paint();
  await commands.submit("A", "/goal pause"); await commands.submit("A", "/goal resume");
  assert.deepEqual(calls.filter(call => call[0] === "goal" || call[0] === "patch"), [], "pause and resume preserve Goal before backend command handling");
  assert.deepEqual(calls.filter(call => call[0] === "send").map(call => call[3]), ["/goal pause", "/goal resume"]);
  calls.length = 0; await commands.applyGoalForTab("B", " target goal "); await commands.applyGoalForTab("B", "");
  assert.deepEqual(calls, [["goal", "B", "target goal", false], ["patch", "B", "target goal"], ["goal", "B", "", false], ["patch", "B", ""]], "activation and clear patch only their explicit source after backend success");

  calls.length = 0; await paint();
  await commands.submit("A", "/goal --deep --research preserve flags");
  assert.deepEqual(calls, [["patch", "A", "preserve flags"], ["undo", "A"],
    ["send", "A", "/goal --deep --research preserve flags", "/goal --deep --research preserve flags", undefined, undefined]], "legacy Goal flags remain backend-visible and do not invoke separate activation");

  calls.length = 0; await paint("A", 1, false, true);
  await assert.rejects(commands.commitThenSend("A", "direct"), /read-only/);
  assert.deepEqual(calls, [], "read-only source preserves undo and sends nothing");
  await paint(); profileGate = deferred();
  const disposed = commands.submit("A", "disposed");
  await act(async () => root.unmount()); profileGate.resolve(); await disposed;
  commands.commitThenSend("A", "after-unmount");
  assert.deepEqual(calls, [["profile", "A"]], "unmount revokes both continuation and committed direct entry");
  console.log("session submission lifecycle: source continuations, Goal atomicity/bytes, replacement, failure, read-only and disposal passed");
} finally { dom.window.close(); }

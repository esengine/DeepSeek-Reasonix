import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { usePendingPlanRevisions } from "../lib/usePendingPlanRevisions";
import { useSessionOperations } from "../app-runtime/useSessionOperations";
import { useSessionSubmission } from "../lib/useSessionSubmission";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
function deferred() {
  let resolve!: () => void; let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const requests: { tab: string; text: string; gate: ReturnType<typeof deferred> }[] = [];
const errors: unknown[] = [];
let remember!: ReturnType<typeof usePendingPlanRevisions>;
function Probe({ tab, gen, running, ready }: { tab: string; gen: number; running: boolean; ready: boolean }) {
  const resources = ["A", "B"].map(tabId => ({ tabId, sessionKey: tabId + gen }));
  const visible = resources.find(target => target.tabId === tab)!;
  const operations = useSessionOperations({ visible, resources });
  const submission = useSessionSubmission({ target: visible, operations, missingSource: "missing",
    resources: resources.map(target => ({ target, remote: false, ready: true, unavailable: "", goalDraft: false,
      collaboration: "normal", approval: "ask" })),
    ports: {
      send: (tab, text) => { const gate = deferred(); requests.push({ tab, text, gate }); return gate.promise; },
      clearUndo: () => {}, setGoal: async () => {}, patchGoal: () => {}, profile: async () => true,
    },
  });
  remember = usePendingPlanRevisions({ visible, resources, running, ready, operations,
    send: submission.sendRevision,
    report: error => { errors.push(error); },
  });
  return null;
}
const paint = (tab = "A", gen = 1, running = false, ready = true) => act(async () => root.render(<Probe tab={tab} gen={gen} running={running} ready={ready} />));
try {
  await paint("A", 1, true); remember("A", "first");
  assert.equal(requests.length, 0, "running turn holds its revision");
  await paint("A", 1, false, false);
  assert.equal(requests.length, 0, "an idle but uncommitted navigation surface cannot submit a revision");
  await paint("B"); assert.equal(requests.length, 0, "B does not submit A's pending revision");
  remember("B", "B revision"); assert.equal(requests[0].tab, "B");
  await paint("A"); assert.deepEqual(requests.map(({ tab, text }) => [tab, text]), [["B", "B revision"], ["A", "first"]]);

  remember("A", "same text"); remember("A", "same text");
  await paint("A", 2); remember("A", "replacement");
  assert.equal(requests.length, 3, "replacement resource can start while the old resource's transport is pending");
  remember("A", "latest");
  await act(async () => requests[1].gate.resolve());
  assert.equal(requests.length, 3, "old finally cannot release the new request or start its queued successor");
  await act(async () => requests[2].gate.resolve());
  assert.equal(requests[3].text, "latest", "matching completion starts exactly the replacement revision");
  await act(async () => requests[3].gate.resolve());
  await paint("A", 2); assert.equal(requests.length, 4, "terminal revisions leave no retried request");
  await act(async () => requests[0].gate.resolve());

  remember("A", "failure"); await paint("B", 2); await paint("A", 2);
  await act(async () => requests[4].gate.reject(Error("old failure")));
  assert.deepEqual(errors, [], "A-B-A does not restore failure UI ownership");
  await paint("B", 2); await paint("A", 2);
  assert.equal(requests[5].text, "failure", "source data survives suppressed old error UI and can be retried on a new activation");
  await act(async () => requests[5].gate.resolve());
  remember("A", "retryable revision");
  await act(async () => requests[6].gate.reject(Error("current failure")));
  assert.equal(errors.length, 1);
  await paint("A", 2); assert.equal(requests.length, 7, "unrelated commits do not loop on a failed revision");
  await paint("B", 2); await paint("A", 2);
  assert.equal(requests[7].text, "retryable revision", "explicit source reactivation retains the existing retryable revision");
  await act(async () => requests[7].gate.resolve());
  remember("A", "disposed"); remember("A", "must not follow");
  await act(async () => root.unmount());
  remember("A", "after unmount");
  await act(async () => requests[8].gate.resolve());
  assert.equal(requests.length, 9, "synchronous disposal releases queue and revokes old follow-on work");
  console.log("pending plan revision lifecycle: source queues, request identity, replacement, ABA and disposal passed");
} finally { dom.window.close(); }

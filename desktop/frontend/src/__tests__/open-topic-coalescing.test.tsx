// Run: tsx src/__tests__/open-topic-coalescing.test.tsx

import { JSDOM } from "jsdom";
import React, { act, useCallback, useRef } from "react";
import { createRoot } from "react-dom/client";
import { enqueueNavigationRequest, enqueueOpenTopicRequest, type PendingOpenTopicRequest } from "../lib/openTopicCoalescing";

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

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

console.log("\nopen topic coalescing");

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

const gates = new Map<string, ReturnType<typeof deferred<void>>>();
const calls: string[] = [];
const updates: string[] = [];
const toasts: string[] = [];
let refreshes = 0;
let openTopic!: (topicId: string) => Promise<void>;

function gateFor(topicId: string) {
  let gate = gates.get(topicId);
  if (!gate) {
    gate = deferred<void>();
    gates.set(topicId, gate);
  }
  return gate;
}

function resetRecords() {
  calls.length = 0;
  updates.length = 0;
  toasts.length = 0;
  refreshes = 0;
}

function Harness() {
  const seqRef = useRef(0);
  const runningRef = useRef(false);
  const pendingRef = useRef<PendingOpenTopicRequest | null>(null);
  const run = useCallback(async (request: PendingOpenTopicRequest) => {
    calls.push(request.topicId);
    try {
      await gateFor(request.topicId).promise;
      if (request.topicId.includes("fail")) throw new Error(request.topicId);
      if (request.seq !== seqRef.current) return;
      updates.push(request.topicId);
      refreshes += 1;
    } catch {
      if (request.seq !== seqRef.current) return;
      toasts.push(request.topicId);
      refreshes += 1;
    }
  }, []);
  openTopic = (topicId: string) => enqueueOpenTopicRequest(
    { seqRef, runningRef, pendingRef },
    { scope: "global", workspaceRoot: "", topicId },
    run,
  );
  return null;
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<Harness />);
  await flushPromises();
});

let bResolved = false;
let cResolved = false;
let dResolved = false;
const pA = openTopic("A");
await act(async () => {
  await flushPromises();
});
const pB = openTopic("B").then(() => { bResolved = true; });
const pC = openTopic("C").then(() => { cResolved = true; });
const pD = openTopic("D").then(() => { dResolved = true; });
await act(async () => {
  await flushPromises();
});

eq(calls.join(","), "A", "only the running request starts while newer requests coalesce");
ok(bResolved && cResolved, "superseded pending requests resolve immediately");
eq(dResolved, false, "latest pending request waits until it runs");

await act(async () => {
  gateFor("A").resolve();
  await pA;
  await flushPromises();
});
eq(calls.join(","), "A,D", "after the running request finishes, only the latest pending request runs");
eq(updates.join(","), "", "stale running request does not update UI");

await act(async () => {
  gateFor("D").resolve();
  await pD;
  await flushPromises();
});
eq(updates.join(","), "D", "latest coalesced request updates UI");
eq(refreshes, 1, "only the latest successful request refreshes metadata");

await pB;
await pC;

resetRecords();
let oldFailRejected = false;
let newFailRejected = false;
const pOldFail = openTopic("old-fail").catch(() => { oldFailRejected = true; });
await act(async () => {
  await flushPromises();
});
const pNewFail = openTopic("new-fail").catch(() => { newFailRejected = true; });
await act(async () => {
  await flushPromises();
  gateFor("old-fail").resolve();
  await pOldFail;
  await flushPromises();
});

eq(calls.join(","), "old-fail,new-fail", "latest pending request starts after an older failing request");
eq(toasts.join(","), "", "stale failure does not show a toast");
eq(oldFailRejected, false, "stale failure promise does not reject");

await act(async () => {
  gateFor("new-fail").resolve();
  await pNewFail;
  await flushPromises();
});
eq(toasts.join(","), "new-fail", "latest failure shows a toast");
eq(newFailRejected, false, "latest failure promise does not reject");
eq(refreshes, 1, "latest failure refreshes metadata once");

await act(async () => {
  root.unmount();
});
dom.window.close();

// Tab-bar switches (App.handleTabChange) route through the same scheduler so
// rapidly clicking between two running sessions can't run switchTab()
// concurrently — concurrent switches race the backend SetActiveTab ordering and
// land events on the wrong session (#5352). This guards serialization (no two
// run()s overlap) + last-click-wins.
{
  const refs = { seqRef: { current: 0 }, runningRef: { current: false }, pendingRef: { current: null as any } };
  let active = 0;
  let maxConcurrent = 0;
  const ran: string[] = [];
  const gates = new Map<string, ReturnType<typeof deferred<void>>>();
  const gate = (id: string) => {
    if (!gates.has(id)) gates.set(id, deferred<void>());
    return gates.get(id)!;
  };
  const switchTab = (req: { tabId: string }) =>
    enqueueNavigationRequest(refs, { tabId: req.tabId }, async (r) => {
      active += 1;
      maxConcurrent = Math.max(maxConcurrent, active);
      await gate(r.tabId).promise;
      ran.push(r.tabId);
      active -= 1;
    });

  const pA = switchTab({ tabId: "A" }); // starts running
  const pB = switchTab({ tabId: "B" }); // coalesced away (superseded while A runs)
  const pC = switchTab({ tabId: "C" }); // latest pending
  gate("A").resolve();
  await pA;
  await flushPromises();
  gate("C").resolve();
  await pC;
  await pB;
  await flushPromises();

  eq(ran.join(","), "A,C", "tab switches serialize: only the running + latest run, middle coalesces");
  eq(maxConcurrent, 1, "no two tab switches run concurrently (no backend SetActiveTab race)");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

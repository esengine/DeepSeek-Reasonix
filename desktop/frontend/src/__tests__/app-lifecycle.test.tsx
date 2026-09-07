import React, { StrictMode, Suspense, startTransition } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import {
  createOperationOwner,
  operationTargetsEqual,
  type OperationIdentity,
  type OperationTarget,
} from "../app-runtime/operationOwner";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useCommittedAsyncCommand } from "../lib/useCommittedAsyncCommand";
import { createSessionSurfaceFence } from "../app-runtime/sessionTarget";

const dom = new JSDOM("<div id='root'></div>");
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const root = createRoot(document.getElementById("root")!);
const never = new Promise<void>(() => undefined);
let command!: (value: number) => number | undefined;
let asyncCommand!: (value: number) => Promise<{ status: string; value?: number; reason?: string }>;
let releaseAsync!: (value: number) => void;
let asyncGate = new Promise<number>((resolve) => { releaseAsync = resolve; });

function CommandProbe({ revision, suspend = false }: { revision: number; suspend?: boolean }) {
  command = useCommittedCommand((value: number) => revision + value);
  if (suspend) throw never;
  return null;
}

async function executeAddition(input: { base: number; gate: Promise<number> }) {
  return input.base + await input.gate;
}
function AsyncCommandProbe({ revision }: { revision: number }) {
  asyncCommand = useCommittedAsyncCommand((value: number) => ({ base: revision + value, gate: asyncGate }), executeAddition);
  return null;
}

const session = (tabId: string, sessionKey: string): OperationTarget => ({
  kind: "session",
  tabId,
  sessionKey,
});

try {
  await act(async () => root.render(
    <StrictMode><Suspense><CommandProbe revision={1} /></Suspense></StrictMode>,
  ));
  const retainedCommand = command;
  assert.equal(retainedCommand(4), 5);

  for (let revision = 2; revision <= 512; revision += 1) {
    await act(async () => root.render(
      <StrictMode><Suspense><CommandProbe revision={revision} /></Suspense></StrictMode>,
    ));
    assert.equal(command, retainedCommand, "the entry point is stable across presentation commits");
    assert.equal(retainedCommand(4), revision + 4, "only committed input owns command dispatch");
  }

  await act(async () => startTransition(() => root.render(
    <StrictMode><Suspense><CommandProbe revision={999} suspend /></Suspense></StrictMode>,
  )));
  assert.equal(retainedCommand(4), 516, "abandoned render input never becomes authoritative");

  await act(async () => root.render(<StrictMode><Suspense><CommandProbe revision={999} suspend /></Suspense></StrictMode>));
  assert.equal(retainedCommand(4), undefined, "a hidden Suspense subtree has no layout-owned command authority");
  await act(async () => root.render(<StrictMode><Suspense><CommandProbe revision={512} /></Suspense></StrictMode>));
  assert.equal(command, retainedCommand, "revealing a suspended surface preserves the stable entry");
  assert.equal(retainedCommand(4), 516, "revealing publishes the current committed input in a fresh lifecycle");

  await act(async () => root.unmount());
  assert.equal(retainedCommand(4), undefined, "a retained command is inert after its owner unmounts");

  const asyncHost = document.createElement("div");
  document.body.append(asyncHost);
  const asyncRoot = createRoot(asyncHost);
  await act(async () => asyncRoot.render(<AsyncCommandProbe revision={1} />));
  const retainedAsyncCommand = asyncCommand;
  const superseded = retainedAsyncCommand(2);
  const releaseSuperseded = releaseAsync;
  asyncGate = new Promise<number>((resolve) => { releaseAsync = resolve; });
  await act(async () => asyncRoot.render(<AsyncCommandProbe revision={10} />));
  const current = retainedAsyncCommand(3);
  releaseSuperseded(4);
  releaseAsync(4);
  assert.deepEqual(await superseded, { status: "cancelled", reason: "superseded" });
  assert.deepEqual(await current, { status: "completed", value: 17 });
  await act(async () => asyncRoot.unmount());
  assert.deepEqual(await retainedAsyncCommand(1), { status: "cancelled", reason: "disposed" });
  asyncHost.remove();

  const owner = createOperationOwner();
  const ownerEpoch = owner.mount();
  const a = session("tab-a", "session-a:1");
  const b = session("tab-b", "session-b:1");

  const firstA = owner.begin(a, 10);
  assert.equal(owner.owns(firstA), true);
  const firstB = owner.begin(b, 11);
  assert.equal(owner.owns(firstA), false, "new navigation supersedes the prior UI operation");
  assert.equal(owner.owns(firstB), true);

  const secondA = owner.begin(a, 12);
  assert.equal(owner.owns(firstB), false);
  assert.equal(owner.owns(firstA), false, "A → B → A does not revive the first A operation");
  assert.equal(owner.owns(secondA), true);

  const thirdA = owner.begin(a, 13);
  assert.equal(owner.finish(secondA), false, "an old finally cannot clear the replacement request");
  assert.equal(owner.owns(thirdA), true);
  assert.equal(owner.finish(thirdA), true);
  assert.equal(owner.activeCount, 0);

  const pending = owner.begin(a, 14);
  owner.unmount(ownerEpoch);
  assert.equal(owner.owns(pending), false, "disposed owner rejects stale async continuations");
  assert.equal(owner.activeCount, 0, "disposed owner releases every operation input");

  const remountedEpoch = owner.mount();
  const remounted = owner.begin(a, 15);
  assert.notEqual(remounted.ownerEpoch, pending.ownerEpoch, "StrictMode remount receives a new epoch");
  assert.equal(owner.owns(remounted), true);
  owner.unmount(remountedEpoch);

  const sameTarget: OperationTarget = { kind: "session", tabId: "tab-a", sessionKey: "session-a:1" };
  assert.equal(operationTargetsEqual(a, sameTarget), true);
  assert.equal(operationTargetsEqual(a, session("tab-a", "session-a:2")), false);
  assert.equal(operationTargetsEqual(a, b), false);

  const surfaceFence = createSessionSurfaceFence();
  const surfaceA1 = surfaceFence.commit("tab-a", "session-a:1")!;
  surfaceFence.commit("tab-b", "session-b:1");
  const surfaceA2 = surfaceFence.commit("tab-a", "session-a:1")!;
  assert.equal(surfaceFence.owns(surfaceA1), false, "A → B → A cannot reacquire old UI ownership");
  assert.equal(surfaceFence.owns(surfaceA2), true, "the latest committed A surface owns UI continuation");
  surfaceFence.dispose();
  assert.equal(surfaceFence.owns(surfaceA2), false, "surface disposal invalidates every retained operation");

  const identities = new Set<OperationIdentity>([firstA, firstB, secondA, thirdA, pending, remounted]);
  assert.equal(identities.size, 6, "every operation has a distinct identity object");
  console.log("PASS App committed commands and source-bound operation ownership are lifecycle safe");
} finally {
  if (document.getElementById("root")?.hasChildNodes()) await act(async () => root.unmount());
  dom.window.close();
}

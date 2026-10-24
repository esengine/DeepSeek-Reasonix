import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { installRemoteSurfaceDom } from "./helpers/remoteSurfaceDom";
import { installDesktopHostStub } from "./desktopHostStub";
import { meta, tabMeta } from "./helpers/sessionSwitchFixtures";
import { useController } from "../lib/useController";
import type { FollowRequest, TranscriptFollowResponse } from "../generated/desktopContract.generated";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
const dom = installRemoteSurfaceDom();
let current = "first";
let epoch = "first-epoch";
const reads: string[] = [];
let opened = deferred<void>();
let releaseOpen = deferred<void>();
let baseline = deferred<void>();
let bindingCommitted = true;
let controller!: ReturnType<typeof useController>;
const currentMeta = () => meta({ session: { hostId: "local", sessionId: current }, sessionPath: `session-id:${current}`,
  sessionGeneration: 1, runtime: { phase: "ready", epoch } });
const stub = installDesktopHostStub({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => [tabMeta({ ...currentMeta(), active: true })],
  MetaForTab: async () => currentMeta(),
  ReplayPendingPrompts: async () => {}, ReplayPendingPromptsForTab: async () => {},
  ContextUsageForTab: async () => ({ used: 0, window: 100, sessionTokens: 0 }),
  EffortForTab: async () => ({ supported: false, current: "auto", levels: [] }),
  BalanceForTab: async () => ({ available: false, display: "" }), JobsForTab: async () => [],
  CheckpointsForTab: async () => [], ForkTargetsForTab: async () => ({ targets: [], verifiable: false }),
  OpenSession: async (ref: { sessionId: string }) => {
    bindingCommitted = false;
    current = ref.sessionId; epoch = `${current}-epoch`;
    stub.emit("runtime:rebuilt", "tab-a", epoch);
    stub.emit("runtime:rebuilt", "tab-a", epoch);
    stub.emit("agent:ready", "tab-a");
    opened.resolve();
    await releaseOpen.promise;
    bindingCommitted = true;
  },
  TranscriptFollowForTab: async (_tab: string, request: FollowRequest): Promise<TranscriptFollowResponse> => {
    if (request.close) return { protocolVersion: 2, subscription: request.subscription, changes: [], resetRequired: false };
    if (request.subscription) return new Promise(() => {});
    const id = current, boundEpoch = epoch;
    reads.push(id);
    if (!bindingCommitted) throw new Error("runtime changed while following transcript");
    if (id !== "first") await baseline.promise;
    return { protocolVersion: 2, subscription: `${id}-${reads.length}`, changes: [], resetRequired: false,
      snapshot: { protocolVersion: 1, snapshotId: id,
        identity: { sessionId: id, runtimeEpoch: boundEpoch, rewriteEpoch: 0, headId: "" },
        projectionRevision: 1, coveredThroughSeq: 1, durableSeq: 1, records: [], activeRecords: [], activeAttempts: [],
        runtime: { status: "completed", pendingEvents: [], samplingCount: 0, toolCount: 0 },
        before: 0, hasOlder: false, totalRecords: 0, totalTurns: 0, stale: false },
      history: { status: "ready", snapshotSequence: 1, coverageSequence: 1, generation: id,
        totalTurns: 0, hasOlder: false, hasNewer: false, messages: [] } };
  },
});
const root = createRoot(document.getElementById("root")!);
function Probe() { controller = useController(); return null; }
async function drain() { await act(async () => { for (let i = 0; i < 40; i++) await new Promise(resolve => setImmediate(resolve)); }); }
try {
  await act(async () => root.render(<Probe />));
  await drain();
  assert.ok(reads.includes("first"), "startup is hydrated");
  reads.length = 0;
  let navigation!: Promise<void>;
  await act(async () => {
    const intent = controller.noteNavigationIntent();
    navigation = controller.openCanonicalSession({ hostId: "local", sessionId: "second" }, intent);
    await opened.promise;
  });
  await drain();
  act(() => releaseOpen.resolve());
  await drain();
  await act(async () => { baseline.resolve(); await navigation; });
  await drain();
  assert.equal(reads.filter(id => id === "second").length, 1,
    "ready/rebuilt notifications and explicit navigation share one target baseline");
  assert.equal(controller.state.meta?.session?.sessionId, "second");
  assert.equal(controller.state.hydrateError, undefined);
  await act(async () => {
    stub.emit("runtime:rebuilt", "tab-a", epoch);
    stub.emit("agent:ready", "tab-a");
  });
  await drain();
  assert.equal(reads.length, 1, "late notifications for the installed runtime do not reload its baseline");
  epoch = "second-replacement-epoch";
  await act(async () => stub.emit("runtime:rebuilt", "tab-a", epoch));
  await drain();
  assert.equal(reads.length, 2, "an actual runtime replacement still establishes a new subscription");

  const slowOpened = deferred<void>(), slowReply = deferred<void>();
  const lastOpened = deferred<void>(), lastReply = deferred<void>();
  stub.commands.OpenSession = async (ref: { sessionId: string }) => {
    if (ref.sessionId === "superseded") {
      slowOpened.resolve(); await slowReply.promise; return;
    }
    current = ref.sessionId; epoch = `${current}-epoch`; bindingCommitted = false;
    stub.emit("runtime:rebuilt", "tab-a", epoch);
    stub.emit("agent:ready", "tab-a");
    lastOpened.resolve(); await lastReply.promise; bindingCommitted = true;
  };
  let slow!: Promise<void>, last!: Promise<void>;
  await act(async () => {
    slow = controller.openCanonicalSession({ hostId: "local", sessionId: "superseded" }, controller.noteNavigationIntent());
    await slowOpened.promise;
  });
  await act(async () => {
    last = controller.openCanonicalSession({ hostId: "local", sessionId: "last" }, controller.noteNavigationIntent());
    await lastOpened.promise;
  });
  await act(async () => { slowReply.resolve(); await slow; stub.emit("agent:ready", "tab-a"); });
  await drain();
  assert.equal(reads.filter(id => id === "last").length, 0, "stale finally cannot release the newer navigation's hydration ownership");
  await act(async () => { lastReply.resolve(); await last; });
  await drain();
  assert.equal(reads.filter(id => id === "last").length, 1);
  assert.equal(controller.state.meta?.session?.sessionId, "last", "last canonical selection wins");

  stub.commands.OpenSession = async () => { throw new Error("session not found"); };
  await act(async () => {
    await assert.rejects(controller.openCanonicalSession({ hostId: "local", sessionId: "missing" }, controller.noteNavigationIntent()), /session not found/);
  });
  epoch = "after-failed-open";
  const beforeRecovery = reads.length;
  await act(async () => { stub.emit("runtime:rebuilt", "tab-a", epoch); });
  await drain();
  assert.equal(reads.length, beforeRecovery + 1, "failed navigation releases ownership so later runtime recovery works");
  assert.equal(controller.state.meta?.session?.sessionId, "last", "failed target never replaces the source identity");
} finally {
  baseline.resolve(); releaseOpen.resolve();
  await act(async () => root.unmount());
  stub.uninstall(); dom.window.close();
}
console.log("PASS canonical navigation performs one target baseline");

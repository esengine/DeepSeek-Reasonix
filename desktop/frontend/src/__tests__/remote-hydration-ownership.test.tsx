import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { installRemoteSurfaceDom } from "./helpers/remoteSurfaceDom";
import { installDesktopHostStub } from "./desktopHostStub";
import type { FollowRequest, TranscriptFollowResponse } from "../generated/desktopContract.generated";

const dom = installRemoteSurfaceDom();
let reads = 0, revision = 1;
let failNext = false;
const pending: Array<(response: TranscriptFollowResponse) => void> = [];
const status = { running: false, label: "Model", plan: false, toolApprovalMode: "ask", goal: "", takenOver: false };
const stub = installDesktopHostStub({
  RemoteTabHydrationState: async () => ({ state: "ready", hydrationRevision: revision }),
  RemoteTabStatus: async () => status,
  RemoteTabMetadata: async () => ({ status }),
  RemoteTabSnapshot: async () => ({ status, history: [] }),
  ForkTargetsRemoteTab: async () => ({ targets: [], verifiable: false }),
  RemoteTranscriptFollowForTab: async (_id: string, request: FollowRequest): Promise<TranscriptFollowResponse> => {
    if (request.close) return { protocolVersion: 2, subscription: request.subscription ?? "", changes: [], resetRequired: false };
    if (request.subscription) return new Promise(() => {});
    reads++;
    if (failNext) { failNext = false; throw new Error("synthetic baseline failure"); }
    return new Promise(resolve => pending.push(resolve));
  },
});
const { useRemoteSession } = await import("../lib/useRemoteSession");
const { setTranscriptBindingIdentity } = await import("../lib/canonicalTranscriptBackend");
setTranscriptBindingIdentity(() => "remote");
let probe!: ReturnType<typeof useRemoteSession>;
function Probe() { probe = useRemoteSession("remote-a", "ready", "session-a"); return null; }
const root = createRoot(document.getElementById("root")!);
async function drain() { await act(async () => { for (let i = 0; i < 40; i++) await new Promise(resolve => setImmediate(resolve)); }); }
const ready = (value: number | undefined) => stub.emit("remote-tab:remote-a:state", { state: "ready", hydrationRevision: value });
const response = (id: string): TranscriptFollowResponse => ({ protocolVersion: 2, subscription: id, changes: [], resetRequired: false,
  snapshot: { protocolVersion: 1, snapshotId: id, identity: { sessionId: id, runtimeEpoch: id, rewriteEpoch: 0, headId: "" },
    projectionRevision: 1, coveredThroughSeq: 1, durableSeq: 1, records: [], activeRecords: [], activeAttempts: [],
    runtime: { status: "completed", pendingEvents: [], samplingCount: 0, toolCount: 0 }, before: 0, hasOlder: false,
    totalRecords: 0, totalTurns: 0, stale: false },
  history: { status: "ready", snapshotSequence: 1, coverageSequence: 1, generation: id, totalTurns: 0,
    hasOlder: false, hasNewer: false, messages: [] } });
try {
  await act(async () => root.render(<Probe />));
  await drain();
  assert.equal(reads, 1);
  await act(async () => { ready(1); ready(1); });
  await drain();
  assert.equal(reads, 1, "mount and duplicate ready notifications share one pending baseline");
  await act(async () => pending.shift()!(response("first")));
  await drain();
  assert.equal(probe.hydrated, true);
  await act(async () => ready(1));
  await drain();
  assert.equal(reads, 1, "late duplicate does not replace an installed subscription");
  await act(async () => { revision = 2; ready(2); });
  await drain();
  assert.equal(reads, 2, "a real ready-to-ready reset starts a new baseline");
  const stale = pending.shift()!;
  await act(async () => { revision = 3; ready(3); ready(2); ready(3); });
  await drain();
  assert.equal(reads, 3, "a newer reset supersedes the old request without accepting replayed barriers");
  await act(async () => stale(response("stale")));
  await drain();
  await act(async () => pending.shift()!(response("latest")));
  await drain();
  assert.equal(probe.transcript.transcriptSessionId, "latest");
  await act(async () => ready(undefined));
  await drain();
  assert.equal(reads, 4, "older hosts without a revision retain explicit ready reset semantics");
  await act(async () => pending.shift()!(response("legacy")));
  await drain();
  await act(async () => { failNext = true; ready(4); });
  await drain();
  assert.equal(reads, 5);
  assert.match(probe.error, /synthetic baseline failure/);
  await act(async () => ready(4));
  await drain();
  assert.equal(reads, 6, "failure releases ownership so the same barrier can retry");
  await act(async () => pending.shift()!(response("recovered")));
  await drain();
  assert.equal(probe.error, "");
  await act(async () => ready(5));
  await drain();
  const disconnected = pending.shift()!;
  await act(async () => stub.emit("remote-tab:remote-a:state", { state: "disconnected", hydrationRevision: 6 }));
  await act(async () => disconnected(response("obsolete")));
  await drain();
  assert.equal(probe.state, "disconnected", "late success cannot resurrect a disconnected surface");
  assert.equal(probe.transcript.transcriptSessionId, "recovered");
  await act(async () => { ready(5); ready(7); });
  await drain();
  assert.equal(reads, 8);
  await act(async () => pending.shift()!(response("reconnected")));
  await drain();
  assert.equal(probe.transcript.transcriptSessionId, "reconnected");
  console.log("PASS remote hydration owns one baseline per authoritative ready revision");
} finally {
  await act(async () => root.unmount());
  dom.window.close();
}

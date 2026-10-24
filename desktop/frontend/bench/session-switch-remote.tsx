import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { installDesktopHostStub } from "../src/__tests__/desktopHostStub";
import { useRemoteSession } from "../src/lib/useRemoteSession";
import type { TranscriptFollowResponse } from "../src/generated/desktopContract.generated";

let revision = 1, reads = 0;
let publishReads = (_reads: number) => {};
const pending: Array<{ revision: number; resolve: (value: TranscriptFollowResponse) => void }> = [];
const status = { running: false, label: "Model", plan: false, toolApprovalMode: "ask", goal: "", takenOver: false };
const stub = installDesktopHostStub({
  RemoteTabHydrationState: async () => ({ state: "ready", hydrationRevision: revision }),
  RemoteTabStatus: async () => status, RemoteTabMetadata: async () => ({ status }),
  RemoteTabSnapshot: async () => ({ status, history: [] }),
  ForkTargetsRemoteTab: async () => ({ targets: [], verifiable: false }),
  RemoteTranscriptFollowForTab: async (_id, request) => {
    if (request.close) return { protocolVersion: 2, subscription: request.subscription ?? "", changes: [], resetRequired: false };
    if (request.subscription) return new Promise(() => {});
    publishReads(++reads);
    return new Promise(resolve => pending.push({ revision, resolve }));
  },
});
function deliver(entry: typeof pending[number] | undefined) {
  if (!entry) return;
  const id = `session-${entry.revision}`;
  entry.resolve({ protocolVersion: 2, subscription: id, changes: [], resetRequired: false,
    snapshot: { protocolVersion: 1, snapshotId: id, identity: { sessionId: id, runtimeEpoch: id, rewriteEpoch: 0, headId: "" },
      projectionRevision: 1, coveredThroughSeq: 1, durableSeq: 1, records: [], activeRecords: [], activeAttempts: [],
      runtime: { status: "completed", pendingEvents: [], samplingCount: 0, toolCount: 0 }, before: 0, hasOlder: false,
      totalRecords: 0, totalTurns: 0, stale: false },
    history: { status: "ready", snapshotSequence: 1, coverageSequence: 1, generation: id, totalTurns: 0,
      hasOlder: false, hasNewer: false, messages: [] } });
}
function Fixture() {
  const session = useRemoteSession("remote-browser", "ready", "remote-session");
  const [count, setCount] = useState(0);
  publishReads = setCount;
  const ready = () => stub.emit("remote-tab:remote-browser:state", { state: "ready", hydrationRevision: revision });
  return <main style={{ fontFamily: "sans-serif", padding: 40 }}>
    <h1>Remote session hydration</h1>
    <button onClick={ready}>Replay ready</button>
    <button onClick={() => { revision++; ready(); }}>New generation</button>
    <button onClick={() => deliver(pending.pop())}>Deliver latest</button>
    <button onClick={() => deliver(pending.shift())}>Deliver oldest</button>
    <output data-reads={count} data-hydrated={session.hydrated} data-session={session.transcript.transcriptSessionId}>
      {JSON.stringify({ reads: count, hydrated: session.hydrated, session: session.transcript.transcriptSessionId })}
    </output>
  </main>;
}
createRoot(document.getElementById("root")!).render(<Fixture />);

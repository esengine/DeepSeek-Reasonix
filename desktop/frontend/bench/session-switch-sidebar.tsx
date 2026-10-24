import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { useController } from "../src/lib/useController";
import { projectSessionRowKey } from "../src/lib/projectSessionIdentity";
import { mergeProjectTopicPage, projectTreeWithoutTopics } from "../src/lib/projectTreeTopic";
import { createSessionLifecycleFences } from "../src/lib/sessionLifecycleFences";
import { installDesktopHostStub } from "../src/__tests__/desktopHostStub";
import { meta, tabMeta } from "../src/__tests__/helpers/sessionSwitchFixtures";
import type { ProjectNode } from "../src/lib/types";
import type { FollowRequest, TranscriptFollowResponse } from "../src/generated/desktopContract.generated";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>(done => { resolve = done; });
  return { promise, resolve };
}
let current = "initial", epoch = "initial-epoch", committed = true;
let releaseOpen = deferred(), releaseBaseline = deferred();
let reads = 0, premature = 0;
const metadata = () => meta({ session: { hostId: "local", sessionId: current }, sessionPath: `session-id:${current}`,
  sessionGeneration: 1, runtime: { phase: "ready", epoch } });
const stub = installDesktopHostStub({
  RegisterNavigationIntent: async () => {},
  ListTabs: async () => [tabMeta({ ...metadata(), active: true })], MetaForTab: async () => metadata(),
  ReplayPendingPrompts: async () => {}, ReplayPendingPromptsForTab: async () => {},
  ContextUsageForTab: async () => ({ used: 0, window: 100, sessionTokens: 0 }),
  EffortForTab: async () => ({ supported: false, current: "auto", levels: [] }),
  BalanceForTab: async () => ({ available: false, display: "" }), JobsForTab: async () => [],
  CheckpointsForTab: async () => [], ForkTargetsForTab: async () => ({ targets: [], verifiable: false }),
  OpenSession: async (ref: { sessionId: string }) => {
    current = ref.sessionId; epoch = `${current}-epoch`; committed = false;
    stub.emit("runtime:rebuilt", "tab-a", epoch);
    stub.emit("runtime:rebuilt", "tab-a", epoch);
    stub.emit("agent:ready", "tab-a");
    await releaseOpen.promise;
    committed = true;
  },
  TranscriptFollowForTab: async (_tab: string, request: FollowRequest): Promise<TranscriptFollowResponse> => {
    if (request.close) return { protocolVersion: 2, subscription: request.subscription, changes: [], resetRequired: false };
    if (request.subscription) return new Promise(() => {});
    const id = current;
    if (id !== "initial") reads++;
    if (!committed) { premature++; throw new Error("runtime changed while following transcript"); }
    if (id !== "initial") await releaseBaseline.promise;
    return { protocolVersion: 2, subscription: `${id}-${reads}`, changes: [], resetRequired: false,
      snapshot: { protocolVersion: 1, snapshotId: id, identity: { sessionId: id, runtimeEpoch: epoch, rewriteEpoch: 0, headId: "" },
        projectionRevision: 1, coveredThroughSeq: 1, durableSeq: 1, records: [], activeRecords: [], activeAttempts: [],
        runtime: { status: "completed", pendingEvents: [], samplingCount: 0, toolCount: 0 },
        before: 0, hasOlder: false, totalRecords: 0, totalTurns: 0, stale: false },
      history: { status: "ready", snapshotSequence: 1, coverageSequence: 1, generation: id, totalTurns: 0,
        hasOlder: false, hasNewer: false, messages: [] } };
  },
});
const source: ProjectNode = { key: "legacy", kind: "global_topic", label: "Imported history",
  source: { hostId: "local", sourceKey: "browser-source", path: "/history.jsonl" } };
const original: ProjectNode = { key: "original", kind: "global_topic", label: "Imported history",
  session: { hostId: "local", sessionId: "original" }, identityAliases: ["source\0local\0browser-source"] };
const reviewed: ProjectNode = { ...original, key: "reviewed", session: { hostId: "local", sessionId: "reviewed" } };
const other: ProjectNode = { key: "other", kind: "global_topic", label: "Other session", session: { hostId: "local", sessionId: "other" } };
const fences = createSessionLifecycleFences();

function Fixture() {
  const controller = useController();
  const [cycle, setCycle] = useState(-1);
  const [status, setStatus] = useState("idle");
  const [result, setResult] = useState({ reads: 0, premature: 0, elapsed: 0 });
  const pages = cycle < 0 ? [source] : cycle % 2
    ? mergeProjectTopicPage([other, reviewed], [original], true)
    : mergeProjectTopicPage([original], [reviewed], true);
  const rows = projectTreeWithoutTopics(pages, fences.keys());
  return <main style={{ fontFamily: "system-ui", padding: 32 }}>
    <h1>Session navigation and sidebar regression</h1>
    <p>Shared-source sessions remain separate through repeated catalog refreshes.</p>
    <button onClick={() => setCycle(value => value + 1)}>Refresh list</button>
    <button onClick={() => { fences.archive(original, { lifecycleGeneration: 1, operationId: "archive-original" }); setCycle(value => value + 2); }}>Archive original</button>
    <button onClick={() => { fences.observeDirectory([{ ...reviewed, lifecycleGeneration: 99 }]); setCycle(value => value + 2); }}>Refresh sibling</button>
    <button onClick={() => { fences.observeDirectory([{ ...original, lifecycleGeneration: 2 }]); setCycle(value => value + 2); }}>Restore original</button>
    <ul>{rows.map(row => <li key={projectSessionRowKey(row)} data-session={row.session?.sessionId ?? "source"}>{row.label}</li>)}</ul>
    <button disabled={status === "opening" || !controller.state.meta?.ready} onClick={() => {
      const startedAt = performance.now();
      reads = 0; premature = 0; releaseOpen = deferred(); releaseBaseline = deferred(); setStatus("opening");
      const intent = controller.noteNavigationIntent();
      void controller.openCanonicalSession({ hostId: "local", sessionId: `target-${intent}` }, intent).then(() => {
        setResult({ reads, premature, elapsed: performance.now() - startedAt }); setStatus("ready");
      }, () => setStatus("failed"));
    }}>Open session</button>
    <button onClick={() => releaseOpen.resolve()}>Commit binding</button>
    <button onClick={() => releaseBaseline.resolve()}>Deliver baseline</button>
    <button onClick={() => {
      stub.emit("runtime:rebuilt", "tab-a", epoch); stub.emit("agent:ready", "tab-a");
      setResult(value => ({ ...value, reads, premature }));
    }}>Replay notifications</button>
    <output data-status={status} data-reads={reads} data-premature={premature} data-cycle={cycle}>
      {JSON.stringify({ status, ...result })}
    </output>
  </main>;
}
createRoot(document.getElementById("root")!).render(<Fixture />);

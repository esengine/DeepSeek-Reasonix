import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { RemoteSessionSurface } from "../../components/RemoteSessionSurface";
import { LocaleProvider } from "../../lib/i18n";
import { useRemoteSession, type RemoteSessionApi } from "../../lib/useRemoteSession";
import type { AppBindings } from "../../lib/bridge";
import type { TabMeta } from "../../lib/types";

export async function runRemoteRuntimeCases({ commands, emitRemote: __emitMockRemoteTab, remoteTab, ok, tape, flush, setSnapshotHistory }: {
  commands: AppBindings;
  emitRemote: (tabId: string, channel: "state" | "event", payload: unknown) => void;
  remoteTab: TabMeta;
  ok: (value: boolean, label: string) => void;
  tape: string[];
  flush: () => Promise<void>;
  setSnapshotHistory: (history: unknown[]) => void;
}) {
  // A lost turn_done must settle the actual shared Transcript from runtime
  // evidence, while a late history response must not erase the next turn.
  const { runtimeStateStore } = await import("../../lib/runtimeStateStore");
  let runtimeProbe: RemoteSessionApi;
  function RuntimeProbe() {
    runtimeProbe = useRemoteSession("tab-runtime", "ready", "/runtime-session");
    return <RemoteSessionSurface tab={{ ...remoteTab, id: "tab-runtime" }} session={runtimeProbe} />;
  }
  const runtimeNode = document.createElement("div");
  document.body.append(runtimeNode);
  const runtimeRoot = createRoot(runtimeNode);
  await act(async () => { runtimeRoot.render(<LocaleProvider><RuntimeProbe /></LocaleProvider>); await flush(); });
  let runtimeRevision = 0;
  async function publishRuntime(phase: "idle" | "executing" | "finishing", turnId = "lost-turn", freshness: "synced" | "unknown" = "synced", sessionPath = "/runtime-session") {
    await act(async () => {
      runtimeStateStore.commit({ epoch: "runtime-app", revision: ++runtimeRevision, topics: [], sessions: [{
        tabId: "tab-runtime", scope: "project", workspaceRoot: "~/app", topicId: "", sessionPath,
        sessionGeneration: 1, open: true, remote: true, freshness,
        state: { schemaVersion: 1, runtimeEpoch: "runtime-controller", revision: runtimeRevision,
          phase, running: phase !== "idle", turnId, turnStatus: phase === "idle" ? "completed" : "in_progress",
          turnEventSeq: runtimeRevision, pendingPrompt: false, cancelRequested: false,
          cancellable: phase === "executing", backgroundJobs: 0, activity: "" },
      }] });
      await flush();
    });
  }
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "turn_started", turnId: "lost-turn" });
    __emitMockRemoteTab("tab-runtime", "event", { kind: "text", text: "partial runtime answer" });
    await flush();
  });
  const beforeRuntimeHistory = tape.filter(entry => entry === "snapshot:tab-runtime").length;
  await publishRuntime("idle", "lost-turn", "unknown");
  ok(runtimeProbe!.transcript.running, "unknown runtime does not settle a stream");
  await publishRuntime("finishing");
  ok(runtimeProbe!.transcript.running, "finishing does not settle the transcript before completion");
  await publishRuntime("idle", "previous-turn");
  ok(runtimeProbe!.transcript.running, "idle evidence for a previous turn cannot settle the current turn");
  await publishRuntime("idle", "lost-turn", "synced", "/other-session");
  ok(runtimeProbe!.transcript.running, "another selected session cannot settle the current transcript");
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "retrying", retryAttempt: 2, retryMax: 4 });
    await flush();
  });
  ok(runtimeProbe!.transcript.retry !== undefined, "lost-completion fixture includes an active retry");
  setSnapshotHistory([{ role: "assistant", content: "complete durable runtime answer" }]);
  await publishRuntime("idle");
  await act(async () => flush());
  ok(!runtimeProbe!.running && !runtimeProbe!.transcript.running && !runtimeProbe!.transcript.turnActive
    && runtimeProbe!.transcript.live === undefined && runtimeProbe!.transcript.retry === undefined,
    "trusted idle settles transcript, live stream, and turn activity together");
  ok(runtimeNode.textContent?.includes("complete durable runtime answer") === true
    && !runtimeNode.querySelector('[data-transcript-block-phase="active"]'),
    "lost completion restores durable content and removes the actual active DOM state");
  await publishRuntime("idle");
  ok(tape.filter(entry => entry === "snapshot:tab-runtime").length === beforeRuntimeHistory + 1,
    "ordinary idle revisions do not repeatedly reload history");
  await act(async () => { await runtimeProbe!.submit("next optimistic turn"); await flush(); });
  await publishRuntime("idle");
  ok(runtimeProbe!.transcript.running, "a newer idle revision of the old turn cannot settle an optimistic submission");
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "turn_started", turnId: "next-turn" });
    __emitMockRemoteTab("tab-runtime", "event", { kind: "text", text: "next live answer" });
    await flush();
  });
  await publishRuntime("idle");
  ok(runtimeProbe!.transcript.running, "old idle remains fenced after the next turn_started");
  const originalSnapshot = commands.RemoteTabSnapshot;
  let releaseRuntimeHistory: ((value: Awaited<ReturnType<AppBindings["RemoteTabSnapshot"]>>) => void) | undefined;
  commands.RemoteTabSnapshot = async () => new Promise(resolve => { releaseRuntimeHistory = resolve; });
  await publishRuntime("idle", "next-turn");
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "turn_started", turnId: "third-turn" });
    __emitMockRemoteTab("tab-runtime", "event", { kind: "text", text: "third live answer" });
    await flush();
    releaseRuntimeHistory?.({ history: [{ role: "assistant", content: "obsolete history" }] });
    await flush();
  });
  ok(runtimeProbe!.transcript.running && runtimeProbe!.transcript.live?.text === "third live answer"
    && !runtimeProbe!.transcript.items.some(item => item.kind === "assistant" && item.text === "obsolete history"),
    "history reconciliation cannot replace a newer streaming turn");
  await publishRuntime("idle", "third-turn");
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "usage", turnId: "third-turn", usage: { promptTokens: 12, completionTokens: 3 } });
    releaseRuntimeHistory?.({ history: [{ role: "assistant", content: "third durable answer" }] });
    await flush();
  });
  ok(runtimeProbe!.transcript.items.some(item => item.kind === "assistant" && item.text === "third durable answer"),
    "late telemetry from the settled turn does not discard durable history");
  commands.RemoteTabSnapshot = originalSnapshot;
  await act(async () => runtimeRoot.unmount());
  runtimeNode.remove();
}

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
  // Only the ordered Follow stream settles transcript activity. Ancillary
  // status cannot replace its content or settle a newer turn.
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
    __emitMockRemoteTab("tab-runtime", "event", { kind: "text", messageId: "runtime-answer", text: "partial runtime answer" });
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
  setSnapshotHistory([{ role: "assistant", content: "obsolete metadata history" }]);
  await publishRuntime("idle");
  ok(runtimeProbe!.transcript.running && runtimeProbe!.transcript.live?.text === "partial runtime answer",
    "ancillary idle cannot settle or erase an active Follow stream");
  ok(tape.filter(entry => entry === "snapshot:tab-runtime").length === beforeRuntimeHistory,
    "runtime metadata never initiates a completion history rebase");
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "message", messageId: "runtime-answer", text: "complete durable runtime answer" });
    __emitMockRemoteTab("tab-runtime", "event", { kind: "turn_done", turnId: "lost-turn" });
    await flush();
  });
  ok(!runtimeProbe!.transcript.running && !runtimeProbe!.transcript.turnActive && runtimeProbe!.transcript.retry === undefined,
    "Follow completion settles activity and retry together");
  ok(runtimeNode.textContent?.includes("complete durable runtime answer") === true,
    "committed final content remains visible after Follow completion");
  await act(async () => {
    __emitMockRemoteTab("tab-runtime", "event", { kind: "turn_started", turnId: "next-turn" });
    __emitMockRemoteTab("tab-runtime", "event", { kind: "text", text: "next live answer" });
    await flush();
  });
  await publishRuntime("idle", "lost-turn");
  ok(runtimeProbe!.transcript.running && runtimeProbe!.transcript.live?.text === "next live answer",
    "old idle cannot settle the next Follow turn");
  const beforeStatus = tape.filter(entry => entry === "snapshot:tab-runtime").length;
  await act(async () => { await commands.RemoteTabStatus("tab-runtime"); await flush(); });
  ok(tape.filter(entry => entry === "snapshot:tab-runtime").length === beforeStatus
    && !runtimeNode.textContent?.includes("obsolete metadata history"),
    "late status cannot replace the transcript with unrelated history");
  await act(async () => runtimeRoot.unmount());
  runtimeNode.remove();
}

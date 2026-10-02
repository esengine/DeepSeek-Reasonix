import assert from "node:assert/strict";
import React, { act, useEffect } from "react";
import { JSDOM } from "jsdom";
import { installDesktopHostStub } from "./desktopHostStub";
import type { RemoteSessionView } from "../lib/types";
import type { RuntimeProjection, RuntimeSession } from "../lib/runtimeStateStore";

const dom = new JSDOM('<div id="root"></div>', { url: "http://localhost/" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.localStorage = dom.window.localStorage;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const row = (id: string, resultSequence: number) => ({ name: id, sessionId: id, title: id, turns: 1, resultSequence, metadataReady: true });
let rows: RemoteSessionView[] = [row("a", 3), row("b", 0)];
let loads = 0;
let deferred = false;
const pending: Array<(rows: RemoteSessionView[]) => void> = [];
const host = installDesktopHostStub({
  async RemoteConnectionStatuses() { return [{ hostId: "host", state: "connected" }]; },
  async RemoteServerStatus() { return { hostId: "host", state: "ready" }; },
  async RemoteProjectSessions() {
    loads++;
    if (deferred) return new Promise<RemoteSessionView[]>(resolve => pending.push(resolve));
    return rows;
  },
});
const [{ createRoot }, groups, reads, topics, runtime, { useRemoteStore }] = await Promise.all([
  import("react-dom/client"), import("../components/ProjectTreeRemoteGroups"),
  import("../components/useProjectTreeReadActivity"), import("../lib/projectTreeTopic"),
  import("../lib/runtimeStateStore"), import("../store/remote"),
]);
const tree = [{ key: "remote", kind: "project" as const, label: "Remote", remote: { hostId: "host", workspace: "/repo" } }];
const expanded = new Set(["remote"]);
const toast = () => {};
const t = (key: string) => key;
function session(id: string, phase: "executing" | "idle", seq: number): RuntimeSession {
  return { tabId: "tab", scope: "remote", workspaceRoot: "/repo", topicId: id, sessionId: id,
    sessionPath: `session-id:${id}`, sessionGeneration: 1, open: id === "b", remote: true, hostId: "host", freshness: "synced",
    state: { schemaVersion: 1, runtimeEpoch: id, revision: seq + 1, activityRevision: seq,
      phase, running: phase === "executing", turnId: id, turnStatus: phase === "idle" ? "completed" : "running",
      turnEventSeq: seq, durableEventSeq: seq, pendingPrompt: false, cancelRequested: false,
      cancellable: phase === "executing", backgroundJobs: 0, activity: "" } };
}
let revision = 0;
const publish = (sessions: RuntimeSession[]) => runtime.runtimeStateStore.commit({ epoch: "desktop", revision: ++revision, topics: [], sessions } satisfies RuntimeProjection);
publish([session("a", "executing", 3), session("b", "executing", 0)]);
useRemoteStore.getState().hydrateStatuses([{ hostId: "host", state: "connected" }]);
function Probe({ active = "b" }: { active?: string }) {
  const group = groups.useRemoteProjectGroups(tree, toast, expanded, "");
  const nodes = groups.useRemoteRuntimeTree(tree, group.remoteSessions, t);
  const read = reads.useProjectTreeReadActivity(nodes);
  useEffect(() => {
    const node = nodes[0].children?.find(node => node.remoteSession?.sessionId === active);
    if (node) read.markNodeRead(node);
  }, [active, nodes, read.markNodeRead]);
  return <>{nodes[0].children?.map(node => <span key={node.key} data-session={node.remoteSession!.sessionId}
    data-unread={topics.projectTreeTopicHasUnreadActivity(node, read.readActivity, undefined, undefined, undefined, undefined, read.readBaselineAt,
      { hostId: "host", workspace: "/repo", sessionId: active })} data-status={node.status ?? ""} />)}</>;
}
const root = createRoot(document.getElementById("root")!);
const flush = async () => { for (let i = 0; i < 15; i++) await Promise.resolve(); await new Promise(resolve => setTimeout(resolve, 0)); };
const unread = (id: string) => document.querySelector(`[data-session="${id}"]`)?.getAttribute("data-unread");
await act(async () => { root.render(<Probe />); await flush(); });
assert.equal(unread("a"), "false");
const baselineLoads = loads;
await act(async () => {
  host.emit("remote-tab:updated", { id: "tab", remote: tree[0].remote, running: true });
  publish([session("a", "executing", 4), session("b", "executing", 1)]);
  await flush();
});
assert.equal(loads, baselineLoads, "streaming does not continuously reload the listing");
rows = [row("a", 6), row("b", 0)];
await act(async () => {
  publish([session("a", "idle", 6), session("b", "executing", 2)]);
  publish([session("a", "idle", 6), session("b", "executing", 3)]);
  await flush();
});
assert.equal(loads, baselineLoads + 1, "background completion refreshes once while foreground is still busy");
assert.equal(unread("a"), "true", "A gains a blue dot without selecting it");
assert.equal(document.querySelector('[data-session="a"]')?.getAttribute("data-status"), "", "normal completion is not disconnected");
await act(async () => { root.render(<Probe active="a" />); await flush(); });
assert.equal(unread("a"), "false", "selecting A clears the blue dot");
await act(async () => { root.render(<Probe />); await flush(); });

// Removal of a completed detached runtime is also an authoritative refresh
// boundary, including when its terminal SSE was missed.
rows = [row("a", 9), row("b", 0)];
await act(async () => { publish([session("b", "executing", 4)]); await flush(); });
assert.equal(unread("a"), "true");

// A listing started before a later completion cannot replace its result count.
deferred = true;
await act(async () => { publish([session("a", "executing", 9), session("b", "executing", 4)]); await flush(); });
await act(async () => { publish([session("a", "idle", 12), session("b", "executing", 4)]); await flush(); });
await act(async () => { publish([session("a", "idle", 15), session("b", "executing", 4)]); await flush(); });
assert.equal(pending.length, 2);
await act(async () => { pending[1]([row("a", 15), row("b", 0)]); await flush(); });
await act(async () => { root.render(<Probe active="a" />); await flush(); });
await act(async () => { pending[0]([row("a", 12), row("b", 0)]); await flush(); });
const stored = JSON.parse(localStorage.getItem("projectTree:readActivity:v3")!);
assert.equal(stored.records["ref\0host\0a"].value, 15, "stale listing cannot lower the confirmed read");

// Drive metadata retry timers explicitly: a pending catalog must not seed a
// placeholder baseline, and a permanently pending catalog has a finite budget.
deferred = false;
const nativeTimeout = globalThis.setTimeout, nativeClearTimeout = globalThis.clearTimeout;
const retryTimers = new Map<number, () => void>();
let timerId = -1;
globalThis.setTimeout = ((callback: (...args: unknown[]) => void, delay?: number, ...args: unknown[]) => {
  if (delay !== 1000) return nativeTimeout(callback, delay, ...args);
  const id = timerId--;
  retryTimers.set(id, () => callback(...args));
  return id;
}) as typeof setTimeout;
globalThis.clearTimeout = ((id: ReturnType<typeof setTimeout>) => {
  if (!retryTimers.delete(Number(id))) nativeClearTimeout(id);
}) as typeof clearTimeout;
const advanceRetry = async () => {
  assert.equal(retryTimers.size, 1);
  const [id, callback] = [...retryTimers.entries()][0];
  retryTimers.delete(id);
  await act(async () => { callback(); await flush(); });
};
await act(async () => { root.render(<Probe />); await flush(); });
rows = [{ ...row("a", 0), metadataReady: false }, row("b", 0)];
await act(async () => { publish([session("a", "idle", 18), session("b", "executing", 4)]); await flush(); });
assert.equal(unread("a"), "false");
rows = [row("a", 18), row("b", 0)];
await advanceRetry();
assert.equal(unread("a"), "true", "ready metadata reveals the completed result without another runtime event");
assert.equal(retryTimers.size, 0);
rows = [{ ...row("a", 0), metadataReady: false }, row("b", 0)];
await act(async () => { publish([session("a", "idle", 21), session("b", "executing", 4)]); await flush(); });
for (let retry = 0; retry < 5; retry++) await advanceRetry();
assert.equal(retryTimers.size, 0, "metadata retries stop after five requests");
await act(async () => root.unmount());
globalThis.setTimeout = nativeTimeout;
globalThis.clearTimeout = nativeClearTimeout;
host.uninstall();
dom.window.close();
console.log("remote badges: background completion, read clearing, runtime retirement and stale listing passed");

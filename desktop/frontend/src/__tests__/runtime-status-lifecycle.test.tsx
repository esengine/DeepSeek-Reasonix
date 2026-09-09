import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { createPollingOwner, type PollClock } from "../app-runtime/pollingOwner";
import { useRuntimeStatus } from "../app-runtime/useRuntimeStatus";
import type { BackgroundRuntimeView, WorkspaceConflictView } from "../lib/types";

function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
let nextTimer = 0;
const timers = new Map<number, () => void>();
const clock: PollClock = { setTimeout: callback => { const id = ++nextTimer; timers.set(id, callback); return id; },
  clearTimeout: handle => { timers.delete(handle as number); } };
const fire = () => { const queued = [...timers.values()]; timers.clear(); for (const callback of queued) callback(); };
let gate = deferred<number>(); let reads = 0; let operations = 0;
const results: number[] = []; const errors: unknown[] = [];
const owner = createPollingOwner({ target: { kind: "application" }, periodMs: 1000, clock,
  read: () => { reads++; return gate.promise; }, publish: value => results.push(value), failed: error => errors.push(error),
}, delta => { operations += delta; });
const first = owner.refresh();
assert.equal(owner.refresh(), first, "manual refresh and timer share one in-flight request");
fire(); assert.equal(reads, 1); assert.equal(operations, 1);
gate.resolve(1); await first;
assert.deepEqual(results, [1]); assert.equal(operations, 0); assert.equal(timers.size, 1);
gate = deferred<number>(); fire(); assert.equal(reads, 2);
const second = owner.refresh(); gate.reject("fixture failure"); await second;
assert.deepEqual(errors, ["fixture failure"]); assert.equal(operations, 0);
const staleTimer = [...timers.values()][0];
owner.dispose(); owner.dispose(); staleTimer(); await owner.refresh();
assert.equal(reads, 2); assert.equal(timers.size, 0);

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const background = deferred<BackgroundRuntimeView[]>();
const requests: { tab: string; value: ReturnType<typeof deferred<WorkspaceConflictView>> }[] = [];
let backgroundReads = 0;
Object.assign(window, { go: { main: { App: {
  BackgroundRuntimes: () => { backgroundReads++; return background.promise; },
  WorkspaceConflictForTab: (tab: string) => { const value = deferred<WorkspaceConflictView>(); requests.push({ tab, value }); return value.promise; },
} } } });
const root = createRoot(document.getElementById("root")!);
let current!: ReturnType<typeof useRuntimeStatus>;
function Probe({ tab, generation = 1, running = true }: { tab: string; generation?: number; running?: boolean }) {
  current = useRuntimeStatus({ tabId: tab, sessionKey: `${tab}:${generation}`, running }, clock);
  return <div>{current.workspaceConflict?.ownerTitle ?? "clear"}</div>;
}
const paint = (tab: string, generation = 1, running = true) => act(async () => root.render(<Probe tab={tab} generation={generation} running={running} />));
const conflict = (ownerTitle: string) => ({ state: "local", ownerTitle } as WorkspaceConflictView);
try {
  await paint("A");
  const refresh = current.refreshBackgroundRuntimes;
  const manual = refresh(); fire();
  assert.equal(backgroundReads, 1);
  await paint("B"); await paint("A", 2);
  assert.deepEqual(requests.map(request => request.tab), ["A", "B", "A"]);
  await act(async () => {
    requests[0].value.resolve(conflict("old A")); requests[1].value.resolve(conflict("B"));
    requests[2].value.resolve(conflict("new A")); background.resolve([]); await manual;
  });
  assert.equal(document.body.textContent, "new A", "only the current resource generation publishes a conflict");
  await paint("A", 2, false);
  assert.equal(document.body.textContent, "clear");
  await act(async () => current.setWorkspaceConflict(conflict("fixture decision")));
  assert.equal(document.body.textContent, "fixture decision", "explicit decision fixtures remain reachable without a running turn");
  await act(async () => current.setWorkspaceConflict(null));
  const queuedTimers = [...timers.values()];
  await act(async () => root.unmount());
  assert.equal(timers.size, 0);
  queuedTimers.forEach(callback => callback()); await refresh();
  assert.equal(backgroundReads, 1, "queued timer and retained manual refresh are inert after unmount");
  assert.equal(requests.length, 3);
  console.log("runtime polling: single flight, deterministic timers, terminal counts, source replacement and synchronous disposal passed");
} finally { dom.window.close(); }

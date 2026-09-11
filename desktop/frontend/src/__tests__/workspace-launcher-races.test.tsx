import assert from "node:assert/strict";
import React, { act, StrictMode, useRef } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useBranchSwitcher } from "../lib/useBranchSwitcher";
import { useWorkspaceDiffStats } from "../lib/useWorkspaceDiffStats";
import { createSerialWorkspacePoll } from "../lib/serialWorkspacePoll";
import { installDesktopHostStub } from "./desktopHostStub";
import type { WorkspaceChangesView } from "../lib/types";
import { DockLauncher } from "../components/DockLauncher";
import { LocaleProvider } from "../lib/i18n";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };

// Completion-driven polling with a clock controlled by the test.
const timers = new Map<number, () => void>();
let nextTimer = 0;
const poll = createSerialWorkspacePoll({
  schedule(callback, delay) { assert.equal(delay, 5000); timers.set(++nextTimer, callback); return nextTimer; },
  cancel(handle) { timers.delete(handle as number); },
});
const pending: ReturnType<typeof deferred<void>>[] = [];
const job = () => { const request = deferred<void>(); pending.push(request); return request.promise; };
poll.setJob(job);
await flush();
assert.equal(pending.length, 1);
assert.equal(timers.size, 0, "slow request must not schedule overlapping polls");
poll.refresh(); poll.refresh(); poll.refresh();
pending[0].resolve(); await flush();
assert.equal(pending.length, 2, "manual refreshes coalesce");
pending[1].resolve(); await flush();
assert.equal(timers.size, 1, "wait five seconds after completion");
const tick = [...timers.values()][0]; timers.clear(); tick(); await flush();
assert.equal(pending.length, 3);
poll.setJob(null); pending[2].resolve(); await flush();
assert.equal(timers.size, 0);
poll.setJob(job); poll.setJob(null); await flush();
assert.equal(pending.length, 3, "hide before dispatch cancels queued work");

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, {
  window: dom.window, document: dom.window.document, Node: dom.window.Node,
  localStorage: dom.window.localStorage, IS_REACT_ACT_ENVIRONMENT: true,
});
let foreground = true;
Object.defineProperty(document, "visibilityState", { get: () => foreground ? "visible" : "hidden" });
const lists: { tab: string; root: string; request: ReturnType<typeof deferred<string[]>> }[] = [];
const switches: { tab: string; root: string; branch: string; request: ReturnType<typeof deferred<void>> }[] = [];
const stats: { tab: string; root: string; request: ReturnType<typeof deferred<WorkspaceChangesView>> }[] = [];
const stub = installDesktopHostStub({
  GitBranchesForTab(tab: string, root: string) {
    const request = deferred<string[]>(); lists.push({ tab, root, request }); return request.promise;
  },
  GitCheckoutForTab(tab: string, root: string, branch: string) {
    const request = deferred<void>(); switches.push({ tab, root, branch, request }); return request.promise;
  },
  GitCreateBranchForTab(tab: string, root: string, branch: string) {
    const request = deferred<void>(); switches.push({ tab, root, branch, request }); return request.promise;
  },
  WorkspaceGitStatsForTab(tab: string, root: string) {
    const request = deferred<WorkspaceChangesView>(); stats.push({ tab, root, request }); return request.promise;
  },
});
let branch!: ReturnType<typeof useBranchSwitcher>;
let diff!: ReturnType<typeof useWorkspaceDiffStats>;
let changes = 0;
function Harness({ scope, visible }: { scope: string; visible: boolean }) {
  const rootRef = useRef<HTMLDivElement>(null);
  diff = useWorkspaceDiffStats("reused-tab", scope, "/" + scope, visible);
  branch = useBranchSwitcher({
    tabId: "reused-tab", scopeKey: scope, workspaceRoot: "/" + scope, gitBranch: "main", rootRef,
    onBranchChanged: () => { changes++; diff.reloadDiffStats(); },
  });
  return <div ref={rootRef} />;
}
const root = createRoot(document.getElementById("root")!);
const render = async (scope: string, visible = true) => act(async () => {
  root.render(<StrictMode><Harness scope={scope} visible={visible} /></StrictMode>);
  await flush();
});
await render("A");
await act(async () => { branch.toggleBranchMenu(); branch.setBranchQuery("old"); await flush(); });
assert.equal(lists[0].root, "/A");
await act(async () => { void branch.checkoutBranch("shared"); await flush(); });
assert.equal(switches[0].root, "/A");
await render("B");
assert.equal(branch.branchQuery, "");
assert.deepEqual(branch.branches, []);
assert.equal(branch.branchSwitchErr, "");
await act(async () => { branch.toggleBranchMenu(); await flush(); });
assert.equal(lists[1].root, "/B", "same tab and same branch still carry project identity");
await act(async () => {
  lists[1].request.resolve(["main", "shared", "B-only"]);
  lists[0].request.resolve(["A-only"]);
  switches[0].request.resolve();
  await flush();
});
assert.deepEqual(branch.branches, ["main", "shared", "B-only"]);
assert.equal(branch.activeBranch, "main", "late A checkout cannot change B's branch");
assert.equal(changes, 0, "late A checkout cannot refresh B");
assert.equal(stats.length, 1, "B waits for the in-flight A statistics request");
await act(async () => { stats[0].request.resolve({ files: [], gitAvailable: true, added: 99 }); await flush(); });
assert.equal(diff.diffStats, null, "A statistics must not render in B");
assert.equal(stats.length, 2);
assert.equal(stats[1].root, "/B");
await act(async () => { stats[1].request.resolve({ files: [], gitAvailable: true, added: 2, incomplete: true }); await flush(); });
assert.equal(diff.diffStats?.added, 2);
assert.equal(diff.diffStats?.incomplete, true);
await act(async () => { void branch.checkoutBranch("shared"); void branch.checkoutBranch("shared"); await flush(); });
assert.equal(switches.length, 2, "duplicate checkout clicks coalesce synchronously");
await act(async () => { switches[1].request.resolve(); await flush(); });
assert.equal(branch.activeBranch, "shared");
assert.equal(changes, 1);
assert.equal(lists.length, 3, "successful checkout refreshes its own branch list");
assert.equal(stats.length, 3, "successful checkout refreshes its own totals");
await act(async () => {
  lists[2].request.resolve(["main", "shared"]);
  stats[2].request.reject(new Error("timeout"));
  await flush();
});
assert.equal(diff.diffStats?.added, 2, "failed count retains last estimate");
assert.equal(diff.diffStats?.incomplete, true);
await act(async () => { void branch.createBranch("new"); await flush(); });
await render("C");
await act(async () => { switches[2].request.reject(new Error("A stale failure")); await flush(); });
assert.equal(branch.branchSwitchErr, "");
await act(async () => {
  foreground = false; document.dispatchEvent(new dom.window.Event("visibilitychange")); await flush();
});
const countBeforeHide = stats.length;
await act(async () => {
  stats.at(-1)!.request.resolve({ files: [], gitAvailable: true, added: 100 });
  diff.reloadDiffStats(); await flush();
});
assert.equal(stats.length, countBeforeHide, "hidden page stops manual and periodic dispatch");
assert.equal(diff.diffStats, null, "hidden request result is invalidated");
await act(async () => { foreground = true; document.dispatchEvent(new dom.window.Event("visibilitychange")); await flush(); });
assert.equal(stats.length, countBeforeHide + 1);
const beforeRemount = stats.length;
await act(async () => { root.render(null); await flush(); });
await render("C");
assert.equal(stats.length, beforeRemount, "remount waits for the old scope's running RPC");
await act(async () => { stats[beforeRemount - 1].request.resolve({ files: [], gitAvailable: true, added: 999 }); await flush(); });
assert.equal(stats.length, beforeRemount + 1, "remount scans again only after the old request completes");
assert.equal(diff.diffStats, null, "remount must discard the pre-hide result");
await render("C", false);
await act(async () => { stats.at(-1)!.request.resolve({ files: [], gitAvailable: true }); await flush(); });
await act(async () => {
  root.render(<LocaleProvider><DockLauncher tabId="badge" scopeKey="badge" workspaceRoot="/badge" visible overlay gitBranch="main" onSelect={() => {}} /></LocaleProvider>);
  await flush();
});
await act(async () => { stats.at(-1)!.request.resolve({ files: [], gitAvailable: true, added: 0, removed: 0, incomplete: true }); await flush(); });
const badge = document.querySelector(".dock-launcher__entry-stats");
assert(badge, "an incomplete zero must not disappear as an exact clean result");
assert(badge.textContent.includes("~"), "incomplete totals visibly show an estimate");
assert(badge.getAttribute("title")?.includes("incomplete"), "estimate has an accessible explanation");
await act(async () => root.unmount());
stub.uninstall(); dom.window.close();
console.log("PASS scoped branch races, stale checkout/create, serial polling, visibility and incomplete totals");

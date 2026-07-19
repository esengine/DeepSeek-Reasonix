// Run: tsx src/__tests__/target-transition-hydration.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextPanel } from "../components/ContextPanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import { commitTargetScopedValue, workspaceScopeKeyForAuthority } from "../lib/targetAuthority";
import { useController } from "../lib/useController";
import type {
  BalanceInfo,
  ContextInfo,
  ContextPanelInfo,
  EffortInfo,
  HistoryMessage,
  JobView,
  Meta,
  RemoteTargetStatusView,
  TabMeta,
  WireEvent,
} from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

async function waitForTimer(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 25));
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

type BackendProjection = {
  tab: TabMeta;
  history: HistoryMessage[];
  context: ContextInfo;
};

function tabMeta(id: string, sessionPath: string, targetKind: "local" | "remote", title: string): TabMeta {
  const workspaceRoot = targetKind === "remote" ? "workspace-opaque" : "D:/local-workspace";
  return {
    id,
    scope: "project",
    targetKind,
    workspaceRoot,
    workspaceName: targetKind === "remote" ? "Remote workspace" : "Local workspace",
    workspacePath: targetKind === "remote" ? "/srv/remote-workspace" : workspaceRoot,
    gitBranch: "main",
    topicId: "topic-collision",
    topicTitle: title,
    sessionPath,
    label: "model",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: targetKind === "remote" ? "/srv/remote-workspace" : workspaceRoot,
  };
}

function metaFor(tab: TabMeta): Meta {
  return {
    label: tab.label,
    ready: tab.ready !== false,
    eventChannel: "agent:event",
    cwd: tab.cwd || tab.workspaceRoot,
    workspaceRoot: tab.workspaceRoot,
    workspaceName: tab.workspaceName,
    workspacePath: tab.workspacePath,
    sessionPath: tab.sessionPath,
    gitBranch: tab.gitBranch,
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
  };
}

function panelInfo(totalTokens: number, requestCount: number): ContextPanelInfo {
  return {
    usedTokens: totalTokens,
    windowTokens: 1_000_000,
    promptTokens: totalTokens,
    completionTokens: 0,
    totalTokens,
    reasoningTokens: 0,
    cacheHitTokens: 0,
    cacheMissTokens: totalTokens,
    sessionCacheHitTokens: 0,
    sessionCacheMissTokens: totalTokens,
    sessionCompletionTokens: 0,
    requestCount,
    readFiles: [],
    changedFiles: [],
  };
}

function transcriptText(controller: Controller | undefined): string {
  return controller?.state.items
    .filter((item) => item.kind === "user" || item.kind === "assistant")
    .map((item) => item.text)
    .join("\n") ?? "";
}

console.log("\ntarget transition hydration");

const staleHeaderTabs = deferred<TabMeta[]>();
const currentHeaderTabs = deferred<TabMeta[]>();
let currentHeaderTargetGen = 0;
let latestCommittedHeaderRequestSeq = 0;
let renderedHeader = "";
const projectHeader = async (
  request: { targetIdentityGen: number; requestSeq: number },
  result: Promise<TabMeta[]>,
) => {
  const tabs = await result;
  commitTargetScopedValue(
    request,
    currentHeaderTargetGen,
    latestCommittedHeaderRequestSeq,
    tabs,
    (accepted) => {
      latestCommittedHeaderRequestSeq = request.requestSeq;
      renderedHeader = accepted.find((tab) => tab.active)?.topicTitle ?? "";
    },
  );
};
const staleHeaderProjection = projectHeader(
  { targetIdentityGen: 0, requestSeq: 1 },
  staleHeaderTabs.promise,
);
currentHeaderTargetGen = 1;
const currentHeaderProjection = projectHeader(
  { targetIdentityGen: 1, requestSeq: 1 },
  currentHeaderTabs.promise,
);
currentHeaderTabs.resolve([tabMeta("shared-tab", "collision-token", "remote", "Remote header")]);
await currentHeaderProjection;
staleHeaderTabs.resolve([tabMeta("shared-tab", "collision-token", "local", "Local stale header")]);
await staleHeaderProjection;
eq(renderedHeader, "Remote header", "late App ListTabs snapshot cannot overwrite the current target header");

const stalePollTabs = deferred<TabMeta[]>();
const latestPollTabs = deferred<TabMeta[]>();
const stalePollProjection = projectHeader(
  { targetIdentityGen: 1, requestSeq: 2 },
  stalePollTabs.promise,
);
const latestPollProjection = projectHeader(
  { targetIdentityGen: 1, requestSeq: 3 },
  latestPollTabs.promise,
);
latestPollTabs.resolve([tabMeta("shared-tab", "collision-token", "remote", "Latest Remote header")]);
await latestPollProjection;
stalePollTabs.resolve([tabMeta("shared-tab", "collision-token", "remote", "Stale Remote poll")]);
await stalePollProjection;
eq(renderedHeader, "Latest Remote header", "late same-target ListTabs poll cannot overwrite a newer request");

const slowPollTabs = deferred<TabMeta[]>();
const nextSlowPollTabs = deferred<TabMeta[]>();
const slowPollProjection = projectHeader(
  { targetIdentityGen: 1, requestSeq: 4 },
  slowPollTabs.promise,
);
const nextSlowPollProjection = projectHeader(
  { targetIdentityGen: 1, requestSeq: 5 },
  nextSlowPollTabs.promise,
);
slowPollTabs.resolve([tabMeta("shared-tab", "collision-token", "remote", "Slow completed header")]);
await slowPollProjection;
eq(renderedHeader, "Slow completed header", "slow ListTabs response can commit while a newer poll remains pending");
nextSlowPollTabs.resolve([tabMeta("shared-tab", "collision-token", "remote", "Next slow header")]);
await nextSlowPollProjection;

const workspaceKeyParts = {
  activeTabId: "shared-tab",
  tabSessionPath: "collision-token",
  metaSessionPath: "collision-token",
  cwd: "/srv/collision",
  sessionGen: 1,
  workspaceControllerEpoch: 1,
};
const localWorkspaceScopeKey = workspaceScopeKeyForAuthority({ targetIdentityGen: 7, ...workspaceKeyParts });
const remoteWorkspaceScopeKey = workspaceScopeKeyForAuthority({ targetIdentityGen: 8, ...workspaceKeyParts });
ok(localWorkspaceScopeKey !== remoteWorkspaceScopeKey, "workspace scope key changes with target authority despite identical tab/session ids");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
const nativeRequestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
const nativeCancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
const heldAnimationFrames = new Map<number, FrameRequestCallback>();
let holdAnimationFrames = false;
let heldAnimationFrameId = -1;
globalThis.requestAnimationFrame = (callback: FrameRequestCallback): number => {
  if (!holdAnimationFrames) return nativeRequestAnimationFrame(callback);
  const id = heldAnimationFrameId;
  heldAnimationFrameId -= 1;
  heldAnimationFrames.set(id, callback);
  return id;
};
globalThis.cancelAnimationFrame = (id: number): void => {
  if (!heldAnimationFrames.delete(id)) nativeCancelAnimationFrame(id);
};

const targetHandlers: Array<(status: RemoteTargetStatusView) => void> = [];
const readyHandlers: Array<(tabId?: string) => void> = [];
const eventHandlers: Array<(event: WireEvent) => void> = [];
const initialTargetQuery = deferred<RemoteTargetStatusView>();
let targetStatusCalls = 0;
let targetStatus: RemoteTargetStatusView = {
  state: "RemoteConnected",
  hostId: "host-a",
  hostLabel: "Host A",
  canReconnect: false,
};
let backend: BackendProjection = {
  tab: tabMeta("remote-initial", "remote-initial-token", "remote", "Remote before Local"),
  history: [{ role: "user", content: "remote-before" }],
  context: { used: 10, window: 1_000, sessionTokens: 10, sessionCost: 0.1, sessionCurrency: "¥" },
};
let reconnectHistory = deferred<HistoryMessage[]>();
let reconnectContext = deferred<ContextInfo>();
let gateReconnect = false;
const historyCalls: string[] = [];
let balanceCalls = 0;
let historyPageStartTurn = 0;
let deferredOlderHistoryPage: ReturnType<typeof deferred<{
  messages: HistoryMessage[];
  startTurn: number;
  endTurn: number;
  totalTurns: number;
  hasOlder: boolean;
}>> | undefined;
let olderHistoryRequestStarted = false;
let deferredMeta: ReturnType<typeof deferred<Meta>> | undefined;
let deferredMetaRequestStarted = false;
let deferredOpenProjectTab: ReturnType<typeof deferred<TabMeta>> | undefined;
let deferredOpenProjectTabRequestStarted = false;
let deferredCloseTab: ReturnType<typeof deferred<void>> | undefined;
let deferredCloseTabRequestStarted = false;
const deferredAncillarySnapshotsStarted = new Set<string>();
let deferTurnDoneSnapshots = false;
let staleTurnDoneSnapshots: {
  meta: ReturnType<typeof deferred<Meta>>;
  context: ReturnType<typeof deferred<ContextInfo>>;
  balance: ReturnType<typeof deferred<BalanceInfo>>;
  effort: ReturnType<typeof deferred<EffortInfo>>;
  jobs: ReturnType<typeof deferred<JobView[]>>;
} | undefined;

window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "remote:target-state") targetHandlers.push(cb as (status: RemoteTargetStatusView) => void);
    if (name === "agent:ready") readyHandlers.push(cb as (tabId?: string) => void);
    if (name === "agent:event") eventHandlers.push(cb as (event: WireEvent) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      RemoteTargetStatus: async () => {
        targetStatusCalls += 1;
        if (targetStatusCalls === 1) return initialTargetQuery.promise;
        return { ...targetStatus };
      },
      ListTabs: async () => {
        return [{ ...backend.tab }];
      },
      SetActiveTab: async () => {},
      OpenProjectTab: async () => {
        if (!deferredOpenProjectTab) return { ...backend.tab };
        const pending = deferredOpenProjectTab;
        deferredOpenProjectTab = undefined;
        deferredOpenProjectTabRequestStarted = true;
        return pending.promise;
      },
      CloseTab: async () => {
        if (!deferredCloseTab) return;
        const pending = deferredCloseTab;
        deferredCloseTab = undefined;
        deferredCloseTabRequestStarted = true;
        return pending.promise;
      },
      MetaForTab: async () => {
        if (deferTurnDoneSnapshots && staleTurnDoneSnapshots) return staleTurnDoneSnapshots.meta.promise;
        if (deferredMeta) {
          const pending = deferredMeta;
          deferredMeta = undefined;
          deferredMetaRequestStarted = true;
          return pending.promise;
        }
        return metaFor(backend.tab);
      },
      ContextUsageForTab: async () => {
        if (deferTurnDoneSnapshots && staleTurnDoneSnapshots) return staleTurnDoneSnapshots.context.promise;
        return gateReconnect ? reconnectContext.promise : { ...backend.context };
      },
      EffortForTab: async () => {
        if (deferTurnDoneSnapshots && staleTurnDoneSnapshots) {
          deferredAncillarySnapshotsStarted.add("effort");
          return staleTurnDoneSnapshots.effort.promise;
        }
        return { supported: true, current: "auto", default: "auto", levels: ["auto"] };
      },
      BalanceForTab: async () => {
        balanceCalls += 1;
        if (deferTurnDoneSnapshots && staleTurnDoneSnapshots) {
          deferredAncillarySnapshotsStarted.add("balance");
          return staleTurnDoneSnapshots.balance.promise;
        }
        return { available: false, display: "" };
      },
      JobsForTab: async () => {
        if (deferTurnDoneSnapshots && staleTurnDoneSnapshots) {
          deferredAncillarySnapshotsStarted.add("jobs");
          return staleTurnDoneSnapshots.jobs.promise;
        }
        return [];
      },
      CheckpointsForTab: async () => [],
      HistoryForTab: async () => gateReconnect ? reconnectHistory.promise : [...backend.history],
      HistoryPageForTab: async (tabId: string, beforeTurn: number) => {
        historyCalls.push(tabId);
        if (beforeTurn >= 0 && deferredOlderHistoryPage) {
          const pending = deferredOlderHistoryPage;
          deferredOlderHistoryPage = undefined;
          olderHistoryRequestStarted = true;
          return pending.promise;
        }
        const messages = gateReconnect ? await reconnectHistory.promise : [...backend.history];
        const turns = messages.filter((message) => message.role === "user").length;
        return {
          messages,
          startTurn: historyPageStartTurn,
          endTurn: historyPageStartTurn + turns,
          totalTurns: historyPageStartTurn + turns,
          hasOlder: historyPageStartTurn > 0,
        };
      },
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;

function Probe() {
  controller = useController();
  return null;
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});
const mountListTabs = deferred<TabMeta[]>();
const mountListTabsRequest = { targetIdentityGen: controller?.targetIdentityGen ?? 0, requestSeq: 1 };
let mountHeader = "";
const mountListTabsProjection = mountListTabs.promise.then((tabs) => {
  commitTargetScopedValue(
    mountListTabsRequest,
    controller?.targetIdentityGen ?? 0,
    0,
    tabs,
    (accepted) => { mountHeader = accepted.find((tab) => tab.active)?.topicTitle ?? ""; },
  );
});
holdAnimationFrames = true;
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "text", tabId: "remote-initial", text: "pre-binding-local-buffer" });
  }
  await flushPromises();
});
await act(async () => {
  // The subscription observes RemoteConnected while an older Local status
  // query is still pending. Its late result must not roll identity back.
  for (const handler of targetHandlers) handler({ ...targetStatus });
  initialTargetQuery.resolve({ state: "LocalConnected", canReconnect: false });
  await flushPromises();
});
await waitFor("initial Remote projection", () =>
  controller?.activeTabId === "remote-initial" && transcriptText(controller) === "remote-before" && controller.state.context.used === 10,
);
eq(controller?.targetIdentityGen, 1, "first stable target advances the App-visible authority generation");
await act(async () => {
  holdAnimationFrames = false;
  const callbacks = Array.from(heldAnimationFrames.values());
  heldAnimationFrames.clear();
  for (const callback of callbacks) callback(0);
  await flushPromises();
});
eq(transcriptText(controller), "remote-before", "pre-binding buffered Local text cannot flush into the first Remote projection");
await act(async () => {
  mountListTabs.resolve([tabMeta("local-mount", "local-mount", "local", "Late mount Local header")]);
  await mountListTabsProjection;
});
eq(mountHeader, "", "first Remote binding rejects a pre-binding Local ListTabs header response");

// First change authority and tab id. This establishes a Local transcript whose
// tab/session identities will deliberately collide with the next Remote target.
targetStatus = { state: "LocalConnected", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "local", "Local session"),
  history: [{ role: "user", content: "local-stale" }],
  context: { used: 111, window: 2_000, sessionTokens: 111, sessionCost: 1.11, sessionCurrency: "¥" },
};
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  await flushPromises();
});
await waitFor("Local projection", () =>
  controller?.activeTabId === "shared-tab" && transcriptText(controller) === "local-stale" && controller.state.sessionTokens === 111,
);
eq(controller?.activeTabId, "shared-tab", "late initial Local query cannot roll back the newer Remote identity");
eq(controller?.state.context.used, 111, "changed active id hydrates Local metrics");
const historyCallsBeforeReconnect = historyCalls.length;

// Hold every turn_done-derived Local snapshot after its RPC has started. They
// will resolve only after Remote has hydrated the same tab id, reproducing the
// cross-authority async completion race independently of history hydration.
staleTurnDoneSnapshots = {
  meta: deferred<Meta>(),
  context: deferred<ContextInfo>(),
  balance: deferred<BalanceInfo>(),
  effort: deferred<EffortInfo>(),
  jobs: deferred<JobView[]>(),
};
deferTurnDoneSnapshots = true;
await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_done", tabId: "shared-tab" });
  await flushPromises();
});
deferTurnDoneSnapshots = false;

// Reconnect Host A using exactly the Local tab id and session token. Before the
// fix, agent:ready treated this as reusable cached history forever.
targetStatus = {
  state: "RemoteConnected",
  hostId: "host-a",
  hostLabel: "Host A",
  canReconnect: false,
};
backend = {
  tab: tabMeta("shared-tab", "collision-token", "remote", "Remote after Local"),
  history: [{ role: "user", content: "remote-after" }],
  context: { used: 222, window: 4_000, sessionTokens: 222, sessionCost: 2.22, sessionCurrency: "$" },
};
gateReconnect = true;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  await flushPromises();
});
eq(controller?.activeTabId, undefined, "same-id target change detaches the stale active projection until Remote ready");
eq(transcriptText(controller), "", "same-id target change clears the Local transcript before Remote hydrate");
eq(controller?.state.context.used, 0, "same-id target change clears stale Local metrics before Remote hydrate");
const balanceCallsBeforeRemoteReady = balanceCalls;

await act(async () => {
  for (const handler of readyHandlers) handler("shared-tab");
  reconnectHistory.resolve([...backend.history]);
  reconnectContext.resolve({ ...backend.context });
  await flushPromises();
});
await waitFor("reattached Remote projection", () =>
  controller?.activeTabId === "shared-tab" &&
  transcriptText(controller) === "remote-after" &&
  controller.state.context.used === 222 &&
  controller.state.sessionTokens === 222,
);
await waitFor("Remote target hydration completion", () => balanceCalls > balanceCallsBeforeRemoteReady);
await act(async () => {
  await flushPromises();
});
eq(controller?.state.sessionCost, 2.22, "Remote session cost replaces Local metrics");
eq(controller?.state.sessionCurrency, "$", "Remote session currency replaces Local metrics");
ok(historyCalls.length > historyCallsBeforeReconnect, "same-id Remote ready forces history reload instead of cache reuse");

await act(async () => {
  staleTurnDoneSnapshots?.meta.resolve(metaFor(tabMeta("shared-tab", "collision-token", "local", "Local stale meta")));
  staleTurnDoneSnapshots?.context.resolve({ used: 111, window: 2_000, sessionTokens: 111, sessionCost: 1.11, sessionCurrency: "¥" });
  staleTurnDoneSnapshots?.balance.resolve({ available: true, display: "LOCAL-STALE" });
  staleTurnDoneSnapshots?.effort.resolve({ supported: true, current: "stale", default: "stale", levels: ["stale"] });
  staleTurnDoneSnapshots?.jobs.resolve([{ id: "local-stale-job", kind: "bash", label: "Local stale", status: "running", startedAt: 1 }]);
  await flushPromises();
});
eq(transcriptText(controller), "remote-after", "late Local turn_done callbacks cannot overwrite the Remote transcript");
eq(controller?.state.context.used, 222, "late Local turn_done context cannot overwrite Remote metrics");
eq(controller?.state.meta?.workspaceName, "Remote workspace", "late Local MetaForTab cannot overwrite Remote metadata");
eq(controller?.state.balance?.available, false, "late Local balance cannot overwrite Remote balance");
eq(controller?.state.effort?.current, "auto", "late Local effort cannot overwrite Remote effort");
eq(controller?.state.jobs.length, 0, "late Local jobs cannot overwrite Remote jobs");

const historyCallsBeforeSameHostRecovery = historyCalls.length;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus, state: "RemoteReconnecting", canReconnect: true });
  await flushPromises();
});
eq(transcriptText(controller), "remote-after", "same-Host transport loss keeps the last atomic Remote transcript visible");
eq(controller?.state.context.used, 222, "same-Host transport loss keeps the last atomic Remote metrics visible");

backend = {
  ...backend,
  history: [{ role: "user", content: "remote-recovered" }],
  context: { used: 333, window: 4_000, sessionTokens: 333, sessionCost: 3.33, sessionCurrency: "$" },
};
gateReconnect = false;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus, state: "RemoteConnected", canReconnect: false });
  for (const handler of readyHandlers) handler("shared-tab");
  await flushPromises();
});
await waitFor("same-Host recovery snapshot", () =>
  transcriptText(controller) === "remote-recovered" && controller?.state.context.used === 333,
);
eq(controller?.state.sessionCost, 3.33, "same-Host ready replaces metrics from the pre-reconnect snapshot");
ok(historyCalls.length > historyCallsBeforeSameHostRecovery, "same-Host ready force-reloads its reattached snapshot");

// A runtime reconciliation that started under Host A must not apply its late
// jobs/effort/balance phase after Local becomes authoritative, even when the
// tab id is unchanged. This exercises the second epoch check after Promise.all.
const staleRuntimeTab = { ...backend.tab, running: true, topicTitle: "Host A stale reconcile" };
const staleReconcileAncillary = {
  meta: deferred<Meta>(),
  context: deferred<ContextInfo>(),
  balance: deferred<BalanceInfo>(),
  effort: deferred<EffortInfo>(),
  jobs: deferred<JobView[]>(),
};
staleTurnDoneSnapshots = staleReconcileAncillary;
deferredAncillarySnapshotsStarted.clear();
deferTurnDoneSnapshots = true;
let staleReconcilePromise: Promise<TabMeta[] | undefined> | undefined;
await act(async () => {
  staleReconcilePromise = controller?.switchTab("shared-tab", staleRuntimeTab);
  await flushPromises();
});
await waitFor("old target reconcile ancillary requests", () =>
  deferredAncillarySnapshotsStarted.has("jobs") &&
  deferredAncillarySnapshotsStarted.has("effort") &&
  deferredAncillarySnapshotsStarted.has("balance"),
);
deferTurnDoneSnapshots = false;

targetStatus = { state: "LocalConnected", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "local", "Local after reconcile"),
  history: [{ role: "user", content: "local-after-reconcile" }],
  context: { used: 444, window: 5_000, sessionTokens: 444, sessionCost: 4.44, sessionCurrency: "¥" },
};
historyPageStartTurn = 0;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  await flushPromises();
});
await waitFor("Local projection after stale reconcile starts", () =>
  transcriptText(controller) === "local-after-reconcile" && controller?.state.context.used === 444,
);
await act(async () => {
  staleReconcileAncillary.jobs.resolve([{ id: "stale-reconcile-job", kind: "bash", label: "stale", status: "running", startedAt: 1 }]);
  staleReconcileAncillary.effort.resolve({ supported: true, current: "stale-reconcile", default: "stale", levels: ["stale"] });
  staleReconcileAncillary.balance.resolve({ available: true, display: "STALE-RECONCILE" });
  await staleReconcilePromise;
  await flushPromises();
});
eq(controller?.state.running, false, "late reconcile runtime snapshot cannot mark the new authority running");
eq(transcriptText(controller), "local-after-reconcile", "late reconcile cannot overwrite the new authority transcript");
eq(controller?.state.jobs.length, 0, "late reconcile jobs cannot overwrite the new authority");
eq(controller?.state.effort?.current, "auto", "late reconcile effort cannot overwrite the new authority");
eq(controller?.state.balance?.available, false, "late reconcile balance cannot overwrite the new authority");

// The startup metadata poll is intentionally allowed to remain in flight while
// Host B is replaced by Host C. refreshMetaOnlyForTab is fetch-only; the caller
// checks the target epoch before it dispatches the late result.
targetStatus = { state: "RemoteConnected", hostId: "host-b", hostLabel: "Host B", canReconnect: false };
backend = {
  tab: { ...tabMeta("shared-tab", "collision-token", "remote", "Host B waiting"), ready: false },
  history: [{ role: "user", content: "host-b-waiting" }],
  context: { used: 555, window: 6_000, sessionTokens: 555, sessionCost: 5.55, sessionCurrency: "$" },
};
const balanceCallsBeforeHostB = balanceCalls;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  for (const handler of readyHandlers) handler("shared-tab");
  await flushPromises();
});
await waitFor("Host B not-ready projection", () =>
  transcriptText(controller) === "host-b-waiting" && controller?.state.meta?.ready === false,
);
await waitFor("Host B ancillary hydration", () => balanceCalls > balanceCallsBeforeHostB);
const staleReadyMeta = deferred<Meta>();
deferredMeta = staleReadyMeta;
deferredMetaRequestStarted = false;
await waitForTimer("Host B metadata poll request", () => deferredMetaRequestStarted);

targetStatus = { state: "RemoteConnected", hostId: "host-c", hostLabel: "Host C", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "remote", "Host C current"),
  history: [{ role: "user", content: "host-c-current" }],
  context: { used: 666, window: 7_000, sessionTokens: 666, sessionCost: 6.66, sessionCurrency: "$" },
};
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  for (const handler of readyHandlers) handler("shared-tab");
  await flushPromises();
});
await waitFor("Host C projection", () =>
  transcriptText(controller) === "host-c-current" && controller?.state.meta?.workspaceName === "Remote workspace",
);
await act(async () => {
  staleReadyMeta.resolve(metaFor(tabMeta("shared-tab", "collision-token", "local", "Host B stale metadata")));
  await flushPromises();
});
eq(controller?.state.meta?.workspaceName, "Remote workspace", "late startup metadata poll cannot overwrite the current target");
eq(transcriptText(controller), "host-c-current", "late metadata poll leaves the current target transcript intact");

// A prepended history page is keyed by more than tab/session identity. Resolve
// a Local older-page request only after Host D has hydrated the exact same ids.
targetStatus = { state: "LocalConnected", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "local", "Local paged history"),
  history: [{ role: "user", content: "local-history-tail" }],
  context: { used: 777, window: 8_000, sessionTokens: 777, sessionCost: 7.77, sessionCurrency: "¥" },
};
historyPageStartTurn = 2;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  await flushPromises();
});
await waitFor("Local paged history projection", () =>
  transcriptText(controller) === "local-history-tail" && controller?.state.historyHasOlder === true,
);
const staleOlderPage = deferred<{
  messages: HistoryMessage[];
  startTurn: number;
  endTurn: number;
  totalTurns: number;
  hasOlder: boolean;
}>();
deferredOlderHistoryPage = staleOlderPage;
olderHistoryRequestStarted = false;
await act(async () => {
  void controller?.loadOlderHistory("shared-tab");
  await flushPromises();
});
await waitFor("Local older history request", () => olderHistoryRequestStarted);

targetStatus = { state: "RemoteConnected", hostId: "host-d", hostLabel: "Host D", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "remote", "Host D current"),
  history: [{ role: "user", content: "host-d-current" }],
  context: { used: 888, window: 9_000, sessionTokens: 888, sessionCost: 8.88, sessionCurrency: "$" },
};
historyPageStartTurn = 0;
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  for (const handler of readyHandlers) handler("shared-tab");
  await flushPromises();
});
await waitFor("Host D projection", () => transcriptText(controller) === "host-d-current");
await act(async () => {
  staleOlderPage.resolve({
    messages: [{ role: "user", content: "local-stale-older" }],
    startTurn: 1,
    endTurn: 2,
    totalTurns: 3,
    hasOlder: true,
  });
  await flushPromises();
});
eq(transcriptText(controller), "host-d-current", "late older history page cannot prepend content into the new target");

// The direct open-tab RPC also needs both target epoch and navigation sequence.
// Its stale result is rejected, not returned for App to seed into the header.
const staleOpenTab = deferred<TabMeta>();
deferredOpenProjectTab = staleOpenTab;
deferredOpenProjectTabRequestStarted = false;
let staleOpenRejected = false;
let staleOpenPromise: Promise<void> | undefined;
await act(async () => {
  staleOpenPromise = controller?.openProjectTab("workspace-opaque", "topic-collision")
    .then(() => {})
    .catch((err) => { staleOpenRejected = err instanceof Error && err.name === "TargetProjectionSupersededError"; });
  await flushPromises();
});
await waitFor("Host D open-tab request", () => deferredOpenProjectTabRequestStarted);
targetStatus = { state: "LocalConnected", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "local", "Local after stale open"),
  history: [{ role: "user", content: "local-after-stale-open" }],
  context: { used: 999, window: 10_000, sessionTokens: 999, sessionCost: 9.99, sessionCurrency: "¥" },
};
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  await flushPromises();
});
await waitFor("Local projection after stale open", () => transcriptText(controller) === "local-after-stale-open");
await act(async () => {
  staleOpenTab.resolve(tabMeta("shared-tab", "collision-token", "remote", "Host D stale open"));
  await staleOpenPromise;
  await flushPromises();
});
ok(staleOpenRejected, "late open-tab completion is rejected after target authority changes");
eq(transcriptText(controller), "local-after-stale-open", "late open-tab completion cannot mutate the new target projection");

const staleClose = deferred<void>();
deferredCloseTab = staleClose;
deferredCloseTabRequestStarted = false;
let staleCloseResult: boolean | undefined;
let staleClosePromise: Promise<boolean> | undefined;
await act(async () => {
  staleClosePromise = controller?.closeTab("shared-tab");
  await flushPromises();
});
await waitFor("Local close-tab request", () => deferredCloseTabRequestStarted);
targetStatus = { state: "RemoteConnected", hostId: "host-e", hostLabel: "Host E", canReconnect: false };
backend = {
  tab: tabMeta("shared-tab", "collision-token", "remote", "Host E after stale close"),
  history: [{ role: "user", content: "host-e-after-stale-close" }],
  context: { used: 1_111, window: 11_000, sessionTokens: 1_111, sessionCost: 11.11, sessionCurrency: "$" },
};
await act(async () => {
  for (const handler of targetHandlers) handler({ ...targetStatus });
  for (const handler of readyHandlers) handler("shared-tab");
  await flushPromises();
});
await waitFor("Host E projection", () => transcriptText(controller) === "host-e-after-stale-close");
await act(async () => {
  staleClose.resolve();
  staleCloseResult = await staleClosePromise;
  await flushPromises();
});
eq(staleCloseResult, false, "old-target close completion reports superseded after authority changes");
eq(controller?.activeTabId, "shared-tab", "old-target close completion cannot delete the new same-id active tab");
eq(transcriptText(controller), "host-e-after-stale-close", "old-target close completion cannot clear the new target transcript");

await act(async () => {
  root.unmount();
});

// ContextPanel owns a second, private metrics snapshot. Its request/cache key
// must include target identity as well as tabId/sessionGen, because all three
// backend identities can collide across Local and Remote.
const lateLocalPanel = deferred<ContextPanelInfo>();
const remotePanel = deferred<ContextPanelInfo>();
let panelLoader: () => Promise<ContextPanelInfo> = async () => panelInfo(111_111, 11);
window.go.main.App = {
  ContextPanel: async () => panelLoader(),
} as Partial<AppBindings> as AppBindings;

function PanelProbe({ targetIdentityGen, refreshKey }: { targetIdentityGen: number; refreshKey: number }) {
  return (
    <LocaleProvider>
      <ContextPanel
        tabId="shared-tab"
        sessionGen={1}
        targetIdentityGen={targetIdentityGen}
        refreshKey={refreshKey}
      />
    </LocaleProvider>
  );
}

function panelSummaryText(): string {
  return document.querySelector(".context-panel__summary-rows")?.textContent ?? "";
}

const panelRoot = createRoot(rootElement);
await act(async () => {
  panelRoot.render(<PanelProbe targetIdentityGen={0} refreshKey={0} />);
  await flushPromises();
});
await waitFor("Local private panel metrics", () => panelSummaryText().includes("111,111"));

panelLoader = () => lateLocalPanel.promise;
await act(async () => {
  panelRoot.render(<PanelProbe targetIdentityGen={0} refreshKey={1} />);
  await flushPromises();
});

panelLoader = () => remotePanel.promise;
await act(async () => {
  panelRoot.render(<PanelProbe targetIdentityGen={1} refreshKey={1} />);
  await flushPromises();
});
ok(!panelSummaryText().includes("111,111"), "target identity change clears ContextPanel's same-tab private cache");

await act(async () => {
  lateLocalPanel.resolve(panelInfo(123_123, 12));
  await flushPromises();
});
ok(!panelSummaryText().includes("123,123"), "late Local ContextPanel request cannot paint the new authority");

await act(async () => {
  remotePanel.resolve(panelInfo(222_222, 22));
  await flushPromises();
});
await waitFor("Remote private panel metrics", () => panelSummaryText().includes("222,222"));
ok(panelSummaryText().includes("22"), "ContextPanel renders the Remote request count after authority refresh");

await act(async () => {
  panelRoot.unmount();
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

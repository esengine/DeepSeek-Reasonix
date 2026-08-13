// Run: tsx src/__tests__/use-controller-live-context.test.tsx

import { JSDOM } from "jsdom";
import React, { act, useCallback, useSyncExternalStore } from "react";
import { createRoot } from "react-dom/client";
import { ContextPanel } from "../components/ContextPanel";
import { StatusBar } from "../components/StatusBar";
import { historySliceFromMessages } from "./mockHistorySlice";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { BalanceInfo, ContextInfo, ContextPanelInfo, EffortInfo, Meta, TabMeta, WireEvent } from "../lib/types";
import { useController } from "../lib/useController";

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

function flushPromises(delay = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, delay));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

async function settleUntil(predicate: () => boolean): Promise<boolean> {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return true;
  }
  return false;
}

function tabMeta(): TabMeta {
  return {
    id: "tab-live-context",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    topicId: "topic-live-context",
    topicTitle: "Live context",
    label: "model",
    ready: true,
    running: false,
    cancellable: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
  };
}

function meta(): Meta {
  return {
    label: backendModel,
    ready: true,
    eventChannel: "agent:event",
    cwd: "/repo",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
  };
}

function usageEvent(source = "executor"): WireEvent {
  return {
    kind: "usage",
    tabId: "tab-live-context",
    usage: {
      promptTokens: 100,
      completionTokens: 10,
      totalTokens: 110,
      cacheHitTokens: 90,
      cacheMissTokens: 10,
      sessionCacheHitTokens: 90,
      sessionCacheMissTokens: 10,
      source,
    },
  };
}

console.log("\nuse controller live context refresh");

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
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const eventHandlers: Array<(event: WireEvent) => void> = [];
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
let backendContext: ContextInfo = {
  used: 100,
  window: 1_000,
  sessionTokens: 110,
  cacheHitTokens: 0,
  cacheMissTokens: 100,
};
let contextCalls = 0;
let contextLoader: (() => Promise<ContextInfo>) | undefined;
let backendBalance: BalanceInfo = { available: true, display: "¥88.00" };
let backendModel = "model";
let balanceCalls = 0;
let balanceLoader: (() => Promise<BalanceInfo>) | undefined;
type ModelSwitchStep = {
  gate: ReturnType<typeof deferred<void>>;
  balance?: BalanceInfo;
  error?: Error;
};
const modelSwitchSteps: ModelSwitchStep[] = [];
let modelSwitchCalls = 0;
const stalePanelInfo: ContextPanelInfo = {
  usedTokens: 100,
  windowTokens: 1_000,
  promptTokens: 100,
  completionTokens: 10,
  totalTokens: 110,
  reasoningTokens: 0,
  cacheHitTokens: 0,
  cacheMissTokens: 100,
  sessionCacheHitTokens: 0,
  sessionCacheMissTokens: 100,
  sessionCompletionTokens: 10,
  requestCount: 1,
  elapsedMs: 1_000,
  readFiles: [],
  changedFiles: [],
};

window.runtime = {
  EventsOn: (name: string, cb: (payload: unknown) => void) => {
    if (name === "agent:event") eventHandlers.push(cb as (event: WireEvent) => void);
    return () => {};
  },
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => [tabMeta()],
      MetaForTab: async () => meta(),
      ContextUsageForTab: async () => {
        contextCalls += 1;
        return contextLoader ? contextLoader() : backendContext;
      },
      // Keep this private snapshot deliberately stale. The shared ContextInfo
      // must still keep the panel average aligned with StatusBar during bursts.
      ContextPanel: async () => stalePanelInfo,
      EffortForTab: async () => effort,
      BalanceForTab: async () => {
        balanceCalls += 1;
        return balanceLoader ? balanceLoader() : backendBalance;
      },
      SetModelForTab: async (_tabId, name) => {
        modelSwitchCalls += 1;
        const step = modelSwitchSteps.shift();
        if (!step) throw new Error("missing model switch step");
        await step.gate.promise;
        if (step.error) throw step.error;
        backendModel = name;
        backendBalance = step.balance ?? { available: false, display: "" };
        balanceLoader = undefined;
      },
      CloseTab: async () => {
        throw new Error("cannot close tab");
      },
      JobsForTab: async () => [],
      CheckpointsForTab: async () => [],
      HistoryForTab: async () => [],
      HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
      HistorySliceForTab: async (tabID, req) => historySliceFromMessages(tabID, [], req),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;
let controllerProbeRenders = 0;

function LiveProbe({ value }: { value: Controller }) {
  const subscribe = useCallback(
    (listener: () => void) => value.liveStore.subscribe(value.activeTabId, listener),
    [value.activeTabId, value.liveStore],
  );
  const getSnapshot = useCallback(
    () => value.liveStore.getSnapshot(value.activeTabId)?.text ?? "",
    [value.activeTabId, value.liveStore],
  );
  const getModelActiveAtSnapshot = useCallback(
    () => value.liveStore.getModelActiveAt(value.activeTabId) ?? 0,
    [value.activeTabId, value.liveStore],
  );
  const text = useSyncExternalStore(subscribe, getSnapshot);
  const modelActiveAt = useSyncExternalStore(subscribe, getModelActiveAtSnapshot);
  return (
    <>
      <span data-live-text>{text}</span>
      <span data-live-model-active-at>{modelActiveAt}</span>
    </>
  );
}

function Probe() {
  controller = useController();
  controllerProbeRenders += 1;
  return (
    <LocaleProvider>
      <>
        <StatusBar
          context={controller.state.context}
          usage={controller.state.usage}
          balance={controller.state.balance}
          running={controller.state.running}
          items={["cache_avg", "balance"]}
        />
        <ContextPanel
          tabId={controller.activeTabId}
          context={controller.state.context}
          usage={controller.state.usage}
          sessionTokens={controller.state.sessionTokens}
          sessionCost={controller.state.sessionCost}
          sessionCurrency={controller.state.sessionCurrency}
          turnTokens={controller.state.turnTotalTokens}
          turnCost={controller.state.turnCost}
          sessionGen={controller.state.sessionGen}
          usageSeq={controller.state.usageSeq}
        />
        <LiveProbe value={controller} />
      </>
    </LocaleProvider>
  );
}

function renderedAverage(): string {
  return document.querySelector('[data-statusbar-item="cache_avg"] b')?.textContent ?? "";
}

function renderedPanelAverage(): string {
  return document.querySelector(".context-panel__summary-rows .context-panel__mini-stat strong")?.textContent ?? "";
}

function renderedBalance(): string {
  return document.querySelector('[data-statusbar-item="balance"] b')?.textContent ?? "";
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});

ok(
  await settleUntil(() => controller?.activeTabId === "tab-live-context" && controller.state.context.cacheMissTokens === 100),
  "initial completed-turn context loads",
);
ok(await settleUntil(() => renderedBalance() === "¥88.00"), "initial DeepSeek balance loads");
const initialContextCalls = contextCalls;

backendContext = {
  used: 900,
  window: 1_000,
  sessionTokens: 1_000,
  cacheHitTokens: 900,
  cacheMissTokens: 100,
};
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "turn_started", tabId: "tab-live-context" });
    handler(usageEvent());
  }
  await flushPromises();
});

ok(
  await settleUntil(() => controller?.state.context.cacheHitTokens === 900),
  "executor usage refreshes all-source context before turn_done",
);
eq(renderedAverage(), "90.00%", "status bar renders the live executor-era session average");
eq(renderedPanelAverage(), "90.00%", "panel ignores its stale private snapshot and matches the status bar");
ok(contextCalls > initialContextCalls, "usage triggers a new ContextUsageForTab snapshot");

const rendersBeforeTextBurst = controllerProbeRenders;
await act(async () => {
  for (const handler of eventHandlers) {
    handler({ kind: "text", tabId: "tab-live-context", text: "one " });
    handler({ kind: "text", tabId: "tab-live-context", text: "two " });
    handler({ kind: "text", tabId: "tab-live-context", text: "three" });
  }
  await flushPromises(20);
});
eq(document.querySelector("[data-live-text]")?.textContent, "one two three", "live subscriber receives the coalesced text burst");
ok(
  Number(document.querySelector("[data-live-model-active-at]")?.textContent ?? 0) > 0,
  "live subscriber receives the first provider-output timestamp",
);
eq(controllerProbeRenders, rendersBeforeTextBurst, "pure stream deltas do not re-render the controller owner");

backendContext = {
  used: 960,
  window: 1_000,
  sessionTokens: 1_100,
  cacheHitTokens: 960,
  cacheMissTokens: 40,
};
await act(async () => {
  for (const handler of eventHandlers) handler(usageEvent("subagent"));
  await flushPromises();
});

ok(
  await settleUntil(() => controller?.state.context.cacheHitTokens === 960),
  "subagent usage also refreshes the shared all-source context",
);
eq(renderedAverage(), "96.00%", "status bar renders the live all-source session average");
eq(renderedPanelAverage(), "96.00%", "panel stays aligned after a burst usage update");
eq(controller?.state.usage?.source, "executor", "subagent usage does not replace the executor latest-request metric");

const staleSnapshot = deferred<ContextInfo>();
const latestSnapshot = deferred<ContextInfo>();
const pendingSnapshots = [staleSnapshot.promise, latestSnapshot.promise];
contextLoader = async () => pendingSnapshots.shift() ?? backendContext;
const raceStartCalls = contextCalls;

await act(async () => {
  for (const handler of eventHandlers) handler(usageEvent());
  await flushPromises();
});
ok(await settleUntil(() => contextCalls === raceStartCalls + 1), "first live snapshot starts");

await act(async () => {
  for (const handler of eventHandlers) handler(usageEvent());
  await flushPromises();
});
ok(await settleUntil(() => contextCalls === raceStartCalls + 2), "newer live snapshot starts");

latestSnapshot.resolve({
  used: 990,
  window: 1_000,
  sessionTokens: 1_200,
  cacheHitTokens: 990,
  cacheMissTokens: 10,
});
ok(
  await settleUntil(() => controller?.state.context.cacheHitTokens === 990),
  "newest usage snapshot wins",
);
eq(renderedAverage(), "99.00%", "status bar follows the newest usage snapshot");
eq(renderedPanelAverage(), "99.00%", "panel follows the same newest usage snapshot");

staleSnapshot.resolve({
  used: 100,
  window: 1_000,
  sessionTokens: 200,
  cacheHitTokens: 100,
  cacheMissTokens: 900,
});
await act(async () => {
  await flushPromises();
});
eq(controller?.state.context.cacheHitTokens, 990, "late stale snapshot cannot regress the status bar");
eq(renderedAverage(), "99.00%", "late stale snapshot cannot regress the rendered average");
eq(renderedPanelAverage(), "99.00%", "late stale snapshot cannot regress the panel average");
contextLoader = undefined;

const staleBalance = deferred<BalanceInfo>();
balanceLoader = () => staleBalance.promise;
const balanceRaceStartCalls = balanceCalls;
await act(async () => {
  for (const handler of eventHandlers) handler({ kind: "turn_done", tabId: "tab-live-context" });
  await flushPromises();
});
ok(await settleUntil(() => balanceCalls === balanceRaceStartCalls + 1), "pre-switch balance refresh starts");

const firstSwitchGate = deferred<void>();
const latestSwitchGate = deferred<void>();
modelSwitchSteps.push(
  { gate: firstSwitchGate, balance: { available: true, display: "A 40.00" } },
  { gate: latestSwitchGate, balance: { available: true, display: "B 25.00" } },
);
const modelSwitchStartCalls = modelSwitchCalls;
let firstSwitchPromise: Promise<boolean> | undefined;
let latestSwitchPromise: Promise<boolean> | undefined;
await act(async () => {
  firstSwitchPromise = controller?.setModel("provider-a/model-a");
  latestSwitchPromise = controller?.setModel("provider-b/model-b");
  await flushPromises();
});
eq(renderedBalance(), "-", "starting a hot model switch immediately hides the DeepSeek balance");
eq(modelSwitchCalls, modelSwitchStartCalls + 1, "rapid model switches enter the backend in click order");

firstSwitchGate.resolve();
let firstSwitchResult: boolean | undefined;
await act(async () => {
  firstSwitchResult = await firstSwitchPromise;
  await flushPromises();
});
eq(firstSwitchResult, false, "superseded model switch reports that it no longer owns the UI");
ok(await settleUntil(() => modelSwitchCalls === modelSwitchStartCalls + 2), "latest model switch starts after the older backend call");
eq(renderedBalance(), "-", "superseded switch cannot restore its provider balance");

latestSwitchGate.resolve();
let latestSwitchResult: boolean | undefined;
await act(async () => {
  latestSwitchResult = await latestSwitchPromise;
  await flushPromises();
});
eq(latestSwitchResult, true, "latest model switch owns the completed UI refresh");
ok(await settleUntil(() => renderedBalance() === "B 25.00"), "latest model switch balance wins");

const coalescedFirstGate = deferred<void>();
const coalescedLatestGate = deferred<void>();
modelSwitchSteps.push(
  { gate: coalescedFirstGate, balance: { available: true, display: "G 20.00" } },
  { gate: coalescedLatestGate, balance: { available: true, display: "I 15.00" } },
);
const coalescedStartCalls = modelSwitchCalls;
let coalescedFirst: Promise<boolean> | undefined;
let coalescedMiddle: Promise<boolean> | undefined;
let coalescedLatest: Promise<boolean> | undefined;
await act(async () => {
  coalescedFirst = controller?.setModel("provider-g/model-g");
  coalescedMiddle = controller?.setModel("provider-h/model-h");
  coalescedLatest = controller?.setModel("provider-i/model-i");
  await flushPromises();
});
eq(modelSwitchCalls, coalescedStartCalls + 1, "only the active model switch enters the backend immediately");
eq(await coalescedMiddle, false, "an unstarted intermediate model switch is coalesced");

coalescedFirstGate.resolve();
await act(async () => {
  await coalescedFirst;
  await flushPromises();
});
ok(
  await settleUntil(() => modelSwitchCalls === coalescedStartCalls + 2),
  "the latest coalesced model switch starts after the active call",
);
coalescedLatestGate.resolve();
let coalescedLatestResult: boolean | undefined;
await act(async () => {
  coalescedLatestResult = await coalescedLatest;
  await flushPromises();
});
eq(coalescedLatestResult, true, "the latest coalesced model switch owns reconciliation");
eq(backendModel, "provider-i/model-i", "the skipped intermediate model never reaches the backend");
eq(modelSwitchCalls, coalescedStartCalls + 2, "three rapid picks perform only two controller rebuilds");

staleBalance.resolve({ available: true, display: "¥88.00" });
await act(async () => {
  await flushPromises();
});
eq(renderedBalance(), "I 15.00", "late DeepSeek balance response cannot overwrite the latest switched provider");

const overlappingSuccessGate = deferred<void>();
const overlappingFailureGate = deferred<void>();
modelSwitchSteps.push(
  { gate: overlappingSuccessGate, balance: { available: true, display: "A 40.00" } },
  { gate: overlappingFailureGate, error: new Error("provider B failed") },
);
let overlappingSuccess: Promise<boolean> | undefined;
let overlappingFailure: Promise<boolean> | undefined;
await act(async () => {
  overlappingSuccess = controller?.setModel("provider-a/model-a");
  overlappingFailure = controller?.setModel("provider-b/model-b");
  await flushPromises();
});
overlappingSuccessGate.resolve();
await act(async () => {
  await overlappingSuccess;
  await flushPromises();
});
eq(renderedBalance(), "-", "older successful switch stays hidden while the latest switch is pending");
overlappingFailureGate.resolve();
let overlappingFailureResult: boolean | undefined;
await act(async () => {
  overlappingFailureResult = await overlappingFailure;
  await flushPromises();
});
eq(overlappingFailureResult, false, "latest failed switch reports failure");
ok(
  await settleUntil(() => renderedBalance() === "A 40.00"),
  "failed latest switch refreshes the provider established by the older queued success",
);
eq(
  controller?.state.meta?.label,
  "provider-a/model-a",
  "failed latest switch reconciles metadata from the older queued success",
);

const failedSwitchGate = deferred<void>();
modelSwitchSteps.push({ gate: failedSwitchGate, error: new Error("session is busy") });
balanceLoader = async () => ({ available: false, display: "", err: "balance fetch failed" });
let failedSwitch: Promise<boolean> | undefined;
await act(async () => {
  failedSwitch = controller?.setModel("provider-c/model-c");
  await flushPromises();
});
eq(renderedBalance(), "-", "a failing switch hides the outgoing balance while pending");
failedSwitchGate.resolve();
let failedSwitchResult: boolean | undefined;
await act(async () => {
  failedSwitchResult = await failedSwitch;
  await flushPromises();
});
eq(failedSwitchResult, false, "failed model switch reports failure to its caller");
eq(renderedBalance(), "A 40.00", "failed switch restores the known balance when its confirmation refresh fails");

const firstFailedQueueGate = deferred<void>();
const latestFailedQueueGate = deferred<void>();
modelSwitchSteps.push(
  { gate: firstFailedQueueGate, error: new Error("provider E failed") },
  { gate: latestFailedQueueGate, error: new Error("provider F failed") },
);
const failedQueueStartCalls = modelSwitchCalls;
let firstFailedQueueSwitch: Promise<boolean> | undefined;
let latestFailedQueueSwitch: Promise<boolean> | undefined;
await act(async () => {
  firstFailedQueueSwitch = controller?.setModel("provider-e/model-e");
  latestFailedQueueSwitch = controller?.setModel("provider-f/model-f");
  await flushPromises();
});
eq(renderedBalance(), "-", "queued failing switches keep the outgoing balance hidden while pending");
eq(modelSwitchCalls, failedQueueStartCalls + 1, "queued failing switches enter the backend in click order");

firstFailedQueueGate.resolve();
let firstFailedQueueResult: boolean | undefined;
await act(async () => {
  firstFailedQueueResult = await firstFailedQueueSwitch;
  await flushPromises();
});
eq(firstFailedQueueResult, false, "superseded queued failure does not own balance reconciliation");
ok(
  await settleUntil(() => modelSwitchCalls === failedQueueStartCalls + 2),
  "latest queued failing switch starts after the older failure",
);
eq(renderedBalance(), "-", "superseded queued failure cannot restore the outgoing balance");

latestFailedQueueGate.resolve();
let latestFailedQueueResult: boolean | undefined;
await act(async () => {
  latestFailedQueueResult = await latestFailedQueueSwitch;
  await flushPromises();
});
eq(latestFailedQueueResult, false, "latest queued failure reports failure");
eq(
  renderedBalance(),
  "A 40.00",
  "consecutive queued failures restore the pre-queue balance when confirmation fails",
);
balanceLoader = undefined;

const switchDuringFailedCloseGate = deferred<void>();
modelSwitchSteps.push({
  gate: switchDuringFailedCloseGate,
  balance: { available: true, display: "D 10.00" },
});
let switchDuringFailedClose: Promise<boolean> | undefined;
await act(async () => {
  switchDuringFailedClose = controller?.setModel("provider-d/model-d");
  await flushPromises();
});
await act(async () => {
  await controller?.closeTab("tab-live-context");
  await flushPromises();
});
switchDuringFailedCloseGate.resolve();
let switchDuringFailedCloseResult: boolean | undefined;
await act(async () => {
  switchDuringFailedCloseResult = await switchDuringFailedClose;
  await flushPromises();
});
eq(switchDuringFailedCloseResult, true, "failed close keeps the in-flight model switch current");
eq(controller?.activeTabId, "tab-live-context", "failed close keeps the tab mounted");
eq(
  controller?.state.meta?.label,
  "provider-d/model-d",
  "failed close preserves model metadata reconciliation",
);
ok(
  await settleUntil(() => renderedBalance() === "D 10.00"),
  "failed close preserves model balance reconciliation",
);

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

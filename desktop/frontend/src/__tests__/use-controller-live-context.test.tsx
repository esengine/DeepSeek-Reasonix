// Run: tsx src/__tests__/use-controller-live-context.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextPanel } from "../components/ContextPanel";
import { StatusBar } from "../components/StatusBar";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { ContextInfo, ContextPanelInfo, EffortInfo, Meta, TabMeta, WireEvent } from "../lib/types";
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
    label: "model",
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
      BalanceForTab: async () => ({ available: false, display: "" }),
      JobsForTab: async () => [],
      CheckpointsForTab: async () => [],
      HistoryForTab: async () => [],
      HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;

function Probe() {
  controller = useController();
  return (
    <LocaleProvider>
      <>
        <StatusBar
          context={controller.state.context}
          usage={controller.state.usage}
          running={controller.state.running}
          items={["cache_avg"]}
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

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

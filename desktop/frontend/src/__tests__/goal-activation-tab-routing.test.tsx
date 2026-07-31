// Run: tsx src/__tests__/goal-activation-tab-routing.test.tsx
//
// App/bridge harness for the first Goal + structured Skill path: hang the
// combined backend call for A, switch active to B, then assert the source tab
// and workbench target token stayed fixed.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { activateGoalAndSubmitOnTab } from "../lib/goalSubmit";
import { useController } from "../lib/useController";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, JobView, Meta, TabMeta } from "../lib/types";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function tabMeta(overrides: Partial<TabMeta> = {}): TabMeta {
  return {
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    topicId: "topic-a",
    topicTitle: "A",
    label: "model",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
    ...overrides,
  };
}

function metaFor(tab: TabMeta): Meta {
  return {
    label: tab.label,
    ready: tab.ready,
    startupErr: tab.startupErr,
    eventChannel: "agent:event",
    cwd: tab.cwd || tab.workspaceRoot,
    workspaceRoot: tab.workspaceRoot,
    workspaceName: tab.workspaceName,
    workspacePath: tab.workspacePath,
    gitBranch: tab.gitBranch,
    autoApproveTools: false,
    bypass: false,
    collaborationMode: tab.collaborationMode ?? "normal",
    toolApprovalMode: tab.toolApprovalMode ?? "ask",
    tokenMode: tab.tokenMode ?? "full",
    goal: tab.goal ?? "",
    goalStatus: tab.goal ? "running" : "stopped",
  };
}

console.log("\ngoal activation tab routing");

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

const tabA = tabMeta({ id: "tab-a", topicId: "topic-a", topicTitle: "A", active: true });
const tabB = tabMeta({ id: "tab-b", topicId: "topic-b", topicTitle: "B", active: false, cwd: "/repo-b", workspaceRoot: "/repo-b", workspacePath: "/repo-b" });
let tabs: TabMeta[] = [tabA, tabB];

const initialGoalCalls: string[] = [];
let releaseGoal!: () => void;
const goalGate = new Promise<void>((resolve) => {
  releaseGoal = resolve;
});

const context: ContextInfo = { used: 0, window: 100, sessionTokens: 0 };
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];

window.runtime = {
  EventsOn: () => () => {},
  BrowserOpenURL: () => {},
};
window.go = {
  main: {
    App: {
      ListTabs: async () => tabs.map((tab) => ({ ...tab })),
      SetActiveTab: async (tabId: string) => {
        tabs = tabs.map((tab) => ({ ...tab, active: tab.id === tabId }));
      },
      MetaForTab: async (tabId: string) => {
        const tab = tabs.find((entry) => entry.id === tabId) ?? tabA;
        return metaFor(tab);
      },
      ContextUsageForTab: async () => context,
      EffortForTab: async () => effort,
      BalanceForTab: async () => balance,
      JobsForTab: async () => jobs,
      CheckpointsForTab: async () => checkpoints,
      HistoryForTab: async (): Promise<HistoryMessage[]> => [],
      HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
      HistoryCheckpointTurnsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      SetGoalForTab: async (tabID: string, goal: string) => {
        tabs = tabs.map((tab) =>
          tab.id === tabID
            ? { ...tab, goal, collaborationMode: goal ? "goal" : "normal" }
            : tab,
        );
      },
      SubmitInitialGoalToTab: async (
        tabID: string,
        goal: string,
        display: string,
        _input: string,
        invocations: { name: string }[],
        collaborationMode: string,
        toolApprovalMode: string,
        targetKind: string,
        targetIdentityGen: number,
        targetRequestSeq: number,
      ): Promise<string[]> => {
        await goalGate;
        initialGoalCalls.push(
          `${tabID}:${goal}:${display}:${invocations[0]?.name ?? ""}:${collaborationMode}:${toolApprovalMode}:${targetKind}:${targetIdentityGen}:${targetRequestSeq}`,
        );
        tabs = tabs.map((tab) =>
          tab.id === tabID
            ? { ...tab, goal, collaborationMode: goal ? "goal" : "normal" }
            : tab,
        );
        return [];
      },
      SubmitInvocationsToTab: async () => {
        throw new Error("split SubmitInvocationsToTab must not be used for initial Goals");
      },
      SubmitToTab: async () => {
        throw new Error("plain SubmitToTab must not be used for structured first Goal turns");
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;

function Probe() {
  controller = useController();
  return null;
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(<Probe />);
  await flushPromises();
});

await waitForActive("tab-a");
eq(controller?.activeTabId, "tab-a", "harness starts on tab A");

const sourceTabId = "tab-a";
const pending = activateGoalAndSubmitOnTab({
  tabId: sourceTabId,
  target: { kind: "ssh", identityGen: 7, requestSeq: 11 },
  displayText: "Cross-tab safe goal",
  submitText: "/ui-ux-pro-max Cross-tab safe goal",
  structured: {
    display: "/ui-ux-pro-max Cross-tab safe goal",
    input: "Cross-tab safe goal",
    invocations: [{ name: "ui-ux-pro-max", kind: "skill", offset: 0 }],
  },
  sendToTab: (tabId, goal, display, submit, structured, target) => {
    if (!controller) throw new Error("controller missing");
    if (!target) throw new Error("target missing");
    return controller.sendToTab(tabId, display, submit, undefined, structured, {
      goal,
      target,
      collaborationMode: "normal",
      toolApprovalMode: "ask",
    });
  },
});

// While the atomic bridge call for A is suspended, the UI switches to tab B.
// The in-flight call must keep both its tab and target token.
await act(async () => {
  await controller?.switchTab("tab-b", tabB);
  await flushPromises();
});
eq(controller?.activeTabId, "tab-b", "active tab switched to B during deferred Goal activation");

releaseGoal();
await act(async () => {
  await pending;
  await flushPromises();
});

eq(
  initialGoalCalls.join("|"),
  "tab-a:Cross-tab safe goal:/ui-ux-pro-max Cross-tab safe goal:ui-ux-pro-max:normal:ask:ssh:7:11",
  "atomic Goal submit kept source tab A and its Remote target token",
);
eq(initialGoalCalls.length, 1, "atomic Goal submit ran once");

// Bridge failure path: controller must reject without falling back to the split
// structured submit.
const failedInitialGoalCalls: string[] = [];
const failInvokeCalls: string[] = [];
(window.go.main.App as AppBindings).SubmitInitialGoalToTab = async (tabID: string) => {
  failedInitialGoalCalls.push(tabID);
  throw new Error("workbench target changed");
};
(window.go.main.App as AppBindings).SubmitInvocationsToTab = async (tabID: string) => {
  failInvokeCalls.push(tabID);
};

let activationFailed = false;
await act(async () => {
  try {
    await activateGoalAndSubmitOnTab({
      tabId: "tab-a",
      target: { kind: "ssh", identityGen: 7, requestSeq: 11 },
      displayText: "Must not run skill",
      submitText: "/ui-ux-pro-max Must not run skill",
      structured: {
        display: "/ui-ux-pro-max Must not run skill",
        input: "Must not run skill",
        invocations: [{ name: "ui-ux-pro-max", kind: "skill", offset: 0 }],
      },
      sendToTab: (tabId, goal, display, submit, structured, target) =>
        controller!.sendToTab(tabId, display, submit, undefined, structured, {
          goal,
          target: target!,
          collaborationMode: "normal",
          toolApprovalMode: "ask",
        }),
    });
  } catch (error) {
    activationFailed = error instanceof Error && error.message.includes("workbench target changed");
  }
  await flushPromises();
});
eq(activationFailed, true, "controller propagates atomic Goal bridge rejection");
eq(failedInitialGoalCalls.join("|"), "tab-a", "failed atomic submit still targeted source tab A");
eq(failInvokeCalls.length, 0, "failed atomic Goal submit does not call split SubmitInvocationsToTab");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

async function waitForActive(tabId: string) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (controller?.activeTabId === tabId) return;
    await act(async () => {
      await flushPromises();
    });
  }
  throw new Error(`timed out waiting for active tab ${tabId}`);
}

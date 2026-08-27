// Run: node --import ./scripts/svg-stub-register.mjs --import tsx src/__tests__/remote-switch-from-local.test.tsx
//
// Local → remote activation must not run HistorySliceForTab and must not
// revert to the previous local tab when that local hydrate fails.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import { useController } from "../lib/useController";
import type { HistorySlice, HistorySliceRequest, TabMeta } from "../lib/types";

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
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}
function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}
async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    await act(async () => { await flushPromises(); });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame?.bind(dom.window) ?? ((cb: FrameRequestCallback) => setTimeout(() => cb(Date.now()), 16) as unknown as number);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame?.bind(dom.window) ?? ((handle: number) => clearTimeout(handle));

const localTab: TabMeta = {
  id: "tab-local",
  scope: "project",
  workspaceRoot: "/repo/local",
  workspaceName: "local",
  workspacePath: "/repo/local",
  topicId: "topic-local",
  topicTitle: "local",
  sessionPath: "/repo/local/sessions/local.jsonl",
  label: "local-model",
  ready: true,
  running: false,
  mode: "normal",
  toolApprovalMode: "ask",
  tokenMode: "full",
  active: true,
  cwd: "/repo/local",
};
const remoteTab: TabMeta = {
  id: "tab-remote",
  scope: "project",
  workspaceRoot: "~/app",
  workspaceName: "app",
  topicId: "",
  topicTitle: "remote session",
  label: "gpu-box",
  ready: true,
  running: false,
  mode: "normal",
  toolApprovalMode: "ask",
  tokenMode: "full",
  active: false,
  cwd: "~/app",
  remote: { hostId: "gpu-box", workspace: "~/app" },
  remoteState: "ready",
};

let backendActiveId = "tab-local";
let historySliceCalls: string[] = [];
let setActiveCalls: string[] = [];

window.go = {
  main: {
    App: {
      ListTabs: async () => {
        const tabs = [localTab, remoteTab].map((tab) => ({ ...tab, active: tab.id === backendActiveId }));
        return tabs;
      },
      SetActiveTab: async (tabID: string) => {
        setActiveCalls.push(tabID);
        backendActiveId = tabID;
      },
      HistorySliceForTab: async (tabID: string, _req: HistorySliceRequest): Promise<HistorySlice> => {
        historySliceCalls.push(tabID);
        if (tabID === "tab-remote") {
          return {
            messages: [],
            startTurn: 0,
            endTurn: 0,
            hasOlder: false,
            hasNewer: false,
            error: "session path unavailable before controller ready",
          };
        }
        return {
          messages: [{ role: "user", content: "local hello" }],
          startTurn: 1,
          endTurn: 1,
          hasOlder: false,
          hasNewer: false,
        };
      },
      MetaForTab: async () => ({ label: "local-model", ready: true, eventChannel: "agent:event", cwd: "/repo/local" }),
      ContextUsageForTab: async () => ({ used: 0, window: 0 }),
      EffortForTab: async () => ({ level: "high", supported: ["high"] }),
      BalanceForTab: async () => ({ available: false, display: "" }),
      JobsForTab: async () => [],
      CheckpointsForTab: async () => [],
      ReplayPendingPrompts: async () => {},
      ReportUIReady: async () => {},
    } as Partial<AppBindings> as AppBindings,
  },
};

type ControllerApi = {
  activeTabId?: string;
  switchTab: (tabId: string, optimisticTab?: TabMeta) => Promise<unknown>;
};

let controller: ControllerApi | undefined;
function Probe() {
  controller = useController() as unknown as ControllerApi;
  return null;
}
const root = createRoot(document.getElementById("root")!);
await act(async () => { root.render(<Probe />); });
await waitFor("local active", () => controller?.activeTabId === "tab-local");

historySliceCalls = [];
setActiveCalls = [];
await act(async () => {
  await controller?.switchTab(remoteTab.id, remoteTab);
  await flushPromises();
});
await waitFor("remote stays active", () => controller?.activeTabId === "tab-remote");

eq(controller?.activeTabId, "tab-remote", "switching to a remote tab keeps the remote tab active");
ok(!historySliceCalls.includes("tab-remote"), "remote switch does not request local HistorySliceForTab");
ok(!setActiveCalls.includes("tab-local"), "failed local hydrate must not revert SetActiveTab back to the local tab");
eq(setActiveCalls[0], "tab-remote", "SetActiveTab is called for the remote tab");

await act(async () => { root.unmount(); });
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);

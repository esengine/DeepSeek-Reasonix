// Run: tsx src/__tests__/remote-tab-opened.test.tsx

import { JSDOM } from "jsdom";
import React, { act, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { __emitMockRemoteTab, __emitMockRemoteTabOpened, __emitMockRemoteTabUpdated, app } from "../lib/bridge";
import type { TabMeta } from "../lib/types";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import { useRemoteSession } from "../lib/useRemoteSession";
import { useRemoteTabOpened } from "../lib/useRemoteTabOpened";
import { useRemoteTabSwitch } from "../lib/useRemoteTabSwitch";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nRemote tab opened/updated routing");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;

const seeded: string[] = [];
const updated: string[] = [];
const switched: string[] = [];
const remoteMeta: TabMeta = {
  id: "remote-1",
  scope: "project",
  workspaceRoot: "remote-project:host-a:/repo",
  workspaceName: "repo",
  workspacePath: "/repo",
  gitBranch: "main",
  label: "model",
  ready: true,
  running: false,
  active: false,
  remote: { hostId: "host-a", workspace: "/repo" },
};

function Harness() {
  const activeTabIdRef = useRef<string | undefined>("local-1");
  useRemoteTabOpened(
    activeTabIdRef,
    (meta) => seeded.push(meta.id),
    (meta) => updated.push(meta.id),
    async (meta) => {
      switched.push(meta.id);
    },
  );
  return null;
}

const root = createRoot(document.getElementById("root")!);
await act(async () => root.render(<Harness />));
await act(async () => __emitMockRemoteTabOpened(remoteMeta));
eq(seeded.join(","), "remote-1", "opened events seed the new remote tab metadata");
eq(switched.join(","), "remote-1", "opened events activate through the dedicated remote switch");

await act(async () => __emitMockRemoteTabUpdated({ ...remoteMeta, topicTitle: "Background title" }));
eq(updated.join(","), "remote-1", "metadata updates patch the remote tab");
eq(switched.join(","), "remote-1", "metadata updates never steal focus");

await act(async () => root.unmount());

let directSwitch: ((meta: TabMeta) => Promise<void>) | undefined;
let historyCalls = 0;
let activeCalls = 0;
const originalHistory = app.HistorySliceForTab;
const previousGo = window.go;
window.go = { main: { App: {
  HistorySliceForTab: async (...args: Parameters<typeof originalHistory>) => {
  historyCalls += 1;
  return originalHistory(...args);
  },
  SetActiveTab: async () => { activeCalls += 1; },
} as unknown as typeof app } } as typeof window.go;

function SwitchHarness() {
  const [activeId, setActiveId] = useState<string | undefined>("local-1");
  const activeIdRef = useRef<string | undefined>(activeId);
  activeIdRef.current = activeId;
  directSwitch = useRemoteTabSwitch({
    activeTabIdRef: activeIdRef,
    setActiveTabId: setActiveId,
    beginNavigation: () => 1,
    navigationCanComplete: () => true,
    navigationIsCurrent: () => true,
    confirmBackendActiveTab: () => undefined,
    reassertVisibleTab: async () => undefined,
  });
  return <span data-active-id={activeId} />;
}

const switchRoot = createRoot(document.getElementById("root")!);
await act(async () => switchRoot.render(<SwitchHarness />));
await act(async () => directSwitch?.(remoteMeta));
eq(document.querySelector("span")?.getAttribute("data-active-id"), "remote-1", "remote activation updates the selected tab");
eq(activeCalls, 1, "remote activation still binds backend focus");
eq(historyCalls, 0, "remote activation bypasses local history hydration");
await act(async () => switchRoot.unmount());

let terminalProbe: RemoteSessionApi | undefined;
window.go = { main: { App: {
  RemoteTabSnapshot: async () => { throw new Error("serve unavailable"); },
  SetActiveTab: async (tabId: string) => {
    __emitMockRemoteTab(tabId, "state", { state: "serve_down", error: "bootstrap failed" });
  },
} as unknown as typeof app } } as typeof window.go;
function TerminalHarness() { terminalProbe = useRemoteSession("remote-terminal", "disconnected"); return null; }
const terminalRoot = createRoot(document.getElementById("root")!);
await act(async () => terminalRoot.render(<TerminalHarness />));
eq(terminalProbe?.state, "serve_down", "restored shell observes terminal state republished during activation");
await act(async () => terminalRoot.unmount());
window.go = previousGo;
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);

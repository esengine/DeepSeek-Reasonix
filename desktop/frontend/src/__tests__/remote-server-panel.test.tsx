// Run: node --import ./scripts/svg-stub-register.mjs --import tsx src/__tests__/remote-server-panel.test.tsx
//
// Tests the Remote SSH Server tab of the right-dock panel: the single
// "Open Remote Web" entry, Serve progress states, the workspace home-directory
// fallback, and the fixed remote-provider hint.

import React from "react";
import { JSDOM } from "jsdom";

import type { AppBindings } from "../lib/bridge";

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

console.log("\nRemote SSH Server tab (Open Remote Web)");
const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });

const [{ createRoot }, { RemotePanel }, { LocaleProvider }, { useRemoteStore }, { __emitMockRemote, onRemoteServer, onRemoteStatus }] = await Promise.all([
  import("react-dom/client"),
  import("../components/RemotePanel"),
  import("../lib/i18n"),
  import("../store/remote"),
  import("../lib/bridge"),
]);

// The production subscription lives in App.tsx; wire the store to the mock
// event fan-out here so remote:server progress reaches the panel.
onRemoteServer((s) => useRemoteStore.getState().setServer(s));
onRemoteStatus((s) => useRemoteStore.getState().applyStatus(s));

const openCalls: Array<{ hostId: string; workspace: string }> = [];
const stopCalls: Array<{ hostId: string; workspace: string }> = [];
const statusCalls: Array<{ hostId: string; workspace: string }> = [];
window.go = { main: { App: {
  async RemoteLastWorkspace(hostId: string) {
    return hostId === "box" ? "/srv/app" : "";
  },
  async RemoteServerStatus(hostId: string, workspace: string) {
    statusCalls.push({ hostId, workspace });
    // Echo the registered per-workspace view when one exists (the bound call
    // returns the tracked entry), else a fresh stopped view.
    return useRemoteStore.getState().servers[hostId]?.[workspace]
      ?? { hostId, workspace, state: "stopped" };
  },
  async RemoteServerLogs() {
    return "";
  },
  async OpenRemoteWorkspace(hostId: string, workspace: string) {
    openCalls.push({ hostId, workspace });
  },
  async StopRemoteServer(hostId: string, workspace: string) {
    stopCalls.push({ hostId, workspace });
  },
} as Partial<AppBindings> as AppBindings } };

const host = { id: "box", label: "box", host: "box.test", port: 22, user: "dev", identityFile: "", proxyJump: "", defaultWorkspace: "/srv/app", serveInstall: "auto", credentialMode: "remote", useSSHConfig: false };
useRemoteStore.getState().setHosts([host]);
useRemoteStore.getState().openExplorer("box");
useRemoteStore.getState().setExplorerTab("server");
useRemoteStore.getState().applyStatus({ hostId: "box", state: "connected" });

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const { act } = await import("react");
const root = createRoot(rootElement);

await act(async () => {
  root.render(
    <LocaleProvider>
      <RemotePanel onClose={() => {}} />
    </LocaleProvider>,
  );
  await Promise.resolve();
});

ok(document.body.textContent?.includes("Open Remote Web") === true, "server tab shows the unified Open Remote Web entry");
ok(
  document.body.textContent?.includes("Models, API keys, and sessions are managed by the Reasonix configuration on the remote server.") === true,
  "server tab states that providers, API keys, and sessions are managed by the remote Reasonix configuration",
);

// Serve progress: remote:server events drive the busy state and label.
await act(async () => {
  __emitMockRemote("server", { hostId: "box", workspace: "/srv/app", state: "starting", message: "detecting platform" });
  await Promise.resolve();
});
ok(document.body.textContent?.includes("Starting") === true, "Serve progress renders while starting");
const openButton = () => Array.from(document.querySelectorAll("button")).find((b) => b.textContent?.includes("Open Remote Web"));
ok(openButton()?.hasAttribute("disabled") === true, "Open Remote Web is disabled while Serve is starting");

await act(async () => {
  __emitMockRemote("server", { hostId: "box", workspace: "/srv/app", state: "ready", localUrl: "http://127.0.0.1:54321/" });
  await Promise.resolve();
});
ok(document.body.textContent?.includes("Ready") === true, "Serve ready state renders");
ok(openButton()?.hasAttribute("disabled") === false, "Open Remote Web is enabled once Serve is ready");

// Clicking the entry opens (or re-points) the host web window with the
// resolved workspace.
await act(async () => {
  openButton()?.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await Promise.resolve();
});
ok(
  openCalls.length === 1 && openCalls[0].hostId === "box" && openCalls[0].workspace === "/srv/app",
  `Open Remote Web calls OpenRemoteWorkspace with the last workspace (got ${JSON.stringify(openCalls)})`,
);

// Fresh host: without a configured or last workspace, the SSH login user's
// home directory is a safe, enterable zero-configuration fallback.
await act(async () => {
  useRemoteStore.getState().openExplorer("bare");
  useRemoteStore.getState().setExplorerTab("server");
  useRemoteStore.getState().setHosts([
    host,
    { id: "bare", label: "bare", host: "bare.test", port: 22, user: "dev", identityFile: "", proxyJump: "", defaultWorkspace: "", serveInstall: "auto", credentialMode: "remote", useSSHConfig: false },
  ]);
  useRemoteStore.getState().applyStatus({ hostId: "bare", state: "connected" });
  await Promise.resolve();
});
const workspaceInput = document.querySelector<HTMLInputElement>('input[placeholder="~"]');
ok(workspaceInput?.value === "~", "fresh host defaults its workspace to the SSH login home");
ok(openButton()?.hasAttribute("disabled") === false, "Open Remote Web is enabled for a fresh host");

await act(async () => {
  openButton()?.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await Promise.resolve();
});
ok(
  openCalls.length === 2 && openCalls[1].hostId === "bare" && openCalls[1].workspace === "~",
  `fresh host opens the SSH login home (got ${JSON.stringify(openCalls)})`,
);

// ── Per-workspace management: two registered serves on one host ──
await act(async () => {
  useRemoteStore.getState().openExplorer("box");
  useRemoteStore.getState().setExplorerTab("server");
  await Promise.resolve();
});
await act(async () => {
  __emitMockRemote("server", { hostId: "box", workspace: "/srv/app", state: "ready" });
  __emitMockRemote("server", { hostId: "box", workspace: "/srv/web", state: "ready" });
  await Promise.resolve();
});
const stopButton = () => Array.from(document.querySelectorAll("button")).find((b) => b.textContent?.includes("Stop"));
ok(statusCalls.some((c) => c.hostId === "box" && c.workspace === "/srv/app"), "status is fetched per workspace (the input's workspace)");
await act(async () => {
  stopButton()?.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await Promise.resolve();
});
ok(
  stopCalls.length === 1 && stopCalls[0].hostId === "box" && stopCalls[0].workspace === "/srv/app",
  `stop targets the managed workspace (got ${JSON.stringify(stopCalls)})`,
);
// Switching the input switches which registered serve the panel manages; the
// other workspace's entry is untouched in the store.
{
  const wsInput = document.querySelector<HTMLInputElement>('input[placeholder="~"]');
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  await act(async () => {
    setter?.call(wsInput, "/srv/web");
    wsInput?.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
    await Promise.resolve();
  });
  ok(stopButton()?.hasAttribute("disabled") === false, "switching the workspace keeps the other ready serve manageable");
  await act(async () => {
    stopButton()?.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
    await Promise.resolve();
  });
  ok(
    stopCalls.length === 2 && stopCalls[1].workspace === "/srv/web",
    `stop follows the workspace switch (got ${JSON.stringify(stopCalls)})`,
  );
  const servers = useRemoteStore.getState().servers;
  ok(
    servers.box?.["/srv/app"]?.state === "ready" && servers.box?.["/srv/web"]?.state === "ready",
    "both workspaces keep independent entries in the store",
  );
}

await act(async () => { root.unmount(); });
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);

// Run: node --import ./scripts/svg-stub-register.mjs --import tsx src/__tests__/remote-serve-update-banner.test.tsx
//
// Covers the remote Serve update surfaces: the in-session drift banner
// (show, two-click upgrade, ignore persistence, updating state) and the
// Remote panel's version line plus its update entry.

import React from "react";
import { JSDOM } from "jsdom";

import type { AppBindings } from "../lib/bridge";
import type { RemoteServerView } from "../lib/remoteTypes";
import { installDesktopHostStub } from "./desktopHostStub";

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

console.log("\nRemote Serve update (banner + panel)");
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

const [{ createRoot }, { RemoteServeUpdateBanner }, { RemotePanel }, { LocaleProvider }, { useRemoteStore }] = await Promise.all([
  import("react-dom/client"),
  import("../components/RemoteServeUpdateBanner"),
  import("../components/RemotePanel"),
  import("../lib/i18n"),
  import("../store/remote"),
]);

const updateCalls: Array<{ hostId: string; workspace: string }> = [];
let updateBehavior: () => Promise<void> = async () => {};
installDesktopHostStub(({
  main: {
    App: {
      async RegisterNavigationIntent() {},
      async RemoteLastWorkspace() { return "/srv/app"; },
      async RemoteServerStatus(hostId: string, workspace: string) {
        return useRemoteStore.getState().servers[hostId]?.[workspace] ?? { hostId, workspace, state: "stopped" };
      },
      async RemoteServerLogs() { return ""; },
      async OpenRemoteWorkspace() {},
      async StopRemoteServer() {},
      async UpdateRemoteServer(hostId: string, workspace: string) {
        updateCalls.push({ hostId, workspace });
        await updateBehavior();
      },
    } as Partial<AppBindings> as AppBindings,
  },
}).main.App);

function readyView(overrides: Partial<RemoteServerView> = {}): RemoteServerView {
  return {
    hostId: "box",
    workspace: "/srv/app",
    state: "ready",
    localUrl: "http://127.0.0.1:1/",
    serveVersion: "1.9.0",
    updateAvailable: true,
    ...overrides,
  };
}

const { act } = await import("react");
const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");

// ── Banner ──
window.localStorage.clear();
let root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView());
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
const bannerText = () => document.body.textContent ?? "";
ok(bannerText().includes("v1.9.0") && bannerText().includes("older than this desktop"), "banner shows the drifted serve version");

const findButton = (label: string) => Array.from(document.querySelectorAll("button")).find((b) => b.textContent === label);
await act(async () => {
  findButton("Update remote Serve")?.click();
  await Promise.resolve();
});
ok(findButton("Interrupt and update") !== undefined, "first upgrade click arms instead of running");
await act(async () => {
  findButton("Interrupt and update")?.click();
  await Promise.resolve();
});
ok(updateCalls.length === 1 && updateCalls[0].hostId === "box" && updateCalls[0].workspace === "/srv/app", "second click runs the update with the serve identity");

await act(async () => root.unmount());
updateCalls.length = 0;

// A fresh serve clears the banner.
root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ serveVersion: "1.9.5", updateAvailable: false }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
ok(bannerText() === "", "banner hides once the serve reports no update");

// Ignore persists per serve version.
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ serveVersion: "1.8.9" }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
await act(async () => {
  findButton("Ignore")?.click();
  await Promise.resolve();
});
ok(bannerText() === "", "ignore hides the banner immediately");
await act(async () => root.unmount());
root = createRoot(rootElement);
await act(async () => {
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
ok(bannerText() === "", "ignore persists across remounts for the same serve version");
await act(async () => root.unmount());
window.localStorage.clear();

// While the update runs, both actions are disabled.
root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ state: "updating", message: "stopping serve" }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
const updatingButtons = Array.from(document.querySelectorAll("button"));
ok(
  updatingButtons.length > 0 && updatingButtons.every((b) => b.hasAttribute("disabled")),
  "updating state disables every banner action",
);
ok(bannerText().includes("Updating"), "banner shows the updating label");
await act(async () => root.unmount());

// ── Panel ──
const host = { id: "box", label: "box", host: "box.test", port: 22, user: "dev", identityFile: "", proxyJump: "", defaultWorkspace: "/srv/app", serveInstall: "auto", credentialMode: "remote", useSSHConfig: false };
useRemoteStore.getState().setHosts([host]);
useRemoteStore.getState().openExplorer("box");
useRemoteStore.getState().setExplorerTab("server");
useRemoteStore.getState().applyStatus({ hostId: "box", state: "connected" });
useRemoteStore.getState().setServer(readyView());
root = createRoot(rootElement);
await act(async () => {
  root.render(
    <LocaleProvider>
      <RemotePanel onClose={() => {}} />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
ok((document.body.textContent ?? "").includes("v1.9.0"), "panel status line shows the serve version");
await act(async () => {
  findButton("Update")?.click();
  await Promise.resolve();
});
ok(findButton("Interrupt and update") !== undefined, "panel update arms in place");
await act(async () => {
  findButton("Interrupt and update")?.click();
  await Promise.resolve();
});
ok(updateCalls.length === 1, "panel confirm drives the update once");
await act(async () => root.unmount());

// A failed update surfaces its error in the banner and stays retryable.
window.localStorage.clear();
updateCalls.length = 0;
updateBehavior = () => Promise.reject(new Error('forced upgrade install failed: no release for "9.9.9"'));
useRemoteStore.getState().setServer(readyView());
root = createRoot(rootElement);
await act(async () => {
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
await act(async () => {
  findButton("Update remote Serve")?.click();
  await Promise.resolve();
});
await act(async () => {
  findButton("Interrupt and update")?.click();
  await Promise.resolve();
});
ok(
  bannerText().includes("Update failed:") && bannerText().includes("9.9.9"),
  "a failed update surfaces its error in the banner",
);
ok(findButton("Update remote Serve") !== undefined, "the banner stays retryable after a failure");
await act(async () => root.unmount());

// Ignore holds across remounts even when persistent storage is unavailable.
const realStorage = window.localStorage;
Object.defineProperty(window, "localStorage", {
  configurable: true,
  value: { getItem: () => null, setItem: () => { throw new Error("quota exceeded"); }, removeItem: () => {} } as Storage,
});
root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ serveVersion: "1.8.8" }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
await act(async () => {
  findButton("Ignore")?.click();
  await Promise.resolve();
});
ok(bannerText() === "", "ignore hides the banner without persistent storage");
await act(async () => root.unmount());
root = createRoot(rootElement);
await act(async () => {
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
ok(bannerText() === "", "the session fallback keeps the dismissal across remounts");
await act(async () => root.unmount());
Object.defineProperty(window, "localStorage", { configurable: true, value: realStorage });

// Transient state is scoped per host: ignoring on one remote does not hide
// the banner on another running the same old version.
window.localStorage.clear();
root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ hostId: "box-a", serveVersion: "1.8.7" }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box-a" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
await act(async () => {
  findButton("Ignore")?.click();
  await Promise.resolve();
});
await act(async () => root.unmount());
root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ hostId: "box-b", serveVersion: "1.8.7" }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box-b" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
ok(bannerText().includes("v1.8.7"), "an ignored version on one host must not hide the banner on another");
await act(async () => root.unmount());

// A full store keeps the newest dismissal and evicts the oldest entry.
window.localStorage.clear();
const filler = Array.from({ length: 200 }, (_, i) => `filler-${i}`);
window.localStorage.setItem("remote.serveUpdate.dismissed", JSON.stringify(filler));
root = createRoot(rootElement);
await act(async () => {
  useRemoteStore.getState().setServer(readyView({ serveVersion: "1.8.6" }));
  root.render(
    <LocaleProvider>
      <RemoteServeUpdateBanner hostId="box" workspace="/srv/app" />
    </LocaleProvider>,
  );
  await Promise.resolve();
});
await act(async () => {
  findButton("Ignore")?.click();
  await Promise.resolve();
});
const stored = JSON.parse(window.localStorage.getItem("remote.serveUpdate.dismissed") ?? "[]") as string[];
ok(stored.length === 200, `a full store stays at 200 entries, got ${stored.length}`);
ok(stored.includes("box|/srv/app|1.8.6"), "the newest dismissal survives the prune");
ok(!stored.includes("filler-0"), "the oldest stored entry is the one evicted");
await act(async () => root.unmount());
window.localStorage.clear();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

// Run: tsx src/__tests__/browser-panel-store.test.ts
import assert from "node:assert/strict";

import { normalizeAddress, zoomStep } from "../lib/browserAddress";
import type { BrowserDownloadView, BrowserTabView, DesktopBrowserHost } from "../lib/browserHost";
import { selectActiveTab, selectAddress, USER_TASK_ID, useBrowserPanelStore } from "../lib/browserPanelStore";

const tab = (overrides: Partial<BrowserTabView>): BrowserTabView => ({
  id: "t", taskId: "A", url: "https://example.com/", title: "Example", loading: false, canGoBack: false, canGoForward: false,
  temporary: false, mode: "agent", epoch: 0, zoom: 1, error: null, ...overrides,
});
const download = (overrides: Partial<BrowserDownloadView>): BrowserDownloadView => ({
  id: "d", tabId: "t", url: "https://example.com/file.zip", filename: "file.zip", path: "/tmp/file.zip",
  state: "progressing", received: 0, total: 100, ...overrides,
});
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function fakeHost(initial: BrowserTabView[] = []) {
  const calls: string[] = [];
  const fail: Partial<Record<"open" | "close" | "navigate" | "takeover", Error>> = {};
  let tabsCb: ((tabs: BrowserTabView[]) => void) | null = null;
  let downloadCb: ((entry: BrowserDownloadView) => void) | null = null;
  const host: DesktopBrowserHost = {
    list: async () => { calls.push("list"); return initial; },
    open: async (url, opts) => {
      calls.push(`open ${url} ${opts?.taskId} ${opts?.temporary}`);
      if (fail.open) throw fail.open;
      return tab({ id: `new:${url}`, url, taskId: opts?.taskId ?? "" });
    },
    close: async (id) => { calls.push(`close ${id}`); if (fail.close) throw fail.close; },
    activate: async (id) => { calls.push(`activate ${id}`); },
    navigate: async (id, target) => { calls.push(`navigate ${id} ${JSON.stringify(target)}`); if (fail.navigate) throw fail.navigate; },
    setZoom: async (id, factor) => { calls.push(`zoom ${id} ${factor}`); },
    toggleDevTools: async (id) => { calls.push(`devtools ${id}`); },
    resume: async (id) => { calls.push(`resume ${id}`); },
    takeover: async (id) => { calls.push(`takeover ${id}`); if (fail.takeover) throw fail.takeover; },
    setLayout: () => {},
    setOverlay: () => {},
    onTabs: (cb) => { tabsCb = cb; return () => { tabsCb = null; }; },
    onDownload: (cb) => { downloadCb = cb; return () => { downloadCb = null; }; },
  };
  return {
    host, calls, fail,
    emitTabs: (tabs: BrowserTabView[]) => tabsCb?.(tabs),
    emitDownload: (entry: BrowserDownloadView) => downloadCb?.(entry),
    get subscribed() { return tabsCb !== null; },
  };
}

const reset = () => useBrowserPanelStore.setState(useBrowserPanelStore.getInitialState(), true);
const store = () => useBrowserPanelStore.getState();
const notices: string[] = [];
const notify = (message: string) => { notices.push(message); };

console.log("\nbrowser panel store");

{
  reset();
  const fake = fakeHost();
  const detach = store().attach(fake.host, notify);
  await tick();
  fake.emitTabs([tab({ id: "a1", taskId: "A" }), tab({ id: "b1", taskId: "B" }), tab({ id: "u1", taskId: USER_TASK_ID })]);
  store().setTaskId("A");
  store().setVisible(true);
  assert.deepEqual(store().shown.map((entry) => entry.id), ["a1", "u1"], "current task tabs plus user tabs are shown");
  assert.equal(store().activeTabId, "u1", "the newest shown tab becomes active");
  assert.deepEqual(fake.calls.filter((call) => call.startsWith("activate")), ["activate u1"]);
  store().setTaskId("B");
  assert.deepEqual(store().shown.map((entry) => entry.id), ["b1", "u1"]);
  assert.equal(store().activeTabId, "u1", "a still-shown active tab survives a task switch");
  assert.equal(fake.calls.filter((call) => call.startsWith("activate")).length, 1, "unchanged activation is not resent");
  store().activate("b1");
  assert.equal(selectActiveTab(store())?.id, "b1");
  store().setTaskId("A");
  assert.equal(store().activeTabId, "u1", "another task's active tab is never shown for this task");
  store().activate("a1");
  store().setVisible(false);
  assert.equal(fake.calls[fake.calls.length - 1], "activate null", "hiding the panel deactivates the native view");
  store().setVisible(true);
  assert.equal(fake.calls[fake.calls.length - 1], "activate a1");
  fake.emitTabs([tab({ id: "u1", taskId: USER_TASK_ID })]);
  assert.equal(store().activeTabId, "u1", "a closed active tab falls back to a shown tab");
  detach();
  assert.equal(fake.subscribed, false, "detach unsubscribes tab updates");
  assert.equal(fake.calls[fake.calls.length - 1], "activate null", "detach releases the native view");
  assert.equal(store().host, null);
  fake.emitTabs([]);
  assert.equal(store().shown.length, 1, "updates after detach are ignored");
  console.log("  PASS  tab projection per task, activation sync and detach");
}

{
  reset();
  const fake = fakeHost([tab({ id: "seed", taskId: "A" })]);
  store().attach(fake.host, notify);
  store().setTaskId("A");
  store().setVisible(true);
  await tick();
  assert.deepEqual(store().shown.map((entry) => entry.id), ["seed"], "attach lists existing tabs");
  assert.equal(selectAddress(store()), "https://example.com/", "address falls back to the active tab url");
  await store().takeover("seed");
  assert.equal(fake.calls[fake.calls.length - 1], "takeover seed", "explicit takeover reaches the host for the selected tab");
  assert.equal(selectActiveTab(store())?.mode, "agent", "ownership waits for the authoritative host update");
  fake.emitTabs([tab({ id: "seed", mode: "human", epoch: 1 })]);
  assert.equal(selectActiveTab(store())?.mode, "human");
  await store().resume("seed");
  assert.equal(fake.calls[fake.calls.length - 1], "resume seed");
  fake.emitTabs([tab({ id: "seed", mode: "agent", epoch: 2 })]);
  assert.equal(selectActiveTab(store())?.mode, "agent");
  store().setDraft("seed", "localhost:3000/app");
  assert.equal(selectAddress(store()), "localhost:3000/app");
  await store().submitAddress();
  assert.equal(fake.calls[fake.calls.length - 1], 'navigate seed {"url":"http://localhost:3000/app"}', "Enter navigates the active tab");
  assert.equal(selectAddress(store()), "https://example.com/", "the draft is consumed by navigation");
  assert.equal(await store().openDraft(), false, "no typed draft means nothing to open");
  store().setDraft("seed", " Example.org ");
  assert.equal(await store().openDraft(), true);
  assert.equal(fake.calls[fake.calls.length - 2], `open https://Example.org ${USER_TASK_ID} false`, "+ opens the draft as a user tab");
  assert.equal(store().activeTabId, "new:https://Example.org");
  assert.equal(fake.calls[fake.calls.length - 1], "activate new:https://Example.org");
  store().clearDraft(store().activeTabId);
  fake.emitTabs([]);
  store().setDraft(null, "wiki.example");
  await store().submitAddress();
  assert.equal(fake.calls[fake.calls.length - 2], `open https://wiki.example ${USER_TASK_ID} false`, "Enter without a tab opens one");
  await store().zoom(store().activeTabId!, 1);
  assert.equal(fake.calls[fake.calls.length - 1], `zoom ${store().activeTabId} 1.1`);
  await store().close(store().activeTabId!);
  assert.equal(store().shown.length, 0, "a closed tab leaves the projection immediately");
  console.log("  PASS  address submission, draft lifecycle, open, zoom and close");
}

{
  reset();
  notices.length = 0;
  const fake = fakeHost();
  store().attach(fake.host, notify);
  store().setTaskId("A");
  store().setVisible(true);
  fake.emitTabs([tab({ id: "a1" })]);
  fake.fail.navigate = new Error("navigation refused");
  await store().navigate("a1", { action: "reload" });
  fake.fail.close = new Error("close refused");
  await store().close("a1");
  fake.fail.open = new Error("open refused");
  await store().open("https://example.net");
  fake.fail.takeover = new Error("takeover refused");
  await store().takeover("a1");
  assert.deepEqual(notices, ["navigation refused", "close refused", "open refused", "takeover refused"], "host failures reach the notifier");
  assert.equal(store().shown.length, 1, "a failed close keeps the tab");
  console.log("  PASS  host errors are surfaced through the bound notifier");
}

{
  reset();
  const fake = fakeHost();
  store().attach(fake.host, notify);
  for (let index = 0; index < 55; index += 1) fake.emitDownload(download({ id: `d${index}`, state: "completed" }));
  assert.equal(store().downloads.length, 50, "downloads keep the last 50 entries");
  assert.equal(store().downloads[0].id, "d54", "newest download first");
  fake.emitDownload(download({ id: "d10", received: 40 }));
  assert.equal(store().downloads[0].id, "d10", "a progress update moves its download to the front");
  assert.equal(store().downloads.filter((entry) => entry.id === "d10").length, 1, "updates replace instead of duplicating");
  store().clearDownloads();
  assert.deepEqual(store().downloads.map((entry) => entry.id), ["d10"], "clearing keeps in-flight downloads");
  console.log("  PASS  download strip bookkeeping");
}

for (const [input, expected] of [
  ["", null], ["   ", null],
  ["example.com", "https://example.com"], [" Docs.example.org/path?q=1 ", "https://Docs.example.org/path?q=1"],
  ["http://example.com", "http://example.com"], ["HTTPS://example.com", "HTTPS://example.com"],
  ["localhost:3000", "http://localhost:3000"], ["127.0.0.1:8080/x", "http://127.0.0.1:8080/x"], ["[::1]:5173", "http://[::1]:5173"],
  ["localhost.example.com", "https://localhost.example.com"],
  ["about:blank", "about:blank"], ["file:///tmp/a.html", "file:///tmp/a.html"], ["data:text/plain,hi", "data:text/plain,hi"],
  ["ftp://host/file", "ftp://host/file"], ["javascript:alert(1)", "https://javascript:alert(1)"],
] as const) {
  assert.equal(normalizeAddress(input), expected, `normalizeAddress(${JSON.stringify(input)})`);
}
console.log("  PASS  address normalisation");

assert.equal(zoomStep(1, 1), 1.1);
assert.equal(zoomStep(1, -1), 0.9);
assert.equal(zoomStep(1.1, 0), 1);
assert.equal(zoomStep(5, 1), 5, "zoom in saturates at the largest preset");
assert.equal(zoomStep(0.25, -1), 0.25, "zoom out saturates at the smallest preset");
assert.equal(zoomStep(1.3, 1), 1.5, "an off-preset factor snaps to the next preset");
assert.equal(zoomStep(1.3, -1), 1.25);
console.log("  PASS  zoom presets");
console.log("browser panel store: all checks passed");

import assert from "node:assert/strict";
import { test } from "node:test";
import type { BrowserTabView } from "../../shared/ipc.js";
import { FakeGuestView, FakeViewFactory, silentLog } from "./fakeGuestViews.js";
import { BrowserSurfaceManager, normaliseBrowserURL, validateLayout, type BrowserTab } from "./surfaceManager.js";

function setup(options: { now?: () => number } = {}) {
  const factory = new FakeViewFactory();
  const takeovers: Array<{ tabId: string; epoch: number; reason: string }> = [];
  const crashes: string[] = [];
  const broadcasts: BrowserTabView[][] = [];
  const manager = new BrowserSurfaceManager({
    views: factory,
    contentSize: () => ({ width: 1000, height: 700 }),
    onTakeover: (tab: BrowserTab, reason: string) => takeovers.push({ tabId: tab.id, epoch: tab.epoch, reason }),
    onCrash: (_tab, reason) => crashes.push(reason),
    log: silentLog,
    now: options.now,
    openWaitMs: 10,
  });
  manager.subscribe((tabs) => broadcasts.push(tabs));
  const viewOf = (tab: BrowserTab) => tab.view as FakeGuestView;
  return { factory, manager, takeovers, crashes, broadcasts, viewOf };
}

test("URLs are normalised to http(s) and bare hosts get https", () => {
  assert.equal(normaliseBrowserURL("example.com"), "https://example.com/");
  assert.equal(normaliseBrowserURL(" http://a.b/c?d=1 "), "http://a.b/c?d=1");
  assert.throws(() => normaliseBrowserURL("file:///etc/passwd"), /only http\(s\)/);
  assert.throws(() => normaliseBrowserURL("javascript:alert(1)"), /only http\(s\)/);
  assert.throws(() => normaliseBrowserURL(""), /empty/);
});

test("layout rects are rounded, clamped to the window and never negative", () => {
  assert.deepEqual(validateLayout({ x: 10.4, y: -5, width: 2000, height: 300.6 }, { width: 1000, height: 700 }), { x: 10, y: 0, width: 990, height: 301 });
  assert.deepEqual(validateLayout({ x: 1200, y: 0, width: 100, height: 100 }, { width: 1000, height: 700 }), { x: 1200, y: 0, width: 0, height: 100 });
  assert.throws(() => validateLayout({ x: Number.NaN, y: 0, width: 1, height: 1 }, null), /finite/);
});

test("open, activate, layout and overlay drive visibility of exactly one view", async () => {
  const { manager, viewOf } = setup();
  const first = await manager.open("example.com", { taskId: "task-1", temporary: false });
  const second = await manager.open("https://b.test", { taskId: "task-1", temporary: true });
  assert.equal(first.partition, "persist:browser");
  assert.equal(second.partition, `temp:${second.id}`);
  assert.equal(viewOf(first).page.calls[0], "load:https://example.com/");
  assert.equal(manager.activeTabId, null, "opens do not select a native view without the application renderer");
  manager.activate(second.id);
  assert.equal(viewOf(second).visible, false, "no layout yet: nothing is visible");

  manager.setLayout({ x: 300, y: 100, width: 600, height: 500 });
  assert.equal(viewOf(second).visible, true);
  assert.deepEqual(viewOf(second).bounds, { x: 300, y: 100, width: 600, height: 500 });
  assert.equal(viewOf(first).visible, false);

  manager.activate(first.id);
  assert.equal(viewOf(first).visible, true);
  assert.equal(viewOf(second).visible, false);

  manager.setOverlay(true);
  assert.equal(viewOf(first).visible, false);
  manager.setOverlay(false);
  assert.equal(viewOf(first).visible, true);

  manager.setLayout(null);
  assert.equal(viewOf(first).visible, false);
  manager.setLayout({ x: 0, y: 0, width: 100, height: 100 });
  manager.activate(null);
  assert.equal(viewOf(first).visible, false);
  assert.throws(() => manager.activate("tab-99"), /unknown browser tab/);
});

test("navigation bumps the epoch, records the URL and clears load errors", async () => {
  const { manager, viewOf, broadcasts } = setup();
  const tab = await manager.open("https://a.test", { taskId: "t", temporary: false });
  manager.activate(tab.id);
  const events = viewOf(tab).fire();
  const before = broadcasts.length;
  events.onStartLoading();
  assert.equal(tab.loading, true);
  events.onFailLoad(-105, "ERR_NAME_NOT_RESOLVED", "https://a.test/");
  assert.deepEqual(tab.error, { code: -105, description: "ERR_NAME_NOT_RESOLVED" });
  const epoch = tab.epoch;
  events.onNavigate("https://a.test/next", false);
  assert.equal(tab.epoch, epoch + 1);
  assert.equal(tab.lastURL, "https://a.test/next");
  assert.equal(tab.error, null);
  events.onNavigate("https://a.test/next#x", true);
  assert.equal(tab.epoch, epoch + 2);
  events.onStopLoading();
  assert.equal(tab.loading, false);
  assert.ok(broadcasts.length >= before + 5, "every change is broadcast");
  const view = manager.list()[0];
  assert.equal(view.mode, "agent");
  assert.equal(view.taskId, "t");
  assert.equal(view.active, true);
});

test("take-over flips to human mode, bumps the epoch and reports to Go; resume hands back", async () => {
  let now = 1000;
  const { manager, takeovers } = setup({ now: () => now });
  const tab = await manager.open("https://a.test", { taskId: "t", temporary: false });
  const epoch = tab.epoch;
  assert.equal(manager.takeoverFromSender(999, "mousedown"), false, "unknown senders are ignored");
  assert.equal(manager.takeoverFromSender(tab.view.page.id, "mousedown"), true);
  assert.equal(tab.mode, "human");
  assert.equal(tab.epoch, epoch + 1);
  assert.deepEqual(takeovers, [{ tabId: tab.id, epoch: epoch + 1, reason: "user mousedown" }]);

  manager.resume(tab.id);
  assert.equal(tab.mode, "agent");
  assert.equal(tab.epoch, epoch + 2);
  manager.resume(tab.id);
  assert.equal(tab.epoch, epoch + 2, "resume is idempotent");

  manager.markAgentInput(tab);
  now += 100;
  assert.equal(manager.takeoverFromSender(tab.view.page.id, "keydown"), false, "the agent's own echoed input is not a take-over");
  assert.equal(tab.mode, "agent");
  now += 1000;
  assert.equal(manager.takeoverFromSender(tab.view.page.id, "keydown"), true);
  assert.equal(tab.mode, "human");
});

test("a crashed website view reloads its last URL in human mode and emits crash", async () => {
  const { manager, viewOf, crashes } = setup();
  const tab = await manager.open("https://a.test", { taskId: "t", temporary: false });
  const events = viewOf(tab).fire();
  events.onNavigate("https://a.test/page", false);
  const epoch = tab.epoch;
  const partition = tab.partition;
  events.onRenderProcessGone("crashed");
  assert.equal(tab.mode, "human");
  assert.equal(tab.epoch, epoch + 1);
  // Promoted from prototypes/electron-browser: the reload keeps the persistent
  // partition, so website logins survive a renderer crash.
  assert.equal(tab.partition, partition, "recovery preserves the login partition");
  assert.deepEqual(crashes, ["crashed"]);
  assert.equal(viewOf(tab).page.calls.at(-1), "load:https://a.test/page");
  for (let i = 0; i < 5; i++) events.onRenderProcessGone("oom");
  assert.equal(viewOf(tab).page.calls.filter((call) => call === "load:https://a.test/page").length, 3, "reloads are capped");
});

test("popups retain task and partition without stealing the application selection", async () => {
  const { manager, viewOf, factory } = setup();
  const tab = await manager.open("https://a.test", { taskId: "t", temporary: true });
  manager.activate(tab.id);
  const adopt = viewOf(tab).fire().onPopup("https://login.test/oauth", "new-window");
  assert.ok(adopt);
  const child = factory.create(tab.partition) as FakeGuestView;
  adopt(child);
  const tabs = manager.all();
  assert.equal(tabs.length, 2);
  assert.equal(tabs[1].taskId, "t");
  assert.equal(tabs[1].partition, tab.partition);
  assert.equal(tabs[1].temporary, true);
  assert.equal(manager.activeTabId, tab.id);
  assert.ok(child.events, "the popup view is bound to its own tab record");
  manager.close(tab.id);
  assert.equal(viewOf(tab).fire().onPopup("https://x", "new-window"), null, "a closed tab cannot spawn popups");
});

test("close and destroyAll tear views down and leave replacement selection to the renderer", async () => {
  const { manager, viewOf } = setup();
  const a = await manager.open("https://a.test", { taskId: "t", temporary: false });
  const b = await manager.open("https://b.test", { taskId: "t", temporary: false });
  manager.activate(b.id);
  manager.setLayout({ x: 0, y: 0, width: 10, height: 10 });
  manager.close(b.id);
  assert.equal(viewOf(b).destroyed, true);
  assert.equal(manager.activeTabId, null);
  assert.equal(viewOf(a).visible, false);
  manager.activate(a.id);
  assert.equal(viewOf(a).visible, true);
  assert.equal(b.mode, "human", "a closed tab fails every pending act as taken over");
  viewOf(a).fire().onDestroyed();
  assert.equal(manager.all().length, 0, "a page closing itself drops the record");
  const c = await manager.open("https://c.test", { taskId: "t", temporary: false });
  manager.destroyAll();
  assert.equal(viewOf(c).destroyed, true);
  assert.equal(manager.all().length, 0);
  assert.equal(c.mode, "human");
});

test("background opens never replace the current task's page or revive a detached renderer", async () => {
  const { manager, viewOf, takeovers } = setup();
  const a = await manager.open("https://a.test", { taskId: "A", temporary: false });
  manager.activate(a.id);
  manager.setLayout({ x: 400, y: 100, width: 500, height: 500 });
  const b = await manager.open("https://b.test", { taskId: "B", temporary: false });
  assert.equal(manager.activeTabId, a.id);
  assert.equal(viewOf(a).visible, true);
  assert.equal(viewOf(b).visible, false);
  const epoch = a.epoch;
  manager.pauseForRendererLoss("app renderer crashed");
  assert.equal(manager.activeTabId, null);
  assert.equal(viewOf(a).visible, false);
  assert.equal(a.mode, "human");
  assert.ok(a.epoch > epoch, "pending acts lose their document epoch");
  assert.equal(takeovers.length, 2);
  manager.activate(a.id);
  assert.equal(viewOf(a).visible, false, "selection cannot restore the lost layout");
  manager.setLayout({ x: 400, y: 100, width: 500, height: 500 });
  assert.equal(viewOf(a).visible, true);
  assert.equal(a.mode, "human", "remount never resumes actions automatically");
});

test("navigate, zoom and devtools act on the page", async () => {
  const { manager, viewOf } = setup();
  const tab = await manager.open("https://a.test", { taskId: "t", temporary: false });
  const page = viewOf(tab).page;
  page.back = true;
  await manager.navigate(tab.id, { action: "back" });
  await manager.navigate(tab.id, { action: "forward" });
  await manager.navigate(tab.id, { action: "reload" });
  await manager.navigate(tab.id, { action: "stop" });
  await manager.navigate(tab.id, { url: "b.test/x" });
  assert.deepEqual(page.calls.slice(1), ["back", "reload", "stop", "load:https://b.test/x"]);
  await assert.rejects(manager.navigate(tab.id, {}), /needs a url/);
  manager.setZoom(tab.id, 9);
  assert.equal(tab.zoom, 5);
  assert.equal(page.zoom, 5);
  assert.throws(() => manager.setZoom(tab.id, Number.NaN), /finite/);
  manager.toggleDevTools(tab.id);
  assert.equal(page.devtools, true);
  manager.toggleDevTools(tab.id);
  assert.equal(page.devtools, false);
});

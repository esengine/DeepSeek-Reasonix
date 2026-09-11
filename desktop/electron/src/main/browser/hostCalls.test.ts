import assert from "node:assert/strict";
import { test } from "node:test";
import { RpcError } from "../rpc.js";
import { ActionExecutor } from "./actions.js";
import { DocumentRegistry } from "./documents.js";
import { DownloadTracker } from "./downloads.js";
import { BROWSER_ERR_NO_GRANT, BROWSER_ERR_STALE_REFERENCE, BROWSER_ERR_TAKEN_OVER } from "./errors.js";
import { FakeViewFactory, silentLog } from "./fakeGuestViews.js";
import { GrantRegistry } from "./grants.js";
import { buildBrowserHostCalls, type HostBrowserTab } from "./hostCalls.js";
import { BrowserSurfaceManager } from "./surfaceManager.js";

const code = (value: number) => (error: unknown) => error instanceof RpcError && error.code === value;

async function setup() {
  const factory = new FakeViewFactory();
  const surfaces = new BrowserSurfaceManager({ views: factory, contentSize: () => null, onTakeover() {}, onCrash() {}, log: silentLog, openWaitMs: 5 });
  const grants = new GrantRegistry({ generation: () => "gen-1" });
  let tokens = 0;
  const documents = new DocumentRegistry(() => `tok-${++tokens}`);
  const actions = new ActionExecutor({ surfaces, documents });
  const directories = new Map<string, string>();
  const downloads = new DownloadTracker({
    tabForWebContents: () => undefined,
    defaultDirectory: (taskId) => `/dl/${taskId}`,
    onUpdate() {},
    log: silentLog,
  });
  const table = buildBrowserHostCalls({
    surfaces,
    grants,
    documents,
    actions,
    downloads,
    snapshot: async (tab, selector) => ({ tabId: tab.id, selector }) as never,
    screenshot: async (tab, request) => ({ tabId: tab.id, ...request }) as never,
  });
  const call = async (method: keyof typeof table, params: Record<string, unknown> = {}): Promise<unknown> => table[method](params);
  const setDirs = downloads.setTaskDirectory.bind(downloads);
  downloads.setTaskDirectory = (taskId, directory) => {
    directories.set(taskId, directory);
    setDirs(taskId, directory);
  };
  return { surfaces, grants, documents, downloads, directories, table, call };
}

test("grant, list and open are scoped to the grant's task", async () => {
  const s = await setup();
  await assert.rejects(s.call("host/browser.tabs.list", { grantId: "g" }), code(BROWSER_ERR_NO_GRANT));
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "s1" });
  await s.call("host/browser.grant", { grantId: "h", tabId: "task-2", sessionId: "s2" });

  const opened = (await s.call("host/browser.tabs.open", { grantId: "g", url: "https://a.test" })) as HostBrowserTab;
  assert.equal(opened.url, "https://a.test/");
  assert.equal(s.surfaces.require(opened.id).taskId, "task-1", "the tab belongs to the grant's task");
  await s.surfaces.open("https://b.test", { taskId: "task-2", temporary: false });

  const listed = (await s.call("host/browser.tabs.list", { grantId: "g" })) as { tabs: HostBrowserTab[] };
  assert.deepEqual(listed.tabs.map((tab) => tab.id), [opened.id], "another task's tabs are invisible");
});

test("tab calls verify the grant and refuse tabs of another task", async () => {
  const s = await setup();
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "" });
  const other = await s.surfaces.open("https://b.test", { taskId: "task-2", temporary: false });
  await assert.rejects(s.call("host/browser.tabs.navigate", { grantId: "g", tabId: other.id, url: "https://c.test" }), code(BROWSER_ERR_NO_GRANT));
  await assert.rejects(s.call("host/browser.tabs.close", { grantId: "g", tabId: other.id }), code(BROWSER_ERR_NO_GRANT));
  await assert.rejects(s.call("host/browser.tabs.navigate", { grantId: "g", tabId: "tab-99", url: "https://c.test" }), code(BROWSER_ERR_NO_GRANT));
});

test("reads and writes refuse a tab the user has taken over", async () => {
  const s = await setup();
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "" });
  const tab = await s.surfaces.open("https://a.test", { taskId: "task-1", temporary: false });
  assert.deepEqual(await s.call("host/browser.snapshot", { grantId: "g", tabId: tab.id, selector: "" }), { tabId: tab.id, selector: "" });
  s.surfaces.takeover(tab.id, "user mousedown");
  await assert.rejects(s.call("host/browser.snapshot", { grantId: "g", tabId: tab.id }), code(BROWSER_ERR_TAKEN_OVER));
  await assert.rejects(s.call("host/browser.tabs.navigate", { grantId: "g", tabId: tab.id, url: "https://c.test" }), code(BROWSER_ERR_TAKEN_OVER));
  await assert.rejects(s.call("host/browser.screenshot", { grantId: "g", tabId: tab.id, ref: "", directory: "" }), code(BROWSER_ERR_TAKEN_OVER));
  const closed = await s.call("host/browser.tabs.close", { grantId: "g", tabId: tab.id });
  assert.deepEqual(closed, {}, "closing is still allowed so the task can clean up");
  assert.equal(s.surfaces.get(tab.id), undefined);
});

test("act re-verifies the grant and rejects a stale document token", async () => {
  const s = await setup();
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "" });
  const tab = await s.surfaces.open("https://a.test", { taskId: "task-1", temporary: false });
  await assert.rejects(
    s.call("host/browser.act", { grantId: "g", tabId: tab.id, documentToken: "nope", action: "click", ref: "e1" }),
    code(BROWSER_ERR_STALE_REFERENCE),
  );
  await s.call("host/browser.revoke", { grantId: "g" });
  await assert.rejects(
    s.call("host/browser.act", { grantId: "g", tabId: tab.id, documentToken: "nope", action: "click", ref: "e1" }),
    code(BROWSER_ERR_NO_GRANT),
  );
});

test("act and screenshot register the scratch directory for downloads", async () => {
  const s = await setup();
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "" });
  const tab = await s.surfaces.open("https://a.test", { taskId: "task-1", temporary: false });
  await s.call("host/browser.screenshot", { grantId: "g", tabId: tab.id, ref: "", directory: "/scratch/one" });
  assert.equal(s.directories.get("task-1"), "/scratch/one");
  await s.call("host/browser.screenshot", { grantId: "g", tabId: tab.id, ref: "", directory: "" });
  assert.equal(s.directories.get("task-1"), "/scratch/one", "an empty directory keeps the previous mapping");
});

test("downloads waits are served per tab and a zero wait answers at once", async () => {
  const s = await setup();
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "" });
  const tab = await s.surfaces.open("https://a.test", { taskId: "task-1", temporary: false });
  assert.deepEqual(await s.call("host/browser.downloads", { grantId: "g", tabId: tab.id, waitForMs: 0 }), { downloads: [] });
});

test("revoke invalidates the task's document tokens", async () => {
  const s = await setup();
  await s.call("host/browser.grant", { grantId: "g", tabId: "task-1", sessionId: "" });
  const tab = await s.surfaces.open("https://a.test", { taskId: "task-1", temporary: false });
  const token = s.documents.issue({ tabId: tab.id, epoch: tab.epoch, snapshotId: "snap", frames: [] });
  assert.ok(s.documents.lookup(token));
  await s.call("host/browser.revoke", { grantId: "g" });
  assert.equal(s.documents.lookup(token), undefined);
});

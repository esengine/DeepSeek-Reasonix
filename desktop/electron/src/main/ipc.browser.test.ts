import assert from "node:assert/strict";
import type { IpcMain } from "electron";
import { test } from "node:test";
import { IPC, type IpcResult } from "../shared/ipc.js";
import { ActionExecutor, type ActRequest } from "./browser/actions.js";
import { DocumentRegistry } from "./browser/documents.js";
import { BROWSER_ERR_STALE_REFERENCE, BROWSER_ERR_TAKEN_OVER } from "./browser/errors.js";
import { FakeGuestView, FakeViewFactory, silentLog } from "./browser/fakeGuestViews.js";
import { BrowserSurfaceManager } from "./browser/surfaceManager.js";
import { parseContract } from "./contract.js";
import { registerRendererIpc } from "./ipc.js";
import { RpcError } from "./rpc.js";

test("trusted toolbar takeover bypasses input suppression immediately, fences actions and permits Resume", async () => {
  type Handler = (event: unknown, ...args: unknown[]) => Promise<IpcResult>;
  const handlers = new Map<string, Handler>();
  const ipcMain = { handle: (channel: string, run: Handler) => handlers.set(channel, run), on() {} } as unknown as IpcMain;
  const trustedSender = { id: 1 }, trustedFrame = {};
  const trusted = { sender: trustedSender, senderFrame: trustedFrame };
  const reports: string[] = [];
  const manager = new BrowserSurfaceManager({ views: new FakeViewFactory(), contentSize: () => null,
    onTakeover: (_tab, reason) => reports.push(reason), onCrash() {}, log: silentLog, now: () => 1000, openWaitMs: 5 });
  const tab = await manager.open("https://example.com", { taskId: "task", temporary: false });
  const page = (tab.view as FakeGuestView).page;
  const documents = new DocumentRegistry(() => "token");
  const documentToken = documents.issue({ tabId: tab.id, epoch: tab.epoch, snapshotId: "snapshot",
    frames: [{ prefix: "", frameTreeNodeId: page.mainFrame.frameTreeNodeId, docId: "doc" }] });
  const actions = new ActionExecutor({ surfaces: manager, documents, fileExists: () => false, sleep: async () => {} });
  const request: ActRequest = { operationId: "op", tabId: tab.id, documentToken, action: "click", ref: "e1",
    text: "", keys: "", options: [], files: [], submit: false, deltaX: 0, deltaY: 0 };
  registerRendererIpc({ ipcMain, contract: parseContract({ digest: "test", commands: ["ListTabs"] }),
    window: { isTrustedSender: (sender, frame) => sender === trustedSender && frame === trustedFrame,
      minimise() {}, toggleMaximise() {}, isMaximised: () => false, close() {},
      bounds: () => ({ x: 0, y: 0, width: 1, height: 1, maximised: false }), setTheme() {}, setBackgroundColour() {},
      getAppZoom: async () => 1, setAppZoom: async () => 1, resetAppZoom: async () => 1 },
    invoke: async () => undefined, serviceState: () => ({ phase: "ready", generation: "g" }),
    clipboard: { writeText() {}, readText: () => "" }, openExternal: async () => {}, log: silentLog,
    browser: { list: () => manager.list(), open: async (url, options) => { const opened = await manager.open(url, options); return manager.list().find((entry) => entry.id === opened.id)!; },
      close: (id) => manager.close(id), activate: (id) => manager.activate(id), navigate: async (id, target) => { await manager.navigate(id, target); },
      setZoom: (id, factor) => manager.setZoom(id, factor), toggleDevTools: (id) => manager.toggleDevTools(id),
      takeover: (id) => manager.takeover(id, "user takeover"), resume: (id) => manager.resume(id),
      setLayout: (rect) => manager.setLayout(rect), setOverlay: (active) => manager.setOverlay(active) },
  });
  const takeover = handlers.get(IPC.browserUserTakeover)!;
  assert.equal(handlers.has(IPC.browserTakeover), false, "guest input reports use a separate channel");
  const epoch = tab.epoch;
  manager.markAgentInput(tab);
  assert.equal(manager.takeoverFromSender(page.id, "mousedown"), false, "physical detection is suppressed during the 750ms grace window");
  assert.deepEqual(await takeover({ sender: { id: page.id }, senderFrame: {} }, tab.id), { ok: false, message: "untrusted sender" });
  assert.deepEqual(await takeover({ sender: trustedSender, senderFrame: {} }, tab.id), { ok: false, message: "untrusted sender" });
  assert.equal((await takeover(trusted, "")).ok, false);
  assert.equal(tab.epoch, epoch, "untrusted and malformed calls cannot alter ownership");
  const pending = takeover(trusted, tab.id);
  assert.equal(tab.mode, "human", "explicit takeover is synchronous even inside the suppression window");
  assert.equal(tab.epoch, epoch + 1);
  assert.deepEqual(await pending, { ok: true, value: undefined });
  assert.deepEqual(reports, ["user takeover"]);
  await assert.rejects(actions.act(tab, request, () => {}), (error: unknown) => error instanceof RpcError && error.code === BROWSER_ERR_TAKEN_OVER);
  assert.deepEqual(await handlers.get(IPC.browserResume)!(trusted, tab.id), { ok: true, value: undefined });
  assert.equal(tab.mode, "agent");
  assert.equal(tab.epoch, epoch + 2);
  await assert.rejects(actions.act(tab, request, () => {}), (error: unknown) => error instanceof RpcError && error.code === BROWSER_ERR_STALE_REFERENCE);
  assert.equal(page.inputs.length, 0, "takeover and Resume never replay a stale write");
  manager.destroyAll();
});

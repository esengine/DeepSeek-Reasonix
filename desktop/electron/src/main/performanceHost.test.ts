import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import type { BrowserWindow } from "electron";
import { test } from "node:test";
import { createPerformanceHost } from "./performanceHost.js";

test("heap export requires consent and an OS save choice, stays single flight, and never uploads", async () => {
  const contents = Object.assign(new EventEmitter(), { isDestroyed: () => false, takeHeapSnapshot: async (path: string) => { paths.push(path); await pending; } });
  const win = Object.assign(new EventEmitter(), { webContents: contents, isVisible: () => true, isFocused: () => true });
  let consent = 0;
  let release!: () => void;
  const pending = new Promise<void>((resolve) => { release = resolve; });
  const paths: string[] = [];
  const host = createPerformanceHost({ window: () => win as unknown as BrowserWindow, workerPath: "unused", locale: () => "en", dialog: {
    showMessageBox: async () => ({ response: consent, checkboxChecked: false }),
    showSaveDialog: async () => ({ canceled: false, filePath: "chosen.heapsnapshot" }),
  } });
  assert.deepEqual(await host.exportHeapSnapshot(), { status: "cancelled" });
  assert.deepEqual(paths, []);
  consent = 1;
  const exportWork = host.exportHeapSnapshot();
  await Promise.resolve(); await Promise.resolve();
  assert.deepEqual(await host.exportHeapSnapshot(), { status: "busy" });
  assert.deepEqual(await host.captureRendererProfile(), { status: "busy" });
  assert.deepEqual(paths, ["chosen.heapsnapshot"]);
  release();
  assert.deepEqual(await exportWork, { status: "saved" });
  host.dispose();
  assert.deepEqual(await host.exportHeapSnapshot(), { status: "unavailable" });
});

test("navigation while the consent dialog is open cancels export", async () => {
  const contents = Object.assign(new EventEmitter(), { isDestroyed: () => false, takeHeapSnapshot: async () => { throw Error("must not capture"); } });
  const win = Object.assign(new EventEmitter(), { webContents: contents, isVisible: () => true, isFocused: () => true });
  const host = createPerformanceHost({ window: () => win as unknown as BrowserWindow, workerPath: "unused", locale: () => "en", dialog: {
    showMessageBox: async () => { contents.emit("did-start-navigation", {}, "reasonix://app", false, true); return { response: 1, checkboxChecked: false }; },
    showSaveDialog: async () => { throw Error("must not save"); },
  } });
  assert.deepEqual(await host.exportHeapSnapshot(), { status: "cancelled" });
  assert.equal(contents.listenerCount("did-start-navigation"), 0);
});

test("production activity policy requires native focus and cancels on blur", async () => {
  let focused = false;
  let attached = false;
  const debuggerApi = Object.assign(new EventEmitter(), {
    isAttached: () => attached,
    attach: () => { attached = true; },
    detach: () => { attached = false; },
    sendCommand: async () => ({ profile: { nodes: [], startTime: 0, endTime: 0 } }),
  });
  const contents = Object.assign(new EventEmitter(), { debugger: debuggerApi, isDestroyed: () => false, isDevToolsOpened: () => false });
  const win = Object.assign(new EventEmitter(), { webContents: contents, isVisible: () => true, isFocused: () => focused });
  const host = createPerformanceHost({ window: () => win as unknown as BrowserWindow, workerPath: "must-not-run", locale: () => "en", dialog: {
    showMessageBox: async () => { throw Error("must not prompt"); }, showSaveDialog: async () => { throw Error("must not save"); },
  } });
  assert.deepEqual(await host.captureRendererProfile("inactive"), { status: "inactive" });
  assert.deepEqual(await host.exportHeapSnapshot(), { status: "unavailable" });
  assert.equal(attached, false);
  focused = true;
  const capture = host.captureRendererProfile("active");
  for (let i = 0; i < 20; i++) await Promise.resolve();
  assert.equal(attached, true);
  focused = false;
  win.emit("blur");
  assert.deepEqual(await capture, { status: "cancelled" });
  assert.equal(attached, false);
  assert.equal(win.listenerCount("blur"), 0);
});

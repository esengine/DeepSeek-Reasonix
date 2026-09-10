import assert from "node:assert/strict";
import type { IpcMain } from "electron";
import { test } from "node:test";
import { IPC, type IpcResult } from "../shared/ipc.js";
import { parseContract } from "./contract.js";
import { isOpenableExternalURL, registerRendererIpc } from "./ipc.js";

const silent = { info() {}, warn() {}, error() {} };

function fakeIpc() {
  const handlers = new Map<string, (event: unknown, ...args: unknown[]) => Promise<IpcResult>>();
  const listeners = new Map<string, (event: { sender: unknown; senderFrame: unknown; returnValue?: unknown }) => void>();
  const ipcMain = {
    handle: (channel: string, fn: (event: unknown, ...args: unknown[]) => Promise<IpcResult>) => handlers.set(channel, fn),
    on: (channel: string, fn: (event: { sender: unknown; senderFrame: unknown; returnValue?: unknown }) => void) => listeners.set(channel, fn),
  } as unknown as IpcMain;
  return { ipcMain, handlers, listeners };
}

test("external URLs are limited to http, https and mailto", () => {
  assert.equal(isOpenableExternalURL("https://example.com/x"), true);
  assert.equal(isOpenableExternalURL("http://example.com"), true);
  assert.equal(isOpenableExternalURL("mailto:a@b.c"), true);
  assert.equal(isOpenableExternalURL("file:///etc/passwd"), false);
  assert.equal(isOpenableExternalURL("javascript:alert(1)"), false);
  assert.equal(isOpenableExternalURL("reasonix://app/"), false);
  assert.equal(isOpenableExternalURL("not a url"), false);
  assert.equal(isOpenableExternalURL(42), false);
});

test("renderer invokes are gated by sender identity and the contract allowlist", async () => {
  const { ipcMain, handlers, listeners } = fakeIpc();
  const trustedSender = { id: 1 };
  const trustedFrame = {};
  const invoked: Array<{ method: string; args: unknown[] }> = [];
  registerRendererIpc({
    ipcMain,
    contract: parseContract({ digest: "sha256:a", commands: ["OpenProjectTab"] }),
    window: {
      isTrustedSender: (sender, frame) => sender === trustedSender && frame === trustedFrame,
      minimise() {},
      toggleMaximise() {},
      isMaximised: () => true,
      close() {},
      bounds: () => ({ x: 1, y: 2, width: 3, height: 4, maximised: false }),
      setTheme() {},
      setBackgroundColour() {},
      getAppZoom: async () => 1,
      setAppZoom: async () => 1,
      resetAppZoom: async () => 1,
    },
    serviceState: () => ({ phase: "ready" as const, generation: "g-test" }),
    invoke: async (method, args) => {
      invoked.push({ method, args });
      if (method === "OpenProjectTab" && args[0] === "/missing") throw new Error("workspace not found");
      return { opened: args[0] };
    },
    clipboard: { writeText: async () => undefined, readText: async () => "clip" },
    openExternal: async () => undefined,
    log: silent,
  });
  const invoke = handlers.get(IPC.invoke);
  assert.ok(invoke);
  const trusted = { sender: trustedSender, senderFrame: trustedFrame };
  assert.deepEqual(await invoke({ sender: { id: 9 }, senderFrame: trustedFrame }, "OpenProjectTab", ["/p"]), { ok: false, message: "untrusted sender" });
  assert.deepEqual(await invoke({ sender: trustedSender, senderFrame: {} }, "OpenProjectTab", ["/p"]), { ok: false, message: "untrusted sender" });
  assert.deepEqual(await invoke(trusted, "OpenProjectTab", ["/p"]), { ok: true, value: { opened: "/p" } });
  assert.deepEqual(await invoke(trusted, "OpenProjectTab", ["/missing"]), { ok: false, message: "workspace not found" });
  assert.deepEqual(await invoke(trusted, "DeleteEverything", []), { ok: false, message: "-32601 method not found: DeleteEverything" });
  assert.deepEqual(await invoke(trusted, "__proto__", []), { ok: false, message: "-32601 method not found: __proto__" });
  assert.deepEqual(invoked.map((call) => call.method), ["OpenProjectTab", "OpenProjectTab"]);

  const contract = listeners.get(IPC.contract);
  assert.ok(contract);
  const event: { sender: unknown; senderFrame: unknown; returnValue?: unknown } = { ...trusted };
  contract(event);
  assert.deepEqual(event.returnValue, { protocolVersion: 1, digest: "sha256:a", commands: ["OpenProjectTab"] });
  const untrusted: { sender: unknown; senderFrame: unknown; returnValue?: unknown } = { sender: { id: 9 }, senderFrame: trustedFrame };
  contract(untrusted);
  assert.equal(untrusted.returnValue, null);

  const open = handlers.get(IPC.openExternal);
  assert.ok(open);
  assert.deepEqual(await open(trusted, "file:///etc/passwd"), { ok: false, message: "refusing to open file:///etc/passwd" });
  assert.deepEqual(await open(trusted, "https://example.com"), { ok: true, value: undefined });
  const bounds = handlers.get(IPC.windowGetBounds);
  assert.ok(bounds);
  assert.deepEqual(await bounds(trusted), { ok: true, value: { x: 1, y: 2, width: 3, height: 4, maximised: false } });
  const read = handlers.get(IPC.clipboardRead);
  assert.ok(read);
  assert.deepEqual(await read(trusted), { ok: true, value: "clip" });
});

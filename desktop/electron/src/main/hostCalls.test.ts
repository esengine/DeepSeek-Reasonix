import assert from "node:assert/strict";
import { test } from "node:test";
import { renderFailurePage, shellActionFromURL } from "./failurePage.js";
import { buildHostCallTable, dispatchHostCall, type HostCallDeps } from "./hostCalls.js";
import { RpcError } from "./rpc.js";

function deps() {
  const calls: string[] = [];
  const record = (name: string) => (...args: unknown[]) => {
    calls.push(`${name}(${args.map((a) => JSON.stringify(a)).join(",")})`);
  };
  const table = buildHostCallTable({
    window: {
      show: record("show"), hide: record("hide"), maximise: record("maximise"), unmaximise: record("unmaximise"),
      minimise: record("minimise"), unminimise: record("unminimise"), toggleMaximise: record("toggleMaximise"),
      center: record("center"), isMaximised: () => true, isMinimised: () => false,
      setPosition: record("setPosition"), setTitle: record("setTitle"), toggleDevTools: record("devtools"),
    },
    dialogs: {
      openDirectory: async () => ({ path: "/dir" }),
      openFile: async () => ({ paths: [] }),
      saveFile: async () => ({ path: "" }),
      message: async () => ({ button: "OK" }),
    },
    tray: { ensure: (labels) => { calls.push(`tray(${labels.openTitle},${labels.quitTitle},${labels.tooltip})`); return { ready: true, reason: "" }; }, destroy: record("trayDestroy") },
    remote: { open: (input) => { calls.push(`remoteOpen(${input.hostKey})`); return { windowId: "7" }; }, navigate: record("remoteNavigate"), focus: record("remoteFocus"), close: record("remoteClose") },
    lifecycle: { approve: record("approve"), relaunch: record("relaunch") },
    openExternal: async (url) => { calls.push(`open(${url})`); },
    hideApp: record("hideApp"),
    screens: () => [{ x: 0, y: 0, width: 1, height: 1, scale: 2, primary: true }],
  } satisfies HostCallDeps);
  return { table, calls };
}

test("every documented host/* method is dispatched with parsed params", async () => {
  const { table, calls } = deps();
  assert.deepEqual(await dispatchHostCall(table, "host/window.show", { reason: "tray" }), {});
  assert.deepEqual(await dispatchHostCall(table, "host/window.isMaximised", {}), { value: true });
  assert.deepEqual(await dispatchHostCall(table, "host/window.setPosition", { x: 10.4, y: "bad" }), {});
  assert.deepEqual(await dispatchHostCall(table, "host/screen.list", {}), { screens: [{ x: 0, y: 0, width: 1, height: 1, scale: 2, primary: true }] });
  assert.deepEqual(await dispatchHostCall(table, "host/dialog.openDirectory", { title: "t" }), { path: "/dir" });
  assert.deepEqual(await dispatchHostCall(table, "host/tray.ensure", { openTitle: "打开", quitTitle: "退出" }), { ready: true, reason: "" });
  assert.deepEqual(await dispatchHostCall(table, "host/remoteWindow.open", { hostKey: "h1", url: "https://x", title: "T" }), { windowId: "7" });
  assert.deepEqual(await dispatchHostCall(table, "host/app.relaunch", { args: ["--x", 3] }), {});
  assert.deepEqual(await dispatchHostCall(table, "host/shell.openExternal", { url: "https://e" }), {});
  assert.deepEqual(await dispatchHostCall(table, "host/app.quit", undefined), {});
  assert.deepEqual(calls, [
    'show("tray")', "setPosition(10.4,0)", "tray(打开,退出,Reasonix)", "remoteOpen(h1)", 'relaunch(["--x"])', "open(https://e/)", "approve()",
  ]);
  for (const method of ["host/window.hide", "host/window.maximise", "host/window.center", "host/devtools.toggle", "host/tray.destroy", "host/app.hide"]) {
    assert.deepEqual(await dispatchHostCall(table, method, {}), {});
  }
});

test("update relaunch preserves the stable launcher path", async () => {
  const { table, calls } = deps();
  await dispatchHostCall(table, "host/app.relaunch", { args: ["--after-update"], execPath: "/opt/reasonix/reasonix-launcher" });
  assert.deepEqual(calls, ['relaunch(["--after-update"],"/opt/reasonix/reasonix-launcher")']);
});

test("host external links reject non-user-facing protocols", async () => {
  const { table, calls } = deps();
  for (const url of ["file:///tmp/probe", "javascript:alert(1)", "data:text/plain,probe", "not a url"]) {
    await assert.rejects(dispatchHostCall(table, "host/shell.openExternal", { url }), /refusing to open/);
  }
  assert.deepEqual(calls, []);
  await dispatchHostCall(table, "host/shell.openExternal", { url: "mailto:test@example.com" });
  assert.deepEqual(calls, ["open(mailto:test@example.com)"]);
});

test("browser host calls merge into the table when the surface is wired", async () => {
  const { table, calls } = deps();
  await assert.rejects(dispatchHostCall(table, "host/browser.tabs.list", {}), (error: unknown) => error instanceof RpcError && error.code === -32601);

  const merged = buildHostCallTable({
    ...({
      window: {
        show: () => {}, hide: () => {}, maximise: () => {}, unmaximise: () => {}, minimise: () => {}, unminimise: () => {},
        toggleMaximise: () => {}, center: () => {}, isMaximised: () => false, isMinimised: () => false,
        setPosition: () => {}, setTitle: () => {}, toggleDevTools: () => {},
      },
      dialogs: { openDirectory: async () => ({ path: "" }), openFile: async () => ({ paths: [] }), saveFile: async () => ({ path: "" }), message: async () => ({ button: "" }) },
      tray: { ensure: () => ({ ready: false, reason: "x" }), destroy: () => {} },
      remote: { open: () => ({ windowId: "" }), navigate: () => {}, focus: () => {}, close: () => {} },
      lifecycle: { approve: () => {}, relaunch: () => {} },
      openExternal: async () => {},
      hideApp: () => {},
      screens: () => [],
      browser: { "host/browser.tabs.list": () => ({ tabs: ["tab-1"] }) },
    } satisfies HostCallDeps),
  });
  assert.deepEqual(await dispatchHostCall(merged, "host/browser.tabs.list", {}), { tabs: ["tab-1"] });
  assert.deepEqual(calls, [], "the plain table was not touched");
});

test("unknown host methods fail with -32601 and never hit Object.prototype", async () => {
  const { table } = deps();
  for (const method of ["host/window.explode", "toString", "__proto__", "hasOwnProperty"]) {
    await assert.rejects(dispatchHostCall(table, method, {}), (error: unknown) => error instanceof RpcError && error.code === -32601);
  }
});

test("failure page actions ride the reasonix://app/__shell/ prefix and the page escapes text", () => {
  assert.equal(shellActionFromURL("reasonix://app/__shell/open-logs"), "open-logs");
  assert.equal(shellActionFromURL("reasonix://app/__shell/restart?x=1"), "restart");
  assert.equal(shellActionFromURL("reasonix://app/__shell/quit"), "quit");
  assert.equal(shellActionFromURL("reasonix://app/__shell/rm-rf"), null);
  assert.equal(shellActionFromURL("reasonix://app/index.html"), null);
  const html = renderFailurePage({ code: -32003, name: "contract_mismatch", title: "Mixed <install>", detail: "digest \"a\" != 'b'" }, "/logs");
  assert.match(html, /Mixed &lt;install&gt;/);
  assert.match(html, /digest &quot;a&quot; != &#39;b&#39;/);
  assert.match(html, /contract_mismatch \(-32003\)/);
  assert.match(html, /reasonix:\/\/app\/__shell\/restart/);
});

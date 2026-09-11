import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { APP_USER_MODEL_ID, applyAppUserModelId, registerTaskbarRelaunch, windowsTaskbarDetails } from "./appIdentity.js";

function windowsApp() {
  const applied: string[] = [];
  return { applied, app: { setAppUserModelId: (id: string) => applied.push(id) } };
}

test("Desktop has its own identity across Studio generations", () => {
  assert.equal(APP_USER_MODEL_ID, "io.reasonix.desktop");
  for (const other of ["Reasonix", "io.reasonix.studio", "dev.reasonix.desktop"]) {
    assert.notEqual(APP_USER_MODEL_ID, other);
  }
});

test("Windows adopts the launcher's shared AppUserModelID before any window exists", () => {
  const { applied, app } = windowsApp();
  applyAppUserModelId(app, "win32");
  assert.deepEqual(applied, [APP_USER_MODEL_ID]);
});

test("other platforms never touch the Windows identity", () => {
  for (const platform of ["darwin", "linux"] as const) {
    const { applied, app } = windowsApp();
    applyAppUserModelId(app, platform);
    assert.deepEqual(applied, [], `${platform} must not set an AppUserModelID`);
  }
});

test("main applies the identity at load time rather than after readiness", () => {
  const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");
  const applied = source.indexOf("applyAppUserModelId(app, process.platform)");
  assert.notEqual(applied, -1, "index.ts must apply the AppUserModelID");
  // app.whenReady() creates the window, so a later call would miss the taskbar.
  const ready = source.indexOf("app.whenReady()");
  assert.notEqual(ready, -1, "index.ts must still gate startup on app readiness");
  assert.ok(applied < ready, "the AppUserModelID must be applied before app.whenReady()");
});

test("new taskbar pins relaunch the permanent launcher for versioned and flat installs", () => {
  for (const executable of [String.raw`C:\Apps\Reasonix test\versions\v1.38.6\app\Reasonix.exe`, String.raw`C:\Apps\Reasonix test\app\Reasonix.exe`]) {
    const launcher = String.raw`C:\Apps\Reasonix test\reasonix-launcher.exe`;
    assert.deepEqual(windowsTaskbarDetails(executable, (path) => path === launcher), {
      appId: APP_USER_MODEL_ID,
      appIconPath: launcher,
      appIconIndex: 0,
      relaunchCommand: `"${launcher}"`,
      relaunchDisplayName: "Reasonix",
    });
  }
  assert.equal(windowsTaskbarDetails(String.raw`C:\Apps\Reasonix\app\Reasonix.exe`, () => false), undefined);
  assert.equal(windowsTaskbarDetails(String.raw`C:\Other\electron.exe`, () => true), undefined);
});

test("every created Windows window gets relaunch metadata and other platforms do not", () => {
  const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");
  const registered = source.indexOf("registerTaskbarRelaunch(app, process.platform, process.execPath, app.isPackaged)");
  assert.ok(registered >= 0 && registered < source.indexOf("bootstrap(home)"));
  type Listener = (event: unknown, window: { setAppDetails(details: object): void }) => void;
  const listeners: Listener[] = [];
  const target = { on: (event: string, callback: Listener) => { assert.equal(event, "browser-window-created"); listeners.push(callback); } };
  registerTaskbarRelaunch(target, "darwin", "/Applications/Reasonix", true, () => true);
  registerTaskbarRelaunch(target, "win32", String.raw`C:\Apps\Reasonix\app\Reasonix.exe`, false, () => true);
  assert.equal(listeners.length, 0);
  registerTaskbarRelaunch(target, "win32", String.raw`C:\Apps\Reasonix\app\Reasonix.exe`, true, () => true);
  const listener = listeners[0];
  assert.ok(listener);
  const applied: object[] = [];
  listener({}, { setAppDetails: (details) => applied.push(details) });
  listener({}, { setAppDetails: (details) => applied.push(details) });
  assert.equal(applied.length, 2);
  assert.deepEqual(applied[0], applied[1]);
});

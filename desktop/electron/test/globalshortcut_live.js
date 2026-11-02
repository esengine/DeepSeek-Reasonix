"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync, spawn } = require("node:child_process");
const { app, globalShortcut } = require("electron");
const { wait } = require("./livekit");

const home = process.env.REASONIX_HOME;
if (!home) throw new Error("Use an isolated REASONIX_HOME for the native shortcut smoke");
fs.mkdirSync(home, { recursive: true });
fs.writeFileSync(path.join(home, "config.toml"), 'default_model = "smoke/smoke-model"\n[desktop]\nwelcomed = true\n[[providers]]\nname = "smoke"\nkind = "openai"\nbase_url = "http://127.0.0.1:9/v1"\nmodels = ["smoke-model"]\ndefault = "smoke-model"\napi_key_env = "SMOKE_API_KEY"\n');
fs.writeFileSync(path.join(home, ".env"), "SMOKE_API_KEY=smoke\n");
const { current } = require("../src/main");
const input = process.env.REASONIX_XDOTOOL || "xdotool";
const binding = "Control+Shift+F9";
let neighbour;

async function until(read, description) {
  const end = Date.now() + 40000;
  while (Date.now() < end) {
    if (await read()) return;
    await wait(100);
  }
  throw new Error(`Timed out: ${description}`);
}

async function run() {
  await until(() => current().win && !current().win.webContents.isLoading(), "Studio window");
  const win = current().win;
  const js = source => win.webContents.executeJavaScript(source);
  await until(() => js('!!document.querySelector(".chrome")'), "real Studio workbench");
  await until(() => js('!document.getElementById("boot")'), "startup overlay finishes");
  assert.equal(await js('window.reasonixHost.globalShortcut().then(value => value.status)'), "disabled");
  await js('document.querySelector(\'[data-action="chrome.settings"]\').click()');
  await until(() => js('!!document.querySelector(\'[data-action="settings.section"][data-value="appearance"]\')'), "settings navigation");
  await js('document.querySelector(\'[data-action="settings.section"][data-value="appearance"]\').click()');
  await until(() => js('!!document.querySelector("#set-global-shortcut input:not(:disabled)")'), "shortcut settings");
  const out = process.env.REASONIX_SCREENSHOTS;
  async function screenshots(stage) {
    if (!out) return;
    fs.mkdirSync(out, { recursive: true });
    for (const width of [1280, 390]) {
      win.setSize(1280, 1000);
      win.webContents.enableDeviceEmulation({ screenPosition: "desktop", screenSize: { width, height: 1000 }, viewSize: { width, height: 1000 }, deviceScaleFactor: 1, scale: 1 });
      for (const mode of ["light", "dark"]) {
        await js(`document.querySelector('[data-action="appearance.scheme"][data-value="${mode}"]').click()`);
        await js('document.querySelector("#set-global-shortcut").scrollIntoView({block:"center"})');
        await wait(350);
        const png = await win.webContents.capturePage({ x: 0, y: 0, width, height: 1000 });
        fs.writeFileSync(path.join(out, `${stage}-${mode}-${width}.png`), png.toPNG());
      }
    }
    win.webContents.disableDeviceEmulation();
  }
  await screenshots("before-disabled");
  await js('document.querySelector(\'button[data-action="window.global-shortcut"][data-value="record"]\').click()');
  win.webContents.sendInputEvent({ type: "keyDown", keyCode: "F9", modifiers: ["control", "shift"] });
  win.webContents.sendInputEvent({ type: "keyUp", keyCode: "F9", modifiers: ["control", "shift"] });
  await until(() => js(`document.querySelector("#set-global-shortcut input").value === "${binding}"`), "captured modifier chord");
  await js('document.querySelector(\'button[data-action="window.global-shortcut"][data-value="apply"]\').click()');
  await until(() => js('window.reasonixHost.globalShortcut().then(value => value.status === "registered")'), "real OS registration");
  assert.equal(globalShortcut.isRegistered(binding), true);
  console.log(`ok: real Studio settings capture/apply registered the native ${process.platform} binding`);
  await screenshots("after-registered");
  if (process.platform === "darwin") {
    execFileSync("osascript", ["-e", `tell application "System Events" to set frontmost of first application process whose unix id is ${process.pid} to true`], { timeout: 10000 });
  }
  win.show(); win.focus();
  await until(() => win.isFocused(), "focused Studio window");
  const press = () => process.platform === "darwin"
    ? execFileSync("osascript", ["-e", 'tell application "System Events" to key code 101 using {control down, shift down}'], { timeout: 10000 })
    : execFileSync(input, ["key", "--clearmodifiers", "ctrl+shift+F9"]);
  press();
  await until(() => !win.isVisible(), "OS key hides focused Studio");
  press();
  await until(() => win.isVisible() && win.isFocused(), "OS key restores hidden Studio");
  console.log("ok: real OS key hides the focused window and restores it from hidden");
  win.minimize();
  await until(() => win.isMinimized(), "minimized Studio");
  press();
  await until(() => !win.isMinimized() && win.isVisible() && win.isFocused(), "OS key restores minimized Studio");
  console.log("ok: real OS key restores and focuses the minimized window");
  if (process.env.REASONIX_XMESSAGE) {
    neighbour = spawn(process.env.REASONIX_XMESSAGE, ["-title", "Reasonix shortcut smoke neighbour", "-buttons", "OK", "Separate native application"], { stdio: "ignore" });
    await until(() => {
      try { return execFileSync(input, ["search", "--name", "Reasonix shortcut smoke neighbour"]).toString().trim(); } catch { return false; }
    }, "external application window");
    const id = execFileSync(input, ["search", "--name", "Reasonix shortcut smoke neighbour"]).toString().trim().split("\n")[0];
    execFileSync(input, ["windowfocus", "--sync", id]);
    await until(() => !win.isFocused() && win.isVisible(), "external application takes focus");
    press();
    await until(() => win.isFocused() && win.isVisible(), "OS key raises Studio from another app");
    console.log("ok: real OS key raises visible, unfocused Studio while a separate native application has focus");
    neighbour.kill();
  }
  assert.equal(globalShortcut.register("Control+Shift+F10", () => {}), true);
  const failed = await js('window.reasonixHost.setGlobalShortcut("Control+Shift+F10")');
  assert.deepEqual(failed, { binding, status: "registered", error: "unavailable" });
  globalShortcut.unregister("Control+Shift+F10");
  const saved = JSON.parse(fs.readFileSync(path.join(app.getPath("userData"), "window-prefs.json"), "utf8"));
  assert.equal(saved["shell-global-shortcut"], binding);
  assert.equal((await js('window.reasonixHost.setGlobalShortcut("C")')).error, "invalid");
  assert.equal(globalShortcut.isRegistered("C"), false);
  assert.equal((await js('window.reasonixHost.setGlobalShortcut("invalid shortcut value")')).error, "invalid");
  assert.equal((await js('window.reasonixHost.setGlobalShortcut("")')).status, "disabled");
  assert.equal(globalShortcut.isRegistered(binding), false);
  console.log("ok: native conflict/invalid entries preserve prior binding and disabling unregisters it");
}

run().then(() => { app.quit(); }).catch(error => {
  neighbour?.kill();
  console.error(error.stack || error);
  app.exit(1);
});

"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow, nativeTheme } = require("electron");
const { GROUND } = require("../src/ground");
const { loadPrefs, prefsFile } = require("../src/prefs");
const { until, wait } = require("./livekit");

const home = process.env.REASONIX_HOME;
if (!home) throw new Error("Use an isolated REASONIX_HOME for the native theme smoke");
fs.mkdirSync(home, { recursive: true });
if (!fs.existsSync(path.join(home, "config.toml"))) {
  fs.writeFileSync(path.join(home, "config.toml"), 'default_model = "smoke/smoke-model"\n[desktop]\nwelcomed = true\n[[providers]]\nname = "smoke"\nkind = "openai"\nbase_url = "http://127.0.0.1:9/v1"\nmodels = ["smoke-model"]\ndefault = "smoke-model"\napi_key_env = "SMOKE_API_KEY"\n');
  fs.writeFileSync(path.join(home, ".env"), "SMOKE_API_KEY=smoke\n");
}
process.chdir(home);
fs.mkdirSync(path.join(home, "electron-profile"), { recursive: true });
app.setPath("userData", path.join(home, "electron-profile"));
const startups = new Map();
app.on("browser-window-created", (_event, win) => {
  // Native options settle after creation; load-start still precedes page paint.
  win.webContents.once("did-start-loading", () => {
    startups.set(win, { source: nativeTheme.themeSource, background: win.getBackgroundColor(), saved: loadPrefs(prefsFile(app.getPath("userData"))) });
  });
});
const { current } = require("../src/main");
let untrusted;

async function run() {
  const win = await until("Studio window", () => current().win && !current().win.webContents.isLoading() && current().win);
  const js = source => win.webContents.executeJavaScript(source);
  await until("real Studio workbench", () => js('!!document.querySelector(".chrome") && !document.getElementById("boot")'));
  const saved = () => loadPrefs(prefsFile(app.getPath("userData")));
  async function matches(scheme, source = scheme) {
    await until(`native ${scheme}/${source} repaint`, async () => nativeTheme.themeSource === source &&
      win.getBackgroundColor().toLowerCase().endsWith(GROUND[scheme].slice(1)) &&
      await js(`document.documentElement.dataset.theme === ${JSON.stringify(scheme)}`));
    console.log(JSON.stringify({ page: scheme, source: nativeTheme.themeSource, background: win.getBackgroundColor() }));
  }
  if (process.env.REASONIX_THEME_RESTART === "1") {
    const startup = startups.get(win);
    assert.equal(startup.saved["rx-theme"], "scheduled");
    assert.equal(startup.source, startup.saved["rx-theme-resolved"]);
    assert.ok(startup.background.toLowerCase().endsWith(GROUND[startup.source].slice(1)), JSON.stringify(startup));
    const clock = new Date();
    const actual = clock.getHours() >= 20 || clock.getHours() < 7 ? "dark" : "light";
    assert.notEqual(actual, startup.source);
    await matches(actual);
    assert.equal(await js('localStorage.getItem("rx-theme")'), "scheduled");
    console.log("ok restart: cached native startup tint, then current local-clock page/native repaint on a new kernel origin");
    return;
  }

  nativeTheme.themeSource = "system";
  const systemDark = nativeTheme.shouldUseDarkColors;
  console.log(`host OS dark=${systemDark}; platform=${process.platform}`);
  await js(`(() => {
    const RealDate = Date;
    window.__themeClock = { at: RealDate.now(), began: performance.now() };
    window.Date = class extends RealDate {
      constructor(...args) { super(...(args.length ? args : [window.__themeClock.at + performance.now() - window.__themeClock.began])); }
      static now() { return window.__themeClock.at + performance.now() - window.__themeClock.began; }
    };
  })()`);
  async function clock(hour, minute = 0, second = 0) {
    const at = new Date(2026, 9, 29, hour, minute, second).getTime();
    await js(`window.__themeClock = { at: ${at}, began: performance.now() }; window.dispatchEvent(new Event("focus"))`);
  }
  await js('document.querySelector(\'[data-action="chrome.settings"]\').click()');
  await until("settings navigation", () => js('!!document.querySelector(\'[data-action="settings.section"][data-value="appearance"]\')'));
  await js('document.querySelector(\'[data-action="settings.section"][data-value="appearance"]\').click()');
  await until("appearance modes", () => js('!!document.querySelector(\'[data-action="appearance.scheme"][data-value="scheduled"]\')'));
  const mode = value => js(`document.querySelector('[data-action="appearance.scheme"][data-value="${value}"]').click()`);
  async function change(field, value) {
    await js(`(() => {
      const input = document.querySelector('[data-action="appearance.schedule"][data-target="${field}"]');
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(input, ${JSON.stringify(value)});
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.dispatchEvent(new Event("change", { bubbles: true }));
    })()`);
  }
  await clock(19, 59, 57);
  await mode("scheduled");
  await matches("light");
  await matches("dark");
  console.log("ok real boundary timeout changes both nativeTheme.themeSource and BrowserWindow background");
  await clock(6, 59, 58);
  await matches("dark");
  await matches("light");
  await clock(21);
  await matches("dark");
  await change("start", "22:00");
  await matches("light");
  await change("start", "20:00");
  await matches("dark");
  await change("end", "21:00");
  await matches("light");
  await change("end", "07:00");
  await matches("dark");
  await js('document.querySelector(\'[data-action="theme.activate"][data-value="official-clear-blue"]\').click()');
  await until("active theme pack", () => js('document.documentElement.dataset.pack === "official-clear-blue"'));
  await matches("dark");
  await js('document.querySelector(".appearance-advanced").open = true; document.querySelector(\'[data-action="appearance.contrast"][data-value="strong"]\').click()');
  await matches("dark");
  await clock(8);
  await matches("light");
  assert.equal(await js('document.documentElement.dataset.pack'), "official-clear-blue");
  assert.equal(await js('document.documentElement.dataset.contrast'), "strong");
  console.log("ok start/end edits, focus resume, active pack and contrast keep the native scheme aligned");

  await clock(systemDark ? 8 : 21);
  await matches(systemDark ? "light" : "dark");
  await mode("auto");
  await until("Auto native system following", () => nativeTheme.themeSource === "system");
  const systemScheme = systemDark ? "dark" : "light";
  await matches(systemScheme, "system");
  await js('localStorage.setItem("rx-theme", "scheduled"); localStorage.setItem("rx-weight", "heavy"); window.dispatchEvent(new Event("pagehide"))');
  assert.equal(saved()["rx-theme"], "scheduled");
  await matches(systemScheme, "system");
  await js('localStorage.setItem("rx-theme", "auto")');
  console.log("ok simulated stale prefs:save cannot override the latest Auto paint or pin system following");
  await clock(8);
  await mode("scheduled");
  await matches("light");
  await mode("dark");
  await matches("dark");
  await js('window.reasonixHost.syncTheme("scheduled", "auto"); window.reasonixHost.syncTheme("light", "dark"); window.reasonixHost.syncTheme("invalid", "light")');
  await wait(200);
  await matches("dark");
  untrusted = new BrowserWindow({ show: false, webPreferences: { preload: path.join(__dirname, "../src/preload.js"), sandbox: true, contextIsolation: true, nodeIntegration: false, partition: "theme-smoke-untrusted" } });
  await untrusted.loadURL("data:text/html,<title>Untrusted theme reporter</title>");
  await untrusted.webContents.executeJavaScript('window.reasonixHost.syncTheme("scheduled", "light")');
  await wait(200);
  await matches("dark");
  untrusted.destroy(); untrusted = null;
  await js('const iframe = document.createElement("iframe"); iframe.id = "theme-subframe"; iframe.srcdoc = "<p>Untrusted subframe</p>"; document.body.append(iframe)');
  await until("isolated subframe", () => js('!!document.getElementById("theme-subframe").contentDocument?.querySelector("p")'));
  assert.equal(await js('typeof document.getElementById("theme-subframe").contentWindow.reasonixHost'), "undefined");
  await js('document.getElementById("theme-subframe").remove()');
  console.log("ok real untrusted renderer and invalid payloads cannot repaint native chrome; subframes have no bridge");

  await mode("scheduled");
  const real = new Date();
  const restartScheme = real.getHours() >= 20 || real.getHours() < 7 ? "light" : "dark";
  await clock(restartScheme === "dark" ? 21 : 8);
  await matches(restartScheme);
  await js('window.dispatchEvent(new Event("pagehide"))');
  assert.equal(saved()["rx-theme"], "scheduled");
  assert.equal(saved()["rx-theme-resolved"], restartScheme);
  const out = process.env.REASONIX_SCREENSHOTS;
  if (out) {
    fs.mkdirSync(out, { recursive: true });
    await js('document.getElementById("set-mode").scrollIntoView({block:"start"})');
    await wait(350);
    fs.writeFileSync(path.join(out, "native-scheduled.png"), (await win.webContents.capturePage()).toPNG());
  }
  console.log("ok saved opposite-clock native tint; run again with REASONIX_THEME_RESTART=1 to check a complete restart");
  console.log("note Windows border rendering and changing the host OS preference are not exercised on this host");
}

let exitCode = 0;
app.once("will-quit", () => app.exit(exitCode));
run().then(() => app.quit()).catch(error => {
  untrusted?.destroy();
  console.error(error.stack || error);
  exitCode = 1;
  app.quit();
});

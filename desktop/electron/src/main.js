"use strict";
const { app, BrowserWindow, dialog, ipcMain, nativeTheme, powerSaveBlocker, screen, session, shell } = require("electron");
const { relaunchForOzonePlatform } = require("./ozone");

// Before the instance lock: this process must hold nothing its relaunch needs.
if (relaunchForOzonePlatform(app, process)) return;
const fs = require("node:fs/promises");
const { existsSync } = require("node:fs");
const path = require("node:path");
const { start, shouldRetry, HANDSHAKE_TIMEOUT_MS } = require("./host");
const { showStarting } = require("./starting");
const { StudioHost } = require("./hostclient");
const { installTray } = require("./tray");
const { instanceID, profileFor } = require("./instance");
const { installApplicationMenu, installContextMenu } = require("./menu");
const { uiLanguage } = require("./uilang");
const { installFullScreenKey } = require("./fullscreen");
const { installReload } = require("./reload");
const { externalTarget } = require("./links");
const { reveal, revealWorkspace } = require("./reveal");
const { appIcon } = require("./appicon");
const layout = require("./layout");
const { offerCleanup } = require("./legacy");
const { stripPackageGrants, unpaintedWindowCause } = require("./packagegrants");
const { BrowserProtocol } = require("./browserprotocol");
const { BrowserViews } = require("./browserviews");
const { startBrowserRelay } = require("./browserrelay");
const { groundFor } = require("./ground");
const { loadPrefs, prefsFile, registerPrefs } = require("./prefs");
const { createPowerGuard, keepsAwake } = require("./powerguard");
const { openLogs, redactArgv, failStartup } = require("./shelllog");
const { crashDir, recordHostExit, pendingHostExit, clearHostExit, hostExitNotice } = require("./hostexit");

// A page in a minimized or fully covered window counts as hidden, and a hidden
// page drops the input the agent sends it: measured, its clicks never arrive and
// the protocol call does not even answer. The agent works while the person is in
// another app, so occlusion must not reach its pages. The cost is that this
// application's renderers keep running at full rate while nobody is looking.
app.commandLine.appendSwitch("disable-backgrounding-occluded-windows");

// Must match serve.TokenCookie and the path the kernel serves the page on. The
// page owns the root: its assets are relative, so a path the kernel does not
// serve them under loads the shell and none of its modules.
const TOKEN_COOKIE = "reasonix_token";
const PAGE_PATH = "/";
// Puts the lights' centre on the content line the chrome row draws its own
// controls on. AppKit takes the top edge and seats them a point above what is
// asked, so this is measured against the window rather than computed from it.
const LIGHTS = { x: 11, y: 20 };
const DEFAULT_SIZE = { width: 1440, height: 900 };
const MIN_SIZE = { width: 760, height: 480 };
const HOST_DRAIN_MS = 1000;
const RETRY_DELAY_MS = 1000;

const where = {
  packaged: app.isPackaged,
  resourcesPath: process.resourcesPath,
  dirname: __dirname,
  platform: process.platform,
  env: process.env,
};
const hostBinary = layout.hostBinary(where);
const computerHelper = layout.computerHelper(where);
const pageDir = layout.pageDir(where);

let kernel = null;
let win = null;
let origin = "";
let quitting = false;
let client = null;
let tray = null;
let grants = null;
let browserViews = null;
let browserRelay = null;
let reload = null;
let powerGuard = null;
let logs = null;
let handshaken = false;

let starting = null;
function closeStarting() {
  const win = starting;
  starting = null;
  if (win && !win.isDestroyed()) win.close();
}

function handshakeTimeout() {
  const ms = Number(process.env.REASONIX_STUDIO_HANDSHAKE_TIMEOUT_MS);
  return Number.isFinite(ms) && ms > 0 ? ms : HANDSHAKE_TIMEOUT_MS;
}

// A kernel that exits or cannot be spawned within seconds is tried once more:
// a scanner holding a freshly written binary lets go about that fast. A slow
// kernel is waited for instead, never restarted.
async function launchKernel(args) {
  for (let attempt = 1; attempt <= 2; attempt++) {
    if (quitting) throw new Error("the launch was ended by quit");
    const began = Date.now();
    logs.shell.line(`host: starting ${hostBinary} (attempt ${attempt})`);
    kernel = start(hostBinary, args, {
      systemLanguage: app.getPreferredSystemLanguages()[0] ?? app.getLocale(),
      timeoutMs: handshakeTimeout(),
      onSlow: () => {
        logs.shell.line("host: no handshake yet; showing the starting window");
        if (starting || quitting) return;
        const win = showStarting(app.getLocale());
        starting = win;
        win.once("closed", () => {
          if (starting !== win) return;
          starting = null;
          logs.shell.line("host: starting window closed; ending the launch");
          app.quit();
        });
      },
      onStderr: (text) => {
        logs.host.raw(text);
        process.stderr.write(text);
      },
      onExit: (code, signal) => {
        logs.shell.line(`host: exited code=${code} signal=${signal}${handshaken ? "" : " before its handshake"}`);
        // Before the handshake the launch itself fails, and boot's catch owns
        // telling the person why; quitting here would race that dialog.
        powerGuard?.close();
        if (handshaken && code !== 0 && !quitting) {
          recordHostExit({ logs, userData: app.getPath("userData"), code, signal, startedAt: began });
          app.quit();
        }
      },
      onAct: handOver,
    });
    const current = kernel;
    current.child.on("error", (err) => logs.shell.line(`host: spawn failed: ${err.message}`));
    current.child.stderr.on("close", () => logs.host.flush());
    try {
      return await current.ready;
    } catch (err) {
      if (quitting || !shouldRetry(err, attempt, Date.now() - began)) {
        err.attempts = attempt;
        closeStarting();
        throw err;
      }
      logs.shell.line(`host: ${err.message}; retrying once`);
      await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY_MS));
    }
  }
}

async function boot() {
  // Which build this is belongs to the shell: inside the bundle the kernel's
  // own os.Executable() names the host binary, not the application around it.
  // Only a packaged build has a version worth reporting -- app.getVersion()
  // falls back to Electron's own, which named a Studio that never shipped and
  // ranked it ahead of every published release.
  const args = ["-page", pageDir, "-crash-dir", crashDir(logs.dir)];
  if (computerHelper && existsSync(computerHelper)) args.push("-computer-helper", computerHelper);
  if (app.isPackaged) {
    args.push("-studio-version", app.getVersion());
    // The other half the kernel cannot work out: which file the application
    // runs as, and which process holds it open while an update waits to
    // replace it. Both are this process's, and the binary it spawned lives
    // inside the bundle rather than being it.
    args.push("-studio-app", process.execPath, "-studio-app-pid", String(process.pid));
  }
  const ready = await launchKernel(args);
  logs.addSecret(ready.token);
  handshaken = true;
  logs.shell.line(`host: handshake from ${ready.origin}`);
  origin = ready.origin;
  client = new StudioHost(ready.origin, ready.token);
  await armCredential(ready);
  powerGuard = createPowerGuard({
    blocker: powerSaveBlocker,
    running: () => client.trayRunning(),
    enabled: () => keepsAwake(loadPrefs(prefsFile(app.getPath("userData")))),
  });
  powerGuard.begin();
  // The poll alone leaves a turn unprotected for up to its period; the page's
  // own submit is the moment one starts, and this process sees it complete.
  session.defaultSession.webRequest.onCompleted({ urls: [`${ready.origin}/*submit`] }, (details) => {
    if (details.method === "POST" && details.statusCode < 300) void powerGuard?.refresh();
  });
  win = createWindow();
  closeStarting();
  guard(win.webContents);
  win.webContents.once("render-process-gone", (_event, details) => {
    if (win.isVisible() || details.reason !== "crashed") return;
    const cause = unpaintedWindowCause(grants, app.getLocale());
    if (cause) dialog.showErrorBox(cause.title, cause.detail);
  });
  installContextMenu(win.webContents, win, uiLang);
  installFullScreenKey(win.webContents, win);
  reload = installReload(win.webContents, win);
  win.once("ready-to-show", () => win.show());
  // No icon, no backgrounding: the close button can only hide the window where
  // something is left that brings it back.
  tray = installTray(client, { onOpen: showWindow, onQuit: () => app.quit() });
  win.on("close", onWindowClose);
  hostAgentBrowser();
  await win.loadURL(origin + PAGE_PATH);
  await tray?.refresh();
  // After the window, deliberately. This asks about an install left behind by
  // the shell this one replaces, and a modal in front of a window that has not
  // painted reads as the application having failed to start.
  cleanUpLegacyInstalls();
  announceHostExit();
}

// The agent's browser draws its pages as views in this window: the kernel
// drives them through the relay, and the page decides where one is shown.
function hostAgentBrowser() {
  browserViews = new BrowserViews({ win, kernelOrigin: origin });
  const protocol = new BrowserProtocol({
    createView: (spec) => browserViews.create(spec),
    post: (frame) => browserRelay?.post(frame),
  });
  browserRelay = startBrowserRelay({
    client,
    onFrame: (frame) => protocol.receive(frame),
    onDrop: () => protocol.drop(),
  });
}

// Told once: the marker stays until the person has seen the notice.
function announceHostExit() {
  const userData = app.getPath("userData");
  const exit = pendingHostExit(userData);
  if (!exit) return;
  const text = hostExitNotice(uiLang(), exit, logs.dir);
  dialog
    .showMessageBox(win, { type: "warning", message: text.message, detail: text.detail, buttons: [text.ok] })
    .then(() => clearHostExit(userData))
    .catch((err) => logs.shell.line(`host exit notice: ${err.message}`));
}

// The Wails install a dmg download leaves beside this one. Detached from boot:
// a launch must not wait on it, and a failure here is not a failed launch.
function cleanUpLegacyInstalls() {
  offerCleanup({
    packaged: app.isPackaged,
    execPath: process.execPath,
    userData: app.getPath("userData"),
    ask: async (bundle) => {
      const { response } = await dialog.showMessageBox(win, {
        type: "question",
        buttons: ["移到废纸篓", "先留着"],
        defaultId: 0,
        cancelId: 1,
        message: "找到一个旧版本的 Reasonix Studio",
        detail: `${bundle}\n\n它和当前这个共用同一份数据，所以两个图标打开的是同一个窗口。移到废纸篓不会动你的会话和设置。`,
      });
      return response === 0;
    },
    trash: (bundle) => shell.trashItem(bundle),
  }).catch((err) => {
    console.error("reasonix-studio: legacy cleanup:", err.message);
    logs.shell.line(`legacy cleanup: ${err.message}`);
  });
}

// Set before anything is loaded, or the first request answers 403 and the
// window opens on a refusal. HttpOnly because nothing in the page ever reads
// it, Strict because no cross-site navigation ever needs to carry it, and no
// expiry so it dies with this session rather than outliving the launch.
async function armCredential(ready) {
  await session.defaultSession.cookies.set({
    url: ready.origin + "/",
    name: TOKEN_COOKIE,
    value: ready.token,
    path: "/",
    httpOnly: true,
    secure: false,
    sameSite: "strict",
  });
}

// The close button hides where an icon can bring the window back, and quits
// where it cannot. Read from the cached preference rather than asked for on the
// spot: a close must not wait on the kernel, nor fail once it has gone.
function onWindowClose(event) {
  if (quitting || !tray?.prefs()?.closeToTray) return;
  event.preventDefault();
  win.hide();
}

// showWindow brings it back from wherever it went. Show alone is a no-op on a
// window that is merely buried, so the focus is what actually raises it.
function showWindow() {
  if (starting && !starting.isDestroyed()) starting.focus();
  if (quitting || !win || win.isDestroyed()) return;
  if (win.isMinimized()) win.restore();
  reload?.revive();
  win.show();
  win.focus();
}

// A window larger than the display it opens on puts its own title bar off
// screen, and a frameless one takes the only way to move it along with it.
function fitted() {
  const area = screen.getPrimaryDisplay().workAreaSize;
  return {
    width: Math.max(MIN_SIZE.width, Math.min(DEFAULT_SIZE.width, area.width)),
    height: Math.max(MIN_SIZE.height, Math.min(DEFAULT_SIZE.height, area.height)),
  };
}

const ground = () => groundFor(loadPrefs(prefsFile(app.getPath("userData"))), nativeTheme.shouldUseDarkColors);

function createWindow() {
  const mac = process.platform === "darwin";
  const windows = process.platform === "win32";
  const chrome = mac || windows;
  return new BrowserWindow({
    ...fitted(),
    ...appIcon(),
    minWidth: MIN_SIZE.width,
    minHeight: MIN_SIZE.height,
    // Shown once it has been measured against the screen it landed on; sizing a
    // visible window makes the correction a flicker.
    show: false,
    backgroundColor: ground(),
    frame: !windows,
    titleBarStyle: mac ? "hiddenInset" : "default",
    ...(mac ? { trafficLightPosition: LIGHTS } : {}),
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      // Stated rather than left to the defaults: this is the list a reviewer
      // reads to know what the renderer may reach.
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webviewTag: false,
      webSecurity: true,
      additionalArguments: [`--reasonix-titlebar=${chrome ? "1" : "0"}`],
    },
  });
}

// The page may only ever be the origin the kernel handed us. A navigation
// anywhere else is refused rather than followed, and a second window is never
// opened at all — a link leaves through the platform opener or not at all.
function guard(contents) {
  contents.on("will-navigate", (event, url) => {
    if (!url.startsWith(origin + "/")) event.preventDefault();
  });
  contents.setWindowOpenHandler(() => ({ action: "deny" }));
}

// Every handler answers the one window this launch owns. A sender that is not
// its contents is refused rather than served.
function fromWindow(event) {
  return win && !win.isDestroyed() && event.sender === win.webContents ? win : null;
}

// The page reads navigator.languages[0], which is the first preferred system
// language; app.getLocale() is the locale Chromium resolved, which can differ.
const uiLang = () => uiLanguage(loadPrefs(prefsFile(app.getPath("userData"))), app.getPreferredSystemLanguages()[0] ?? app.getLocale());
registerPrefs(ipcMain, () => prefsFile(app.getPath("userData")), fromWindow, () => {
  installApplicationMenu(uiLang);
  if (win && !win.isDestroyed()) win.setBackgroundColor(ground());
  void powerGuard?.refresh();
});
nativeTheme.on("updated", () => {
  if (win && !win.isDestroyed()) win.setBackgroundColor(ground());
});

ipcMain.handle("window:minimise", (event) => {
  fromWindow(event)?.minimize();
});
ipcMain.handle("window:toggle-maximise", (event) => {
  const target = fromWindow(event);
  if (!target) return;
  if (target.isMaximized()) target.unmaximize();
  else target.maximize();
});
ipcMain.handle("window:is-maximised", (event) => fromWindow(event)?.isMaximized() ?? false);
ipcMain.handle("window:close", (event) => {
  fromWindow(event)?.close();
});
ipcMain.handle("browser:show", (event, targetId, rect) => {
  if (fromWindow(event)) browserViews?.show(String(targetId), rect);
});
ipcMain.handle("browser:hide", (event) => {
  if (fromWindow(event)) browserViews?.hide();
});
ipcMain.handle("browser:freeze", (event) => (fromWindow(event) ? (browserViews?.freeze() ?? "") : ""));
ipcMain.handle("browser:control", (event, targetId, action) => {
  if (fromWindow(event)) browserViews?.control(String(targetId), String(action));
});
ipcMain.handle("browser:navigate", (event, targetId, address) =>
  fromWindow(event) ? (browserViews?.navigate(String(targetId), String(address)) ?? "scheme") : "scheme",
);
ipcMain.handle("browser:trust-certificate", (event, targetId) =>
  fromWindow(event) ? (browserViews?.trustCertificate(String(targetId)) ?? false) : false,
);
ipcMain.handle("browser:login-answer", (event, id, username, password) => {
  if (fromWindow(event)) browserViews?.answerLogin(String(id), String(username || ""), String(password || ""));
});
ipcMain.handle("shell:open-external", (event, raw) => {
  if (!fromWindow(event)) return;
  const target = externalTarget(raw);
  if (target) return shell.openExternal(target);
});
ipcMain.handle("shell:reveal-workspace", (event, root) =>
  fromWindow(event) && client ? revealWorkspace(client, shell, String(root)) : { code: "", error: "no window" },
);
ipcMain.handle("shell:reveal", (event, base, rel) =>
  fromWindow(event) && client ? reveal(client, shell, String(base), String(rel)) : { code: "", error: "no window" },
);

// A dismissed dialog answers with "", which is what the page reads as "they
// said no". Only a failure to write is an error.
async function saveTo(event, name, write) {
  const target = fromWindow(event);
  if (!target) return "";
  const picked = await dialog.showSaveDialog(target, { defaultPath: name });
  if (picked.canceled || !picked.filePath) return "";
  await write(picked.filePath);
  return picked.filePath;
}

ipcMain.handle("dialog:save-text", (event, name, content) =>
  saveTo(event, name, (path) => fs.writeFile(path, content, "utf8")),
);
ipcMain.handle("dialog:save-bytes", (event, name, bytes) =>
  saveTo(event, name, (path) => fs.writeFile(path, Buffer.from(bytes))),
);

// createDirectory is the half that carries meaning: a panel that can only open
// what exists reads as an app that cannot start a project, which is what the
// Wails picker was reported as before it said so. A dismissed panel answers ""
// like the save dialogs, and startIn is dropped when it names nothing.
ipcMain.handle("dialog:pick-folder", async (event, startIn) => {
  const target = fromWindow(event);
  if (!target) return "";
  const picked = await dialog.showOpenDialog(target, {
    defaultPath: startIn || undefined,
    properties: ["openDirectory", "createDirectory"],
  });
  if (picked.canceled || !picked.filePaths.length) return "";
  return picked.filePaths[0];
});

// Named before any path is derived from it: userData hangs off the app name,
// and a name that depended on how this was launched would put two launches of
// the same install on two profiles — and so on two locks.
app.setName("Reasonix Studio");

// Claimed before anything is created: the profile decides which launches share
// a lock, and Chromium reads it the moment the app is ready. A launch that does
// not get the lock has to leave without spawning a kernel of its own — the one
// already running owns those session files.
const identity = instanceID(hostBinary);
app.setPath("userData", profileFor(app.getPath("userData"), identity));
// A launch from a shortcut has no console, so this file is the only place a
// failure before the window can be read back from.
logs = openLogs(app.getPath("userData"));
logs.shell.line(
  `shell: start version=${app.getVersion()} packaged=${app.isPackaged} platform=${process.platform}/${process.arch} ` +
    `instance=${identity || "(unknown)"} argv=${JSON.stringify(redactArgv(process.argv))}`,
);
const primary = app.requestSingleInstanceLock();
if (!primary) {
  // The ordinary second launch: the running instance raises its own window.
  logs.shell.line(`shell: another instance holds the lock (instance=${identity || "(unknown)"}); leaving`);
  app.quit();
} else {
  app.on("second-instance", showWindow);
  // macOS reopens an existing app through activation, without a second process.
  app.on("activate", showWindow);
  grants = stripPackageGrants(hostBinary, { ...where, execPath: process.execPath });
  if (grants?.stripped.length) {
    const note = `removed app-package grants that stop sandboxed processes loading: ${grants.stripped.join(", ")}`;
    console.error(`reasonix-studio: ${note}`);
    logs.shell.line(note);
  }
  if (grants?.refused.length) logs.shell.line(`app-package grants that could not be removed: ${grants.refused.join(", ")}`);
}

app.whenReady().then(() => {
  if (!primary) return;
  installApplicationMenu(uiLang);
  boot().catch((err) => {
    console.error("reasonix-studio:", err.message);
    if (quitting) {
      logs.shell.line(`startup ended by quit: ${err.message}`);
      return;
    }
    void hostLastWords().then((words) => {
      if (quitting) return logs.shell.line(`startup ended by quit: ${err.message}`);
      failStartup({ app, dialog, logs, locale: app.getLocale() }, err, words);
    });
  });
});

// A child's exit can be reported before its stderr has drained, and what it
// said last is usually the reason it gave up. Bounded, for a host still running.
async function hostLastWords() {
  if (handshaken || !kernel) return "";
  const stderr = kernel.child.stderr;
  if (!stderr.closed) {
    await new Promise((resolve) => {
      const timer = setTimeout(resolve, HOST_DRAIN_MS);
      stderr.once("close", () => {
        clearTimeout(timer);
        resolve();
      });
    });
  }
  return logs.host.tail();
}

// The acts a handover asks of the application. The kernel decides when: it has
// downloaded and staged a replacement, and what is left is the part only this
// process can do. They arrive in this order, so arming the restart before
// ending is the sequence rather than a coincidence.
//
// Nothing is answered. Both are performed by ending, so an acknowledgement
// would have to come from a process on its way out.
function handOver(act) {
  if (act === "relaunch") {
    app.relaunch();
    return;
  }
  if (act === "quit") app.quit();
}

// Before the handshake the only window is the starting one, and the launch
// decides for itself whether to go on.
app.on("window-all-closed", () => {
  if (handshaken) app.quit();
});

// Closing this end of the pipe is what tells the kernel to drain. Without it a
// session file is left being written by a process nobody is holding open.
// What this launch is holding, for a test that drives the real shell rather
// than a copy of it.
module.exports = { current: () => ({ win, tray, client, origin }) };

app.on("before-quit", () => {
  quitting = true;
  logs.shell.line("shell: quitting");
  browserRelay?.stop();
  powerGuard?.close();
  tray?.close();
  kernel?.child.stdin.end();
});

"use strict";
const { app, BrowserWindow, dialog, ipcMain, screen, session, shell } = require("electron");
const { relaunchForOzonePlatform } = require("./ozone");

// Before the instance lock: this process must hold nothing its relaunch needs.
if (relaunchForOzonePlatform(app, process)) return;
const fs = require("node:fs/promises");
const { existsSync } = require("node:fs");
const path = require("node:path");
const { start } = require("./host");
const { StudioHost } = require("./hostclient");
const { installTray } = require("./tray");
const { instanceID, profileFor } = require("./instance");
const { installApplicationMenu, installContextMenu } = require("./menu");
const { installFullScreenKey } = require("./fullscreen");
const { externalTarget } = require("./links");
const { reveal } = require("./reveal");
const { appIcon } = require("./appicon");
const layout = require("./layout");
const { offerCleanup } = require("./legacy");
const { stripPackageGrants, unpaintedWindowCause } = require("./packagegrants");
const { BrowserProtocol } = require("./browserprotocol");
const { BrowserViews } = require("./browserviews");
const { startBrowserRelay } = require("./browserrelay");
const { prefsFile, registerPrefs } = require("./prefs");

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

async function boot() {
  // Which build this is belongs to the shell: inside the bundle the kernel's
  // own os.Executable() names the host binary, not the application around it.
  // Only a packaged build has a version worth reporting -- app.getVersion()
  // falls back to Electron's own, which named a Studio that never shipped and
  // ranked it ahead of every published release.
  const args = ["-page", pageDir];
  if (computerHelper && existsSync(computerHelper)) args.push("-computer-helper", computerHelper);
  if (app.isPackaged) {
    args.push("-studio-version", app.getVersion());
    // The other half the kernel cannot work out: which file the application
    // runs as, and which process holds it open while an update waits to
    // replace it. Both are this process's, and the binary it spawned lives
    // inside the bundle rather than being it.
    args.push("-studio-app", process.execPath, "-studio-app-pid", String(process.pid));
  }
  kernel = start(hostBinary, args, {
    onStderr: (text) => process.stderr.write(text),
    onExit: (code) => {
      if (code !== 0 && !quitting) app.quit();
    },
    onAct: handOver,
  });
  const ready = await kernel.ready;
  origin = ready.origin;
  client = new StudioHost(ready.origin, ready.token);
  await armCredential(ready);
  win = createWindow();
  guard(win.webContents);
  win.webContents.once("render-process-gone", (_event, details) => {
    if (win.isVisible() || details.reason !== "crashed") return;
    const cause = unpaintedWindowCause(grants, app.getLocale());
    if (cause) dialog.showErrorBox(cause.title, cause.detail);
  });
  installContextMenu(win.webContents, win);
  installFullScreenKey(win.webContents, win);
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
  }).catch((err) => console.error("reasonix-studio: legacy cleanup:", err.message));
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
  if (!win || win.isDestroyed()) return;
  if (win.isMinimized()) win.restore();
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

registerPrefs(ipcMain, () => prefsFile(app.getPath("userData")), fromWindow);

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
ipcMain.handle("browser:control", (event, targetId, action) => {
  if (fromWindow(event)) browserViews?.control(String(targetId), String(action));
});
ipcMain.handle("browser:navigate", (event, targetId, address) =>
  fromWindow(event) ? (browserViews?.navigate(String(targetId), String(address)) ?? false) : false,
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
  if (target) void shell.openExternal(target);
});
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
const primary = app.requestSingleInstanceLock();
if (!primary) {
  app.quit();
} else {
  app.on("second-instance", showWindow);
  grants = stripPackageGrants(hostBinary, { ...where, execPath: process.execPath });
  if (grants?.stripped.length) {
    console.error(`reasonix-studio: removed app-package grants that stop sandboxed processes loading: ${grants.stripped.join(", ")}`);
  }
}

app.whenReady().then(() => {
  if (!primary) return;
  installApplicationMenu();
  boot().catch((err) => {
    console.error("reasonix-studio:", err.message);
    app.quit();
  });
});

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

app.on("window-all-closed", () => app.quit());

// Closing this end of the pipe is what tells the kernel to drain. Without it a
// session file is left being written by a process nobody is holding open.
// What this launch is holding, for a test that drives the real shell rather
// than a copy of it.
module.exports = { current: () => ({ win, tray, client, origin }) };

app.on("before-quit", () => {
  quitting = true;
  browserRelay?.stop();
  tray?.close();
  kernel?.child.stdin.end();
});

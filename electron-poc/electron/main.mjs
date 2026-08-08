/**
 * Electron main: supervise reasonix serve + serve Wails-style desktop UI dist
 * through a same-origin reverse proxy (lib/desktopShell.mjs).
 */
import path from "node:path";
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  shell,
} from "electron";
import { ServeSupervisor } from "../lib/serveSupervisor.mjs";
import { resolveReasonixBin } from "../lib/resolveReasonixBin.mjs";
import { POC_CAPABILITIES } from "../lib/capabilities.mjs";
import { startDesktopShell, defaultDesktopUiDir } from "../lib/desktopShell.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(packageRoot, "..");

/** @type {ServeSupervisor | null} */
let supervisor = null;
/** @type {BrowserWindow | null} */
let mainWindow = null;
/** @type {Awaited<ReturnType<ServeSupervisor['start']>> | null} */
let lastStart = null;
/** @type {Awaited<ReturnType<typeof startDesktopShell>> | null} */
let desktopShell = null;
let quitting = false;

const gotLock = app.requestSingleInstanceLock();
if (!gotLock) {
  app.quit();
} else {
  app.on("second-instance", () => {
    if (mainWindow) {
      if (mainWindow.isMinimized()) mainWindow.restore();
      mainWindow.focus();
    }
  });
}

function logLine(msg) {
  const line = `[electron-poc] ${msg}\n`;
  process.stderr.write(line);
  if (lastStart?.logFile) {
    try {
      fs.appendFileSync(lastStart.logFile, line);
    } catch {
      /* ignore */
    }
  }
}

function endpointPayload() {
  if (!lastStart) return null;
  // Prefer shell same-origin base so renderer avoids CORS.
  const baseUrl = desktopShell?.baseUrl || lastStart.baseUrl;
  return {
    baseUrl,
    token: lastStart.token,
    uiUrl: desktopShell?.uiUrl || lastStart.uiUrl,
    port: desktopShell?.port || lastStart.port,
    logFile: lastStart.logFile,
    workspace: lastStart.workspace,
    serveBaseUrl: lastStart.baseUrl,
    capabilities: POC_CAPABILITIES,
  };
}

async function startDesktopUiShell() {
  if (desktopShell) {
    await desktopShell.close().catch(() => {});
    desktopShell = null;
  }
  if (!lastStart) throw new Error("serve not started");
  const staticDir = process.env.REASONIX_DESKTOP_UI_DIR || defaultDesktopUiDir();
  logLine(`desktop UI dir: ${staticDir}`);
  desktopShell = await startDesktopShell({
    staticDir,
    serveBaseUrl: lastStart.baseUrl,
    token: lastStart.token,
    workspace: lastStart.workspace,
  });
  logLine(`desktop shell at ${desktopShell.uiUrl}`);
  return desktopShell;
}

/**
 * @param {Awaited<ReturnType<ServeSupervisor['start']>>} info
 */
async function applyServeReady(info) {
  lastStart = info;
  logLine(
    `serve ready at ${info.baseUrl} pid=${info.pid}${info.fromCrashRestart ? " (crash-restart)" : ""}`,
  );
  try {
    await startDesktopUiShell();
  } catch (err) {
    logLine(`desktop shell failed: ${err}`);
    // Fall back to built-in serve UI so the window is never blank.
  }
  const loadUrl = desktopShell?.uiUrl || info.uiUrl;
  if (mainWindow && !mainWindow.isDestroyed() && loadUrl) {
    await mainWindow.loadURL(loadUrl);
    mainWindow.webContents.send("poc:serve-restarted", endpointPayload());
  }
}

async function startSupervisor(workspace) {
  const bin = resolveReasonixBin({
    env: process.env,
    repoRoot,
  });
  logLine(`using reasonix binary: ${bin}`);
  supervisor = new ServeSupervisor({
    bin,
    repoRoot,
    workspace: workspace || process.env.REASONIX_WORKSPACE || process.cwd(),
    crashRestartOnce: process.env.REASONIX_POC_CRASH_RESTART !== "0",
    onExit: (code, signal) => {
      logLine(`serve exited code=${code} signal=${signal}`);
      if (!quitting && mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents
          .executeJavaScript(
            `console.warn(${JSON.stringify(`reasonix serve exited (${code}/${signal})`)})`,
          )
          .catch(() => {});
      }
    },
    onCrashRestarted: async (info) => {
      try {
        await applyServeReady(info);
      } catch (err) {
        logLine(`crash-restart UI refresh failed: ${err}`);
      }
    },
  });
  const info = await supervisor.start();
  await applyServeReady(info);
  return info;
}

function createWindow(uiUrl) {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 840,
    title: "Reasonix Desktop (Electron PoC)",
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });

  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    try {
      const u = new URL(url);
      if (u.hostname === "127.0.0.1" || u.hostname === "localhost") {
        return { action: "allow" };
      }
    } catch {
      /* fallthrough */
    }
    shell.openExternal(url);
    return { action: "deny" };
  });

  mainWindow.loadURL(uiUrl);
  mainWindow.on("closed", () => {
    mainWindow = null;
  });
}

function registerIpc() {
  ipcMain.handle("poc:get-endpoint", async () => endpointPayload());
  ipcMain.on("poc:get-endpoint-sync", (event) => {
    event.returnValue = endpointPayload();
  });
  ipcMain.handle("poc:get-capabilities", async () => POC_CAPABILITIES);
  ipcMain.handle("poc:pick-workspace", async () => {
    // Multi-tab: only pick a directory; frontend OpenProjectTab / host.openProject
    // opens a new Controller tab without restarting serve.
    const res = await dialog.showOpenDialog(mainWindow ?? undefined, {
      properties: ["openDirectory", "createDirectory"],
    });
    if (res.canceled || !res.filePaths[0]) return null;
    const ws = res.filePaths[0];
    return {
      workspace: ws,
      baseUrl: desktopShell?.baseUrl || lastStart?.baseUrl,
    };
  });
  ipcMain.handle("poc:open-log", async () => {
    if (!lastStart?.logFile) return false;
    await shell.openPath(lastStart.logFile);
    return true;
  });
  ipcMain.handle("poc:restart-serve", async () => {
    if (!supervisor) return null;
    const info = await supervisor.restartInWorkspace(
      lastStart?.workspace ?? process.cwd(),
    );
    await applyServeReady(info);
    return endpointPayload();
  });
}

async function boot() {
  registerIpc();
  try {
    await startSupervisor();
    const loadUrl = desktopShell?.uiUrl || lastStart?.uiUrl;
    if (!loadUrl) throw new Error("no UI URL");
    createWindow(loadUrl);
  } catch (err) {
    logLine(`failed to start: ${err}`);
    dialog.showErrorBox(
      "Reasonix Electron PoC",
      `Failed to start:\n\n${err instanceof Error ? err.message : String(err)}\n\n` +
        `Build the desktop UI first:\n  cd electron-poc && npm run build:ui\n` +
        `And ensure reasonix is on PATH or set REASONIX_BIN.`,
    );
    app.quit();
  }
}

app.whenReady().then(boot);

app.on("before-quit", async (e) => {
  if (quitting) return;
  if (supervisor?.pid || desktopShell) {
    e.preventDefault();
    quitting = true;
    try {
      if (desktopShell) await desktopShell.close();
    } catch {
      /* ignore */
    }
    try {
      await supervisor?.stop();
    } catch (err) {
      logLine(`stop error: ${err}`);
    }
    app.exit(0);
  }
});

app.on("window-all-closed", () => {
  app.quit();
});

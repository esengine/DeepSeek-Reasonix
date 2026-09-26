"use strict";
const { contextBridge, ipcRenderer, webUtils } = require("electron");

// Whether the page draws the title bar is decided where the window is created;
// a sandboxed preload cannot require that module, so it is passed in.
const titleBar = process.argv.includes("--reasonix-titlebar=1");

function rectOf(rect) {
  return { x: Number(rect?.x), y: Number(rect?.y), width: Number(rect?.width), height: Number(rect?.height) };
}

function subscribe(channel, listener) {
  const handler = (_event, payload) => listener(payload);
  ipcRenderer.on(channel, handler);
  return () => ipcRenderer.removeListener(channel, handler);
}

// Verbs, and the two facts a layout needs. The origin the page was loaded from
// is already its own; the credential that opens it never crosses here at all.
contextBridge.exposeInMainWorld("reasonixHost", {
  shell: "electron",
  platform: process.platform,
  titleBar,
  minimiseWindow: () => ipcRenderer.invoke("window:minimise"),
  toggleMaximiseWindow: () => ipcRenderer.invoke("window:toggle-maximise"),
  isWindowMaximised: () => ipcRenderer.invoke("window:is-maximised"),
  closeWindow: () => ipcRenderer.invoke("window:close"),
  openExternal: (url) => ipcRenderer.invoke("shell:open-external", String(url)),
  // A pane and a path inside its workspace; main asks the kernel where that is.
  revealPath: (base, rel) => ipcRenderer.invoke("shell:reveal", String(base), String(rel)),
  // Where a dropped file lives. Resolved here rather than in the page: the
  // renderer is handed a File and never a path, and a turn that has to work on
  // the file itself cannot do it on a copy of the bytes.
  pathForFile: (file) => {
    try {
      return webUtils.getPathForFile(file);
    } catch {
      return "";
    }
  },
  saveText: (name, content) => ipcRenderer.invoke("dialog:save-text", String(name), String(content)),
  saveBytes: (name, bytes) => ipcRenderer.invoke("dialog:save-bytes", String(name), bytes),
  pickFolder: (startIn) => ipcRenderer.invoke("dialog:pick-folder", String(startIn)),
  // The agent's browser: which of its pages to draw over a rectangle of this
  // page, and the few controls a person has over a page they are watching.
  showBrowserView: (targetId, rect) => ipcRenderer.invoke("browser:show", String(targetId), rectOf(rect)),
  hideBrowserView: () => ipcRenderer.invoke("browser:hide"),
  controlBrowserView: (targetId, action) => ipcRenderer.invoke("browser:control", String(targetId), String(action)),
  navigateBrowserView: (targetId, address) => ipcRenderer.invoke("browser:navigate", String(targetId), String(address)),
  // Why a page did not load, and a login a page or proxy asks for. The password
  // goes back to this window's main process only; the kernel never sees it.
  onBrowserLoadState: (listener) => subscribe("browser:load-state", listener),
  onBrowserLogin: (listener) => subscribe("browser:login", listener),
  trustBrowserCertificate: (targetId) => ipcRenderer.invoke("browser:trust-certificate", String(targetId)),
  answerBrowserLogin: (id, username, password) =>
    ipcRenderer.invoke("browser:login-answer", String(id), String(username || ""), String(password || "")),
});

// localStorage is keyed by origin and the kernel's port is new each launch, so
// the page's own preferences are restored here, before any page script reads
// them, and handed back as they change. A key the page already holds wins: it
// is at least as new as the copy on disk.
const PREFIX = "rx-";
function ownPrefs() {
  const out = {};
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (key && key.startsWith(PREFIX)) out[key] = localStorage.getItem(key) ?? "";
  }
  return out;
}
try {
  const saved = ipcRenderer.sendSync("prefs:load") || {};
  for (const [key, value] of Object.entries(saved)) {
    if (key.startsWith(PREFIX) && localStorage.getItem(key) === null) localStorage.setItem(key, String(value));
  }
} catch {
  // Storage unavailable: the page runs on its defaults, as it always has.
}
let sent = "";
function flush(sync) {
  try {
    const prefs = ownPrefs();
    const next = JSON.stringify(prefs);
    if (next === sent) return;
    sent = next;
    if (sync) ipcRenderer.sendSync("prefs:save", prefs);
    else ipcRenderer.send("prefs:save", prefs);
  } catch {
    // Nothing to keep this time; the next change tries again.
  }
}
setInterval(() => flush(false), 3000);
window.addEventListener("pagehide", () => flush(true));

"use strict";

const { contextBridge, ipcRenderer } = require("electron");

/**
 * Minimal, whitelist-only bridge for the PoC renderer.
 * Sync endpoint lets the React bridge init HttpSseHost before first paint.
 */
contextBridge.exposeInMainWorld("reasonixPoc", {
  getEndpoint: () => ipcRenderer.invoke("poc:get-endpoint"),
  getEndpointSync: () => ipcRenderer.sendSync("poc:get-endpoint-sync"),
  getCapabilities: () => ipcRenderer.invoke("poc:get-capabilities"),
  pickWorkspace: () => ipcRenderer.invoke("poc:pick-workspace"),
  openLog: () => ipcRenderer.invoke("poc:open-log"),
  restartServe: () => ipcRenderer.invoke("poc:restart-serve"),
  onServeRestarted: (cb) => {
    const listener = (_e, payload) => cb(payload);
    ipcRenderer.on("poc:serve-restarted", listener);
    return () => ipcRenderer.removeListener("poc:serve-restarted", listener);
  },
});

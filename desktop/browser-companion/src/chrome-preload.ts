// Minimal trusted-chrome bridge. It exposes exactly two operations to the
// chrome renderer — sending commands to the main process and subscribing to
// state pushes — with no generic IPC passthrough. The chrome renderer is the
// only context that ever sees this bridge; remote pages have no preload at
// all.

import { contextBridge, ipcRenderer } from "electron";

export interface ChromeCommand {
  kind:
    | "activateTab"
    | "closeTab"
    | "newTab"
    | "navigate"
    | "back"
    | "forward"
    | "reload"
    | "takeover"
    | "focusAddress";
  tabId?: string;
  url?: string;
}

export interface ChromeState {
  tabs: Array<{ id: string; url: string; title: string; active: boolean }>;
  agentControlling: boolean;
}

const CHANNEL = "reasonix-chrome";


contextBridge.exposeInMainWorld("reasonixChrome", {
  command(command: ChromeCommand): void {
    ipcRenderer.send(CHANNEL, command);
  },
  onState(cb: (state: ChromeState) => void): () => void {
    const listener = (_event: unknown, state: ChromeState) => cb(state);
    ipcRenderer.on(CHANNEL, listener);
    return () => {
      ipcRenderer.removeListener(CHANNEL, listener);
    };
  },
  requestState(): void {
    ipcRenderer.send(CHANNEL, { kind: "requestState" });
  },
});

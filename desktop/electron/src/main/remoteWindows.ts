import { BrowserWindow, session } from "electron";
import { errorText, type Logger } from "./log.js";

const HOST_KEY = /^[A-Za-z0-9._-]{1,128}$/;
const REMOTE_PERMISSIONS = new Set(["clipboard-read", "clipboard-sanitized-write", "fullscreen"]);

interface RemoteEntry {
  win: BrowserWindow;
  origin: string;
}

export interface RemoteWindowInput {
  hostKey: string;
  url: string;
  title: string;
}

function remoteURL(value: string): URL {
  const url = new URL(value);
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error(`remote window URL must be http(s): ${value}`);
  return url;
}

function sameOrigin(candidate: string, origin: string): boolean {
  try {
    return new URL(candidate).origin === origin;
  } catch {
    return false;
  }
}

export class RemoteWindowHost {
  private readonly windows = new Map<string, RemoteEntry>();

  constructor(private readonly deps: { platform: NodeJS.Platform; icon?: string; log: Logger; onClosed?: (hostKey: string) => void }) {}

  open(input: RemoteWindowInput): { windowId: string } {
    if (!HOST_KEY.test(input.hostKey)) throw new Error("invalid remote window host key");
    const target = remoteURL(input.url);
    const existing = this.entry(input.hostKey);
    if (existing) {
      this.navigate(input);
      existing.win.show();
      return { windowId: String(existing.win.id) };
    }
    const partition = session.fromPartition(`persist:remote-${input.hostKey}`);
    partition.setPermissionRequestHandler((_contents, permission, callback) => callback(REMOTE_PERMISSIONS.has(permission)));
    const win = new BrowserWindow({
      width: 1180,
      height: 820,
      minWidth: 760,
      minHeight: 480,
      show: false,
      title: input.title || "Reasonix",
      backgroundColor: "#1a1a2e",
      autoHideMenuBar: true,
      icon: this.deps.icon,
      webPreferences: { session: partition, sandbox: true, contextIsolation: true, nodeIntegration: false, spellcheck: false },
    });
    const entry: RemoteEntry = { win, origin: target.origin };
    this.windows.set(input.hostKey, entry);
    if (this.deps.platform !== "darwin") win.setMenuBarVisibility(false);
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    const guard = (event: { preventDefault(): void }, next: string) => {
      if (sameOrigin(next, entry.origin)) return;
      event.preventDefault();
      this.deps.log.warn(`remote window ${input.hostKey}: blocked navigation to ${next}`);
    };
    win.webContents.on("will-navigate", guard);
    win.webContents.on("will-redirect", guard);
    win.on("closed", () => {
      if (this.windows.get(input.hostKey) !== entry) return;
      this.windows.delete(input.hostKey);
      this.deps.onClosed?.(input.hostKey);
    });
    win.once("ready-to-show", () => win.show());
    void win.loadURL(target.href).catch((error: unknown) => {
      this.deps.log.warn(`remote window ${input.hostKey}: load failed: ${errorText(error)}`);
    });
    return { windowId: String(win.id) };
  }

  navigate(input: RemoteWindowInput): void {
    const entry = this.entry(input.hostKey);
    if (!entry) throw new Error(`no remote window for ${input.hostKey}`);
    const target = remoteURL(input.url);
    entry.origin = target.origin;
    if (input.title) entry.win.setTitle(input.title);
    void entry.win.loadURL(target.href).catch((error: unknown) => {
      this.deps.log.warn(`remote window ${input.hostKey}: navigation failed: ${errorText(error)}`);
    });
  }

  focus(hostKey: string): void {
    const entry = this.entry(hostKey);
    if (!entry) throw new Error(`no remote window for ${hostKey}`);
    if (entry.win.isMinimized()) entry.win.restore();
    entry.win.show();
    entry.win.focus();
  }

  close(hostKey: string): void {
    this.entry(hostKey)?.win.close();
  }

  closeAll(): void {
    for (const entry of this.windows.values()) {
      if (!entry.win.isDestroyed()) entry.win.destroy();
    }
    this.windows.clear();
  }

  private entry(hostKey: string): RemoteEntry | null {
    const entry = this.windows.get(hostKey);
    if (!entry) return null;
    if (entry.win.isDestroyed()) {
      this.windows.delete(hostKey);
      return null;
    }
    return entry;
  }
}

import { BrowserWindow, nativeTheme, type WebContents, type WebFrameMain } from "electron";
import type { EventFrame, WindowBounds, WindowTheme } from "../shared/ipc.js";
import { IPC } from "../shared/ipc.js";
import { shellActionFromURL, type ShellAction } from "./failurePage.js";
import type { HelloWindow } from "./handshake.js";
import { errorText, type Logger } from "./log.js";
import { APP_ORIGIN } from "./protocol.js";
import { AppZoomStore } from "./zoomStore.js";

export const DEFAULT_GEOMETRY: HelloWindow = { width: 1280, height: 820, minWidth: 760, minHeight: 480, frameless: false, zoomFactor: 1 };

export interface MainWindowDeps {
  preloadPath: string;
  appURL: string;
  platform: NodeJS.Platform;
  icon?: string;
  log: Logger;
  onAppDomReady(rendererGeneration: number): void;
  onRendererLost?(reason: string): void;
  onCloseRequested(): Promise<boolean>;
  onCloseAllowed(): void;
  onShellAction(action: ShellAction): void;
  zoomStore: AppZoomStore;
}

type Content = "none" | "app" | "failure";

function hex(value: number): string {
  return Math.max(0, Math.min(255, Math.round(value))).toString(16).padStart(2, "0");
}

export class MainWindow {
  private win: BrowserWindow | null = null;
  private content: Content = "none";
  private rendererGeneration = 0;
  private closing = false;
  private closeAllowed = false;
  private readonly appOrigin: string;

  constructor(private readonly deps: MainWindowDeps) {
    let origin = APP_ORIGIN;
    try {
      origin = new URL(deps.appURL).origin;
    } catch {
      // A malformed dev URL fails at load time with a logged error.
    }
    this.appOrigin = origin === "null" ? APP_ORIGIN : origin;
  }

  get browserWindow(): BrowserWindow | null {
    return this.win && !this.win.isDestroyed() ? this.win : null;
  }

  create(geometry: HelloWindow): void {
    if (this.browserWindow) return;
    const { deps } = this;
    const win = new BrowserWindow({
      width: Math.round(geometry.width),
      height: Math.round(geometry.height),
      minWidth: Math.round(geometry.minWidth),
      minHeight: Math.round(geometry.minHeight),
      show: false,
      title: "Reasonix",
      backgroundColor: "#1a1a2e",
      titleBarStyle: deps.platform === "darwin" ? "hiddenInset" : "default",
      frame: !geometry.frameless,
      autoHideMenuBar: deps.platform !== "darwin",
      icon: deps.icon,
      webPreferences: {
        preload: deps.preloadPath,
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
        spellcheck: false,
        zoomFactor: this.deps.zoomStore.current.appZoomFactor,
      },
    });
    this.win = win;
    this.content = "none";
    if (deps.platform !== "darwin") win.setMenuBarVisibility(false);
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    win.webContents.on("will-attach-webview", (event) => event.preventDefault());
    win.webContents.on("will-navigate", (event, url) => {
      const action = shellActionFromURL(url);
      if (action) {
        event.preventDefault();
        deps.onShellAction(action);
        return;
      }
      if (url.startsWith(this.appOrigin + "/")) return;
      event.preventDefault();
      deps.log.warn(`blocked main window navigation to ${url}`);
    });
    win.webContents.on("dom-ready", () => {
      if (this.content !== "app") return;
      this.rendererGeneration += 1;
      deps.onAppDomReady(this.rendererGeneration);
    });
    win.webContents.on("render-process-gone", (_event, details) => {
      deps.log.error(`renderer process gone: ${details.reason} (exit code ${details.exitCode})`);
      deps.onRendererLost?.(`app renderer ${details.reason}`);
      if (this.content === "app" && this.browserWindow) win.webContents.reload();
    });
    win.on("close", (event) => {
      if (this.closeAllowed) return;
      event.preventDefault();
      void this.requestClose();
    });
    win.on("closed", () => {
      this.win = null;
    });
  }

  async getAppZoom(): Promise<number> { return (await this.deps.zoomStore.load()).appZoomFactor; }
  async setAppZoom(factor: number): Promise<number> {
    const state = await this.deps.zoomStore.set(factor);
    this.browserWindow?.webContents.setZoomFactor(state.appZoomFactor);
    return state.appZoomFactor;
  }
  async resetAppZoom(): Promise<number> { return this.setAppZoom(1); }
  async stepAppZoom(direction: 1 | -1): Promise<number> {
    const current = await this.getAppZoom();
    return this.setAppZoom(current + direction * 0.05);
  }

  async loadApp(): Promise<void> {
    const win = this.browserWindow;
    if (!win) return;
    this.content = "app";
    try {
      await win.loadURL(this.deps.appURL);
    } catch (error) {
      this.deps.log.error(`failed to load ${this.deps.appURL}: ${errorText(error)}`);
    }
  }

  reattachApp(): boolean {
    if (!this.browserWindow || this.content !== "app") return false;
    this.rendererGeneration += 1;
    this.deps.onAppDomReady(this.rendererGeneration);
    return true;
  }

  async showFailure(html: string): Promise<void> {
    const win = this.browserWindow;
    if (!win) return;
    this.deps.onRendererLost?.("app failure page");
    this.content = "failure";
    try {
      await win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(html)}`);
    } catch (error) {
      this.deps.log.error(`failed to load the failure page: ${errorText(error)}`);
    }
    win.show();
  }

  allowClose(): void {
    this.closeAllowed = true;
  }

  isTrustedSender(sender: WebContents, frame: WebFrameMain | null | undefined): boolean {
    const win = this.browserWindow;
    return Boolean(win) && sender === win?.webContents && frame != null && frame === win.webContents.mainFrame;
  }

  send(channel: string, payload: unknown): void {
    const win = this.browserWindow;
    if (!win || win.webContents.isDestroyed()) return;
    win.webContents.send(channel, payload);
  }

  sendShellEvent(name: string, generation: string, args: unknown[] = []): void {
    const frame: EventFrame = { seq: 0, generation, name, args };
    this.send(IPC.event, frame);
  }

  show(reason: string): void {
    const win = this.browserWindow;
    if (!win) return;
    void reason;
    if (win.isMinimized()) win.restore();
    win.show();
  }

  focusForSecondInstance(): void {
    this.show("second-instance");
    this.browserWindow?.focus();
  }

  hide(): void {
    this.browserWindow?.hide();
  }

  maximise(): void {
    this.browserWindow?.maximize();
  }

  unmaximise(): void {
    this.browserWindow?.unmaximize();
  }

  minimise(): void {
    this.browserWindow?.minimize();
  }

  unminimise(): void {
    this.browserWindow?.restore();
  }

  toggleMaximise(): void {
    const win = this.browserWindow;
    if (!win) return;
    if (win.isMaximized()) win.unmaximize();
    else win.maximize();
  }

  center(): void {
    this.browserWindow?.center();
  }

  isMaximised(): boolean {
    return this.browserWindow?.isMaximized() ?? false;
  }

  isMinimised(): boolean {
    return this.browserWindow?.isMinimized() ?? false;
  }

  setPosition(x: number, y: number): void {
    this.browserWindow?.setPosition(Math.round(x), Math.round(y));
  }

  setTitle(title: string): void {
    this.browserWindow?.setTitle(title);
  }

  toggleDevTools(): void {
    this.browserWindow?.webContents.toggleDevTools();
  }

  close(): void {
    this.browserWindow?.close();
  }

  contentSize(): { width: number; height: number } | null {
    const win = this.browserWindow;
    if (!win) return null;
    const [width, height] = win.getContentSize();
    return { width, height };
  }

  bounds(): WindowBounds {
    const win = this.browserWindow;
    if (!win) return { x: 0, y: 0, width: 0, height: 0, maximised: false };
    const [x, y] = win.getPosition();
    const [width, height] = win.getSize();
    return { x, y, width, height, maximised: win.isMaximized() };
  }

  setTheme(theme: WindowTheme): void {
    nativeTheme.themeSource = theme;
  }

  setBackgroundColour(r: number, g: number, b: number, a: number): void {
    void a;
    this.browserWindow?.setBackgroundColor(`#${hex(r)}${hex(g)}${hex(b)}`);
  }

  private async requestClose(): Promise<void> {
    if (this.closing) return;
    this.closing = true;
    try {
      const prevent = await this.deps.onCloseRequested();
      if (prevent) this.hide();
      else this.deps.onCloseAllowed();
    } catch (error) {
      this.deps.log.warn(`beforeClose(window) failed, closing anyway: ${errorText(error)}`);
      this.deps.onCloseAllowed();
    } finally {
      this.closing = false;
    }
  }
}

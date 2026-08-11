// Main-process side of the trusted browser chrome: creates the chrome
// WebContentsView (own non-persistent session, minimal preload bridge), renders
// tab state into it, and translates its commands into TabManager operations.

import { BaseWindow, WebContentsView, session } from "electron";
import * as path from "node:path";
import { CHROME_HEIGHT, type TabManager, type TabRecord } from "./tabs";

export interface ChromeCommands {
  activateTab(tabId: string): void;
  closeTab(tabId: string): void;
  newTab(): void;
  navigate(url: string): void;
  back(): void;
  forward(): void;
  reload(): void;
  takeover(): void;
  /** Returns the current tab state for rendering. */
  snapshot(): { tabs: TabRecord[]; agentControlling: boolean };
}

const STATE_CHANNEL = "reasonix-chrome";

export class Chrome {
  view: WebContentsView | null = null;

  constructor(
    private window: BaseWindow,
    private tabs: TabManager,
    private commands: ChromeCommands,
  ) {}

  /** Creates and attaches the chrome view above the page area. */
  mount(): void {
    const view = new WebContentsView({
      webPreferences: {
        nodeIntegration: false,
        contextIsolation: true,
        sandbox: true,
        preload: path.join(__dirname, "chrome-preload.js"),
        // The chrome gets its own ephemeral session: no cookies, no storage.
        session: session.fromPartition(`reasonix-chrome-${process.pid}`),
      },
    });
    this.view = view;
    this.tabs.attachChrome(view);

    view.webContents.on("ipc-message", (_event, channel, ...args) => {
      if (channel !== STATE_CHANNEL) return;
      const cmd = args[0] as { kind?: string; tabId?: string; url?: string };
      switch (cmd?.kind) {
        case "activateTab":
          if (cmd.tabId) this.commands.activateTab(cmd.tabId);
          break;
        case "closeTab":
          if (cmd.tabId) this.commands.closeTab(cmd.tabId);
          break;
        case "newTab":
          this.commands.newTab();
          break;
        case "navigate":
          if (cmd.url) this.commands.navigate(cmd.url);
          break;
        case "back":
          this.commands.back();
          break;
        case "forward":
          this.commands.forward();
          break;
        case "reload":
          this.commands.reload();
          break;
        case "takeover":
          this.commands.takeover();
          break;
        case "requestState":
          this.pushState();
          break;
      }
    });
    view.webContents.on("did-finish-load", () => this.pushState());
    this.layout();
    // The chrome view is the first child of the window's contentView; remote
    // page views are stacked above it by the TabManager layout.
    this.window.contentView.addChildView(view);
    void view.webContents.loadFile(path.join(__dirname, "chrome.html"));
  }

  layout(): void {
    if (!this.view) return;
    const bounds = this.window.getContentBounds();
    this.view.setBounds({ x: 0, y: 0, width: bounds.width, height: CHROME_HEIGHT });
    this.view.setVisible(true);
  }

  /** Pushes the current tab state into the chrome renderer. */
  pushState(): void {
    if (!this.view || this.view.webContents.isDestroyed()) return;
    const { tabs, agentControlling } = this.commands.snapshot();
    this.view.webContents.send(STATE_CHANNEL, {
      tabs: tabs.map((t) => ({ id: t.id, url: t.url, title: t.title, active: t.active })),
      agentControlling,
    });
  }

  destroy(): void {
    if (!this.view) return;
    if (this.window.contentView.children.includes(this.view)) {
      this.window.contentView.removeChildView(this.view);
    }
    if (!this.view.webContents.isDestroyed()) {
      this.view.webContents.stop();
      this.view.webContents.close({ waitForBeforeUnload: false });
    }
    this.view = null;
  }
}

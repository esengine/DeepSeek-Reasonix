// CompanionApp is the companion's application core: the method dispatcher
// (the same one the host drives over stdin/stdout), the window/tab/chrome
// wiring, and the chrome command callbacks. main.ts only connects stdin/stdout
// and the Electron lifecycle to this class, and the integration test drives
// the exact same handle() path a real host would — so production behavior and
// tests cannot drift.
//
// The visible owner is a single source of truth: TabManager.visibleOwner.
// main.ts never keeps a second copy of it.

import { BaseWindow } from "electron";
import {
  assertHttpUrl,
  ProtocolError,
  PROTOCOL_VERSION,
  responseError,
  responseOk,
  type BrowserRequest,
  type BrowserResponse,
} from "./protocol";
import { TabManager, type TabRecord } from "./tabs";
import { Chrome } from "./chrome";
import { AgentController } from "./agent";
import {
  BROWSER_EVENTS,
  BROWSER_METHODS,
  type BrowserErrorCode,
} from "./generated/browserProtocol.generated";

export interface CompanionAppDeps {
  componentVersion: string;
  electronVersion: string;
  chromiumVersion: string;
  pid: number;
  /** Fires an unsolicited event frame to the host. */
  emitEvent(name: string, ownerId: string, data: unknown): void;
  /** Exits the process (after window.close). */
  exit(code: number): void;
}

export class CompanionApp {
  window: BaseWindow | null = null;
  tabs: TabManager | null = null;
  chrome: Chrome | null = null;
  agent: AgentController | null = null;
  private readyToOpen = false;
  private helloSeen = false;

  constructor(private deps: CompanionAppDeps) {}

  /** The chat whose pages the chrome currently shows. */
  get activeOwnerId(): string {
    return this.tabs?.visibleOwner ?? "";
  }

  /** Called when Electron is ready; opens the window if hello arrived first. */
  markReady(): void {
    this.readyToOpen = true;
    if (this.helloSeen) {
      this.openWindow();
    }
  }

  /** The state the chrome renders: the visible owner's tabs + lease badge. */
  chromeSnapshot(): { tabs: TabRecord[]; agentControlling: boolean } {
    const ownerId = this.activeOwnerId || "default";
    const list = this.tabs?.list(ownerId) ?? [];
    // The badge reflects the agent LEASE, not whether a tab happens to be
    // active: normal human browsing must never show "Agent controlling".
    return { tabs: list, agentControlling: this.tabs?.agentControllingFor(ownerId) ?? false };
  }

  // ---- chrome command callbacks (the trusted chrome drives these) ----

  chromeActivateTab(tabId: string): void {
    // Commands are silent when no owner is visible: a stale click racing an
    // owner.remove must not throw out of the ipc handler.
    if (!this.activeOwnerId) return;
    this.tabs?.activate(this.activeOwnerId, tabId);
    this.chrome?.pushState();
  }

  chromeCloseTab(tabId: string): void {
    if (!this.activeOwnerId) return;
    this.tabs?.closeTab(this.activeOwnerId, tabId);
    this.chrome?.pushState();
  }

  chromeNewTab(): void {
    // Chrome-initiated new tab: blank page, http(s) enforced on navigation.
    const ownerId = this.activeOwnerId || "default";
    this.tabs?.createChromeTab(ownerId);
    this.tabs?.setActiveOwner(ownerId);
    this.chrome?.pushState();
  }

  chromeNavigate(url: string): void {
    if (!this.activeOwnerId) return;
    const tabId = this.activeTabIdOf(this.activeOwnerId);
    if (!tabId) return;
    try {
      this.tabs?.navigate(this.activeOwnerId, tabId, assertHttpUrl(url));
    } catch {
      // Ignore invalid chrome input; the address bar keeps its text.
    }
    this.chrome?.pushState();
  }

  chromeBack(): void {
    this.activeWebContents()?.goBack();
    this.chrome?.pushState();
  }

  chromeForward(): void {
    this.activeWebContents()?.goForward();
    this.chrome?.pushState();
  }

  chromeReload(): void {
    this.activeWebContents()?.reload();
    this.chrome?.pushState();
  }

  chromeTakeover(): void {
    if (!this.activeOwnerId) return;
    const tabId = this.activeTabIdOf(this.activeOwnerId);
    if (tabId) this.tabs?.revokeLease(this.activeOwnerId, tabId, "user", "user takeover");
    this.chrome?.pushState();
  }

  // ---- window lifecycle ----

  openWindow(): void {
    if (this.window) {
      this.window.show();
      this.window.focus();
      return;
    }
    const win = new BaseWindow({
      width: 1200,
      height: 800,
      minWidth: 640,
      minHeight: 400,
      title: "Reasonix Browser",
    });
    this.window = win;
    this.tabs = new TabManager(win, (name, ownerId, data) => {
      this.deps.emitEvent(name, ownerId, data);
      // Any tab state change (navigation, titles, lease revokes) must be
      // reflected in the chrome UI immediately.
      this.chrome?.pushState();
    });
    this.agent = new AgentController(this.tabs);
    this.chrome = new Chrome(win, this.tabs, {
      activateTab: (tabId) => this.chromeActivateTab(tabId),
      closeTab: (tabId) => this.chromeCloseTab(tabId),
      newTab: () => this.chromeNewTab(),
      navigate: (url) => this.chromeNavigate(url),
      back: () => this.chromeBack(),
      forward: () => this.chromeForward(),
      reload: () => this.chromeReload(),
      takeover: () => this.chromeTakeover(),
      snapshot: () => this.chromeSnapshot(),
    });
    this.chrome.mount();
    win.on("resize", () => {
      this.chrome?.layout();
      this.tabs?.layout();
    });
    win.on("closed", () => {
      // The host persists tab state; this process just destroys its views.
      this.tabs?.destroyAll();
      this.chrome?.destroy();
      this.window = null;
      this.tabs = null;
      this.chrome = null;
      this.agent = null;
      this.deps.exit(0);
    });
  }

  private activeWebContents(): Electron.WebContents | null {
    if (!this.tabs || !this.activeOwnerId) return null;
    const tabId = this.activeTabIdOf(this.activeOwnerId);
    if (!tabId) return null;
    return this.tabs.webContentsFor(tabId)?.webContents ?? null;
  }

  private activeTabIdOf(ownerId: string): string {
    const list = this.tabs?.list(ownerId) ?? [];
    const active = list.find((t) => t.active);
    return active?.id ?? list[list.length - 1]?.id ?? "";
  }

  // ---- method dispatch (the exact path the host drives) ----

  async handle(req: BrowserRequest): Promise<BrowserResponse> {
    // The window opens on hello; any tab operation before that is refused
    // with the protocol's not_ready code (this gate lives in the dispatch
    // path so hosts and tests exercise the same behavior).
    if ((req.method === "tab.open" || req.method === "tab.navigate") && !this.tabs) {
      return responseError(req.requestId, "not_ready", "window not open");
    }
    const params = req.params as Record<string, unknown>;
    switch (req.method) {
      case "hello": {
        this.helloSeen = true;
        if (this.readyToOpen) this.openWindow();
        return responseOk(req.requestId, {
          protocolVersion: PROTOCOL_VERSION,
          componentVersion: this.deps.componentVersion,
          electronVersion: this.deps.electronVersion,
          chromiumVersion: this.deps.chromiumVersion,
          pid: this.deps.pid,
          capabilities: {
            maxProtocolVersion: PROTOCOL_VERSION,
            methods: [...BROWSER_METHODS],
            events: [...BROWSER_EVENTS],
          },
        });
      }
      case "request.cancel":
        this.agent?.cancel(requireParam(params, "requestId"));
        return responseOk(req.requestId, {});
      case "window.open":
        this.openWindow();
        if (req.ownerId && this.tabs) {
          this.tabs.setActiveOwner(req.ownerId);
        }
        this.chrome?.pushState();
        return responseOk(req.requestId, {});
      case "window.focus":
        this.openWindow();
        if (req.ownerId && this.tabs) {
          this.tabs.setActiveOwner(req.ownerId);
        }
        this.window?.focus();
        this.chrome?.pushState();
        return responseOk(req.requestId, {});
      case "window.close":
        this.tabs?.destroyAll();
        this.chrome?.destroy();
        if (this.window) {
          this.window.destroy();
          this.window = null;
        }
        // Reply before exiting so the host's graceful shutdown sees the ack.
        setImmediate(() => this.deps.exit(0));
        return responseOk(req.requestId, {});
      case "owner.activate": {
        const ownerId = requireOwner(params);
        this.tabs?.setActiveOwner(ownerId);
        this.chrome?.pushState();
        return responseOk(req.requestId, {});
      }
      case "owner.remove": {
        const ownerId = requireOwner(params);
        this.tabs?.removeOwner(ownerId);
        if (this.activeOwnerId === ownerId) {
          // Nothing is visible any more; keep the owner empty rather than
          // showing a deleted chat's chrome.
          this.tabs?.setActiveOwner("");
        }
        this.chrome?.pushState();
        return responseOk(req.requestId, {});
      }
      case "tab.open": {
        const ownerId = requireOwner(params);
        const url = assertHttpUrl(paramString(params, "url"));
        const disposition = paramString(params, "disposition") || "foreground";
        const fromAgent = params.fromAgent === true;
        const record = this.tabs!.createTab(ownerId, url, disposition, fromAgent);
        // The ordinary chat-link path sends only tab.open. A foreground tab
        // (or the very first tab) binds the chrome to that chat; background
        // tabs preserve the current visible owner.
        if (disposition === "foreground" || !this.tabs!.visibleOwner) {
          this.tabs!.setActiveOwner(ownerId);
        }
        this.chrome?.pushState();
        return responseOk(req.requestId, toTabInfo(record));
      }
      case "tab.list": {
        const ownerId = requireOwner(params);
        const records = this.tabs?.list(ownerId) ?? [];
        this.chrome?.pushState();
        return responseOk(req.requestId, { tabs: records.map(toTabInfo) });
      }
      case "tab.activate": {
        const { ownerId, tabId } = requireTab(params);
        this.tabs?.activate(ownerId, tabId);
        this.chrome?.pushState();
        return responseOk(req.requestId, {});
      }
      case "tab.close": {
        const { ownerId, tabId } = requireTab(params);
        this.tabs?.closeTab(ownerId, tabId);
        this.chrome?.pushState();
        return responseOk(req.requestId, {});
      }
      case "tab.navigate": {
        const { ownerId, tabId } = requireTab(params);
        const url = assertHttpUrl(paramString(params, "url"));
        const record = this.tabs?.navigate(ownerId, tabId, url);
        this.chrome?.pushState();
        return responseOk(req.requestId, record ? toTabInfo(record) : {});
      }
      case "tab.snapshot":
        return responseOk(req.requestId, await this.agent!.snapshot(params));
      case "tab.screenshot":
        return responseOk(req.requestId, await this.agent!.screenshot(params));
      case "tab.wait":
        return responseOk(req.requestId, await this.agent!.wait(req.requestId, params));
      case "tab.act":
        return responseOk(req.requestId, await this.agent!.act(params));
      case "data.clear": {
        const scopes = Array.isArray(params.scopes) ? (params.scopes as string[]) : [];
        const cleared: string[] = [];
        const ses = this.tabs?.sharedSession;
        if (ses) {
          for (const scope of scopes) {
            switch (scope) {
              case "cookies":
                await ses.clearStorageData({ storages: ["cookies", "localstorage", "indexdb"] });
                cleared.push("cookies");
                break;
              case "cache":
                await ses.clearCache();
                cleared.push("cache");
                break;
              case "history":
                await ses.clearCache();
                cleared.push("history");
                break;
              case "all":
                await ses.clearStorageData();
                await ses.clearCache();
                cleared.push("all");
                break;
              case "downloads":
                // Download records follow the save flow in a later phase.
                cleared.push("downloads");
                break;
            }
          }
        }
        return responseOk(req.requestId, { cleared });
      }
      case "permissions.list":
        return responseOk(req.requestId, { grants: [] });
      case "permissions.revoke":
        return responseOk(req.requestId, {});
      default:
        return responseError(req.requestId, "unknown_method", `unknown method ${req.method}`);
    }
  }
}

function toTabInfo(r: TabRecord): {
  tabId: string;
  url: string;
  title: string;
  active: boolean;
  generation: number;
} {
  return { tabId: r.id, url: r.url, title: r.title, active: r.active, generation: r.generation };
}

function paramString(params: Record<string, unknown>, key: string): string {
  const v = params[key];
  return typeof v === "string" ? v : "";
}

function requireParam(params: Record<string, unknown>, key: string): string {
  const v = paramString(params, key);
  if (v.length === 0) {
    throw new ProtocolError("invalid_params", `${key} is required`);
  }
  return v;
}

function requireOwner(params: Record<string, unknown>): string {
  return requireParam(params, "ownerId");
}

function requireTab(params: Record<string, unknown>): { ownerId: string; tabId: string } {
  return { ownerId: requireOwner(params), tabId: requireParam(params, "tabId") };
}

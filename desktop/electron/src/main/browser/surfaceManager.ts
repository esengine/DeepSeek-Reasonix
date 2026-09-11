import type { Rectangle } from "electron";
import type { BrowserLayoutRect, BrowserNavigateTarget, BrowserTabMode, BrowserTabView, BrowserTakeoverKind } from "../../shared/ipc.js";
import type { Logger } from "../log.js";
import type { GuestView, GuestViewEvents, GuestViewFactory } from "./guestView.js";

export const SHARED_PARTITION = "persist:browser";
export const USER_TASK_ID = "user";
export const OPEN_WAIT_MS = 15_000;
// Input the shell dispatches for the agent reaches the page as trusted, so
// the guest preload reports it like a user; reports inside this window after
// a dispatch are the agent's own echo, not a take-over.
export const AGENT_INPUT_GRACE_MS = 750;
export const MAX_CRASH_RELOADS = 3;
export const MIN_ZOOM = 0.25;
export const MAX_ZOOM = 5;

export interface BrowserTab {
  id: string;
  taskId: string;
  view: GuestView;
  partition: string;
  temporary: boolean;
  epoch: number;
  mode: BrowserTabMode;
  loading: boolean;
  error: { code: number; description: string } | null;
  zoom: number;
  createdAt: number;
  lastURL: string;
  crashes: number;
  agentInputUntil: number;
}

export interface OpenOptions {
  taskId: string;
  temporary: boolean;
}

export interface SurfaceManagerDeps {
  views: GuestViewFactory;
  contentSize(): { width: number; height: number } | null;
  onTakeover(tab: BrowserTab, reason: string): void;
  onCrash(tab: BrowserTab, reason: string): void;
  log: Logger;
  now?(): number;
  openWaitMs?: number;
}

export function normaliseBrowserURL(input: string): string {
  const raw = input.trim();
  if (raw === "") throw new Error("empty URL");
  const candidate = /^[a-z][a-z0-9+.-]*:/i.test(raw) ? raw : `https://${raw}`;
  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    throw new Error(`invalid URL: ${raw.slice(0, 120)}`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error(`only http(s) URLs can be opened, not ${url.protocol}`);
  return url.href;
}

export function validateLayout(rect: BrowserLayoutRect, content: { width: number; height: number } | null): Rectangle {
  const values = [rect.x, rect.y, rect.width, rect.height];
  if (values.some((value) => typeof value !== "number" || !Number.isFinite(value))) throw new Error("layout rect must be finite numbers");
  const x = Math.max(0, Math.round(rect.x));
  const y = Math.max(0, Math.round(rect.y));
  let width = Math.max(0, Math.round(rect.width));
  let height = Math.max(0, Math.round(rect.height));
  if (content) {
    width = Math.min(width, Math.max(0, content.width - x));
    height = Math.min(height, Math.max(0, content.height - y));
  }
  return { x, y, width, height };
}

export class BrowserSurfaceManager {
  private readonly tabs = new Map<string, BrowserTab>();
  private readonly listeners = new Set<(tabs: BrowserTabView[]) => void>();
  private layout: Rectangle | null = null;
  private overlay = false;
  private activeId: string | null = null;
  private counter = 0;
  private readonly now: () => number;

  constructor(private readonly deps: SurfaceManagerDeps) {
    this.now = deps.now ?? (() => Date.now());
  }

  get activeTabId(): string | null {
    return this.activeId;
  }

  get(tabId: string): BrowserTab | undefined {
    return this.tabs.get(tabId);
  }

  require(tabId: string): BrowserTab {
    const tab = this.tabs.get(tabId);
    if (!tab) throw new Error(`unknown browser tab ${tabId || "(empty)"}`);
    return tab;
  }

  all(): BrowserTab[] {
    return [...this.tabs.values()];
  }

  tabsForTask(taskId: string): BrowserTab[] {
    return this.all().filter((tab) => tab.taskId === taskId);
  }

  list(): BrowserTabView[] {
    return this.all().map((tab) => this.view(tab));
  }

  view(tab: BrowserTab): BrowserTabView {
    const page = tab.view.page;
    const gone = page.isDestroyed();
    return {
      id: tab.id,
      taskId: tab.taskId,
      url: gone ? tab.lastURL : page.getURL(),
      title: gone ? "" : page.getTitle(),
      loading: tab.loading,
      canGoBack: !gone && page.navigationHistory.canGoBack(),
      canGoForward: !gone && page.navigationHistory.canGoForward(),
      temporary: tab.temporary,
      mode: tab.mode,
      epoch: tab.epoch,
      zoom: tab.zoom,
      active: tab.id === this.activeId,
      error: tab.error,
    };
  }

  subscribe(listener: (tabs: BrowserTabView[]) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  async open(url: string, options: OpenOptions): Promise<BrowserTab> {
    const href = normaliseBrowserURL(url);
    const id = this.nextId();
    const partition = options.temporary ? `temp:${id}` : SHARED_PARTITION;
    const view = this.deps.views.create(partition);
    const tab = this.register(id, view, options.taskId, partition, options.temporary);
    if (this.layout) view.setBounds(this.layout);
    // The application renderer owns selection. Agent opens must not replace
    // another task's visible page while its address bar still names that task.
    this.broadcast();
    const load = view.page.loadURL(href).catch((error: unknown) => {
      this.deps.log.warn(`browser tab ${tab.id} load failed: ${String(error)}`);
    });
    await Promise.race([load, new Promise<void>((resolve) => setTimeout(resolve, this.deps.openWaitMs ?? OPEN_WAIT_MS).unref?.())]);
    return tab;
  }

  close(tabId: string): void {
    const tab = this.tabs.get(tabId);
    if (!tab) return;
    this.forget(tab);
    tab.view.destroy();
    this.broadcast();
  }

  activate(tabId: string | null): void {
    if (tabId !== null) this.require(tabId);
    this.activeId = tabId;
    this.applyVisibility();
    this.broadcast();
  }

  setLayout(rect: BrowserLayoutRect | null): void {
    this.layout = rect === null ? null : validateLayout(rect, this.deps.contentSize());
    this.applyVisibility();
  }

  setOverlay(active: boolean): void {
    if (this.overlay === active) return;
    this.overlay = active;
    this.applyVisibility();
  }

  async navigate(tabId: string, target: BrowserNavigateTarget): Promise<BrowserTab> {
    const tab = this.require(tabId);
    const page = tab.view.page;
    switch (target.action) {
      case "back":
        if (page.navigationHistory.canGoBack()) page.navigationHistory.goBack();
        return tab;
      case "forward":
        if (page.navigationHistory.canGoForward()) page.navigationHistory.goForward();
        return tab;
      case "reload":
        page.reload();
        return tab;
      case "stop":
        page.stop();
        return tab;
      default:
        break;
    }
    if (typeof target.url !== "string") throw new Error("navigate needs a url or an action");
    await page.loadURL(normaliseBrowserURL(target.url)).catch((error: unknown) => {
      this.deps.log.warn(`browser tab ${tab.id} navigation failed: ${String(error)}`);
    });
    return tab;
  }

  setZoom(tabId: string, factor: number): void {
    const tab = this.require(tabId);
    if (!Number.isFinite(factor)) throw new Error("zoom factor must be a finite number");
    tab.zoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, factor));
    tab.view.page.setZoomFactor(tab.zoom);
    this.broadcast();
  }

  toggleDevTools(tabId: string): void {
    const page = this.require(tabId).view.page;
    if (page.isDevToolsOpened()) page.closeDevTools();
    else page.openDevTools({ mode: "detach" });
  }

  resume(tabId: string): void {
    const tab = this.require(tabId);
    if (tab.mode === "agent") return;
    tab.mode = "agent";
    tab.epoch += 1;
    this.broadcast();
  }

  takeover(tabId: string, reason: string): void {
    const tab = this.tabs.get(tabId);
    if (!tab) return;
    tab.epoch += 1;
    tab.mode = "human";
    this.broadcast();
    this.deps.onTakeover(tab, reason);
  }

  // Called with the guest preload's report; the sender id identifies the tab.
  takeoverFromSender(webContentsId: number, kind: BrowserTakeoverKind): boolean {
    const tab = this.all().find((entry) => entry.view.page.id === webContentsId);
    if (!tab) return false;
    if (this.now() < tab.agentInputUntil) return false;
    this.takeover(tab.id, `user ${kind}`);
    return true;
  }

  markAgentInput(tab: BrowserTab): void {
    tab.agentInputUntil = this.now() + AGENT_INPUT_GRACE_MS;
  }

  pauseForRendererLoss(reason: string): void {
    this.layout = null;
    this.activeId = null;
    this.applyVisibility();
    for (const tab of this.all()) this.takeover(tab.id, reason);
  }

  destroyAll(): void {
    const tabs = this.all();
    this.tabs.clear();
    this.activeId = null;
    for (const tab of tabs) {
      tab.mode = "human";
      tab.epoch += 1;
      tab.view.destroy();
    }
    this.broadcast();
  }

  private nextId(): string {
    this.counter += 1;
    return `tab-${this.counter}`;
  }

  private register(id: string, view: GuestView, taskId: string, partition: string, temporary: boolean): BrowserTab {
    const tab: BrowserTab = {
      id,
      taskId,
      view,
      partition,
      temporary,
      epoch: 0,
      mode: "agent",
      loading: false,
      error: null,
      zoom: 1,
      createdAt: this.now(),
      lastURL: "",
      crashes: 0,
      agentInputUntil: 0,
    };
    this.tabs.set(id, tab);
    view.bind(this.events(tab));
    return tab;
  }

  private events(tab: BrowserTab): GuestViewEvents {
    return {
      onStartLoading: () => {
        tab.loading = true;
        this.broadcast();
      },
      onStopLoading: () => {
        tab.loading = false;
        this.broadcast();
      },
      onNavigate: (url, inPage) => {
        tab.epoch += 1;
        tab.lastURL = url;
        if (!inPage) tab.error = null;
        this.broadcast();
      },
      onTitle: () => this.broadcast(),
      onFailLoad: (code, description) => {
        tab.error = { code, description };
        this.broadcast();
      },
      onRenderProcessGone: (reason) => this.recover(tab, reason),
      onDestroyed: () => {
        if (!this.tabs.has(tab.id)) return;
        this.forget(tab);
        this.broadcast();
      },
      onPopup: () => {
        if (!this.tabs.has(tab.id)) return null;
        return (view) => {
          this.register(this.nextId(), view, tab.taskId, tab.partition, tab.temporary);
          this.broadcast();
        };
      },
    };
  }

  // A crashed website view never replays anything: it reloads the last
  // committed URL in human mode and the agent must look again.
  private recover(tab: BrowserTab, reason: string): void {
    tab.mode = "human";
    tab.epoch += 1;
    tab.loading = false;
    tab.crashes += 1;
    tab.error = { code: 0, description: `renderer ${reason}` };
    this.broadcast();
    this.deps.onCrash(tab, reason);
    if (tab.crashes > MAX_CRASH_RELOADS || tab.lastURL === "" || tab.view.page.isDestroyed()) return;
    tab.view.page.loadURL(tab.lastURL).catch((error: unknown) => {
      this.deps.log.warn(`browser tab ${tab.id} recovery failed: ${String(error)}`);
    });
  }

  private forget(tab: BrowserTab): void {
    this.tabs.delete(tab.id);
    tab.mode = "human";
    tab.epoch += 1;
    if (this.activeId === tab.id) {
      this.activeId = null;
      this.applyVisibility();
    }
  }

  private applyVisibility(): void {
    for (const tab of this.tabs.values()) {
      const visible = !this.overlay && this.layout !== null && tab.id === this.activeId && this.layout.width > 0 && this.layout.height > 0;
      if (this.layout) tab.view.setBounds(this.layout);
      tab.view.setVisible(visible);
    }
  }

  private broadcast(): void {
    if (this.listeners.size === 0) return;
    const tabs = this.list();
    for (const listener of [...this.listeners]) {
      try {
        listener(tabs);
      } catch (error) {
        this.deps.log.warn(`browser tab listener failed: ${String(error)}`);
      }
    }
  }
}

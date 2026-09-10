// Renderer view of the shell's browser surface manager. Mirrors the
// `window.reasonixDesktop.browser` preload API (docs/DESKTOP_BROWSER.md).
export type BrowserTabMode = "agent" | "human";

export interface BrowserTabView {
  id: string;
  taskId: string;
  url: string;
  title: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
  temporary: boolean;
  mode: BrowserTabMode;
  epoch: number;
  zoom: number;
  error: { code: number; description: string } | null;
}

export type BrowserDownloadState = "progressing" | "completed" | "cancelled" | "interrupted";

export interface BrowserDownloadView {
  id: string;
  tabId: string;
  url: string;
  filename: string;
  path: string;
  state: BrowserDownloadState;
  received: number;
  total: number;
}

export interface BrowserLayoutRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface BrowserNavigationTarget {
  url?: string;
  action?: "back" | "forward" | "reload" | "stop";
}

export interface DesktopBrowserHost {
  list(): Promise<BrowserTabView[]>;
  open(url: string, opts?: { temporary?: boolean; taskId?: string }): Promise<BrowserTabView>;
  close(tabId: string): Promise<void>;
  activate(tabId: string | null): Promise<void>;
  navigate(tabId: string, target: BrowserNavigationTarget): Promise<void>;
  setZoom(tabId: string, factor: number): Promise<void>;
  toggleDevTools(tabId: string): Promise<void>;
  resume(tabId: string): Promise<void>;
  takeover(tabId: string): Promise<void>;
  setLayout(rect: BrowserLayoutRect | null): void;
  setOverlay(active: boolean): void;
  onTabs(cb: (tabs: BrowserTabView[]) => void): () => void;
  onDownload(cb: (download: BrowserDownloadView) => void): () => void;
}

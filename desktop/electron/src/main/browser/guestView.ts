import type { KeyboardInputEvent, MouseInputEvent, MouseWheelInputEvent, Rectangle, WebPreferences } from "electron";

// The slice of Electron the browser modules touch. Production binds the real
// WebContentsView (see electronGuestViews.ts); tests inject fakes.

export interface GuestFrame {
  readonly frameTreeNodeId: number;
  readonly url: string;
  readonly detached: boolean;
  readonly parent: GuestFrame | null;
  readonly framesInSubtree: GuestFrame[];
  executeJavaScript(code: string, userGesture?: boolean): Promise<unknown>;
}

export interface GuestDebugger {
  isAttached(): boolean;
  attach(protocolVersion?: string): void;
  detach(): void;
  sendCommand(method: string, params?: unknown): Promise<unknown>;
  on(event: "message", listener: (event: unknown, method: string, params: unknown) => void): unknown;
  removeListener(event: "message", listener: (event: unknown, method: string, params: unknown) => void): unknown;
}

export interface GuestImage {
  toPNG(): Buffer;
  getSize(): { width: number; height: number };
}

export interface GuestPage {
  readonly id: number;
  readonly mainFrame: GuestFrame;
  readonly debugger: GuestDebugger;
  readonly navigationHistory: { canGoBack(): boolean; canGoForward(): boolean; goBack(): void; goForward(): void };
  isDestroyed(): boolean;
  loadURL(url: string): Promise<void>;
  getURL(): string;
  getTitle(): string;
  isLoading(): boolean;
  reload(): void;
  stop(): void;
  getZoomFactor(): number;
  setZoomFactor(factor: number): void;
  isDevToolsOpened(): boolean;
  openDevTools(options?: { mode: "detach" }): void;
  closeDevTools(): void;
  focus(): void;
  executeJavaScriptInIsolatedWorld(worldId: number, scripts: { code: string }[], userGesture?: boolean): Promise<unknown>;
  sendInputEvent(event: MouseInputEvent | MouseWheelInputEvent | KeyboardInputEvent): void;
  insertText(text: string): Promise<void>;
  capturePage(rect?: Rectangle): Promise<GuestImage>;
}

export interface GuestViewEvents {
  onStartLoading(): void;
  onStopLoading(): void;
  onNavigate(url: string, inPage: boolean): void;
  onTitle(title: string): void;
  onFailLoad(code: number, description: string, url: string): void;
  onRenderProcessGone(reason: string): void;
  onDestroyed(): void;
  // Popups (window.open, target=_blank) become tabs of the same task and
  // partition; adopt() receives the view Electron created for the child.
  onPopup(url: string, disposition: string): ((view: GuestView) => void) | null;
}

export interface GuestView {
  readonly page: GuestPage;
  bind(events: GuestViewEvents): void;
  setBounds(bounds: Rectangle): void;
  setVisible(visible: boolean): void;
  destroy(): void;
}

export interface GuestViewFactory {
  create(partition: string, inherited?: WebPreferences): GuestView;
}

export const BLOCKED_NAVIGATION_SCHEMES = new Set(["file:", "reasonix:", "javascript:", "data:", "blob:"]);

export function isBlockedNavigation(url: string): boolean {
  try {
    return BLOCKED_NAVIGATION_SCHEMES.has(new URL(url).protocol);
  } catch {
    return true;
  }
}

export function isPopupURL(url: string): boolean {
  if (url === "about:blank" || url === "") return true;
  try {
    const protocol = new URL(url).protocol;
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

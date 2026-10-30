// What a window can do and a page cannot, behind one interface so nothing in
// the app learns which shell it is running in. Electron exposes a preload
// bridge; a browser tab has none, and answers for itself.

import type { FeedbackProcessMemory } from "./feedback";

export type Shell = "electron" | "browser";

export interface HostInfo {
  shell: Shell;
  /** "darwin" | "windows" | "linux", or "" where the page cannot know. */
  platform: string;
  /** The window has no native title bar, so the top row of the page is one.
   *  Answered by the shell: only it knows how its window was created. */
  titleBar: boolean;
}

/** How this launch draws. `launchedOff` is what the shell applied at start,
 *  `savedOff` the saved choice alone (absent from an older shell), and
 *  `compositing` Electron's own status string ("" when it has none yet). */
export interface GraphicsInfo {
  launchedOff: boolean;
  savedOff?: boolean;
  compositing: string;
}

export interface GlobalShortcutInfo {
  binding: string;
  status: "disabled" | "registered" | "invalid" | "unavailable" | "unsupported";
  error?: "invalid" | "unavailable" | "save" | "quitting";
}

const unsupportedShortcut: GlobalShortcutInfo = { binding: "", status: "unsupported" };

export interface TextContextMenuRequest {
  editable: boolean;
  selected: boolean;
  canUndo: boolean;
  canRedo: boolean;
  quote: boolean;
  x: number;
  y: number;
}

export type TextContextMenuResult = "quote" | "undo" | "redo" | "selectAll" | "";

export interface HostPort {
  /** Whether this page runs inside the desktop shell, as opposed to a browser
   *  tab, where the browser's own keys (zoom among them) are the reader's. */
  inShell(): boolean;
  describe(): Promise<HostInfo>;
  minimiseWindow(): void;
  toggleMaximiseWindow(): void;
  isWindowMaximised(): Promise<boolean>;
  closeWindow(): void;
  syncTheme(mode: string, scheme: "light" | "dark"): void;
  /** Whether this shell can hand back a picture of its own window. */
  capturesWindow(): boolean;
  /** A PNG of this window as drawn now; null when the shell cannot or the
   *  window has nothing to show. It names no window: only the asker's own. */
  captureWindow(): Promise<Uint8Array | null>;
  /** How the shell draws this launch; null where it has no say, as in a tab. */
  graphics(): Promise<GraphicsInfo | null>;
  globalShortcut(): Promise<GlobalShortcutInfo>;
  setGlobalShortcut(binding: string): Promise<GlobalShortcutInfo>;
  /** The shell's own processes as a type and two sizes each, for the memory a
   *  report may carry; empty where there is no shell to ask, as in a tab. */
  processMemory(): Promise<FeedbackProcessMemory[]>;
  openExternal(url: string): void | Promise<void>;
  setUnreadBadge(unread: boolean): Promise<void>;
  /** Where dropped files live. Empty where the shell cannot say — a browser
   *  tab never learns a path. */
  pathsForFiles(files: File[]): string[];
  /** The files the clipboard carries, named by the shell from the formats that
   *  carry files. Empty where the shell has none to name — a browser tab asks
   *  nobody, and a clipboard holding text names no file. */
  clipboardFiles(): Promise<string[]>;
  /** Put text on disk where the user picks. null means this shell has no save
   *  surface at all; "" means they dismissed the dialog, which is an answer. */
  saveText(name: string, content: string): Promise<string | null>;
  saveBytes(name: string, bytes: Uint8Array): Promise<string | null>;
  /** null means unsupported; "" means dismissal or a standard native action. */
  showTextContextMenu(request: TextContextMenuRequest): Promise<TextContextMenuResult | null>;
  /** Ask for a directory. null means this shell has no picker at all; "" means
   *  they dismissed it, the same two answers saveText gives. startIn is where
   *  to open: the shell owns the dialog, the kernel owns which workspace runs,
   *  so the page carries one to the other. */
  pickFolder(startIn: string): Promise<string | null>;
  /** Whether this shell can show a file in the platform's file manager. */
  revealsFiles(): boolean;
  /** Show an entry of the workspace behind the pane at base, "" being its
   *  root. The shell asks the kernel where that is and selects only the answer;
   *  null once shown, otherwise why it was not. */
  revealPath(base: string, path: string): Promise<Refusal | null>;
  opensFiles(): boolean;
  openFile(base: string, path: string): Promise<Refusal | null>;
  /** Show a project the sidebar lists, which needs no pane. The hub answers
   *  where it is and refuses any folder it does not list. */
  revealWorkspace(root: string): Promise<Refusal | null>;
  /** Whether this shell can open the folder it writes its own logs to. */
  revealsLogs(): boolean;
  /** Open that folder in the platform's file manager; null once shown,
   *  otherwise why it was not. */
  revealLogs(): Promise<Refusal | null>;
  /** Whether this shell can enter a folder in the platform's file manager. */
  opensFolders(): boolean;
  /** Enter a project the sidebar lists: the same kernel answer revealWorkspace
   *  asks for, opened rather than selected in its parent folder. */
  openWorkspace(root: string): Promise<Refusal | null>;
  /** Whether this shell draws the agent's browser pages inside the window. */
  drawsBrowserViews(): boolean;
  /** Draw one of the agent's pages over rect, in on-screen coordinates, and
   *  hide every other; hideBrowserView puts them all away again. */
  showBrowserView(target: string, rect: ViewRect): void;
  hideBrowserView(): void;
  /** Put the page on screen away the way hideBrowserView does, answering with
   *  a picture of what it showed at that moment, or "" when none was taken. */
  freezeBrowserView(): Promise<string>;
  controlBrowserView(target: string, action: BrowserControl): void;
  /** Load what the person typed. "" when it loads, else why the shell refused it. */
  navigateBrowserView(target: string, address: string): Promise<BrowserRefusal>;
  /** Why a page did not load, or null once another load starts. */
  onBrowserLoadState(listener: (state: BrowserLoadState) => void): () => void;
  /** A page or a proxy asking for a login; answered with answerBrowserLogin. */
  onBrowserLogin(listener: (ask: BrowserLogin) => void): () => void;
  /** An empty username cancels the login. */
  answerBrowserLogin(id: string, username: string, password: string): void;
  /** Proceed past the certificate this page was refused for, for this run. */
  trustBrowserCertificate(target: string): Promise<boolean>;
}

/** Why the shell will not load a typed address: a scheme it never opens, or a
 *  file on another machine. */
export type BrowserRefusal = "" | "scheme" | "network_file";

/** A kernel refusal as the shell hands it back: the code, and English fallback. */
export interface Refusal {
  code?: string;
  error?: string;
  params?: Record<string, string | number>;
}

export interface BrowserLoadFailure {
  url: string;
  /** Chromium's net error number, negative. */
  code: number;
  /** Chromium's net error name, e.g. ERR_CERT_AUTHORITY_INVALID. */
  reason: string;
  /** The certificate a certificate error was raised for. */
  certificate?: BrowserCertificate;
}

export interface BrowserCertificate {
  host: string;
  error: string;
  fingerprint: string;
  subject: string;
  issuer: string;
  /** Seconds since the epoch. */
  expiry: number;
}

export interface BrowserLoadState {
  targetId: string;
  failure: BrowserLoadFailure | null;
}

export interface BrowserLogin {
  id: string;
  targetId: string;
  url: string;
  host: string;
  port: number;
  realm: string;
  proxy: boolean;
}

export interface ViewRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export type BrowserControl = "back" | "forward" | "reload" | "stop";

// The preload bridge. Verbs only: the origin the page was loaded from and the
// credential that opens it never cross it.
interface ElectronBridge {
  shell: "electron";
  platform: string;
  titleBar: boolean;
  minimiseWindow(): Promise<void>;
  toggleMaximiseWindow(): Promise<void>;
  isWindowMaximised(): Promise<boolean>;
  closeWindow(): Promise<void>;
  syncTheme?(mode: string, scheme: "light" | "dark"): void;
  captureWindow?(): Promise<Uint8Array | null>;
  graphics?(): Promise<GraphicsInfo | null>;
  globalShortcut?(): Promise<GlobalShortcutInfo | null>;
  setGlobalShortcut?(binding: string): Promise<GlobalShortcutInfo | null>;
  processMemory?(): Promise<unknown>;
  openExternal(url: string): Promise<void>;
  setUnreadBadge?(unread: boolean): Promise<void>;
  pathForFile(file: File): string;
  clipboardFiles?(): Promise<string[]>;
  saveText(name: string, content: string): Promise<string>;
  saveBytes(name: string, bytes: Uint8Array): Promise<string>;
  showTextContextMenu?(request: TextContextMenuRequest): Promise<TextContextMenuResult>;
  pickFolder(startIn: string): Promise<string>;
  revealPath?(base: string, path: string): Promise<Refusal | null>;
  openFile?(base: string, path: string): Promise<Refusal | null>;
  revealWorkspace?(root: string): Promise<Refusal | null>;
  revealLogs?(): Promise<Refusal | null>;
  openWorkspace?(root: string): Promise<Refusal | null>;
  showBrowserView?(target: string, rect: ViewRect): Promise<void>;
  hideBrowserView?(): Promise<void>;
  freezeBrowserView?(): Promise<string>;
  controlBrowserView?(target: string, action: string): Promise<void>;
  navigateBrowserView?(target: string, address: string): Promise<BrowserRefusal>;
  onBrowserLoadState?(listener: (state: BrowserLoadState) => void): () => void;
  onBrowserLogin?(listener: (ask: BrowserLogin) => void): () => void;
  answerBrowserLogin?(id: string, username: string, password: string): Promise<void>;
  trustBrowserCertificate?(target: string): Promise<boolean>;
}

const bridge = () => (window as unknown as { reasonixHost?: ElectronBridge }).reasonixHost;

// Electron reports the platform under Node's names; the page spells them the
// way Go does, and one spelling is what keeps a CSS selector honest.
function normalise(platform: string): string {
  if (platform === "win32") return "windows";
  return platform;
}

class ElectronHost implements HostPort {
  inShell() {
    return true;
  }
  constructor(private readonly api: ElectronBridge) {}
  describe() {
    return Promise.resolve({
      shell: "electron" as const,
      platform: normalise(this.api.platform),
      titleBar: this.api.titleBar,
    });
  }
  minimiseWindow() {
    void this.api.minimiseWindow();
  }
  toggleMaximiseWindow() {
    void this.api.toggleMaximiseWindow();
  }
  isWindowMaximised() {
    return this.api.isWindowMaximised().catch(() => false);
  }
  closeWindow() {
    void this.api.closeWindow();
  }
  syncTheme(mode: string, scheme: "light" | "dark") {
    this.api.syncTheme?.(mode, scheme);
  }
  capturesWindow() {
    return typeof this.api.captureWindow === "function";
  }
  captureWindow() {
    return this.api.captureWindow?.().then((b) => (b?.byteLength ? new Uint8Array(b) : null)).catch(() => null) ?? Promise.resolve(null);
  }
  showTextContextMenu(request: TextContextMenuRequest) {
    return this.api.showTextContextMenu?.(request) ?? Promise.resolve(null);
  }
  graphics() {
    return this.api.graphics?.().catch(() => null) ?? Promise.resolve(null);
  }
  globalShortcut() {
    return this.api.globalShortcut?.().then(value => value ?? unsupportedShortcut) ?? Promise.resolve(unsupportedShortcut);
  }
  setGlobalShortcut(binding: string) {
    return this.api.setGlobalShortcut?.(binding).then(value => value ?? unsupportedShortcut) ?? Promise.resolve(unsupportedShortcut);
  }
  processMemory() {
    return this.api.processMemory?.().then((rows: unknown) => (Array.isArray(rows) ? (rows as FeedbackProcessMemory[]) : [])).catch(() => []) ?? Promise.resolve([]);
  }
  openExternal(url: string) {
    return this.api.openExternal(url);
  }
  setUnreadBadge(unread: boolean) {
    return this.api.setUnreadBadge?.(unread) ?? Promise.resolve();
  }
  pathsForFiles(files: File[]) {
    // Resolved one at a time because that is the shape the platform offers;
    // a file the shell cannot place answers with "" and is dropped here.
    return files.map((f) => this.api.pathForFile(f)).filter(Boolean);
  }
  clipboardFiles() {
    // A shell older than the verb has no clipboard to ask, and answers with
    // nothing rather than with a guess.
    return this.api.clipboardFiles?.().then((paths) => (Array.isArray(paths) ? paths : [])).catch(() => []) ?? Promise.resolve<string[]>([]);
  }
  saveText(name: string, content: string) {
    return this.api.saveText(name, content);
  }
  saveBytes(name: string, bytes: Uint8Array) {
    return this.api.saveBytes(name, bytes);
  }
  pickFolder(startIn: string) {
    return this.api.pickFolder(startIn);
  }
  revealsFiles() {
    return typeof this.api.revealPath === "function" && typeof this.api.revealWorkspace === "function";
  }
  revealPath(base: string, path: string) {
    return this.api.revealPath?.(base, path) ?? Promise.resolve({ error: "this shell cannot show files" });
  }
  opensFiles() { return typeof this.api.openFile === "function"; }
  openFile(base: string, path: string) {
    return this.api.openFile?.(base, path) ?? Promise.resolve({ error: "this shell cannot open files" });
  }
  revealWorkspace(root: string) {
    return this.api.revealWorkspace?.(root) ?? Promise.resolve({ error: "this shell cannot show files" });
  }
  revealsLogs() {
    return typeof this.api.revealLogs === "function";
  }
  revealLogs() {
    return this.api.revealLogs?.() ?? Promise.resolve({ error: "this shell cannot show files" });
  }
  opensFolders() {
    return typeof this.api.openWorkspace === "function";
  }
  openWorkspace(root: string) {
    return this.api.openWorkspace?.(root) ?? Promise.resolve({ error: "this shell cannot open folders" });
  }
  // A shell older than the verbs has no views to draw, and says so by lacking them.
  drawsBrowserViews() {
    return typeof this.api.showBrowserView === "function";
  }
  showBrowserView(target: string, rect: ViewRect) {
    void this.api.showBrowserView?.(target, rect);
  }
  hideBrowserView() {
    void this.api.hideBrowserView?.();
  }
  freezeBrowserView() {
    if (this.api.freezeBrowserView) return this.api.freezeBrowserView().catch(() => "");
    this.hideBrowserView();
    return Promise.resolve("");
  }
  controlBrowserView(target: string, action: BrowserControl) {
    void this.api.controlBrowserView?.(target, action);
  }
  navigateBrowserView(target: string, address: string) {
    return this.api.navigateBrowserView?.(target, address) ?? Promise.resolve<BrowserRefusal>("scheme");
  }
  onBrowserLoadState(listener: (state: BrowserLoadState) => void) {
    return this.api.onBrowserLoadState?.(listener) ?? (() => {});
  }
  onBrowserLogin(listener: (ask: BrowserLogin) => void) {
    return this.api.onBrowserLogin?.(listener) ?? (() => {});
  }
  answerBrowserLogin(id: string, username: string, password: string) {
    void this.api.answerBrowserLogin?.(id, username, password);
  }
  trustBrowserCertificate(target: string) {
    return this.api.trustBrowserCertificate?.(target) ?? Promise.resolve(false);
  }
}

class BrowserHost implements HostPort {
  inShell() {
    return false;
  }
  describe() {
    return Promise.resolve({ shell: "browser" as const, platform: "", titleBar: false });
  }
  minimiseWindow() {}
  toggleMaximiseWindow() {}
  isWindowMaximised() {
    return Promise.resolve(false);
  }
  closeWindow() {}
  capturesWindow() {
    return false;
  }
  captureWindow() {
    return Promise.resolve(null);
  }
  syncTheme() {}
  showTextContextMenu() {
    return Promise.resolve(null);
  }
  graphics() {
    return Promise.resolve(null);
  }
  globalShortcut() {
    return Promise.resolve(unsupportedShortcut);
  }
  setGlobalShortcut() {
    return Promise.resolve(unsupportedShortcut);
  }
  processMemory() {
    return Promise.resolve([]);
  }
  openExternal(url: string) {
    window.open(url, "_blank", "noopener,noreferrer");
  }
  setUnreadBadge() {
    return Promise.resolve();
  }
  pathsForFiles() {
    return [];
  }
  clipboardFiles() {
    return Promise.resolve<string[]>([]);
  }
  saveText() {
    return Promise.resolve(null);
  }
  saveBytes() {
    return Promise.resolve(null);
  }
  pickFolder() {
    return Promise.resolve(null);
  }
  revealsFiles() {
    return false;
  }
  revealPath() {
    return Promise.resolve({ error: "a browser tab cannot show files" });
  }
  opensFiles() { return false; }
  openFile() { return Promise.resolve({ error: "a browser tab cannot open files" }); }
  revealWorkspace() {
    return Promise.resolve({ error: "a browser tab cannot show files" });
  }
  revealsLogs() {
    return false;
  }
  revealLogs() {
    return Promise.resolve({ error: "a browser tab cannot show files" });
  }
  opensFolders() {
    return false;
  }
  openWorkspace() {
    return Promise.resolve({ error: "a browser tab cannot open folders" });
  }
  drawsBrowserViews() {
    return false;
  }
  showBrowserView() {}
  hideBrowserView() {}
  freezeBrowserView() {
    return Promise.resolve("");
  }
  controlBrowserView() {}
  navigateBrowserView() {
    return Promise.resolve<BrowserRefusal>("scheme");
  }
  onBrowserLoadState() {
    return () => {};
  }
  onBrowserLogin() {
    return () => {};
  }
  answerBrowserLogin() {}
  trustBrowserCertificate() {
    return Promise.resolve(false);
  }
}

function pick(): HostPort {
  const api = bridge();
  if (api) return new ElectronHost(api);
  return new BrowserHost();
}

let chosen: HostPort | null = null;

/** The shell under this page, decided once. */
export function host(): HostPort {
  chosen ??= pick();
  return chosen;
}

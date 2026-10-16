// What a window can do and a page cannot, behind one interface so nothing in
// the app learns which shell it is running in. Electron exposes a preload
// bridge; a browser tab has none, and answers for itself; an installed phone
// shell exposes a third, narrower one through `@JavascriptInterface`.

export type Shell = "electron" | "browser" | "android";

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

export interface HostPort {
  /** Whether this page runs inside the desktop shell, as opposed to a browser
   *  tab, where the browser's own keys (zoom among them) are the reader's. */
  inShell(): boolean;
  describe(): Promise<HostInfo>;
  minimiseWindow(): void;
  toggleMaximiseWindow(): void;
  isWindowMaximised(): Promise<boolean>;
  closeWindow(): void;
  /** Whether this shell can hand back a picture of its own window. */
  capturesWindow(): boolean;
  /** A PNG of this window as drawn now; null when the shell cannot or the
   *  window has nothing to show. It names no window: only the asker's own. */
  captureWindow(): Promise<Uint8Array | null>;
  /** How the shell draws this launch; null where it has no say, as in a tab. */
  graphics(): Promise<GraphicsInfo | null>;
  openExternal(url: string): void | Promise<void>;
  setUnreadBadge(unread: boolean): Promise<void>;
  /** Where dropped files live. Empty where the shell cannot say — a browser
   *  tab never learns a path. */
  pathsForFiles(files: File[]): string[];
  /** Put text on disk where the user picks. null means this shell has no save
   *  surface at all; "" means they dismissed the dialog, which is an answer. */
  saveText(name: string, content: string): Promise<string | null>;
  saveBytes(name: string, bytes: Uint8Array): Promise<string | null>;
  /** Whether this shell can hand a file to the platform's share sheet.
   *  Asked synchronously while a menu is drawn, like `canImportIntoHome`. */
  canShareText(): boolean;
  /** Hand a file to whatever the device shares with: the mail client, a chat
   *  app, a file manager. false means nothing on the device took it. */
  shareText(name: string, content: string): Promise<boolean>;
  /** Whether this shell can drop a copy straight into the place the phone's
   *  own file manager opens. Asked while a bar is drawn, so it is synchronous
   *  like `canShareText`. */
  canExportText(): boolean;
  /** Copy a file into that place without asking where, and answer with the
   *  path a person can go and open. null means this shell has no such place —
   *  a desktop saves where it is told instead — and "" means the copy failed.
   *
   *  A phone needs the difference. The kernel's workspace is app-private
   *  storage, so the file someone wants is somewhere no file manager can show,
   *  and "另存为" opens a picker that starts at recent — one more place to lose
   *  it. This is the answer to "where is it": Downloads, under the app's name,
   *  where the phone's own Files app already looks. */
  exportText(name: string, content: string): Promise<string | null>;
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
  /**
   * Copy a file the person picks from outside the app into the kernel's home,
   * and answer with its path there. null means this shell cannot ask.
   *
   * On a desktop the question does not arise: `~/.ssh/id_ed25519` is a path the
   * person already has, and typing it is the whole interaction. A phone has no
   * such path — the file lives in a document provider the kernel cannot read —
   * so the shell has to carry it across. `kind` says which directory inside the
   * home it belongs in, because that is a fact about the kernel's layout rather
   * than about the file.
   */
  importIntoHome(kind: "identity" | "env", name: string): Promise<ImportOutcome>;
  /**
   * Whether `importIntoHome` can actually put a file somewhere the kernel reads.
   *
   * Asked synchronously while a form is being drawn, which is why it is not a
   * promise: the button it gates either exists in this render or does not. A
   * desktop answers false — its file paths are already real, so there is nothing
   * to carry — and a browser tab false, having no picker at all.
   */
  canImportIntoHome(): boolean;
  /**
   * Whether this shell can read a pairing code with its camera.
   *
   * A phone can and a desktop cannot: the code is shown *by* the machine the
   * person is sitting at, so the only device that needs to scan is the one in
   * their hand. Asked synchronously while a button is drawn, like
   * `canImportIntoHome`, so the entry point either exists in this render or not.
   */
  canScanPairingCode(): boolean;
  /**
   * The `type` a field holding a secret should carry in this shell.
   *
   * `type="password"` is how a browser hides a value, and on a Chinese ROM it is
   * also a signal to the system: the field switches to that ROM's own "secure"
   * keyboard, which withholds the clipboard. An API key is forty characters of
   * random case that nobody types twice, so a field that can be neither pasted
   * into nor copied out of is one the person cannot use at all.
   *
   * So the phone answers "text" and masks the value in the page instead
   * (`-webkit-text-security`), which hides the glyphs without telling the
   * platform's keyboard anything. Every other shell answers "password" and keeps
   * the browser's own masking — the behaviour is only wrong on the ROMs that
   * overreach, so it is only replaced there.
   */
  secretFieldType(): "password" | "text";
  /**
   * Put text on the clipboard, when this shell has one of its own.
   *
   * `true`/`false` is the shell doing it and saying whether it worked. `null`
   * means it has no clipboard to offer and the page's own route — a WebView's
   * `navigator.clipboard`, which Android gates behind a permission only a
   * focused document gets — is the one to try.
   *
   * The three-valued answer is the point. A phone shell can reach the system
   * clipboard directly and must, because the page cannot; a desktop and a
   * browser tab both have a working route in the page, and routing those
   * through a shell would be a longer path to the same place.
   */
  copyText(text: string): boolean | null;
  /**
   * How this shell can be asked for a folder, if at all.
   *
   *   "native" — a dialog that answers with a real filesystem path. Electron.
   *   "typed"  — no dialog, but the person is at a machine and knows its paths,
   *              so typing one is a fair question. A browser tab.
   *   "none"   — neither, and this is the phone: its picker answers with a
   *              document-provider URI the kernel cannot open, and the kernel's
   *              own root is app-private storage whose path nobody has seen.
   *
   * Three answers rather than a boolean because the caller has to tell "ask with
   * a dialog" from "ask by typing" from "do not ask" — with no picker,
   * "open a project" falls through to a prompt for a path *on the kernel's
   * machine*, which is a fair question on a desktop and an unanswerable one on a
   * phone.
   */
  folderPicker(): "native" | "typed" | "none";
  /**
   * Hold the screen on while a turn is running.
   *
   * Optional, and only a phone implements it: a desktop window has no screen to
   * keep awake, and a browser tab cannot ask. The page calls it when its own
   * `running` changes — the fact is the page's, the window flag is the shell's.
   */
  setRunning?(running: boolean): void;
  /**
   * Whether this shell can open a folder on this device as a workspace, and
   * whether it is allowed to yet.
   *
   * Two questions because the phone's answer is two: the picker exists, and the
   * every-file permission that makes its answer usable may not have been
   * granted. A shell without the notion answers `granted: true`.
   */
  filesAccess?(): { granted: boolean };
  /** Ask the shell to obtain that access. The system page for it is the only
   *  place Android offers, so this navigates rather than prompting. */
  requestFilesAccess?(): void;
  /** Whether the app is exempt from the system's battery optimisation, which is
   *  what lets a running turn keep making progress with the screen off. */
  batteryUnrestricted?(): boolean;
  /** Ask for that exemption. */
  requestBatteryUnrestricted?(): void;
  /**
   * Tell the shell whether a dismissible layer is on screen.
   *
   * The back gesture belongs to the shell — a page never sees it — but only the
   * page knows what is open. So the page reports the fact and the shell keeps
   * the decision: close the innermost layer, and only when there is none leave
   * the screen. The alternative, the shell inspecting the DOM, would be a second
   * implementation of "what is open" that drifts from the first.
   *
   * Optional: a browser tab and a desktop window both have a back of their own.
   */
  reportLayerOpen?(open: boolean): void;
  /**
   * Open the camera and answer with the code's own text, "" if none was read.
   *
   * The text is returned raw and the page decides what it means: the pairing
   * link a computer shows is a URL with a `#pair=` fragment, and the handling for
   * that already ships with the interface. Parsing it here would be a second
   * implementation of a rule the page already owns.
   */
  scanPairingCode(): Promise<string>;
  /**
   * Load a URL in this shell's own window instead of handing it to a browser.
   *
   * Asked for right after a scan: the whole point of reading a computer's code is
   * to end up looking at that computer, and the credential the pairing exchange
   * leaves behind is scoped to this window. A shell that cannot do it says so by
   * having no method; a browser tab has no window of its own to prefer.
   */
  openInApp?(url: string): void;
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

/**
 * The answer to "put this file where the kernel can read it".
 *
 * Three outcomes, kept apart on purpose, because a caller that conflates any
 * two of them asks the person to pick the same file twice: a path means it
 * landed, "" means they dismissed the picker (an answer), and null means this
 * shell has no picker at all.
 */
export type ImportOutcome = string | null;

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
  captureWindow?(): Promise<Uint8Array | null>;
  graphics?(): Promise<GraphicsInfo | null>;
  openExternal(url: string): Promise<void>;
  setUnreadBadge?(unread: boolean): Promise<void>;
  pathForFile(file: File): string;
  saveText(name: string, content: string): Promise<string>;
  saveBytes(name: string, bytes: Uint8Array): Promise<string>;
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
  capturesWindow() {
    return typeof this.api.captureWindow === "function";
  }
  captureWindow() {
    return this.api.captureWindow?.().then((b) => (b?.byteLength ? new Uint8Array(b) : null)).catch(() => null) ?? Promise.resolve(null);
  }
  graphics() {
    return this.api.graphics?.().catch(() => null) ?? Promise.resolve(null);
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
  saveText(name: string, content: string) {
    return this.api.saveText(name, content);
  }
  saveBytes(name: string, bytes: Uint8Array) {
    return this.api.saveBytes(name, bytes);
  }
  canShareText() {
    // A desktop writes the file and shows it; the platform's share sheet is a
    // phone's answer to "where did it go", and there is nothing to add here.
    return false;
  }
  shareText() {
    return Promise.resolve(false);
  }
  canExportText() {
    // A desktop's save dialog names a real folder the person chose, and its own
    // file manager is right there. A second destination would be one more place
    // to look, not fewer.
    return false;
  }
  exportText() {
    return Promise.resolve(null);
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
  importIntoHome() {
    // Electron already has the file: the path the picker answers with is a real
    // one the kernel can open, so there is nothing to carry across.
    return Promise.resolve("");
  }
  canImportIntoHome() {
    return false;
  }
  canScanPairingCode() {
    // The code is shown *by* the machine this shell runs on, so there is
    // nothing here to point a camera at.
    return false;
  }
  scanPairingCode() {
    return Promise.resolve("");
  }
  secretFieldType() {
    // A desktop's browser masks the value and its keyboard still pastes. The
    // clipboard-disabling "secure" keyboard is a phone ROM's behaviour, so
    // nothing here needs replacing.
    return "password" as const;
  }
  copyText() {
    // The page's own `navigator.clipboard` works in this window, so the shell
    // hands the text back rather than carrying it a second way.
    return null;
  }
  folderPicker() {
    // The window's own dialog, and the path it answers with is one the kernel
    // can open.
    return "native" as const;
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
  graphics() {
    return Promise.resolve(null);
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
  saveText() {
    return Promise.resolve(null);
  }
  saveBytes() {
    return Promise.resolve(null);
  }
  canShareText() {
    // A browser tab has no share sheet of its own; its own download path is the
    // way out, and a second one here would be a second answer to one tap.
    return false;
  }
  shareText() {
    return Promise.resolve(false);
  }
  canExportText() {
    return false;
  }
  exportText() {
    // A browser tab has the browser's own download shelf, which is a real one.
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
  importIntoHome() {
    return Promise.resolve(null);
  }
  canImportIntoHome() {
    return false;
  }
  canScanPairingCode() {
    // A tab has a camera through `getUserMedia`, and deliberately does not use
    // it: reading a code is the installed app's gesture, and offering it here
    // would promise a flow whose second half — the pairing cookie, kept in the
    // app's own jar — a tab cannot complete the same way.
    return false;
  }
  scanPairingCode() {
    return Promise.resolve("");
  }
  secretFieldType() {
    return "password" as const;
  }
  copyText() {
    // A tab is a secure context on every host that matters, so the page's own
    // clipboard call is the shorter path and the shell declines.
    return null;
  }
  folderPicker() {
    // No dialog of its own, but the person is at a machine and knows its paths —
    // and when the kernel *can* open one, `hub.hostCapabilities` asks it to and
    // this is never reached. Typing a path is a fair question here and an
    // unanswerable one on a phone, which is the whole reason for the third
    // answer.
    return "typed" as const;
  }
}

/**
 * An installed phone shell.
 *
 * It speaks a deliberately narrow contract: `@JavascriptInterface` exposes only
 * these methods, and each one answers with a string or nothing. The page never
 * learns a filesystem it can walk, a shell it can run, or a path outside the
 * kernel's own home — the three things that would turn a phone shell into a
 * general-purpose bridge.
 *
 * What it can do that a browser tab cannot is the short list below, and each
 * entry is a thing the phone genuinely has and the page genuinely needs: the
 * system's file picker, the system's share sheet, the app's own window, and the
 * device's back gesture.
 */
interface AndroidBridge {
  version: string;
  /** Copy a file the person picks into the kernel's home; "" if dismissed. */
  importIntoHome(kind: string, name: string): string;
  /** Open a URL in the device's browser rather than this window. */
  openExternal(url: string): void;
  /** Put text on the clipboard, which is the only way out of a sandboxed page.
   *  Answers whether the write landed; a refusal is reported as one. */
  copyText(text: string): boolean;
  /** The device's own dark/light answer, for a page with no preference saved. */
  systemTheme(): string;
  /** Read the pairing code a computer is showing; "" if none was read. */
  scanPairingCode(): string;
  /** Load a URL here instead of in a browser, after a scan. */
  openInApp(url: string): void;
  /** Whether a panel is up that the back gesture should close first. */
  setLayerOpen(open: boolean): void;
  /** Put a file on disk where the person picks; "" if they dismissed it. */
  saveText(name: string, content: string): string;
  /** Hand a file to the system share sheet; false if nothing took it. */
  shareText(name: string, content: string): boolean;
  /** Drop a copy in Downloads under the app's own folder; the path, or "". */
  exportToDownloads(name: string, content: string): string;
  /** Ask for a directory and answer with its real filesystem path; "" if they
   *  dismissed the picker. `startIn` is a hint the picker may ignore. */
  pickFolder(startIn: string): string;
  /** Tell the shell whether a turn is running, so it can hold the screen on. */
  setRunning(running: boolean): void;
  /** Whether this app may read every file, which is what makes a phone folder
   *  usable as a workspace. Always true below API 30. */
  hasAllFilesAccess(): boolean;
  /** Open the system page holding that permission — there is no dialog. */
  requestAllFilesAccess(): void;
  /** Whether the app is exempt from the system's battery optimisation. */
  batteryUnrestricted(): boolean;
  /** Ask for that exemption; this one does have a dialog. */
  requestBatteryUnrestricted(): void;
}

const androidBridge = () => (window as unknown as { ReasonixNative?: AndroidBridge }).ReasonixNative;

// The native browser view, when this shell has one.
//
// Kotlin injects a `window.reasonixHost` carrying exactly the verbs the Electron
// preload does, so a phone draws a real top-level WebView instead of the DOM
// iframe the panel falls back to. That is the difference that matters: an iframe
// is subject to the target site's `X-Frame-Options`, and the sites worth reading
// (Stack Overflow, most signed-in ones) refuse to be framed — the panel showed
// `net::ERR_BLOCKED_BY_RESPONSE` where the page should have been. A WebView is
// its own browsing context and is not framed by anything, so the same page
// loads. Reading it for the agent is the kernel's half, over the shell's own
// loopback server; this is only the drawing half.
//
// Optional on both ends: an older shell ships no such object, `drawsBrowserViews`
// answers false, and the panel keeps using the iframe it always did.
const nativeBrowserView = () =>
  (window as unknown as { reasonixHost?: Partial<ElectronBridge> }).reasonixHost;

class AndroidHost implements HostPort {
  constructor(private readonly api: AndroidBridge) {}
  inShell() {
    // Deliberately false. "In a shell" gates the desktop's own keyboard
    // shortcuts and its zoom chords, and a phone has neither a keyboard nor a
    // window to resize — the WebView's own pinch zoom is the reader's.
    return false;
  }
  describe() {
    return Promise.resolve({ shell: "android" as const, platform: "android", titleBar: false });
  }
  minimiseWindow() {}
  toggleMaximiseWindow() {}
  isWindowMaximised() {
    return Promise.resolve(false);
  }
  closeWindow() {}
  capturesWindow() {
    // The feedback panel offers "capture this window" only where a shell can
    // draw one into a picture. A phone's window belongs to the system, and
    // reading it needs a permission this app does not hold, so the answer is no
    // and the panel keeps its own file picker instead.
    return false;
  }
  captureWindow() {
    return Promise.resolve(null);
  }
  graphics() {
    // Electron reports its own compositing status here. A WebView has none, and
    // the page falls back to what the platform itself says.
    return Promise.resolve(null);
  }
  openExternal(url: string) {
    this.api.openExternal(url);
  }
  setUnreadBadge() {
    // An app-icon badge is the launcher's, and this shell does not ask for one:
    // the notification a finished turn posts is what reaches someone who is not
    // looking at the page, and a badge would only repeat it.
    return Promise.resolve();
  }
  pathsForFiles() {
    // A phone has no path for a picked file until the shell has copied it, and
    // copying is `importIntoHome`'s job rather than this one's.
    return [];
  }
  saveText(name: string, content: string) {
    // A phone has to write this itself. The page's own route — a blob URL on an
    // `<a download>` — reaches nothing here: a WebView has no downloads shelf,
    // and its DownloadListener only fires for a real navigation, not for a blob
    // the page just built. So the bytes cross the bridge and the shell asks the
    // system where to put them, which is also the only way to reach the
    // Documents folder a person can actually find again.
    return Promise.resolve(this.api.saveText(name, content));
  }
  saveBytes() {
    // Bytes do not cross `@JavascriptInterface` — it takes strings — and every
    // caller that has bytes also has the text they came from.
    return Promise.resolve(null);
  }
  canShareText() {
    return true;
  }
  shareText(name: string, content: string) {
    // The share sheet, not a second save dialog: "where is that file" is the
    // question this answers, and a chat app or the mail client is where a phone
    // puts a document it wants to keep or send.
    return Promise.resolve(this.api.shareText(name, content));
  }
  canExportText() {
    return true;
  }
  exportText(name: string, content: string) {
    // No dialog: this one is the answer to "where is it", so asking where to
    // put it would be asking the question it exists to settle. The bridge's ""
    // is a failed write and stays "" — folding it into null would report a
    // failed copy as "this shell has nowhere to put it", which is a different
    // and untrue answer.
    return Promise.resolve(this.api.exportToDownloads(name, content));
  }
  revealsFiles() {
    return false;
  }
  revealPath() {
    return Promise.resolve({ error: "a phone cannot show files in a file manager" });
  }
  opensFiles() {
    // A desktop hands a file to whatever the platform registered for its type.
    // A phone has the same idea — an intent — but the kernel's own workspace is
    // app-private, so there is no file on the device to hand over. What a phone
    // does instead is carry a copy out: `shareText` and `exportText` above.
    return false;
  }
  openFile() {
    return Promise.resolve({ error: "a phone cannot open files in another application" });
  }
  revealWorkspace() {
    return Promise.resolve({ error: "a phone cannot show files in a file manager" });
  }
  revealsLogs() {
    return false;
  }
  revealLogs() {
    return Promise.resolve({ error: "a phone cannot show files in a file manager" });
  }
  opensFolders() {
    // False on purpose, and this is the one answer here that the page acts on
    // rather than merely reports: it gates the folder mark in the title bar,
    // which opens the workspace directory itself. A phone has no file manager
    // that can be pointed at app-private storage, so the mark is not offered —
    // the picker in the sidebar is how a folder is chosen here.
    return false;
  }
  openWorkspace() {
    return Promise.resolve({ error: "a phone cannot show files in a file manager" });
  }
  drawsBrowserViews() {
    // True once the shell has injected its native view. Until then the panel's
    // own iframe is what it draws, exactly as before.
    return typeof nativeBrowserView()?.showBrowserView === "function";
  }
  showBrowserView(target: string, rect: ViewRect) {
    void nativeBrowserView()?.showBrowserView?.(target, rect);
  }
  hideBrowserView() {
    void nativeBrowserView()?.hideBrowserView?.();
  }
  freezeBrowserView() {
    return nativeBrowserView()?.freezeBrowserView?.().catch(() => "") ?? Promise.resolve("");
  }
  controlBrowserView(target: string, action: BrowserControl) {
    void nativeBrowserView()?.controlBrowserView?.(target, action);
  }
  navigateBrowserView(target: string, address: string) {
    return nativeBrowserView()?.navigateBrowserView?.(target, address) ?? Promise.resolve<BrowserRefusal>("scheme");
  }
  onBrowserLoadState(listener: (state: BrowserLoadState) => void) {
    return nativeBrowserView()?.onBrowserLoadState?.(listener) ?? (() => {});
  }
  onBrowserLogin(listener: (ask: BrowserLogin) => void) {
    return nativeBrowserView()?.onBrowserLogin?.(listener) ?? (() => {});
  }
  answerBrowserLogin(id: string, username: string, password: string) {
    void nativeBrowserView()?.answerBrowserLogin?.(id, username, password);
  }
  trustBrowserCertificate(target: string) {
    return nativeBrowserView()?.trustBrowserCertificate?.(target) ?? Promise.resolve(false);
  }
  importIntoHome(kind: "identity" | "env", name: string) {
    // The bridge is synchronous by construction — `@JavascriptInterface` cannot
    // await — so it blocks the WebView's JS thread for the length of the copy.
    // That is acceptable for the files this carries (a private key, a dotenv,
    // both kilobytes) and is why nothing larger is routed through it.
    return Promise.resolve(this.api.importIntoHome(kind, name));
  }
  canImportIntoHome() {
    // The one thing a phone must add over a desktop: the kernel reads paths
    // under its own home, and a key sitting in the device's Documents folder is
    // not one of them.
    return true;
  }
  canScanPairingCode() {
    // The reason this shell exists: the code is shown by the computer being
    // driven, and the phone is the device holding the camera.
    return true;
  }
  scanPairingCode() {
    // Synchronous on the far side — `@JavascriptInterface` cannot await — so the
    // promise only carries the answer across, exactly as `importIntoHome` does.
    // It blocks the WebView's JS thread while the camera is open, which is a
    // full-screen view with nothing else in the page running.
    return Promise.resolve(this.api.scanPairingCode() || "");
  }
  secretFieldType() {
    // The one shell where `type="password"` costs the person the field.
    //
    // Chinese ROMs treat a password field as a signal to switch to their own
    // "secure" keyboard, and that keyboard withholds the clipboard. An API key is
    // forty characters of random case that nobody types twice, so a field it
    // cannot be pasted into is one that cannot be filled — and the same keyboard
    // stops a key being copied back out to check against the provider's console.
    // Answering "text" moves the masking into the page, which hides the glyphs
    // without telling the platform's keyboard anything.
    return "text" as const;
  }
  copyText(text: string) {
    // The one shell where the page cannot copy by itself, and the reason the
    // bridge has a `copyText` at all: a WebView gates `navigator.clipboard`
    // behind a permission Android grants only to a focused document, and the
    // `execCommand` fallback needs a selection the page no longer has once a
    // button has taken the focus. Every copy in the interface read "复制失败"
    // for that reason — a control whose only job is to copy, unable to.
    //
    // Synchronous on the far side, like `importIntoHome`: the shell has the
    // system clipboard and answers at once. The answer is the shell's, not an
    // assumption — a refused write is reported as one, and the button says so.
    return this.api.copyText(text);
  }
  folderPicker() {
    // A real dialog with a real answer.
    //
    // The picker hands back a document *tree*, and the shell translates it to
    // the filesystem path the kernel opens — which is why the every-file
    // permission has to be granted first, and why `pickFolder` below is the
    // only place that translation happens. Without the permission the path is
    // real but unreadable, and the kernel reports an empty folder a long way
    // from the cause, so `App` asks about it before asking for the folder.
    return "native" as const;
  }
  pickFolder(startIn: string) {
    // Synchronous on the far side — `@JavascriptInterface` cannot await — so the
    // promise only carries the answer across. "" is a dismissal, which the
    // caller reads as "they changed their mind", not as a failure.
    return Promise.resolve(this.api.pickFolder(startIn));
  }
  setRunning(running: boolean) {
    // A window flag is not something a page can set, so the fact crosses and the
    // shell applies it. `running` is the page's to say: only it knows whether
    // the kernel is working.
    this.api.setRunning(running);
  }
  filesAccess() {
    // The name and the shape are the port's, not the shell's: `HostPort` asks
    // one question with two parts — may this shell read a folder, and has the
    // person allowed it yet — and the call site reads `.granted` off the
    // answer. Answering a bare boolean under a different method name left the
    // caller with `undefined`, which it reads as "no opinion" and skips the
    // permission request entirely: the picker then returned a path the kernel
    // could not open, and the failure surfaced as an empty folder.
    return { granted: this.api.hasAllFilesAccess() };
  }
  requestFilesAccess() {
    this.api.requestAllFilesAccess();
  }
  batteryUnrestricted() {
    return this.api.batteryUnrestricted();
  }
  requestBatteryUnrestricted() {
    this.api.requestBatteryUnrestricted();
  }
  reportLayerOpen(open: boolean) {
    // One boolean, told to the shell as it changes. The shell keeps the back
    // gesture; this only answers "is there something to close first".
    this.api.setLayerOpen(open);
  }
  openInApp(url: string) {
    this.api.openInApp(url);
  }
}

function pick(): HostPort {
  const api = bridge();
  if (api) return new ElectronHost(api);
  const phone = androidBridge();
  if (phone) return new AndroidHost(phone);
  return new BrowserHost();
}

let chosen: HostPort | null = null;

/** The shell under this page, decided once. */
export function host(): HostPort {
  chosen ??= pick();
  return chosen;
}

/** Whether this page runs inside the installed phone shell.
 *
 *  Read from the document rather than by asking the shell: `index.html` sets
 *  `data-shell="android"` before the bundle is parsed, and `main.tsx` never
 *  overwrites an existing "android", so the answer is on the element before any
 *  component renders. It is the same attribute `mobile.css` keys every one of
 *  its rules off, so a stylesheet rule and a component that both mean "on a
 *  phone" cannot drift into disagreeing about which surface this is.
 *
 *  Synchronous on purpose: a component that renders a different set of controls
 *  here has to decide during render, not after a promise resolves. */
export function phoneShell(): boolean {
  return typeof document !== "undefined" && document.documentElement.dataset.shell === "android";
}

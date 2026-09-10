import type { KeyboardInputEvent, MouseInputEvent, MouseWheelInputEvent, Rectangle, WebPreferences } from "electron";
import type { GuestDebugger, GuestFrame, GuestImage, GuestPage, GuestView, GuestViewEvents, GuestViewFactory } from "./guestView.js";

// In-memory stand-ins for WebContentsView so the browser modules run under
// plain node --test. Scripts are answered by the injected `run` callback.

export type ScriptRunner = (code: string, frame: FakeFrame) => unknown;

export class FakeFrame implements GuestFrame {
  detached = false;
  parent: FakeFrame | null = null;
  children: FakeFrame[] = [];

  constructor(readonly frameTreeNodeId: number, public url: string, private readonly run: ScriptRunner) {}

  get framesInSubtree(): GuestFrame[] {
    return [this, ...this.children.flatMap((child) => child.framesInSubtree)];
  }

  async executeJavaScript(code: string): Promise<unknown> {
    return this.run(code, this);
  }
}

export class FakeDebugger implements GuestDebugger {
  attached = false;
  readonly commands: Array<{ method: string; params: unknown }> = [];
  private readonly listeners = new Set<(event: unknown, method: string, params: unknown) => void>();
  respond: (method: string, params: unknown) => unknown = () => ({});

  isAttached(): boolean {
    return this.attached;
  }

  attach(): void {
    this.attached = true;
  }

  detach(): void {
    this.attached = false;
  }

  async sendCommand(method: string, params?: unknown): Promise<unknown> {
    this.commands.push({ method, params });
    return this.respond(method, params);
  }

  emit(method: string, params: unknown): void {
    for (const listener of [...this.listeners]) listener({}, method, params);
  }

  on(_event: "message", listener: (event: unknown, method: string, params: unknown) => void): void {
    this.listeners.add(listener);
  }

  removeListener(_event: "message", listener: (event: unknown, method: string, params: unknown) => void): void {
    this.listeners.delete(listener);
  }
}

export class FakePage implements GuestPage {
  url = "about:blank";
  title = "";
  loading = false;
  destroyed = false;
  zoom = 1;
  devtools = false;
  back = false;
  forward = false;
  readonly calls: string[] = [];
  readonly inputs: Array<MouseInputEvent | MouseWheelInputEvent | KeyboardInputEvent> = [];
  readonly inserted: string[] = [];
  readonly mainFrame: FakeFrame;
  readonly debugger = new FakeDebugger();
  loadResult: Promise<void> = Promise.resolve();
  image: GuestImage = { toPNG: () => Buffer.from("png"), getSize: () => ({ width: 8, height: 6 }) };
  readonly capturedRects: Array<Rectangle | undefined> = [];

  constructor(readonly id: number, public run: ScriptRunner = () => undefined) {
    this.mainFrame = new FakeFrame(id * 100, this.url, (code, frame) => this.run(code, frame));
  }

  readonly navigationHistory = {
    canGoBack: () => this.back,
    canGoForward: () => this.forward,
    goBack: () => this.calls.push("back"),
    goForward: () => this.calls.push("forward"),
  };

  isDestroyed(): boolean {
    return this.destroyed;
  }

  loadURL(url: string): Promise<void> {
    this.calls.push(`load:${url}`);
    this.url = url;
    return this.loadResult;
  }

  getURL(): string {
    return this.url;
  }

  getTitle(): string {
    return this.title;
  }

  isLoading(): boolean {
    return this.loading;
  }

  reload(): void {
    this.calls.push("reload");
  }

  stop(): void {
    this.calls.push("stop");
  }

  getZoomFactor(): number {
    return this.zoom;
  }

  setZoomFactor(factor: number): void {
    this.zoom = factor;
  }

  isDevToolsOpened(): boolean {
    return this.devtools;
  }

  openDevTools(): void {
    this.devtools = true;
  }

  closeDevTools(): void {
    this.devtools = false;
  }

  focus(): void {
    this.calls.push("focus");
  }

  async executeJavaScriptInIsolatedWorld(_worldId: number, scripts: { code: string }[]): Promise<unknown> {
    return this.run(scripts[0]?.code ?? "", this.mainFrame);
  }

  sendInputEvent(event: MouseInputEvent | MouseWheelInputEvent | KeyboardInputEvent): void {
    this.inputs.push(event);
  }

  async insertText(text: string): Promise<void> {
    this.inserted.push(text);
  }

  async capturePage(rect?: Rectangle): Promise<GuestImage> {
    this.capturedRects.push(rect);
    return this.image;
  }
}

export class FakeGuestView implements GuestView {
  readonly page: FakePage;
  events: GuestViewEvents | null = null;
  bounds: Rectangle | null = null;
  visible = false;
  destroyed = false;

  constructor(id: number, readonly partition: string, readonly inherited?: WebPreferences) {
    this.page = new FakePage(id);
  }

  bind(events: GuestViewEvents): void {
    this.events = events;
  }

  setBounds(bounds: Rectangle): void {
    this.bounds = bounds;
  }

  setVisible(visible: boolean): void {
    this.visible = visible;
  }

  destroy(): void {
    this.destroyed = true;
    this.page.destroyed = true;
  }

  fire(): GuestViewEvents {
    if (!this.events) throw new Error("view is not bound");
    return this.events;
  }
}

export class FakeViewFactory implements GuestViewFactory {
  readonly views: FakeGuestView[] = [];
  private nextId = 1;

  create(partition: string, inherited?: WebPreferences): GuestView {
    const view = new FakeGuestView(this.nextId++, partition, inherited);
    this.views.push(view);
    return view;
  }
}

export const silentLog = { info() {}, warn() {}, error() {} };

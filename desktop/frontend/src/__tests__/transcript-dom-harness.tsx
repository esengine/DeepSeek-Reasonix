// Shared jsdom harness for Transcript rendering tests. The scroll container,
// block measurements, and window extent use deterministic layout metrics.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createServer, type ViteDevServer } from "vite";
import type { ReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import type { Item } from "../lib/useController";
import { TranscriptTestClock } from "./transcript-test-clock";

export interface TranscriptHarnessOptions {
  deterministic?: boolean;
  /** Viewport height of the scroll container. Default huge: every row mounts. */
  viewportHeight?: number;
  /** Fixed measured height for every transcript row. */
  rowHeight?: number;
  /** Extra localStorage seed values (display mode, fold preference, …). */
  storage?: Record<string, string>;
  /** Authoritative reasoning mode to hydrate before Transcript is imported. */
  reasoningDisplayMode?: ReasoningDisplayMode;
}

export interface TranscriptHarness {
  clock: TranscriptTestClock;
  resizeNotifications: Array<() => void>;
  observers: Array<{ target: Element; notify: () => void }>;
  dom: JSDOM;
  container: HTMLElement;
  server: ViteDevServer;
  scrollElement: () => HTMLElement;
  render: (items: Item[], props?: Record<string, unknown>) => Promise<void>;
  flush: () => Promise<void>;
  settle: () => Promise<void>;
  waitFor: (condition: () => boolean, description: string, attempts?: number) => Promise<void>;
  unmount: () => Promise<void>;
  close: () => Promise<void>;
  loadModule: <T>(path: string) => Promise<T>;
}

export async function createTranscriptHarness(options: TranscriptHarnessOptions = {}): Promise<TranscriptHarness> {
  const viewportHeight = options.viewportHeight ?? 100_000;
  const rowHeight = options.rowHeight ?? 10;
  const clock = new TranscriptTestClock();
  const resizeNotifications: Array<() => void> = [];
  const observers: Array<{ target: Element; notify: () => void }> = [];

  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.WheelEvent = dom.window.WheelEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  if (options.deterministic) {
    globalThis.requestAnimationFrame = dom.window.requestAnimationFrame = clock.requestAnimationFrame;
    globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame = clock.cancelAnimationFrame;
  }
  globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window) as typeof getComputedStyle;
  class TranscriptResizeObserver {
    constructor(private readonly callback: ResizeObserverCallback) {}
    observe(target: Element) {
      const element = target as HTMLElement;
      const height = element.classList.contains("transcript__row")
        ? rowHeight
        : element.classList.contains("transcript__block")
          ? Math.max(rowHeight, element.querySelectorAll(".transcript__row").length * rowHeight)
        : element.classList.contains("transcript")
          ? viewportHeight
          : element.classList.contains("transcript__header")
            ? rowHeight
            : 0;
      const notify = () => this.callback([{
        target,
        contentRect: { width: 800, height, top: 0, right: 800, bottom: height, left: 0, x: 0, y: 0, toJSON: () => ({}) },
        borderBoxSize: [{ inlineSize: 800, blockSize: height }],
        contentBoxSize: [{ inlineSize: 800, blockSize: height }],
        devicePixelContentBoxSize: [{ inlineSize: 800, blockSize: height }],
      } as unknown as ResizeObserverEntry], this as unknown as ResizeObserver);
      observers.push({ target, notify });
      if (options.deterministic) resizeNotifications.push(notify);
      else queueMicrotask(notify);
    }
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = TranscriptResizeObserver as unknown as typeof ResizeObserver;
  dom.window.ResizeObserver = TranscriptResizeObserver as unknown as typeof ResizeObserver;
  Object.defineProperty(dom.window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true, // prefers-reduced-motion: keep visual transitions out of the assertions
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  });
  const storage = new Map<string, string>(Object.entries(options.storage ?? {}));
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => void storage.set(key, value),
      removeItem: (key: string) => void storage.delete(key),
      clear: () => storage.clear(),
      key: () => null,
      length: 0,
    },
  });

  const proto = dom.window.HTMLElement.prototype;
  const heightOf = (element: HTMLElement): number => {
    if (element.classList.contains("transcript")) return viewportHeight;
    if (element.classList.contains("transcript__window")) return Number.parseFloat(element.style.height) || 0;
    if (element.classList.contains("transcript__block")) return Math.max(rowHeight, element.querySelectorAll(".transcript__row").length * rowHeight);
    if (element.classList.contains("transcript__row")) return rowHeight;
    return Array.from(element.children).reduce((height, child) => height + ((child as HTMLElement).style.position === "absolute" ? 0 : heightOf(child as HTMLElement)), 0);
  };
  const topOf = (element: HTMLElement): number => {
    if (element.classList.contains("transcript") || !element.parentElement) return 0;
    const parent = element.parentElement;
    const parentTop = topOf(parent) - (parent.classList.contains("transcript") ? parent.scrollTop : 0);
    if (element.style.position === "absolute") return parentTop + (Number.parseFloat(element.style.top) || 0);
    let top = parentTop;
    for (const sibling of parent.children) {
      if (sibling === element) break;
      if ((sibling as HTMLElement).style.position !== "absolute") top += heightOf(sibling as HTMLElement);
    }
    return top;
  };
  proto.getBoundingClientRect = function () {
    const top = topOf(this), height = heightOf(this);
    return { top, bottom: top + height, left: 0, right: 800, width: 800, height, x: 0, y: top, toJSON: () => ({}) };
  };
  Object.defineProperty(proto, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(proto, "detachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(proto, "offsetHeight", {
    configurable: true,
    get(this: HTMLElement) {
      if (this.classList.contains("transcript")) return viewportHeight;
      if (this.classList.contains("transcript__row")) return rowHeight;
      if (this.classList.contains("transcript__block")) return Math.max(rowHeight, this.querySelectorAll(".transcript__row").length * rowHeight);
      return 0;
    },
  });
  Object.defineProperty(proto, "offsetWidth", {
    configurable: true,
    get() {
      return 800;
    },
  });
  Object.defineProperty(proto, "clientHeight", {
    configurable: true,
    get(this: HTMLElement) {
      if (this.classList.contains("transcript")) return viewportHeight;
      return 0;
    },
  });
  Object.defineProperty(proto, "clientWidth", {
    configurable: true,
    get(this: HTMLElement) {
      if (this.classList.contains("transcript")) return 800;
      return 0;
    },
  });
  Object.defineProperty(proto, "scrollHeight", {
    configurable: true,
    get(this: HTMLElement) {
      if (this.classList.contains("transcript")) {
        const window = this.querySelector<HTMLElement>(".transcript__window");
        const windowHeight = Number.parseFloat(window?.style.height || "0");
        const residentRows = Array.from(this.querySelectorAll(".transcript__resident-tail .transcript__row"))
          .filter((row) => !row.closest(".transcript__window-item")).length;
        return Math.max(0, windowHeight + residentRows * rowHeight);
      }
      return 0;
    },
  });
  // Keep generic element scroll methods available to nested controls. The
  // transcript itself writes through TranscriptViewportWriter.
  (proto as unknown as { scrollTo: (arg?: number | ScrollToOptions) => void }).scrollTo = function (
    this: HTMLElement,
    arg?: number | ScrollToOptions,
  ) {
    const max = Math.max(0, this.scrollHeight - this.clientHeight);
    if (typeof arg === "number") {
      this.scrollTop = Math.max(0, Math.min(max, arg));
    } else if (arg && typeof arg.top === "number") {
      this.scrollTop = Math.max(0, Math.min(max, arg.top));
    }
  };
  (proto as unknown as { scrollBy: (arg?: number | ScrollToOptions) => void }).scrollBy = function (
    this: HTMLElement,
    arg?: number | ScrollToOptions,
  ) {
    const max = Math.max(0, this.scrollHeight - this.clientHeight);
    if (typeof arg === "number") {
      this.scrollTop = Math.max(0, Math.min(max, this.scrollTop + arg));
    } else if (arg && typeof arg.top === "number") {
      this.scrollTop = Math.max(0, Math.min(max, this.scrollTop + arg.top));
    }
  };

  const server = await createServer({
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
  if (options.reasoningDisplayMode) {
    const preference = await server.ssrLoadModule("/src/lib/reasoningDisplayPreference.ts") as {
      hydrateReasoningDisplayMode: (mode: unknown, explicit: boolean) => void;
    };
    preference.hydrateReasoningDisplayMode(options.reasoningDisplayMode, true);
  }
  const { TranscriptTestSurface } = await server.ssrLoadModule("/src/__tests__/transcript-test-surface.tsx");
  const { LocaleProvider } = await server.ssrLoadModule("/src/lib/i18n.tsx");
  const TranscriptComponent = TranscriptTestSurface as React.ComponentType<Record<string, unknown>>;
  const Locale = LocaleProvider as React.ComponentType<{ children?: React.ReactNode }>;

  const container = dom.window.document.getElementById("root")!;
  let root: Root | null = createRoot(container);

  const flush = async () => {
    await act(async () => {
      if (options.deterministic) {
        resizeNotifications.splice(0).forEach((notify) => notify());
        clock.flushFrames();
        await new Promise<void>((resolve) => setImmediate(resolve));
      } else await new Promise((resolve) => setTimeout(resolve, 30));
    });
  };

  // Drain lazy Markdown work, ResizeObserver delivery, and the kernel's
  // coalesced tail frame before a test takes manual control of the viewport.
  const settle = async () => {
    for (let i = 0; i < 8; i += 1) {
      await flush();
    }
  };

  // Each attempt costs one 30ms flush, so the default is a three-second budget,
  // not eight tries. It returns the moment the condition holds, so a generous
  // cap is free on the happy path and only makes a genuine failure slower —
  // which beats failing a correct test on a loaded runner.
  const waitFor = async (condition: () => boolean, description: string, attempts = 100) => {
    for (let i = 0; i < attempts; i += 1) {
      if (condition()) return;
      await flush();
    }
    if (!condition()) throw new Error(`timed out waiting for ${description}`);
  };

  return {
    clock,
    resizeNotifications,
    observers,
    dom,
    container,
    server,
    scrollElement: () => {
      const el = container.querySelector<HTMLElement>(".transcript");
      if (!el) throw new Error("transcript scroll element not mounted");
      return el;
    },
    render: async (items, props = {}) => {
      await act(async () => {
        root!.render(
          React.createElement(
            Locale,
            null,
            React.createElement(TranscriptComponent, {
              items,
              onPrompt: () => {},
              questionNavigator: false,
              viewportHeight,
              rowHeight,
              kernelClock: options.deterministic ? clock : undefined,
              ...props,
            }),
          ),
        );
      });
      // Lazy Markdown and window measurement can schedule a second commit.
      await flush();
      await flush();
    },
    flush,
    settle,
    waitFor,
    unmount: async () => {
      const current = root;
      root = null;
      await act(async () => current?.unmount());
    },
    close: async () => {
      // React.lazy Markdown chunks may resolve just after the last act() in a
      // block. Let those requests settle before tearing down Vite's SSR
      // module runner; otherwise the runner reports a transport disconnect
      // even though every assertion completed.
      await new Promise((resolve) => setTimeout(resolve, 100));
      await server.close();
    },
    loadModule: <T,>(path: string) => server.ssrLoadModule(path) as Promise<T>,
  };
}

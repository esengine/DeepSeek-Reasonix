// Shared jsdom harness for Transcript rendering tests. The transcript is a
// virtual list, so tests need controllable layout metrics: the scroll
// container reports `viewportHeight`, each mounted row reports `rowHeight`,
// and scrollHeight follows the sizer's total height (written by the
// virtualizer's direct DOM updates). Not a test file itself — the runner only
// discovers *.test.*.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createServer, type ViteDevServer } from "vite";
import type { Item } from "../lib/useController";

export interface TranscriptHarnessOptions {
  /** Viewport height of the scroll container. Default huge: every row mounts. */
  viewportHeight?: number;
  /** Fixed measured height for every transcript row. */
  rowHeight?: number;
  /** Extra localStorage seed values (display mode, fold preference, …). */
  storage?: Record<string, string>;
}

export interface TranscriptHarness {
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

class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

export async function createTranscriptHarness(options: TranscriptHarnessOptions = {}): Promise<TranscriptHarness> {
  const viewportHeight = options.viewportHeight ?? 100_000;
  const rowHeight = options.rowHeight ?? 10;

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
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window) as typeof getComputedStyle;
  globalThis.ResizeObserver = NoopResizeObserver as unknown as typeof ResizeObserver;
  dom.window.ResizeObserver = NoopResizeObserver as unknown as typeof ResizeObserver;
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
  Object.defineProperty(proto, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(proto, "detachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(proto, "offsetHeight", {
    configurable: true,
    get(this: HTMLElement) {
      if (this.classList.contains("transcript")) return viewportHeight;
      if (this.classList.contains("transcript__row")) return rowHeight;
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
  Object.defineProperty(proto, "scrollHeight", {
    configurable: true,
    get(this: HTMLElement) {
      if (this.classList.contains("transcript")) {
        // The virtualizer writes the total row height onto the sizer.
        const sizer = this.querySelector<HTMLElement>(".transcript__virtual-sizer");
        const height = sizer ? Number.parseFloat(sizer.style.height) : 0;
        return Number.isFinite(height) ? height : 0;
      }
      return 0;
    },
  });
  // jsdom does not implement Element.scrollTo; the virtualizer scrolls through it.
  (proto as unknown as { scrollTo: (arg?: number | ScrollToOptions) => void }).scrollTo = function (
    this: HTMLElement,
    arg?: number | ScrollToOptions,
  ) {
    if (typeof arg === "number") {
      this.scrollTop = arg;
    } else if (arg && typeof arg.top === "number") {
      this.scrollTop = arg.top;
    }
  };

  const server = await createServer({
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
  const { Transcript } = await server.ssrLoadModule("/src/components/Transcript.tsx");
  const { LocaleProvider } = await server.ssrLoadModule("/src/lib/i18n.tsx");
  const TranscriptComponent = Transcript as React.ComponentType<Record<string, unknown>>;
  const Locale = LocaleProvider as React.ComponentType<{ children?: React.ReactNode }>;

  const container = dom.window.document.getElementById("root")!;
  let root: Root | null = createRoot(container);

  const flush = async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30));
    });
  };

  // Initial mount schedules several bottom-pin animation frames
  // (scrollToBottomAfterLayout snaps once per frame for five frames); flush
  // generously so tests can take manual control of the scroll position
  // afterwards. Stability alone is not a drain signal — pending frames re-snap
  // to the same value.
  const settle = async () => {
    for (let i = 0; i < 8; i += 1) {
      await flush();
    }
  };

  const waitFor = async (condition: () => boolean, description: string, attempts = 8) => {
    for (let i = 0; i < attempts; i += 1) {
      if (condition()) return;
      await flush();
    }
    if (!condition()) throw new Error(`timed out waiting for ${description}`);
  };

  return {
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
            React.createElement(TranscriptComponent, { items, onPrompt: () => {}, questionNavigator: false, ...props }),
          ),
        );
      });
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
      await server.close();
    },
    loadModule: <T,>(path: string) => server.ssrLoadModule(path) as Promise<T>,
  };
}

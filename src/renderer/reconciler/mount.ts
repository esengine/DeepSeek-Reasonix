import { type ReactNode, createElement } from "react";
import { type DiffPools, diffFrames } from "../diff/diff-frames.js";
import { type Cursor, type Frame, emptyFrame } from "../diff/frame.js";
import { serializePatches } from "../diff/serialize.js";
import { RendererBridgeContext } from "../ink-compat/renderer-bridge.js";
import { AppContext } from "../ink-compat/use-app.js";
import { ViewportContext } from "../ink-compat/viewport.js";
import { KeystrokeContext, KeystrokeReader, type KeystrokeSource } from "../input/index.js";
import { renderViewport, renderVirtual } from "../layout/layout.js";
import type { LayoutNode } from "../layout/node.js";
import { renderToBytes } from "../runtime/render-to-bytes.js";
import { CursorContext, type CursorTarget } from "./cursor.js";
import { type HostRoot, hostToLayoutNode, reconciler } from "./host-config.js";

export type ScrollMode = "scrollback" | "virtual";

export interface MountOptions {
  readonly viewportWidth: number;
  readonly viewportHeight: number;
  readonly pools: DiffPools;
  readonly write: (bytes: string) => void;
  readonly cursor?: () => Cursor;
  readonly stdin?: KeystrokeSource;
  readonly onExit?: (error?: Error) => void;
  /** "scrollback" (default): rows that overflow viewport go to scrollback, frozen.
   *  "virtual": full layout stays live in memory; PgUp/PgDown/Home/End scroll within. */
  readonly scroll?: ScrollMode;
}

export interface Handle {
  update(element: ReactNode): void;
  resize(width: number, height: number): void;
  emitStatic(element: ReactNode): void;
  destroy(): void;
}

const RESET_SGR = "\x1b[0m";
const CLOSE_HYPERLINK = "\x1b]8;;\x1b\\";
// alt-screen + button-event mouse + SGR coords. Shift+drag bypasses for native selection.
const ENTER_VIRTUAL = "\x1b[?1049h\x1b[2J\x1b[H\x1b[?1002h\x1b[?1006h";
const LEAVE_VIRTUAL = "\x1b[?1006l\x1b[?1002l\x1b[?1000l\x1b[?1049l";

export function mount(element: ReactNode, opts: MountOptions): Handle {
  const scrollMode: ScrollMode = opts.scroll ?? "scrollback";
  let viewportWidth = opts.viewportWidth;
  let viewportHeight = opts.viewportHeight;
  let frame: Frame = emptyFrame(viewportWidth, viewportHeight);
  let reservedRows = 0;
  let promotedRows = 0;
  let scrollOffset = 0;
  let stickyBottom = true;
  let lastTotalRows = 0;
  let destroyed = false;
  let lastElement: ReactNode = element;

  const reader = opts.stdin ? new KeystrokeReader({ source: opts.stdin }) : null;

  let cursorTarget: CursorTarget | null = null;
  const setCursorTarget = (next: CursorTarget | null): void => {
    cursorTarget = next;
    if (!destroyed && root.children.length > 0) root.onCommit();
  };

  const computeCursor = (screenHeight: number): Cursor => {
    if (cursorTarget) {
      const rowFromBottom = Math.max(0, cursorTarget.rowFromBottom ?? 0);
      const y = Math.max(0, screenHeight - 1 - rowFromBottom);
      return {
        x: Math.max(0, cursorTarget.col),
        y,
        visible: cursorTarget.visible !== false,
      };
    }
    if (opts.cursor) return opts.cursor();
    return { x: 0, y: screenHeight, visible: true };
  };

  const scrollBy = (delta: number): void => {
    if (scrollMode !== "virtual") return;
    const window = Math.max(1, viewportHeight - 1);
    const maxOffset = Math.max(0, lastTotalRows - window);
    const next = Math.max(0, Math.min(maxOffset, scrollOffset + delta));
    if (next === scrollOffset) return;
    scrollOffset = next;
    stickyBottom = next === maxOffset;
    if (root.children.length > 0) {
      root.onCommit();
    }
  };

  let exitCleanup: (() => void) | null = null;
  let signalCleanup: (() => void) | null = null;
  if (scrollMode === "virtual") {
    opts.write(ENTER_VIRTUAL);
    if (reader) {
      reader.subscribe((k) => {
        const window = Math.max(1, viewportHeight - 1);
        if (k.pageUp) scrollBy(-window);
        else if (k.pageDown) scrollBy(window);
        else if (k.home) scrollBy(-Number.MAX_SAFE_INTEGER);
        else if (k.end) scrollBy(Number.MAX_SAFE_INTEGER);
        else if (k.wheelUp) scrollBy(-3);
        else if (k.wheelDown) scrollBy(3);
      });
    }
    if (typeof process !== "undefined" && process.on) {
      exitCleanup = () => {
        try {
          opts.write(LEAVE_VIRTUAL);
        } catch {
          /* stdout already closed */
        }
      };
      signalCleanup = () => process.exit(130);
      process.on("exit", exitCleanup);
      process.on("SIGINT", signalCleanup);
      process.on("SIGTERM", signalCleanup);
    }
  }

  const commitScrollback = (layout: LayoutNode): void => {
    const maxHeight = Math.max(1, viewportHeight - 1);
    const split = renderViewport(layout, viewportWidth, opts.pools, maxHeight);
    const newPromote = Math.max(0, split.skipped - promotedRows);
    if (newPromote > 0) {
      if (frame.screen.height > 0) {
        opts.write(`\r\x1b[${frame.screen.height}A\x1b[J`);
      } else if (reservedRows === 0) {
        opts.write("\r\x1b[J");
      }
      opts.write(split.serializePromoted(promotedRows, split.skipped));
      promotedRows = split.skipped;
      frame = emptyFrame(viewportWidth, viewportHeight);
      reservedRows = 0;
    }
    const screen = split.screen;
    const targetReserve = Math.min(screen.height, Math.max(0, viewportHeight - 1));
    if (targetReserve > reservedRows) {
      const delta = targetReserve - reservedRows;
      let prelude = "";
      if (reservedRows === 0) prelude += "\r";
      prelude += `${"\n".repeat(delta)}\x1b[${delta}A`;
      opts.write(prelude);
      reservedRows = targetReserve;
    }
    const next: Frame = {
      screen,
      viewportWidth,
      viewportHeight,
      cursor: computeCursor(screen.height),
    };
    const patches = diffFrames(frame, next, opts.pools);
    if (patches.length > 0) opts.write(serializePatches(patches));
    frame = next;
  };

  const commitVirtual = (layout: LayoutNode): void => {
    const window = Math.max(1, viewportHeight - 1);
    const virt = renderVirtual(layout, viewportWidth, opts.pools);
    const maxOffset = Math.max(0, virt.totalRows - window);
    if (stickyBottom || virt.totalRows < lastTotalRows) {
      scrollOffset = maxOffset;
    } else {
      scrollOffset = Math.min(scrollOffset, maxOffset);
    }
    lastTotalRows = virt.totalRows;
    const screen = virt.windowAt(scrollOffset, window);
    const targetReserve = Math.min(window, Math.max(0, viewportHeight - 1));
    if (targetReserve > reservedRows) {
      const delta = targetReserve - reservedRows;
      let prelude = "";
      if (reservedRows === 0) prelude += "\r";
      prelude += `${"\n".repeat(delta)}\x1b[${delta}A`;
      opts.write(prelude);
      reservedRows = targetReserve;
    }
    const next: Frame = {
      screen,
      viewportWidth,
      viewportHeight,
      cursor: computeCursor(screen.height),
    };
    const patches = diffFrames(frame, next, opts.pools);
    if (patches.length > 0) opts.write(serializePatches(patches));
    frame = next;
  };

  const root: HostRoot = {
    children: [],
    onCommit: () => {
      if (destroyed) return;
      const layout = collectRootLayout(root.children);
      if (scrollMode === "virtual") {
        commitVirtual(layout);
      } else {
        commitScrollback(layout);
      }
    },
  };

  const container = reconciler.createContainer(
    root,
    0,
    null,
    false,
    null,
    "rsx",
    () => {
      /* recoverable error */
    },
    null,
  );

  const emitStatic = (node: ReactNode): void => {
    if (destroyed) return;
    const bytes = renderToBytes(node, viewportWidth, opts.pools);
    if (bytes.length === 0) return;
    if (frame.screen.height > 0) {
      opts.write(`\r\x1b[${frame.screen.height}A\x1b[J`);
    } else {
      opts.write("\r\x1b[J");
    }
    opts.write(bytes);
    frame = emptyFrame(viewportWidth, viewportHeight);
    root.onCommit();
  };

  const bridge = { emitStatic };

  const appApi = {
    exit: (error?: Error): void => {
      if (destroyed) return;
      queueMicrotask(() => opts.onExit?.(error));
    },
  };

  const wrap = (node: ReactNode): ReactNode => {
    const withApp: ReactNode = createElement(AppContext.Provider, { value: appApi }, node);
    const withBridge: ReactNode = createElement(
      RendererBridgeContext.Provider,
      { value: bridge },
      withApp,
    );
    const withViewport: ReactNode = createElement(
      ViewportContext.Provider,
      { value: { columns: viewportWidth, rows: viewportHeight } },
      withBridge,
    );
    const withCursor: ReactNode = createElement(
      CursorContext.Provider,
      { value: setCursorTarget },
      withViewport,
    );
    return reader
      ? createElement(KeystrokeContext.Provider, { value: reader }, withCursor)
      : withCursor;
  };

  reconciler.updateContainer(wrap(element), container, null, () => {
    /* committed */
  });

  return {
    update(nextElement: ReactNode): void {
      if (destroyed) return;
      lastElement = nextElement;
      reconciler.updateContainer(wrap(nextElement), container, null, () => {
        /* committed */
      });
    },
    resize(width: number, height: number): void {
      if (destroyed) return;
      viewportWidth = width;
      viewportHeight = height;
      frame = emptyFrame(width, height);
      reservedRows = 0;
      promotedRows = 0;
      scrollOffset = 0;
      stickyBottom = true;
      lastTotalRows = 0;
      opts.write("\x1b[2J\x1b[H");
      reconciler.updateContainer(wrap(lastElement), container, null, () => {
        /* committed */
      });
    },
    emitStatic,
    destroy(): void {
      if (destroyed) return;
      destroyed = true;
      reader?.destroy();
      reconciler.updateContainer(null, container, null, () => {
        /* committed */
      });
      const teardown = scrollMode === "virtual" ? LEAVE_VIRTUAL : "";
      opts.write(`${teardown}${RESET_SGR}${CLOSE_HYPERLINK}`);
      if (exitCleanup && typeof process !== "undefined" && process.off) {
        process.off("exit", exitCleanup);
        exitCleanup = null;
      }
      if (signalCleanup && typeof process !== "undefined" && process.off) {
        process.off("SIGINT", signalCleanup);
        process.off("SIGTERM", signalCleanup);
        signalCleanup = null;
      }
    },
  };
}

function collectRootLayout(children: ReadonlyArray<unknown>): LayoutNode {
  const layoutChildren: LayoutNode[] = [];
  for (const c of children) {
    const child = hostToLayoutNode(c as Parameters<typeof hostToLayoutNode>[0]);
    if (child) layoutChildren.push(child);
  }
  if (layoutChildren.length === 1) return layoutChildren[0]!;
  return { kind: "box", children: layoutChildren };
}

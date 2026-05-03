import { type ReactNode, createElement } from "react";
import { type DiffPools, diffFrames } from "../diff/diff-frames.js";
import { type Cursor, type Frame, emptyFrame } from "../diff/frame.js";
import { serializePatches } from "../diff/serialize.js";
import { RendererBridgeContext } from "../ink-compat/renderer-bridge.js";
import { AppContext } from "../ink-compat/use-app.js";
import { ViewportContext } from "../ink-compat/viewport.js";
import { KeystrokeContext, KeystrokeReader, type KeystrokeSource } from "../input/index.js";
import { renderViewport } from "../layout/layout.js";
import type { LayoutNode } from "../layout/node.js";
import { renderToBytes } from "../runtime/render-to-bytes.js";
import { type HostRoot, hostToLayoutNode, reconciler } from "./host-config.js";

export interface MountOptions {
  readonly viewportWidth: number;
  readonly viewportHeight: number;
  readonly pools: DiffPools;
  readonly write: (bytes: string) => void;
  readonly cursor?: () => Cursor;
  readonly stdin?: KeystrokeSource;
  readonly onExit?: (error?: Error) => void;
}

export interface Handle {
  update(element: ReactNode): void;
  resize(width: number, height: number): void;
  emitStatic(element: ReactNode): void;
  destroy(): void;
}

const RESET_SGR = "\x1b[0m";
const CLOSE_HYPERLINK = "\x1b]8;;\x1b\\";

export function mount(element: ReactNode, opts: MountOptions): Handle {
  let viewportWidth = opts.viewportWidth;
  let viewportHeight = opts.viewportHeight;
  let frame: Frame = emptyFrame(viewportWidth, viewportHeight);
  let reservedRows = 0;
  let promotedRows = 0;
  let destroyed = false;
  let lastElement: ReactNode = element;

  const reader = opts.stdin ? new KeystrokeReader({ source: opts.stdin }) : null;

  const root: HostRoot = {
    children: [],
    onCommit: () => {
      if (destroyed) return;
      const layout = collectRootLayout(root.children);
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
        cursor: opts.cursor?.() ?? { x: 0, y: screen.height, visible: true },
      };
      const patches = diffFrames(frame, next, opts.pools);
      if (patches.length > 0) opts.write(serializePatches(patches));
      frame = next;
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
    return reader
      ? createElement(KeystrokeContext.Provider, { value: reader }, withViewport)
      : withViewport;
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
      opts.write(`${RESET_SGR}${CLOSE_HYPERLINK}`);
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

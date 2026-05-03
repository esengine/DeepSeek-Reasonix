import type { ReactNode } from "react";
import { type DiffPools, diffFrames } from "../diff/diff-frames.js";
import { type Cursor, type Frame, emptyFrame } from "../diff/frame.js";
import { serializePatches } from "../diff/serialize.js";
import { renderToScreen } from "../layout/layout.js";
import type { LayoutNode } from "../layout/node.js";
import { type HostRoot, hostToLayoutNode, reconciler } from "./host-config.js";

export interface MountOptions {
  readonly viewportWidth: number;
  readonly viewportHeight: number;
  readonly pools: DiffPools;
  readonly write: (bytes: string) => void;
  readonly cursor?: () => Cursor;
}

export interface Handle {
  update(element: ReactNode): void;
  resize(width: number, height: number): void;
  destroy(): void;
}

const RESET_SGR = "\x1b[0m";
const CLOSE_HYPERLINK = "\x1b]8;;\x1b\\";

export function mount(element: ReactNode, opts: MountOptions): Handle {
  let viewportWidth = opts.viewportWidth;
  let viewportHeight = opts.viewportHeight;
  let frame: Frame = emptyFrame(viewportWidth, viewportHeight);
  let destroyed = false;

  const root: HostRoot = {
    children: [],
    onCommit: () => {
      if (destroyed) return;
      const layout = collectRootLayout(root.children);
      const screen = renderToScreen(layout, viewportWidth, opts.pools);
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

  reconciler.updateContainer(element, container, null, () => {
    /* committed */
  });

  return {
    update(nextElement: ReactNode): void {
      if (destroyed) return;
      reconciler.updateContainer(nextElement, container, null, () => {
        /* committed */
      });
    },
    resize(width: number, height: number): void {
      if (destroyed) return;
      viewportWidth = width;
      viewportHeight = height;
      // Trigger a fresh paint at the new viewport.
      root.onCommit();
    },
    destroy(): void {
      if (destroyed) return;
      destroyed = true;
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

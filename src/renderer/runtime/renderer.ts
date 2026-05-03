import type { ReactNode } from "react";
import { type DiffPools, diffFrames } from "../diff/diff-frames.js";
import { type Cursor, type Frame, emptyFrame } from "../diff/frame.js";
import { serializePatches } from "../diff/serialize.js";
import { render as renderReactTree } from "../react/render.js";

export interface RendererOptions {
  readonly viewportWidth: number;
  readonly viewportHeight: number;
  readonly pools: DiffPools;
  readonly write: (bytes: string) => void;
}

const RESET_SGR = "\x1b[0m";
const CLOSE_HYPERLINK = "\x1b]8;;\x1b\\";

export class Renderer {
  private viewportWidth: number;
  private viewportHeight: number;
  private frame: Frame;
  private destroyed = false;

  constructor(private readonly opts: RendererOptions) {
    this.viewportWidth = opts.viewportWidth;
    this.viewportHeight = opts.viewportHeight;
    this.frame = emptyFrame(this.viewportWidth, this.viewportHeight);
  }

  update(element: ReactNode, cursor?: Cursor): void {
    if (this.destroyed) return;
    const screen = renderReactTree(element, {
      width: this.viewportWidth,
      pools: this.opts.pools,
    });
    const next: Frame = {
      screen,
      viewportWidth: this.viewportWidth,
      viewportHeight: this.viewportHeight,
      cursor: cursor ?? { x: 0, y: screen.height, visible: true },
    };
    const patches = diffFrames(this.frame, next, this.opts.pools);
    if (patches.length > 0) {
      this.opts.write(serializePatches(patches));
    }
    this.frame = next;
  }

  resize(width: number, height: number): void {
    if (this.destroyed) return;
    this.viewportWidth = width;
    this.viewportHeight = height;
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.opts.write(`${RESET_SGR}${CLOSE_HYPERLINK}`);
  }
}

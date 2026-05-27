import type { Cursor } from './cursor.js';
import type { Size } from './layout/geometry.js';
import type { ScrollHint } from './render-node-to-output.js';
import {
  type CharPool,
  createScreen,
  type HyperlinkPool,
  type Screen,
  type StylePool,
} from './screen.js';

/** Immutable snapshot of one rendered frame. */
export type Frame = {
  readonly screen: Screen;
  readonly viewport: Size;
  readonly cursor: Cursor;
  /** DECSTBM scroll-optimisation hint. Set only in alt-screen mode. */
  readonly scrollHint?: ScrollHint | null;
  /** A ScrollBox has remaining `pendingScrollDelta` — schedule one more frame. */
  readonly scrollDrainPending?: boolean;
};

export function emptyFrame(
  rows: number,
  columns: number,
  stylePool: StylePool,
  charPool: CharPool,
  hyperlinkPool: HyperlinkPool,
): Frame {
  return {
    screen: createScreen(0, 0, stylePool, charPool, hyperlinkPool),
    viewport: { width: columns, height: rows },
    cursor: { x: 0, y: 0, visible: true },
  };
}

export type FlickerReason = 'resize' | 'offscreen' | 'clear';

export type FrameEvent = {
  durationMs: number;
  /** Per-phase breakdown. */
  phases?: {
    /** DOM → Yoga → screen buffer pipeline cost. */
    renderer: number;
    /** `LogUpdate.render()` — screen diff to Patch list. */
    diff: number;
    /** `optimize()` — patch merge / dedupe pass. */
    optimize: number;
    /** `writeDiffToTerminal()` — patch list to ANSI to stdout. */
    write: number;
    /** Pre-optimise patch count — proxy for "how much changed". */
    patches: number;
    /** `yoga.calculateLayout()` from `resetAfterCommit`. */
    yoga: number;
    /** React reconcile, `scrollMutated` → `resetAfterCommit`. 0 with no commit. */
    commit: number;
    /** `layoutNode()` recursive call count, including cache hits. */
    yogaVisited: number;
    /** Calls to the expensive text-measure path. */
    yogaMeasured: number;
    /** Hits on the per-node single-slot measurement cache. */
    yogaCacheHits: number;
    /** Live Yoga node count (creates − frees). Sustained growth = leak. */
    yogaLive: number;
  };
  /** Frames where the renderer had to clear because the diff alone wouldn't work. */
  flickers: Array<{
    desiredHeight: number;
    availableHeight: number;
    reason: FlickerReason;
  }>;
};

export type Patch =
  | { type: 'stdout'; content: string }
  | { type: 'clear'; count: number }
  | {
      type: 'clearTerminal';
      reason: FlickerReason;
      /** Set by log-update when a scrollback diff triggered the reset. */
      debug?: { triggerY: number; prevLine: string; nextLine: string };
    }
  | { type: 'cursorHide' }
  | { type: 'cursorShow' }
  | { type: 'cursorMove'; x: number; y: number }
  | { type: 'cursorTo'; col: number }
  | { type: 'carriageReturn' }
  | { type: 'hyperlink'; uri: string }
  | { type: 'styleStr'; str: string };

export type Diff = Patch[];

/** Decide whether the next frame requires a hard clear before patching. */
export function shouldClearScreen(prevFrame: Frame, frame: Frame): FlickerReason | undefined {
  const resized =
    frame.viewport.height !== prevFrame.viewport.height ||
    frame.viewport.width !== prevFrame.viewport.width;
  if (resized) return 'resize';

  const overflowsNow = frame.screen.height >= frame.viewport.height;
  const overflowedBefore = prevFrame.screen.height >= prevFrame.viewport.height;
  if (overflowsNow || overflowedBefore) return 'offscreen';

  return undefined;
}

import noop from 'lodash-es/noop.js';
import type { ReactElement } from 'react';
import { LegacyRoot } from 'react-reconciler/constants.js';
import { logForDebugging } from './_internal/debug.js';
import { createNode, type DOMElement } from './dom.js';
import { FocusManager } from './focus.js';
import Output from './output.js';
import reconciler from './reconciler.js';
import renderNodeToOutput, {
  resetLayoutShifted,
} from './render-node-to-output.js';
import {
  CellWidth,
  CharPool,
  cellAtIndex,
  createScreen,
  HyperlinkPool,
  type Screen,
  StylePool,
  setCellStyleId,
} from './screen.js';

export type MatchPosition = {
  row: number;
  col: number;
  /** Number of CELLS the match spans. */
  len: number;
};

// State shared across calls. The pools intern style/char tables, so reusing
// them means later calls benefit from earlier interns. Reusing the root
// and container saves the createContainer cost (~1ms per call). LegacyRoot
// is chosen on purpose: all work runs synchronously with no scheduler, so
// there's no cross-root backlog leaking through flushSyncWork the way
// ConcurrentRoot would.
let root: DOMElement | undefined;
let container: ReturnType<typeof reconciler.createContainer> | undefined;
let stylePool: StylePool | undefined;
let charPool: CharPool | undefined;
let hyperlinkPool: HyperlinkPool | undefined;
let output: Output | undefined;

const timing = { reconcile: 0, yoga: 0, paint: 0, scan: 0, calls: 0 };
const LOG_EVERY = 20;

/** Render a React element into an isolated Screen at the given width. */
export function renderToScreen(
  el: ReactElement,
  width: number,
): { screen: Screen; height: number } {
  if (!root) {
    root = createNode('ink-root');
    root.focusManager = new FocusManager(() => false);
    stylePool = new StylePool();
    charPool = new CharPool();
    hyperlinkPool = new HyperlinkPool();
    container = reconciler.createContainer(
      root,
      LegacyRoot,
      null,
      false,
      null,
      'search-render',
      noop,
      noop,
      noop,
      noop,
    );
  }

  const t0 = performance.now();
  reconciler.updateContainerSync(el, container, null, noop);
  reconciler.flushSyncWork();
  const t1 = performance.now();

  // Yoga pass. An empty tree leaves the root without a yogaNode — height 0.
  root.yogaNode?.setWidth(width);
  root.yogaNode?.calculateLayout(width);
  const height = Math.ceil(root.yogaNode?.getComputedHeight() ?? 0);
  const t2 = performance.now();

  // Paint into a fresh Screen at the natural height. No alt-screen, no
  // prevScreen — every call starts from blank.
  const screen = createScreen(
    width,
    // createScreen may not tolerate a 0-height buffer, so floor at 1.
    Math.max(1, height),
    stylePool!,
    charPool!,
    hyperlinkPool!,
  );
  if (!output) {
    output = new Output({ width, height, stylePool: stylePool!, screen });
  } else {
    output.reset(width, height, screen);
  }
  resetLayoutShifted();
  renderNodeToOutput(root, output, { prevScreen: undefined });
  // renderNodeToOutput stages writes inside Output; .get() flushes them
  // into the Screen's cell arrays. Without this the buffer is still
  // constructor-zero and the caller scans blank cells.
  const rendered = output.get();
  const t3 = performance.now();

  // Unmount so the next call gets a clean tree. Root/container/pools persist.
  reconciler.updateContainerSync(null, container, null, noop);
  reconciler.flushSyncWork();

  timing.reconcile += t1 - t0;
  timing.yoga += t2 - t1;
  timing.paint += t3 - t2;
  if (++timing.calls % LOG_EVERY === 0) {
    const total = timing.reconcile + timing.yoga + timing.paint + timing.scan;
    logForDebugging(
      `renderToScreen: ${timing.calls} calls · ` +
        `reconcile=${timing.reconcile.toFixed(1)}ms yoga=${timing.yoga.toFixed(1)}ms ` +
        `paint=${timing.paint.toFixed(1)}ms scan=${timing.scan.toFixed(1)}ms · ` +
        `total=${total.toFixed(1)}ms · avg ${(total / timing.calls).toFixed(2)}ms/call`,
    );
  }

  return { screen: rendered, height };
}

/** Find every occurrence of `query` in a Screen buffer. */
export function scanPositions(screen: Screen, query: string): MatchPosition[] {
  const lq = query.toLowerCase();
  if (!lq) return [];
  const qlen = lq.length;
  const w = screen.width;
  const h = screen.height;
  const noSelect = screen.noSelect;
  const positions: MatchPosition[] = [];

  const t0 = performance.now();
  for (let row = 0; row < h; row++) {
    const rowOff = row * w;
    // Rebuild the searchable text for this row and remember the cell
    // column each lowercased code unit came from. We need a two-level
    // map because `text.length` and `colOf.length` diverge whenever a
    // cell contributes more or fewer code units than 1 to the lowercased
    // text — surrogate pairs, ligature cells, multi-unit lowercase.
    let text = '';
    const colOf: number[] = [];
    const codeUnitToCell: number[] = [];
    for (let col = 0; col < w; col++) {
      const idx = rowOff + col;
      const cell = cellAtIndex(screen, idx);
      if (
        cell.width === CellWidth.SpacerTail ||
        cell.width === CellWidth.SpacerHead ||
        noSelect[idx] === 1
      ) {
        continue;
      }
      const lc = cell.char.toLowerCase();
      const cellIdx = colOf.length;
      for (let i = 0; i < lc.length; i++) {
        codeUnitToCell.push(cellIdx);
      }
      text += lc;
      colOf.push(col);
    }
    // Non-overlapping matches, advancing by qlen on each hit — matches
    // applySearchHighlight's behaviour so the two stay in sync.
    let pos = text.indexOf(lq);
    while (pos >= 0) {
      const startCi = codeUnitToCell[pos]!;
      const endCi = codeUnitToCell[pos + qlen - 1]!;
      const col = colOf[startCi]!;
      const endCol = colOf[endCi]! + 1;
      positions.push({ row, col, len: endCol - col });
      pos = text.indexOf(lq, pos + qlen);
    }
  }
  timing.scan += performance.now() - t0;

  return positions;
}

/** Highlight the "currently selected" search match by upgrading its style. */
export function applyPositionedHighlight(
  screen: Screen,
  stylePool: StylePool,
  positions: MatchPosition[],
  rowOffset: number,
  currentIdx: number,
): boolean {
  if (currentIdx < 0 || currentIdx >= positions.length) return false;
  const p = positions[currentIdx]!;
  const row = p.row + rowOffset;
  if (row < 0 || row >= screen.height) return false;
  const transform = (id: number) => stylePool.withCurrentMatch(id);
  const rowOff = row * screen.width;
  for (let col = p.col; col < p.col + p.len; col++) {
    if (col < 0 || col >= screen.width) continue;
    const cell = cellAtIndex(screen, rowOff + col);
    setCellStyleId(screen, col, row, transform(cell.styleId));
  }
  return true;
}

import type { CharPool } from "../pools/char-pool.js";
import type { HyperlinkPool } from "../pools/hyperlink-pool.js";
import type { StylePool } from "../pools/style-pool.js";
import { type Cell, CellWidth } from "../screen/cell.js";
import { diffEach } from "../screen/diff.js";
import type { Frame } from "./frame.js";
import type { Patch } from "./patch.js";

export interface DiffPools {
  readonly char: CharPool;
  readonly style: StylePool;
  readonly hyperlink: HyperlinkPool;
}

interface CursorState {
  x: number;
  y: number;
  styleId: number;
  hyperlinkId: number;
}

export function diffFrames(prev: Frame, next: Frame, pools: DiffPools): Patch[] {
  if (prev.viewportWidth !== next.viewportWidth || prev.viewportHeight !== next.viewportHeight) {
    return fullReset(next, pools);
  }

  const out: Patch[] = [];
  const cursor: CursorState = {
    x: prev.cursor.x,
    y: prev.cursor.y,
    styleId: pools.style.none,
    hyperlinkId: 0,
  };

  diffEach(prev.screen, next.screen, (x, y, _prevCell, nextCell) => {
    moveTo(out, cursor, x, y, next.viewportWidth);
    writeCell(out, cursor, nextCell, pools);
    return undefined;
  });

  appendTrailClears(out, cursor, prev, next);

  if (next.cursor.x !== cursor.x || next.cursor.y !== cursor.y) {
    moveTo(out, cursor, next.cursor.x, next.cursor.y, next.viewportWidth);
  }

  if (prev.cursor.visible !== next.cursor.visible) {
    out.push({ type: "cursorVisible", visible: next.cursor.visible });
  }

  resetTrailingState(out, cursor, pools);
  return out;
}

function fullReset(next: Frame, pools: DiffPools): Patch[] {
  const out: Patch[] = [{ type: "clearTerminal" }];
  const cursor: CursorState = { x: 0, y: 0, styleId: pools.style.none, hyperlinkId: 0 };
  for (let y = 0; y < next.screen.height; y++) {
    for (let x = 0; x < next.screen.width; x++) {
      const cell = next.screen.cellAt(x, y);
      if (!cell) continue;
      if (cell.width === CellWidth.SpacerTail) continue;
      moveTo(out, cursor, x, y, next.viewportWidth);
      writeCell(out, cursor, cell, pools);
    }
  }
  resetTrailingState(out, cursor, pools);
  return out;
}

function moveTo(
  out: Patch[],
  cursor: CursorState,
  targetX: number,
  targetY: number,
  viewportWidth: number,
): void {
  if (cursor.x === targetX && cursor.y === targetY) return;

  if (cursor.x >= viewportWidth) {
    out.push({ type: "carriageReturn" });
    cursor.x = 0;
  }

  if (cursor.y !== targetY) {
    const dy = targetY - cursor.y;
    out.push({ type: "carriageReturn" });
    out.push({ type: "cursorMove", dx: 0, dy });
    cursor.x = 0;
    cursor.y = targetY;
  }

  if (cursor.x !== targetX) {
    out.push({ type: "cursorMove", dx: targetX - cursor.x, dy: 0 });
    cursor.x = targetX;
  }
}

function writeCell(
  out: Patch[],
  cursor: CursorState,
  cell: Cell | undefined,
  pools: DiffPools,
): void {
  if (!cell) {
    transitionStyle(out, cursor, pools.style.none, pools);
    transitionHyperlink(out, cursor, 0, pools);
    out.push({ type: "stdout", content: " " });
    cursor.x++;
    return;
  }
  if (cell.width === CellWidth.SpacerTail) return;

  transitionStyle(out, cursor, cell.styleId, pools);
  transitionHyperlink(out, cursor, cell.hyperlinkId, pools);
  const isWide = cell.width === CellWidth.Wide;
  const charStr = pools.char.get(cell.charId);
  if (isWide) {
    // Compensation for terminal-vs-layout width disagreement on East Asian
    // Ambiguous chars: pre-write a space at col+1 (so it's blank on Western
    // terms where the glyph renders only 1 wide), then jump back, write the
    // char, then force the cursor to col+2 regardless of how many cells the
    // terminal actually advanced. Borrowed from Claude Code's logUpdate.
    out.push({ type: "cursorTo", col: cursor.x + 1 });
    out.push({ type: "stdout", content: " " });
    out.push({ type: "cursorTo", col: cursor.x });
    out.push({ type: "stdout", content: charStr });
    cursor.x += 2;
    out.push({ type: "cursorTo", col: cursor.x });
  } else {
    out.push({ type: "stdout", content: charStr });
    cursor.x += 1;
  }
}

function transitionStyle(
  out: Patch[],
  cursor: CursorState,
  targetStyleId: number,
  pools: DiffPools,
): void {
  if (cursor.styleId === targetStyleId) return;
  const str = pools.style.transition(cursor.styleId, targetStyleId);
  if (str.length > 0) out.push({ type: "styleStr", str });
  cursor.styleId = targetStyleId;
}

function transitionHyperlink(
  out: Patch[],
  cursor: CursorState,
  targetId: number,
  pools: DiffPools,
): void {
  if (cursor.hyperlinkId === targetId) return;
  out.push({ type: "hyperlink", uri: pools.hyperlink.get(targetId) ?? "" });
  cursor.hyperlinkId = targetId;
}

function resetTrailingState(out: Patch[], cursor: CursorState, pools: DiffPools): void {
  transitionStyle(out, cursor, pools.style.none, pools);
  transitionHyperlink(out, cursor, 0, pools);
}

/** Per-row trail clear when prev had content past next's last visible cell — defends shrinking rows against terminal-state desync. */
function appendTrailClears(out: Patch[], cursor: CursorState, prev: Frame, next: Frame): void {
  const w = Math.min(prev.screen.width, next.viewportWidth);
  const h = Math.max(prev.screen.height, next.screen.height);
  for (let y = 0; y < h; y++) {
    let lastNextCol = -1;
    for (let x = next.screen.width - 1; x >= 0; x--) {
      const c = next.screen.cellAt(x, y);
      if (c && c.charId !== 0) {
        lastNextCol = x;
        break;
      }
    }
    let prevHasTrail = false;
    for (let x = lastNextCol + 1; x < w; x++) {
      const c = prev.screen.cellAt(x, y);
      if (c && c.charId !== 0) {
        prevHasTrail = true;
        break;
      }
    }
    if (!prevHasTrail) continue;
    moveTo(out, cursor, Math.max(0, lastNextCol + 1), y, next.viewportWidth);
    out.push({ type: "clearToEOL" });
  }
}

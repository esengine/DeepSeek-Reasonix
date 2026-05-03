import stringWidth from "string-width";
import type { CharPool } from "../pools/char-pool.js";
import type { HyperlinkPool } from "../pools/hyperlink-pool.js";
import type { AnsiCode, StylePool } from "../pools/style-pool.js";
import { type Cell, CellWidth } from "../screen/cell.js";
import { Screen } from "../screen/screen.js";
import type { LayoutNode, TextNode } from "./node.js";

export interface RenderPools {
  readonly char: CharPool;
  readonly style: StylePool;
  readonly hyperlink: HyperlinkPool;
}

interface TextRow {
  readonly graphemes: ReadonlyArray<{ glyph: string; width: number }>;
  readonly styleId: number;
  readonly hyperlinkId: number;
}

export function renderToScreen(node: LayoutNode, width: number, pools: RenderPools): Screen {
  const w = Math.max(0, width | 0);
  if (w === 0) return new Screen(0, 0);
  const rows = collectRows(node, w, pools);
  const screen = new Screen(w, rows.length);
  for (let y = 0; y < rows.length; y++) {
    blitRow(screen, rows[y]!, y, pools);
  }
  screen.resetDamage();
  return screen;
}

function collectRows(node: LayoutNode, width: number, pools: RenderPools): TextRow[] {
  const out: TextRow[] = [];
  walk(node, width, pools, out);
  return out;
}

function walk(node: LayoutNode, width: number, pools: RenderPools, out: TextRow[]): void {
  if (node.kind === "text") {
    pushTextRows(node, width, pools, out);
    return;
  }
  for (const child of node.children) walk(child, width, pools, out);
}

function pushTextRows(node: TextNode, width: number, pools: RenderPools, out: TextRow[]): void {
  const styleId = node.style ? pools.style.intern(node.style) : pools.style.none;
  const hyperlinkId = pools.hyperlink.intern(node.hyperlink);
  const segmenter = getSegmenter();
  for (const line of node.content.split("\n")) {
    const wrapped = wrapLine(line, width, segmenter);
    if (wrapped.length === 0) {
      out.push({ graphemes: [], styleId, hyperlinkId });
      continue;
    }
    for (const row of wrapped) {
      out.push({ graphemes: row, styleId, hyperlinkId });
    }
  }
}

function wrapLine(
  line: string,
  width: number,
  segmenter: Intl.Segmenter,
): Array<Array<{ glyph: string; width: number }>> {
  if (width <= 0) return [];
  const rows: Array<Array<{ glyph: string; width: number }>> = [];
  let current: Array<{ glyph: string; width: number }> = [];
  let used = 0;
  for (const seg of segmenter.segment(line)) {
    const glyph = seg.segment;
    const gw = Math.max(0, stringWidth(glyph));
    if (gw === 0) continue;
    if (used + gw > width) {
      if (current.length > 0) rows.push(current);
      current = [];
      used = 0;
      if (gw > width) continue;
    }
    current.push({ glyph, width: gw });
    used += gw;
  }
  if (current.length > 0) rows.push(current);
  return rows;
}

function blitRow(screen: Screen, row: TextRow, y: number, pools: RenderPools): void {
  let x = 0;
  for (const { glyph, width } of row.graphemes) {
    if (x >= screen.width) break;
    const charId = pools.char.intern(glyph);
    const cell: Cell = {
      charId,
      styleId: row.styleId,
      hyperlinkId: row.hyperlinkId,
      width: width === 2 ? CellWidth.Wide : CellWidth.Single,
    };
    screen.writeCell(x, y, cell);
    if (width === 2 && x + 1 < screen.width) {
      screen.writeCell(x + 1, y, {
        charId: 1,
        styleId: row.styleId,
        hyperlinkId: row.hyperlinkId,
        width: CellWidth.SpacerTail,
      });
    }
    x += width;
  }
}

let _segmenter: Intl.Segmenter | undefined;
function getSegmenter(): Intl.Segmenter {
  if (_segmenter) return _segmenter;
  _segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
  return _segmenter;
}

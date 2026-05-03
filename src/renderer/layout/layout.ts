import stringWidth from "string-width";
import type { CharPool } from "../pools/char-pool.js";
import type { HyperlinkPool } from "../pools/hyperlink-pool.js";
import type { AnsiCode, StylePool } from "../pools/style-pool.js";
import { type Cell, CellWidth } from "../screen/cell.js";
import { Screen } from "../screen/screen.js";
import { type BorderStyle, resolveBorderStyle } from "./borders.js";
import type { BoxNode, LayoutNode, TextNode } from "./node.js";

export interface RenderPools {
  readonly char: CharPool;
  readonly style: StylePool;
  readonly hyperlink: HyperlinkPool;
}

interface RowFragment {
  readonly graphemes: ReadonlyArray<{ glyph: string; width: number }>;
  readonly styleId: number;
  readonly hyperlinkId: number;
  readonly leftPad: number;
}

type LayoutRows = RowFragment[][];

interface Laid {
  rows: LayoutRows;
  width: number;
}

export function renderToScreen(node: LayoutNode, width: number, pools: RenderPools): Screen {
  const w = Math.max(0, width | 0);
  if (w === 0) return new Screen(0, 0);
  const laid = layout(node, w, pools);
  const screen = new Screen(w, laid.rows.length);
  for (let y = 0; y < laid.rows.length; y++) {
    for (const frag of laid.rows[y]!) blitFragment(screen, frag, y, pools);
  }
  screen.resetDamage();
  return screen;
}

function layout(node: LayoutNode, availableWidth: number, pools: RenderPools): Laid {
  if (availableWidth <= 0) return { rows: [], width: 0 };
  if (node.kind === "text") return layoutText(node, availableWidth, pools);
  return layoutBox(node, availableWidth, pools);
}

function layoutBox(node: BoxNode, availableWidth: number, pools: RenderPools): Laid {
  const border = resolveBorderStyle(node.borderStyle);
  const useTop = border !== undefined && node.borderTop !== false;
  const useBottom = border !== undefined && node.borderBottom !== false;
  const useLeft = border !== undefined && node.borderLeft !== false;
  const useRight = border !== undefined && node.borderRight !== false;

  const padTop = clampPad(node.paddingTop);
  const padBottom = clampPad(node.paddingBottom);
  const padLeft = clampPad(node.paddingLeft);
  const padRight = clampPad(node.paddingRight);
  const horizBorder = (useLeft ? 1 : 0) + (useRight ? 1 : 0);
  const innerWidth = Math.max(0, availableWidth - horizBorder - padLeft - padRight);

  const inner =
    node.flexDirection === "row"
      ? layoutRow(node, innerWidth, pools)
      : layoutColumn(node, innerWidth, pools);

  const contentRows: LayoutRows = [
    ...blanks(padTop),
    ...shiftFragments(inner.rows, padLeft + (useLeft ? 1 : 0)),
    ...blanks(padBottom),
  ];

  if (useLeft || useRight) {
    const leftStyle = node.borderLeftColor ?? node.borderColor;
    const rightStyle = node.borderRightColor ?? node.borderColor;
    const leftStyleId = leftStyle ? pools.style.intern(leftStyle) : pools.style.none;
    const rightStyleId = rightStyle ? pools.style.intern(rightStyle) : pools.style.none;
    for (let i = 0; i < contentRows.length; i++) {
      const row = contentRows[i]!;
      const next: typeof row = [];
      if (useLeft && border) {
        next.push(makeBorderFragment(border.left, 0, leftStyleId));
      }
      next.push(...row);
      if (useRight && border) {
        next.push(makeBorderFragment(border.right, availableWidth - 1, rightStyleId));
      }
      contentRows[i] = next;
    }
  }

  const allRows: LayoutRows = [];
  if (useTop && border) {
    allRows.push(
      makeBorderEdge(
        border,
        "top",
        availableWidth,
        useLeft,
        useRight,
        node.borderTopColor ?? node.borderColor,
        pools,
      ),
    );
  }
  allRows.push(...contentRows);
  if (useBottom && border) {
    allRows.push(
      makeBorderEdge(
        border,
        "bottom",
        availableWidth,
        useLeft,
        useRight,
        node.borderBottomColor ?? node.borderColor,
        pools,
      ),
    );
  }

  return { rows: allRows, width: availableWidth };
}

function makeBorderFragment(glyph: string, leftPad: number, styleId: number): RowFragment {
  const w = Math.max(1, stringWidth(glyph));
  return {
    graphemes: [{ glyph, width: w }],
    styleId,
    hyperlinkId: 0,
    leftPad,
  };
}

function makeBorderEdge(
  style: BorderStyle,
  side: "top" | "bottom",
  width: number,
  useLeft: boolean,
  useRight: boolean,
  color: ReadonlyArray<AnsiCode> | undefined,
  pools: RenderPools,
): RowFragment[] {
  if (width <= 0) return [];
  const corners =
    side === "top" ? [style.topLeft, style.topRight] : [style.bottomLeft, style.bottomRight];
  const edge = side === "top" ? style.top : style.bottom;
  const styleId = color ? pools.style.intern(color) : pools.style.none;
  const graphemes: Array<{ glyph: string; width: number }> = [];
  for (let x = 0; x < width; x++) {
    let glyph: string;
    if (x === 0 && useLeft) glyph = corners[0]!;
    else if (x === width - 1 && useRight) glyph = corners[1]!;
    else glyph = edge;
    graphemes.push({ glyph, width: Math.max(1, stringWidth(glyph)) });
  }
  return [
    {
      graphemes,
      styleId,
      hyperlinkId: 0,
      leftPad: 0,
    },
  ];
}

function layoutColumn(node: BoxNode, innerWidth: number, pools: RenderPools): Laid {
  const rows: LayoutRows = [];
  for (const child of node.children) {
    const laid = layout(child, innerWidth, pools);
    rows.push(...laid.rows);
  }
  return { rows, width: innerWidth };
}

function layoutRow(node: BoxNode, innerWidth: number, pools: RenderPools): Laid {
  if (innerWidth === 0 || node.children.length === 0) {
    return { rows: [], width: innerWidth };
  }
  const allocations = allocateRowWidths(node.children, innerWidth);
  const childResults = node.children.map((child, i) => layout(child, allocations[i] ?? 0, pools));

  let xOffset = 0;
  const merged: LayoutRows = [];
  for (let i = 0; i < childResults.length; i++) {
    const child = childResults[i]!;
    const allocated = allocations[i] ?? 0;
    for (let y = 0; y < child.rows.length; y++) {
      while (merged.length <= y) merged.push([]);
      for (const frag of child.rows[y]!) {
        merged[y]!.push({ ...frag, leftPad: frag.leftPad + xOffset });
      }
    }
    xOffset += allocated;
  }
  return { rows: merged, width: innerWidth };
}

function allocateRowWidths(children: ReadonlyArray<LayoutNode>, available: number): number[] {
  const intrinsics = children.map((c) => Math.min(intrinsicWidth(c), available));
  const grows = children.map((c) => (c.kind === "box" ? Math.max(0, c.flexGrow ?? 0) : 0));
  const totalIntrinsic = intrinsics.reduce((s, w) => s + w, 0);

  if (totalIntrinsic > available) {
    const scale = available / totalIntrinsic;
    const out = intrinsics.map((w) => Math.floor(w * scale));
    let used = out.reduce((s, w) => s + w, 0);
    for (let i = 0; used < available && i < out.length; i++) {
      out[i]!++;
      used++;
    }
    return out;
  }

  const slack = available - totalIntrinsic;
  const totalGrow = grows.reduce((s, g) => s + g, 0);
  if (slack === 0 || totalGrow === 0) return intrinsics;

  const out = intrinsics.slice();
  let distributed = 0;
  for (let i = 0; i < out.length; i++) {
    if (grows[i]! > 0) {
      const add = Math.floor((slack * grows[i]!) / totalGrow);
      out[i]! += add;
      distributed += add;
    }
  }
  for (let i = 0; distributed < slack && i < out.length; i++) {
    if (grows[i]! > 0) {
      out[i]!++;
      distributed++;
    }
  }
  return out;
}

function intrinsicWidth(node: LayoutNode): number {
  if (node.kind === "text") {
    let max = 0;
    for (const line of node.content.split("\n")) {
      const w = Math.max(0, stringWidth(line));
      if (w > max) max = w;
    }
    return max;
  }
  const padLeft = clampPad(node.paddingLeft);
  const padRight = clampPad(node.paddingRight);
  const border = resolveBorderStyle(node.borderStyle);
  const borderH =
    (border && node.borderLeft !== false ? 1 : 0) + (border && node.borderRight !== false ? 1 : 0);
  if (node.children.length === 0) return padLeft + padRight + borderH;
  if (node.flexDirection === "row") {
    return padLeft + padRight + borderH + node.children.reduce((s, c) => s + intrinsicWidth(c), 0);
  }
  let max = 0;
  for (const c of node.children) {
    const w = intrinsicWidth(c);
    if (w > max) max = w;
  }
  return padLeft + padRight + borderH + max;
}

function layoutText(node: TextNode, width: number, pools: RenderPools): Laid {
  const styleId = node.style ? pools.style.intern(node.style) : pools.style.none;
  const hyperlinkId = pools.hyperlink.intern(node.hyperlink);
  const segmenter = getSegmenter();
  const rows: LayoutRows = [];
  for (const line of node.content.split("\n")) {
    const wrapped = wrapLine(line, width, segmenter);
    if (wrapped.length === 0) {
      rows.push([{ graphemes: [], styleId, hyperlinkId, leftPad: 0 }]);
      continue;
    }
    for (const row of wrapped) {
      rows.push([{ graphemes: row, styleId, hyperlinkId, leftPad: 0 }]);
    }
  }
  return { rows, width };
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

function shiftFragments(rows: LayoutRows, dx: number): LayoutRows {
  if (dx === 0) return rows;
  return rows.map((row) => row.map((frag) => ({ ...frag, leftPad: frag.leftPad + dx })));
}

function blanks(n: number): LayoutRows {
  const out: LayoutRows = [];
  for (let i = 0; i < n; i++) out.push([]);
  return out;
}

function blitFragment(screen: Screen, frag: RowFragment, y: number, pools: RenderPools): void {
  let x = frag.leftPad;
  for (const { glyph, width } of frag.graphemes) {
    if (x >= screen.width) break;
    const charId = pools.char.intern(glyph);
    const cell: Cell = {
      charId,
      styleId: frag.styleId,
      hyperlinkId: frag.hyperlinkId,
      width: width === 2 ? CellWidth.Wide : CellWidth.Single,
    };
    screen.writeCell(x, y, cell);
    if (width === 2 && x + 1 < screen.width) {
      screen.writeCell(x + 1, y, {
        charId: 1,
        styleId: frag.styleId,
        hyperlinkId: frag.hyperlinkId,
        width: CellWidth.SpacerTail,
      });
    }
    x += width;
  }
}

function clampPad(v: number | undefined): number {
  if (v === undefined) return 0;
  if (!Number.isFinite(v)) return 0;
  return Math.max(0, Math.floor(v));
}

let _segmenter: Intl.Segmenter | undefined;
function getSegmenter(): Intl.Segmenter {
  if (_segmenter) return _segmenter;
  _segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
  return _segmenter;
}

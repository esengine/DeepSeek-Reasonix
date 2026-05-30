import chalk from 'chalk';
import cliBoxes, { type Boxes, type BoxStyle } from 'cli-boxes';
import { applyColor } from './colorize.js';
import type { DOMNode } from './dom.js';
import type Output from './output.js';
import { stringWidth } from './stringWidth.js';
import type { Color } from './styles.js';

// ── CJK terminal mitigation ──────────────────────────────

/** True when the system locale (or Windows console code page) biases
 *  ambiguous-width characters (U+2500–U+257F box-drawing) toward double-cell
 *  rendering.  Yoga-layout assumes single-cell, so borders overflow. */
function isCJKLocale(): boolean {
  try {
    return /^(zh|ja|ko)/.test(
      Intl.DateTimeFormat().resolvedOptions().locale,
    );
  } catch {
    return false;
  }
}

/** Map common box-drawing Unicode graphemes to their single-cell ASCII
 *  equivalents.  Only the ranges used by `cli-boxes` (and our custom
 *  dashed style) are covered — anything unrecognised passes through. */
const cjkAsciiEq: Record<string, string> = {
  // Horizontal
  '\u2500': '-', // ─
  '\u2501': '=', // ━
  '\u254C': '-', // ╌ (dashed)
  '\u2550': '=', // ═
  '\u2191': '-', // ↑
  '\u2193': '-', // ↓
  // Vertical
  '\u2502': '|', // │
  '\u2503': '|', // ┃
  '\u254E': '|', // ╎ (dashed)
  '\u2551': '|', // ║
  '\u2190': '|', // ←
  '\u2192': '|', // →
  // Corners
  '\u250C': '+', // ┌
  '\u250F': '+', // ┏
  '\u2510': '+', // ┐
  '\u2513': '+', // ┓
  '\u2514': '+', // └
  '\u2517': '+', // ┗
  '\u2518': '+', // ┘
  '\u251B': '+', // ┛
  '\u251C': '+', // ├
  '\u2524': '+', // ┤
  '\u252C': '+', // ┬
  '\u2534': '+', // ┴
  '\u253C': '+', // ┼
  '\u2552': '+', // ╒
  '\u2553': '+', // ╓
  '\u2554': '+', // ╔
  '\u2555': '+', // ╕
  '\u2556': '+', // ╖
  '\u2557': '+', // ╗
  '\u2558': '+', // ╘
  '\u2559': '+', // ╙
  '\u255A': '+', // ╚
  '\u255B': '+', // ╛
  '\u255C': '+', // ╜
  '\u255D': '+', // ╝
  '\u256D': '+', // ╭
  '\u256E': '+', // ╮
  '\u256F': '+', // ╯
  '\u2570': '+', // ╰
  '\u2196': '+', // ↖
  '\u2197': '+', // ↗
  '\u2198': '+', // ↘
  '\u2199': '+', // ↙
};

// ── Public types and style module ────────────────────────

export type BorderTextOptions = {
  /** Pre-rendered string, may include SGR sequences. */
  content: string;
  position: 'top' | 'bottom';
  align: 'start' | 'end' | 'center';
  /** Offset from the chosen edge, in cells. Only used with start/end align. */
  offset?: number;
};

/** Border presets that aren't in cli-boxes. */
export const CUSTOM_BORDER_STYLES = {
  dashed: {
    top: '╌',
    left: '╎',
    right: '╎',
    bottom: '╌',
    topLeft: ' ',
    topRight: ' ',
    bottomLeft: ' ',
    bottomRight: ' ',
  },
} as const;

export type BorderStyle =
  | keyof Boxes
  | keyof typeof CUSTOM_BORDER_STYLES
  | BoxStyle;

function embedTextInBorder(
  borderLine: string,
  text: string,
  align: 'start' | 'end' | 'center',
  offset: number = 0,
  borderChar: string,
): [before: string, text: string, after: string] {
  const textWidth = stringWidth(text);
  const borderWidth = stringWidth(borderLine);
  const charWidth = stringWidth(borderChar);
  const hasLeftCorner = borderLine[0] !== borderChar;
  const hasRightCorner = borderLine[borderLine.length - 1] !== borderChar;
  const leftCornerWidth = hasLeftCorner ? stringWidth(borderLine[0]) : 0;
  const rightCornerWidth = hasRightCorner
    ? stringWidth(borderLine[borderLine.length - 1])
    : 0;

  if (textWidth >= borderWidth - leftCornerWidth - rightCornerWidth) {
    return ['', text.substring(0, borderWidth / charWidth), ''];
  }

  let posCells: number;
  if (align === 'center') {
    posCells =
      leftCornerWidth +
      Math.floor(
        (borderWidth - leftCornerWidth - rightCornerWidth - textWidth) / 2,
      );
  } else if (align === 'start') {
    posCells = leftCornerWidth + offset;
  } else {
    posCells = borderWidth - textWidth - offset - rightCornerWidth;
  }

  posCells = Math.max(
    leftCornerWidth,
    Math.min(posCells, borderWidth - textWidth - rightCornerWidth),
  );
  const beforeChars = Math.max(
    0,
    Math.floor((posCells - leftCornerWidth) / charWidth),
  );
  const afterChars = Math.max(
    0,
    Math.floor(
      (borderWidth - posCells - textWidth - rightCornerWidth) / charWidth,
    ),
  );
  const before =
    (hasLeftCorner ? borderLine[0] : '') + borderChar.repeat(beforeChars);
  const after =
    borderChar.repeat(afterChars) +
    (hasRightCorner ? borderLine[borderLine.length - 1] : '');
  return [before, text, after];
}

function styleBorderLine(
  line: string,
  color: Color | undefined,
  dim: boolean | undefined,
): string {
  let styled = applyColor(line, color);
  if (dim) {
    styled = chalk.dim(styled);
  }
  return styled;
}

const renderBorder = (
  x: number,
  y: number,
  node: DOMNode,
  output: Output,
): void => {
  if (!node.style.borderStyle) {
    return;
  }

  const width = Math.floor(node.yogaNode!.getComputedWidth());
  const height = Math.floor(node.yogaNode!.getComputedHeight());
  const box =
    typeof node.style.borderStyle === 'string'
      ? (CUSTOM_BORDER_STYLES[
          node.style.borderStyle as keyof typeof CUSTOM_BORDER_STYLES
        ] ?? cliBoxes[node.style.borderStyle as keyof Boxes])
      : node.style.borderStyle;

  // East Asian terminals render box-drawing chars as double-width cells,
  // but yoga-layout assumes single-width — causing overflow and misalignment.
  // Remap to ASCII so every character occupies exactly one column.
  const safe = isCJKLocale()
    ? {
        ...box,
        top: cjkAsciiEq[box.top] ?? box.top,
        bottom: cjkAsciiEq[box.bottom] ?? box.bottom,
        left: cjkAsciiEq[box.left] ?? box.left,
        right: cjkAsciiEq[box.right] ?? box.right,
        topLeft: cjkAsciiEq[box.topLeft] ?? box.topLeft,
        topRight: cjkAsciiEq[box.topRight] ?? box.topRight,
        bottomLeft: cjkAsciiEq[box.bottomLeft] ?? box.bottomLeft,
        bottomRight: cjkAsciiEq[box.bottomRight] ?? box.bottomRight,
      }
    : box;

  // Per-side colour falls back to the catch-all borderColor; same for the
  // dim flag. This mirrors how every CSS-shaped border API behaves.
  const topBorderColor = node.style.borderTopColor ?? node.style.borderColor;
  const bottomBorderColor =
    node.style.borderBottomColor ?? node.style.borderColor;
  const leftBorderColor = node.style.borderLeftColor ?? node.style.borderColor;
  const rightBorderColor =
    node.style.borderRightColor ?? node.style.borderColor;

  const dimTopBorderColor =
    node.style.borderTopDimColor ?? node.style.borderDimColor;
  const dimBottomBorderColor =
    node.style.borderBottomDimColor ?? node.style.borderDimColor;
  const dimLeftBorderColor =
    node.style.borderLeftDimColor ?? node.style.borderDimColor;
  const dimRightBorderColor =
    node.style.borderRightDimColor ?? node.style.borderDimColor;

  const showTopBorder = node.style.borderTop !== false;
  const showBottomBorder = node.style.borderBottom !== false;
  const showLeftBorder = node.style.borderLeft !== false;
  const showRightBorder = node.style.borderRight !== false;

  const contentWidth = Math.max(
    0,
    width - (showLeftBorder ? 1 : 0) - (showRightBorder ? 1 : 0),
  );

  const topBorderLine = showTopBorder
    ? (showLeftBorder ? safe.topLeft : '') +
      safe.top.repeat(contentWidth) +
      (showRightBorder ? safe.topRight : '')
    : '';

  let topBorder: string | undefined;
  if (showTopBorder && node.style.borderText?.position === 'top') {
    const [before, text, after] = embedTextInBorder(
      topBorderLine,
      node.style.borderText.content,
      node.style.borderText.align,
      node.style.borderText.offset,
      safe.top,
    );
    // Style the border slices around the text but leave the text itself
    // alone — callers pass already-coloured content.
    topBorder =
      styleBorderLine(before, topBorderColor, dimTopBorderColor) +
      text +
      styleBorderLine(after, topBorderColor, dimTopBorderColor);
  } else if (showTopBorder) {
    topBorder = styleBorderLine(
      topBorderLine,
      topBorderColor,
      dimTopBorderColor,
    );
  }

  let verticalBorderHeight = height;
  if (showTopBorder) verticalBorderHeight -= 1;
  if (showBottomBorder) verticalBorderHeight -= 1;
  verticalBorderHeight = Math.max(0, verticalBorderHeight);

  // Build the vertical borders as one tall string with embedded newlines —
  // Output.write will place each line at successive rows. Dim is applied
  // once at the end so the SGR pair brackets the whole column instead of
  // each character.
  let leftBorder = (applyColor(safe.left, leftBorderColor) + '\n').repeat(
    verticalBorderHeight,
  );
  if (dimLeftBorderColor) {
    leftBorder = chalk.dim(leftBorder);
  }

  let rightBorder = (applyColor(safe.right, rightBorderColor) + '\n').repeat(
    verticalBorderHeight,
  );
  if (dimRightBorderColor) {
    rightBorder = chalk.dim(rightBorder);
  }

  const bottomBorderLine = showBottomBorder
    ? (showLeftBorder ? safe.bottomLeft : '') +
      safe.bottom.repeat(contentWidth) +
      (showRightBorder ? safe.bottomRight : '')
    : '';

  let bottomBorder: string | undefined;
  if (showBottomBorder && node.style.borderText?.position === 'bottom') {
    const [before, text, after] = embedTextInBorder(
      bottomBorderLine,
      node.style.borderText.content,
      node.style.borderText.align,
      node.style.borderText.offset,
      safe.bottom,
    );
    bottomBorder =
      styleBorderLine(before, bottomBorderColor, dimBottomBorderColor) +
      text +
      styleBorderLine(after, bottomBorderColor, dimBottomBorderColor);
  } else if (showBottomBorder) {
    bottomBorder = styleBorderLine(
      bottomBorderLine,
      bottomBorderColor,
      dimBottomBorderColor,
    );
  }

  const offsetY = showTopBorder ? 1 : 0;

  if (topBorder) {
    output.write(x, y, topBorder);
  }

  if (showLeftBorder) {
    output.write(x, y + offsetY, leftBorder);
  }

  if (showRightBorder) {
    output.write(x + width - 1, y + offsetY, rightBorder);
  }

  if (bottomBorder) {
    output.write(x, y + height - 1, bottomBorder);
  }
};

export default renderBorder;

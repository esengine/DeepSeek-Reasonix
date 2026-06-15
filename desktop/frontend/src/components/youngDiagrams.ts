// Translate `\yng` (ytableau package) and `\young` (youngtab package)
// macros into KaTeX-compatible `\begin{array}...\end{array}` forms.
// KaTeX 0.17 does not include either macro package, so without this
// pass the macros fail with `Undefined control sequence` and the chat
// surfaces the raw LaTeX source as a red error block.
//
// `\yng(2,1)`               — empty (2,1) Young diagram
// `\yng(2,1,3)`             — empty Young diagram with three rows
// `\yng(2,1){a&b\\c\\d&e}`  — same shape, cells filled by row/col
//                              (rows separated by `\\`, cells by `&`)
// `\young(ab,c)`            — labelled boxes (youngtab syntax)
//
// Cells are `\square` (a Unicode white-square, rendered by KaTeX as
// `mord amsrm` with a real visible glyph) by default so the diagram
// has the same width as a filled one AND is actually visible. Earlier
// versions used `\hphantom{x}` for invisible placeholder width, which
// renders to *no visible glyph* — correct typesetting but the user
// sees nothing on screen and the chat looks empty.
//
// The translator is stateful: it tracks `$…$` math delimiters so it
// only wraps *bare* `\yng`/`\young` in `$…$`. When the macros are
// already inside a math block (e.g. `$\yng(2,1)$`), it just expands
// the inner form without adding extra delimiters — the inner call in
// the math content is enough.

const MAX_ROWS = 64;
const MAX_CELLS = 512;
const EMPTY_CELL = "\\square";
const SKEW_CELL = "\\hphantom{\\boxed{x}}";

function boxedCell(cell: string): string {
  return `\\boxed{${cell}}`;
}

function splitAtTopLevel(s: string, sep: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let buf = "";
  for (let i = 0; i < s.length; i++) {
    const ch = s[i];
    if (ch === "{") depth++;
    else if (ch === "}") depth = Math.max(0, depth - 1);
    if (depth === 0 && s.startsWith(sep, i)) {
      out.push(buf);
      buf = "";
      i += sep.length - 1;
      continue;
    }
    buf += ch;
  }
  out.push(buf);
  return out;
}

function parseShape(s: string, sep: "comma" | "space"): number[] | null {
  const re = sep === "comma" ? /\s*,\s*/ : /\s+/;
  const parts = s.trim().split(re).filter(Boolean);
  if (parts.length === 0 || parts.length > MAX_ROWS) return null;

  const rows: number[] = [];
  let totalCells = 0;
  for (const part of parts) {
    const token = part.trim();
    if (!/^\d+$/.test(token)) return null;
    const n = Number(token);
    if (!Number.isSafeInteger(n) || n <= 0) return null;
    totalCells += n;
    if (totalCells > MAX_CELLS) return null;
    rows.push(n);
  }
  return rows;
}

function renderRows(cells: string[][]): string {
  const arrRows = cells.map((row) => row.join(" \\! "));
  // Use `{l}` (left) instead of `{c}` (centered): a Young diagram has
  // every row's first cell at the same horizontal position — the
  // shorter rows just have fewer cells to the right. `{c}` would
  // centre each row relative to the widest row, which doesn't look
  // like a Young diagram.
  return (
    "\\begin{array}{l}" +
    arrRows.join(" \\\\[-0.525em] ") +
    "\\end{array}"
  );
}

function expandShape(rows: number[], content: string | undefined): string | null {
  const maxN = rows.length === 0 ? 0 : Math.max(...rows);
  // 2D array of cell content. Each cell is `\square` by default
  // (visible Unicode white-square) so the diagram has uniform width
  // AND is actually visible to the reader.
  const cells: string[][] = Array.from({ length: rows.length }, () =>
    Array(maxN).fill(EMPTY_CELL),
  );

  if (content) {
    // Parse content: rows separated by `\\`, cells separated by `&`.
    // The content may contain nested `{...}` (e.g. `\frac{a}{b}`), so
    // we split on `\\` and `&` at brace-depth 0 only.
    const contentRows = splitAtTopLevel(content, "\\\\");
    for (let i = 0; i < contentRows.length && i < rows.length; i++) {
      const cs = splitAtTopLevel(contentRows[i], "&");
      for (let j = 0; j < cs.length && j < rows[i]; j++) {
        const c = cs[j].trim();
        cells[i][j] = c === "" ? EMPTY_CELL : boxedCell(c);
      }
    }
  }

  // Use per-row negative spacing `\\\\[-0.525em]` between rows instead of the
  // default `\\`. The default katex display math baseline-to-baseline
  // spacing is 1.2em, but the `\square` glyph is only 0.675em tall
  // (measured from the katex strut of a single `\square`). With the
  // default spacing, the gap between the bottom of one row's square
  // and the top of the next is 1.2 − 0.675 = 0.525em of visible white
  // space — the diagram looks like a column of disconnected boxes.
  // `\\\\[-0.525em]` reduces the baseline gap to exactly 0.675em so
  // adjacent squares touch with zero visible gap. This is a *per-row
  // spacing* fix, not a per-cell vertical shift: the offset is
  // symmetric across the diagram, so all rows stay aligned.
  return renderRows(cells.map((row, ri) => row.slice(0, rows[ri])));
}

function readBalancedGroup(s: string, openBrace: number): { content: string; end: number } | null {
  let depth = 1;
  let i = openBrace + 1;
  while (i < s.length && depth > 0) {
    if (s[i] === "{") depth++;
    else if (s[i] === "}") depth--;
    if (depth === 0) return { content: s.slice(openBrace + 1, i), end: i + 1 };
    i++;
  }
  return null;
}

function readLatexCell(row: string, start: number): { cell: string; end: number } {
  if (row[start] === "\\") {
    let end = start + 1;
    while (end < row.length && /[A-Za-z]/.test(row[end])) end++;
    if (end === start + 1 && end < row.length) end++;
    while (row[end] === "{") {
      const group = readBalancedGroup(row, end);
      if (!group) break;
      end = group.end;
    }
    return { cell: row.slice(start, end), end };
  }

  if (row[start] === "{") {
    const group = readBalancedGroup(row, start);
    if (group) return { cell: group.content, end: group.end };
  }

  return { cell: row[start], end: start + 1 };
}

function parseYoungTableau(s: string): string[][] | null {
  const rawRows = splitAtTopLevel(s, ",");
  if (rawRows.length === 0 || rawRows.length > MAX_ROWS) return null;

  const rows: string[][] = [];
  let totalCells = 0;
  for (const rawRow of rawRows) {
    const row: string[] = [];
    for (let i = 0; i < rawRow.length;) {
      if (/\s/.test(rawRow[i])) {
        i++;
        continue;
      }
      if (rawRow[i] === ":") {
        row.push(SKEW_CELL);
        totalCells++;
        if (totalCells > MAX_CELLS) return null;
        i++;
        continue;
      }
      const token = readLatexCell(rawRow, i);
      const cell = token.cell.trim();
      if (cell) {
        row.push(boxedCell(cell));
        totalCells++;
        if (totalCells > MAX_CELLS) return null;
      }
      i = token.end;
    }
    rows.push(row);
  }

  if (totalCells === 0) return null;
  return rows;
}

function expandYoungMacro(isYng: boolean, shapeText: string, content: string | undefined): string | null {
  if (isYng) {
    const rows = parseShape(shapeText, "comma");
    return rows ? expandShape(rows, content) : null;
  }

  // Keep compatibility with the existing PR's `\young(2 1)` shape shorthand,
  // but treat comma-separated `\young(ab,c)` as the actual youngtab labelled
  // tableau syntax.
  if (!shapeText.includes(",") && /^\s*\d+(?:\s+\d+)+\s*$/.test(shapeText)) {
    const rows = parseShape(shapeText, "space");
    return rows ? expandShape(rows, undefined) : null;
  }

  const rows = parseYoungTableau(shapeText);
  return rows ? renderRows(rows) : null;
}

function readYoungStart(src: string, i: number): { isYng: boolean; openIdx: number } | null {
  if (src.startsWith("\\young", i)) {
    let openIdx = i + "\\young".length;
    while (/\s/.test(src[openIdx] ?? "")) openIdx++;
    if (src[openIdx] === "(") return { isYng: false, openIdx };
  }
  if (src.startsWith("\\yng", i)) {
    let openIdx = i + "\\yng".length;
    while (/\s/.test(src[openIdx] ?? "")) openIdx++;
    if (src[openIdx] === "(") return { isYng: true, openIdx };
  }
  return null;
}

function findClosingParen(src: string, openIdx: number): number {
  let depth = 1;
  for (let i = openIdx + 1; i < src.length; i++) {
    if (src[i] === "(") depth++;
    else if (src[i] === ")") depth--;
    if (depth === 0) return i;
  }
  return -1;
}

// Find the end of a `\yng(…)` or `\young(…)` call, including optional
// `{…}` content. Returns the index just past the entire macro call,
// or -1 if the open-paren has no matching close.
function findYoungCallEnd(src: string, openIdx: number): number {
  const closeIdx = findClosingParen(src, openIdx);
  if (closeIdx < 0) return -1;
  const afterClose = closeIdx + 1;
  if (src[afterClose] === "{") {
    const group = readBalancedGroup(src, afterClose);
    return group ? group.end : -1;
  }
  return afterClose;
}

/**
 * Walk the input, replacing `\yng(…)` / `\young(…)` with the equivalent
 * KaTeX-compatible `\begin{array}{l}…\end{array}`. If the macro is
 * outside any `$…$` math block, wrap the result in `$…$` so remark-math
 * actually parses it as math. If the macro is already inside a math
 * block (the existing common case), just substitute the expanded form.
 */
export function expandYoungDiagrams(src: string): string {
  let out = "";
  let i = 0;
  // Track whether we're inside a math block. We use a small stack-like
  // counter (depth): 0 = prose, 1 = inline math `$…$`, 2 = display
  // math `$$…$$`. We bump on `$`, decrement on `$`, and clamp at 0/2.
  // For our wrapping decision, depth>0 means "leave the macro alone,
  // it's already in math".
  let depth = 0;

  while (i < src.length) {
    const ch = src[i];

    if (ch === "$") {
      if (isEscapedDollar(src, i)) {
        out += ch;
        i += 1;
        continue;
      }
      if (depth === 0 && isCurrencyLikeDollar(src, i) && !opensYoungMathSpan(src, i)) {
        out += ch;
        i += 1;
        continue;
      }
      // Look ahead for `$$` (display) vs single `$` (inline).
      if (src[i + 1] === "$") {
        // Display math: toggle 0↔2.
        depth = depth === 0 ? 2 : depth === 2 ? 0 : depth;
        out += "$$";
        i += 2;
      } else {
        // Inline math: toggle 0↔1. If we're in display math (depth=2),
        // a single `$` doesn't end it — we need another `$` to do that,
        // so single `$` inside display math is left alone (depth stays 2).
        if (depth === 0) depth = 1;
        else if (depth === 1) depth = 0;
        // depth === 2: single `$` inside `$$…$$` is literal, depth unchanged.
        out += ch;
        i += 1;
      }
      continue;
    }

    // Look for \yng( or \young( and decide whether to wrap or just substitute.
    const youngStart = readYoungStart(src, i);
    if (youngStart) {
      const callEnd = findYoungCallEnd(src, youngStart.openIdx);
      if (callEnd > 0) {
        const closeIdx = findClosingParen(src, youngStart.openIdx);
        if (closeIdx < 0) {
          out += ch;
          i++;
          continue;
        }
        const shapeText = src.slice(youngStart.openIdx + 1, closeIdx);
        let content: string | undefined;
        let contentStart = closeIdx + 1;
        if (src[contentStart] === "{") {
          const group = readBalancedGroup(src, contentStart);
          if (group) content = group.content;
        }
        const expanded = expandYoungMacro(youngStart.isYng, shapeText, content);
        if (!expanded) {
          out += src.slice(i, callEnd);
          i = callEnd;
          continue;
        }
        // Wrap in `$…$` only if we're outside math. Inside math, the
        // surrounding `$`/`$$` already supplies the math delimiters.
        if (depth === 0) {
          const leadingSep = out.endsWith("$") && !isEscapedDollar(out, out.length - 1) ? " " : "";
          const trailingSep = src[callEnd] === "$" && !isEscapedDollar(src, callEnd) ? " " : "";
          out += leadingSep + "$" + expanded + "$" + trailingSep;
        } else {
          out += expanded;
        }
        i = callEnd;
        continue;
      }
    }

    out += ch;
    i++;
  }
  return out;
}

function isEscapedDollar(src: string, i: number): boolean {
  let slashCount = 0;
  for (let j = i - 1; j >= 0 && src[j] === "\\"; j--) slashCount++;
  return slashCount % 2 === 1;
}

function isCurrencyLikeDollar(src: string, i: number): boolean {
  return /\d/.test(src[i + 1] ?? "") || /[\d%]/.test(src[i - 1] ?? "");
}

function opensYoungMathSpan(src: string, dollarIdx: number): boolean {
  if (src[dollarIdx + 1] === "$") return false;

  let closeIdx = -1;
  for (let i = dollarIdx + 1; i < src.length && src[i] !== "\n"; i++) {
    if (src[i] === "$" && !isEscapedDollar(src, i)) {
      closeIdx = i;
      break;
    }
  }
  if (closeIdx < 0) return false;

  for (let i = dollarIdx + 1; i < closeIdx; i++) {
    if (readYoungStart(src, i)) return true;
  }
  return false;
}

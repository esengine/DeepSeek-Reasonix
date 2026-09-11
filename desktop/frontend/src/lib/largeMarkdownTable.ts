export type MarkdownTableAlignment = "left" | "center" | "right" | null;

export interface VirtualMarkdownTableData {
  align: MarkdownTableAlignment[];
  header: string[];
  rows: string[][];
}

export interface ExtractedMarkdownTables {
  text: string;
  markerPrefix: string;
  tables: VirtualMarkdownTableData[];
}

const LARGE_TABLE_MIN_ROWS = 50;
const UNSAFE_INLINE_MARKDOWN_RE = /[\\`*_{}\[\]<>!&$~^]/;

function stripOuterPipe(line: string): string {
  let value = line.trim();
  if (value.startsWith("|")) value = value.slice(1);
  if (value.endsWith("|")) value = value.slice(0, -1);
  return value;
}

function splitPlainRow(line: string): string[] | null {
  if (!line.includes("|") || /^\s{4}/.test(line)) return null;
  const cells = stripOuterPipe(line).split("|").map((cell) => cell.trim());
  if (cells.length < 2 || cells.some((cell) => UNSAFE_INLINE_MARKDOWN_RE.test(cell))) return null;
  return cells;
}

function delimiterAlignment(line: string): MarkdownTableAlignment[] | null {
  if (!line.includes("|") || /^\s{4}/.test(line)) return null;
  const cells = stripOuterPipe(line).split("|").map((cell) => cell.trim());
  if (cells.length < 2 || cells.some((cell) => !/^:?-+:?$/.test(cell))) return null;
  return cells.map((cell) => {
    const left = cell.startsWith(":");
    const right = cell.endsWith(":");
    if (left && right) return "center";
    if (right) return "right";
    if (left) return "left";
    return null;
  });
}

function normalizeRow(cells: string[], columns: number): string[] {
  if (cells.length >= columns) return cells.slice(0, columns);
  return [...cells, ...Array<string>(columns - cells.length).fill("")];
}

function fenceStart(line: string): { marker: string; length: number } | null {
  const match = /^ {0,3}(`{3,}|~{3,})/.exec(line);
  return match ? { marker: match[1][0], length: match[1].length } : null;
}

function fenceEnd(line: string, fence: { marker: string; length: number }): boolean {
  const escaped = fence.marker === "`" ? "`" : "~";
  return new RegExp(`^ {0,3}${escaped}{${fence.length},}[ \\t]*$`).test(line);
}

function unusedMarkerPrefix(text: string): string {
  let suffix = 0;
  let prefix = "REASONIXLARGETABLE";
  while (text.includes(prefix)) {
    suffix += 1;
    prefix = `REASONIXLARGETABLE${suffix}`;
  }
  return prefix;
}

/**
 * Extract only large, top-level GFM tables whose cells are provably plain
 * text. Complex tables keep flowing through remark-gfm unchanged. This avoids
 * micromark's super-linear giant-table path without reimplementing inline
 * Markdown, references, footnotes, math, escapes, entities, or raw HTML.
 */
export function extractLargePlainMarkdownTables(text: string): ExtractedMarkdownTables {
  const lines = text.split("\n");
  const output: string[] = [];
  const tables: VirtualMarkdownTableData[] = [];
  const markerPrefix = unusedMarkerPrefix(text);
  let fence: { marker: string; length: number } | null = null;

  for (let index = 0; index < lines.length;) {
    const line = lines[index];
    if (fence) {
      output.push(line);
      if (fenceEnd(line, fence)) fence = null;
      index += 1;
      continue;
    }
    const openingFence = fenceStart(line);
    if (openingFence) {
      fence = openingFence;
      output.push(line);
      index += 1;
      continue;
    }

    const header = splitPlainRow(line);
    const align = index + 1 < lines.length ? delimiterAlignment(lines[index + 1]) : null;
    if (!header || !align || header.length !== align.length) {
      output.push(line);
      index += 1;
      continue;
    }

    const rows: string[][] = [];
    let rowIndex = index + 2;
    while (rowIndex < lines.length) {
      const cells = splitPlainRow(lines[rowIndex]);
      if (!cells) break;
      rows.push(normalizeRow(cells, header.length));
      rowIndex += 1;
    }
    // Fail closed when a complex/non-plain row might still belong to the same
    // GFM table. Only a blank line or EOF is an unambiguous fast-path end.
    const cleanEnd = rowIndex === lines.length || lines[rowIndex].trim() === "";
    if (rows.length <= LARGE_TABLE_MIN_ROWS || !cleanEnd) {
      output.push(line);
      index += 1;
      continue;
    }

    const tableIndex = tables.length;
    tables.push({ align, header, rows });
    output.push(`${markerPrefix}${tableIndex}`);
    index = rowIndex;
  }

  return { text: output.join("\n"), markerPrefix, tables };
}

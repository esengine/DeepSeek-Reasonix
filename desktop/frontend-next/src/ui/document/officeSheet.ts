import type { Cell, CellValue, Workbook, Worksheet } from "exceljs";
import * as SSF from "ssf";

export const SHEET_ROW_LIMIT = 2000;
export const SHEET_COLUMN_LIMIT = 200;

export interface SheetView {
  name: string;
  html: string;
  rows: number;
  columns: number;
  clipped: boolean;
}

const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
const color = (argb: unknown) => typeof argb === "string" && /^[0-9a-f]{8}$/i.test(argb) ? `#${argb.slice(2)}` : "";
const EXCEL_EPOCH = Date.UTC(1899, 11, 30);

function raw(value: CellValue): unknown {
  if (value === null || value === undefined) return "";
  if (value instanceof Date) return value;
  if (typeof value !== "object") return value;
  if ("richText" in value) return value.richText.map((part) => part.text).join("");
  if ("result" in value) return value.result === undefined ? "" : raw(value.result as CellValue);
  if ("error" in value) return value.error;
  if ("text" in value) return value.text;
  return "";
}

/** The text Excel would show: the cell's own number format applied to its value. */
export function displayText(value: CellValue, numFmt?: string): string {
  const v = raw(value);
  const format = numFmt && numFmt !== "General" ? numFmt : undefined;
  try {
    if (v instanceof Date) return SSF.format(format ?? "yyyy-mm-dd", (v.getTime() - EXCEL_EPOCH) / 864e5);
    if (typeof v === "number" && format) return SSF.format(format, v);
  } catch {
    // An unparseable format falls back to the plain value rather than failing the sheet.
  }
  if (v instanceof Date) return v.toISOString().slice(0, 10);
  return String(v);
}

function cellStyle(cell: Cell): string {
  const parts: string[] = [];
  const fill = cell.fill?.type === "pattern" ? color(cell.fill.fgColor?.argb) : "";
  if (fill) parts.push(`background:${fill}`);
  const font = cell.font;
  if (font?.bold) parts.push("font-weight:bold");
  if (font?.italic) parts.push("font-style:italic");
  const fg = color(font?.color?.argb);
  if (fg) parts.push(`color:${fg}`);
  if (typeof font?.size === "number" && font.size > 0 && font.size < 200) parts.push(`font-size:${font.size}pt`);
  const align = cell.alignment?.horizontal;
  if (align === "left" || align === "center" || align === "right") parts.push(`text-align:${align}`);
  else if (typeof raw(cell.value) === "number") parts.push("text-align:right");
  return parts.join(";");
}

function decodeRange(range: string): { top: number; left: number; bottom: number; right: number } | undefined {
  const m = /^([A-Z]+)(\d+):([A-Z]+)(\d+)$/.exec(range);
  if (!m) return undefined;
  const col = (s: string) => [...s].reduce((n, ch) => n * 26 + ch.charCodeAt(0) - 64, 0);
  return { top: Number(m[2]), left: col(m[1]), bottom: Number(m[4]), right: col(m[3]) };
}

export function sheetView(ws: Worksheet): SheetView {
  const rows = Math.min(ws.rowCount, SHEET_ROW_LIMIT);
  const columns = Math.min(ws.columnCount, SHEET_COLUMN_LIMIT);
  const span = new Map<string, { rows: number; cols: number }>();
  const covered = new Set<string>();
  for (const range of (ws.model as { merges?: string[] }).merges ?? []) {
    const r = decodeRange(range);
    if (!r) continue;
    span.set(`${r.top}:${r.left}`, { rows: r.bottom - r.top + 1, cols: r.right - r.left + 1 });
    for (let y = r.top; y <= r.bottom; y++) for (let x = r.left; x <= r.right; x++) if (y !== r.top || x !== r.left) covered.add(`${y}:${x}`);
  }
  const widths: string[] = [];
  for (let x = 1; x <= columns; x++) {
    const w = ws.getColumn(x).width;
    widths.push(`<col style="width:${Math.round(Math.min(Math.max(typeof w === "number" ? w : 9, 2), 120) * 7 + 10)}px">`);
  }
  let body = "";
  for (let y = 1; y <= rows; y++) {
    const row = ws.getRow(y);
    let cells = `<th>${y}</th>`;
    for (let x = 1; x <= columns; x++) {
      if (covered.has(`${y}:${x}`)) continue;
      const cell = row.getCell(x);
      const merge = span.get(`${y}:${x}`);
      const attrs = merge ? `${merge.rows > 1 ? ` rowspan="${merge.rows}"` : ""}${merge.cols > 1 ? ` colspan="${merge.cols}"` : ""}` : "";
      const style = cellStyle(cell);
      cells += `<td data-cell="${escape(cell.address)}"${attrs}${style ? ` style="${style}"` : ""}>${escape(displayText(cell.value, cell.numFmt))}</td>`;
    }
    body += `<tr>${cells}</tr>`;
  }
  let head = "<th></th>";
  for (let x = 1; x <= columns; x++) head += `<th>${escape(ws.getColumn(x).letter)}</th>`;
  const html = `<style>${SHEET_CSS}</style><table><colgroup><col style="width:40px">${widths.join("")}</colgroup><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
  return { name: ws.name, html, rows: ws.rowCount, columns: ws.columnCount, clipped: ws.rowCount > rows || ws.columnCount > columns };
}

const SHEET_CSS = "body{margin:0;font:13px system-ui,sans-serif;color:#1f1f1f;background:#fff}"
  + "table{border-collapse:collapse;table-layout:fixed}"
  + "td,th{border:1px solid #d9d9d9;padding:2px 6px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}"
  + "th{background:#f3f3f3;color:#666;font-weight:normal;position:sticky;top:0}"
  + "tbody th{position:sticky;left:0}";

export async function readWorkbook(data: ArrayBuffer): Promise<SheetView[]> {
  const { default: ExcelJS } = await import("exceljs");
  const book: Workbook = new ExcelJS.Workbook();
  await book.xlsx.load(data);
  return book.worksheets.filter((ws) => ws.state !== "hidden" && ws.state !== "veryHidden").map(sheetView);
}

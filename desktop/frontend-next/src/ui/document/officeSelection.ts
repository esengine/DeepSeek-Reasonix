import { t } from "../../i18n";

export interface OfficeSelection {
  text: string;
  /** Where the text sits in the file, in terms a later read of the same file can resolve. */
  position: string;
  /** The same place, worded for the reader. */
  label: string;
}

const CELL = /^([A-Z]+)(\d+)$/;
const columnNumber = (s: string) => [...s].reduce((n, ch) => n * 26 + ch.charCodeAt(0) - 64, 0);
function columnLetters(n: number): string {
  let s = "";
  for (; n > 0; n = Math.floor((n - 1) / 26)) s = String.fromCharCode(65 + ((n - 1) % 26)) + s;
  return s;
}

/** The rectangle two cell addresses span, written top-left to bottom-right. */
export function cellRange(a: string, b: string): { top: number; left: number; bottom: number; right: number; text: string } | undefined {
  const x = CELL.exec(a), y = CELL.exec(b);
  if (!x || !y) return undefined;
  const top = Math.min(Number(x[2]), Number(y[2])), bottom = Math.max(Number(x[2]), Number(y[2]));
  const left = Math.min(columnNumber(x[1]), columnNumber(y[1])), right = Math.max(columnNumber(x[1]), columnNumber(y[1]));
  const start = `${columnLetters(left)}${top}`, end = `${columnLetters(right)}${bottom}`;
  return { top, left, bottom, right, text: start === end ? start : `${start}:${end}` };
}

/** The cells of a range in the rendered sheet, as tab-separated rows. */
export function sheetSelection(doc: Document, sheet: string, a: string, b: string): OfficeSelection | undefined {
  const range = cellRange(a, b);
  if (!range) return undefined;
  const rows: string[] = [];
  for (let r = range.top; r <= range.bottom; r++) {
    const row: string[] = [];
    for (let c = range.left; c <= range.right; c++) {
      row.push(doc.querySelector(`[data-cell="${columnLetters(c)}${r}"]`)?.textContent ?? "");
    }
    rows.push(row.join("\t"));
  }
  return { text: rows.join("\n"), position: `sheet "${sheet}", cells ${range.text}`, label: t("{sheet} 工作表 {range}", { sheet, range: range.text }) };
}

/** Marks the cells of a range, clearing any earlier mark. */
export function markCells(doc: Document, a: string, b: string): void {
  doc.querySelectorAll(".sel").forEach((el) => el.classList.remove("sel"));
  const range = cellRange(a, b);
  if (!range) return;
  for (let r = range.top; r <= range.bottom; r++) {
    for (let c = range.left; c <= range.right; c++) doc.querySelector(`[data-cell="${columnLetters(c)}${r}"]`)?.classList.add("sel");
  }
}

const bodyBlocks = (doc: Document) => [...doc.querySelectorAll("section.docx > article > *")];

function blockOf(blocks: Element[], node: Node | null): number {
  for (let el: Node | null = node; el; el = el.parentNode) {
    const at = blocks.indexOf(el as Element);
    if (at >= 0) return at + 1;
  }
  return 0;
}

/** The selected text of a rendered Word body and the body blocks it covers, counted
 *  from 1 in document order with each paragraph and each table one block. */
export function wordSelection(doc: Document): OfficeSelection | undefined {
  const sel = doc.getSelection();
  const text = sel?.toString().trim();
  if (!sel || sel.isCollapsed || !text || sel.rangeCount === 0) return undefined;
  const range = sel.getRangeAt(0);
  const blocks = bodyBlocks(doc);
  const first = blockOf(blocks, range.startContainer);
  let last = blockOf(blocks, range.endContainer);
  if (first === 0 || last === 0) return undefined;
  // A triple click ends the range at the start of the next block; a block none of
  // whose text is selected is not part of the selection.
  if (last > first) {
    const tail = doc.createRange();
    tail.setStart(blocks[last - 1], 0);
    tail.setEnd(range.endContainer, range.endOffset);
    if (!tail.toString().trim()) last--;
  }
  return first === last
    ? { text, position: `body block ${first}`, label: t("正文第 {n} 块", { n: first }) }
    : { text, position: `body blocks ${first}-${last}`, label: t("正文第 {a}–{b} 块", { a: first, b: last }) };
}

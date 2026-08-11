import type { TranscriptSelectionPoint } from "./transcriptSelectionStore";

export const TRANSCRIPT_SELECTABLE_SELECTOR = "[data-transcript-selectable]";
export const TRANSCRIPT_ROW_SELECTOR = ".transcript__row[data-row-key]";
const TRANSCRIPT_SOURCE_FALLBACK_SELECTOR = "[data-transcript-selection-source-fallback]";

type ProjectionSegment = {
  node: Node;
  start: number;
  end: number;
  atomic: boolean;
};

export type DomSelectionProjection = {
  text: string;
  segments: readonly ProjectionSegment[];
};

const BLOCK_TAGS = new Set([
  "ADDRESS", "ARTICLE", "ASIDE", "BLOCKQUOTE", "DD", "DETAILS", "DIV", "DL", "DT",
  "FIGCAPTION", "FIGURE", "FOOTER", "FORM", "H1", "H2", "H3", "H4", "H5", "H6",
  "HEADER", "HR", "LI", "MAIN", "NAV", "OL", "P", "PRE", "SECTION", "SUMMARY", "UL",
]);
const IGNORE_TAGS = new Set(["BUTTON", "INPUT", "OPTION", "SCRIPT", "SELECT", "STYLE", "TEXTAREA"]);

function elementForNode(node: Node | null): Element | null {
  if (!node) return null;
  return node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement;
}

export function selectableRootForNode(node: Node | null): HTMLElement | null {
  return elementForNode(node)?.closest<HTMLElement>(TRANSCRIPT_SELECTABLE_SELECTOR) ?? null;
}

export function rowKeyForNode(node: Node | null): string | null {
  return elementForNode(node)?.closest<HTMLElement>(TRANSCRIPT_ROW_SELECTOR)?.dataset.rowKey ?? null;
}

/**
 * Plain Markdown fallbacks expose source characters rather than the readable
 * HAST projection. Keep the browser's native range until the canonical
 * rendered projection is mounted, otherwise the frozen UTF-16 offsets can
 * address different characters when the worker result arrives.
 */
export function transcriptSelectionProjectionReadyForNode(node: Node | null): boolean {
  const root = selectableRootForNode(node);
  return root != null
    && !root.matches(TRANSCRIPT_SOURCE_FALLBACK_SELECTOR)
    && !root.querySelector(TRANSCRIPT_SOURCE_FALLBACK_SELECTOR);
}

function ignored(element: HTMLElement): boolean {
  return IGNORE_TAGS.has(element.tagName)
    || element.getAttribute("aria-hidden") === "true"
    || element.hasAttribute("data-transcript-selection-ignore");
}

function formulaSource(element: HTMLElement): string | null {
  const annotation = element.querySelector<HTMLElement>('annotation[encoding="application/x-tex"]');
  return annotation?.textContent ?? element.dataset.latexSource ?? null;
}

export function projectTranscriptSelectableDom(root: HTMLElement): DomSelectionProjection {
  let text = "";
  const segments: ProjectionSegment[] = [];
  const append = (value: string, node: Node, atomic = false) => {
    if (!value) return;
    const start = text.length;
    text += value;
    segments.push({ node, start, end: text.length, atomic });
  };
  const lineBreak = (lines = 1) => {
    if (!text) return;
    const trailing = /\n*$/.exec(text)?.[0].length ?? 0;
    if (trailing < lines) text += "\n".repeat(lines - trailing);
  };

  const projectTable = (table: HTMLElement) => {
    const rows = Array.from(table.querySelectorAll<HTMLTableRowElement>("tr"));
    rows.forEach((row, rowIndex) => {
      const cells = Array.from(row.children).filter(
        (cell): cell is HTMLElement => cell instanceof HTMLElement && (cell.tagName === "TH" || cell.tagName === "TD"),
      );
      cells.forEach((cell, cellIndex) => {
        walk(cell, false);
        if (cellIndex < cells.length - 1) append("\t", cell, true);
      });
      if (rowIndex < rows.length - 1) append("\n", row, true);
    });
  };

  const walk = (node: Node, honorBlock = true): void => {
    if (node.nodeType === Node.TEXT_NODE) {
      const value = node.textContent ?? "";
      const parent = node.parentElement;
      if (/^\s+$/.test(value) && /[\r\n]/.test(value) && !parent?.closest("pre, code")) return;
      append(value, node);
      return;
    }
    if (!(node instanceof HTMLElement) || ignored(node)) return;
    if (node.matches(".katex-display, .katex")) {
      const source = formulaSource(node);
      if (source) append(node.classList.contains("katex-display") ? `$$\n${source}\n$$` : `$${source}$`, node, true);
      return;
    }
    if (node.tagName === "BR") {
      lineBreak(1);
      return;
    }
    if (node.tagName === "IMG") {
      append(node.getAttribute("alt") ?? "", node, true);
      return;
    }
    if (node.tagName === "TABLE") {
      lineBreak(1);
      projectTable(node);
      lineBreak(1);
      return;
    }
    const block = honorBlock && BLOCK_TAGS.has(node.tagName);
    if (block) lineBreak(1);
    for (const child of node.childNodes) walk(child);
    if (block) lineBreak(1);
  };

  for (const child of root.childNodes) walk(child);
  return { text: text.replace(/^\n+|\n+$/g, ""), segments };
}

function pointInsideNode(pointNode: Node, candidate: Node): boolean {
  return pointNode === candidate || (candidate instanceof Element && candidate.contains(pointNode));
}

function collapsedRange(doc: Document, node: Node, offset: number): globalThis.Range | null {
  try {
    const range = doc.createRange();
    range.setStart(node, Math.max(0, Math.min(offset, node.nodeType === Node.TEXT_NODE ? node.textContent?.length ?? 0 : node.childNodes.length)));
    range.collapse(true);
    return range;
  } catch {
    return null;
  }
}

function pointBeforeNode(point: globalThis.Range, node: Node): boolean {
  const doc = node.ownerDocument;
  if (!doc) return false;
  try {
    const start = doc.createRange();
    start.selectNode(node);
    start.collapse(true);
    return point.compareBoundaryPoints(globalThis.Range.START_TO_START, start) <= 0;
  } catch {
    return false;
  }
}

export function domPointToTranscriptOffset(
  root: HTMLElement,
  node: Node,
  offset: number,
  clientX?: number,
): number | null {
  if (!root.contains(node) && root !== node) return null;
  const projection = projectTranscriptSelectableDom(root);
  const point = collapsedRange(root.ownerDocument, node, offset);
  for (const segment of projection.segments) {
    if (segment.node.nodeType === Node.TEXT_NODE && segment.node === node) {
      return Math.max(segment.start, Math.min(segment.end, segment.start + offset));
    }
    if (segment.atomic && pointInsideNode(node, segment.node)) {
      if (clientX !== undefined && segment.node instanceof Element) {
        const rect = segment.node.getBoundingClientRect();
        return clientX < rect.left + rect.width / 2 ? segment.start : segment.end;
      }
      return offset <= 0 ? segment.start : segment.end;
    }
    if (point && pointBeforeNode(point, segment.node)) return segment.start;
  }
  return projection.text.length;
}

export function transcriptSelectionPointFromDom(
  node: Node | null,
  offset: number,
  clientX?: number,
): TranscriptSelectionPoint | null {
  const root = selectableRootForNode(node);
  const rowKey = rowKeyForNode(node);
  if (!root || !rowKey || !node) return null;
  const textOffset = domPointToTranscriptOffset(root, node, offset, clientX);
  return textOffset == null ? null : { rowKey, textOffset, affinity: "forward" };
}

type CaretDocument = Document & {
  caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
  caretRangeFromPoint?: (x: number, y: number) => globalThis.Range | null;
};

function nearestSelectableRoot(doc: Document, x: number, y: number): HTMLElement | null {
  const direct = doc.elementFromPoint?.(x, y)?.closest<HTMLElement>(TRANSCRIPT_SELECTABLE_SELECTOR);
  if (direct) return direct;
  const roots = Array.from(doc.querySelectorAll<HTMLElement>(TRANSCRIPT_SELECTABLE_SELECTOR))
    .filter((root) => root.getClientRects().length > 0);
  let nearest: HTMLElement | null = null;
  let distance = Number.POSITIVE_INFINITY;
  for (const root of roots) {
    const rect = root.getBoundingClientRect();
    const vertical = y < rect.top ? rect.top - y : y > rect.bottom ? y - rect.bottom : 0;
    const horizontal = x < rect.left ? rect.left - x : x > rect.right ? x - rect.right : 0;
    const next = vertical * 4 + horizontal;
    if (next < distance) {
      distance = next;
      nearest = root;
    }
  }
  return nearest;
}

export function transcriptSelectionPointFromClient(doc: Document, x: number, y: number): TranscriptSelectionPoint | null {
  const caretDoc = doc as CaretDocument;
  const root = nearestSelectableRoot(doc, x, y);
  const position = caretDoc.caretPositionFromPoint?.(x, y);
  if (position && (!root || selectableRootForNode(position.offsetNode) === root)) {
    const point = transcriptSelectionPointFromDom(position.offsetNode, position.offset, x);
    if (point) return point;
  }
  const range = caretDoc.caretRangeFromPoint?.(x, y);
  if (range && (!root || selectableRootForNode(range.startContainer) === root)) {
    const point = transcriptSelectionPointFromDom(range.startContainer, range.startOffset, x);
    if (point) return point;
  }
  if (!root) return null;
  const projection = projectTranscriptSelectableDom(root);
  const rowKey = rowKeyForNode(root);
  if (!rowKey) return null;
  const rect = root.getBoundingClientRect();
  const textOffset = y < rect.top || (y <= rect.bottom && x < rect.left + rect.width / 2) ? 0 : projection.text.length;
  return { rowKey, textOffset, affinity: "forward" };
}

function boundaryForOffset(
  root: HTMLElement,
  projection: DomSelectionProjection,
  offset: number,
  end: boolean,
): { node: Node; offset: number } {
  const clamped = Math.max(0, Math.min(projection.text.length, offset));
  for (const segment of projection.segments) {
    if (clamped < segment.start || (clamped === segment.start && !end)) {
      const parent = segment.node.parentNode;
      if (parent) return { node: parent, offset: Array.prototype.indexOf.call(parent.childNodes, segment.node) };
    }
    if (clamped <= segment.end) {
      if (!segment.atomic && segment.node.nodeType === Node.TEXT_NODE) {
        return { node: segment.node, offset: Math.max(0, Math.min(segment.end - segment.start, clamped - segment.start)) };
      }
      const parent = segment.node.parentNode;
      if (parent) {
        const index = Array.prototype.indexOf.call(parent.childNodes, segment.node);
        return { node: parent, offset: index + (clamped > segment.start || end ? 1 : 0) };
      }
    }
  }
  return { node: root, offset: root.childNodes.length };
}

export function domRangeForTranscriptOffsets(root: HTMLElement, start: number, end: number): globalThis.Range | null {
  const projection = projectTranscriptSelectableDom(root);
  if (start <= 0 && end >= projection.text.length) {
    const range = root.ownerDocument.createRange();
    range.selectNodeContents(root);
    return range;
  }
  const from = boundaryForOffset(root, projection, start, false);
  const to = boundaryForOffset(root, projection, end, true);
  try {
    const range = root.ownerDocument.createRange();
    range.setStart(from.node, from.offset);
    range.setEnd(to.node, to.offset);
    return range;
  } catch {
    return null;
  }
}

export function transcriptSelectionPointClientRect(point: TranscriptSelectionPoint): DOMRect | null {
  const row = Array.from(document.querySelectorAll<HTMLElement>(TRANSCRIPT_ROW_SELECTOR))
    .find((candidate) => candidate.dataset.rowKey === point.rowKey);
  const root = row?.querySelector<HTMLElement>(TRANSCRIPT_SELECTABLE_SELECTOR);
  if (!root) return null;
  const projection = projectTranscriptSelectableDom(root);
  const offset = Math.max(0, Math.min(projection.text.length, point.textOffset));
  const start = offset > 0 ? offset - 1 : 0;
  const end = offset < projection.text.length ? offset + 1 : projection.text.length;
  const range = domRangeForTranscriptOffsets(root, start, end);
  if (!range || typeof range.getClientRects !== "function") return null;
  const rects = Array.from(range.getClientRects()).filter((rect) => rect.width > 0 || rect.height > 0);
  return (point.affinity === "backward" ? rects[0] : rects[rects.length - 1]) ?? null;
}

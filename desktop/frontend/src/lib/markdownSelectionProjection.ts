import type { Element as HastElement, RootContent as HastRootContent } from "hast";
import type { VirtualMarkdownTableData } from "./largeMarkdownTable";

export interface SelectionProjectionBlock {
  children: HastRootContent[];
  virtualTable?: VirtualMarkdownTableData;
}

const BLOCK_TAGS = new Set([
  "address", "article", "aside", "blockquote", "dd", "details", "div", "dl", "dt",
  "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6",
  "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "summary", "ul",
]);
const IGNORE_TAGS = new Set(["button", "input", "option", "script", "select", "style", "textarea"]);

function classNames(node: HastElement): string[] {
  const value: unknown = node.properties.className;
  if (Array.isArray(value)) return value.filter((item): item is string => typeof item === "string");
  return typeof value === "string" ? value.split(/\s+/).filter(Boolean) : [];
}

function property(node: HastElement, ...names: string[]): unknown {
  for (const name of names) {
    if (Object.prototype.hasOwnProperty.call(node.properties, name)) return node.properties[name];
  }
  return undefined;
}

function isHidden(node: HastElement): boolean {
  const ariaHidden = property(node, "ariaHidden", "aria-hidden");
  const ignored = property(node, "dataTranscriptSelectionIgnore", "data-transcript-selection-ignore");
  return ariaHidden === true || ariaHidden === "true" || ignored !== undefined;
}

function latexSource(node: HastElement): string | null {
  const direct = property(node, "dataLatexSource", "data-latex-source");
  if (typeof direct === "string" && direct) return direct;
  const find = (children: readonly HastRootContent[]): string | null => {
    for (const child of children) {
      if (child.type !== "element") continue;
      if (child.tagName === "annotation" && property(child, "encoding") === "application/x-tex") {
        return child.children.map((part) => part.type === "text" ? part.value : "").join("");
      }
      const nested = find(child.children);
      if (nested) return nested;
    }
    return null;
  };
  return find(node.children);
}

class TextProjection {
  private value = "";

  text(text: string): void {
    this.value += text;
  }

  break(lines = 1): void {
    if (!this.value) return;
    const trailing = /\n*$/.exec(this.value)?.[0].length ?? 0;
    if (trailing < lines) this.value += "\n".repeat(lines - trailing);
  }

  result(): string {
    return this.value.replace(/^\n+|\n+$/g, "");
  }
}

function nodeText(node: HastRootContent): string {
  const output = new TextProjection();
  projectNode(node, output);
  return output.result();
}

function tableRows(node: HastElement): HastElement[] {
  const rows: HastElement[] = [];
  const walk = (element: HastElement) => {
    if (element.tagName === "tr") {
      rows.push(element);
      return;
    }
    for (const child of element.children) if (child.type === "element") walk(child);
  };
  walk(node);
  return rows;
}

function projectTable(node: HastElement, output: TextProjection): void {
  const rows = tableRows(node);
  rows.forEach((row, rowIndex) => {
    const cells = row.children.filter(
      (child): child is HastElement => child.type === "element" && (child.tagName === "td" || child.tagName === "th"),
    );
    output.text(cells.map(nodeText).join("\t"));
    if (rowIndex < rows.length - 1) output.break(1);
  });
}

function projectNode(node: HastRootContent, output: TextProjection, preserveWhitespace = false): void {
  if (node.type === "text") {
    if (!preserveWhitespace && /^\s+$/.test(node.value) && /[\r\n]/.test(node.value)) return;
    output.text(node.value);
    return;
  }
  if (node.type !== "element") return;
  if (IGNORE_TAGS.has(node.tagName) || isHidden(node)) return;

  const classes = classNames(node);
  if (classes.includes("katex-display") || classes.includes("katex")) {
    const source = latexSource(node);
    if (source) output.text(classes.includes("katex-display") ? `$$\n${source}\n$$` : `$${source}$`);
    return;
  }
  if (node.tagName === "br") {
    output.break(1);
    return;
  }
  if (node.tagName === "img") {
    const alt = property(node, "alt");
    if (typeof alt === "string") output.text(alt);
    return;
  }
  if (node.tagName === "table") {
    projectTable(node, output);
    return;
  }

  const block = BLOCK_TAGS.has(node.tagName);
  if (block) output.break(1);
  const childPreservesWhitespace = preserveWhitespace || node.tagName === "pre" || node.tagName === "code";
  for (const child of node.children) projectNode(child, output, childPreservesWhitespace);
  if (block) output.break(1);
}

export function virtualMarkdownTableSelectionText(data: VirtualMarkdownTableData): string {
  return [data.header, ...data.rows].map((row) => row.join("\t")).join("\n");
}

/** Project rendered, user-readable text from the same HAST used for display. */
export function markdownSelectionTextFromBlocks(blocks: readonly SelectionProjectionBlock[]): string {
  const projected = blocks.map((block) => {
    if (block.virtualTable) return virtualMarkdownTableSelectionText(block.virtualTable);
    const output = new TextProjection();
    for (const child of block.children) projectNode(child, output);
    return output.result();
  });
  // Keep this separator identical to the rendered DOM adapter. Logical
  // selection endpoints are measured against that DOM projection, so adding
  // an extra newline between parser blocks would shift every later offset.
  return projected.filter((text) => text !== "").join("\n");
}

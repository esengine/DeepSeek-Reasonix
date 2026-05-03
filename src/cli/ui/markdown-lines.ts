/** Pure markdown → flat MdLine[]. Streaming-safe: marked.lexer tolerates partial input. */

import { type Token, type Tokens, marked } from "marked";

export interface InlineStyle {
  bold?: boolean;
  italic?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
  fileRef?: { path: string; line?: number; lineEnd?: number };
}

export interface InlineSpan extends InlineStyle {
  text: string;
}

export type MdLine =
  | { kind: "blank" }
  | { kind: "hr" }
  | { kind: "heading"; level: number; spans: InlineSpan[] }
  | { kind: "paragraph"; spans: InlineSpan[] }
  | {
      kind: "list";
      ordered: boolean;
      index: number;
      depth: number;
      task?: "todo" | "done";
      spans: InlineSpan[];
    }
  | { kind: "code"; lang: string; text: string }
  | { kind: "blockquote"; spans: InlineSpan[] };

const FILE_REF_RE = /\b([A-Za-z0-9_./@\-]+\.[A-Za-z0-9]{1,6})(?::(\d+)(?:-(\d+))?)?\b/g;

marked.use({ gfm: true, breaks: false });

export function markdownToLines(text: string): MdLine[] {
  if (text.length === 0) return [];
  const tokens = marked.lexer(text);
  const out: MdLine[] = [];
  for (const tok of tokens) emitBlock(tok, out, 0);
  return out;
}

function emitBlock(tok: Token, out: MdLine[], depth: number): void {
  switch (tok.type) {
    case "heading": {
      const h = tok as Tokens.Heading;
      out.push({ kind: "heading", level: h.depth, spans: inline(h.tokens ?? []) });
      return;
    }
    case "paragraph": {
      const p = tok as Tokens.Paragraph;
      out.push({ kind: "paragraph", spans: inline(p.tokens ?? []) });
      return;
    }
    case "code": {
      const c = tok as Tokens.Code;
      out.push({ kind: "code", lang: (c.lang ?? "").split(/\s+/)[0] ?? "", text: c.text });
      return;
    }
    case "list": {
      const l = tok as Tokens.List;
      const startIndex = Number(l.start) || 1;
      l.items.forEach((item, i) => emitListItem(item, out, l.ordered, startIndex + i, depth));
      return;
    }
    case "hr":
      out.push({ kind: "hr" });
      return;
    case "blockquote": {
      const bq = tok as Tokens.Blockquote;
      for (const child of bq.tokens ?? []) {
        if (child.type === "paragraph") {
          out.push({ kind: "blockquote", spans: inline((child as Tokens.Paragraph).tokens ?? []) });
        } else if (child.type === "space") {
          // skip
        } else {
          // For nested non-paragraph blocks (lists, code), fall back to a flat blockquote span.
          const flat = plainTokens(child);
          if (flat.length > 0) out.push({ kind: "blockquote", spans: [{ text: flat }] });
        }
      }
      return;
    }
    case "space":
      out.push({ kind: "blank" });
      return;
    case "html": {
      const h = tok as Tokens.HTML;
      out.push({ kind: "paragraph", spans: [{ text: h.text }] });
      return;
    }
    default: {
      // Unknown / table / def — render the raw text as a paragraph fallback.
      const raw = (tok as { raw?: string }).raw ?? "";
      if (raw.trim().length > 0) out.push({ kind: "paragraph", spans: [{ text: raw }] });
    }
  }
}

function emitListItem(
  item: Tokens.ListItem,
  out: MdLine[],
  ordered: boolean,
  index: number,
  depth: number,
): void {
  const task = item.task ? (item.checked ? "done" : "todo") : undefined;
  const head: MdLine = {
    kind: "list",
    ordered,
    index,
    depth,
    spans: [],
    ...(task ? { task } : {}),
  };
  out.push(head);
  for (const tok of item.tokens) {
    if (tok.type === "text") {
      const t = tok as Tokens.Text;
      const spans = t.tokens ? inline(t.tokens) : inlineFromText(t.text);
      head.spans.push(...spans);
    } else if (tok.type === "list") {
      const sub = tok as Tokens.List;
      const subStart = Number(sub.start) || 1;
      sub.items.forEach((s, i) => emitListItem(s, out, sub.ordered, subStart + i, depth + 1));
    } else {
      emitBlock(tok, out, depth);
    }
  }
}

function inline(tokens: Token[]): InlineSpan[] {
  const out: InlineSpan[] = [];
  walk(tokens, {}, out);
  return mergeAdjacent(out);
}

function walk(tokens: Token[], style: InlineStyle, out: InlineSpan[]): void {
  for (const tok of tokens) {
    switch (tok.type) {
      case "text": {
        const t = tok as Tokens.Text;
        if (t.tokens && t.tokens.length > 0) walk(t.tokens, style, out);
        else pushTextSpans(t.text, style, out);
        break;
      }
      case "strong":
        walk((tok as Tokens.Strong).tokens, { ...style, bold: true }, out);
        break;
      case "em":
        walk((tok as Tokens.Em).tokens, { ...style, italic: true }, out);
        break;
      case "del":
        walk((tok as Tokens.Del).tokens, { ...style, strike: true }, out);
        break;
      case "codespan":
        out.push({ text: (tok as Tokens.Codespan).text, code: true, ...style });
        break;
      case "link": {
        const l = tok as Tokens.Link;
        // A link's children are still subject to ancestor styles; emit each
        // descendant span with the link href so OSC8 can wrap it later.
        const before = out.length;
        walk(l.tokens, style, out);
        for (let i = before; i < out.length; i++) {
          const span = out[i]!;
          if (!span.link) span.link = l.href;
        }
        break;
      }
      case "image": {
        const im = tok as Tokens.Image;
        out.push({ text: `[image: ${im.text || im.href}]`, ...style });
        break;
      }
      case "br":
        out.push({ text: "\n", ...style });
        break;
      case "escape":
        pushTextSpans((tok as Tokens.Escape).text, style, out);
        break;
      case "html":
        pushTextSpans((tok as Tokens.HTML).text, style, out);
        break;
      default:
        pushTextSpans((tok as { raw?: string }).raw ?? "", style, out);
    }
  }
}

function pushTextSpans(text: string, style: InlineStyle, out: InlineSpan[]): void {
  if (text.length === 0) return;
  // Split out file refs so the renderer can OSC8-link them.
  let cursor = 0;
  for (const m of text.matchAll(FILE_REF_RE)) {
    const start = m.index ?? 0;
    const end = start + m[0].length;
    if (start > cursor) out.push({ text: text.slice(cursor, start), ...style });
    const path = m[1]!;
    const ln = m[2] ? Number(m[2]) : undefined;
    const lnEnd = m[3] ? Number(m[3]) : undefined;
    out.push({
      text: m[0],
      ...style,
      fileRef: {
        path,
        ...(ln !== undefined ? { line: ln } : {}),
        ...(lnEnd !== undefined ? { lineEnd: lnEnd } : {}),
      },
    });
    cursor = end;
  }
  if (cursor < text.length) out.push({ text: text.slice(cursor), ...style });
}

function inlineFromText(text: string): InlineSpan[] {
  const out: InlineSpan[] = [];
  pushTextSpans(text, {}, out);
  return out;
}

function mergeAdjacent(spans: InlineSpan[]): InlineSpan[] {
  if (spans.length < 2) return spans;
  const out: InlineSpan[] = [];
  for (const s of spans) {
    const last = out[out.length - 1];
    if (last && stylesEqual(last, s)) {
      out[out.length - 1] = { ...last, text: last.text + s.text };
    } else {
      out.push(s);
    }
  }
  return out;
}

function stylesEqual(a: InlineSpan, b: InlineSpan): boolean {
  return (
    !!a.bold === !!b.bold &&
    !!a.italic === !!b.italic &&
    !!a.strike === !!b.strike &&
    !!a.code === !!b.code &&
    a.link === b.link &&
    fileRefEqual(a.fileRef, b.fileRef)
  );
}

function fileRefEqual(a: InlineSpan["fileRef"], b: InlineSpan["fileRef"]): boolean {
  if (!a && !b) return true;
  if (!a || !b) return false;
  return a.path === b.path && a.line === b.line && a.lineEnd === b.lineEnd;
}

function plainTokens(tok: Token): string {
  if ("raw" in tok && typeof (tok as { raw?: string }).raw === "string") {
    return (tok as { raw: string }).raw.trim();
  }
  return "";
}

/** Extract just the visible characters from a span list — handy for tests / previews. */
export function spansText(spans: ReadonlyArray<InlineSpan>): string {
  let s = "";
  for (const span of spans) s += span.text;
  return s;
}

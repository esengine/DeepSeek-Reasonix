/** What a person can change on a picked element of a page that is a file. */
export type SourceEdit =
  | { kind: "text"; before: string; after: string }
  | { kind: "style"; property: StyleProperty; value: string };

export type StyleProperty = "color" | "backgroundColor" | "fontSize" | "fontWeight";

/** Why a source could not take the edit. Each answer is the file's, not the page's:
 *  the page may look fine while the file cannot say where an element lives. */
export type PatchFailure = "not_found" | "ambiguous" | "unmappable" | "mismatch" | "not_plain_text" | "bad_value";

export type Patched = { ok: true; content: string } | { ok: false; code: PatchFailure };

export interface Target {
  selector: string;
  tagName: string;
}

interface Tag {
  name: string;
  start: number;
  end: number;
  /** The first end tag or start tag after this one, which closes its text. */
  next?: Token;
}

interface Token {
  closing: boolean;
  name: string;
  start: number;
}

const RAW_TEXT = new Set(["script", "style", "textarea", "title"]);
// Elements the parser makes without a tag in the source.
const IMPLIED = new Set(["html", "head", "body", "tbody"]);
const CSS_NAME: Record<StyleProperty, string> = {
  color: "color",
  backgroundColor: "background-color",
  fontSize: "font-size",
  fontWeight: "font-weight",
};

// The source's start tags in document order, found without building a tree: a
// comment, a doctype and the body of a raw-text element hold no tags of the
// page's. A scan that cannot tell is reported as null, never guessed at.
function scan(source: string): { starts: Tag[]; tokens: Token[] } | null {
  const starts: Tag[] = [];
  const tokens: Token[] = [];
  let at = 0;
  while (at < source.length) {
    const open = source.indexOf("<", at);
    if (open < 0) break;
    if (source.startsWith("<!--", open)) {
      const close = source.indexOf("-->", open + 4);
      if (close < 0) return null;
      at = close + 3;
      continue;
    }
    if (source[open + 1] === "!" || source[open + 1] === "?") {
      const close = source.indexOf(">", open);
      if (close < 0) return null;
      at = close + 1;
      continue;
    }
    const match = /^<(\/?)([A-Za-z][^\s/>]*)/.exec(source.slice(open, open + 64));
    if (!match) {
      at = open + 1;
      continue;
    }
    const end = tagEnd(source, open + match[0].length);
    if (end < 0) return null;
    const name = match[2].toLowerCase();
    const token: Token = { closing: match[1] === "/", name, start: open };
    tokens.push(token);
    if (!token.closing) starts.push({ name, start: open, end });
    at = end;
    if (!token.closing && RAW_TEXT.has(name)) {
      const close = new RegExp(`</${name}[\\s/>]`, "i").exec(source.slice(at));
      if (!close) return null;
      at += close.index;
    }
  }
  for (const tag of starts) {
    const index = tokens.findIndex((token) => token.start === tag.start);
    tag.next = tokens[index + 1];
  }
  return { starts, tokens };
}

// The offset just past the ">" that ends a tag, honouring quoted values.
function tagEnd(source: string, from: number): number {
  let quote = "";
  for (let i = from; i < source.length; i++) {
    const c = source[i];
    if (quote) {
      if (c === quote) quote = "";
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === ">") {
      return i + 1;
    }
  }
  return -1;
}

// Which source tag made each element of the parsed page. The parser may add
// elements the source never wrote (html, head, body, tbody); anything else it
// added, or dropped, means the source cannot be read off the page by position.
function align(elements: Element[], starts: Tag[]): Map<Element, Tag> | null {
  const out = new Map<Element, Tag>();
  let i = 0;
  for (const el of elements) {
    const name = el.localName.toLowerCase();
    if (starts[i] && starts[i].name === name) {
      out.set(el, starts[i++]);
    } else if (!IMPLIED.has(name)) {
      return null;
    }
  }
  return i === starts.length ? out : null;
}

const collapse = (text: string) => text.replace(/\s+/g, " ").trim();

const escapeText = (text: string) => text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
const escapeAttr = (text: string) => text.replace(/&/g, "&amp;").replace(/"/g, "&quot;");

// A value that stays inside one declaration. A rejected one is refused rather
// than cleaned: what was typed is not what would be saved.
const SAFE_VALUE = /^[^;{}<>"\r\n\\]{1,80}$/;

function declarations(style: string): [string, string][] {
  const out: [string, string][] = [];
  let depth = 0;
  let quote = "";
  let from = 0;
  const flush = (to: number) => {
    const part = style.slice(from, to);
    const colon = part.indexOf(":");
    if (colon > 0) out.push([part.slice(0, colon).trim(), part.slice(colon + 1).trim()]);
  };
  for (let i = 0; i < style.length; i++) {
    const c = style[i];
    if (quote) {
      if (c === quote) quote = "";
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === "(") {
      depth++;
    } else if (c === ")") {
      depth = Math.max(0, depth - 1);
    } else if (c === ";" && depth === 0) {
      flush(i);
      from = i + 1;
    }
  }
  flush(style.length);
  return out;
}

function withStyle(el: Element, tag: Tag, source: string, property: StyleProperty, value: string): Patched {
  const name = CSS_NAME[property];
  const had = declarations(el.getAttribute("style") ?? "");
  const index = had.findIndex(([key]) => key.toLowerCase() === name);
  const next: [string, string][] = index >= 0 ? had.map((d, i) => (i === index ? [d[0], value] : d)) : [...had, [name, value]];
  const written = ` style="${escapeAttr(next.map(([k, v]) => `${k}: ${v}`).join("; "))}"`;
  const body = source.slice(tag.start, tag.end);
  const attr = /\sstyle\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/i.exec(body);
  if (attr) {
    const from = tag.start + attr.index;
    return { ok: true, content: source.slice(0, from) + written + source.slice(from + attr[0].length) };
  }
  const at = tag.start + 1 + tag.name.length;
  return { ok: true, content: source.slice(0, at) + written + source.slice(at) };
}

function withText(el: Element, tag: Tag, source: string, edit: { before: string; after: string }): Patched {
  if (el.children.length > 0 || RAW_TEXT.has(tag.name)) return { ok: false, code: "not_plain_text" };
  if (collapse(el.textContent ?? "") !== collapse(edit.before)) return { ok: false, code: "mismatch" };
  const close = tag.next;
  if (!close || !close.closing || close.name !== tag.name) return { ok: false, code: "unmappable" };
  return { ok: true, content: source.slice(0, tag.end) + escapeText(edit.after) + source.slice(close.start) };
}

/** Applies one edit to the source of a page. The element the person picked is
 *  found again in the file, not trusted from the page: the page may have been
 *  changed by its own scripts since the file was written. */
export function patchSource(source: string, target: Target, edit: SourceEdit): Patched {
  if (edit.kind === "style" && !SAFE_VALUE.test(edit.value)) return { ok: false, code: "bad_value" };
  const scanned = scan(source);
  if (!scanned) return { ok: false, code: "unmappable" };
  const doc = new DOMParser().parseFromString(source, "text/html");
  let found: Element[];
  try {
    found = [...doc.querySelectorAll(target.selector)];
  } catch {
    return { ok: false, code: "not_found" };
  }
  const el = found[0];
  if (!el || el.localName.toLowerCase() !== target.tagName.toLowerCase()) return { ok: false, code: "not_found" };
  if (found.length > 1) return { ok: false, code: "ambiguous" };
  const tags = align([...doc.querySelectorAll("*")], scanned.starts);
  const tag = tags?.get(el);
  if (!tag) return { ok: false, code: "unmappable" };
  return edit.kind === "text" ? withText(el, tag, source, edit) : withStyle(el, tag, source, edit.property, edit.value);
}

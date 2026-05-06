/** web_search uses Mojeek (DDG returns anti-bot 202 to unauthenticated POSTs); web_fetch sniffs HTML to text. */

import { parse as parseHtml } from "node-html-parser";
import type { ToolRegistry } from "../tools.js";

export interface SearchResult {
  title: string;
  url: string;
  snippet: string;
}

export interface PageContent {
  url: string;
  title?: string;
  text: string;
  /** True when the extracted text was clipped to fit the cap. */
  truncated: boolean;
}

export interface WebFetchOptions {
  /** Max bytes of extracted text. Defaults to 32_000 to match tool-result cap. */
  maxChars?: number;
  /** Timeout in ms. Defaults to 15_000. */
  timeoutMs?: number;
  signal?: AbortSignal;
}

export interface WebSearchOptions {
  topK?: number;
  signal?: AbortSignal;
}

const DEFAULT_FETCH_MAX_CHARS = 32_000;
const DEFAULT_FETCH_TIMEOUT_MS = 15_000;
const DEFAULT_TOPK = 5;
/** Bytes cap applied before `resp.text()` — char cap can't fire until the body is fully buffered. */
const FETCH_MAX_BYTES = 10 * 1024 * 1024;
// Real-browser UA. Servers like Mojeek are bot-friendly but still gate
// obvious scraper UAs; a stock Chrome string avoids the fast-path block.
const USER_AGENT =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";
const MOJEEK_ENDPOINT = "https://www.mojeek.com/search";

/** Distinguishes "truly 0 results" from "layout changed / blocked" so callers can tell. */
export async function webSearch(
  query: string,
  opts: WebSearchOptions = {},
): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(10, opts.topK ?? DEFAULT_TOPK));
  const resp = await fetch(`${MOJEEK_ENDPOINT}?q=${encodeURIComponent(query)}`, {
    headers: {
      "User-Agent": USER_AGENT,
      Accept: "text/html,application/xhtml+xml,application/xml;q=0.9",
      "Accept-Language": "en-US,en;q=0.9",
    },
    signal: opts.signal,
    redirect: "follow",
  });
  if (!resp.ok) throw new Error(`web_search ${resp.status}`);
  const html = await resp.text();
  const results = parseMojeekResults(html).slice(0, topK);
  if (results.length === 0) {
    if (/no results found|did not match any documents/i.test(html)) return [];
    if (/captcha|verify you are human|access denied|forbidden/i.test(html)) {
      throw new Error("web_search: Mojeek anti-bot page — rate-limited or blocked");
    }
    throw new Error(
      `web_search: 0 results but response doesn't look like a real empty page (${html.length} chars, first 120: ${html.slice(0, 120).replace(/\s+/g, " ")})`,
    );
  }
  return results;
}

/** Title-anchor + snippet-paragraph passes paired positionally — robust to attribute reorder. */
export function parseMojeekResults(html: string): SearchResult[] {
  const titles: string[] = [];
  const titleAnchorRe = /<a\b[^>]*\bclass="title"[^>]*>[\s\S]*?<\/a>/g;
  let m: RegExpExecArray | null;
  while (true) {
    m = titleAnchorRe.exec(html);
    if (m === null) break;
    titles.push(m[0]);
  }

  const snippets: string[] = [];
  const snippetRe = /<p\b[^>]*\bclass="s"[^>]*>([\s\S]*?)<\/p>/g;
  while (true) {
    m = snippetRe.exec(html);
    if (m === null) break;
    snippets.push(m[1] ?? "");
  }

  const hrefRe = /href="([^"]+)"/;
  const innerRe = /<a\b[^>]*>([\s\S]*?)<\/a>/;
  const results: SearchResult[] = [];
  for (let i = 0; i < titles.length; i++) {
    const anchor = titles[i]!;
    const hrefMatch = anchor.match(hrefRe);
    const innerMatch = anchor.match(innerRe);
    if (!hrefMatch?.[1]) continue;
    results.push({
      title: decodeHtmlEntities(stripHtml(innerMatch?.[1] ?? "")).trim(),
      url: hrefMatch[1],
      snippet: decodeHtmlEntities(stripHtml(snippets[i] ?? ""))
        .replace(/\s+/g, " ")
        .trim(),
    });
  }
  return results;
}

export async function webFetch(url: string, opts: WebFetchOptions = {}): Promise<PageContent> {
  const maxChars = opts.maxChars ?? DEFAULT_FETCH_MAX_CHARS;
  const timeoutMs = opts.timeoutMs ?? DEFAULT_FETCH_TIMEOUT_MS;
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeoutMs);
  // Forward the caller's abort too so an Esc during a long fetch is respected.
  const cancel = () => ctl.abort();
  opts.signal?.addEventListener("abort", cancel, { once: true });
  let resp: Response;
  try {
    resp = await fetch(url, {
      headers: { "User-Agent": USER_AGENT, Accept: "text/html,text/plain,*/*" },
      signal: ctl.signal,
      redirect: "follow",
    });
  } finally {
    clearTimeout(timer);
    opts.signal?.removeEventListener("abort", cancel);
  }
  if (!resp.ok) throw new Error(`web_fetch ${resp.status} for ${url}`);
  const contentType = resp.headers.get("content-type") ?? "";
  // Pre-check Content-Length when the server provides it. Cheaper to
  // refuse upfront than to start streaming a 1GB ISO.
  const declaredLen = Number(resp.headers.get("content-length") ?? "");
  if (Number.isFinite(declaredLen) && declaredLen > FETCH_MAX_BYTES) {
    throw new Error(
      `web_fetch refused: content-length ${declaredLen} bytes exceeds ${FETCH_MAX_BYTES}-byte cap (${url})`,
    );
  }
  const raw = await readBodyCapped(resp, FETCH_MAX_BYTES);
  const title = extractTitle(raw);
  const text = contentType.includes("text/html") ? htmlToText(raw) : raw;
  const truncated = text.length > maxChars;
  const finalText = truncated
    ? `${text.slice(0, maxChars)}\n\n[… truncated ${text.length - maxChars} chars …]`
    : text;
  return { url, title, text: finalText, truncated };
}

/** Streams + caps so chunked responses (or servers lying about Content-Length) can't balloon the heap. */
async function readBodyCapped(resp: Response, maxBytes: number): Promise<string> {
  if (!resp.body) return await resp.text();
  const reader = resp.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let total = 0;
  let out = "";
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maxBytes) {
        try {
          await reader.cancel();
        } catch {
          /* already torn down */
        }
        throw new Error(
          `web_fetch refused: response body exceeded ${maxBytes}-byte cap (${total} bytes seen)`,
        );
      }
      out += decoder.decode(value, { stream: true });
    }
    out += decoder.decode();
  } finally {
    try {
      reader.releaseLock();
    } catch {
      /* reader already cancelled / released */
    }
  }
  return out;
}

/** Hard cap so the per-request HTML budget stays linear-time even on adversarial pages. */
const MAX_HTML_INPUT = 5 * 1024 * 1024;

const STRIP_BLOCK_TAGS = "script, style, noscript, nav, footer, aside, svg";

/** Block-level tags that should produce a paragraph break in the extracted text. */
const BLOCK_BREAK_TAGS = new Set([
  "p",
  "div",
  "br",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "li",
  "tr",
  "section",
  "article",
]);

export function htmlToText(html: string): string {
  const input = html.length > MAX_HTML_INPUT ? html.slice(0, MAX_HTML_INPUT) : html;
  // Real HTML parser — sidesteps the well-known regex anti-patterns
  // (`<X[\s\S]*?</X>`, `<[^>]+>`) CodeQL flags as bad-tag-filter and
  // incomplete-multi-character-sanitization.
  const root = parseHtml(input);
  for (const node of root.querySelectorAll(STRIP_BLOCK_TAGS)) node.remove();

  const out: string[] = [];
  walkExtract(root, out);
  let s = out.join("");
  s = decodeHtmlEntities(s);
  s = s.replace(/[ \t]+/g, " ");
  s = s.replace(/\n[ \t]+/g, "\n");
  s = s.replace(/\n{3,}/g, "\n\n");
  return s.trim();
}

interface WalkableNode {
  nodeType: number;
  rawText?: string;
  text?: string;
  rawTagName?: string;
  childNodes: WalkableNode[];
}

function walkExtract(node: WalkableNode, out: string[]): void {
  // nodeType 3 = TEXT_NODE; 1 = ELEMENT_NODE per node-html-parser.
  if (node.nodeType === 3) {
    out.push(node.rawText ?? node.text ?? "");
    return;
  }
  const tag = node.rawTagName?.toLowerCase();
  const isBreak = tag !== undefined && BLOCK_BREAK_TAGS.has(tag);
  if (isBreak) out.push("\n");
  for (const child of node.childNodes) walkExtract(child, out);
  if (isBreak) out.push("\n");
}

function stripHtml(s: string): string {
  return parseHtml(s).text;
}

const HTML_ENTITIES: Readonly<Record<string, string>> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  nbsp: " ",
};

/** Single-pass decode — the previous chained `replace`s decoded `&amp;lt;` into `<` because `&amp;` ran before `&lt;`. */
function decodeHtmlEntities(s: string): string {
  return s.replace(/&(#\d+|#x[0-9a-fA-F]+|\w+);/g, (raw, name: string) => {
    if (name.startsWith("#x") || name.startsWith("#X")) {
      const code = Number.parseInt(name.slice(2), 16);
      return Number.isFinite(code) ? String.fromCodePoint(code) : raw;
    }
    if (name.startsWith("#")) {
      const code = Number.parseInt(name.slice(1), 10);
      return Number.isFinite(code) ? String.fromCodePoint(code) : raw;
    }
    return HTML_ENTITIES[name.toLowerCase()] ?? raw;
  });
}

function extractTitle(html: string): string | undefined {
  const m = html.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
  if (!m?.[1]) return undefined;
  return m[1].replace(/\s+/g, " ").trim() || undefined;
}

export interface WebToolsOptions {
  /** Default top-K for `web_search` when the model doesn't specify. */
  defaultTopK?: number;
  /** Byte cap for `web_fetch` extracted text. */
  maxFetchChars?: number;
}

export function registerWebTools(registry: ToolRegistry, opts: WebToolsOptions = {}): ToolRegistry {
  const defaultTopK = opts.defaultTopK ?? DEFAULT_TOPK;
  const maxFetchChars = opts.maxFetchChars ?? DEFAULT_FETCH_MAX_CHARS;

  registry.register({
    name: "web_search",
    description:
      "Search the public web. Returns ranked results with title, url, and snippet. Call this when the answer's correctness depends on current state — anything that changes over time (events, prices, releases, status of a thing in the real world). Composing such answers from training memory invents stale numbers; search first, then ground the answer in the results. For evergreen / definitional questions you don't need this.",
    readOnly: true,
    parallelSafe: true,
    parameters: {
      type: "object",
      properties: {
        query: { type: "string", description: "Natural-language search query." },
        topK: {
          type: "integer",
          description: `Number of results to return (1..10). Default ${defaultTopK}.`,
        },
      },
      required: ["query"],
    },
    fn: async (args: { query: string; topK?: number }, ctx) => {
      const results = await webSearch(args.query, {
        topK: args.topK ?? defaultTopK,
        signal: ctx?.signal,
      });
      return formatSearchResults(args.query, results);
    },
  });

  registry.register({
    name: "web_fetch",
    description:
      "Download a URL and return its visible text content (HTML pages get scripts/styles/nav stripped). Truncated at the tool-result cap. Use after web_search when a snippet isn't enough.",
    readOnly: true,
    parallelSafe: true,
    parameters: {
      type: "object",
      properties: {
        url: { type: "string", description: "Absolute http:// or https:// URL." },
      },
      required: ["url"],
    },
    fn: async (args: { url: string }, ctx) => {
      if (!/^https?:\/\//i.test(args.url)) {
        throw new Error("web_fetch: url must start with http:// or https://");
      }
      const page = await webFetch(args.url, { maxChars: maxFetchChars, signal: ctx?.signal });
      const header = page.title ? `${page.title}\n${page.url}` : page.url;
      return `${header}\n\n${page.text}`;
    },
  });

  return registry;
}

export function formatSearchResults(query: string, results: SearchResult[]): string {
  const lines: string[] = [`query: ${query}`, `\nresults (${results.length}):`];
  results.forEach((r, i) => {
    lines.push(`\n${i + 1}. ${r.title}`);
    lines.push(`   ${r.url}`);
    if (r.snippet) lines.push(`   ${r.snippet}`);
  });
  return lines.join("\n");
}

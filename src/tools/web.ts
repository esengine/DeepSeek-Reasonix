/** web_search uses Bing (cn.bing.com — works from CN without proxy); web_fetch sniffs HTML to text. */

import { lookup } from "node:dns/promises";
import { isIP } from "node:net";
import { parse as parseHtml } from "node-html-parser";
import {
  loadExaApiKey,
  loadMetasoApiKey,
  loadOllamaApiKey,
  loadPerplexityApiKey,
  loadTavilyApiKey,
  webSearchEndpoint as loadWebSearchEndpoint,
  webSearchEngine as loadWebSearchEngine,
} from "../config.js";
import { t } from "../i18n/index.js";
import type { ToolRegistry } from "../tools.js";

export interface SearchResult {
  title: string;
  url: string;
  snippet: string;
  /** AI-generated answer text — set by AI-native engines (Perplexity, Exa); undefined for traditional engines. */
  answer?: string;
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
  /** Config path for provider-specific keys. Defaults to ~/.reasonix/config.json. */
  configPath?: string;
  signal?: AbortSignal;
}

export interface WebSearchOptions {
  topK?: number;
  signal?: AbortSignal;
  /** Config path for provider-specific keys. Defaults to ~/.reasonix/config.json. */
  configPath?: string;
  /** Backend engine: "bing" (scrapes cn.bing.com HTML — default, works from CN without proxy), "searxng" (self-hosted SearXNG), "metaso" (Metaso API), "tavily" (LLM-friendly JSON API), "perplexity" (Perplexity AI), "exa" (Exa API), or "ollama" (Ollama cloud web search). */
  engine?: "bing" | "searxng" | "metaso" | "tavily" | "perplexity" | "exa" | "ollama";
  /** Base URL for SearXNG. Default http://localhost:8080. */
  endpoint?: string;
}

const DEFAULT_FETCH_MAX_CHARS = 32_000;
const DEFAULT_FETCH_TIMEOUT_MS = 15_000;
const DEFAULT_TOPK = 5;
/** Bytes cap applied before `resp.text()` — char cap can't fire until the body is fully buffered. */
const FETCH_MAX_BYTES = 10 * 1024 * 1024;
// Real-browser UA. Most search backends gate obvious scraper UAs; a stock
// Chrome string clears the fast-path block.
const USER_AGENT =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";
// cn.bing.com over www.bing.com — CN endpoint returns raw URLs in the
// HTML; the international endpoint wraps them in `bing.com/ck/a?u=a1<base64>`
// click-tracking redirects we'd have to decode per result.
const BING_ENDPOINT = "https://cn.bing.com/search";
const METASO_ENDPOINT = "https://metaso.cn/api/v1";
const TAVILY_ENDPOINT = "https://api.tavily.com/search";
const PERPLEXITY_ENDPOINT = "https://api.perplexity.ai/chat/completions";
const EXA_ENDPOINT = "https://api.exa.ai/answer";
const OLLAMA_WEB_SEARCH_ENDPOINT = "https://ollama.com/api/web_search";
const OLLAMA_WEB_FETCH_ENDPOINT = "https://ollama.com/api/web_fetch";
const FETCH_MAX_REDIRECTS = 5;

/** Pick a status-specific webErrors key so the model gets an actionable hint, not a bare status. */
function searchStatusError(status: number): string {
  if (status === 429) return t("webErrors.rateLimit429");
  if (status === 403) return t("webErrors.forbidden403");
  if (status >= 500 && status <= 599) return t("webErrors.serverError5xx", { status });
  return t("webErrors.status", { status });
}

function fetchStatusError(status: number, url: string): string {
  if (status === 429) return t("webErrors.fetchRateLimit429", { url });
  if (status === 403) return t("webErrors.fetchForbidden403", { url });
  if (status >= 500 && status <= 599) return t("webErrors.fetchServerError5xx", { status, url });
  return t("webErrors.fetchStatus", { status, url });
}

function parseIpv4(address: string): number | null {
  const parts = address.split(".");
  if (parts.length !== 4) return null;
  let out = 0;
  for (const part of parts) {
    if (!/^\d+$/.test(part)) return null;
    const n = Number(part);
    if (!Number.isInteger(n) || n < 0 || n > 255) return null;
    out = (out << 8) + n;
  }
  return out >>> 0;
}

function ipv4InRange(value: number, base: string, bits: number): boolean {
  const parsed = parseIpv4(base);
  if (parsed === null) return false;
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  return (value & mask) === (parsed & mask);
}

function isPrivateIpv4(address: string): boolean {
  const value = parseIpv4(address);
  if (value === null) return false;
  return (
    ipv4InRange(value, "0.0.0.0", 8) ||
    ipv4InRange(value, "10.0.0.0", 8) ||
    ipv4InRange(value, "100.64.0.0", 10) ||
    ipv4InRange(value, "127.0.0.0", 8) ||
    ipv4InRange(value, "169.254.0.0", 16) ||
    ipv4InRange(value, "172.16.0.0", 12) ||
    ipv4InRange(value, "192.0.0.0", 24) ||
    ipv4InRange(value, "192.0.2.0", 24) ||
    ipv4InRange(value, "192.168.0.0", 16) ||
    ipv4InRange(value, "198.18.0.0", 15) ||
    ipv4InRange(value, "198.51.100.0", 24) ||
    ipv4InRange(value, "203.0.113.0", 24) ||
    ipv4InRange(value, "224.0.0.0", 4) ||
    ipv4InRange(value, "240.0.0.0", 4)
  );
}

function normalizeIpv6(address: string): string {
  return address.toLowerCase().replace(/(^|:)0+([0-9a-f])/g, "$1$2");
}

function isPrivateIpv6(address: string): boolean {
  const normalized = normalizeIpv6(address);
  const mapped = /^::ffff:(?:0+:)?(\d+\.\d+\.\d+\.\d+)$/i.exec(normalized);
  if (mapped) return isPrivateIpv4(mapped[1]!);
  return (
    normalized === "::" ||
    normalized === "::1" ||
    normalized.startsWith("fc") ||
    normalized.startsWith("fd") ||
    normalized.startsWith("fe8") ||
    normalized.startsWith("fe9") ||
    normalized.startsWith("fea") ||
    normalized.startsWith("feb") ||
    normalized.startsWith("ff")
  );
}

function isInternalAddress(address: string): boolean {
  const family = isIP(address);
  if (family === 4) return isPrivateIpv4(address);
  if (family === 6) return isPrivateIpv6(address);
  return false;
}

async function assertPublicHttpUrl(rawUrl: string): Promise<URL> {
  const url = new URL(rawUrl);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error(`web_fetch refuses non-HTTP URL: ${url.protocol}`);
  }

  const host = url.hostname;
  const literal = isIP(host);
  const addresses = literal
    ? [host]
    : (await lookup(host, { all: true, verbatim: true })).map((entry) => entry.address);
  if (addresses.length === 0 || addresses.some(isInternalAddress)) {
    throw new Error(`web_fetch refuses internal or reserved host: ${host}`);
  }
  return url;
}

function redirectLocation(resp: Response, currentUrl: string): string | null {
  if (resp.status < 300 || resp.status > 399) return null;
  const location = resp.headers.get("location");
  if (!location) return null;
  return new URL(location, currentUrl).toString();
}

/** Distinguishes "truly 0 results" from "layout changed / blocked" so callers can tell. */
export async function webSearch(
  query: string,
  opts: WebSearchOptions = {},
): Promise<SearchResult[]> {
  if (opts.engine === "metaso") {
    return searchMetaso(query, opts);
  }
  if (opts.engine === "searxng") {
    return searchSearxng(query, opts);
  }
  if (opts.engine === "tavily") {
    return searchTavily(query, opts);
  }
  if (opts.engine === "perplexity") {
    return searchPerplexity(query, opts);
  }
  if (opts.engine === "exa") {
    return searchExa(query, opts);
  }
  if (opts.engine === "ollama") {
    return searchOllama(query, opts);
  }
  return searchBing(query, opts);
}

async function searchBing(query: string, opts: WebSearchOptions = {}): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(10, opts.topK ?? DEFAULT_TOPK));
  const resp = await fetch(`${BING_ENDPOINT}?q=${encodeURIComponent(query)}`, {
    headers: {
      "User-Agent": USER_AGENT,
      Accept: "text/html,application/xhtml+xml,application/xml;q=0.9",
      "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
    },
    signal: opts.signal,
    redirect: "follow",
  });
  if (!resp.ok) throw new Error(searchStatusError(resp.status));
  const html = await resp.text();
  const results = parseBingResults(html).slice(0, topK);
  if (results.length === 0) {
    if (/no results found|did not match any documents/i.test(html)) return [];
    if (/captcha|verify you are human|access denied|forbidden/i.test(html)) {
      throw new Error(t("webErrors.bingBlocked"));
    }
    throw new Error(
      t("webErrors.bingNoResults", {
        chars: html.length,
        preview: html.slice(0, 120).replace(/\s+/g, " "),
      }),
    );
  }
  return results;
}

/** Parse + validate a SearXNG endpoint. Returns origin (protocol + host). */
function normalizeSearxngEndpoint(raw: string): string {
  let url: URL;
  try {
    url = new URL(raw.includes("://") ? raw : `http://${raw}`);
  } catch {
    throw new Error(t("webErrors.invalidEndpoint", { endpoint: raw }));
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error(t("webErrors.endpointMustBeHttp", { protocol: url.protocol }));
  }
  return url.origin;
}

async function searchSearxng(query: string, opts: WebSearchOptions = {}): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(10, opts.topK ?? DEFAULT_TOPK));
  const baseUrl = normalizeSearxngEndpoint(opts.endpoint ?? "http://localhost:8080");

  // JSON API is often blocked by SearXNG's default limiter; HTML always works.
  const url = `${baseUrl}/search?format=html&q=${encodeURIComponent(query)}`;
  let resp: Response;
  try {
    resp = await fetch(url, {
      headers: {
        "User-Agent": USER_AGENT,
        Accept: "text/html",
      },
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof TypeError && (err as Error).message.includes("fetch")) {
      throw new Error(
        t("webErrors.cannotReach", { endpoint: opts.endpoint ?? "http://localhost:8080" }),
      );
    }
    throw err;
  }
  if (!resp.ok) throw new Error(searchStatusError(resp.status));
  const html = await resp.text();
  const results = parseSearxngHtmlResults(html).slice(0, topK);
  if (results.length === 0) {
    if (/no results found|did not match any documents/i.test(html)) return [];
    throw new Error(t("webErrors.searxngNoResults", { chars: html.length }));
  }
  return results;
}

interface MetasoWebpage {
  title: string;
  link: string;
  snippet?: string;
  summary?: string;
  score?: string;
  position?: number;
  date?: string;
}

interface MetasoSearchResponse {
  credits?: number;
  total?: number;
  webpages?: MetasoWebpage[];
  code?: number;
  message?: string;
}

async function searchMetaso(query: string, opts: WebSearchOptions = {}): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(100, opts.topK ?? DEFAULT_TOPK));
  const apiKey = loadMetasoApiKey();
  if (!apiKey) throw new Error(t("webErrors.metasoMissingKey"));

  let resp: Response;
  try {
    resp = await fetch(`${METASO_ENDPOINT}/search`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify({
        q: query,
        scope: "webpage",
        size: topK,
      }),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof TypeError && (err as Error).message.includes("fetch")) {
      throw new Error(t("webErrors.cannotReach", { endpoint: METASO_ENDPOINT }));
    }
    throw err;
  }

  const raw = await resp.text();
  let data: MetasoSearchResponse;
  try {
    data = JSON.parse(raw) as MetasoSearchResponse;
  } catch {
    throw new Error(t("webErrors.metasoParseError", { status: resp.status }));
  }

  if (!resp.ok) {
    if (resp.status === 401 || resp.status === 403) {
      throw new Error(t("webErrors.metasoUnauthorized"));
    }
    if (resp.status === 429) {
      throw new Error(t("webErrors.metasoRateLimit"));
    }
    throw new Error(t("webErrors.metasoServerError", { status: resp.status }));
  }

  if (data.code === 3003) {
    throw new Error(t("webErrors.metasoDailyLimit"));
  }
  if (data.code === 2005) {
    throw new Error(t("webErrors.metasoUnauthorized"));
  }
  if (data.code && data.code !== 0) {
    throw new Error(
      t("webErrors.metasoApiError", { code: data.code, message: data.message ?? "" }),
    );
  }

  const webpages = data.webpages ?? [];
  if (webpages.length === 0) {
    return [];
  }

  return webpages.slice(0, topK).map((wp) => ({
    title: wp.title,
    url: wp.link,
    snippet: wp.snippet ?? wp.summary ?? "",
  }));
}

interface TavilyResultItem {
  title: string;
  url: string;
  content?: string;
  score?: number;
}

interface TavilySearchResponse {
  results?: TavilyResultItem[];
  // Tavily error responses use { detail: { error: "..." } } shape.
  detail?: { error?: string } | string;
}

async function searchTavily(query: string, opts: WebSearchOptions = {}): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(20, opts.topK ?? DEFAULT_TOPK));
  const apiKey = loadTavilyApiKey();
  if (!apiKey) throw new Error(t("webErrors.tavilyMissingKey"));

  let resp: Response;
  try {
    resp = await fetch(TAVILY_ENDPOINT, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({
        api_key: apiKey,
        query,
        search_depth: "basic",
        max_results: topK,
        include_answer: false,
        include_raw_content: false,
        include_images: false,
      }),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof TypeError && (err as Error).message.includes("fetch")) {
      throw new Error(t("webErrors.cannotReach", { endpoint: TAVILY_ENDPOINT }));
    }
    throw err;
  }

  if (!resp.ok) {
    if (resp.status === 401 || resp.status === 403) {
      throw new Error(t("webErrors.tavilyUnauthorized"));
    }
    if (resp.status === 429) throw new Error(t("webErrors.tavilyRateLimit"));
    throw new Error(t("webErrors.tavilyServerError", { status: resp.status }));
  }

  let data: TavilySearchResponse;
  try {
    data = (await resp.json()) as TavilySearchResponse;
  } catch {
    throw new Error(t("webErrors.tavilyParseError", { status: resp.status }));
  }

  const results = data.results ?? [];
  return results.slice(0, topK).map((r) => ({
    title: r.title,
    url: r.url,
    snippet: r.content ?? "",
  }));
}

interface PerplexityChoice {
  message?: { content?: string };
}

interface PerplexityResponse {
  choices?: PerplexityChoice[];
  citations?: unknown[];
}

async function searchPerplexity(
  query: string,
  opts: WebSearchOptions = {},
): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(20, opts.topK ?? DEFAULT_TOPK));
  const apiKey = loadPerplexityApiKey();
  if (!apiKey) throw new Error(t("webErrors.perplexityMissingKey"));

  let resp: Response;
  try {
    resp = await fetch(PERPLEXITY_ENDPOINT, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: "sonar",
        messages: [{ role: "user", content: query }],
        max_tokens: 1024,
        return_related_questions: false,
      }),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof TypeError && (err as Error).message.includes("fetch")) {
      throw new Error(t("webErrors.cannotReach", { endpoint: PERPLEXITY_ENDPOINT }));
    }
    throw err;
  }

  if (!resp.ok) {
    if (resp.status === 401 || resp.status === 403) {
      throw new Error(t("webErrors.perplexityUnauthorized"));
    }
    if (resp.status === 429) throw new Error(t("webErrors.perplexityRateLimit"));
    throw new Error(t("webErrors.perplexityServerError", { status: resp.status }));
  }

  const raw = await resp.text();
  let data: PerplexityResponse;
  try {
    data = JSON.parse(raw) as PerplexityResponse;
  } catch {
    throw new Error(t("webErrors.perplexityParseError", { status: resp.status }));
  }

  const answer = data.choices?.[0]?.message?.content ?? "";
  const citations = Array.isArray(data.citations) ? data.citations : [];

  const results: SearchResult[] = [];

  // First entry carries the AI answer
  if (answer) {
    results.push({ title: answer, url: "", snippet: "", answer });
  }

  const count = Math.min(citations.length, topK);
  for (let i = 0; i < count; i++) {
    const c = citations[i];
    if (typeof c === "string") {
      results.push({ title: `Source ${i + 1}`, url: c, snippet: "" });
    } else if (
      c &&
      typeof c === "object" &&
      typeof (c as Record<string, unknown>).url === "string"
    ) {
      const item = c as Record<string, unknown>;
      results.push({
        title: typeof item.title === "string" ? item.title : `Source ${i + 1}`,
        url: item.url as string,
        snippet: "",
      });
    }
  }

  return results;
}

interface ExaCitation {
  url?: string;
  title?: string;
  text?: string;
  publishedDate?: string;
}

interface ExaAnswerResponse {
  answer?: string;
  citations?: ExaCitation[];
}

async function searchExa(query: string, opts: WebSearchOptions = {}): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(20, opts.topK ?? DEFAULT_TOPK));
  const apiKey = loadExaApiKey();
  if (!apiKey) throw new Error(t("webErrors.exaMissingKey"));

  let resp: Response;
  try {
    resp = await fetch(EXA_ENDPOINT, {
      method: "POST",
      headers: {
        "x-api-key": apiKey,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ query, text: true }),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof TypeError && (err as Error).message.includes("fetch")) {
      throw new Error(t("webErrors.cannotReach", { endpoint: EXA_ENDPOINT }));
    }
    throw err;
  }

  if (!resp.ok) {
    if (resp.status === 401 || resp.status === 403) {
      throw new Error(t("webErrors.exaUnauthorized"));
    }
    if (resp.status === 429) throw new Error(t("webErrors.exaRateLimit"));
    throw new Error(t("webErrors.exaServerError", { status: resp.status }));
  }

  const raw = await resp.text();
  let data: ExaAnswerResponse;
  try {
    data = JSON.parse(raw) as ExaAnswerResponse;
  } catch {
    throw new Error(t("webErrors.exaParseError", { status: resp.status }));
  }

  const answer = data.answer ?? "";
  const citations = data.citations ?? [];

  const results: SearchResult[] = [];

  // First entry carries the AI answer
  if (answer) {
    results.push({ title: answer, url: "", snippet: "", answer });
  }

  const count = Math.min(citations.length, topK);
  for (let i = 0; i < count; i++) {
    const c = citations[i]!;
    if (!c.url) continue;
    results.push({
      title: c.title || `Source ${i + 1}`,
      url: c.url,
      snippet: c.text ?? "",
    });
  }

  return results;
}

interface OllamaSearchItem {
  title?: string;
  url?: string;
  content?: string;
}

interface OllamaSearchResponse {
  results?: OllamaSearchItem[];
}

async function searchOllama(query: string, opts: WebSearchOptions = {}): Promise<SearchResult[]> {
  const topK = Math.max(1, Math.min(10, opts.topK ?? DEFAULT_TOPK));
  const apiKey = loadOllamaApiKey(opts.configPath);
  if (!apiKey) {
    throw new Error(
      "web_search: Ollama web search requires an API key — set OLLAMA_API_KEY, `ollamaApiKey`, or use /search-engine ollama <key>.",
    );
  }

  let resp: Response;
  try {
    resp = await fetch(OLLAMA_WEB_SEARCH_ENDPOINT, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({ query, max_results: topK }),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof TypeError && (err as Error).message.includes("fetch")) {
      throw new Error(t("webErrors.cannotReach", { endpoint: OLLAMA_WEB_SEARCH_ENDPOINT }));
    }
    throw err;
  }

  if (!resp.ok) {
    if (resp.status === 401 || resp.status === 403) {
      throw new Error("web_search: Ollama API key rejected — check OLLAMA_API_KEY.");
    }
    if (resp.status === 429) {
      throw new Error("web_search: Ollama web search is rate-limited or quota-limited.");
    }
    throw new Error(`web_search: Ollama web search returned HTTP ${resp.status}.`);
  }

  let data: OllamaSearchResponse;
  try {
    data = (await resp.json()) as OllamaSearchResponse;
  } catch {
    throw new Error(`web_search: Ollama returned unparseable response (HTTP ${resp.status}).`);
  }

  return (data.results ?? []).slice(0, topK).map((r, i) => ({
    title: r.title || `Result ${i + 1}`,
    url: r.url || "",
    snippet: r.content ?? "",
  }));
}

interface OllamaFetchResponse {
  title?: string;
  content?: string;
  links?: string[];
}

async function webFetchOllama(
  url: string,
  opts: WebFetchOptions = {},
): Promise<PageContent & { links?: string[] }> {
  const apiKey = loadOllamaApiKey(opts.configPath);
  if (!apiKey) {
    throw new Error(
      "web_fetch: Ollama web fetch requires an API key — set OLLAMA_API_KEY, `ollamaApiKey`, or use /search-engine ollama <key>.",
    );
  }

  const ctrl = new AbortController();
  const timeout = setTimeout(
    () =>
      ctrl.abort(
        new Error(
          t("webErrors.fetchTimeout", { ms: opts.timeoutMs ?? DEFAULT_FETCH_TIMEOUT_MS, url }),
        ),
      ),
    opts.timeoutMs ?? DEFAULT_FETCH_TIMEOUT_MS,
  );
  const signal = opts.signal ? AbortSignal.any([opts.signal, ctrl.signal]) : ctrl.signal;

  let resp: Response;
  try {
    resp = await fetch(OLLAMA_WEB_FETCH_ENDPOINT, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({ url }),
      signal,
    });
  } finally {
    clearTimeout(timeout);
  }

  if (!resp.ok) {
    if (resp.status === 401 || resp.status === 403) {
      throw new Error("web_fetch: Ollama API key rejected — check OLLAMA_API_KEY.");
    }
    if (resp.status === 429) {
      throw new Error("web_fetch: Ollama web fetch is rate-limited or quota-limited.");
    }
    throw new Error(`web_fetch: Ollama web fetch returned HTTP ${resp.status} for ${url}.`);
  }

  let data: OllamaFetchResponse;
  try {
    data = (await resp.json()) as OllamaFetchResponse;
  } catch {
    throw new Error(`web_fetch: Ollama returned unparseable response for ${url}.`);
  }

  const maxChars = opts.maxChars ?? DEFAULT_FETCH_MAX_CHARS;
  const text = data.content ?? "";
  return {
    url,
    title: data.title,
    text: text.length > maxChars ? text.slice(0, maxChars) : text,
    truncated: text.length > maxChars,
    links: data.links,
  };
}

/** Parse SearXNG HTML search results using node-html-parser. */
export function parseSearxngHtmlResults(html: string): SearchResult[] {
  const root = parseHtml(html);
  const results: SearchResult[] = [];

  // Try <article class="result"> first (default SearXNG theme)
  const articles = root.querySelectorAll("article.result, div.result");
  if (articles.length > 0) {
    for (const article of articles) {
      const link = article.querySelector("h3 a, h4 a, a[href^='http']");
      if (!link) continue;
      const href = link.getAttribute("href");
      if (!href) continue;
      const title = link.textContent.trim();
      if (!title) continue;
      let snippet = "";
      for (const p of article.querySelectorAll("p")) {
        const text = p.textContent.trim();
        if (text.length > 10 && !text.includes(title)) {
          snippet = text;
          break;
        }
      }
      if (!snippet) {
        const cs = article.querySelector(".content, .result-content, [class*='snippet']");
        if (cs) snippet = cs.textContent.trim();
      }
      results.push({ title, url: href, snippet });
    }
    return results;
  }

  // Fallback: <h3><a href> pairs directly
  for (const a of root.querySelectorAll("h3 a[href]")) {
    const href = a.getAttribute("href");
    if (!href || href.startsWith("#")) continue;
    const title = a.textContent.trim();
    if (!title) continue;
    let snippet = "";
    const p = a.parentNode?.parentNode?.querySelector("p");
    if (p) snippet = p.textContent.trim();
    results.push({ title, url: href, snippet });
  }
  return results;
}

/** Title-anchor + snippet-paragraph passes paired positionally — robust to attribute reorder. */
export function parseBingResults(html: string): SearchResult[] {
  // DOM walk rather than regex — `<li[^>]*\bclass\b[^>]*>` triggers
  // polynomial backtracking on adversarial input (CodeQL js/polynomial-redos).
  const root = parseHtml(html);
  const results: SearchResult[] = [];
  for (const li of root.querySelectorAll("li.b_algo")) {
    const anchor = li.querySelector("h2 a[href]");
    if (!anchor) continue;
    const href = anchor.getAttribute("href");
    if (!href) continue;
    const title = anchor.textContent.trim();
    if (!title) continue;
    const cap = li.querySelector("div.b_caption p");
    const snippet = cap ? cap.textContent.trim().replace(/\s+/g, " ") : "";
    results.push({ title, url: href, snippet });
  }
  return results;
}

export async function webFetch(url: string, opts: WebFetchOptions = {}): Promise<PageContent> {
  const maxChars = opts.maxChars ?? DEFAULT_FETCH_MAX_CHARS;
  const timeoutMs = opts.timeoutMs ?? DEFAULT_FETCH_TIMEOUT_MS;
  const ctl = new AbortController();
  // Track whether the abort came from our internal timer vs the caller's
  // signal — only the timer-driven abort should produce a "timed out" hint.
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    ctl.abort();
  }, timeoutMs);
  // Forward the caller's abort too so an Esc during a long fetch is respected.
  const cancel = () => ctl.abort();
  opts.signal?.addEventListener("abort", cancel, { once: true });
  let resp: Response;
  let currentUrl = url;
  try {
    for (let redirects = 0; ; redirects++) {
      const parsed = await assertPublicHttpUrl(currentUrl);
      if (ctl.signal.aborted) throw new DOMException("aborted", "AbortError");
      resp = await fetch(parsed, {
        headers: { "User-Agent": USER_AGENT, Accept: "text/html,text/plain,*/*" },
        signal: ctl.signal,
        redirect: "manual",
      });
      const nextUrl = redirectLocation(resp, parsed.toString());
      if (!nextUrl) break;
      if (redirects >= FETCH_MAX_REDIRECTS) {
        throw new Error(`web_fetch redirect limit exceeded for ${url}`);
      }
      currentUrl = nextUrl;
    }
  } catch (err) {
    if (timedOut) {
      throw new Error(t("webErrors.fetchTimeout", { ms: timeoutMs, url }));
    }
    throw err;
  } finally {
    clearTimeout(timer);
    opts.signal?.removeEventListener("abort", cancel);
  }
  if (!resp.ok) throw new Error(fetchStatusError(resp.status, url));
  const contentType = resp.headers.get("content-type") ?? "";
  // Pre-check Content-Length when the server provides it. Cheaper to
  // refuse upfront than to start streaming a 1GB ISO.
  const declaredLen = Number(resp.headers.get("content-length") ?? "");
  if (Number.isFinite(declaredLen) && declaredLen > FETCH_MAX_BYTES) {
    throw new Error(t("webErrors.fetchTooLarge", { len: declaredLen, cap: FETCH_MAX_BYTES, url }));
  }
  const raw = await readBodyCapped(resp, FETCH_MAX_BYTES);
  const title = extractTitle(raw);
  const text = contentType.includes("text/html") ? htmlToText(raw) : raw;
  const truncated = text.length > maxChars;
  const finalText = truncated
    ? `${text.slice(0, maxChars)}\n\n[… truncated ${text.length - maxChars} chars …]`
    : text;
  return { url: currentUrl, title, text: finalText, truncated };
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
        throw new Error(t("webErrors.fetchBodyTooLarge", { cap: maxBytes, seen: total }));
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
  /** Config path to read at tool-call time. Defaults to ~/.reasonix/config.json. */
  configPath?: string;
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
          description: `Number of results to return. Default ${defaultTopK}.`,
        },
      },
      required: ["query"],
    },
    fn: async (args: { query: string; topK?: number }, ctx) => {
      // Read at call time, not registration time — `/search-engine` mutates config mid-session (#1309).
      const engine = loadWebSearchEngine(opts.configPath);
      const endpoint = loadWebSearchEndpoint(opts.configPath);
      const results = await webSearch(args.query, {
        topK: args.topK ?? defaultTopK,
        signal: ctx?.signal,
        engine,
        endpoint,
        configPath: opts.configPath,
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
        throw new Error(t("webErrors.fetchInvalidUrl"));
      }
      if (loadWebSearchEngine(opts.configPath) === "ollama") {
        const page = await webFetchOllama(args.url, {
          maxChars: maxFetchChars,
          signal: ctx?.signal,
          configPath: opts.configPath,
        });
        const header = page.title ? `${page.title}\n${page.url}` : page.url;
        const links = page.links?.length ? `\n\nlinks:\n${page.links.join("\n")}` : "";
        return `${header}\n\n${page.text}${links}`;
      }
      const page = await webFetch(args.url, { maxChars: maxFetchChars, signal: ctx?.signal });
      const header = page.title ? `${page.title}\n${page.url}` : page.url;
      return `${header}\n\n${page.text}`;
    },
  });

  return registry;
}

export function formatSearchResults(query: string, results: SearchResult[]): string {
  const lines: string[] = [`query: ${query}`];

  // Check if the first result carries an AI answer (Perplexity/Exa)
  const hasAnswer = results.length > 0 && results[0]?.url === "" && results[0]?.answer;

  if (hasAnswer) {
    lines.push("\nanswer:");
    lines.push(`  ${results[0]!.answer}`);
    const sources = results.slice(1);
    lines.push(`\nsources (${sources.length}):`);
    sources.forEach((r, i) => {
      lines.push(`\n${i + 1}. ${r.title}`);
      lines.push(`   ${r.url}`);
      if (r.snippet) lines.push(`   ${r.snippet}`);
    });
  } else {
    lines.push(`\nresults (${results.length}):`);
    results.forEach((r, i) => {
      lines.push(`\n${i + 1}. ${r.title}`);
      lines.push(`   ${r.url}`);
      if (r.snippet) lines.push(`   ${r.snippet}`);
    });
  }

  return lines.join("\n");
}

// Syntax highlighting via highlight.js core with a hand-picked language set
// (registering only what a coding agent surfaces keeps the bundle lean). This is
// the engine behind the editor seam's HljsCode / HljsDiff; token colors are
// themed in styles.css (.hljs-*) to match the app palette rather than a stock CSS.

import hljs from "highlight.js/lib/core";
import apache from "highlight.js/lib/languages/apache";
import bash from "highlight.js/lib/languages/bash";
import c from "highlight.js/lib/languages/c";
import clojure from "highlight.js/lib/languages/clojure";
import cmake from "highlight.js/lib/languages/cmake";
import coffeescript from "highlight.js/lib/languages/coffeescript";
import cpp from "highlight.js/lib/languages/cpp";
import csharp from "highlight.js/lib/languages/csharp";
import css from "highlight.js/lib/languages/css";
import dart from "highlight.js/lib/languages/dart";
import diff from "highlight.js/lib/languages/diff";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import elixir from "highlight.js/lib/languages/elixir";
import erlang from "highlight.js/lib/languages/erlang";
import fsharp from "highlight.js/lib/languages/fsharp";
import go from "highlight.js/lib/languages/go";
import gradle from "highlight.js/lib/languages/gradle";
import graphql from "highlight.js/lib/languages/graphql";
import groovy from "highlight.js/lib/languages/groovy";
import haskell from "highlight.js/lib/languages/haskell";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import julia from "highlight.js/lib/languages/julia";
import kotlin from "highlight.js/lib/languages/kotlin";
import latex from "highlight.js/lib/languages/latex";
import less from "highlight.js/lib/languages/less";
import lua from "highlight.js/lib/languages/lua";
import makefile from "highlight.js/lib/languages/makefile";
import markdown from "highlight.js/lib/languages/markdown";
import matlab from "highlight.js/lib/languages/matlab";
import nginx from "highlight.js/lib/languages/nginx";
import objectivec from "highlight.js/lib/languages/objectivec";
import perl from "highlight.js/lib/languages/perl";
import php from "highlight.js/lib/languages/php";
import powershell from "highlight.js/lib/languages/powershell";
import properties from "highlight.js/lib/languages/properties";
import protobuf from "highlight.js/lib/languages/protobuf";
import python from "highlight.js/lib/languages/python";
import r from "highlight.js/lib/languages/r";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import scala from "highlight.js/lib/languages/scala";
import scss from "highlight.js/lib/languages/scss";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import vbnet from "highlight.js/lib/languages/vbnet";
import vim from "highlight.js/lib/languages/vim";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

import { ALIASES } from "./lang";

hljs.registerLanguage("apache", apache);
hljs.registerLanguage("bash", bash);
hljs.registerLanguage("c", c);
hljs.registerLanguage("clojure", clojure);
hljs.registerLanguage("cmake", cmake);
hljs.registerLanguage("coffeescript", coffeescript);
hljs.registerLanguage("cpp", cpp);
hljs.registerLanguage("csharp", csharp);
hljs.registerLanguage("css", css);
hljs.registerLanguage("dart", dart);
hljs.registerLanguage("diff", diff);
hljs.registerLanguage("dockerfile", dockerfile);
hljs.registerLanguage("elixir", elixir);
hljs.registerLanguage("erlang", erlang);
hljs.registerLanguage("fsharp", fsharp);
hljs.registerLanguage("go", go);
hljs.registerLanguage("gradle", gradle);
hljs.registerLanguage("graphql", graphql);
hljs.registerLanguage("groovy", groovy);
hljs.registerLanguage("haskell", haskell);
hljs.registerLanguage("ini", ini);
hljs.registerLanguage("java", java);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("json", json);
hljs.registerLanguage("julia", julia);
hljs.registerLanguage("kotlin", kotlin);
hljs.registerLanguage("latex", latex);
hljs.registerLanguage("less", less);
hljs.registerLanguage("lua", lua);
hljs.registerLanguage("makefile", makefile);
hljs.registerLanguage("markdown", markdown);
hljs.registerLanguage("matlab", matlab);
hljs.registerLanguage("nginx", nginx);
hljs.registerLanguage("objectivec", objectivec);
hljs.registerLanguage("perl", perl);
hljs.registerLanguage("php", php);
hljs.registerLanguage("powershell", powershell);
hljs.registerLanguage("properties", properties);
hljs.registerLanguage("protobuf", protobuf);
hljs.registerLanguage("python", python);
hljs.registerLanguage("r", r);
hljs.registerLanguage("ruby", ruby);
hljs.registerLanguage("rust", rust);
hljs.registerLanguage("scala", scala);
hljs.registerLanguage("scss", scss);
hljs.registerLanguage("sql", sql);
hljs.registerLanguage("swift", swift);
hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("vbnet", vbnet);
hljs.registerLanguage("vim", vim);
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("yaml", yaml);

function escapeHtml(s: string): string {
  return s.replace(/[&<>]/g, (c) => (c === "&" ? "&amp;" : c === "<" ? "&lt;" : "&gt;"));
}

export const MAX_HIGHLIGHT_BYTES = 512 * 1024;
export const MAX_HIGHLIGHT_LINES = 20_000;

export function shouldHighlightCode(sourceSize: number, lineCount: number): boolean {
  return sourceSize <= MAX_HIGHLIGHT_BYTES && lineCount <= MAX_HIGHLIGHT_LINES;
}

// Apply one shared syntax-highlighting budget to chat blocks, tool output, and
// workspace previews. Callers with an authoritative byte size or an existing
// line split can pass those values to avoid duplicate work; ordinary chat
// blocks are measured here without allocating another copy of the source.
export function shouldHighlightSource(
  source: string,
  sourceSize?: number,
  lineCount?: number,
): boolean {
  if (sourceSize != null && sourceSize > MAX_HIGHLIGHT_BYTES) return false;
  if (lineCount != null && lineCount > MAX_HIGHLIGHT_LINES) return false;
  if (sourceSize != null && lineCount != null) return true;

  let measuredBytes = sourceSize ?? 0;
  let measuredLines = lineCount ?? 1;
  for (let i = 0; i < source.length; i += 1) {
    const codeUnit = source.charCodeAt(i);
    if (lineCount == null && codeUnit === 10) {
      measuredLines += 1;
      if (measuredLines > MAX_HIGHLIGHT_LINES) return false;
    }
    if (sourceSize != null) continue;

    if (codeUnit < 0x80) {
      measuredBytes += 1;
    } else if (codeUnit < 0x800) {
      measuredBytes += 2;
    } else if (
      codeUnit >= 0xd800
      && codeUnit <= 0xdbff
      && i + 1 < source.length
      && source.charCodeAt(i + 1) >= 0xdc00
      && source.charCodeAt(i + 1) <= 0xdfff
    ) {
      measuredBytes += 4;
      i += 1;
    } else {
      measuredBytes += 3;
    }
    if (measuredBytes > MAX_HIGHLIGHT_BYTES) return false;
  }
  return true;
}

// resolveLang maps a markdown fence tag or guessed name to a registered language,
// or "" when we can't highlight it (caller renders escaped plain text).
export function resolveLang(lang?: string): string {
  if (!lang) return "";
  const l = lang.toLowerCase();
  const resolved = ALIASES[l] ?? l;
  return hljs.getLanguage(resolved) ? resolved : "";
}

// LRU cache for highlighted output. The same code block can re-render many
// times (Re-renders due to streaming updates that don't change this block,
// React's StrictMode double-invoke in dev, hover-reveals of the toolbar's
// child elements). highlight.highlight() is a real lexer walk that shows
// up in the profile for large blocks; a 200-entry LRU keyed on the
// resolved language plus a fast hash of the code keeps the steady-state
// cost at a Map.get(). The Map is a plain LRU, not a WeakMap, so a large
// (non-streaming) transcript will eventually evict; the size of 200 is
// chosen to cover the visible viewport plus a small overshoot — most
// transcripts re-render the same ~30 visible blocks.
const HL_CACHE_MAX = 200;

// djb2 hash for the cache key. We only use the hash inside the key
// (Map<number, string>), not as a security primitive; collisions are fine
// because we ALSO store the original code alongside the entry and verify
// before serving the cached value. The hash is what makes the key
// constant-time to compare for Map.get(); comparing the full source
// string would be O(n) on every render and dwarf the savings.
function hashCode(s: string): number {
  let h = 5381;
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
  return h;
}

interface CacheEntry {
  code: string;
  html: string;
}
const hlCache = new Map<number, CacheEntry>();

function cacheGet(code: string, lang: string): string | null {
  const key = hashCode(lang + "\0" + code);
  const e = hlCache.get(key);
  if (!e) return null;
  // Defend against the (rare) hash collision: the stored code must match
  // the queried code exactly. We move the entry to the end of the Map to
  // mark it most-recently-used.
  if (e.code !== code) return null;
  hlCache.delete(key);
  hlCache.set(key, e);
  return e.html;
}

function cachePut(code: string, lang: string, html: string): void {
  const key = hashCode(lang + "\0" + code);
  if (hlCache.has(key)) hlCache.delete(key); // refresh
  hlCache.set(key, { code, html });
  while (hlCache.size > HL_CACHE_MAX) {
    // Map iteration is insertion-order; the first key is the oldest.
    const oldest = hlCache.keys().next().value;
    if (oldest === undefined) break;
    hlCache.delete(oldest);
  }
}

// highlightToHtml returns highlighted HTML (token <span>s) for the given code, or
// escaped plain text when the language is unknown. ignoreIllegals so partial /
// streaming snippets never throw. The LRU cache shaves the bulk of the work
// when a transcript re-renders the same blocks (most common: a streaming
// update changes the *next* block, not this one).
export function highlightToHtml(code: string, lang?: string): string {
  const resolved = resolveLang(lang);
  if (!resolved) return escapeHtml(code);
  const cached = cacheGet(code, resolved);
  if (cached !== null) return cached;
  let html: string;
  try {
    html = hljs.highlight(code, { language: resolved, ignoreIllegals: true }).value;
  } catch {
    return escapeHtml(code);
  }
  cachePut(code, resolved, html);
  return html;
}

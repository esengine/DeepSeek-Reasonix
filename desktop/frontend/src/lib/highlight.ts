// Syntax highlighting via highlight.js core with a hand-picked language set
// (registering only what a coding agent surfaces keeps the bundle lean). This is
// the engine behind the editor seam's HljsCode / HljsDiff; token colors are
// themed in styles.css (.hljs-*) to match the app palette rather than a stock CSS.

import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import css from "highlight.js/lib/languages/css";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import rust from "highlight.js/lib/languages/rust";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

import { ALIASES } from "./lang";

hljs.registerLanguage("bash", bash);
hljs.registerLanguage("css", css);
hljs.registerLanguage("go", go);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("json", json);
hljs.registerLanguage("markdown", markdown);
hljs.registerLanguage("python", python);
hljs.registerLanguage("rust", rust);
hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("yaml", yaml);

function escapeHtml(s: string): string {
  return s.replace(/[&<>]/g, (c) => (c === "&" ? "&amp;" : c === "<" ? "&lt;" : "&gt;"));
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

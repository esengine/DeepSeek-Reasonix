import { describe, expect, it } from "vitest";
import { EN } from "./en";
import { codes } from "./kernel";

// The catalogue degrades instead of breaking: a key it does not carry renders as
// Chinese. That is the right failure at runtime and the wrong one to ship, so
// this is what stops an English window from quietly filling with Chinese.
//
// Vite reads the sources, not the filesystem — the frontend carries no Node
// types, and a gate that needed them would only typecheck by accident.
const SOURCES = import.meta.glob(["../**/*.{ts,tsx}", "!../i18n/**", "!../**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

// Every Chinese literal inside a t()/tx()/plural() argument list is a key,
// including the arms of `t(cond ? "A" : "B")`. Matching only a literal that
// follows the paren immediately left those arms unchecked, which is how three
// of them reached the screen untranslated.
const HAN = /[一-鿿]/;

function keysIn(src: string): string[] {
  const keys: string[] = [];
  const call = /\b(?:t|tx|plural)\(/g;
  let m: RegExpExecArray | null;
  while ((m = call.exec(src)) !== null) {
    let i = m.index + m[0].length - 1;
    let depth = 0;
    while (i < src.length) {
      const c = src[i];
      if (c === "(" || c === "[" || c === "{") depth++;
      else if (c === ")" || c === "]" || c === "}") {
        depth--;
        if (depth === 0) break;
      } else if (c === '"' || c === "'") {
        const quote = c;
        let j = i + 1;
        let text = "";
        while (j < src.length && src[j] !== quote) {
          if (src[j] === "\\") j++;
          text += src[j];
          j++;
        }
        if (HAN.test(text)) keys.push(text);
        i = j;
      }
      i++;
    }
  }
  return keys;
}

describe("English catalogue", () => {
  it("carries every Chinese key the interface asks for", () => {
    const missing: string[] = [];
    for (const [file, body] of Object.entries(SOURCES)) {
      for (const key of keysIn(body)) {
        if (!(key in EN)) missing.push(`${JSON.stringify(key)} — ${file}`);
      }
    }
    expect([...new Set(missing)], "untranslated keys").toEqual([]);
  });

  // The catalogue can only carry a string the interface asks it for. Chinese
  // written straight into JSX renders as Chinese in an English window and no
  // check sees it, which is how a tab read 新会话 beside a row reading "New
  // session". Text between tags and text in a rendered attribute are the two
  // places that always reach the screen.
  it("routes rendered Chinese through the catalogue", () => {
    // Not after "=": a fat arrow is not a tag opening, and an arrow returning
    // Chinese before the next "<" in the file reads to this pattern as markup
    // — which is a false positive on ordinary code, and a guard that cries on
    // valid source is one people learn to edit around.
    const JSX_TEXT = /(?<!=)>([^<>{}]*[一-鿿][^<>{}]*)</g;
    const ATTR = /\b(title|placeholder|aria-label|label|alt)="([^"]*[一-鿿][^"]*)"/g;
    const raw: string[] = [];
    for (const [file, body] of Object.entries(SOURCES)) {
      // JSX only exists in .tsx; a ">" inside a plain string is not markup.
      if (!file.endsWith(".tsx")) continue;
      const code = body.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
      for (const m of code.matchAll(JSX_TEXT)) {
        if (m[1].trim()) raw.push(`${JSON.stringify(m[1].trim())} — ${file}`);
      }
      for (const m of code.matchAll(ATTR)) raw.push(`${m[1]}=${JSON.stringify(m[2])} — ${file}`);
    }
    expect([...new Set(raw)], "rendered Chinese not passed to t()").toEqual([]);
  });

  // The kernel's refusals are worded in kernel.ts, which SOURCES excludes along
  // with the rest of this directory — so every code added there reached English
  // windows in Chinese and no check saw it. Its values are keys like any other:
  // say() puts each through the same t().
  it("carries the wording the kernel's refusals resolve to", () => {
    const missing = Object.entries(codes)
      .filter(([, zh]) => !(zh in EN))
      .map(([code, zh]) => `${code} — ${JSON.stringify(zh)}`);
    expect(missing, "refusal wording with no English").toEqual([]);
  });

  it("reads the sources it is meant to be guarding", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(30);
  });
});

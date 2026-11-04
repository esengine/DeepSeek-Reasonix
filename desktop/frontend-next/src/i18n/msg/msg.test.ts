import { describe, expect, it } from "vitest";
import { AREAS, KEY, MESSAGES } from ".";

// The sources a key can be asked for in, read as text the way i18n.test.ts does.
const SOURCES = import.meta.glob(["../../**/*.{ts,tsx}", "!../../i18n/**", "!../../**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;
// The i18n directory's own tables of wording (kernel codes, notices) name keys too.
const WORDING = import.meta.glob(["../*.ts", "!../en*.ts", "!../*.test.ts"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const TABLES = import.meta.glob(["./*.ts", "!./*.test.ts", "!./index.ts", "!./types.ts"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const HAN = /[一-鿿]/;
const holes = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

/** The string literals a t()/tx()/plural() call is keyed by: those at its own
 *  level, not ones nested in the values it fills in. */
function literalsIn(src: string): string[] {
  const out: string[] = [];
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
        let j = i + 1;
        let text = "";
        while (j < src.length && src[j] !== c) {
          if (src[j] === "\\") j++;
          text += src[j];
          j++;
        }
        if (depth === 1) out.push(text);
        i = j;
      }
      i++;
    }
  }
  return out;
}

const asked = new Map<string, string>();
for (const [file, body] of Object.entries(SOURCES)) for (const lit of literalsIn(body)) if (KEY.test(lit)) asked.set(lit, file);

describe("the message tables", () => {
  it("name every message as area.meaning, in the table of its area", () => {
    const wrong = Object.entries(AREAS).flatMap(([area, table]) =>
      Object.keys(table).filter((key) => !KEY.test(key) || key.split(".")[0] !== area).map((key) => `${area}: ${key}`));
    expect(wrong).toEqual([]);
  });

  it("word every message in both languages, English without Chinese unless it is a name, with the same placeholders", () => {
    const bad = Object.entries(MESSAGES).flatMap(([key, m]) => [
      ...(!m.zh.trim() || !m.en.trim() ? [`${key}: empty`] : []),
      ...(HAN.test(m.en) && m.en !== m.zh ? [`${key}: Chinese in English`] : []),
      ...(holes(m.zh).join() !== holes(m.en).join() ? [`${key}: {${holes(m.zh)}} vs {${holes(m.en)}}`] : []),
    ]);
    expect(bad).toEqual([]);
  });

  it("carry every key the interface asks for", () => {
    const missing = [...asked].filter(([key]) => !(key in MESSAGES)).map(([key, file]) => `${key} — ${file}`);
    expect(missing).toEqual([]);
  });

  // A key may sit in a table the call site hands to t() later, so anywhere a
  // source names it as a string counts as asking for it.
  it("carry nothing the interface no longer asks for", () => {
    const named = new Set<string>();
    for (const body of [...Object.values(SOURCES), ...Object.values(WORDING)]) for (const m of body.matchAll(/["'`]([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)["'`]/g)) named.add(m[1]);
    expect(Object.keys(MESSAGES).filter((key) => !asked.has(key) && !named.has(key))).toEqual([]);
  });

  it("define every key once", () => {
    const seen = new Map<string, string>();
    const twice: string[] = [];
    for (const [file, body] of Object.entries(TABLES)) {
      for (const m of body.matchAll(/^\s*"([^"]+)":\s*\{/gm)) {
        if (seen.has(m[1])) twice.push(`${m[1]} — ${seen.get(m[1])} and ${file}`);
        else seen.set(m[1], file);
      }
    }
    expect(twice).toEqual([]);
    expect(seen.size).toBe(Object.keys(MESSAGES).length);
  });

  it("reads the sources it is meant to be guarding", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(30);
    expect(Object.keys(TABLES).length).toBe(Object.keys(AREAS).length);
  });
});

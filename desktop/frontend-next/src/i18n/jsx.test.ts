import { parse } from "@babel/parser";
import { describe, expect, it } from "vitest";

// Chinese that reaches a screen without passing through t(). The catalogue test
// beside this one checks the keys a t() call names; a literal written straight
// into JSX names no key at all, so nothing there ever sees it and the English
// window shows Chinese. i18n-sweep.mjs catches these too, but only where a walk
// of the running app happens to render them — a version list with no catalogue,
// an install that did not fail, and a whole panel goes unread.
const SOURCES = import.meta.glob(["../**/*.tsx", "!../i18n/**", "!../**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const HAN = /[一-鿿]/;
// t and tx translate; plural picks between two keys. A file may rename any of
// them on import, so the names are read from its own imports rather than fixed.
const TRANSLATORS = ["t", "tx", "plural"];

function translators(src: string): Set<string> {
  const names = new Set(TRANSLATORS);
  for (const m of src.matchAll(/import\s*\{([^}]*)\}\s*from\s*"[^"]*i18n[^"]*"/g)) {
    for (const part of m[1].split(",")) {
      const [orig, alias] = part.split(/\s+as\s+/).map((x) => x.trim());
      if (TRANSLATORS.includes(orig)) names.add(alias || orig);
    }
  }
  return names;
}

function untranslated(file: string, src: string): string[] {
  const found: string[] = [];
  const names = translators(src);
  const ast = parse(src, { sourceType: "module", plugins: ["typescript", "jsx"] });
  const visit = (node: any, parents: any[]): void => {
    if (!node || typeof node.type !== "string") return;
    const chain = [...parents, node];
    if (node.type === "JSXText" && HAN.test(node.value)) {
      found.push(`${file}:${node.loc.start.line} ${JSON.stringify(node.value.trim().slice(0, 40))}`);
    }
    const literal =
      (node.type === "StringLiteral" && HAN.test(node.value)) ||
      (node.type === "TemplateLiteral" && node.quasis.some((q: any) => HAN.test(q.value.raw)));
    if (literal && parents.some((p) => p.type.startsWith("JSX"))) {
      // The nearest enclosing call is the one that would translate it; a literal
      // under map() or createPortal() is on its way to the screen as written.
      const call = [...parents].reverse().find((p) => p.type === "CallExpression");
      const callee = call?.callee?.name ?? call?.callee?.property?.name ?? "";
      if (!names.has(callee)) {
        const text = node.type === "StringLiteral" ? node.value : node.quasis.map((q: any) => q.value.raw).join("${}");
        found.push(`${file}:${node.loc.start.line} ${JSON.stringify(text.slice(0, 40))}`);
      }
    }
    for (const key of Object.keys(node)) {
      if (key === "loc" || key.endsWith("Comments")) continue;
      const value = node[key];
      if (Array.isArray(value)) value.forEach((c) => c && typeof c.type === "string" && visit(c, chain));
      else if (value && typeof value.type === "string") visit(value, chain);
    }
  };
  visit(ast.program, []);
  return found;
}

describe("every Chinese string on screen goes through the catalogue", () => {
  it("no component renders a Chinese literal of its own", () => {
    const found = Object.entries(SOURCES).flatMap(([file, src]) => untranslated(file, src));
    expect(found, "untranslated literals in JSX").toEqual([]);
  });
});

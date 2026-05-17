/** Catches dangling `var(--foo)` references like the #919 invisible h3 — where var(--grad-8) was undefined and the heading text fell back to near-black on the dark page background. */

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const cssPath = fileURLToPath(new URL("../dashboard/app.css", import.meta.url));
const css = readFileSync(cssPath, "utf8");

const STRIPPED = css.replace(/\/\*[\s\S]*?\*\//g, "");

function definedVars(src: string): Set<string> {
  const defs = new Set<string>();
  const re = /(--[A-Za-z0-9-]+)\s*:/g;
  let m: RegExpExecArray | null = re.exec(src);
  while (m !== null) {
    defs.add(m[1] ?? "");
    m = re.exec(src);
  }
  defs.delete("");
  return defs;
}

function referencedVars(src: string): Set<string> {
  const refs = new Set<string>();
  const re = /var\(\s*(--[A-Za-z0-9-]+)(\s*,\s*[^)]*)?\)/g;
  let m: RegExpExecArray | null = re.exec(src);
  while (m !== null) {
    refs.add(m[1] ?? "");
    m = re.exec(src);
  }
  refs.delete("");
  return refs;
}

describe("dashboard/app.css — CSS custom property references", () => {
  it("every var(--foo) resolves to a defined custom property (regression for #919)", () => {
    const defs = definedVars(STRIPPED);
    const refs = referencedVars(STRIPPED);
    const undefinedRefs = [...refs].filter((r) => !defs.has(r)).sort();
    expect(undefinedRefs).toEqual([]);
  });
});

import { describe, expect, it } from "vitest";

// `.railhost` is a zero-height sticky layer over the transcript, so it turns
// pointer events off and every child that a person can press has to turn them
// back on. Forgetting that is invisible to jsdom — it does no hit testing — and
// shows up only as "the rows cannot be clicked" in a real browser.
const SHEETS = import.meta.glob("./{app,studio}.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

interface Rule {
  selector: string;
  decls: Map<string, string>;
}

function rules(css: string): Rule[] {
  const out: Rule[] = [];
  for (const m of css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const decls = new Map<string, string>();
    for (const part of m[2].split(";")) {
      const at = part.indexOf(":");
      if (at > 0) decls.set(part.slice(0, at).trim(), part.slice(at + 1).trim());
    }
    out.push({ selector: m[1].trim(), decls });
  }
  return out;
}

/** The declarations that apply to `className`, across every sheet, in source order. */
function declsOf(className: string): Map<string, string> {
  const merged = new Map<string, string>();
  for (const css of Object.values(SHEETS)) {
    for (const rule of rules(css)) {
      if (!rule.selector.split(",").some((s) => s.trim() === `.${className}`)) continue;
      for (const [k, v] of rule.decls) merged.set(k, v);
    }
  }
  return merged;
}

describe("the conversation rail can be pressed", () => {
  it("turns pointer events off on the layer that floats over the transcript", () => {
    expect(declsOf("railhost").get("pointer-events")).toBe("none");
  });

  // Anything a person can press inside that layer has to opt back in. The rail
  // itself always did; the two surfaces added with it — the unloaded-history
  // marker and the opened run — did not, and their rows fell through to the
  // transcript underneath.
  for (const surface of ["srail", "srail-earlier", "srail-sub"]) {
    it(`lets .${surface} take the pointer`, () => {
      expect(declsOf(surface).get("pointer-events"), `.${surface} sits inside pointer-events: none`).toBe("auto");
    });
  }
});

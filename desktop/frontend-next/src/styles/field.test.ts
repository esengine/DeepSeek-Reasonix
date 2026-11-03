import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob("./*.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

function rules(css: string): { selector: string; body: string }[] {
  const out: { selector: string; body: string }[] = [];
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const re = /([^{}]+)\{([^{}]*)\}/g;
  for (let m = re.exec(clean); m; m = re.exec(clean)) out.push({ selector: m[1].trim().replace(/\s+/g, " "), body: m[2] });
  return out;
}

const FORM_FIELDS = [
  ".unit-field", ".unit-field input", ".threshold-control input", ".addp .fields input", ".addp .ml-row input",
  ".net .fields input", ".bk-fields input", ".rmtf input", ".fontown", ".peerfield", ".mline .malias",
  ".advanced-field textarea", ".composition-editor input", ".addp .fields textarea",
];

describe("settings text fields", () => {
  it("take one box from kit.css: 32px, 7px corners, 13px text; 24px in model rows", () => {
    const kit = rules(SHEETS["./kit.css"]);
    const box = kit.find((r) => r.selector.startsWith(".prefs-main :is(input:not([type])") && /\bheight:\s*32px/.test(r.body));
    expect(box?.body).toMatch(/border-radius:\s*var\(--r-sm\)/);
    expect(box?.body).toMatch(/font-size:\s*var\(--fs-ui\)/);
    expect(box?.selector).toContain(".prefs-main .unit-field");
    const compact = kit.find((r) => r.selector.includes(".prefs-main .ml-row") && /\bheight:\s*24px/.test(r.body));
    expect(compact?.body).toMatch(/font-size:\s*var\(--fs-meta\)/);
  });

  it("are not re-sized by the page rules that used to draw them", () => {
    const owned = /(^|[;\s])(height|min-height|padding|border|border-radius|background|font-size|outline)\s*:/;
    const offenders: string[] = [];
    for (const [file, css] of Object.entries(SHEETS)) {
      if (file === "./kit.css") continue;
      for (const r of rules(css)) {
        const hit = r.selector.split(",").map((s) => s.trim()).filter((s) => FORM_FIELDS.includes(s.replace(/:(focus|focus-visible|focus-within)$/, "")));
        if (hit.length && owned.test(r.body)) offenders.push(`${file} ${hit.join(", ")}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});

import { describe, expect, it } from "vitest";

const CSS = import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const ROWS = import.meta.glob("./treerows.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const MARKER = "Theme token closure";

describe("the Studio shell closes over the active theme", () => {
  const css = Object.values(CSS)[0];
  const at = css.lastIndexOf(MARKER);
  const closure = at >= 0 ? css.slice(at) : "";

  it("keeps the compatibility layer last so prototype literals cannot win later", () => {
    expect(at).toBeGreaterThan(0);
    expect(css.slice(at + MARKER.length)).not.toContain(MARKER);
  });

  it("shares plain reading surfaces and weight with foundation palettes", () => {
    expect(css).toContain(':root[data-theme="light"]:not([data-bg]) {');
    expect(css).toContain(':root[data-theme="light"]:not([data-bg]):not([data-weight]) {');
  });

  it("previews the default from its own palette even while a pack is active", () => {
    const preview = css.match(/\.pal\[data-value=""\] \.pal-art\s*\{([^}]*)\}/s)?.[1] ?? "";
    for (const name of ["page", "surface", "panel", "border", "text", "dim", "accent"]) {
      expect(preview).toContain(`--pal-${name}: var(--default-${name})`);
    }
  });

  it("covers every persistent product surface", () => {
    for (const selector of [
      ".chrome", ".rail", ".compose", ".activity-group", ".studio-browser", ".btn.send",
      ".prefs-sheet", ".prefs-nav", ".prefs-main", ".grp",
    ]) {
      expect(closure, `${selector} is outside the theme closure`).toContain(selector);
    }
  });

  it("uses tokens instead of adding another fixed palette", () => {
    expect(closure).not.toMatch(/#[0-9a-f]{3,8}\b/i);
    for (const token of ["--page", "--surface", "--raised", "--overlay", "--border", "--text", "--muted", "--accent"]) {
      expect(closure, `${token} is not consumed by the closure`).toContain(`var(${token})`);
    }
  });

  it("gives selection a stronger neutral fill than hover and no extra bar", () => {
    const rows = Object.values(ROWS)[0];
    expect(rows).toMatch(/\.sessrow\[aria-selected="true"\][^{]*\{[^}]*var\(--text\) 8%/);
    expect(rows).toMatch(/\.sessrow:hover[^{]*\{[^}]*var\(--text\) 4%/);
    expect(rows).not.toContain("inset 2px");
    expect(Object.values(CSS)[0]).not.toMatch(/\.sessrow\[aria-selected="true"\]::before/);
  });

  // The scale is the reader's size and weight, not a literal: a pack states
  // colours and cannot move either, which is what this has always claimed.
  it("pins the composer to the Studio type scale even when a theme pack is active", () => {
    expect(closure).toMatch(/\.compose textarea\s*\{[^}]*font:\s*var\(--w-reg\) var\(--read\)\/1\.65 var\(--ui\)/s);
    expect(closure).toMatch(/\.compose textarea::placeholder\s*\{[^}]*font:\s*inherit/s);
    expect(closure).toMatch(/\.compose \.queue\s*\{[^}]*font:\s*500 calc\(10px \* var\(--fs-scale, 1\)\)\/1\.35 var\(--ui\)/s);
    expect(closure).toMatch(/\.compose \.mode,[\s\S]*?font-size:\s*calc\(11px \* var\(--fs-scale, 1\)\) !important/s);
  });

  it("keeps the sidebar brand on the same title-bar row as the chrome", () => {
    expect(closure).toMatch(/\.studio-brand\s*\{[^}]*height:\s*var\(--chrome-h\)/s);
    expect(css).toMatch(/\.studio-menu\s*\{[^}]*position:\s*fixed[^}]*width:\s*30px[^}]*height:\s*30px/s);
  });

  it("gives light-theme run data readable surfaces and leaves the composer metrics unframed", () => {
    const rails = [...closure.matchAll(/(?:^|\})\s*((?::root\[data-theme="light"\] )?\.studio-meterrail)\s*\{([^}]*)\}/g)];
    expect(rails.length).toBeGreaterThan(0);
    for (const [, , body] of rails) expect(body).not.toMatch(/\b(border|background|box-shadow|backdrop-filter)\s*:/);
    expect(closure).toMatch(/\.scroll\[data-pane="analysis"\]\s*\{[^}]*background:\s*transparent/s);
    expect(closure).toMatch(/\.run-budget, \.run-signals, \.run-rounds\s*\{[^}]*background:[^}]*var\(--raised\)/s);
  });

  it("keeps an inspected activity row neutral instead of using an AI-like status glow", () => {
    const selected = closure.match(/\.run-rounds li\[data-on\]\s*\{([^}]*)\}/s)?.[1] ?? "";
    expect(selected).toContain("var(--text)");
    expect(selected).toContain("var(--muted)");
    expect(selected).not.toContain("var(--ra-recovery)");
    expect(selected).not.toContain("var(--accent)");
  });
});

// The closure at the foot of this file only outranks rules of equal weight. A
// scheme-scoped literal — `:root[data-theme="light"] .x { color: #aaa }` — is
// heavier than the closure's `.x`, so it wins wherever the two name the same
// thing, and the migration reads as done while nothing changed. That is how the
// activity card and the session rail each kept a fixed palette under a theme
// pack. The count may fall freely; a new one has to take a rule out first.
describe("scheme-scoped literals, which outrank the closure", () => {
  const css = Object.values(CSS)[0];
  const SCHEME_LITERAL = /^:root\[data-theme=[^\n]*#[0-9a-fA-F]{3,8}/gm;
  const CARRIED = 24;

  it("is not growing", () => {
    const found = css.match(SCHEME_LITERAL) ?? [];
    expect(
      found.length,
      `scheme-scoped literal rules: ${found.length}. Put the difference in tokens.css instead — ` +
        "it already redefines every token per scheme, and a rule here silently beats the closure.",
    ).toBeLessThanOrEqual(CARRIED);
  });
});

import { describe, expect, it } from "vitest";

const CSS = import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = Object.values(CSS)[0];
const stage = [...css.matchAll(/\.onb-stage\s*\{([^}]*)\}/g)].map((match) => match[1]).join("\n");

describe("onboarding folds in the room left after interface zoom", () => {
  it("measures its own inline size", () => {
    expect(stage).toMatch(/container:\s*onboarding\s*\/\s*inline-size/);
  });

  it("moves the complete card to one column through a container query", () => {
    const fold = css.match(/@container onboarding \(max-width: 760px\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
    expect(fold).toMatch(/\.onb-shell\s*\{[^}]*grid-template-columns:\s*1fr/);
    expect(fold).toContain(".onb-progress");
    expect(fold).toContain(".onb { padding: 26px 22px 24px; }");
    const media = css.match(/@media \(max-width: 760px\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
    expect(media).not.toContain(".onb");
  });

  it("keeps viewport scrolling and focus clearance independent of the fold", () => {
    expect(stage).toContain("height: calc(100dvh / var(--zoom, 1))");
    expect(stage).toContain("scroll-padding-top: 64px");
    expect(stage).toContain("overflow: auto");
    expect(css).toMatch(/\.onb-brandbar\s*\{[^}]*position:\s*sticky;[^}]*top:\s*0/);
  });
});

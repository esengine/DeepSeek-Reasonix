import { describe, expect, it } from "vitest";

const CSS = import.meta.glob("./app.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

// `.cols` names four tracks and the window has three panels to put in them, so
// the fourth is only ever as wide as something mounted in it asks for. It was
// written the other way round — open by default, closed by an attribute — and
// the attribute was being driven by the built-in browser, which had since moved
// onto the workbench. The shell went on reserving 308px for a panel that no
// longer existed anywhere, and the workbench lost that much of the window every
// time the browser was opened.
describe("the shell reserves a column only for a panel that mounts in it", () => {
  const css = Object.values(CSS)[0];

  it("closes the side track by default", () => {
    // The rule that names the tracks, not whichever `.app` block comes first.
    const app = css.match(/\n\s*\.app\s*\{([^}]*--nav-w[^}]*)\}/);
    expect(app, "no .app track rule found; this file matches nothing").not.toBeNull();
    expect(app![1]).toMatch(/--side-w:\s*0px/);
  });

  it("opens it from the attribute that says a panel is there", () => {
    expect(css).toMatch(/\.app\[data-side="on"\]\s*\{\s*--side-w:\s*var\(--side-open\)/);
  });

  it("leaves no rule that reopens it from the attribute's absence", () => {
    expect(css).not.toMatch(/\[data-side="off"\]/);
  });
});

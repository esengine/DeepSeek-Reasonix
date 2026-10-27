import { expect, it } from "vitest";

const CSS = import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = CSS["./studio.css"]!;

// A workspace of several repositories wraps their sections in one container. Left
// unbounded it grows to the full height of every change and pushes the file list
// out of the explorer, so it has to cap its height and scroll on its own.
it("caps the multi-repository Git panel and lets it scroll", () => {
  const rule = css.match(/\.workbench-git\s*\{([^}]*)\}/)?.[1] ?? "";
  expect(rule).toMatch(/max-height:\s*\d+%/);
  expect(rule).toMatch(/overflow-y:\s*auto/);
  expect(rule).toMatch(/min-height:\s*0/);
});

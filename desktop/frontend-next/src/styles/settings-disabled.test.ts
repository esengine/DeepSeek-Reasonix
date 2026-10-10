import { expect, it } from "vitest";

const CSS = import.meta.glob(["./app.css", "./studio.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = CSS["./app.css"];

it("dims disabled settings navigation and keeps its cursor inactive", () => {
  const rule = css.match(/\.prefs-nav button:disabled,\s*\.prefs-found button:disabled\s*\{([^}]*)\}/)?.[1];
  expect(rule).toBeDefined();
  expect(rule).toMatch(/opacity:\s*\.45\s*;/);
  expect(rule).toMatch(/cursor:\s*default\s*(;|$)/);
});

it("limits navigation row and icon hover styling to enabled buttons", () => {
  expect(css).toMatch(/\.prefs-nav button:not\(:disabled\):hover\s*\{[^}]*background:\s*var\(--overlay\)/);
  expect(css).toMatch(/\.prefs-nav button:not\(:disabled\):hover svg\s*\{[^}]*color:\s*var\(--muted\)/);
});

it.each(Object.entries(CSS))("excludes disabled navigation from every hover override in %s", (_path, stylesheet) => {
  expect(stylesheet.match(/\.prefs-nav button:hover(?:\s|\{)/)).toBeNull();
  expect(stylesheet.match(/\.prefs-nav button:not\(:disabled\):hover\s*\{[^}]*background:/)).not.toBeNull();
});

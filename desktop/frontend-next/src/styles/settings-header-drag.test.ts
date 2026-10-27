import { expect, it } from "vitest";

const CSS = import.meta.glob("./dialogcenter.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = CSS["./dialogcenter.css"]!;

// Electron treats everything inside a drag region as part of the title bar, so
// the settings header that takes over the title bar swallows clicks on its own
// buttons and search field unless each is carved back out.
const rulesOf = (re: RegExp) => [...css.matchAll(re)].map((m) => m[1]!).join("\n");

it("makes the full-window settings header a drag region and carves its controls back out", () => {
  const drag = css.match(/\[data-titlebar="app"\] \.prefs-sheet\[data-wide\] \.prefs-hd\s*\{[^}]*-webkit-app-region:\s*drag/);
  expect(drag).not.toBeNull();
  const carved = rulesOf(/\[data-titlebar="app"\] \.prefs-sheet\[data-wide\] \.prefs-hd :is\(([^)]*)\)\s*\{[^}]*-webkit-app-region:\s*no-drag[^}]*\}/g);
  for (const control of ["button", "input", ".winctl"]) expect(carved).toContain(control);
});

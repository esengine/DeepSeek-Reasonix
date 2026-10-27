import { expect, it } from "vitest";

const CSS = import.meta.glob("./dialogcenter.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = CSS["./dialogcenter.css"]!;

it("keeps the transparent bottom border that centres a dialog under the title bar", () => {
  expect(css).toMatch(/:is\(\.prefs, \.fbk-veil, \.image-preview, \.cpv, \.agent-tx-scrim\)\s*\{\s*border-bottom:\s*var\(--chrome-h, 39px\) solid transparent;/);
});

it("takes it off a settings window that has been enlarged, so the sheet reaches the bottom of the window", () => {
  const rule = css.match(/\.prefs:has\(> \.prefs-sheet\[data-wide\]\)\s*\{([^}]*)\}/)?.[1] ?? "";
  expect(rule).toMatch(/border-bottom-width:\s*0/);
});

it("lets an enlarged settings window cover the title bar too, with its header standing in for the bar", () => {
  expect(css).toMatch(/\.prefs:has\(> \.prefs-sheet\[data-wide\]\)\s*\{\s*inset:\s*0;/);
  expect(css).toMatch(/\.prefs-sheet\[data-wide\]\s*\{\s*border-width:\s*0;\s*border-radius:\s*0;/);
  expect(css).toMatch(/\[data-titlebar="app"\] \.prefs-sheet\[data-wide\] \.prefs-hd\s*\{\s*-webkit-app-region:\s*drag;/);
  expect(css).toMatch(/\[data-platform="darwin"\] \.prefs-sheet\[data-wide\] \.prefs-hd\s*\{\s*padding-left:\s*calc\(var\(--lights-w, 96px\) \+ 8px\);/);
});
